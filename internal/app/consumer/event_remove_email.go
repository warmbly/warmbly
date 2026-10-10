package jobs

import (
	"context"
	"errors"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// HandleRemoveEmail processes a message removal observed during mailbox sync.
//
// Tampering protection: if the removed message was a warmup email (tracked in
// warmup_received) and it went soon after it arrived, the worker is asked
// where it went (checkWarmupRemoval). Only a message found in the trash or
// gone for good is a strike; one moved to any other folder is still in the
// mailbox. A removal later than the window is housekeeping (see
// warmupDeletionCounts) and is not held against anyone.
//
// It drops the local unibox entry only after durable arrival replay is complete.
func (s *JobsService) HandleRemoveEmail(ctx context.Context, e *models.JobEventRemoveEmail) error {
	if s.ArrivalOutbox != nil {
		pending, err := s.ArrivalOutbox.HasPendingArrival(ctx, e.UserID, e.EmailID, e.ID)
		if err != nil {
			return err
		}
		if pending {
			return &syncArrivalPendingError{user: e.UserID, email: e.EmailID, id: e.ID}
		}
	}
	var checkErr error
	// A message the sync found in a folder the owner excluded is filed, not
	// deleted: it is still in the mailbox, so nothing is held against anyone.
	if s.WarmupRepo != nil && e.SkippedFolder == "" {
		if rec, _ := s.WarmupRepo.GetWarmupReceived(ctx, e.EmailID, e.ID); rec != nil {
			switch {
			case s.consumeSelfMove(ctx, e.EmailID, rec.MessageID):
				// Our own engagement foldered this message. Providers that
				// report a move as a removal (Graph) would otherwise ban the
				// recipient for the action we asked it to perform.
				log.Debug().
					Str("email_id", e.EmailID.String()).
					Str("message_id", rec.MessageID).
					Msg("Warmup message left its folder because we moved it; not tampering")
			case !warmupDeletionCounts(rec, time.Now()):
				log.Debug().
					Str("email_id", e.EmailID.String()).
					Str("message_id", rec.MessageID).
					Msg("Warmup message removed after its engagement window; housekeeping, not tampering")
			default:
				checkErr = s.checkWarmupRemoval(ctx, e.UserID, e.EmailID, rec.MessageID)
			}
		}
	}

	if s.UniboxRepository != nil {
		if err := s.UniboxRepository.Delete(ctx, e.UserID, e.ID); err != nil {
			return errors.Join(checkErr, err)
		}
	}

	s.publishInboxDeleted(ctx, e.UserID, e.EmailID, e.ID.String())
	return checkErr
}

// warmupDeletionCounts decides whether a deletion of a received warmup message
// is tampering. It is when the message is still fresh: the engagement legs
// run inside the first hours, and removing the mail before then costs the
// pool the signal it was sent for. Past config.WarmupDeletionStrikeHours the
// platform's own retention is going to delete it anyway, so an owner tidying
// the folder, Gmail purging its Trash or a server retention rule is doing the
// platform's job early, not harm. A message the retention sweep has already
// retired is the platform's own deletion whenever it is observed.
func warmupDeletionCounts(rec *repository.WarmupReceived, now time.Time) bool {
	if rec == nil || rec.RetiredAt != nil {
		return false
	}
	return now.Sub(rec.CreatedAt) < time.Duration(config.WarmupDeletionStrikeHours)*time.Hour
}
