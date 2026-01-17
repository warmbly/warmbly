package unibox

import (
	"context"
	"strconv"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func (s *uniboxService) GetByThread(
	ctx context.Context,
	userID, emailID uuid.UUID,
	threadID, limit, cursor string,
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

	resp, err := s.uniboxRepository.GetByThread(ctx, userID, emailID, threadID, l, cursor)

	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}

	return resp, nil
}
