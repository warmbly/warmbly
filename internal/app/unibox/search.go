package unibox

import (
	"context"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// Search searches emails with filters
func (s *uniboxService) Search(
	ctx context.Context,
	userID uuid.UUID,
	params *models.MailSearchParams,
) (*models.MailSearchResult, *errx.Error) {
	// Validate page size
	if params.PageSize < LimitMin {
		params.PageSize = DefaultLimit
	}
	if params.PageSize > LimitMax {
		params.PageSize = LimitMax
	}

	// If only sender filter is used, delegate to optimized method
	if params.Sender != nil && *params.Sender != "" &&
		params.Subject == nil && params.Since == nil &&
		params.Until == nil && params.Unseen == nil {
		resp, err := s.uniboxRepository.GetBySender(ctx, userID, *params.Sender, params.PageSize, params.Cursor)
		if err != nil {
			sentry.CaptureException(err)
			return nil, errx.InternalError()
		}
		return resp, nil
	}

	resp, err := s.uniboxRepository.Search(ctx, userID, params)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}

	return resp, nil
}

// GetUnseenCount returns the count of unseen emails
func (s *uniboxService) GetUnseenCount(
	ctx context.Context,
	userID uuid.UUID,
	emailAccountID *uuid.UUID,
) (int64, *errx.Error) {
	count, err := s.uniboxRepository.GetUnseenCount(ctx, userID, emailAccountID)
	if err != nil {
		sentry.CaptureException(err)
		return 0, errx.InternalError()
	}

	return count, nil
}
