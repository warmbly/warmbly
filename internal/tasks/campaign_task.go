package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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

	if campaignTask == nil {
		return errx.ErrNotFound
	}
	// campaign_tasks nulls its link when the campaign is deleted: the chain
	// ends here rather than leaving the row claimed as active.
	if campaignTask.CampaignID == nil {
		s.taskRepo.UpdateTaskStatus(ctx, taskID, "cancelled")
		executionStatus = "completed"
		return nil
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
		// Deleted between the pending check and here: the chain ends.
		if errors.Is(err, errx.ErrResourceNotFound) {
			s.taskRepo.UpdateTaskStatus(ctx, taskID, "cancelled")
			executionStatus = "completed"
			return nil
		}
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
			OrgID:          campaignOrgID(campaign),
			CampaignID:     campaign.ID.String(),
			TaskID:         taskID.String(),
			Status:         "active",
			Progress:       progress,
			TotalContacts:  totalContacts,
			ProcessedCount: processedCount,
		})
	}

	// STEP 5.4: Tenancy gate. organization_id is what scopes the entitlement
	// check below and the recipient suppression check in STEP 7; missing it used
	// to skip both and send anyway. Fail closed instead — an orgless campaign is
	// stopped and surfaced, never mailed unchecked.
	if campaign.OrganizationID == nil {
		s.haltOrglessCampaign(ctx, campaign.ID, taskID)
		executionStatus = "completed"
		return nil
	}
	orgID := *campaign.OrganizationID

	// STEP 5.5: Check if organization can send campaign emails (trial expired, etc.)
	if s.featureGate != nil {
		canSend, _ := s.featureGate.CanSendCampaignEmail(ctx, orgID)
		if !canSend {
			// Organization cannot send - pause campaign
			s.campaignRepo.UpdateStatus(ctx, campaign.ID, "paused_trial_expired")
			s.taskRepo.UpdateTaskStatus(ctx, taskID, "skipped_trial_expired")
			executionStatus = "completed"
			return nil
		}

		// Check daily limit
		limit, _ := s.featureGate.GetDailyEmailLimit(ctx, orgID)
		if limit >= 0 {
			sentToday, err := s.campaignProgressRepo.CountEmailsSentTodayByOrganization(ctx, orgID)
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
				if calcErr == nil || errors.Is(calcErr, scheduler.ErrCampaignDeferred) {
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
			s.autoPauseCampaign(ctx, *campaignTask.CampaignID, taskID, autoPauseReason(err))
			executionStatus = "completed"
			return nil
		}
		if errors.Is(err, scheduler.ErrCampaignDeferred) {
			// A valid contact exists but no eligible mailbox right now (ESP-strict
			// has no same-provider mailbox, or the daily new-lead cap is reached).
			// Reschedule at the deferred slot WITHOUT sending and WITHOUT touching
			// progress / daily counters / rotation — mirrors the daily-limit path.
			// Capped: the next-due moment can be days out, and until this chain
			// wakes nothing re-reads the campaign, so leads imported meanwhile
			// would sit queued until then.
			scheduledNext := scheduler.DeferSlot(nextTime)
			if cerr := s.createCampaignTask(ctx, campaign.ID, accountID, scheduledNext); cerr != nil {
				log.Warn().Err(cerr).Str("campaign_id", campaign.ID.String()).Str("task_id", taskID.String()).Msg("Failed to schedule deferred campaign task")
			}
			s.taskRepo.UpdateTaskStatus(ctx, taskID, "completed")
			executionStatus = "completed"
			return nil
		}
		// Terminal: all emails sent, OR the campaign passed its end date. Both end
		// the campaign at the "completed" status (the status enum has no separate
		// "ended"); the reason differs in the activity log.
		if errors.Is(err, scheduler.ErrCampaignCompleted) || errors.Is(err, scheduler.ErrCampaignEnded) {
			reason := "Campaign completed: all emails sent"
			if errors.Is(err, scheduler.ErrCampaignEnded) {
				reason = "Campaign ended: reached its end date"
			}
			// Leads verification refused are never routed, so say so here
			// rather than letting "all emails sent" cover for them.
			if n, cerr := s.campaignProgressRepo.CountUndeliverableLeads(ctx, campaign.ID); cerr == nil && n > 0 {
				reason = fmt.Sprintf("%s (%d lead(s) skipped: address verification refused them)", reason, n)
			}
			s.campaignRepo.UpdateStatus(ctx, campaign.ID, "completed")
			if s.campaignLogRepo != nil {
				s.campaignLogRepo.CreateLog(ctx, &repository.CampaignLogEntry{
					CampaignID: campaign.ID,
					EventType:  "completed",
					Message:    reason,
				})
			}
			// Broadcast live so the dashboard (and the sidebar campaign counters)
			// flip from "sending" to "finished" without a manual refresh.
			if s.streamingPublisher != nil {
				s.streamingPublisher.PublishCampaignEvent(ctx, &pubsub.CampaignEvent{
					BaseEvent: pubsub.BaseEvent{
						EventType: pubsub.EventCampaignCompleted,
						UserID:    campaign.UserID,
					},
					OrgID:      campaignOrgID(campaign),
					CampaignID: campaign.ID.String(),
					Name:       campaign.Name,
					Status:     "completed",
				})
			}
			s.taskRepo.UpdateTaskStatus(ctx, taskID, "completed")
			executionStatus = "completed"
			return nil
		}
		// Benign: the campaign was paused/deleted between ticks. Stop this chain
		// cleanly; a resume re-seeds it.
		if errors.Is(err, scheduler.ErrCampaignNotActive) {
			s.taskRepo.UpdateTaskStatus(ctx, taskID, "cancelled")
			executionStatus = "completed"
			return nil
		}
		// Transient / unknown error (a DB blip bubbled up from the scheduler). Do
		// NOT silently mark the task completed — that strands the campaign with no
		// successor. Record the failure for dashboard review, reset the task to
		// pending, and return 5xx so Cloud Tasks retries (with backoff). The
		// campaign reconciler is the backstop if retries are ever exhausted.
		sentry.CaptureException(err)
		s.recordSchedulerFailure(ctx, campaign.ID, "scheduler_error", "Could not compute the next step; retrying", err)
		// Pulse the dashboard so the failure appears live for the whole team. A
		// CAMPAIGN_UPDATED with empty status invalidates the campaign logs query
		// without flipping the campaign's status.
		if s.streamingPublisher != nil {
			s.streamingPublisher.PublishCampaignEvent(ctx, &pubsub.CampaignEvent{
				BaseEvent:  pubsub.BaseEvent{EventType: pubsub.EventCampaignUpdated, UserID: campaign.UserID},
				OrgID:      campaignOrgID(campaign),
				CampaignID: campaign.ID.String(),
			})
		}
		if rerr := s.taskRepo.UpdateTaskStatus(ctx, taskID, "pending"); rerr != nil {
			sentry.CaptureException(rerr)
		}
		executionStatus = "failed"
		return errx.InternalError()
	}

	// STEP 7: Load contact and sequence
	contact, xerr := s.contactRepo.GetByID(ctx, nextPair.ContactID)
	if xerr != nil {
		return xerr
	}

	if s.advanced != nil {
		suppressed, reason, sxerr := s.advanced.ShouldSuppressRecipient(ctx, orgID, contact.Email)
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
	// bounce. 'invalid' is always dropped; 'risky' is dropped only when the
	// campaign's "send to risky emails" toggle is off (see the next gate).
	// 'unknown'/'valid' always send.
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

	// Risky-recipient gate: when "send to risky emails" is off, also drop
	// addresses verification flagged 'risky' (catch-all / role / low-quality),
	// which raise bounce risk. Enforces the campaign.RiskyEmails toggle that the
	// settings UI exposes — without this the toggle is stored but inert.
	if !campaign.RiskyEmails && contact.VerificationStatus == "risky" {
		_ = s.taskRepo.UpdateTaskStatusWithLock(ctx, taskID, "skipped_suppressed")
		if s.campaignLogRepo != nil {
			_ = s.campaignLogRepo.CreateLog(ctx, &repository.CampaignLogEntry{
				CampaignID: campaign.ID,
				EventType:  "suppressed",
				Message:    fmt.Sprintf("Risky recipient skipped (send to risky emails is off): %s", contact.Email),
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

	// STEP 7.6: Non-email nodes (action / wait). These run a control-plane side
	// effect and route onward WITHOUT sending mail — the render/send block below
	// is reached only for email nodes. We stamp the node visited so routing
	// advances past it next tick, then schedule the next campaign tick (now for
	// instant actions and "end", now+wait for a wait node). An "end" node has no
	// outgoing connection, so the contact drops out of routing afterwards while
	// the campaign keeps processing other contacts.
	if sequence.Kind != "email" {
		var cfg models.ActionConfig
		if len(sequence.Action) > 0 {
			_ = json.Unmarshal(sequence.Action, &cfg)
		}
		aerr := s.executeActionNode(ctx, campaign, contact, sequence.ID, &cfg)
		if aerr != nil {
			log.Warn().Err(aerr).Str("campaign_id", campaign.ID.String()).Str("task_id", taskID.String()).Str("action", cfg.Type).Msg("Action node execution failed")
		}
		resumeAt := nextTime
		if cfg.Type == "wait" && cfg.WaitMinutes != nil && *cfg.WaitMinutes > 0 {
			resumeAt = time.Now().UTC().Add(time.Duration(*cfg.WaitMinutes) * time.Minute)
		}
		if rerr := s.campaignProgressRepo.RecordEmailSent(ctx, campaign.ID, contact.ID, sequence.ID); rerr != nil {
			log.Warn().Err(rerr).Str("campaign_id", campaign.ID.String()).Msg("Failed to record action node progress")
		}
		if cerr := s.createCampaignTask(ctx, campaign.ID, accountID, resumeAt); cerr != nil {
			log.Warn().Err(cerr).Str("campaign_id", campaign.ID.String()).Msg("Failed to schedule next task after action node")
		}
		if s.campaignLogRepo != nil {
			// Record the real outcome: a failed or skipped action (e.g. the linked
			// automation is disabled) must be visible, not logged as if it ran.
			evt, msg := "action", fmt.Sprintf("Ran '%s' action for %s", cfg.Type, contact.Email)
			if aerr != nil {
				evt, msg = "action_skipped", fmt.Sprintf("Action '%s' for %s did not run: %v", cfg.Type, contact.Email, aerr)
			}
			_ = s.campaignLogRepo.CreateLog(ctx, &repository.CampaignLogEntry{
				CampaignID: campaign.ID,
				EventType:  evt,
				Message:    msg,
			})
		}
		s.taskRepo.UpdateTaskStatus(ctx, taskID, "completed")
		executionStatus = "completed"
		return nil
	}

	// Load campaign attachments (campaign-wide; metadata only — the worker
	// fetches the bytes from object storage by S3 key at send time).
	var attachmentRefs []models.AttachmentRef
	if s.attachmentRepo != nil {
		atts, attErr := s.attachmentRepo.ListByCampaign(ctx, campaign.ID)
		if attErr != nil {
			log.Warn().Err(attErr).Str("campaign_id", campaign.ID.String()).Str("task_id", taskID.String()).Msg("Failed to load campaign attachments")
		} else {
			for _, a := range atts {
				attachmentRefs = append(attachmentRefs, models.AttachmentRef{
					S3Key:    a.S3Key,
					Filename: a.Filename,
					MimeType: a.MimeType,
				})
			}
		}
	}

	// STEP 7.5: Update campaign task with contact_id and sequence_id for tracking
	// This allows the tracking consumer to find the correct contact/sequence when
	// processing open/click events from the tracking pixel service
	if err := s.taskRepo.UpdateCampaignTaskTracking(ctx, taskID, contact.ID, sequence.ID); err != nil {
		// Log but don't fail - tracking can still work via fallback methods
		log.Warn().Err(err).Str("campaign_id", campaign.ID.String()).Str("task_id", taskID.String()).Msg("Failed to update campaign task tracking")
	}

	// stop_on_reply is enforced inside FindNextRoutedPair (STEP 6), and it is now
	// ROUTE-AWARE: a contact who replied is only handed back when their next step
	// is part of the reply flow (the reply branch's own path). The normal cold
	// sequence stops there, so there is no longer a blanket "contact has replied,
	// skip" check here — that would also kill the reply branch's follow-up emails.

	// STEP 9: Load email account
	account, xerr := s.emailRepo.GetByID(ctx, accountID)
	if xerr != nil {
		return xerr
	}

	// STEP 10: Render email template with contact variables, then expand any
	// {a|b|c} spintax per-recipient (only real |-groups; literal braces/CSS are
	// left intact) so each send varies for deliverability.
	subject := expandSpintax(RenderTemplate(sequence.Subject, *contact))
	bodyHTML := expandSpintax(RenderTemplate(sequence.BodyHTML, *contact))
	bodyPlain := expandSpintax(RenderTemplate(sequence.BodyPlain, *contact))

	// If no plain text provided, extract from HTML
	if bodyPlain == "" && bodyHTML != "" {
		bodyPlain = ExtractPlainTextFromHTML(bodyHTML)
	}

	if s.advanced != nil {
		selection, sxerr := s.advanced.SelectVariant(ctx, orgID, campaign.ID, contact.ID, sequence.ID, subject, bodyHTML, bodyPlain)
		if sxerr != nil {
			return sxerr
		}
		if selection != nil {
			subject = selection.Subject
			bodyHTML = selection.BodyHTML
			bodyPlain = selection.BodyPlain
		}
	}

	// STEP 10.5: Resolve per-recipient AI variables (AI blocks in the body that
	// generate unique copy for THIS contact). Runs before tracking/signature so
	// the generated copy gets open/click tracking like any other body text. A
	// zero-cost no-op when the body has no AI blocks. A generation failure fails
	// the send (recorded like other send failures) so the task retries with the
	// same cached output.
	if s.aiProvider != nil && s.aiCredits != nil {
		var aerr error
		subject, bodyHTML, bodyPlain, aerr = s.resolveAIVariables(ctx, campaign, contact, sequence.ID, subject, bodyHTML, bodyPlain)
		if aerr != nil {
			s.taskRepo.RecordTaskFailure(ctx, taskID, "AI variable resolution failed", aerr.Error())
			if s.campaignLogRepo != nil {
				s.campaignLogRepo.CreateLog(ctx, &repository.CampaignLogEntry{
					CampaignID: campaign.ID,
					EventType:  "email_failed",
					Message:    fmt.Sprintf("Failed to resolve AI variables for %s", contact.Email),
					Metadata: map[string]interface{}{
						"contact_id":  contact.ID.String(),
						"sequence_id": sequence.ID.String(),
						"error":       aerr.Error(),
					},
				})
			}
			if rerr := s.taskRepo.UpdateTaskStatus(ctx, taskID, "pending"); rerr != nil {
				sentry.CaptureException(rerr)
			}
			executionStatus = "failed"
			return errx.InternalError()
		}
	}

	// STEP 10.75: Score the copy the recipient will actually receive, after
	// merge fields, spintax, A/B and AI blocks have resolved. Advisory: it
	// warns once per step and never blocks or delays the send.
	s.warnOnWeakContent(ctx, orgID, campaign.ID, sequence.ID, sequence.Position+1,
		subject, bodyHTML, bodyPlain, len(attachmentRefs))

	// STEP 11: Add tracking on the host resolveTrackingHost picks, and log any
	// override that was configured but not verified so a customer whose links
	// are not going through their own domain can see why.
	trackingDomain, ignored := resolveTrackingHost(config.TrackingHost(), account, campaign)
	if s.campaignLogRepo != nil {
		for _, ign := range ignored {
			s.campaignLogRepo.CreateLog(ctx, &repository.CampaignLogEntry{
				CampaignID: campaign.ID,
				EventType:  "tracking_domain_unverified",
				Message:    ign.Message,
				Metadata:   map[string]interface{}{"scope": ign.Scope, "tracking_domain": ign.Domain, "mailbox": account.Email},
			})
		}
	}

	if campaign.OpenTracking && bodyHTML != "" {
		bodyHTML = AddOpenTrackingPixel(bodyHTML, taskID, trackingDomain)
	}

	if campaign.LinkTracking && bodyHTML != "" {
		wrapped, links := WrapLinksForTracking(bodyHTML, taskID, campaign.ID, trackingDomain)
		if len(links) == 0 {
			bodyHTML = wrapped
		} else if err := s.trackedLinkRepo.CreateBatch(ctx, links); err != nil {
			// Tracking is a nicety: ship the original working links rather
			// than tickets that would 404 at the tracking service.
			log.Warn().Err(err).Str("campaign_id", campaign.ID.String()).Str("task_id", taskID.String()).Msg("Failed to store tracked links; sending untracked")
		} else {
			bodyHTML = wrapped
		}
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

	// STEP 13: Warm the organization DEK so the publisher's encrypt pass (the
	// one whose ciphertext is actually sent) fails fast here if KMS is down.
	if account.OrganizationID == nil {
		sentry.CaptureException(fmt.Errorf("email account %s has no organization", account.ID))
		return errx.InternalError()
	}
	if _, err := s.cipherService.Cipher(ctx, *account.OrganizationID); err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	// STEP 14: Generate Message-ID
	messageID := generateMessageID(account.Email)

	// STEP 15: Build tracking info (worker receives the already-resolved host).
	var tracking *models.TrackingInfo
	if campaign.OpenTracking || campaign.LinkTracking {
		tracking = &models.TrackingInfo{
			OpenTracking:   campaign.OpenTracking,
			LinkTracking:   campaign.LinkTracking,
			TrackingDomain: trackingDomain,
		}
	}

	// STEP 15.5: Generate List-Unsubscribe URL if enabled
	var unsubscribeURL string
	if campaign.UnsubscribeHeader {
		unsubscribeURL = fmt.Sprintf("https://%s/unsubscribe?cid=%s&rid=%s",
			config.Domain, campaign.ID.String(), contact.ID.String())
	}

	// STEP 15.9: Reserve the send BEFORE it goes on the bus. Once the command is
	// published the recipient's copy is committed, so the record of the attempt
	// has to exist first: without it, a crash or a failed progress write between
	// the dispatch and the sent_at stamp leaves the step looking "never sent"
	// and the next tick emails the same person again (issue #169). The
	// reservation also counts the send against the day's counters, so a lost
	// stamp can never let the daily cap over-send either.
	reserved, rerr := s.campaignProgressRepo.ReserveSend(ctx, campaign.ID, contact.ID, sequence.ID, taskID, nextPair.IsNewLead)
	if rerr != nil {
		// The attempt could not be made durable, so it must not be made at all.
		// Retry the whole task rather than sending something nothing remembers.
		sentry.CaptureException(rerr)
		s.taskRepo.RecordTaskFailure(ctx, taskID, "Could not reserve the send", rerr.Error())
		s.recordSchedulerFailure(ctx, campaign.ID, "send_reservation_failed",
			fmt.Sprintf("Could not record the send to %s before dispatching it; retrying", contact.Email), rerr)
		if uerr := s.taskRepo.UpdateTaskStatus(ctx, taskID, "pending"); uerr != nil {
			sentry.CaptureException(uerr)
		}
		executionStatus = "failed"
		return errx.InternalError()
	}
	if !reserved {
		// Another tick already has this (contact, step) in flight or delivered.
		// End this one instead of sending a second copy, and keep the chain
		// alive for whoever is next.
		log.Warn().Str("campaign_id", campaign.ID.String()).Str("task_id", taskID.String()).
			Str("contact_id", contact.ID.String()).Str("sequence_id", sequence.ID.String()).
			Msg("campaign send skipped: the step is already in flight or sent")
		_ = s.taskRepo.UpdateTaskStatusWithLock(ctx, taskID, "skipped_duplicate")
		_ = s.createCampaignTask(ctx, campaign.ID, accountID, nextTime)
		executionStatus = "completed"
		return nil
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
		UnsubscribeURL: unsubscribeURL,
		Attachments:    attachmentRefs,
	}

	if err := s.emailSender.Send(ctx, taskID, emailMsg, *account); err != nil {
		// The send never reached a worker (none assigned, worker offline, bus
		// or storage down). Nothing is stamped sent; the task is dead-lettered
		// for the retry loop and the chain is re-seeded by the reconciler.
		//
		// Give the reservation back so the next tick retries the step — but ONLY
		// when the command provably never left. A failure of the publish call
		// itself is ambiguous (the bus may have taken it), so that reservation
		// stands and is resolved by the worker's own result, or by the reclaimer
		// if none ever comes.
		if !errors.Is(err, ErrSendDispatchUnknown) {
			if relErr := s.campaignProgressRepo.ReleaseSend(ctx, campaign.ID, contact.ID, sequence.ID, nextPair.IsNewLead); relErr != nil {
				sentry.CaptureException(relErr)
				log.Error().Err(relErr).Str("campaign_id", campaign.ID.String()).Str("task_id", taskID.String()).Msg("Failed to release the reservation for a send that never left; the reclaimer will retry it")
			}
		}
		s.taskRepo.RecordTaskFailure(ctx, taskID, "Send failed", err.Error())
		if s.campaignLogRepo != nil {
			s.campaignLogRepo.CreateLog(ctx, &repository.CampaignLogEntry{
				CampaignID: campaign.ID,
				EventType:  "email_failed",
				Message:    fmt.Sprintf("Could not hand %s's email to a sending worker, will retry: %s", contact.Email, err.Error()),
				Metadata: map[string]interface{}{
					"level":       "error",
					"code":        "WORKER_UNAVAILABLE",
					"contact_id":  contact.ID.String(),
					"sequence_id": sequence.ID.String(),
					"account_id":  account.ID.String(),
					"error":       err.Error(),
					"will_retry":  true,
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
				OrgID:          campaignOrgID(campaign),
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

	// STEP 17: Stamp the step sent. The reservation already made the attempt
	// durable, so this is the timing stamp follow-up pacing reads, not the
	// duplicate guard — but losing it still parks the lead, so retry it, and
	// escalate rather than whispering when every retry fails. The worker's own
	// EMAIL_SENT repairs it downstream if it never lands.
	// Today's counters were bumped by the reservation, inside the same
	// transaction, so the new-lead/day cap can never under-count.
	if err := s.stampSendRecorded(ctx, campaign.ID, contact.ID, sequence.ID); err != nil {
		sentry.CaptureException(err)
		log.Error().Err(err).Str("campaign_id", campaign.ID.String()).Str("task_id", taskID.String()).Str("contact_id", contact.ID.String()).Msg("Failed to record email sent; the worker result will repair the stamp")
		if s.campaignLogRepo != nil {
			s.campaignLogRepo.CreateLog(ctx, &repository.CampaignLogEntry{
				CampaignID: campaign.ID,
				EventType:  "progress_write_failed",
				Message:    fmt.Sprintf("The email to %s was sent but recording it failed; it will not be sent again", contact.Email),
				Metadata: map[string]interface{}{
					"level":       "error",
					"code":        "PROGRESS_WRITE_FAILED",
					"contact_id":  contact.ID.String(),
					"sequence_id": sequence.ID.String(),
					"error":       err.Error(),
				},
			})
		}
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

	// STEP 18.5: Advance the explicit-sender rotation cursor on a GENUINE send
	// only (single atomic UPDATE), so round_robin/least_recently_used cursors
	// stay coherent and a send-failure/skip never bumps them. The UPDATE is
	// scoped to (campaign_id, email_account_id), so it's a harmless no-op for
	// tag/all-resolved mailboxes that have no campaign_senders row.
	if err := s.campaignRepo.AdvanceCampaignSender(ctx, campaign.ID, account.ID); err != nil {
		log.Warn().Err(err).Str("campaign_id", campaign.ID.String()).Str("task_id", taskID.String()).Msg("Failed to advance campaign sender cursor")
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
		// EMAIL_SENT (org-scoped): the whole team sees the send + which
		// lead/step fired, live in the campaign view.
		s.streamingPublisher.PublishEmailSent(ctx, &pubsub.TaskProgressEvent{
			BaseEvent:      pubsub.BaseEvent{UserID: campaign.UserID},
			OrgID:          campaignOrgID(campaign),
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

	// STEP 20: Create next campaign task. The successor serves whichever lead
	// is due next, so it must never be shaped for the contact just emailed:
	// send-time optimization used to push it to that contact's next preferred
	// hour (tomorrow 09:00 by default after 17:00 UTC), leaving every other
	// lead queued for a day. Recipient-time placement belongs in slot
	// selection for the selected contact (issue #156), not here.
	if err := s.createCampaignTask(ctx, campaign.ID, account.ID, nextTime); err != nil {
		// Log but don't fail the current task
		log.Warn().Err(err).Str("campaign_id", campaign.ID.String()).Str("task_id", taskID.String()).Msg("Failed to create next campaign task")
	}

	executionStatus = "completed"
	return nil
}

// autoPauseCampaign pauses a campaign when no active email accounts are available.
// Uses advisory lock to prevent concurrent auto-pause from multiple tasks.
// autoPauseReason turns a no-mailbox scheduling error into the sentence the
// owner reads in the activity log. The narrower sentinels wrap
// ErrNoEmailAccounts, so they must be tested first; the difference between them
// is the difference between "fix your DNS", "widen your sending window", and
// "connect a mailbox".
func autoPauseReason(err error) string {
	switch {
	case errors.Is(err, scheduler.ErrDomainAuthFailing):
		return "Campaign auto-paused: every mailbox is sending from a domain that fails SPF or DMARC authentication"
	case errors.Is(err, scheduler.ErrNoEligibleMailbox):
		return "Campaign auto-paused: every mailbox is outside its sending window or over its daily budget"
	default:
		return "Campaign auto-paused: no active email accounts available"
	}
}

// autoPauseCampaign parks a campaign that has nothing it can send from. The
// reason is carried through to the activity log because "paused_no_accounts"
// covers several very different fixes (connect a mailbox, widen a sending
// window, repair DNS) and the status alone cannot tell them apart.
func (s *tasksService) autoPauseCampaign(ctx context.Context, campaignID, taskID uuid.UUID, reason string) {
	s.campaignRepo.UpdateStatusWithLock(ctx, campaignID, "paused_no_accounts")
	s.taskRepo.UpdateTaskStatus(ctx, taskID, "completed")
	if s.campaignLogRepo != nil {
		s.campaignLogRepo.CreateLog(ctx, &repository.CampaignLogEntry{
			CampaignID: campaignID,
			EventType:  "auto_paused",
			Message:    reason,
		})
	}
}

// haltOrglessCampaign stops a campaign that reached the send path with no
// organization. Suppression and the entitlement gate are both org-scoped, so
// there is no way to honour an unsubscribe, bounce or complaint for this
// campaign — it is parked rather than sent unchecked. Since migration 000092
// made campaigns.organization_id NOT NULL this should be unreachable, so it is
// also reported to Sentry: reaching it means tenancy was lost somewhere else.
func (s *tasksService) haltOrglessCampaign(ctx context.Context, campaignID, taskID uuid.UUID) {
	const reason = "Campaign paused: it has no workspace, so unsubscribes, bounces and complaints cannot be checked before sending. Contact support to reattach it."

	sentry.CaptureException(fmt.Errorf("campaign %s reached the send path with no organization", campaignID))
	log.Error().Str("campaign_id", campaignID.String()).Str("task_id", taskID.String()).
		Msg("campaign send blocked: no organization, suppression cannot be enforced")

	if err := s.campaignRepo.UpdateStatusWithLock(ctx, campaignID, "paused"); err != nil {
		sentry.CaptureException(err)
	}
	s.taskRepo.UpdateTaskStatus(ctx, taskID, "cancelled")
	if s.campaignLogRepo != nil {
		s.campaignLogRepo.CreateLog(ctx, &repository.CampaignLogEntry{
			CampaignID: campaignID,
			EventType:  "auto_paused",
			Message:    reason,
			Metadata:   map[string]interface{}{"level": "error", "code": "no_organization"},
		})
	}
}

// executeActionNode runs the control-plane side effect for a non-email node.
// "wait" and "end" have no side effect (their behaviour is timing / routing
// only); the others reuse existing repos/services. Everything here is
// control-plane — the worker is never involved for an action node.
func (s *tasksService) executeActionNode(ctx context.Context, campaign *models.Campaign, contact *models.Contact, sequenceID uuid.UUID, cfg *models.ActionConfig) error {
	switch cfg.Type {
	case "wait", "end", "":
		return nil
	case "ai_step":
		return s.execSequenceAIAgentStep(ctx, campaign, contact, sequenceID, cfg)
	case "add_tag":
		if cfg.CategoryID == nil || campaign.OrganizationID == nil {
			return nil
		}
		if _, xerr := s.contactRepo.Update(ctx, campaign.UserID, contact.ID.String(), *campaign.OrganizationID, &models.UpdateContact{
			AddCategories: []string{cfg.CategoryID.String()},
		}); xerr != nil {
			return xerr
		}
		return nil
	case "remove_tag":
		if cfg.CategoryID == nil || campaign.OrganizationID == nil {
			return nil
		}
		if _, xerr := s.contactRepo.Update(ctx, campaign.UserID, contact.ID.String(), *campaign.OrganizationID, &models.UpdateContact{
			RemoveCategories: []string{cfg.CategoryID.String()},
		}); xerr != nil {
			return xerr
		}
		return nil
	case "label_email":
		// Apply unibox labels to the contact's most recent conversation. A no-op
		// when the contact has no thread yet (returns "" thread, nil error).
		if len(cfg.LabelIDs) == 0 {
			return nil
		}
		owner, perr := uuid.Parse(campaign.UserID)
		if perr != nil {
			return nil
		}
		if _, xerr := s.advanced.LabelLatestThreadForContact(ctx, owner, contact.Email, cfg.LabelIDs); xerr != nil {
			return xerr
		}
		return nil
	case "unsubscribe":
		if xerr := s.advanced.Unsubscribe(ctx, campaign.ID, contact.ID); xerr != nil {
			return xerr
		}
		return nil
	case "create_task":
		if s.advanced == nil || campaign.OrganizationID == nil {
			return nil
		}
		owner, perr := uuid.Parse(campaign.UserID)
		if perr != nil {
			return nil
		}
		title := strings.TrimSpace(cfg.TaskTitle)
		if title == "" {
			name := strings.TrimSpace(contact.FirstName + " " + contact.LastName)
			if name == "" {
				name = contact.Email
			}
			title = "Follow up: " + name
		}
		// Per-step assignee; fall back to the campaign owner only when neither a
		// user nor a team is chosen (a team-assigned task has no single owner).
		assignee := cfg.TaskAssignedTo
		if assignee == nil && cfg.TaskAssignedTeamID == nil {
			assignee = &owner
		}
		// Task types are user-managed free text; pass the configured name
		// through (empty = untyped).
		cid := contact.ID
		data := &models.CreateCRMTask{
			ContactID:      &cid,
			Title:          title,
			Type:           cfg.TaskType,
			Priority:       cfg.TaskPriority,
			AssignedTo:     assignee,
			AssignedTeamID: cfg.TaskAssignedTeamID,
		}
		if cfg.TaskDueOffsetDays != nil {
			due := time.Now().UTC().AddDate(0, 0, *cfg.TaskDueOffsetDays)
			data.DueDate = &due
		}
		if _, xerr := s.advanced.CreateContactTask(ctx, *campaign.OrganizationID, owner, data); xerr != nil {
			return xerr
		}
		return nil
	case "create_deal":
		if s.advanced == nil || campaign.OrganizationID == nil {
			return nil
		}
		if cfg.DealPipelineID == nil || cfg.DealStageID == nil {
			// Misconfigured node (no pipeline/stage chosen): skip rather than fail
			// the whole chain.
			return nil
		}
		owner, perr := uuid.Parse(campaign.UserID)
		if perr != nil {
			return nil
		}
		// Deal name supports the same {{first_name}}/{{company}} templating other
		// campaign copy uses; fall back to a contact-derived name when blank.
		name := RenderTemplate(strings.TrimSpace(cfg.DealName), *contact)
		if name == "" {
			cn := strings.TrimSpace(contact.FirstName + " " + contact.LastName)
			if cn == "" {
				cn = contact.Email
			}
			name = "Deal: " + cn
		}
		currency := strings.TrimSpace(cfg.DealCurrency)
		if currency == "" {
			currency = "USD"
		}
		cid := contact.ID
		cmpID := campaign.ID
		data := &models.CreateDeal{
			PipelineID: *cfg.DealPipelineID,
			StageID:    *cfg.DealStageID,
			ContactID:  &cid,
			Name:       name,
			Value:      cfg.DealValue,
			Currency:   currency,
			CampaignID: &cmpID,
			AssignedTo: &owner,
		}
		if _, xerr := s.advanced.CreateContactDeal(ctx, *campaign.OrganizationID, owner, data); xerr != nil {
			return xerr
		}
		return nil
	case "move_deal_stage":
		if s.advanced == nil || campaign.OrganizationID == nil {
			return nil
		}
		if cfg.DealPipelineID == nil || cfg.DealStageID == nil {
			return nil
		}
		moved, xerr := s.advanced.MoveContactDealStage(ctx, *campaign.OrganizationID, contact.ID, *cfg.DealPipelineID, *cfg.DealStageID)
		if xerr != nil {
			return xerr
		}
		if moved == nil {
			// No open deal in the target pipeline: documented no-op. Log it so the
			// gap is observable instead of silently doing nothing.
			log.Info().
				Str("campaign_id", campaign.ID.String()).
				Str("contact_id", contact.ID.String()).
				Str("pipeline_id", cfg.DealPipelineID.String()).
				Msg("move_deal_stage no-op: contact has no open deal in pipeline")
		}
		return nil
	case "run_automation":
		if s.automationRunner == nil || campaign.OrganizationID == nil || cfg.AutomationID == nil {
			return nil
		}
		// Seed the automation's event data with the standard contact/campaign
		// keys so its action templates ({{.contact_email}} etc.) work out of the
		// box; the user-supplied values (rendered per contact) add/override extras.
		data := map[string]any{
			"campaign_id":   campaign.ID.String(),
			"campaign_name": campaign.Name,
			"contact_id":    contact.ID.String(),
			"contact_email": contact.Email,
			"first_name":    contact.FirstName,
			"last_name":     contact.LastName,
			"company":       contact.Company,
			"phone":         contact.Phone,
			// Stable per-(campaign,contact,step) key. Campaign tasks are
			// at-least-once, so a duplicate delivery would re-launch this step;
			// downstream actions (notably a webhook.ping to Zapier/Make) can dedupe
			// on this. The scheduler already routes each step once per lead, so a
			// real double-run is only a narrow retry window, shared by every action.
			"idempotency_key": fmt.Sprintf("campaign:%s:%s:%s", campaign.ID, contact.ID, sequenceID),
		}
		for _, kv := range cfg.AutomationValues {
			key := strings.TrimSpace(kv.Key)
			if key == "" {
				continue
			}
			data[key] = RenderTemplate(kv.Value, *contact)
		}
		return s.automationRunner.RunAutomationByID(ctx, *campaign.OrganizationID, *cfg.AutomationID, data)
	case "fire_event":
		if s.advanced == nil || campaign.OrganizationID == nil {
			return nil
		}
		s.advanced.FireCampaignEvent(ctx, *campaign.OrganizationID, campaign.ID.String(), cfg.EventName, cfg.EventFields, contact)
		return nil
	case "switch":
		return s.execSequenceSwitchStep(ctx, campaign, contact, sequenceID, cfg)
	default:
		return nil
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

	// Fan an opt-in firehose webhook for the send (campaign.email_sent).
	if s.advanced != nil && campaign.OrganizationID != nil {
		data := map[string]any{
			"campaign_id": campaign.ID.String(),
			"contact_id":  contact.ID.String(),
			"sequence_id": sequence.ID.String(),
		}
		if contact.Email != "" {
			data["contact_email"] = contact.Email
		}
		if account != nil {
			data["from_email"] = account.Email
		}
		s.advanced.EmitCampaignEvent(ctx, *campaign.OrganizationID, models.WebhookEventCampaignEmailSent, data)
	}
}

// campaignOrgID returns the campaign's organization id for org-scoped
// realtime events, or "" for legacy orgless rows.
func campaignOrgID(campaign *Campaign) string {
	if campaign == nil || campaign.OrganizationID == nil {
		return ""
	}
	return campaign.OrganizationID.String()
}

// stampSendRecorded writes the sent_at stamp for a send already on the bus,
// retrying a transient database error instead of tolerating it. The email
// cannot be un-sent at this point, so a single failed write used to be enough
// to make routing offer the same step again (issue #169); the reservation now
// prevents that, and these retries keep the lead's pacing correct too.
func (s *tasksService) stampSendRecorded(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID) error {
	var err error
	for attempt := 1; attempt <= config.CampaignSendStampAttempts; attempt++ {
		if err = s.campaignProgressRepo.RecordEmailSent(ctx, campaignID, contactID, sequenceID); err == nil {
			return nil
		}
		if attempt < config.CampaignSendStampAttempts {
			select {
			case <-ctx.Done():
				return err
			case <-time.After(time.Duration(attempt) * 250 * time.Millisecond):
			}
		}
	}
	return err
}

// recordSchedulerFailure writes a campaign-scoped, reviewable failure to the
// activity log so a stalled or retrying step is VISIBLE in the dashboard
// instead of failing silently. metadata.level="error" tints it red in the
// campaign detail (TaskPreview) and the log feed is already realtime-invalidated
// for every teammate. Best-effort and nil-safe — recording a failure must never
// itself break the tick.
func (s *tasksService) recordSchedulerFailure(ctx context.Context, campaignID uuid.UUID, code, message string, cause error) {
	if s.campaignLogRepo == nil {
		return
	}
	meta := map[string]interface{}{
		"level": "error",
		"code":  code,
	}
	if cause != nil {
		meta["error"] = cause.Error()
	}
	_ = s.campaignLogRepo.CreateLog(ctx, &repository.CampaignLogEntry{
		CampaignID: campaignID,
		EventType:  "scheduler_failed",
		Message:    message,
		Metadata:   meta,
	})
}
