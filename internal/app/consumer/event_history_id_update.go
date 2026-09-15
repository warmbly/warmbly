package jobs

import (
	"context"

	"github.com/warmbly/warmbly/internal/models"
)

func (s *JobsService) HandleHistoryIDUpdate(ctx context.Context, e *models.JobEventHistoryIDUpdate) error {
	if err := s.EmailHistoryIDRepository.Put(ctx, e.UserID, e.EmailID, uint64(e.HistoryID)); err != nil {
		CaptureError(e.UserID, e.EmailID, err)
		return err
	}

	return nil
}
