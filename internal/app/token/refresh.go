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

	if err := s.tokenRepository.RefreshToken(ctx, sess.ID, t.Nonce, accessNonce, refreshNonce, issuedAt); err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}

	// Invalidate the cached session. Postgres now has the new nonces, but
	// Redis still has the old ones from when the session was last read. The
	// very next API call would arrive with the NEW access nonce, GetSession
	// would return the stale cached row with the OLD access nonce, the
	// mismatch check would 401, and the frontend would log the user out.
	// Dropping the cache forces the next GetSession to re-read from the
	// updated Postgres row.
	if err := s.deleteSession(ctx, sess.ID); err != nil {
		sentry.CaptureException(err)
		// Don't fail the refresh — the worst case if the delete somehow
		// failed is the user retries; we already returned the new tokens.
	}

	return &models.Token{
		AccessToken:           newAccessToken,
		AccessTokenExpiresAt:  accessTokenExpiresAt,
		RefreshToken:          newRefreshToken,
		RefreshTokenExpiresAt: refreshTokenExpiresAt,
	}, nil
}
