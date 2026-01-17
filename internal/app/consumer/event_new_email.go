package jobs

import (
	"context"

	"github.com/warmbly/warmbly/internal/models"
)

func (s *JobsService) HandleNewEmail(ctx context.Context, e *models.JobEventNewEmail) error {
	if err := s.UniboxRepository.CreateEntry(ctx, e.UserID, e.Message); err != nil {
		CaptureError(e.UserID, e.Message.EmailID, err)
		return err
	}

	return nil
}
