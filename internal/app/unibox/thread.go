package unibox

import (
	"context"
	"strconv"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// GetByThread returns every message in a thread. limit/cursor are
// optional — clients that want the whole conversation can leave them
// empty and the service returns up to ThreadLimitMax messages.
func (s *uniboxService) GetByThread(
	ctx context.Context,
	userID, emailID uuid.UUID,
	threadID, limit, cursor string,
) (*models.MailSearchResult, *errx.Error) {
	l := DefaultThreadLimit
	if limit != "" {
		parsed, err := strconv.Atoi(limit)
		if err != nil {
			return nil, errx.ErrUniboxLimit
		}
		if parsed < LimitMin || parsed > ThreadLimitMax {
			return nil, errx.ErrUniboxLimit
		}
		l = parsed
	}

	resp, err := s.uniboxRepository.GetByThread(ctx, userID, emailID, threadID, l, cursor)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}

	return resp, nil
}
