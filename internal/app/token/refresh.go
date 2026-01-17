package token

import (
	"context"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/crypt"
)

func (s *tokenService) RefreshToken(ctx context.Context, refreshToken string) (*models.Token, *errx.Error) {
	t, err := s.VerifyToken(refreshToken)
	if err != nil {
		return nil, err
	}

	if t.ExpiresAt.Before(time.Now()) {
		return nil, errx.ErrToken
	}

	sess, err := s.GetSession(ctx, t.SessionID)
	if err != nil {
		return nil, err
	}

	if sess.RefreshNonce != t.Nonce {
		return nil, errx.ErrToken
	}

	issuedAt := time.Now()

	accessTokenExpiresAt := issuedAt.Add(AccessTokenLifeTime)
	accessNonce, xerr := crypt.Nonce()
	if xerr != nil {
		sentry.CaptureException(xerr)
		return nil, errx.InternalError()
	}

	newAccessToken, xerr := s.GenerateToken(sess.UserID, sess.ID, "", accessNonce, issuedAt, accessTokenExpiresAt)
	if xerr != nil {
		sentry.CaptureException(xerr)
		return nil, errx.InternalError()
	}

	refreshTokenExpiresAt := issuedAt.Add(2 * 30 * 24 * time.Hour)
	refreshNonce, xerr := crypt.Nonce()
	if xerr != nil {
		sentry.CaptureException(xerr)
		return nil, errx.InternalError()
	}

	newRefreshToken, xerr := s.GenerateToken(sess.UserID, sess.ID, "", refreshNonce, issuedAt, refreshTokenExpiresAt)
	if xerr != nil {
		sentry.CaptureException(xerr)
		return nil, errx.InternalError()
	}

	if err := s.tokenRepository.RefreshToken(ctx, sess.ID, refreshToken, accessNonce, refreshNonce, issuedAt); err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}

	return &models.Token{
		AccessToken:           newAccessToken,
		AccessTokenExpiresAt:  accessTokenExpiresAt,
		RefreshToken:          newRefreshToken,
		RefreshTokenExpiresAt: refreshTokenExpiresAt,
	}, nil
}
