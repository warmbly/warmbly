package auth

import (
	"context"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/notify/templates"
	"github.com/warmbly/warmbly/internal/pkg/argon2"
	"github.com/warmbly/warmbly/internal/pkg/crypt"
)

func (s *authService) RegistrationStart(ctx context.Context, data *AuthData, ipaddr string) (*models.AuthSession, *errx.Error) {
	if s.policy.DisablePasswordLogin {
		return nil, errx.New(errx.Forbidden, "password sign-up is disabled on this deployment")
	}

	if err := s.signupAllowed(ctx, data.Email, data.Invite); err != nil {
		return nil, err
	}

	if xerr := s.captcha.Verify(ctx, data.Turnstile, ipaddr); xerr != nil {
		sentry.CaptureException(xerr)
		return nil, xerr
	}

	if !crypt.ValidatePassword(data.Password) {
		return nil, errx.ErrPassword
	}

	passwordHash, xerr := argon2.Hash(data.Password)
	if xerr != nil {
		sentry.CaptureException(xerr)
		return nil, errx.InternalError()
	}

	// With verification off, or with a transport that cannot deliver, there is
	// nothing to confirm: create the account now rather than issuing a code
	// nobody can receive. Every product surveyed defaults self-host to this.
	if !s.policy.RequireEmailVerification || !s.mailDelivers {
		if err := s.createAccount(ctx, data.Email, passwordHash, data.ReferralCode, data.Invite); err != nil {
			return nil, err
		}
		return &models.AuthSession{CodeRequired: false}, nil
	}

	if err := s.canSendEmail(ctx, emailFlowRegistration, data.Email); err != nil {
		return nil, err
	}

	issuedAt := time.Now()
	expiresAt := issuedAt.Add(AuthSessionTTL)
	sessionID := uuid.New()
	nonce, xerr := crypt.Nonce()
	if xerr != nil {
		sentry.CaptureException(xerr)
		return nil, errx.InternalError()
	}

	code, xerr := crypt.VerificationCode()
	if xerr != nil {
		sentry.CaptureException(xerr)
		return nil, errx.InternalError()
	}

	text, xerr := templates.GenerateRegistrationCodeHTML(code)
	if xerr != nil {
		sentry.CaptureException(xerr)
		return nil, errx.InternalError()
	}

	if xerr := s.sendAuthEmail(ctx, data.Email, "Your Verification Code", text); xerr != nil {
		sentry.CaptureException(xerr)
		return nil, errx.ErrMailUndeliverable
	}

	codeHash, xerr := argon2.Hash(code)
	if xerr != nil {
		sentry.CaptureException(xerr)
		return nil, errx.InternalError()
	}

	session := &models.RegistrationSession{
		CodeHash:     codeHash,
		PasswordHash: passwordHash,
		Nonce:        nonce,
		ReferralCode: data.ReferralCode,
		Invite:       data.Invite,
	}

	if err := s.saveRegistrationSession(ctx, sessionID, session, expiresAt); err != nil {
		return nil, err
	}

	sessionToken, xerr := s.tokenService.GenerateToken(uuid.Nil, sessionID, data.Email, nonce, issuedAt, expiresAt)
	if xerr != nil {
		sentry.CaptureException(xerr)
		return nil, errx.InternalError()
	}

	return &models.AuthSession{
		Session:      sessionToken,
		CodeRequired: true,
	}, nil
}

func (s *authService) RegistrationConfirm(ctx context.Context, data *ConfirmData, session, ipaddr string) *errx.Error {
	token, err := s.tokenService.VerifyToken(session)
	if err != nil {
		return err
	}
	if token.ExpiresAt.Before(time.Now()) {
		return errx.ErrSession
	}
	sess, err := s.getRegistrationSession(ctx, token.SessionID)
	if err != nil {
		return err
	}
	if sess == nil || sess.Nonce != token.Nonce {
		return errx.ErrSession
	}

	if sess.Tries >= AuthAttempts {
		return errx.ErrCodeLimit
	}

	v, xerr := argon2.Verify(data.Code, sess.CodeHash)
	if xerr != nil {
		sentry.CaptureException(xerr)
		return errx.InternalError()
	}

	if !v {
		sess.Tries++
		_ = s.saveRegistrationSession(ctx, token.SessionID, sess, token.ExpiresAt.Time)
		return errx.ErrCode
	}

	// Re-check the policy: a session minted while signups were open must not
	// outlive a lockdown applied before the code came back.
	if err := s.signupAllowed(ctx, token.Email, sess.Invite); err != nil {
		return err
	}

	return s.createAccount(ctx, token.Email, sess.PasswordHash, sess.ReferralCode, sess.Invite)
}
