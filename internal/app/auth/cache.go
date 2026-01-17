package auth

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
	"github.com/warmbly/warmbly/internal/pkg/crypt"
)

func getEmailVerificationKey(email string) string {
	return "email_verification:" + crypt.SHA256(email)
}

func getPasswordResetLimitKey(email string) string {
	return "password_reset_limit:" + crypt.SHA256(email)
}

func getLoginSessionKey(sessionID uuid.UUID) string {
	return "login_sess:" + sessionID.String()
}

func getRegistrationSessionKey(sessionID uuid.UUID) string {
	return "registration_sess:" + sessionID.String()
}

func getResetPasswordSessionKey(sessionID uuid.UUID) string {
	return "reset_password:" + sessionID.String()
}

func (s *authService) saveLoginSession(ctx context.Context, sessionID uuid.UUID, session *models.LoginSession, expiresAt time.Time) *errx.Error {
	data, err := json.Marshal(session)
	if err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	if err := s.cache.Set(ctx, getLoginSessionKey(sessionID), data, time.Until(expiresAt)).Err(); err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	return nil
}

func (s *authService) getLoginSession(ctx context.Context, sessionID uuid.UUID) (*models.LoginSession, *errx.Error) {
	data, err := s.cache.Get(ctx, getLoginSessionKey(sessionID)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}

	var session models.LoginSession
	if err := json.Unmarshal(data, &session); err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}

	return &session, nil
}

func (s *authService) saveRegistrationSession(ctx context.Context, sessionID uuid.UUID, session *models.RegistrationSession, expiresAt time.Time) *errx.Error {
	data, err := json.Marshal(session)
	if err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	if err := s.cache.Set(ctx, getRegistrationSessionKey(sessionID), data, time.Until(expiresAt)).Err(); err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	return nil
}

func (s *authService) getRegistrationSession(ctx context.Context, sessionID uuid.UUID) (*models.RegistrationSession, *errx.Error) {
	data, err := s.cache.Get(ctx, getRegistrationSessionKey(sessionID)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}

	var session models.RegistrationSession
	if err := json.Unmarshal(data, &session); err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}

	return &session, nil
}

func (s *authService) canSendEmail(ctx context.Context, email string) *errx.Error {
	key := getEmailVerificationKey(email)

	count, err := s.cache.Incr(ctx, key).Result()
	if err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	if count == 1 {
		if err := s.cache.Expire(ctx, key, AuthEmailTTL).Err(); err != nil {
			sentry.CaptureException(err)
			return errx.InternalError()
		}
	}

	if count > AuthEmailLimit {
		return errx.ErrAuthLimit
	}

	return nil
}

func (s *authService) passwordResetLimit(ctx context.Context, email string) *errx.Error {
	key := getPasswordResetLimitKey(email)

	count, err := s.cache.Incr(ctx, key).Result()
	if err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	if count == 1 {
		if err := s.cache.Expire(ctx, key, PasswordResetLimitTTL).Err(); err != nil {
			sentry.CaptureException(err)
			return errx.InternalError()
		}
	}

	if count > PasswordResetLimit {
		return errx.ErrAuthLimit
	}

	return nil
}

func (s *authService) saveResetPasswordSession(ctx context.Context, sessionID uuid.UUID, nonce string) *errx.Error {
	if err := s.cache.SetEx(ctx, getResetPasswordSessionKey(sessionID), nonce, SessionTTL).Err(); err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	return nil
}

func (s *authService) getResetPasswordSession(ctx context.Context, sessionID uuid.UUID) (string, *errx.Error) {
	val, err := s.cache.Get(ctx, getResetPasswordSessionKey(sessionID)).Result()
	if err != nil {
		sentry.CaptureException(err)
		return "", errx.InternalError()
	}

	return val, nil
}

func (s *authService) deletePasswordResetSession(ctx context.Context, sessionID uuid.UUID) *errx.Error {
	val, err := s.cache.Del(ctx, getResetPasswordSessionKey(sessionID)).Result()
	if err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	if val == 0 {
		return errx.ErrToken
	}

	return nil
}
