package unibox

import (
	"context"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func (s *uniboxService) MarkSeen(ctx context.Context, userID, emailID uuid.UUID, seen bool) *errx.Error {
	if err := s.uniboxRepository.MarkSeen(ctx, userID, emailID, seen); err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	return nil
}

func (s *uniboxService) MarkSeenBulk(ctx context.Context, orgID uuid.UUID, data *models.MarkSeen) (*models.MarkSeen, *errx.Error) {
	if len(data.EmailIDs) > 500 {
		return nil, errx.ErrSeenMax
	}

	// A folder sweep and an id list are different requests; refuse the
	// ambiguous combination instead of guessing which one was meant.
	if data.Folder != "" {
		if len(data.EmailIDs) > 0 {
			return nil, errx.ErrSeenFolderAndIDs
		}
		if !models.ValidFolder(data.Folder) {
			return nil, errx.ErrUniboxFolder
		}
		if err := s.uniboxRepository.MarkSeenByFolder(ctx, orgID, data.Folder, data.Seen); err != nil {
			sentry.CaptureException(err)
			return nil, errx.InternalError()
		}
		return data, nil
	}

	if err := s.uniboxRepository.MarkSeenBulk(ctx, orgID, data.EmailIDs, data.Seen); err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}

	return data, nil
}
