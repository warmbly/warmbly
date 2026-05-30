package jobs

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
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

	// Advanced reply-intent automation is best-effort and should not block inbox ingest.
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

	// If the warmup mail arrived in a Junk/Spam state, record a
	// spam_placement event against the sender. This is distinct from a
	// user_complaint (which fires later via HandleFlagsAdd when a recipient
	// flags an already-delivered message) because nobody actively rejected
	// it — the provider classifier placed it there on arrival.
	if containsSpamFlag(e.Message.Flags) && s.WarmupService != nil {
		_, _ = s.WarmupService.RecordSpamPlacement(ctx, e.Message.EmailID, token.SenderAccountID, e.Message.MessageID)
	}

	// Perform warmup actions
	s.performWarmupActions(ctx, e)
	return true, nil
}

// performWarmupActions publishes warmup action events to the worker
func (s *JobsService) performWarmupActions(ctx context.Context, e *models.JobEventNewEmail) {
	if s.Publisher == nil {
		return
	}

	action := &models.WarmupEmailAction{
		UserID:             e.UserID,
		EmailID:            e.Message.EmailID,
		GmailID:            e.Message.GmailID,
		UID:                e.Message.UID,
		MailboxUIDValidity: e.Message.Mailbox,
		Actions:            []string{"move_to_warmbly", "mark_read", "remove_from_spam", "mark_important"},
	}

	// Look up the worker ID from the email account
	if s.EmailRepository != nil {
		account, xerr := s.EmailRepository.GetByID(ctx, e.Message.EmailID)
		if xerr == nil && account != nil && account.WorkerID != nil {
			s.Publisher.PublishWarmupAction(ctx, *account.WorkerID, action)
		}
	}
}

func (s *JobsService) applyInvalidWarmupAttempt(ctx context.Context, accountID uuid.UUID, attemptedToken string, scoreDelta int) {
	if s.WarmupService != nil {
		if _, err := s.WarmupService.ApplyInvalidTokenAttempt(ctx, accountID, attemptedToken, scoreDelta); err == nil {
			return
		}
	}

	if s.WarmupRepo == nil {
		return
	}

	_ = s.WarmupRepo.RecordInvalidTokenAttempt(ctx, accountID, attemptedToken)
	if scoreDelta > 0 {
		_, _ = s.WarmupRepo.IncrementSpamScore(ctx, accountID, scoreDelta)
	}

	s.checkAndAutoBlock(ctx, accountID)
}

// checkAndAutoBlock checks if an account should be auto-blocked based on invalid token attempts or spam score
func (s *JobsService) checkAndAutoBlock(ctx context.Context, accountID uuid.UUID) {
	if s.WarmupRepo == nil {
		return
	}

	since := time.Now().Add(-24 * time.Hour)
	attempts, _ := s.WarmupRepo.CountRecentInvalidAttempts(ctx, accountID, since)
	if attempts >= 3 {
		_ = s.WarmupRepo.BlockFromPool(ctx, accountID,
			fmt.Sprintf("Auto-blocked: %d invalid warmup token attempts in 24h", attempts))
		return
	}

	score, _ := s.WarmupRepo.GetSpamScore(ctx, accountID)
	if score > 50 {
		_ = s.WarmupRepo.BlockFromPool(ctx, accountID,
			fmt.Sprintf("Auto-blocked: spam score %d exceeds threshold", score))
	}
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
