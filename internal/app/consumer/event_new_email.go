package jobs

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

func (s *JobsService) HandleNewEmail(ctx context.Context, e *models.JobEventNewEmail) error {
	// Check for warmup token header in message headers
	warmupToken := extractHeaderValue(e.Message, "X-Warmbly-Token")
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

	return nil
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
		s.WarmupRepo.RecordInvalidTokenAttempt(ctx, e.Message.EmailID, tokenStr)
		s.checkAndAutoBlock(ctx, e.Message.EmailID)
		return false, nil // Process as normal email
	}

	token, err := s.WarmupRepo.GetWarmupToken(ctx, tokenUUID)
	if err != nil || token == nil {
		// Token not found/expired → suspicious
		s.WarmupRepo.RecordInvalidTokenAttempt(ctx, e.Message.EmailID, tokenStr)
		s.WarmupRepo.IncrementSpamScore(ctx, e.Message.EmailID, 5)
		s.checkAndAutoBlock(ctx, e.Message.EmailID)
		return false, nil
	}

	// Verify recipient matches
	if token.RecipientAccountID != e.Message.EmailID {
		s.WarmupRepo.RecordInvalidTokenAttempt(ctx, e.Message.EmailID, tokenStr)
		s.checkAndAutoBlock(ctx, e.Message.EmailID)
		return false, nil
	}

	// Valid! Consume the token
	s.WarmupRepo.ConsumeWarmupToken(ctx, tokenUUID)

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
		UserID:  e.UserID,
		EmailID: e.Message.EmailID,
		GmailID: e.Message.GmailID,
		UID:     e.Message.UID,
		Actions: []string{"move_to_warmbly", "mark_read", "remove_from_spam", "mark_important"},
	}

	// Look up the worker ID from the email account
	if s.EmailRepository != nil {
		account, xerr := s.EmailRepository.GetByID(ctx, e.Message.EmailID)
		if xerr == nil && account != nil && account.WorkerID != nil {
			s.Publisher.PublishWarmupAction(ctx, *account.WorkerID, action)
		}
	}
}

// checkAndAutoBlock checks if an account should be auto-blocked based on invalid token attempts or spam score
func (s *JobsService) checkAndAutoBlock(ctx context.Context, accountID uuid.UUID) {
	if s.WarmupRepo == nil {
		return
	}

	// Check invalid token attempts in last 24h
	since := time.Now().Add(-24 * time.Hour)
	attempts, _ := s.WarmupRepo.CountRecentInvalidAttempts(ctx, accountID, since)
	if attempts >= 3 {
		s.WarmupRepo.BlockFromPool(ctx, accountID,
			fmt.Sprintf("Auto-blocked: %d invalid warmup token attempts in 24h", attempts))
		return
	}

	// Check spam score
	score, _ := s.WarmupRepo.GetSpamScore(ctx, accountID)
	if score > 50 {
		s.WarmupRepo.BlockFromPool(ctx, accountID,
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
