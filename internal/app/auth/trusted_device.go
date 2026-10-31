package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"strings"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/crypt"
)

// A trusted device is a browser the account owner asked to remember after
// entering an emailed login code. The browser keeps a random 256-bit token and
// the server keeps only its SHA-256, bound to the user, so a token is useless
// for any other account and a leaked cache reveals nothing replayable.
//
// It skips the emailed code and nothing else: the password, the captcha, TOTP
// and the sign-in risk assessment all still apply. The window is absolute,
// never extended by use, and every trusted device is forgotten when the
// password changes or the user signs out everywhere.

func getTrustedDeviceKey(userID uuid.UUID, tokenHash string) string {
	return getTrustedDevicePrefix(userID) + tokenHash
}

func getTrustedDevicePrefix(userID uuid.UUID) string {
	return "trusted_device:" + userID.String() + ":"
}

// trustDevice mints a token for this browser. An empty result means the
// device could not be remembered, which only costs the user a code next time.
func (s *authService) trustDevice(ctx context.Context, userID uuid.UUID) string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		errs.CaptureException(err)
		return ""
	}
	tok := base64.RawURLEncoding.EncodeToString(b)
	if err := s.cache.SetEx(ctx, getTrustedDeviceKey(userID, crypt.SHA256(tok)), "1", TrustedDeviceTTL).Err(); err != nil {
		errs.CaptureExceptionContext(ctx, err, errs.Tag("area", "trusted_device"))
		return ""
	}
	return tok
}

// isTrustedDevice reports whether token was issued to this user and is still
// inside its window. A cache error fails closed into "send a code".
func (s *authService) isTrustedDevice(ctx context.Context, userID uuid.UUID, token string) bool {
	token = strings.TrimSpace(token)
	if token == "" || len(token) > 128 || s.cache == nil {
		return false
	}
	n, err := s.cache.Exists(ctx, getTrustedDeviceKey(userID, crypt.SHA256(token))).Result()
	return err == nil && n > 0
}

// ForgetTrustedDevices drops every remembered device for the user, so the next
// sign-in from each of them asks for an emailed code again. An error means
// some may survive, so callers must not report the revocation as done.
func (s *authService) ForgetTrustedDevices(ctx context.Context, userID uuid.UUID) *errx.Error {
	if s.cache == nil {
		return nil
	}
	iter := s.cache.Scan(ctx, 0, getTrustedDevicePrefix(userID)+"*", 100).Iterator()
	var keys []string
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		errs.CaptureExceptionContext(ctx, err, errs.Tag("area", "trusted_device"))
		return errx.InternalError()
	}
	if len(keys) == 0 {
		return nil
	}
	if err := s.cache.Del(ctx, keys...).Err(); err != nil {
		errs.CaptureExceptionContext(ctx, err, errs.Tag("area", "trusted_device"))
		return errx.InternalError()
	}
	return nil
}

// ForgetTrustedDevice drops the given tokens' devices for this user; a token
// issued to another account names no key here and changes nothing.
func (s *authService) ForgetTrustedDevice(ctx context.Context, userID uuid.UUID, tokens []string) *errx.Error {
	if s.cache == nil {
		return nil
	}
	var keys []string
	for _, tok := range tokens {
		if tok = strings.TrimSpace(tok); tok != "" && len(tok) <= 128 {
			keys = append(keys, getTrustedDeviceKey(userID, crypt.SHA256(tok)))
		}
	}
	if len(keys) == 0 {
		return nil
	}
	if err := s.cache.Del(ctx, keys...).Err(); err != nil {
		errs.CaptureExceptionContext(ctx, err, errs.Tag("area", "trusted_device"))
		return errx.InternalError()
	}
	return nil
}
