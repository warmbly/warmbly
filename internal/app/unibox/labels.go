package unibox

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
	return s.ListThreadLabelsWithin(ctx, orgID, threadID, nil)
}

// ListThreadLabelsWithin hides unknown and inaccessible conversations alike; nil allows every mailbox.
func (s *uniboxService) ListThreadLabelsWithin(ctx context.Context, orgID uuid.UUID, threadID string, accountIDs []uuid.UUID) ([]models.MiniCategory, *errx.Error) {
	if threadID == "" {
		return nil, errx.New(errx.BadRequest, "thread_id is required")
	}
	labels, err := s.uniboxRepository.ListThreadLabelsWithin(ctx, orgID, threadID, accountIDs)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errx.New(errx.NotFound, "thread not found")
	}
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	return labels, nil
}

func (s *uniboxService) CategoriesForMailboxes(ctx context.Context, orgID uuid.UUID, accountIDs []uuid.UUID) ([]models.Group, *errx.Error) {
	groups, err := s.uniboxRepository.CategoriesForMailboxes(ctx, orgID, accountIDs)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	return groups, nil
}
