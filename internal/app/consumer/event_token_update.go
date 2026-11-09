package jobs

import (
	"context"

	"github.com/warmbly/warmbly/internal/models"
)

func (s *JobsService) HandleTokenUpdate(ctx context.Context, e *models.JobEventTokenUpdate) error {
	if e.SealedTokens != "" {
		return s.EmailRepository.RefreshBoxTokenSealed(ctx, e.EmailID, e.SealedTokens, e.ExpiresAt)
	}
	return s.EmailRepository.RefreshWorkerBoxToken(
		ctx,
		e.EmailID,
		e.AccessToken,
		e.RefreshToken,
		e.ExpiresAt,
	)
}
