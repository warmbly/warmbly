package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/mail"
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
	// Drop malformed events rather than dereferencing nil: this handler runs on
	// the shared consumer, so one bad payload would otherwise panic the process
	// and stop every org's event processing.
	if e == nil || e.Message == nil {
		log.Warn().Msg("NEW_EMAIL event without a message body, dropping")
		return nil
	}
	// Check for warmup token header in message headers.
	// Try the current header name first, then the legacy "X-Warmbly-Token"
	// so messages in flight during the rollout continue to verify.
	warmupToken := extractHeaderValue(e.Message, config.WarmupVerifyHeader)
	if warmupToken == "" {
		warmupToken = extractHeaderValue(e.Message, "X-Warmbly-Token")
	}
	// A mailbox Warmbly Cloud warms receives the cloud's tokens: the cloud
	// vouches for those; anything else is ordinary mail this instance cannot score.
	if warmupToken != "" && s.CloudLink != nil && s.CloudLink.IsEnrolled(ctx, e.Message.EmailID) {
		if ok, err := s.CloudLink.VerifyWarmupToken(ctx, e.Message.EmailID, warmupToken); err == nil && ok {
			return nil
		}
		warmupToken = ""
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
	} else if s.handleUnmarkedWarmupEmail(ctx, e) {
		// Warmup whose verify header did not survive delivery. Every Microsoft
		// mailbox sends this way, so without this branch its warmup mail is
		// filed as ordinary inbox mail at every recipient.
		return nil
	}

	// A pool-linked mailbox is warmup-only: everything else is dropped unread.
	if s.PoolLinkRepo != nil {
		if linked, lerr := s.PoolLinkRepo.GetMailboxByAccount(ctx, e.Message.EmailID); lerr == nil && linked != nil {
			log.Debug().Str("email_account_id", e.Message.EmailID.String()).Msg("dropping non-warmup mail for pool-linked mailbox")
			return nil
		}
	}

	// Normal email processing
	if err := s.UniboxRepository.CreateEntry(ctx, e.UserID, e.Message); err != nil {
		CaptureError(e.UserID, e.Message.EmailID, err)
		return err
	}
	if e.Message != nil {
		inbox := s.emailInboxEvent(ctx, e.UserID, e.Message)
		if s.StreamingPublisher != nil {
			s.StreamingPublisher.PublishEmailReceived(ctx, inbox)
		}
		// Fan an opt-in firehose webhook for the arrival (inbox.email_received).
		if s.AdvancedService != nil && inbox.OrgID != "" {
			if orgID, perr := uuid.Parse(inbox.OrgID); perr == nil {
				s.AdvancedService.EmitCampaignEvent(ctx, orgID, models.WebhookEventInboxEmailReceived, map[string]any{
					"email_account_id": inbox.EmailAccountID,
					"message_id":       inbox.MessageID,
					"thread_id":        inbox.ThreadID,
					"subject":          inbox.Subject,
					"from":             inbox.From,
				})
			}
		}
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
	s.StreamingPublisher.PublishEmailUpdated(ctx, s.emailInboxEvent(ctx, userID, message))
}

// emailInboxEvent builds the realtime inbox payload. Org-scoped (best-effort)
// so every teammate's unibox updates live, not just the mailbox owner's.
func (s *JobsService) emailInboxEvent(ctx context.Context, userID uuid.UUID, message *models.EmailMessageStoreData) *pubsub.EmailInboxEvent {
	var orgID string
	if account, err := s.EmailRepository.GetByID(ctx, message.EmailID); err == nil && account != nil && account.OrganizationID != nil {
		orgID = account.OrganizationID.String()
	}
	return &pubsub.EmailInboxEvent{
		BaseEvent:      pubsub.BaseEvent{UserID: userID.String()},
		OrgID:          orgID,
		EmailAccountID: message.EmailID.String(),
		MessageID:      message.ID.String(),
		ThreadID:       message.ThreadID,
		Subject:        message.Subject,
		From:           strings.Join(message.FromAddr, ", "),
		Preview:        message.Snippet,
		Folder:         models.NormalizeFolder(message.Folder, message.Flags),
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

	s.acceptWarmupEmail(ctx, e, token)
	return true, nil
}

// handleUnmarkedWarmupEmail verifies warmup mail that arrived without its
// verify header. Microsoft Graph drops custom headers in transit (and
// re-stamps the Message-ID), so mail sent from an Outlook or Microsoft 365
// mailbox reaches every recipient carrying no marker at all; matched only on
// the header it would count for nobody and be filed as ordinary inbox mail.
//
// Unlike the header path a miss here is not suspicious — almost every message
// that reaches this point is simply ordinary mail — so nothing is recorded as
// an invalid attempt.
func (s *JobsService) handleUnmarkedWarmupEmail(ctx context.Context, e *models.JobEventNewEmail) bool {
	if s.WarmupRepo == nil || e.Message == nil {
		return false
	}
	token, err := s.WarmupRepo.FindDeliveredWarmupToken(
		ctx,
		e.Message.EmailID,
		firstSenderAddress(e.Message.FromAddr),
		e.Message.MessageID,
		e.Message.Subject,
	)
	if err != nil {
		CaptureError(e.UserID, e.Message.EmailID, fmt.Errorf("unmarked warmup lookup: %w", err))
		return false
	}
	if token == nil {
		return false
	}
	log.Debug().
		Str("token", token.Token.String()).
		Str("email_account_id", e.Message.EmailID.String()).
		Msg("verified warmup mail that arrived without its verify header")
	s.acceptWarmupEmail(ctx, e, token)
	return true
}

// firstSenderAddress pulls the bare address out of the first From value
// ("Name <addr>" or a bare address).
func firstSenderAddress(from []string) string {
	for _, raw := range from {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if addr, err := mail.ParseAddress(raw); err == nil {
			return strings.TrimSpace(addr.Address)
		}
		if i := strings.LastIndex(raw, "<"); i >= 0 {
			if j := strings.Index(raw[i:], ">"); j > 0 {
				return strings.TrimSpace(raw[i+1 : i+j])
			}
		}
		if strings.Contains(raw, "@") {
			return raw
		}
	}
	return ""
}

// acceptWarmupEmail consumes a verified token and runs everything that follows
// from a warmup email having arrived. Shared by both verification paths so the
// header and the header-less route cannot drift apart.
func (s *JobsService) acceptWarmupEmail(ctx context.Context, e *models.JobEventNewEmail, token *models.WarmupToken) {
	s.WarmupRepo.ConsumeWarmupToken(ctx, token.Token)

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

	// Occasionally answer the sender, so the thread reads as a conversation.
	s.scheduleWarmupReplyBack(ctx, token, e.Message.EmailID)

	// Perform warmup actions
	s.performWarmupActions(ctx, e)
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
		// Stable key so Graph accounts re-resolve the live message id at action
		// time (Graph ids change on move).
		RFCMessageID: e.Message.MessageID,
	}

	// Resolve the receiving mailbox once (worker routing + timezone for the
	// waking-hours engagement guard).
	var workerID *uuid.UUID
	var recipientTZ string
	if s.EmailRepository != nil {
		if account, xerr := s.EmailRepository.GetByID(ctx, e.Message.EmailID); xerr == nil && account != nil {
			workerID = account.WorkerID
			recipientTZ = account.Timezone
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
		// Mark BEFORE publishing: the move can land, and its removal be
		// observed, before a marker written afterwards would exist.
		if hasAction(immediate, "move_to_warmbly") {
			s.markSelfMove(ctx, e.Message.EmailID, e.Message.MessageID)
		}
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
	fireAt := humanizeFireAt(time.Now().Add(time.Duration(delaySeconds)*time.Second), recipientTZ)
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
