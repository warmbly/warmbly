package token

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/crypt"
)

func (s *tokenService) RefreshToken(ctx context.Context, refreshToken string) (*models.Token, *errx.Error) {
	t, err := s.VerifyTokenFor(PurposeRefresh, refreshToken)
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

	// Neither a revoked session nor one past its absolute expiry refreshes back to life.
	if !sess.Live(time.Now()) {
		return nil, errx.ErrSessionEnded
	}

	if sess.RefreshNonce != t.Nonce {
		return nil, s.refusedRefresh(ctx, sess.ID, sess.UserID, t.Nonce)
	}

	issuedAt := time.Now()

	accessTokenExpiresAt := capAtSessionExpiry(issuedAt.Add(AccessTokenLifeTime), sess)
	accessNonce, xerr := crypt.Nonce()
	if xerr != nil {
		errs.CaptureException(xerr)
		return nil, errx.InternalError()
	}

	newAccessToken, xerr := s.GenerateTokenFor(PurposeAccess, sess.UserID, sess.ID, "", accessNonce, issuedAt, accessTokenExpiresAt)
	if xerr != nil {
		errs.CaptureException(xerr)
		return nil, errx.InternalError()
	}

	refreshTokenExpiresAt := capAtSessionExpiry(issuedAt.Add(RefreshTokenLifeTime), sess)
	refreshNonce, xerr := crypt.Nonce()
	if xerr != nil {
		errs.CaptureException(xerr)
		return nil, errx.InternalError()
	}

	// Explicitly PurposeRefresh. GenerateToken defaults to PurposeAccess, so
	// minting the rotated refresh token through it produced one this very
	// function would refuse on the next call: the session survived one refresh
	// and died on the second, about a day after every sign-in.
	newRefreshToken, xerr := s.GenerateTokenFor(PurposeRefresh, sess.UserID, sess.ID, "", refreshNonce, issuedAt, refreshTokenExpiresAt)
	if xerr != nil {
		errs.CaptureException(xerr)
		return nil, errx.InternalError()
	}

	if err := s.tokenRepository.RefreshToken(ctx, sess.ID, t.Nonce, accessNonce, refreshNonce, issuedAt); err != nil {
		// No row matched means the nonce was already rotated, which is two
		// tabs refreshing at once, not a fault. It has to stay the 401 that
		// tells the client to re-authenticate: reporting it and answering 500
		// paged on every lost race and left the client with nothing to do.
		if err == errx.ErrToken {
			return nil, s.refusedRefresh(ctx, sess.ID, sess.UserID, t.Nonce)
		}
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}

	// Invalidate copies used by older processes during rolling deployments.
	if err := s.deleteSession(ctx, sess.ID); err != nil {
		errs.CaptureException(err)
	}

	return &models.Token{
		AccessToken:           newAccessToken,
		AccessTokenExpiresAt:  accessTokenExpiresAt,
		RefreshToken:          newRefreshToken,
		RefreshTokenExpiresAt: refreshTokenExpiresAt,
	}, nil
}

// capAtSessionExpiry keeps a rotated token from outliving the session that issued it.
func capAtSessionExpiry(at time.Time, sess *models.Session) time.Time {
	if sess.ExpiresAt != nil && sess.ExpiresAt.Before(at) {
		return *sess.ExpiresAt
	}
	return at
}

// refreshRaceWindow is how long the refresh token a session just rotated away
// from is still taken for a lost race between two tabs rather than a replay.
const refreshRaceWindow = 2 * time.Minute

// refusedRefresh answers a refresh token that is no longer current. One that
// was replaced more than refreshRaceWindow ago, or earlier still, has been
// used twice, so the session it belongs to is ended: whoever holds the copy
// loses it at the same moment the rightful client would.
func (s *tokenService) refusedRefresh(ctx context.Context, sessionID, userID uuid.UUID, nonce string) *errx.Error {
	revoked, err := s.tokenRepository.RevokeOnRefreshReuse(ctx, sessionID, nonce, time.Now().Add(-refreshRaceWindow))
	if err != nil {
		errs.CaptureException(err)
		return errx.ErrToken
	}
	if revoked {
		if xerr := s.evictRevoked(ctx, sessionID, userID, time.Now()); xerr != nil {
			errs.CaptureException(xerr)
		}
		s.notifyRevoked(ctx, userID)
	}
	return errx.ErrToken
}
