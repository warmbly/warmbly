package auth

import (
	"context"
	"errors"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

	user, err := s.userRepository.GetUserByEmail(ctx, data.Email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errx.ErrUser
		}
		return errx.InternalError()
	}

	u, xerr := s.userService.GetUser(ctx, user.ID)
	if xerr != nil {
		return xerr
	}

	if err := s.passwordResetLimit(ctx, u.Email); err != nil {
		return err
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
		return errx.InternalError()
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

	return nil
}

// ChangePassword updates a logged-in user's password. It verifies the current
// password first (so a hijacked but unattended session can't silently change
// it), rejects OAuth-only accounts, and enforces the password policy.
func (s *authService) ChangePassword(ctx context.Context, userID uuid.UUID, data *ChangePassword) *errx.Error {
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
	return s.authRepository.ResetPassword(ctx, userID, newHash)
}
