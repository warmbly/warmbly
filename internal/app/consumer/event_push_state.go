package jobs

import (
	"context"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/app/mailpush"
	"github.com/warmbly/warmbly/internal/models"
)

// HandlePushState records the provider push channel a worker holds for a
// mailbox, which the next ADD_EMAIL hands back so the channel is renewed.
func (s *JobsService) HandlePushState(ctx context.Context, e *models.JobEventPushState) error {
	if s.MailboxPushRepository == nil || e == nil {
		return nil
	}
	if err := s.MailboxPushRepository.PutPushState(ctx, e.UserID, e.EmailID, e.State); err != nil {
		CaptureError(e.UserID, e.EmailID, err)
		return err
	}
	// Gmail reaches a mailbox only through this row, so a change that landed before it was stored is synced now.
	if e.State != nil && e.State.Provider == models.MailboxPushGmail && s.Publisher != nil {
		if err := mailpush.NewWaker(s.MailboxPushRepository, s.Publisher, nil).WakeMailboxes(ctx, []uuid.UUID{e.EmailID}, false); err != nil {
			log.Debug().Err(err).Str("email_id", e.EmailID.String()).Msg("stored Gmail channel could not wake its mailbox; the poll covers it")
		}
	}
	return nil
}
