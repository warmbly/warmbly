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

	if xerr := s.sendAuthEmail(ctx, data.Email, "Your Login Code", text); xerr != nil {
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

func (s *authService) LoginConfirm(ctx context.Context, data *ConfirmData, session, ipaddr string, userAgent string) (*models.LoginResult, *errx.Error) {
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

	// Ban-scope enforcement (migration 000045). The runtime treats
	// BanScopeLogin as "this account cannot authenticate" — the row's
	// banned_at is set in tandem so legacy callers still see the user
	// as banned, but the bit makes the rule auditable.
	if scope, scopeErr := s.userRepository.GetBanState(ctx, atoken.UserID); scopeErr == nil {
		if models.BanScope(scope).Has(models.BanScopeLogin) {
			return nil, errx.New(errx.Forbidden, "this account has been suspended")
		}
	}

	// 2FA gate: after the email-code + ban check, if the user has TOTP enabled,
	// issue a single-use pending challenge instead of a full session. The FE
	// distinguishes on two_fa_required and POSTs /auth/2fa/verify next.
	if s.twofa != nil {
		if enabled, _ := s.twofa.IsEnabled(ctx, atoken.UserID); enabled {
			pendTok, expiresIn, perr := s.twofa.CreatePendingChallenge(ctx, atoken.UserID)
			if perr != nil {
				return nil, perr
			}
			// Consume the email login session so it can't be re-confirmed to mint
			// fresh pending tokens (which would reset the per-pending 2FA attempt
			// counter). One email confirmation => exactly one 2FA challenge.
			_ = s.cache.Del(ctx, getLoginSessionKey(atoken.SessionID)).Err()
			return &models.LoginResult{TwoFARequired: true, PendingToken: pendTok, ExpiresIn: expiresIn}, nil
		}
	}

	newToken, err := s.tokenService.GenerateSession(ctx, atoken.UserID, "", ipaddr, userAgent, token.AuthProviderEmail)
	if err != nil {
		return nil, err
	}

	return &models.LoginResult{Token: newToken}, nil
}
