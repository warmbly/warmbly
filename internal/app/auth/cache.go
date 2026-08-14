package auth

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/crypt"
)

// Login and registration keep separate send budgets. They used to share one
// key, which let anyone lock a known user out of login for the whole window by
// POSTing /auth/register with that user's address.
func getEmailVerificationKey(flow, email string) string {
	return "email_verification:" + flow + ":" + crypt.SHA256(email)
}

// getKnownDeviceKey remembers a device that has already completed a full login,
// so AUTH_LOGIN_CODE=new_device only challenges genuinely new ones.
func getKnownDeviceKey(userID uuid.UUID, fingerprint string) string {
	return "known_device:" + userID.String() + ":" + fingerprint
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

func (s *authService) canSendEmail(ctx context.Context, flow, email string) *errx.Error {
	key := getEmailVerificationKey(flow, email)

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

// saveResetPasswordSession binds the emailed reset JWT to a server-side nonce.
// The TTL is PasswordResetTTL, the same lifetime the JWT carries and the same
// one the email quotes: it used to be SessionTTL (10 minutes) against a 1-hour
// token and a mail that promised 4 hours.
func (s *authService) saveResetPasswordSession(ctx context.Context, sessionID uuid.UUID, nonce string) *errx.Error {
	if err := s.cache.SetEx(ctx, getResetPasswordSessionKey(sessionID), nonce, PasswordResetTTL).Err(); err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	return nil
}

func (s *authService) getResetPasswordSession(ctx context.Context, sessionID uuid.UUID) (string, *errx.Error) {
	val, err := s.cache.Get(ctx, getResetPasswordSessionKey(sessionID)).Result()
	if err != nil {
		// An expired or already-used link is ordinary, not an internal fault.
		if errors.Is(err, redis.Nil) {
			return "", errx.ErrToken
		}
		sentry.CaptureException(err)
		return "", errx.InternalError()
	}

	return val, nil
}

// rememberDevice marks this device as having completed a full login, so
// AUTH_LOGIN_CODE=new_device stops challenging it.
func (s *authService) rememberDevice(ctx context.Context, userID uuid.UUID, userAgent string) {
	fp := deviceFingerprint(userAgent)
	if fp == "" {
		return
	}
	_ = s.cache.SetEx(ctx, getKnownDeviceKey(userID, fp), "1", KnownDeviceTTL).Err()
}

// isKnownDevice reports whether this device has completed a login inside the
// retention window.
func (s *authService) isKnownDevice(ctx context.Context, userID uuid.UUID, userAgent string) bool {
	fp := deviceFingerprint(userAgent)
	if fp == "" {
		return false
	}
	n, err := s.cache.Exists(ctx, getKnownDeviceKey(userID, fp)).Result()
	return err == nil && n > 0
}

// deviceFingerprint is deliberately weak: a user agent is trivially spoofable,
// so this is a convenience signal for skipping a code on a device the user has
// already used, never an authentication factor. An empty agent yields no
// fingerprint, which fails closed into "new device".
func deviceFingerprint(userAgent string) string {
	if strings.TrimSpace(userAgent) == "" {
		return ""
	}
	return crypt.SHA256(userAgent)
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
