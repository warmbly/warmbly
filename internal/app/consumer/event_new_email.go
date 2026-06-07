package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
)

func (s *JobsService) HandleNewEmail(ctx context.Context, e *models.JobEventNewEmail) error {
	// Check for warmup token header in message headers.
	// Try the current header name first, then the legacy "X-Warmbly-Token"
	// so messages in flight during the rollout continue to verify.
	warmupToken := extractHeaderValue(e.Message, config.WarmupVerifyHeader)
	if warmupToken == "" {
		warmupToken = extractHeaderValue(e.Message, "X-Warmbly-Token")
	}
	if warmupToken != "" {
		handled, err := s.handleWarmupEmail(ctx, e, warmupToken)
		if err != nil {
			// Log but don't block normal processing
			CaptureError(e.UserID, e.Message.EmailID, fmt.Errorf("warmup handling error: %w", err))
		}
		if handled {
			return nil // Don't add to unibox
		}
	}

	// Normal email processing
	if err := s.UniboxRepository.CreateEntry(ctx, e.UserID, e.Message); err != nil {
		CaptureError(e.UserID, e.Message.EmailID, err)
		return err
	}
	if s.StreamingPublisher != nil && e.Message != nil {
		s.StreamingPublisher.PublishEmailReceived(ctx, emailInboxEvent(e.UserID, e.Message))
	}

	// Advanced reply-intent automation is best-effort and should not block inbox
	// ingest. ProcessIncomingReply also runs the layered reply classifier
	// (replyclassify) and persists reply_class/confidence/source on the contact's
	// campaign progress, gating replied_at so automated replies (auto_reply /
	// out_of_office) never count as a human reply for stop_on_reply / branching.
	if s.AdvancedService != nil {
		_ = s.AdvancedService.ProcessIncomingReply(ctx, e.Message.EmailID, e.Message)
	}

	return nil
}

func (s *JobsService) publishEmailUpdated(ctx context.Context, userID uuid.UUID, message *models.EmailMessageStoreData) {
	if s.StreamingPublisher == nil || message == nil {
		return
	}
	s.StreamingPublisher.PublishEmailUpdated(ctx, emailInboxEvent(userID, message))
}

func emailInboxEvent(userID uuid.UUID, message *models.EmailMessageStoreData) *pubsub.EmailInboxEvent {
	return &pubsub.EmailInboxEvent{
		BaseEvent:      pubsub.BaseEvent{UserID: userID.String()},
		EmailAccountID: message.EmailID.String(),
		MessageID:      message.ID.String(),
		ThreadID:       message.ThreadID,
		Subject:        message.Subject,
		From:           strings.Join(message.FromAddr, ", "),
		Preview:        message.Snippet,
	}
}

// extractHeaderValue extracts a custom header value from the email message
// Checks InReplyTo field encoding or direct header access
func extractHeaderValue(msg *models.EmailMessageStoreData, headerName string) string {
	if msg == nil {
		return ""
	}

	// Check flags for X-Warmbly-Token (workers store custom headers in flags for detection)
	for _, flag := range msg.Flags {
		if strings.HasPrefix(flag, headerName+":") {
			return strings.TrimPrefix(flag, headerName+":")
		}
	}

	return ""
}

// handleWarmupEmail handles a detected warmup email
func (s *JobsService) handleWarmupEmail(ctx context.Context, e *models.JobEventNewEmail, tokenStr string) (bool, error) {
	if s.WarmupRepo == nil {
		return false, nil
	}

	tokenUUID, err := uuid.Parse(tokenStr)
	if err != nil {
		// Invalid format → record attempt
		s.applyInvalidWarmupAttempt(ctx, e.Message.EmailID, tokenStr, 0)
		return false, nil // Process as normal email
	}

	token, err := s.WarmupRepo.GetWarmupToken(ctx, tokenUUID)
	if err != nil || token == nil {
		// Token not found/expired → suspicious
		s.applyInvalidWarmupAttempt(ctx, e.Message.EmailID, tokenStr, 5)
		return false, nil
	}

	// Verify recipient matches
	if token.RecipientAccountID != e.Message.EmailID {
		s.applyInvalidWarmupAttempt(ctx, e.Message.EmailID, tokenStr, 0)
		return false, nil
	}

	// Valid! Consume the token
	s.WarmupRepo.ConsumeWarmupToken(ctx, tokenUUID)

	// Record the receipt so a later deletion or spam-flag of THIS message can be
	// attributed back to warmup and to the sender. Verified warmup mail is not
	// stored in the unibox, so this is the only record that the message was a
	// warmup email.
	if e.Message != nil {
		_ = s.WarmupRepo.RecordWarmupReceived(ctx, e.Message.EmailID, e.Message.ID, e.Message.MessageID, token.SenderAccountID)
	}

	// If the warmup mail arrived in a Junk/Spam state, record a
	// spam_placement event against the sender. This is distinct from a
	// user_complaint (which fires later via HandleFlagsAdd when a recipient
	// flags an already-delivered message) because nobody actively rejected
	// it — the provider classifier placed it there on arrival.
	if containsSpamFlag(e.Message.Flags) && s.WarmupService != nil {
		// Record which recipient provider/domain filtered it into spam so the
		// placement signal can be segmented per provider, not one flat rate.
		provider, domain := s.recipientProviderDomain(ctx, e.Message.EmailID)
		health, _ := s.WarmupService.RecordSpamPlacement(ctx, e.Message.EmailID, token.SenderAccountID, e.Message.MessageID, token.ContentSource, provider, domain)
		s.markRiskBandFromWarmupHealth(ctx, token.SenderAccountID, health)
	}

	// Perform warmup actions
	s.performWarmupActions(ctx, e)
	return true, nil
}

// performWarmupActions publishes warmup action events to the worker. Action
// selection is probabilistic and per-mailbox (see engagementPlan) so the pool
// doesn't behave in detectable lockstep, with a randomised recipient-side
// dwell before the actions run.
func (s *JobsService) performWarmupActions(ctx context.Context, e *models.JobEventNewEmail) {
	if s.Publisher == nil {
		return
	}

	settings := s.getGenerationSettings(ctx)
	actions, delaySeconds := engagementPlan(e.Message.EmailID, settings.Engagement)
	immediate, delayed := splitEngagementLegs(actions)

	base := models.WarmupEmailAction{
		UserID:             e.UserID,
		EmailID:            e.Message.EmailID,
		GmailID:            e.Message.GmailID,
		UID:                e.Message.UID,
		MailboxUIDValidity: e.Message.Mailbox,
	}

	// Resolve the receiving mailbox's worker once.
	var workerID *uuid.UUID
	if s.EmailRepository != nil {
		if account, xerr := s.EmailRepository.GetByID(ctx, e.Message.EmailID); xerr == nil && account != nil {
			workerID = account.WorkerID
		}
	}
	if workerID == nil {
		// No assigned worker (mid-migration / just-unassigned / assignment lag):
		// the warmup mail can't be foldered or engaged with. Log instead of
		// dropping silently so the gap is observable.
		log.Warn().
			Str("email_id", e.Message.EmailID.String()).
			Msg("Warmup actions skipped: recipient mailbox has no assigned worker")
		return
	}

	// Immediate, durable leg (folder + spam-rescue): publish to the worker now.
	if len(immediate) > 0 {
		act := base
		act.Actions = immediate
		s.Publisher.PublishWarmupAction(ctx, *workerID, &act)
	}

	if len(delayed) == 0 {
		return
	}

	act := base
	act.Actions = delayed

	// Delayed leg (read / important / star): with no dwell (or no durable store
	// available) publish immediately; otherwise persist it to the durable
	// schedule so a worker restart mid-dwell can't drop it. The poller publishes
	// it when fire_at passes.
	if delaySeconds <= 0 || s.WarmupEngagementRepo == nil {
		s.Publisher.PublishWarmupAction(ctx, *workerID, &act)
		return
	}

	payload, err := json.Marshal(act)
	if err != nil {
		log.Warn().Err(err).Str("email_id", e.Message.EmailID.String()).Msg("Failed to marshal delayed warmup engagement; publishing immediately")
		s.Publisher.PublishWarmupAction(ctx, *workerID, &act)
		return
	}
	fireAt := time.Now().Add(time.Duration(delaySeconds) * time.Second)
	if err := s.WarmupEngagementRepo.EnqueuePendingEngagement(ctx, e.Message.EmailID, payload, fireAt); err != nil {
		log.Warn().Err(err).Str("email_id", e.Message.EmailID.String()).Msg("Failed to enqueue delayed warmup engagement; publishing immediately")
		s.Publisher.PublishWarmupAction(ctx, *workerID, &act)
	}
}

// recipientProviderDomain best-effort resolves a recipient mailbox's provider
// ("google"/"smtp_imap") and email domain for the per-provider placement
// dimension. Returns empty strings when the account can't be loaded.
func (s *JobsService) recipientProviderDomain(ctx context.Context, accountID uuid.UUID) (string, string) {
	if s.EmailRepository == nil {
		return "", ""
	}
	acc, err := s.EmailRepository.GetByID(ctx, accountID)
	if err != nil || acc == nil {
		return "", ""
	}
	domain := ""
	if at := strings.LastIndex(acc.Email, "@"); at >= 0 {
		domain = strings.ToLower(acc.Email[at+1:])
	}
	return acc.Provider, domain
}

func (s *JobsService) applyInvalidWarmupAttempt(ctx context.Context, accountID uuid.UUID, attemptedToken string, scoreDelta int) {
	if s.WarmupService != nil {
		if health, err := s.WarmupService.ApplyInvalidTokenAttempt(ctx, accountID, attemptedToken, scoreDelta); err == nil {
			s.markRiskBandFromWarmupHealth(ctx, accountID, health)
			return
		}
	}

	if s.WarmupRepo == nil {
		return
	}

	// Degraded mode (no warmup service): record the raw signal only. All
	// blocking is owned by the banded health model (evaluateMetrics), which
	// already enforces the invalid-token threshold with a blocked_until and an
	// appeal path. The old checkAndAutoBlock issued permanent blocks
	// (blocked_until = NULL) that UpdateParticipantHealth then refused to ever
	// re-evaluate — a divergent dead-end that is now removed.
	_ = s.WarmupRepo.RecordInvalidTokenAttempt(ctx, accountID, attemptedToken)
	if scoreDelta > 0 {
		_, _ = s.WarmupRepo.IncrementSpamScore(ctx, accountID, scoreDelta)
	}
	s.markRiskBandFromWarmupHealth(ctx, accountID, nil)
}

// containsSpamFlag checks if any flag is a spam flag
func containsSpamFlag(flags []string) bool {
	spamFlags := []string{"\\Junk", "\\Spam", "SPAM", "Junk"}
	for _, f := range flags {
		if slices.Contains(spamFlags, f) {
			return true
		}
	}
	return false
}
