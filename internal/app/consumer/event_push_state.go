package jobs

import (
	"context"

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
	return nil
}
