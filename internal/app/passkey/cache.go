package passkey

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/getsentry/sentry-go"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/warmbly/warmbly/internal/errx"
)

// Challenge/session data lives in Redis between a Begin and Finish call,
// keyed by the user (registration — already authenticated) or by an opaque
// session id (discoverable login — no user yet). It is single-use and
// short-lived; the challenge itself is the unguessable secret.

func registrationKey(userID uuid.UUID) string {
	return "webauthn_reg:" + userID.String()
}

func loginKey(sessionID uuid.UUID) string {
	return "webauthn_login:" + sessionID.String()
}

func (s *service) saveSession(ctx context.Context, key string, data *webauthn.SessionData) *errx.Error {
	raw, err := json.Marshal(data)
	if err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	if err := s.cache.Set(ctx, key, raw, CeremonyTTL).Err(); err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	return nil
}

// takeSession reads and atomically deletes the stored SessionData, enforcing
// single-use of the challenge (replay protection). A missing key means the
// ceremony expired or was already consumed.
func (s *service) takeSession(ctx context.Context, key string) (*webauthn.SessionData, *errx.Error) {
	raw, err := s.cache.GetDel(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, errx.ErrPasskeySession
		}
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}

	var data webauthn.SessionData
	if err := json.Unmarshal(raw, &data); err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}

	return &data, nil
}
