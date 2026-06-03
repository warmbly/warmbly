package tasks

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/scheduler"
	"github.com/warmbly/warmbly/internal/tasks/proto"
)

func (s *tasksService) HandleCampaignTask(task *proto.ProcessTask) *errx.Error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// STEP 1: Parse task ID
	taskID, err := uuid.Parse(task.TaskId)
	if err != nil {
		sentry.CaptureException(err)
		return errx.New(errx.BadRequest, "invalid task ID")
	}

	executionKey := "campaign:" + taskID.String()
	executionStatus := "failed"
	if s.advanced != nil {
		duplicate, xerr := s.advanced.StartTaskExecution(ctx, taskID, executionKey, map[string]interface{}{
			"task_type": "campaign",
		})
		if xerr != nil {
			return xerr
		}
		if duplicate {
			return nil
		}
		defer func() {
			_ = s.advanced.CompleteTaskExecution(ctx, taskID, executionKey, executionStatus, map[string]interface{}{
				"task_type": "campaign",
			})
		}()
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

	if taskRecord.Status != "pending" {
		log.Info().
			Str("task_id", taskID.String()).
			Str("status", taskRecord.Status).
			Msg("campaign task skipped: task not in pending state")
		executionStatus = "completed"
		return nil
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

	// Get campaign progress for task progress events
	campaignProgress, _ := s.campaignProgressRepo.GetCampaignProgress(ctx, *campaignTask.CampaignID)
	var totalContacts, processedCount int
	if campaignProgress != nil {
		totalContacts = campaignProgress.TotalContacts
		processedCount = campaignProgress.EmailsSent
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
		executionStatus = "completed"
		return nil // Don't create next task
	}

	// Publish task started progress event
	if s.streamingPublisher != nil {
		progress := 0
		if totalContacts > 0 {
			progress = (processedCount * 100) / totalContacts
		}
		s.streamingPublisher.PublishTaskProgress(ctx, &pubsub.TaskProgressEvent{
			BaseEvent:      pubsub.BaseEvent{UserID: campaign.UserID},
			CampaignID:     campaign.ID.String(),
			TaskID:         taskID.String(),
			Status:         "active",
			Progress:       progress,
			TotalContacts:  totalContacts,
			ProcessedCount: processedCount,
		})
	}

	// STEP 5.5: Check if organization can send campaign emails (trial expired, etc.)
	if s.featureGate != nil && campaign.OrganizationID != nil {
		canSend, _ := s.featureGate.CanSendCampaignEmail(ctx, *campaign.OrganizationID)
		if !canSend {
			// Organization cannot send - pause campaign
			s.campaignRepo.UpdateStatus(ctx, campaign.ID, "paused_trial_expired")
			s.taskRepo.UpdateTaskStatus(ctx, taskID, "skipped_trial_expired")
			executionStatus = "completed"
			return nil
		}

		// Check daily limit
		limit, _ := s.featureGate.GetDailyEmailLimit(ctx, *campaign.OrganizationID)
		if limit >= 0 {
			sentToday, err := s.campaignProgressRepo.CountEmailsSentTodayByOrganization(ctx, *campaign.OrganizationID)
			if err == nil && sentToday >= limit {
				s.taskRepo.UpdateTaskStatus(ctx, taskID, "skipped_daily_limit")
				if s.campaignLogRepo != nil {
					s.campaignLogRepo.CreateLog(ctx, &repository.CampaignLogEntry{
						CampaignID: campaign.ID,
						EventType:  "daily_limit_reached",
						Message:    "Campaign paused for today: organization daily email limit reached",
						Metadata: map[string]interface{}{
							"sent_today": sentToday,
							"limit":      limit,
						},
					})
				}

				// Reschedule to the next day to keep campaign progression alive.
				nextDay := time.Now().UTC().Truncate(24 * time.Hour).Add(24 * time.Hour).Add(5 * time.Minute)
				_, _, nextAccountID, calcErr := s.scheduler.CalculateNextCampaignTime(ctx, *campaignTask.CampaignID)
				if calcErr == nil {
					if err := s.createCampaignTask(ctx, campaign.ID, nextAccountID, nextDay); err != nil {
						log.Warn().Err(err).Str("campaign_id", campaign.ID.String()).Str("task_id", taskID.String()).Msg("Failed to create next campaign task after daily limit")
					}
				}
				executionStatus = "completed"
				return nil
			}
		}
	}

	// STEP 6: Calculate next email to send
	nextTime, nextPair, accountID, err := s.scheduler.CalculateNextCampaignTime(ctx, *campaignTask.CampaignID)
	if err != nil {
		if errors.Is(err, scheduler.ErrNoEmailAccounts) {
			s.autoPauseCampaign(ctx, *campaignTask.CampaignID, taskID)
			executionStatus = "completed"
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
		executionStatus = "completed"
		return nil
	}

	// STEP 7: Load contact and sequence
	contact, xerr := s.contactRepo.GetByID(ctx, nextPair.ContactID)
	if xerr != nil {
		return xerr
	}

	if s.advanced != nil && campaign.OrganizationID != nil {
		suppressed, reason, sxerr := s.advanced.ShouldSuppressRecipient(ctx, *campaign.OrganizationID, contact.Email)
		if sxerr != nil {
			return sxerr
		}
		if suppressed {
			_ = s.taskRepo.UpdateTaskStatusWithLock(ctx, taskID, "skipped_suppressed")
			if s.campaignLogRepo != nil {
				_ = s.campaignLogRepo.CreateLog(ctx, &repository.CampaignLogEntry{
					CampaignID: campaign.ID,
					EventType:  "suppressed",
					Message:    fmt.Sprintf("Suppressed recipient skipped: %s", contact.Email),
					Metadata: map[string]interface{}{
						"reason": reason,
					},
				})
			}
			_ = s.createCampaignTask(ctx, campaign.ID, accountID, nextTime)
			executionStatus = "completed"
			return nil
		}
	}

	// Pre-send verification gate: drop addresses already known to be invalid
	// (bad syntax / no MX / 550 RCPT) before a worker sends and earns a hard
	// bounce. Only 'invalid' is dropped — 'risky'/'unknown'/'valid' still send.
	if contact.VerificationStatus == "invalid" {
		_ = s.taskRepo.UpdateTaskStatusWithLock(ctx, taskID, "skipped_suppressed")
		if s.campaignLogRepo != nil {
			_ = s.campaignLogRepo.CreateLog(ctx, &repository.CampaignLogEntry{
				CampaignID: campaign.ID,
				EventType:  "suppressed",
				Message:    fmt.Sprintf("Unverifiable recipient skipped: %s", contact.Email),
				Metadata:   map[string]interface{}{"reason": contact.VerificationReason},
			})
		}
		_ = s.createCampaignTask(ctx, campaign.ID, accountID, nextTime)
		executionStatus = "completed"
		return nil
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
		log.Warn().Err(err).Str("campaign_id", campaign.ID.String()).Str("task_id", taskID.String()).Msg("Failed to update campaign task tracking")
	}

	// STEP 8: Check stop_on_reply
	if campaign.StopOnReply {
		hasReplied, err := s.campaignProgressRepo.CheckContactHasReplied(ctx, contact.ID, campaign.ID)
		if err == nil && hasReplied {
			// Contact has replied, skip
			s.taskRepo.UpdateTaskStatus(ctx, taskID, "completed")
			// Create next task anyway for next contact
			s.createCampaignTask(ctx, campaign.ID, accountID, nextTime)
			executionStatus = "completed"
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

	if s.advanced != nil && campaign.OrganizationID != nil {
		selection, sxerr := s.advanced.SelectVariant(ctx, *campaign.OrganizationID, campaign.ID, contact.ID, subject, bodyHTML, bodyPlain)
		if sxerr != nil {
			return sxerr
		}
		if selection != nil {
			subject = selection.Subject
			bodyHTML = selection.BodyHTML
			bodyPlain = selection.BodyPlain
		}
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

	// STEP 15.5: Generate List-Unsubscribe URL if enabled
	var unsubscribeURL string
	if campaign.UnsubscribeHeader {
		unsubscribeURL = fmt.Sprintf("https://%s/unsubscribe?cid=%s&rid=%s",
			config.Domain, campaign.ID.String(), contact.ID.String())
	}

	// STEP 16: Send email to worker via Kafka
	emailMsg := EmailMessage{
		From:           account.Email,
		To:             []string{contact.Email},
		CC:             campaign.CC,
		BCC:            campaign.BCC,
		Subject:        subject,
		BodyHTML:       bodyHTML,
		BodyPlain:      bodyPlain,
		MessageID:      messageID,
		IsWarmup:       false,
		Tracking:       tracking,
		UserID:         userUUID,
		UnsubscribeURL: unsubscribeURL,
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

			// Publish detailed task progress event for failure
			progress := 0
			if totalContacts > 0 {
				progress = (processedCount * 100) / totalContacts
			}
			contactName := contact.FirstName
			if contact.LastName != "" {
				contactName = contactName + " " + contact.LastName
			}
			s.streamingPublisher.PublishTaskProgress(ctx, &pubsub.TaskProgressEvent{
				BaseEvent:      pubsub.BaseEvent{UserID: campaign.UserID},
				CampaignID:     campaign.ID.String(),
				TaskID:         taskID.String(),
				Status:         "failed",
				ContactID:      contact.ID.String(),
				ContactEmail:   contact.Email,
				ContactName:    contactName,
				SequenceID:     sequence.ID.String(),
				SequenceName:   sequence.Name,
				Progress:       progress,
				TotalContacts:  totalContacts,
				ProcessedCount: processedCount,
			})
		}
		if s.advanced != nil {
			_ = s.advanced.CaptureTaskDeadLetter(ctx, taskID, "campaign", map[string]interface{}{
				"campaign_id": campaign.ID.String(),
				"contact_id":  contact.ID.String(),
				"email":       contact.Email,
			}, err.Error(), 1)
			_ = s.taskRepo.UpdateTaskStatus(ctx, taskID, "dead_lettered")
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
		log.Warn().Err(err).Str("campaign_id", campaign.ID.String()).Str("task_id", taskID.String()).Msg("Failed to record email sent")
	}

	// Publish campaign progress summary to Pub/Sub for real-time dashboard updates
	if s.streamingPublisher != nil {
		if progress, pErr := s.campaignProgressRepo.GetCampaignProgress(ctx, campaign.ID); pErr == nil && progress != nil {
			s.streamingPublisher.PublishCampaignProgress(ctx, campaign.UserID, campaign.ID, progress)
		}
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

		// Publish detailed task progress event
		newProcessedCount := processedCount + 1
		progress := 0
		if totalContacts > 0 {
			progress = (newProcessedCount * 100) / totalContacts
		}
		contactName := contact.FirstName
		if contact.LastName != "" {
			contactName = contactName + " " + contact.LastName
		}
		// Get sequence index
		sequences, _ := s.campaignRepo.GetSequencesByCampaignID(ctx, campaign.ID)
		seqIndex := 0
		for i, seq := range sequences {
			if seq.ID == sequence.ID {
				seqIndex = i + 1
				break
			}
		}
		s.streamingPublisher.PublishTaskProgress(ctx, &pubsub.TaskProgressEvent{
			BaseEvent:      pubsub.BaseEvent{UserID: campaign.UserID},
			CampaignID:     campaign.ID.String(),
			TaskID:         taskID.String(),
			Status:         "completed",
			ContactID:      contact.ID.String(),
			ContactEmail:   contact.Email,
			ContactName:    contactName,
			SequenceID:     sequence.ID.String(),
			SequenceName:   sequence.Name,
			SequenceIndex:  seqIndex,
			Progress:       progress,
			TotalContacts:  totalContacts,
			ProcessedCount: newProcessedCount,
		})
	}

	// STEP 19: Publish events to Kafka
	s.publishEmailSentEvent(ctx, taskRecord, account, campaign, contact, sequence)

	// STEP 20: Create next campaign task
	scheduledNext := nextTime
	if s.advanced != nil && campaign.OrganizationID != nil {
		if optimized, xerr := s.advanced.OptimizeSendTime(ctx, *campaign.OrganizationID, contact, nextTime); xerr == nil {
			scheduledNext = optimized
		}
	}

	if err := s.createCampaignTask(ctx, campaign.ID, account.ID, scheduledNext); err != nil {
		// Log but don't fail the current task
		log.Warn().Err(err).Str("campaign_id", campaign.ID.String()).Str("task_id", taskID.String()).Msg("Failed to create next campaign task")
	}

	executionStatus = "completed"
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
func (s *tasksService) createCampaignTask(ctx context.Context, campaignID, accountID uuid.UUID, scheduleTime time.Time) error {
	// Create task in database with advisory lock
	newTaskID := uuid.New()
	newTask := &Task{
		ID:             newTaskID,
		TaskType:       "campaign",
		EmailAccountID: accountID,
		Status:         "pending",
		ScheduledAt:    &scheduleTime,
	}

	campaignTask := &CampaignTask{
		TaskID:     newTaskID,
		CampaignID: &campaignID,
	}

	created, err := s.taskRepo.CreateTaskWithLock(ctx, newTask, campaignTask)
	if err != nil {
		return err
	}
	if !created {
		return nil
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
func (s *tasksService) publishEmailSentEvent(
	ctx context.Context,
	task *Task,
	account *Email,
	campaign *Campaign,
	contact *Contact,
	sequence *Sequence,
) {
	if s.eventsPublisher == nil {
		return
	}

	if err := s.eventsPublisher.PublishEmailSent(ctx, task, account, campaign, contact, sequence); err != nil {
		log.Warn().Err(err).Str("campaign_id", campaign.ID.String()).Str("task_id", task.ID.String()).Msg("Failed to publish email sent event")
	}
}
