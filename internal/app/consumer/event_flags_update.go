package jobs

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func (s *JobsService) HandleFlagsAdd(ctx context.Context, e *models.JobEventFlags) error {
	// Tampering check first: verified warmup mail is NOT in the unibox, so we
	// detect it via the warmup_received record. A spam label names no actor on
	// any provider: on mail that arrived in spam it is the filter's own, and
	// any other move is held and attributed on the evidence around it
	// (attributeSpamMove) before anyone is charged.
	if s.WarmupRepo != nil {
		if rec, _ := s.WarmupRepo.GetWarmupReceived(ctx, e.EmailID, e.ID); rec != nil {
			switch {
			case s.WarmupService == nil:
			case containsSpamFlag(e.Flags) && (rec.LandedSpam || rec.MessageID == ""):
			case containsSpamFlag(e.Flags):
				if _, err := s.WarmupRepo.RecordWarmupSpamMove(ctx, repository.WarmupSpamMove{
					EmailAccountID: e.EmailID, MessageID: rec.MessageID,
					SenderAccountID: rec.SenderAccountID, ReceivedAt: rec.CreatedAt,
				}); err != nil {
					return fmt.Errorf("hold warmup spam move: %w", err)
				}
			}
			return nil
		}
	}

	email, err := s.emailForSyncUpdate(ctx, e.UserID, e.ID, func(message *models.EmailMessageStoreData) {
		for _, flag := range e.Flags {
			if !slices.Contains(message.Flags, flag) {
				message.Flags = append(message.Flags, flag)
			}
		}
		if models.SeenFromFlags(e.Flags) {
			message.Seen = true
		}
		if containsSpamFlag(e.Flags) && message.Folder != models.FolderTrash {
			message.Folder = models.FolderSpam
		}
	})
	if err != nil {
		CaptureError(e.UserID, e.EmailID, fmt.Errorf("Email (%s): %w", e.ID.String(), err))
		return err
	}
	if email == nil {
		return nil
	}

	// Check if a warmup email is being flagged as spam
	if s.WarmupRepo != nil && containsSpamFlag(e.Flags) {
		if tokenStr := warmupTokenFromFlags(email.Flags); tokenStr != "" {
			tokenID, parseErr := uuid.Parse(tokenStr)
			if parseErr == nil {
				token, tokenErr := s.WarmupRepo.FindWarmupToken(ctx, tokenID)
				if tokenErr == nil && token != nil {
					if s.WarmupService != nil {
						health, _ := s.WarmupService.ApplySpamReport(ctx, e.EmailID, token.SenderAccountID, email.MessageID, "user_complaint")
						s.markRiskBandFromWarmupHealth(ctx, token.SenderAccountID, health)
					} else {
						// Degraded mode (no warmup service): record the raw signal
						// so the bands count it whenever they next run. Blocking is
						// owned solely by the banded health model (evaluateMetrics)
						// so every block carries a blocked_until and an appeal path.
						_, _ = s.WarmupRepo.RecordSpamReport(ctx, &repository.SpamReport{
							ID:                uuid.New(),
							ReporterAccountID: e.EmailID,
							ReportedAccountID: token.SenderAccountID,
							MessageID:         email.MessageID,
							ReportType:        "user_complaint",
						})
						s.markRiskBandFromWarmupHealth(ctx, token.SenderAccountID, nil)
					}
				}
			}
		}
	}

	var updated bool

	for i := range e.Flags {
		if !slices.Contains(email.Flags, e.Flags[i]) {
			email.Flags = append(email.Flags, e.Flags[i])
			updated = true
			if e.Flags[i] == models.FlagFlagged {
				s.noteOwnerActivity(ctx, e.EmailID, email.InternalDate)
			}
		}
	}

	// Read state is its own column, so gaining \Seen is a change even when the
	// flag array already carried it. Gmail and Graph report read state this
	// way; without this the unibox would only ever be marked read from inside
	// Warmbly, leaving mail the customer read in their own client unread here.
	update := repository.UpdateUniboxEntry{}
	if models.SeenFromFlags(e.Flags) && !email.Seen {
		seen := true
		update.Seen = &seen
		email.Seen = true
		updated = true
		s.noteOwnerActivity(ctx, e.EmailID, email.InternalDate)
	}

	if !updated {
		return nil
	}

	update.Flags = email.Flags
	// A provider-side junking (Gmail SPAM label, IMAP \Junk) moves the
	// message into the spam folder; trash placement is stronger and kept.
	if containsSpamFlag(e.Flags) && email.Folder != models.FolderTrash && email.Folder != models.FolderSpam {
		folder := models.FolderSpam
		update.Folder = &folder
		email.Folder = folder
	}

	if err := s.UniboxRepository.UpdateEntry(
		ctx,
		e.UserID,
		e.EmailID,
		e.ID,
		&update,
	); err != nil {
		return err
	}

	s.publishEmailUpdated(ctx, e.UserID, email)
	return nil
}

func warmupTokenFromFlags(flags []string) string {
	// Try current header name first, then legacy "X-Warmbly-Token" so messages
	// sent before the header rename continue to verify until they age out.
	prefixes := []string{config.WarmupVerifyHeader + ":", "X-Warmbly-Token:"}
	for _, flag := range flags {
		for _, p := range prefixes {
			if strings.HasPrefix(flag, p) {
				return strings.TrimPrefix(flag, p)
			}
		}
	}
	return ""
}

func (s *JobsService) HandleFlagsRemove(ctx context.Context, e *models.JobEventFlags) error {
	email, err := s.emailForSyncUpdate(ctx, e.UserID, e.ID, func(message *models.EmailMessageStoreData) {
		message.Flags = slices.DeleteFunc(message.Flags, func(flag string) bool { return slices.Contains(e.Flags, flag) })
		if models.SeenFromFlags(e.Flags) {
			message.Seen = false
		}
		if message.Folder == models.FolderSpam && !containsSpamFlag(message.Flags) {
			message.Folder = models.FolderInbox
		}
	})
	if err != nil {
		CaptureError(e.UserID, e.EmailID, fmt.Errorf("Email (%s): %w", e.ID.String(), err))
		return err
	}
	if email == nil {
		return nil
	}

	// Losing \Seen is the provider reporting the message back to unread, and
	// that is the column the inbox reads, not the flag array.
	unread := models.SeenFromFlags(e.Flags) && email.Seen
	if unread || (slices.Contains(e.Flags, models.FlagFlagged) && slices.Contains(email.Flags, models.FlagFlagged)) {
		s.noteOwnerActivity(ctx, e.EmailID, email.InternalDate)
	}

	if len(email.Flags) == 0 && !unread {
		return nil
	}

	// Build a set of flags to remove
	removeSet := make(map[string]struct{}, len(e.Flags))
	for _, f := range e.Flags {
		removeSet[f] = struct{}{}
	}

	// Filter out flags that should be removed
	newFlags := make([]string, 0, len(email.Flags))
	for _, f := range email.Flags {
		if _, toRemove := removeSet[f]; !toRemove {
			newFlags = append(newFlags, f)
		}
	}

	// No change → skip DB update
	if len(newFlags) == len(email.Flags) && !unread {
		return nil
	}

	update := repository.UpdateUniboxEntry{Flags: newFlags}
	if unread {
		seen := false
		update.Seen = &seen
		email.Seen = false
	}
	// Un-junking at the provider (spam label cleared while nothing else
	// still marks it spam) restores the message to the inbox.
	if email.Folder == models.FolderSpam && containsSpamFlag(e.Flags) && !containsSpamFlag(newFlags) {
		folder := models.FolderInbox
		update.Folder = &folder
		email.Folder = folder
	}

	if err := s.UniboxRepository.UpdateEntry(
		ctx,
		e.UserID,
		e.EmailID,
		e.ID,
		&update,
	); err != nil {
		return err
	}

	email.Flags = newFlags
	s.publishEmailUpdated(ctx, e.UserID, email)
	return nil
}

// ownerActivityArrivalGrace is how long after arrival a filter may still be labelling a message.
const ownerActivityArrivalGrace = 2 * time.Minute

// noteOwnerActivity records the owner acting on their own mail at the
// provider. Callers pass only changes our store did not already hold, so a
// change made in Warmbly and echoed back by the sync never counts, and a
// change to mail that only just arrived may be a filter finishing delivery.
func (s *JobsService) noteOwnerActivity(ctx context.Context, accountID uuid.UUID, arrived time.Time) {
	if s.WarmupRepo == nil || arrived.IsZero() || time.Since(arrived) < ownerActivityArrivalGrace {
		return
	}
	if err := s.WarmupRepo.RecordOwnerActivity(ctx, accountID, time.Now()); err != nil {
		log.Warn().Err(err).Str("email_id", accountID.String()).Msg("owner activity not recorded")
	}
}

// containsTrashFlag reports the transition Gmail emits for Delete: the TRASH
// label, passed through untranslated by the worker.
func containsTrashFlag(flags []string) bool {
	for _, f := range flags {
		if f == "TRASH" || f == "\\Trash" {
			return true
		}
	}
	return false
}
