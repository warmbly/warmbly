package token

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func (s *tokenService) GetSession(ctx context.Context, sessionID uuid.UUID) (*models.Session, *errx.Error) {
	// PostgreSQL is authoritative for session state on every request.
	return s.tokenRepository.GetSession(ctx, sessionID)
}

func sameTokenIssueTime(a, b time.Time) bool {
	// JWT issued-at precision can differ from DB timestamp precision.
	// Compare at second precision to avoid false mismatches.
	return a.UTC().Truncate(time.Second).Equal(b.UTC().Truncate(time.Second))
}

func (s *tokenService) ValidateAccessToken(ctx context.Context, accessToken string) (*models.Session, *errx.Error) {
	t, err := s.verifySessionToken(PurposeAccess, PurposePortalAccess, accessToken)
	if err != nil {
		return nil, err
	}

	if t.ExpiresAt.Before(time.Now()) {
		return nil, errx.ErrToken
	}

	session, err := s.GetSession(ctx, t.SessionID)
	if err != nil {
		return nil, err
	}

	// Revocation and absolute expiry are checked against the authoritative row.
	if !session.Live(time.Now()) {
		return nil, errx.ErrSessionEnded
	}

	if session.AccessNonce != t.Nonce || !sameTokenIssueTime(session.LastRefreshedAt, t.IssuedAt.Time) {
		return nil, errx.ErrToken
	}
	if t.Purpose != accessPurpose(session.PortalAgencyID != nil) {
		return nil, errx.ErrToken
	}

	return session, nil
}

// VerifyTokenFor is VerifyToken plus the check that the token was minted for
// this flow. A token with no purpose verifies for none.
func (s *tokenService) VerifyTokenFor(purpose, tokenStr string) (*TokenClaims, *errx.Error) {
	claims, err := s.VerifyToken(tokenStr)
	if err != nil {
		return nil, err
	}
	if claims.Purpose == "" || claims.Purpose != purpose {
		return nil, errx.ErrToken
	}
	return claims, nil
}

// verifySessionToken accepts a token minted for either the normal or the portal form of one purpose;
// the caller checks the form against the session.
func (s *tokenService) verifySessionToken(normal, portal, tokenStr string) (*TokenClaims, *errx.Error) {
	claims, err := s.VerifyToken(tokenStr)
	if err != nil {
		return nil, err
	}
	if claims.Purpose == "" || (claims.Purpose != normal && claims.Purpose != portal) {
		return nil, errx.ErrToken
	}
	return claims, nil
}

// VerifyWebsocketTicket accepts a socket ticket of either form.
func (s *tokenService) VerifyWebsocketTicket(tokenStr string) (*TokenClaims, *errx.Error) {
	return s.verifySessionToken(PurposeWebSocket, PurposePortalWebSocket, tokenStr)
}
