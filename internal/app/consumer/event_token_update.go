package jobs

import (
	"context"

	"github.com/warmbly/warmbly/internal/models"
)

func (s *JobsService) HandleTokenUpdate(ctx context.Context, e *models.JobEventTokenUpdate) error {
	if err := s.EmailRepository.RefreshBoxToken(
		ctx,
		e.EmailID,
		e.AccessToken,
		e.RefreshToken,
		e.ExpiresAt,
	); err != nil {
		return err
	}

	return nil
}
