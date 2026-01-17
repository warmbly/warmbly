package jobs

import (
	"context"

	"github.com/warmbly/warmbly/internal/models"
)

func (s *JobsService) HandleMailboxUpdate(ctx context.Context, e *models.JobEventMailboxUpdate) error {
	if err := s.MailboxRepository.CreateEntry(
		ctx,
		e.UserID,
		e.EmailID,
		e.Data,
	); err != nil { // replaces the data if it already exists
		CaptureError(e.UserID, e.EmailID, err)
		return err
	}

	return nil
}
