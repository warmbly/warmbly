package token

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func getSessionKey(id uuid.UUID) string {
	return "session" + ":" + id.String()
}

func (s *tokenService) saveSession(ctx context.Context, session *models.Session, ttl time.Duration) *errx.Error {
	data, err := json.Marshal(session)
	if err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	if err := s.cache.Set(ctx, getSessionKey(session.ID), data, ttl).Err(); err != nil {
		return errx.InternalError()
	}

	return nil
}

func (s *tokenService) getSession(ctx context.Context, sessionID uuid.UUID) (*models.Session, *errx.Error) {
	data, err := s.cache.Get(ctx, getSessionKey(sessionID)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}

	var session models.Session
	if err := json.Unmarshal(data, &session); err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}

	return &session, nil
}

func (s *tokenService) deleteSession(ctx context.Context, sessionID uuid.UUID) *errx.Error {
	if err := s.cache.Del(ctx, getSessionKey(sessionID)).Err(); err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	return nil
}
