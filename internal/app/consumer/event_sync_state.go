package jobs

import (
	"context"

	"github.com/rs/zerolog/log"
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
		CaptureError(e.UserID, e.EmailID, err)
		return err
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
