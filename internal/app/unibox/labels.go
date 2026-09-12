package unibox

import (
	"context"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
)

// SetThreadLabels replaces the conversation's label set with the given
// categories. The repository only attaches categories the workspace actually
// owns, so callers can pass the picker's selection straight through; userID
// records who filed it.
func (s *uniboxService) SetThreadLabels(ctx context.Context, orgID, userID uuid.UUID, threadID string, categoryIDs []uuid.UUID) ([]models.MiniCategory, *errx.Error) {
	if threadID == "" {
		return nil, errx.New(errx.BadRequest, "thread_id is required")
	}
	labels, err := s.uniboxRepository.SetThreadLabels(ctx, orgID, userID, threadID, categoryIDs)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	return labels, nil
}

// ListThreadLabels returns the conversation's current labels.
func (s *uniboxService) ListThreadLabels(ctx context.Context, orgID uuid.UUID, threadID string) ([]models.MiniCategory, *errx.Error) {
	if threadID == "" {
		return nil, errx.New(errx.BadRequest, "thread_id is required")
	}
	labels, err := s.uniboxRepository.ListThreadLabels(ctx, orgID, threadID)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	return labels, nil
}
