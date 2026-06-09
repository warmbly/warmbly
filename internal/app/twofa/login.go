package twofa

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/crypt"
)

// CreatePendingChallenge mints a short-lived single-use pending token bound to a
// fresh nonce in Redis. The token carries no AccessNonce + has no DB session, so
// it can never be used as an access token — only exchanged via VerifyLogin.
func (s *service) CreatePendingChallenge(ctx context.Context, userID uuid.UUID) (string, int, *errx.Error) {
	sid := uuid.New()
	nonce, err := crypt.Nonce()
	if err != nil {
		return "", 0, errx.InternalError()
	}
	now := time.Now()
	pendTok, terr := s.tokens.GenerateToken(userID, sid, "", nonce, now, now.Add(pendingTTL))
	if terr != nil {
		return "", 0, errx.InternalError()
	}
	if err := s.savePending(ctx, sid, &models.TwoFAPending{UserID: userID, Nonce: nonce}, pendingTTL); err != nil {
		return "", 0, errx.InternalError()
	}
	return pendTok, int(pendingTTL.Seconds()), nil
}

// VerifyLogin validates the pending token + code (TOTP or recovery) and, on
// success, mints a real session via the SAME path as a normal login.
func (s *service) VerifyLogin(ctx context.Context, pendingToken, code, ipaddr, userAgent string) (*models.Token, *errx.Error) {
	claims, xerr := s.tokens.VerifyToken(pendingToken)
	if xerr != nil {
		return nil, errx.New(errx.BadRequest, "Invalid or expired session")
	}
	pend, err := s.getPending(ctx, claims.SessionID)
	if err != nil {
		return nil, errx.InternalError()
	}
	// Bind the token to its single-use Redis record (an access token's session id
	// has no 2fa_pending record, so it can't be replayed here).
	if pend == nil || pend.Nonce != claims.Nonce || pend.UserID != claims.UserID {
		return nil, errx.New(errx.BadRequest, "Invalid or expired session")
	}
	if pend.Tries >= maxTries {
		s.deletePending(ctx, claims.SessionID)
		return nil, errx.New(errx.BadRequest, "Too many attempts, please sign in again")
	}
	if !s.ipAllowed(ctx, ipaddr) {
		return nil, errx.New(errx.BadRequest, "Too many attempts, try again later")
	}

	row, err := s.repo.Get(ctx, claims.UserID)
	if err != nil {
		return nil, errx.InternalError()
	}
	if row == nil || !row.Enabled {
		s.deletePending(ctx, claims.SessionID)
		return nil, errx.New(errx.BadRequest, "2FA is not enabled")
	}

	if !s.validCode(ctx, claims.UserID, row, code) {
		pend.Tries++
		_ = s.savePending(ctx, claims.SessionID, pend, pendingTTL)
		return nil, errx.New(errx.BadRequest, "Invalid code")
	}

	// Single-use: delete the pending record BEFORE minting (delete-then-mint
	// closes a double-spend race).
	s.deletePending(ctx, claims.SessionID)
	return s.tokens.GenerateSession(ctx, claims.UserID, "", ipaddr, userAgent, token.AuthProviderEmail)
}
