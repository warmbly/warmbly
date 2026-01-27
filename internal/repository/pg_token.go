package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

type TokenRepository interface {
	GenerateSession(ctx context.Context, tx pgx.Tx, session *models.Session) *errx.Error
	GetSession(ctx context.Context, sessionID uuid.UUID) (*models.Session, *errx.Error)
	RefreshToken(ctx context.Context, sessionID uuid.UUID, oldRefreshNonce, refreshNonce, accessNonce string, issuedAt time.Time) *errx.Error
	RevokeSession(ctx context.Context, tx pgx.Tx, sessionID uuid.UUID, revokedAt time.Time) *errx.Error

	FindExpiredSessions(ctx context.Context, userID uuid.UUID, cutoff time.Time) ([]uuid.UUID, *errx.Error)
	RevokeSessions(ctx context.Context, userID uuid.UUID) *errx.Error

	// Organization switching
	UpdateCurrentOrganization(ctx context.Context, sessionID uuid.UUID, orgID *uuid.UUID) *errx.Error
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
		 os_name, browser_name
		) VALUES (
		 $1, $2, $3,
		 $4, $5, $6, $7,
		 $8, $9,
		 $10, $11, $12, $13, $14,
		 $15, $16
		)
	`

	params := []any{
		session.ID, session.UserID, session.CurrentOrganizationID,
		session.CreatedAt, session.ExpiresAt, session.LastRefreshedAt, session.RevokedAt,
		session.AccessNonce, session.RefreshNonce,
		session.LocationCity, session.LocationRegion, session.LocationCountry, session.LocationCountryCode, session.LocationPostalCode,
		session.OSName, session.BrowserName,
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
		 os_name, browser_name
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
		&sess.OSName, &sess.BrowserName,
	)
	if err != nil {
		db.CaptureError(err, query, params, "queryrow")
		return nil, errx.InternalError()
	}

	return &sess, nil
}

func (r *tokenRepository) RefreshToken(ctx context.Context, sessionID uuid.UUID, oldRefreshNonce, accessNonce, refreshNonce string, issuedAt time.Time) *errx.Error {
	query := `
		UPDATE sessions
		SET last_refreshed_at = $5,
		 access_nonce = $1, refresh_nonce = $2
		WHERE refresh_nonce = $3 AND id = $4
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

func (r *tokenRepository) FindExpiredSessions(ctx context.Context, userID uuid.UUID, cutoff time.Time) ([]uuid.UUID, *errx.Error) {
	query := `
        SELECT id
        FROM sessions
        WHERE revoked_at IS NULL
          AND last_refreshed_at < $1
    `

	params := []any{
		cutoff,
	}

	rows, err := r.DB.Query(
		ctx,
		query,
		cutoff,
	)
	if err != nil {
		db.CaptureError(err, query, params, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()

	var sessions []uuid.UUID
	for rows.Next() {
		var s uuid.UUID
		if err := rows.Scan(&s); err != nil {
			db.CaptureError(err, "", nil, "scan")
			return nil, errx.InternalError()
		}
		sessions = append(sessions, s)
	}

	if err := rows.Err(); err != nil {
		db.CaptureError(err, "", nil, "rows_err")
		return nil, errx.InternalError()
	}

	return sessions, nil
}

func (r *tokenRepository) RevokeSessions(ctx context.Context, userID uuid.UUID) *errx.Error {
	query := `
        UPDATE sessions
		SET revoked_at = now()
        WHERE revoked_at IS NULL
		  AND user_id = $1
    `

	params := []any{
		userID,
	}

	_, err := r.DB.Exec(
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
