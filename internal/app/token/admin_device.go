package token

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/crypt"
)

// AdminDeviceIssuer keeps delegation separate from ordinary sign-in minting.
type AdminDeviceIssuer interface {
	GenerateAdminDeviceSession(context.Context, uuid.UUID, uuid.UUID, time.Time) (*models.Token, *errx.Error)
}

func validAdminDeviceProof(mfa bool, revoked, expires, reauth *time.Time, approvedAt, now time.Time) bool {
	return mfa && revoked == nil && expires != nil && expires.After(now) && reauth != nil &&
		!approvedAt.After(now) && now.Sub(approvedAt) <= ReauthWindow && !reauth.Before(approvedAt)
}

func (s *tokenService) GenerateAdminDeviceSession(ctx context.Context, sourceID, userID uuid.UUID, approvedAt time.Time) (*models.Token, *errx.Error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, errx.InternalError()
	}
	defer tx.Rollback(ctx)
	var source models.Session
	var perms uint32
	var banScope uint64
	// Lock both authorities through insertion so revocation and permission changes serialize with issuance.
	err = tx.QueryRow(ctx, `SELECT s.auth_provider, s.mfa_verified, s.revoked_at, s.expires_at, s.reauth_at,
		u.admin_permissions, u.ban_scope FROM sessions s JOIN users u ON u.id=s.user_id
		WHERE s.id=$1 AND s.user_id=$2 FOR UPDATE OF s, u`, sourceID, userID).Scan(
		&source.AuthProvider, &source.MFAVerified, &source.RevokedAt, &source.ExpiresAt, &source.ReauthAt, &perms, &banScope)
	now := time.Now().UTC()
	if err != nil || perms == 0 || banScope&uint64(models.BanScopeLogin) != 0 || !validAdminDeviceProof(source.MFAVerified, source.RevokedAt, source.ExpiresAt, source.ReauthAt, approvedAt, now) {
		return nil, errx.ErrUnauthorized
	}
	accessNonce, err := crypt.Nonce()
	if err != nil {
		return nil, errx.InternalError()
	}
	refreshNonce, err := crypt.Nonce()
	if err != nil {
		return nil, errx.InternalError()
	}
	expires := now.Add(RefreshTokenLifeTime)
	session := &models.Session{ID: uuid.New(), UserID: userID, AuthProvider: source.AuthProvider,
		MFAVerified: source.MFAVerified, ReauthAt: &approvedAt, CreatedAt: now, LastRefreshedAt: now,
		ExpiresAt: &expires, AccessNonce: accessNonce, RefreshNonce: refreshNonce, BrowserName: "warmblyctl", OSName: "CLI"}
	tok := &models.Token{AccessTokenExpiresAt: now.Add(AccessTokenLifeTime), RefreshTokenExpiresAt: expires}
	tok.AccessToken, err = s.GenerateTokenFor(PurposeAccess, userID, session.ID, "", accessNonce, now, tok.AccessTokenExpiresAt)
	if err != nil {
		return nil, errx.InternalError()
	}
	tok.RefreshToken, err = s.GenerateTokenFor(PurposeRefresh, userID, session.ID, "", refreshNonce, now, expires)
	if err != nil {
		return nil, errx.InternalError()
	}
	if xerr := s.tokenRepository.GenerateSession(ctx, tx, session); xerr != nil {
		return nil, xerr
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, errx.InternalError()
	}
	if s.signInAlert != nil {
		alerter := s.signInAlert
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			alerter.NewSignIn(ctx, userID, session.BrowserName, session.OSName, "", "")
		}()
	}
	if s.sessionObserver != nil {
		observer := s.sessionObserver
		start := SessionStart{UserID: userID, Browser: session.BrowserName, OS: session.OSName, AuthProvider: source.AuthProvider, MFAVerified: source.MFAVerified, NewDevice: true}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			observer.SessionStarted(ctx, start)
		}()
	}
	return tok, nil
}
