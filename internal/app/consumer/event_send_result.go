package jobs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// errSendResultEarly is returned when a worker result lands before the
// control plane has finished stamping the task it belongs to. The message is
// left for redelivery; the stamping finishes within milliseconds.
var errSendResultEarly = errors.New("send result arrived before the task was stamped; retrying")

// HandleEmailSent confirms a send the worker delivered to the provider. The
// control plane reserves the step before it hands the send over and stamps it
// right after, so this normally only persists the Message-ID the worker put on
// the wire. It is also the repair path: when the stamp was lost between the
// dispatch and the control plane's write (a crash, or a failed progress write),
// the delivered email would otherwise leave no sent_at for follow-up pacing to
// read, so a reserved step is stamped here from the worker's own confirmation.
func (s *JobsService) HandleEmailSent(ctx context.Context, result models.SendEmailResult) error {
	if s.TaskRepo == nil || result.TaskID == uuid.Nil {
		return nil
	}
	task, err := s.TaskRepo.GetTask(ctx, result.TaskID)
	if err != nil {
		return err
	}
	if task == nil {
		log.Warn().Str("task_id", result.TaskID.String()).Msg("email sent result for unknown task")
		return nil
	}
	// The worker reports the Message-ID the provider put on the wire, which is
	// not always the one the control plane minted: Graph re-stamps it. Take the
	// worker's answer whenever it differs, because everything that matches a
	// send back to us later (campaign reply threading, warmup reply candidates)
	// keys on what the recipient actually received.
	if result.MessageID != "" && result.MessageID != task.MessageID {
		if err := s.TaskRepo.UpdateTaskMessageID(ctx, task.ID, result.MessageID); err != nil {
			log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("could not record worker message id")
		}
	}
	switch task.TaskType {
	case "campaign":
		s.repairCampaignSendStamp(ctx, task)
		// Anchor the graduation ramp on the mailbox's first CONFIRMED cold
		// send. Anchoring at dispatch would start the clock on a send the
		// worker then failed, and the ramp is a proxy for days of proven
		// sending. Idempotent in SQL, so every later send is a no-op.
		if s.WarmupRepo != nil {
			if err := s.WarmupRepo.StampColdRampStart(ctx, task.EmailAccountID); err != nil {
				log.Warn().Err(err).Str("email_account_id", task.EmailAccountID.String()).Msg("could not anchor the cold ramp")
			}
		}
	case "warmup":
		// Without this the recipient has nothing to match a warmup email
		// against when the verify header did not survive delivery.
		if s.WarmupRepo != nil && result.MessageID != "" {
			if err := s.WarmupRepo.RecordWarmupTokenDelivery(ctx, task.ID, result.MessageID); err != nil {
				log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("could not record the delivered warmup message id")
			}
		}
	}
	return nil
}

// repairCampaignSendStamp completes a reserved campaign step the control plane
// never stamped. A no-op in the normal case, where the stamp is already there.
func (s *JobsService) repairCampaignSendStamp(ctx context.Context, task *repository.Task) {
	if s.CampaignProgressRepo == nil {
		return
	}
	ct, err := s.TaskRepo.GetCampaignTask(ctx, task.ID)
	if err != nil || ct == nil || ct.CampaignID == nil || ct.ContactID == nil || ct.SequenceID == nil {
		return
	}
	repaired, err := s.CampaignProgressRepo.StampDispatchedSend(ctx, *ct.CampaignID, *ct.ContactID, *ct.SequenceID)
	if err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("could not repair the campaign send stamp")
		return
	}
	if repaired {
		log.Warn().
			Str("task_id", task.ID.String()).
			Str("campaign_id", ct.CampaignID.String()).
			Str("contact_id", ct.ContactID.String()).
			Msg("stamped a delivered campaign send the control plane never recorded")
	}
}

// HandleEmailFailed walks back a send the worker could not deliver. The
// control plane reserves a step and stamps it sent the moment it hands it to a
// worker, so without this the lead would sit at "processing" forever with no
// email ever leaving. For a campaign send the step's reservation and sent_at
// are cleared so the next tick retries it, the day's counters give the send
// back, the failure is written to the campaign's activity log, and a campaign
// that completed while the send was in flight is reopened. Once the retry cap
// is spent the lead is marked failed and routing drops it.
func (s *JobsService) HandleEmailFailed(ctx context.Context, result models.SendEmailResult) error {
	if s.TaskRepo == nil || result.TaskID == uuid.Nil {
		return nil
	}
	task, err := s.TaskRepo.GetTask(ctx, result.TaskID)
	if err != nil {
		return err
	}
	if task == nil {
		log.Warn().Str("task_id", result.TaskID.String()).Msg("email failed result for unknown task")
		return nil
	}

	if task.Status == "active" {
		// The worker answered before the control plane finished the tick; the
		// stamping completes within milliseconds, so look once more before
		// leaving the result for redelivery.
		time.Sleep(300 * time.Millisecond)
		if task, err = s.TaskRepo.GetTask(ctx, result.TaskID); err != nil || task == nil {
			return err
		}
	}
	switch task.Status {
	case "completed":
		// The normal case: stamped by the control plane, refused by the worker.
	case "active":
		return errSendResultEarly
	default:
		// Already walked back (duplicate delivery), cancelled, or dead-lettered.
		return nil
	}

	reason, code := sendFailureReason(result)
	if err := s.TaskRepo.RecordTaskFailure(ctx, task.ID, "Send failed", reason); err != nil {
		return err
	}

	switch task.TaskType {
	case "campaign":
		return s.failCampaignSend(ctx, task, reason, code, nil)
	case "email":
		s.notifyUserSendFailed(ctx, task, reason)
	}
	return nil
}

// failCampaignSend is the campaign half of HandleEmailFailed. countedOn is the
// day the send was counted against, for giving the daily counters back; nil
// reads it off the task, which is right for a worker result but not for the
// reclaimer, whose sends can be counted on an earlier day.
func (s *JobsService) failCampaignSend(ctx context.Context, task *repository.Task, reason, code string, countedOn *time.Time) error {
	ct, err := s.TaskRepo.GetCampaignTask(ctx, task.ID)
	if err != nil {
		return err
	}
	if ct == nil || ct.CampaignID == nil {
		return nil
	}
	campaignID := *ct.CampaignID

	var campaign *models.Campaign
	if s.CampaignRepo != nil {
		campaign, _ = s.CampaignRepo.GetByID(ctx, campaignID)
	}

	recipient := ""
	if ct.ContactID != nil && s.ContactRepo != nil {
		if contact, cerr := s.ContactRepo.GetByID(ctx, *ct.ContactID); cerr == nil && contact != nil {
			recipient = contact.Email
		}
	}

	attempts, exhausted, rolledBack := 0, false, false
	if ct.ContactID != nil && ct.SequenceID != nil && s.CampaignProgressRepo != nil {
		attempts, exhausted, rolledBack, err = s.CampaignProgressRepo.RecordSendFailure(ctx, campaignID, *ct.ContactID, *ct.SequenceID, reason)
		if err != nil {
			return err
		}
	}

	if rolledBack && s.CampaignRepo != nil {
		// With the step walked back, no other stamped step means this was the
		// lead's first email, which the new-lead cap counted.
		newLead := true
		if has, herr := s.CampaignProgressRepo.HasSentSteps(ctx, campaignID, *ct.ContactID); herr == nil {
			newLead = !has
		}
		day := time.Now()
		switch {
		case countedOn != nil:
			day = *countedOn
		case task.CompletedAt != nil:
			day = *task.CompletedAt
		}
		if derr := s.CampaignRepo.DecrementCampaignDailySend(ctx, campaignID, day, newLead); derr != nil {
			log.Warn().Err(derr).Str("campaign_id", campaignID.String()).Msg("could not give back the failed send's daily count")
		}
	}

	// A recipient the mail server refused at RCPT is a hard bounce that simply
	// arrived synchronously. Retrying it from the same mailbox only spends
	// reputation, so it goes through the bounce pipeline (progress, optional
	// suppression, guardrails, warmup health, webhooks) and the lead is
	// dropped as bounced instead of being offered again.
	if rolledBack && code == string(errx.MailErrorCodeRecipientRejected) {
		if s.recordSynchronousBounce(ctx, task, ct, campaign, recipient, reason) {
			s.logCampaignSendFailure(ctx, campaignID, ct, recipient, reason, code, attempts, false, false, false)
			s.publishCampaignUpdated(ctx, campaign, campaignID, "")
			log.Info().Str("task_id", task.ID.String()).Str("campaign_id", campaignID.String()).Msg("campaign send refused at RCPT; recorded as bounce")
			return nil
		}
	}

	reopened := false
	if rolledBack && !exhausted && campaign != nil && campaign.Status == "completed" && s.CampaignRepo != nil {
		if ok, rerr := s.CampaignRepo.ReopenAfterSendFailure(ctx, campaignID); rerr != nil {
			log.Warn().Err(rerr).Str("campaign_id", campaignID.String()).Msg("could not reopen campaign after send failure")
		} else {
			reopened = ok
		}
	}

	s.logCampaignSendFailure(ctx, campaignID, ct, recipient, reason, code, attempts, exhausted, rolledBack, reopened)

	status := ""
	if reopened {
		status = "active"
	}
	s.publishCampaignUpdated(ctx, campaign, campaignID, status)

	log.Info().
		Str("task_id", task.ID.String()).
		Str("campaign_id", campaignID.String()).
		Str("code", code).
		Int("attempts", attempts).
		Bool("exhausted", exhausted).
		Bool("rolled_back", rolledBack).
		Bool("reopened", reopened).
		Msg("campaign send failed in worker; step walked back")
	return nil
}

// recordSynchronousBounce feeds a RCPT-refused send into the deliverability
// pipeline as a bounce. Returns false when the bounce could not be attributed
// (no org, no recipient), in which case the caller falls back to the retry
// path.
func (s *JobsService) recordSynchronousBounce(ctx context.Context, task *repository.Task, ct *repository.CampaignTask, campaign *models.Campaign, recipient, reason string) bool {
	if s.AdvancedService == nil || campaign == nil || campaign.OrganizationID == nil || recipient == "" {
		return false
	}
	taskID := task.ID
	req := &models.IngestDeliverabilityEventRequest{
		EventType:      models.DeliverabilityEventBounce,
		Provider:       "smtp_reject",
		TaskID:         &taskID,
		CampaignID:     ct.CampaignID,
		ContactID:      ct.ContactID,
		RecipientEmail: recipient,
		Reason:         reason,
		IdempotencyKey: "reject:" + taskID.String(),
	}
	if xerr := s.AdvancedService.IngestDeliverabilityEvent(ctx, *campaign.OrganizationID, req); xerr != nil {
		log.Warn().Str("task_id", taskID.String()).Str("error", xerr.Message).Msg("could not record refused recipient as a bounce")
		return false
	}
	return true
}

// publishCampaignUpdated pulses the campaign for every teammate (status "" keeps
// the dashboard's status as is).
func (s *JobsService) publishCampaignUpdated(ctx context.Context, campaign *models.Campaign, campaignID uuid.UUID, status string) {
	if s.StreamingPublisher == nil || campaign == nil {
		return
	}
	s.StreamingPublisher.PublishCampaignEvent(ctx, &pubsub.CampaignEvent{
		BaseEvent:  pubsub.BaseEvent{EventType: pubsub.EventCampaignUpdated, UserID: campaign.UserID},
		OrgID:      campaignOrgID(campaign),
		CampaignID: campaignID.String(),
		Name:       campaign.Name,
		Status:     status,
	})
}

// logCampaignSendFailure writes the failure to the campaign activity log.
// metadata.level "error" tints it red in the dashboard's activity feed.
func (s *JobsService) logCampaignSendFailure(ctx context.Context, campaignID uuid.UUID, ct *repository.CampaignTask, recipient, reason, code string, attempts int, exhausted, rolledBack, reopened bool) {
	if s.CampaignLogRepo == nil {
		return
	}
	who := recipient
	if who == "" {
		who = "the recipient"
	}
	var msg string
	switch {
	case code == string(errx.MailErrorCodeRecipientRejected):
		msg = fmt.Sprintf("The mail server refused %s (hard bounce): %s", who, reason)
	case exhausted:
		msg = fmt.Sprintf("Gave up on %s after %d failed attempts: %s", who, attempts, reason)
	case rolledBack:
		msg = fmt.Sprintf("Could not send to %s (attempt %d of %d), will retry: %s", who, attempts, config.CampaignSendMaxAttempts, reason)
	default:
		msg = fmt.Sprintf("Could not send to %s: %s", who, reason)
	}
	if reopened {
		msg += ". Campaign reopened to retry."
	}
	meta := map[string]interface{}{
		"level":        "error",
		"code":         code,
		"error":        reason,
		"attempts":     attempts,
		"max_attempts": config.CampaignSendMaxAttempts,
		"will_retry":   rolledBack && !exhausted && code != string(errx.MailErrorCodeRecipientRejected),
		"task_id":      ct.TaskID.String(),
	}
	if ct.ContactID != nil {
		meta["contact_id"] = ct.ContactID.String()
	}
	if ct.SequenceID != nil {
		meta["sequence_id"] = ct.SequenceID.String()
	}
	if reopened {
		meta["reopened"] = true
	}
	_ = s.CampaignLogRepo.CreateLog(ctx, &repository.CampaignLogEntry{
		CampaignID: campaignID,
		EventType:  "email_failed",
		Message:    msg,
		Metadata:   meta,
	})
}

// notifyUserSendFailed tells the mailbox owner that a one-off send (compose,
// reply, scheduled email) did not leave, since nothing else would.
func (s *JobsService) notifyUserSendFailed(ctx context.Context, task *repository.Task, reason string) {
	if s.StreamingPublisher == nil || s.EmailRepository == nil {
		return
	}
	account, xerr := s.EmailRepository.GetByID(ctx, task.EmailAccountID)
	if xerr != nil || account == nil {
		return
	}
	s.StreamingPublisher.PublishEmailError(ctx, account.UserID, account.ID, task.ID,
		"Email could not be sent",
		fmt.Sprintf("%s could not send your email: %s", account.Email, reason))
}

// sendFailureReason picks the most useful human-readable reason and the
// machine code out of a worker result.
func sendFailureReason(result models.SendEmailResult) (reason, code string) {
	if result.Error != nil {
		code = result.Error.Code
		reason = strings.TrimSpace(result.Error.Message)
		if reason == "" {
			reason = strings.TrimSpace(result.Error.UserMessage)
		}
	}
	if reason == "" {
		reason = strings.TrimSpace(result.LegacyErrorMsg)
	}
	if reason == "" {
		reason = "the sending worker reported an unknown error"
	}
	if code == "" {
		code = "SEND_FAILED"
	}
	return reason, code
}

// campaignOrgID returns the campaign's organization id for org-scoped
// realtime events, or "" for legacy orgless rows.
func campaignOrgID(campaign *models.Campaign) string {
	if campaign == nil || campaign.OrganizationID == nil {
		return ""
	}
	return campaign.OrganizationID.String()
}
