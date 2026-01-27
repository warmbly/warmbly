package tasks

import (
	"context"
	"fmt"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/tasks/proto"
)

func (s *tasksService) HandleCampaignTask(task *proto.ProcessTask) *errx.Error {
	ctx := context.Background()

	// STEP 1: Parse task ID
	taskID, err := uuid.Parse(task.TaskId)
	if err != nil {
		sentry.CaptureException(err)
		return errx.New(errx.BadRequest, "invalid task ID")
	}

	// STEP 2: Load task record
	taskRecord, err := s.taskRepo.GetTask(ctx, taskID)
	if err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	if taskRecord == nil {
		return errx.ErrNotFound
	}

	// STEP 3: Mark task as active
	if err := s.taskRepo.UpdateTaskStatus(ctx, taskID, "active"); err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	// STEP 4: Load campaign task details
	campaignTask, err := s.taskRepo.GetCampaignTask(ctx, taskID)
	if err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	if campaignTask == nil || campaignTask.CampaignID == nil {
		return errx.ErrNotFound
	}

	// STEP 5: Load campaign
	campaign, err := s.campaignRepo.GetByID(ctx, *campaignTask.CampaignID)
	if err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	// Check if campaign is still active
	if campaign.Status != "active" {
		s.taskRepo.UpdateTaskStatus(ctx, taskID, "cancelled")
		return nil // Don't create next task
	}

	// STEP 5.5: Check if user can send campaign emails (trial expired, etc.)
	userID, parseErr := uuid.Parse(campaign.UserID)
	if parseErr != nil {
		sentry.CaptureException(parseErr)
		return errx.InternalError()
	}

	if s.featureGate != nil {
		canSend, _ := s.featureGate.CanSendCampaignEmail(ctx, userID)
		if !canSend {
			// User cannot send - pause campaign
			s.campaignRepo.UpdateStatus(ctx, campaign.ID, "paused_trial_expired")
			s.taskRepo.UpdateTaskStatus(ctx, taskID, "skipped_trial_expired")
			return nil
		}

		// Check daily limit
		limit, _ := s.featureGate.GetDailyEmailLimit(ctx, userID)
		if limit >= 0 {
			// TODO: Check sentToday from stats repository
			// For now, we'll just track that there's a limit
			// sentToday, _ := s.statsRepo.GetSentToday(ctx, userID)
			// if sentToday >= limit {
			//     s.taskRepo.UpdateTaskStatus(ctx, taskID, "skipped_daily_limit")
			//     return nil
			// }
		}
	}

	// STEP 6: Calculate next email to send
	nextTime, nextPair, accountID, err := s.scheduler.CalculateNextCampaignTime(ctx, *campaignTask.CampaignID)
	if err != nil {
		// Campaign might be completed or ended
		s.taskRepo.UpdateTaskStatus(ctx, taskID, "completed")
		return nil
	}

	// STEP 7: Load contact and sequence
	contact, xerr := s.contactRepo.GetByID(ctx, nextPair.ContactID)
	if xerr != nil {
		return xerr
	}

	sequence, err := s.campaignRepo.GetSequenceByID(ctx, nextPair.SequenceID)
	if err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	// STEP 8: Check stop_on_reply
	if campaign.StopOnReply {
		hasReplied, err := s.campaignProgressRepo.CheckContactHasReplied(ctx, contact.ID, campaign.ID)
		if err == nil && hasReplied {
			// Contact has replied, skip
			s.taskRepo.UpdateTaskStatus(ctx, taskID, "completed")
			// Create next task anyway for next contact
			s.createCampaignTask(ctx, campaign.ID, nextTime)
			return nil
		}
	}

	// STEP 9: Load email account
	account, xerr := s.emailRepo.GetByID(ctx, accountID)
	if xerr != nil {
		return xerr
	}

	// STEP 10: Render email template with contact variables
	subject := RenderTemplate(sequence.Subject, *contact)
	bodyHTML := RenderTemplate(sequence.BodyHTML, *contact)
	bodyPlain := RenderTemplate(sequence.BodyPlain, *contact)

	// If no plain text provided, extract from HTML
	if bodyPlain == "" && bodyHTML != "" {
		bodyPlain = ExtractPlainTextFromHTML(bodyHTML)
	}

	// STEP 11: Add tracking
	if campaign.OpenTracking && bodyHTML != "" {
		bodyHTML = AddOpenTrackingPixel(bodyHTML, taskID, account.TrackingDomain)
	}

	if campaign.LinkTracking && bodyHTML != "" {
		bodyHTML = WrapLinksForTracking(bodyHTML, taskID, account.TrackingDomain)
	}

	// STEP 12: Add signature
	if account.SignatureSync {
		if bodyHTML != "" {
			bodyHTML = AddSignature(bodyHTML, account.SignatureHTML, true)
		}
		if bodyPlain != "" {
			bodyPlain = AddSignature(bodyPlain, account.SignaturePlain, false)
		}
	}

	// STEP 13: Encrypt email content
	userUUID, err := uuid.Parse(account.UserID)
	if err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	cipher, err := s.cipherService.Cipher(ctx, userUUID)
	if err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	_, err = cipher.Encrypt(ctx, subject)
	if err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	_, err = cipher.Encrypt(ctx, bodyHTML)
	if err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	// STEP 14: Generate Message-ID
	messageID := generateMessageID(account.Email)

	// STEP 15: Build tracking info
	var tracking *models.TrackingInfo
	if campaign.OpenTracking || campaign.LinkTracking {
		tracking = &models.TrackingInfo{
			OpenTracking:   campaign.OpenTracking,
			LinkTracking:   campaign.LinkTracking,
			TrackingDomain: account.TrackingDomain,
		}
	}

	// STEP 16: Send email to worker via Kafka
	emailMsg := EmailMessage{
		From:      account.Email,
		To:        []string{contact.Email},
		CC:        campaign.CC,
		BCC:       campaign.BCC,
		Subject:   subject,
		BodyHTML:  bodyHTML,
		BodyPlain: bodyPlain,
		MessageID: messageID,
		IsWarmup:  false,
		Tracking:  tracking,
	}

	if err := s.emailSender.Send(ctx, taskID, emailMsg, *account); err != nil {
		// Failed to send to worker, record failure
		s.taskRepo.RecordTaskFailure(ctx, taskID, "Send failed", err.Error())
		return nil
	}

	// STEP 16: Store sent email metadata (encrypted) in database
	// Note: Full email stored in Cassandra by email sync service
	taskRecord.MessageID = messageID
	taskRecord.Status = "completed"

	// STEP 17: Update campaign progress
	if err := s.campaignProgressRepo.RecordEmailSent(ctx, campaign.ID, contact.ID, sequence.ID); err != nil {
		// Log but don't fail
		fmt.Printf("Failed to record email sent: %v\n", err)
	}

	// STEP 18: Mark task as completed
	if err := s.taskRepo.UpdateTaskStatus(ctx, taskID, "completed"); err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	// STEP 19: Publish events to Kafka
	s.publishEmailSentEvent(ctx, taskRecord, account, campaign)

	// STEP 20: Create next campaign task
	if err := s.createCampaignTask(ctx, campaign.ID, nextTime); err != nil {
		// Log but don't fail the current task
		fmt.Printf("Failed to create next campaign task: %v\n", err)
	}

	return nil
}

// createCampaignTask creates a new campaign task in GCP Cloud Tasks
func (s *tasksService) createCampaignTask(ctx context.Context, campaignID uuid.UUID, scheduleTime time.Time) error {
	// Create task in database
	newTaskID := uuid.New()
	newTask := &Task{
		ID:          newTaskID,
		TaskType:    "campaign",
		Status:      "pending",
		ScheduledAt: &scheduleTime,
	}

	if err := s.taskRepo.CreateTask(ctx, newTask); err != nil {
		return err
	}

	// Create campaign task entry
	campaignTask := &CampaignTask{
		TaskID:     newTaskID,
		CampaignID: &campaignID,
	}

	if err := s.taskRepo.CreateCampaignTask(ctx, campaignTask); err != nil {
		return err
	}

	// Create GCP Cloud Task
	processTask := &proto.ProcessTask{
		TaskId: newTaskID.String(),
	}

	cloudTaskName, err := s.tasksClient.CreateTask(ctx, processTask, scheduleTime)
	if err != nil {
		return err
	}

	// Update task with cloud task name
	if err := s.taskRepo.UpdateTaskScheduledAt(ctx, newTaskID, scheduleTime, cloudTaskName); err != nil {
		return err
	}

	return nil
}

// publishEmailSentEvent publishes email sent event to Kafka
func (s *tasksService) publishEmailSentEvent(ctx context.Context, task *Task, account *Email, campaign *Campaign) {
	// TODO: Implement Kafka event publishing
	// This will be implemented in Phase 5
	fmt.Printf("Email sent: task=%s, account=%s, campaign=%s\n", task.ID, account.ID, campaign.ID)
}

