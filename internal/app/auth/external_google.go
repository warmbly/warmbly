package auth

import (
	"context"

	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func (s *authService) GoogleAuth(ctx context.Context, code, ipaddr, userAgent string) (*models.LoginResult, *errx.Error) {
	atoken, err := s.externalAuth.GoogleAuth.Exchange(ctx, code)
	if err != nil {
		return nil, errx.ErrExternalCode
	}

	user, err := s.externalAuth.GoogleAuth.GetUserInfo(ctx, atoken)
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

	// Ban enforcement and the 2FA gate, which this path skipped entirely: a
	// user who enrolled TOTP could sign in without it by choosing Google.
	return s.finishLoginAs(ctx, udb.ID, ipaddr, userAgent, token.AuthProviderGoogle)
}
