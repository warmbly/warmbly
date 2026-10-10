package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

type TokenRepository interface {
	GenerateSession(ctx context.Context, tx pgx.Tx, session *models.Session) *errx.Error
	// StampReauth records that this session just re-proved the account holder.
	StampReauth(ctx context.Context, sessionID uuid.UUID, at time.Time) error
	GetSession(ctx context.Context, sessionID uuid.UUID) (*models.Session, *errx.Error)
	ListSessionsByUser(ctx context.Context, userID uuid.UUID) ([]*models.Session, *errx.Error)
	RefreshToken(ctx context.Context, sessionID uuid.UUID, oldRefreshNonce, refreshNonce, accessNonce string, issuedAt time.Time) *errx.Error
	// RevokeOnRefreshReuse revokes the session when the presented refresh nonce
	// is neither current nor the one rotated away from after raceSince, and
	// reports whether it did.
	RevokeOnRefreshReuse(ctx context.Context, sessionID uuid.UUID, presentedNonce string, raceSince time.Time) (bool, *errx.Error)
	RevokeSession(ctx context.Context, tx pgx.Tx, sessionID uuid.UUID, revokedAt time.Time) *errx.Error
	RevokeSessionByID(ctx context.Context, userID, sessionID uuid.UUID, revokedAt time.Time) (bool, *errx.Error)
	ListOtherActiveSessionIDs(ctx context.Context, userID, exceptID uuid.UUID) ([]uuid.UUID, *errx.Error)
	RevokeOtherSessions(ctx context.Context, userID, exceptID uuid.UUID) *errx.Error

	// Organization switching
	UpdateCurrentOrganization(ctx context.Context, sessionID uuid.UUID, orgID *uuid.UUID) *errx.Error
	DefaultOrganization(ctx context.Context, userID uuid.UUID) (*uuid.UUID, *errx.Error)
	ClearOrganization(ctx context.Context, userID, orgID uuid.UUID) ([]uuid.UUID, *errx.Error)
}

type tokenRepository struct {
	DB *db.DB
}

func NewTokenRepostory(db *db.DB) TokenRepository {
	return &tokenRepository{
		DB: db,
	}
}

func (r *tokenRepository) GenerateSession(ctx context.Context, tx pgx.Tx, session *models.Session) *errx.Error {
	query := `
		INSERT INTO sessions (
		 id, user_id, current_organization_id,
		 created_at, expires_at, last_refreshed_at, revoked_at,
		 access_nonce, refresh_nonce,
		 location_city, location_region, location_country, location_country_code, location_postal_code,
		 os_name, browser_name, auth_provider, mfa_verified, reauth_at
		) VALUES (
		 $1, $2, $3,
		 $4, $5, $6, $7,
		 $8, $9,
		 $10, $11, $12, $13, $14,
		 $15, $16, $17, $18, $19
		)
	`

	params := []any{
		session.ID, session.UserID, session.CurrentOrganizationID,
		session.CreatedAt, session.ExpiresAt, session.LastRefreshedAt, session.RevokedAt,
		session.AccessNonce, session.RefreshNonce,
		session.LocationCity, session.LocationRegion, session.LocationCountry, session.LocationCountryCode, session.LocationPostalCode,
		session.OSName, session.BrowserName, session.AuthProvider, session.MFAVerified, session.ReauthAt,
	}

	_, err := tx.Exec(
		ctx,
		query,
		params...,
	)
	if err != nil {
		db.CaptureError(err, query, params, "exec")
		return errx.InternalError()
	}
	return nil
}

func (r *tokenRepository) GetSession(ctx context.Context, sessionID uuid.UUID) (*models.Session, *errx.Error) {
	query := `
		SELECT
		 id, user_id, current_organization_id,
		 created_at, expires_at, last_refreshed_at, revoked_at,
		 access_nonce, refresh_nonce,
		 location_city, location_region, location_country, location_country_code, location_postal_code,
		 os_name, browser_name, auth_provider, mfa_verified, reauth_at
		FROM sessions
		WHERE id = $1
	`

	params := []any{
		sessionID,
	}

	var sess models.Session

	err := r.DB.QueryRow(
		ctx,
		query,
		params...,
	).Scan(
		&sess.ID, &sess.UserID, &sess.CurrentOrganizationID,
		&sess.CreatedAt, &sess.ExpiresAt, &sess.LastRefreshedAt, &sess.RevokedAt,
		&sess.AccessNonce, &sess.RefreshNonce,
		&sess.LocationCity, &sess.LocationRegion, &sess.LocationCountry, &sess.LocationCountryCode, &sess.LocationPostalCode,
		&sess.OSName, &sess.BrowserName, &sess.AuthProvider, &sess.MFAVerified, &sess.ReauthAt,
	)
	if err != nil {
		// A missing session row is an expected auth outcome, not a server
		// fault: the session expired, was revoked and purged, or the token
		// references one that no longer exists. Surface it as a clean 401 so
		// the client refreshes/logs out, instead of a 500 that also pages
		// the error reporter on every stale refresh.
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errx.ErrToken
		}
		db.CaptureError(err, query, params, "queryrow")
		return nil, errx.InternalError()
	}

	return &sess, nil
}

// ListSessionsByUser returns the user's active (non-revoked, unexpired)
// sessions, most-recently-active first. Nonces are loaded but the service is
// responsible for never serializing them.
func (r *tokenRepository) ListSessionsByUser(ctx context.Context, userID uuid.UUID) ([]*models.Session, *errx.Error) {
	query := `
		SELECT
		 id, user_id, current_organization_id,
		 created_at, expires_at, last_refreshed_at, revoked_at,
		 access_nonce, refresh_nonce,
		 location_city, location_region, location_country, location_country_code, location_postal_code,
		 os_name, browser_name, auth_provider, mfa_verified, reauth_at
		FROM sessions
		WHERE user_id = $1
		  AND revoked_at IS NULL
		  AND (expires_at IS NULL OR expires_at > now())
		ORDER BY last_refreshed_at DESC, created_at DESC
	`

	params := []any{userID}

	rows, err := r.DB.Query(ctx, query, params...)
	if err != nil {
		db.CaptureError(err, query, params, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()

	var sessions []*models.Session
	for rows.Next() {
		var sess models.Session
		if err := rows.Scan(
			&sess.ID, &sess.UserID, &sess.CurrentOrganizationID,
			&sess.CreatedAt, &sess.ExpiresAt, &sess.LastRefreshedAt, &sess.RevokedAt,
			&sess.AccessNonce, &sess.RefreshNonce,
			&sess.LocationCity, &sess.LocationRegion, &sess.LocationCountry, &sess.LocationCountryCode, &sess.LocationPostalCode,
			&sess.OSName, &sess.BrowserName, &sess.AuthProvider, &sess.MFAVerified, &sess.ReauthAt,
		); err != nil {
			db.CaptureError(err, "", nil, "scan")
			return nil, errx.InternalError()
		}
		sessions = append(sessions, &sess)
	}

	if err := rows.Err(); err != nil {
		db.CaptureError(err, "", nil, "rows_err")
		return nil, errx.InternalError()
	}

	return sessions, nil
}

// RevokeSessionByID revokes a single session, scoped to its owner so one user
// can never revoke another's session by id. Returns false when nothing was
// updated (wrong owner, unknown id, or already revoked).
func (r *tokenRepository) RevokeSessionByID(ctx context.Context, userID, sessionID uuid.UUID, revokedAt time.Time) (bool, *errx.Error) {
	query := `
		UPDATE sessions
		SET revoked_at = $1
		WHERE id = $2 AND user_id = $3 AND revoked_at IS NULL
	`

	params := []any{revokedAt, sessionID, userID}

	cmd, err := r.DB.Exec(ctx, query, params...)
	if err != nil {
		db.CaptureError(err, query, params, "exec")
		return false, errx.InternalError()
	}

	return cmd.RowsAffected() > 0, nil
}

// ListOtherActiveSessionIDs returns the ids of the user's active sessions
// excluding the given one — used to bust their caches after a bulk revoke.
func (r *tokenRepository) ListOtherActiveSessionIDs(ctx context.Context, userID, exceptID uuid.UUID) ([]uuid.UUID, *errx.Error) {
	query := `
		SELECT id
		FROM sessions
		WHERE user_id = $1
		  AND id <> $2
		  AND revoked_at IS NULL
	`

	params := []any{userID, exceptID}

	rows, err := r.DB.Query(ctx, query, params...)
	if err != nil {
		db.CaptureError(err, query, params, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			db.CaptureError(err, "", nil, "scan")
			return nil, errx.InternalError()
		}
		ids = append(ids, id)
	}

	if err := rows.Err(); err != nil {
		db.CaptureError(err, "", nil, "rows_err")
		return nil, errx.InternalError()
	}

	return ids, nil
}

// RevokeOtherSessions revokes every active session for the user except the
// given one (used by "sign out all other sessions").
func (r *tokenRepository) RevokeOtherSessions(ctx context.Context, userID, exceptID uuid.UUID) *errx.Error {
	query := `
		UPDATE sessions
		SET revoked_at = now()
		WHERE user_id = $1
		  AND id <> $2
		  AND revoked_at IS NULL
	`

	params := []any{userID, exceptID}

	_, err := r.DB.Exec(ctx, query, params...)
	if err != nil {
		db.CaptureError(err, query, params, "exec")
		return errx.InternalError()
	}

	return nil
}

func (r *tokenRepository) RefreshToken(ctx context.Context, sessionID uuid.UUID, oldRefreshNonce, accessNonce, refreshNonce string, issuedAt time.Time) *errx.Error {
	query := `
		UPDATE sessions
		SET last_refreshed_at = $5,
		 access_nonce = $1, refresh_nonce = $2, previous_refresh_nonce = $3
		WHERE refresh_nonce = $3 AND id = $4 AND revoked_at IS NULL
	`

	params := []any{
		accessNonce, refreshNonce,
		oldRefreshNonce, sessionID,
		issuedAt,
	}

	cmd, err := r.DB.Exec(
		ctx,
		query,
		params...,
	)
	if err != nil {
		db.CaptureError(err, query, params, "exec")
		return errx.InternalError()
	}

	if cmd.RowsAffected() == 0 {
		return errx.ErrToken
	}

	return nil
}

func (r *tokenRepository) RevokeOnRefreshReuse(ctx context.Context, sessionID uuid.UUID, presentedNonce string, raceSince time.Time) (bool, *errx.Error) {
	query := `
		UPDATE sessions
		SET revoked_at = now()
		WHERE id = $1
		  AND revoked_at IS NULL
		  AND refresh_nonce IS DISTINCT FROM $2
		  AND NOT (previous_refresh_nonce IS NOT DISTINCT FROM $2 AND last_refreshed_at > $3)
	`

	params := []any{sessionID, presentedNonce, raceSince}

	cmd, err := r.DB.Exec(ctx, query, params...)
	if err != nil {
		db.CaptureError(err, query, params, "exec")
		return false, errx.InternalError()
	}
	return cmd.RowsAffected() > 0, nil
}

func (r *tokenRepository) RevokeSession(ctx context.Context, tx pgx.Tx, sessionID uuid.UUID, revokedAt time.Time) *errx.Error {
	query := `
		UPDATE sessions
		SET revoked_at = $1
		WHERE id = $2
	`

	params := []any{
		revokedAt, sessionID,
	}

	_, err := tx.Exec(
		ctx,
		query,
		params...,
	)
	if err != nil {
		db.CaptureError(err, query, params, "exec")
		return errx.InternalError()
	}

	return nil
}

func (r *tokenRepository) UpdateCurrentOrganization(ctx context.Context, sessionID uuid.UUID, orgID *uuid.UUID) *errx.Error {
	query := `
		UPDATE sessions
		SET current_organization_id = $2
		WHERE id = $1 AND revoked_at IS NULL
	`

	params := []any{
		sessionID, orgID,
	}

	cmd, err := r.DB.Exec(
		ctx,
		query,
		params...,
	)
	if err != nil {
		db.CaptureError(err, query, params, "exec")
		return errx.InternalError()
	}

	if cmd.RowsAffected() == 0 {
		return errx.New(errx.NotFound, "session not found")
	}

	return nil
}

// ClearOrganization deselects orgID on every live session of userID and returns every live session left
// with none, so a cached copy of a row the organization's deletion already cleared is evicted too.
func (r *tokenRepository) ClearOrganization(ctx context.Context, userID, orgID uuid.UUID) ([]uuid.UUID, *errx.Error) {
	const query = `
		WITH cleared AS (
			UPDATE sessions
			SET current_organization_id = NULL
			WHERE user_id = $1 AND current_organization_id = $2 AND revoked_at IS NULL
			RETURNING id
		)
		SELECT id FROM cleared
		UNION
		SELECT id FROM sessions
		WHERE user_id = $1 AND revoked_at IS NULL AND expires_at > now() AND current_organization_id IS NULL
	`
	params := []any{userID, orgID}
	rows, err := r.DB.Query(ctx, query, params...)
	if err != nil {
		db.CaptureError(err, query, params, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			db.CaptureError(err, query, params, "scan")
			return nil, errx.InternalError()
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		db.CaptureError(err, query, params, "rows")
		return nil, errx.InternalError()
	}
	return ids, nil
}

// DefaultOrganization picks the workspace a new session starts in: the one the
// user owns, else their earliest-joined membership. A session without one used
// to reach org-scoped writes with no tenant, which is what let orgless rows be
// created in the first place (issue #168). Returns nil only when the user
// belongs to no organization at all.
func (r *tokenRepository) DefaultOrganization(ctx context.Context, userID uuid.UUID) (*uuid.UUID, *errx.Error) {
	const query = `
		SELECT m.organization_id
		FROM organization_members m
		LEFT JOIN organizations o ON o.id = m.organization_id
		WHERE m.user_id = $1 AND o.deletion_scheduled_for IS NULL
		ORDER BY (o.owner_user_id = $1) DESC, m.invited_at, m.organization_id
		LIMIT 1
	`
	var orgID uuid.UUID
	if err := r.DB.QueryRow(ctx, query, userID).Scan(&orgID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		db.CaptureError(err, query, []any{userID}, "queryrow")
		return nil, errx.InternalError()
	}
	return &orgID, nil
}

// StampReauth records a successful re-authentication on the session.
func (r *tokenRepository) StampReauth(ctx context.Context, sessionID uuid.UUID, at time.Time) error {
	_, err := r.DB.Exec(ctx,
		`UPDATE sessions SET reauth_at = $2 WHERE id = $1 AND revoked_at IS NULL`,
		sessionID, at)
	return err
}
