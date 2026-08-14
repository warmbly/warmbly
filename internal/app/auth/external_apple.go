package auth

import (
	"context"
	"errors"

	"github.com/getsentry/sentry-go"
	"github.com/meszmate/apple-go"
	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func (s *authService) AppleAuth(ctx context.Context, code, ipaddr, userAgent string) (*models.LoginResult, *errx.Error) {
	atoken, err := s.externalAuth.AppleAuth.ValidateCode(code)
	if err != nil {
		if errors.Is(err, apple.ErrorResponseInvalidGrant) {
			return nil, errx.ErrExternalCode
		}
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}

	user, err := apple.GetUserInfoFromIDToken(atoken.IDToken)
	if err != nil {
		return nil, errx.InternalError()
	}

	if !user.EmailVerified || user.Email == "" {
		return nil, errx.ErrExternalEmail
	}

	udb, xerr := s.authRepository.ExternalLogin(ctx, user.Email)
	if xerr != nil {
		return nil, xerr
	}

	// Ban enforcement and the 2FA gate, which this path skipped entirely.
	return s.finishLoginAs(ctx, udb.ID, ipaddr, userAgent, token.AuthProviderApple)
}
