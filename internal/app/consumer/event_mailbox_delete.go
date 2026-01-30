package jobs

import (
	"context"

	"github.com/warmbly/warmbly/internal/models"
)

func (s *JobsService) HandleMailboxDelete(ctx context.Context, e *models.JobEventMailboxDelete) error {
	if err := s.MailboxRepository.DeleteMailbox(
		ctx,
		e.UserID,
		e.EmailID,
		e.UIDValidity,
	); err != nil {
		CaptureError(e.UserID, e.EmailID, err)
		return err
	}

	return nil
}
