package auth

import (
	"context"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/notify/templates"
	"github.com/warmbly/warmbly/internal/pkg/argon2"
	"github.com/warmbly/warmbly/internal/pkg/crypt"
)

func (s *authService) LoginStart(ctx context.Context, data *AuthData, ipaddr string) (*models.AuthSession, *errx.Error) {
	if xerr := s.captcha.Verify(ctx, data.Turnstile, ipaddr); xerr != nil {
		sentry.CaptureException(xerr)
		return nil, xerr
	}

	uid, err := s.authRepository.IsValidCredentials(ctx, data.Email, data.Password)
	if err != nil {
		return nil, err
	}

	if err := s.canSendEmail(ctx, data.Email); err != nil {
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

	text, xerr := templates.GenerateLoginCodeHTML(code)
	if xerr != nil {
		sentry.CaptureException(xerr)
		return nil, errx.InternalError()
	}

	if xerr := s.emailNotificationService.Send(ctx, []string{data.Email}, nil, nil, "Your Login Code", text); xerr != nil {
		sentry.CaptureException(xerr)
		return nil, errx.InternalError()
	}

	codeHash, xerr := argon2.Hash(code)
	if xerr != nil {
		sentry.CaptureException(xerr)
		return nil, errx.InternalError()
	}

	session := &models.LoginSession{
		CodeHash: codeHash,
		Nonce:    nonce,
	}

	sessionToken, xerr := s.tokenService.GenerateToken(uid, sessionID, "", nonce, issuedAt, expiresAt)
	if xerr != nil {
		sentry.CaptureException(xerr)
		return nil, errx.InternalError()
	}

	if err := s.saveLoginSession(ctx, sessionID, session, expiresAt); err != nil {
		return nil, err
	}

	return &models.AuthSession{
		Session: sessionToken,
	}, nil
}

func (s *authService) LoginConfirm(ctx context.Context, data *ConfirmData, session, ipaddr string, userAgent string) (*models.Token, *errx.Error) {
	atoken, err := s.tokenService.VerifyToken(session)
	if err != nil {
		return nil, err
	}
	if atoken.ExpiresAt.Before(time.Now()) {
		return nil, errx.ErrSession
	}
	sess, err := s.getLoginSession(ctx, atoken.SessionID)
	if err != nil {
		return nil, err
	}
	if sess == nil || sess.Nonce != atoken.Nonce {
		return nil, errx.ErrSession
	}

	if sess.Tries >= AuthAttempts {
		return nil, errx.ErrCodeLimit
	}

	v, xerr := argon2.Verify(data.Code, sess.CodeHash)
	if xerr != nil {
		sentry.CaptureException(xerr)
		return nil, errx.InternalError()
	}

	if !v {
		sess.Tries++
		_ = s.saveLoginSession(ctx, atoken.SessionID, sess, atoken.ExpiresAt.Time)
		return nil, errx.ErrCode
	}

	newToken, err := s.tokenService.GenerateSession(ctx, atoken.UserID, "", ipaddr, userAgent, token.AuthProviderEmail)
	if err != nil {
		return nil, err
	}

	return newToken, nil
}
