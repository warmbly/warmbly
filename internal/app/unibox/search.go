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
	orgID uuid.UUID,
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

	// Everything goes through Search: it collapses messages into one row
	// per thread and attaches per-thread counts + labels. The old
	// sender-only fast path (GetBySender) returned un-collapsed,
	// label-less rows, so it can't serve the stacked list anymore — the
	// sender filter is handled inside Search via params.Sender.
	resp, err := s.uniboxRepository.Search(ctx, orgID, userID, params)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}

	return resp, nil
}

// GetUnseenCount returns the count of unseen emails
func (s *uniboxService) GetUnseenCount(
	ctx context.Context,
	orgID uuid.UUID,
	emailAccountID *uuid.UUID,
) (int64, *errx.Error) {
	count, err := s.uniboxRepository.GetUnseenCount(ctx, orgID, emailAccountID)
	if err != nil {
		sentry.CaptureException(err)
		return 0, errx.InternalError()
	}

	return count, nil
}
