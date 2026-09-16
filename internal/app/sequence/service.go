package sequence

import (
	"context"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/storage"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type SequenceService interface {
	Create(ctx context.Context, orgID, campaignID string) (*models.Sequence, *errx.Error)
	Get(ctx context.Context, orgID, campaignID string) ([]models.Sequence, *errx.Error)
	Update(ctx context.Context, orgID, campaignID, sequenceID string, data *models.UpdateSequence) (*models.Sequence, *errx.Error)
	UpdateLayout(ctx context.Context, orgID, campaignID string, positions []models.SequencePosition) *errx.Error
	Delete(ctx context.Context, orgID, campaignID, sequenceID string) *errx.Error
}

type sequenceService struct {
	sequenceRepository repository.SequenceRepository
	attachmentRepo     repository.AttachmentRepository
	storage            storage.Store
}

func NewService(sequenceRepository repository.SequenceRepository) SequenceService {
	return &sequenceService{
		sequenceRepository: sequenceRepository,
	}
}

// AttachmentAware is implemented by the sequence service so main can hand it
// the attachment repository and object store. Deleting a step cascades its
// attachment rows away, so without these the files scoped to that step leave
// their bytes in storage forever, still counted against the org's quota.
type AttachmentAware interface {
	WireAttachments(repo repository.AttachmentRepository, store storage.Store)
}

func (s *sequenceService) WireAttachments(repo repository.AttachmentRepository, store storage.Store) {
	s.attachmentRepo, s.storage = repo, store
}
