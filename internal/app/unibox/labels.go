package unibox

import (
	"context"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// SetThreadLabels replaces the conversation's label set with the given
// categories. The repository only attaches categories the user actually
// owns, so callers can pass the picker's selection straight through.
func (s *uniboxService) SetThreadLabels(ctx context.Context, userID uuid.UUID, threadID string, categoryIDs []uuid.UUID) ([]models.MiniCategory, *errx.Error) {
	if threadID == "" {
		return nil, errx.New(errx.BadRequest, "thread_id is required")
	}
	labels, err := s.uniboxRepository.SetThreadLabels(ctx, userID, threadID, categoryIDs)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}
	return labels, nil
}

// ListThreadLabels returns the conversation's current labels.
func (s *uniboxService) ListThreadLabels(ctx context.Context, userID uuid.UUID, threadID string) ([]models.MiniCategory, *errx.Error) {
	if threadID == "" {
		return nil, errx.New(errx.BadRequest, "thread_id is required")
	}
	labels, err := s.uniboxRepository.ListThreadLabels(ctx, userID, threadID)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}
	return labels, nil
}
