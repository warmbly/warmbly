package sequence

import (
	"context"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type SequenceService interface {
	Create(ctx context.Context, userID, campaignID string) (*models.Sequence, *errx.Error)
	Get(ctx context.Context, userID, campaignID string) ([]models.Sequence, *errx.Error)
	Update(ctx context.Context, userID, campaignID, sequenceID string, data *models.UpdateSequence) (*models.Sequence, *errx.Error)
	UpdateLayout(ctx context.Context, userID, campaignID string, positions []models.SequencePosition) *errx.Error
	Delete(ctx context.Context, userID, campaignID, sequenceID string) *errx.Error
}

type sequenceService struct {
	sequenceRepository repository.SequenceRepository
}

func NewService(sequenceRepository repository.SequenceRepository) SequenceService {
	return &sequenceService{
		sequenceRepository: sequenceRepository,
	}
}
