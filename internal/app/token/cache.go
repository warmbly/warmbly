package token

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
)

func getSessionKey(id uuid.UUID) string {
	return "session" + ":" + id.String()
}

func (s *tokenService) saveSession(ctx context.Context, session *models.Session, ttl time.Duration) *errx.Error {
	data, err := json.Marshal(session)
	if err != nil {
		errs.CaptureException(err)
		return errx.InternalError()
	}

	if err := s.cache.Set(ctx, getSessionKey(session.ID), data, ttl).Err(); err != nil {
		return errx.InternalError()
	}

	return nil
}

func (s *tokenService) deleteSession(ctx context.Context, sessionID uuid.UUID) *errx.Error {
	if err := s.cache.Del(ctx, getSessionKey(sessionID)).Err(); err != nil {
		errs.CaptureException(err)
		return errx.InternalError()
	}

	return nil
}
