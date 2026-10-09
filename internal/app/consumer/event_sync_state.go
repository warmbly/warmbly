package jobs

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
)

// HandleSyncState persists the worker's SYNC_STATE relay and tells the
// dashboard when something a person would notice changed: the import
// finished, or fair use started or stopped holding the mailbox. Progress
// ticks are persisted silently; the mailbox drawer polls them while an
// import is running.
func (s *JobsService) HandleSyncState(ctx context.Context, e *models.JobEventSyncState) error {
	if e == nil || s.EmailSyncStateRepository == nil {
		return nil
	}

	var prev *models.SyncState
	if saved, err := s.EmailSyncStateRepository.Get(ctx, e.EmailID); err == nil {
		prev = saved
	}

	if err := s.EmailSyncStateRepository.Put(ctx, e.UserID, e.EmailID, &e.State); err != nil {
		if s.dropForDeletedMailbox(ctx, e.UserID, e.EmailID, err) {
			return nil
		}
		CaptureError(e.UserID, e.EmailID, err)
		return err
	}

	// Progress relays are not recovery evidence; require a newer successful check.
	if e.State.LastSyncedAt != nil && e.State.Deferred == 0 && e.State.BackfillCursor.GoogleRecovery == nil &&
		(prev == nil || prev.LastSyncedAt == nil || e.State.LastSyncedAt.After(*prev.LastSyncedAt)) {
		s.resolveTransientMailErrors(ctx, e.EmailID, e.State.LastSyncedAt)
	}

	if s.StreamingPublisher == nil {
		return nil
	}
	completed := e.State.BackfillStatus == models.SyncBackfillComplete &&
		(prev == nil || prev.BackfillStatus != models.SyncBackfillComplete)
	throttleFlipped := (e.State.ThrottledUntil != nil) != (prev != nil && prev.ThrottledUntil != nil)
	if !completed && !throttleFlipped {
		return nil
	}

	account, xerr := s.EmailRepository.GetByID(ctx, e.EmailID)
	if xerr != nil || account == nil {
		return nil
	}
	orgID := ""
	if account.OrganizationID != nil {
		orgID = account.OrganizationID.String()
	}
	reason := ""
	if e.State.ThrottledUntil != nil {
		reason = e.State.ThrottleReason
	}
	s.StreamingPublisher.PublishAccountEvent(ctx, &pubsub.AccountEvent{
		BaseEvent: pubsub.BaseEvent{
			EventType: pubsub.EventAccountSyncState,
			UserID:    e.UserID.String(),
		},
		OrgID:          orgID,
		EmailAccountID: e.EmailID.String(),
		Email:          account.Email,
		Provider:       account.Provider,
		Status:         string(e.State.BackfillStatus),
		Reason:         reason,
	})

	if throttleFlipped && e.State.ThrottledUntil != nil {
		log.Info().
			Str("email_id", e.EmailID.String()).
			Str("reason", e.State.ThrottleReason).
			Time("until", *e.State.ThrottledUntil).
			Msg("mailbox sync held by fair use")
		s.StreamingPublisher.PublishEmailWarning(
			ctx,
			e.UserID.String(),
			e.EmailID,
			"Mailbox sync paused by fair use",
			"New mail for "+account.Email+" is waiting on the sync budget. Replies to your outreach keep syncing; the rest resumes automatically.",
		)
	}
	return nil
}

// transientMailErrorCodes are the errors a completed sync pass disproves.
// Anything that needs the user to act (credentials, domain authentication,
// fair-use deactivation) is deliberately absent: those stay until the person
// fixes them or reconnects the mailbox.
var transientMailErrorCodes = []string{
	string(errx.MailErrorCodeServerUnreachable),
	string(errx.MailErrorCodeConnectionLost),
	string(errx.MailErrorCodeNotFound),
	// A NO/BAD the client has no dedicated error for. It is relayed as a
	// temporary server error and never deactivates the mailbox, so a pass
	// that completed afterwards disproves it like any other outage.
	string(errx.MailErrorCodeImapUnknown),
}

func (s *JobsService) resolveTransientMailErrors(ctx context.Context, emailID uuid.UUID, syncedAt *time.Time) {
	if s.EmailAccountErrorRepository == nil {
		return
	}
	// Only errors raised before this pass ran. The bus can redeliver an older
	// event after a newer one, and without the bound a stale success would
	// clear a failure that happened after it, leaving the mailbox looking
	// healthy while it was not.
	if syncedAt == nil {
		return
	}
	if err := s.EmailAccountErrorRepository.ResolveByCodesBefore(ctx, emailID, transientMailErrorCodes, *syncedAt, "sync recovered"); err != nil {
		log.Warn().Str("error", err.Message).Str("email_id", emailID.String()).
			Msg("could not clear the mailbox's transient errors after a successful sync")
	}
}
