package sequence

import (
	"context"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func (s *sequenceService) Create(ctx context.Context, userID, campaignID string) (*models.Sequence, *errx.Error) {
	return s.sequenceRepository.Create(ctx, userID, campaignID)
}

func (s *sequenceService) Get(ctx context.Context, userID, campaignID string) ([]models.Sequence, *errx.Error) {
	return s.sequenceRepository.Get(ctx, userID, campaignID)
}

func (s *sequenceService) Update(ctx context.Context, userID, campaignID, sequenceID string, data *models.UpdateSequence) (*models.Sequence, *errx.Error) {
	// Branch routing is resolved (and made safe against deleted/dangling targets
	// and loops) at schedule time in the repository's finder; the repository also
	// validates branch shape before persisting. No cross-step write validation is
	// needed here — the canvas only ever points a branch at a real step or stop.
	return s.sequenceRepository.Update(ctx, userID, campaignID, sequenceID, data)
}

func (s *sequenceService) Delete(ctx context.Context, userID, campaignID, sequenceID string) *errx.Error {
	return s.sequenceRepository.Delete(ctx, userID, campaignID, sequenceID)
}
