package auth

import (
	"context"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/notify/templates"
	"github.com/warmbly/warmbly/internal/pkg/argon2"
	"github.com/warmbly/warmbly/internal/pkg/crypt"
)

func (s *authService) ResetPasswordStart(ctx context.Context, data *ResetPasswordStart, ipaddr string) *errx.Error {
	if err := s.captcha.Verify(ctx, data.Turnstile, ipaddr); err != nil {
		sentry.CaptureException(err)
		return err
	}

	// Spend the budget before the lookup, and key it on the submitted address,
	// so an unknown address costs the attacker the same as a known one.
	if err := s.passwordResetLimit(ctx, data.Email); err != nil {
		return err
	}

	user, uerr := s.userRepository.GetUserByEmail(ctx, data.Email)
	if uerr != nil {
		// Unknown address answers 200 like every other. Returning ErrUser here
		// was an enumeration oracle, and because *errx.Error has no Unwrap the
		// errors.Is check never matched, so it answered 500 instead.
		return nil
	}

	u, xerr := s.userService.GetUser(ctx, user.ID)
	if xerr != nil {
		return nil
	}

	sessionID := uuid.New()
	nonce, err := crypt.Nonce()
	if err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	issuedAt := time.Now()
	expiresAt := issuedAt.Add(PasswordResetTTL)

	token, err := s.tokenService.GenerateToken(user.ID, sessionID, data.Email, nonce, issuedAt, expiresAt)
	if err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	if err := s.saveResetPasswordSession(ctx, sessionID, nonce); err != nil {
		return err
	}

	url := config.GetPasswordResetURL(token)

	text, err := templates.GenerateResetPasswordHTML(u.FirstName, url)
	if err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	if err := s.sendAuthEmail(ctx, u.Email, "Password Reset Confirmation", text); err != nil {
		sentry.CaptureException(err)
		return errx.ErrMailUndeliverable
	}

	return nil
}

func (s *authService) ResetPasswordConfirm(ctx context.Context, data *ResetPasswordConfirm, session, ipaddr string) *errx.Error {
	if err := s.captcha.Verify(ctx, data.Turnstile, ipaddr); err != nil {
		sentry.CaptureException(err)
		return err
	}

	sess, err := s.tokenService.VerifyToken(session)
	if err != nil {
		return err
	}

	if sess.ExpiresAt.Before(time.Now()) {
		return errx.ErrToken
	}

	nonce, err := s.getResetPasswordSession(ctx, sess.SessionID)
	if err != nil {
		return err
	}

	if nonce != sess.Nonce {
		return errx.ErrToken
	}

	if err := s.deletePasswordResetSession(ctx, sess.SessionID); err != nil {
		return err
	}

	if !crypt.ValidatePassword(data.Password) {
		return errx.ErrPassword
	}

	passwordHash, hashErr := argon2.Hash(data.Password)
	if hashErr != nil {
		sentry.CaptureException(hashErr)
		return errx.InternalError()
	}

	if err := s.authRepository.ResetPassword(ctx, sess.UserID, passwordHash); err != nil {
		return err
	}

	// A forgotten-password reset means the account may be compromised: evict
	// every existing session (no current device to keep — uuid.Nil matches
	// none, so all are revoked) so a reset always fully cuts off prior access.
	if s.tokenService != nil {
		if err := s.tokenService.RevokeOtherSessions(ctx, sess.UserID, uuid.Nil); err != nil {
			sentry.CaptureException(err)
			// Non-fatal: the password is already reset.
		}
	}

	return nil
}

// ChangePassword updates a logged-in user's password. It verifies the current
// password first (so a hijacked but unattended session can't silently change
// it), rejects OAuth-only accounts, and enforces the password policy.
func (s *authService) ChangePassword(ctx context.Context, userID, currentSessionID uuid.UUID, data *ChangePassword) *errx.Error {
	hash, xerr := s.authRepository.GetPasswordHash(ctx, userID)
	if xerr != nil {
		return xerr
	}
	if hash == "" {
		return errx.New(errx.BadRequest, "this account signs in without a password")
	}

	ok, verr := argon2.Verify(data.CurrentPassword, hash)
	if verr != nil {
		sentry.CaptureException(verr)
		return errx.InternalError()
	}
	if !ok {
		return errx.ErrCredentials
	}

	if !crypt.ValidatePassword(data.NewPassword) {
		return errx.ErrPassword
	}
	if data.NewPassword == data.CurrentPassword {
		return errx.New(errx.BadRequest, "the new password must be different")
	}

	newHash, hashErr := argon2.Hash(data.NewPassword)
	if hashErr != nil {
		sentry.CaptureException(hashErr)
		return errx.InternalError()
	}
	if err := s.authRepository.ResetPassword(ctx, userID, newHash); err != nil {
		return err
	}

	// Changing the password evicts every OTHER signed-in device (the whole
	// point of changing it when a session may be compromised). The current
	// device keeps its session so the user isn't logged out of the action
	// they just performed.
	if s.tokenService != nil && currentSessionID != uuid.Nil {
		if err := s.tokenService.RevokeOtherSessions(ctx, userID, currentSessionID); err != nil {
			sentry.CaptureException(err)
			// Non-fatal: the password is already changed.
		}
	}
	return nil
}
