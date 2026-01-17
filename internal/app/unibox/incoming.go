package unibox

import (
	"context"
	"strconv"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func (s *uniboxService) Incoming(
	ctx context.Context,
	userID uuid.UUID,
	limit, cursor, from string,
) (*models.MailSearchResult, *errx.Error) {
	l, err := strconv.Atoi(limit)
	if err != nil {
		if limit != "" {
			return nil, errx.ErrUniboxLimit
		}
		l = DefaultLimit
	}

	if l < LimitMin || l > LimitMax {
		return nil, errx.ErrUniboxLimit
	}

	var resp *models.MailSearchResult

	if from != "" {
		resp, err = s.uniboxRepository.GetBySender(ctx, userID, from, l, cursor)
	} else {
		resp, err = s.uniboxRepository.GetIncoming(ctx, userID, l, cursor)
	}

	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}

	return resp, nil
}
