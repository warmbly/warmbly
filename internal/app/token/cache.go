package token

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
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

func (s *tokenService) getSession(ctx context.Context, sessionID uuid.UUID) (*models.Session, *errx.Error) {
	data, err := s.cache.Get(ctx, getSessionKey(sessionID)).Bytes()
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			// A cache that cannot answer is a miss, not a failed request. The
			// session lives in Postgres and the caller reads it from there;
			// treating an unreachable Redis as an auth failure turned a cache
			// outage into nobody being able to sign in at all.
			//
			// Safe on the revocation path too: falling through reads the
			// authoritative row rather than a cached copy of it.
			errs.CaptureException(err)
		}
		return nil, nil
	}

	var session models.Session
	if err := json.Unmarshal(data, &session); err != nil {
		// Unreadable cached bytes are a miss for the same reason.
		errs.CaptureException(err)
		return nil, nil
	}

	return &session, nil
}

func (s *tokenService) deleteSession(ctx context.Context, sessionID uuid.UUID) *errx.Error {
	if err := s.cache.Del(ctx, getSessionKey(sessionID)).Err(); err != nil {
		errs.CaptureException(err)
		return errx.InternalError()
	}

	return nil
}
