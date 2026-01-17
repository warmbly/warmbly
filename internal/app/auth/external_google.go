package auth

import (
	"context"

	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func (s *authService) GoogleAuth(ctx context.Context, code, ipaddr, userAgent string) (*models.Token, *errx.Error) {
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

	session, xerr := s.tokenService.GenerateSession(ctx, udb.ID, "", ipaddr, userAgent, token.AuthProviderGoogle)
	if xerr != nil {
		return nil, xerr
	}

	return session, nil
}
