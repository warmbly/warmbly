package tasks

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/scheduler"
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

	// STEP 3: Mark task as active (with advisory lock)
	if err := s.taskRepo.UpdateTaskStatusWithLock(ctx, taskID, "active"); err != nil {
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

	// STEP 5.5: Check if organization can send campaign emails (trial expired, etc.)
	if s.featureGate != nil && campaign.OrganizationID != nil {
		canSend, _ := s.featureGate.CanSendCampaignEmail(ctx, *campaign.OrganizationID)
		if !canSend {
			// Organization cannot send - pause campaign
			s.campaignRepo.UpdateStatus(ctx, campaign.ID, "paused_trial_expired")
			s.taskRepo.UpdateTaskStatus(ctx, taskID, "skipped_trial_expired")
			return nil
		}

		// Check daily limit
		limit, _ := s.featureGate.GetDailyEmailLimit(ctx, *campaign.OrganizationID)
		if limit >= 0 {
			// TODO: Check sentToday from stats repository
			// For now, we'll just track that there's a limit
			// sentToday, _ := s.statsRepo.GetSentToday(ctx, orgID)
			// if sentToday >= limit {
			//     s.taskRepo.UpdateTaskStatus(ctx, taskID, "skipped_daily_limit")
			//     return nil
			// }
		}
	}

	// STEP 6: Calculate next email to send
	nextTime, nextPair, accountID, err := s.scheduler.CalculateNextCampaignTime(ctx, *campaignTask.CampaignID)
	if err != nil {
		if errors.Is(err, scheduler.ErrNoEmailAccounts) {
			s.autoPauseCampaign(ctx, *campaignTask.CampaignID, taskID)
			return nil
		}
		if errors.Is(err, scheduler.ErrCampaignCompleted) {
			s.campaignRepo.UpdateStatus(ctx, campaign.ID, "completed")
			if s.campaignLogRepo != nil {
				s.campaignLogRepo.CreateLog(ctx, &repository.CampaignLogEntry{
					CampaignID: campaign.ID,
					EventType:  "completed",
					Message:    "Campaign completed: all emails sent",
				})
			}
		}
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

	// STEP 7.5: Update campaign task with contact_id and sequence_id for tracking
	// This allows the tracking consumer to find the correct contact/sequence when
	// processing open/click events from the tracking pixel service
	if err := s.taskRepo.UpdateCampaignTaskTracking(ctx, taskID, contact.ID, sequence.ID); err != nil {
		// Log but don't fail - tracking can still work via fallback methods
		fmt.Printf("Failed to update campaign task tracking: %v\n", err)
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
		UserID:    userUUID,
	}

	if err := s.emailSender.Send(ctx, taskID, emailMsg, *account); err != nil {
		// Failed to send to worker, record failure
		s.taskRepo.RecordTaskFailure(ctx, taskID, "Send failed", err.Error())
		if s.campaignLogRepo != nil {
			s.campaignLogRepo.CreateLog(ctx, &repository.CampaignLogEntry{
				CampaignID: campaign.ID,
				EventType:  "email_failed",
				Message:    fmt.Sprintf("Failed to send to %s", contact.Email),
				Metadata: map[string]interface{}{
					"contact_id": contact.ID.String(),
					"error":      err.Error(),
				},
			})
		}
		// Publish task failure to Pub/Sub
		if s.streamingPublisher != nil {
			s.streamingPublisher.PublishTaskStatus(ctx, campaign.UserID, taskID, pubsub.EventTaskFailed, "Failed to send email", map[string]string{
				"campaign_id": campaign.ID.String(),
				"contact_id":  contact.ID.String(),
				"error":       err.Error(),
			})
		}
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

	// Log email sent
	if s.campaignLogRepo != nil {
		s.campaignLogRepo.CreateLog(ctx, &repository.CampaignLogEntry{
			CampaignID: campaign.ID,
			EventType:  "email_sent",
			Message:    fmt.Sprintf("Email sent to %s", contact.Email),
			Metadata: map[string]interface{}{
				"contact_id":  contact.ID.String(),
				"sequence_id": sequence.ID.String(),
				"account_id":  account.ID.String(),
			},
		})
	}

	// STEP 18: Mark task as completed (with advisory lock)
	if err := s.taskRepo.UpdateTaskStatusWithLock(ctx, taskID, "completed"); err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	// Publish task completion to Pub/Sub
	if s.streamingPublisher != nil {
		s.streamingPublisher.PublishTaskStatus(ctx, campaign.UserID, taskID, pubsub.EventTaskCompleted, "Email sent successfully", map[string]string{
			"campaign_id": campaign.ID.String(),
			"contact_id":  contact.ID.String(),
		})
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

// autoPauseCampaign pauses a campaign when no active email accounts are available.
// Uses advisory lock to prevent concurrent auto-pause from multiple tasks.
func (s *tasksService) autoPauseCampaign(ctx context.Context, campaignID, taskID uuid.UUID) {
	s.campaignRepo.UpdateStatusWithLock(ctx, campaignID, "paused_no_accounts")
	s.taskRepo.UpdateTaskStatus(ctx, taskID, "completed")
	if s.campaignLogRepo != nil {
		s.campaignLogRepo.CreateLog(ctx, &repository.CampaignLogEntry{
			CampaignID: campaignID,
			EventType:  "auto_paused",
			Message:    "Campaign auto-paused: no active email accounts available",
		})
	}
}

// createCampaignTask creates a new campaign task in GCP Cloud Tasks
func (s *tasksService) createCampaignTask(ctx context.Context, campaignID uuid.UUID, scheduleTime time.Time) error {
	// Create task in database with advisory lock
	newTaskID := uuid.New()
	newTask := &Task{
		ID:          newTaskID,
		TaskType:    "campaign",
		Status:      "pending",
		ScheduledAt: &scheduleTime,
	}

	campaignTask := &CampaignTask{
		TaskID:     newTaskID,
		CampaignID: &campaignID,
	}

	if err := s.taskRepo.CreateTaskWithLock(ctx, newTask, campaignTask); err != nil {
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

