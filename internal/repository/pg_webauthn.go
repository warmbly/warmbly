package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

const webauthnCredentialColumns = `
	id, user_id, credential_id, public_key,
	attestation_type, attestation_format,
	transports, aaguid, sign_count,
	clone_warning, backup_eligible, backup_state,
	user_present, user_verified,
	name, created_at, last_used_at
`

type WebAuthnRepository interface {
	// CreateCredential persists a freshly registered passkey.
	CreateCredential(ctx context.Context, cred *models.WebAuthnCredential) *errx.Error
	// ListByUser returns a user's passkeys newest-first.
	ListByUser(ctx context.Context, userID uuid.UUID) ([]*models.WebAuthnCredential, *errx.Error)
	// TouchCredential persists the post-assertion counter/flags and the
	// last-used timestamp. Best-effort: a missing row is not an error.
	TouchCredential(ctx context.Context, credentialID []byte, signCount uint32, cloneWarning, backupState bool, lastUsedAt time.Time) *errx.Error
	// Rename updates a passkey's friendly name (scoped to the owner).
	Rename(ctx context.Context, userID, id uuid.UUID, name string) *errx.Error
	// Delete removes a passkey (scoped to the owner).
	Delete(ctx context.Context, userID, id uuid.UUID) *errx.Error
	// CountByUser returns how many passkeys a user has.
	CountByUser(ctx context.Context, userID uuid.UUID) (int, *errx.Error)
}

type webauthnRepository struct {
	DB *db.DB
}

func NewWebAuthnRepository(db *db.DB) WebAuthnRepository {
	return &webauthnRepository{DB: db}
}

// scanCredential reads a full credential row. transports is stored as JSONB
// and sign_count as BIGINT, so both are decoded explicitly.
func scanCredential(row pgx.Row, cred *models.WebAuthnCredential) error {
	var transportsRaw []byte
	var signCount int64

	if err := row.Scan(
		&cred.ID, &cred.UserID, &cred.CredentialID, &cred.PublicKey,
		&cred.AttestationType, &cred.AttestationFormat,
		&transportsRaw, &cred.AAGUID, &signCount,
		&cred.CloneWarning, &cred.BackupEligible, &cred.BackupState,
		&cred.UserPresent, &cred.UserVerified,
		&cred.Name, &cred.CreatedAt, &cred.LastUsedAt,
	); err != nil {
		return err
	}

	if signCount < 0 {
		signCount = 0
	}
	cred.SignCount = uint32(signCount)

	cred.Transports = []string{}
	if len(transportsRaw) > 0 {
		_ = json.Unmarshal(transportsRaw, &cred.Transports)
	}

	return nil
}

func (r *webauthnRepository) CreateCredential(ctx context.Context, cred *models.WebAuthnCredential) *errx.Error {
	transports, err := json.Marshal(cred.Transports)
	if err != nil {
		return errx.InternalError()
	}

	query := `
		INSERT INTO webauthn_credentials (
			user_id, credential_id, public_key,
			attestation_type, attestation_format,
			transports, aaguid, sign_count,
			clone_warning, backup_eligible, backup_state,
			user_present, user_verified, name
		) VALUES (
			$1, $2, $3,
			$4, $5,
			$6, $7, $8,
			$9, $10, $11,
			$12, $13, $14
		)
		RETURNING id, created_at
	`

	params := []any{
		cred.UserID, cred.CredentialID, cred.PublicKey,
		cred.AttestationType, cred.AttestationFormat,
		transports, cred.AAGUID, int64(cred.SignCount),
		cred.CloneWarning, cred.BackupEligible, cred.BackupState,
		cred.UserPresent, cred.UserVerified, cred.Name,
	}

	if err := r.DB.QueryRow(ctx, query, params...).Scan(&cred.ID, &cred.CreatedAt); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return errx.ErrPasskeyExists
		}
		db.CaptureError(err, query, params, "queryrow")
		return errx.InternalError()
	}

	return nil
}

func (r *webauthnRepository) ListByUser(ctx context.Context, userID uuid.UUID) ([]*models.WebAuthnCredential, *errx.Error) {
	query := `
		SELECT ` + webauthnCredentialColumns + `
		FROM webauthn_credentials
		WHERE user_id = $1
		ORDER BY created_at DESC
	`

	params := []any{userID}

	rows, err := r.DB.Query(ctx, query, params...)
	if err != nil {
		db.CaptureError(err, query, params, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()

	credentials := make([]*models.WebAuthnCredential, 0)
	for rows.Next() {
		var cred models.WebAuthnCredential
		if err := scanCredential(rows, &cred); err != nil {
			db.CaptureError(err, "", nil, "scan")
			return nil, errx.InternalError()
		}
		credentials = append(credentials, &cred)
	}

	if err := rows.Err(); err != nil {
		db.CaptureError(err, "", nil, "rows_err")
		return nil, errx.InternalError()
	}

	return credentials, nil
}

func (r *webauthnRepository) TouchCredential(ctx context.Context, credentialID []byte, signCount uint32, cloneWarning, backupState bool, lastUsedAt time.Time) *errx.Error {
	query := `
		UPDATE webauthn_credentials
		SET sign_count = $2,
			clone_warning = $3,
			backup_state = $4,
			last_used_at = $5
		WHERE credential_id = $1
	`

	params := []any{credentialID, int64(signCount), cloneWarning, backupState, lastUsedAt}

	if _, err := r.DB.Exec(ctx, query, params...); err != nil {
		db.CaptureError(err, query, params, "exec")
		return errx.InternalError()
	}

	return nil
}

func (r *webauthnRepository) Rename(ctx context.Context, userID, id uuid.UUID, name string) *errx.Error {
	query := `
		UPDATE webauthn_credentials
		SET name = $3
		WHERE id = $1 AND user_id = $2
	`

	params := []any{id, userID, name}

	cmd, err := r.DB.Exec(ctx, query, params...)
	if err != nil {
		db.CaptureError(err, query, params, "exec")
		return errx.InternalError()
	}

	if cmd.RowsAffected() == 0 {
		return errx.ErrPasskeyNotFound
	}

	return nil
}

func (r *webauthnRepository) Delete(ctx context.Context, userID, id uuid.UUID) *errx.Error {
	query := `
		DELETE FROM webauthn_credentials
		WHERE id = $1 AND user_id = $2
	`

	params := []any{id, userID}

	cmd, err := r.DB.Exec(ctx, query, params...)
	if err != nil {
		db.CaptureError(err, query, params, "exec")
		return errx.InternalError()
	}

	if cmd.RowsAffected() == 0 {
		return errx.ErrPasskeyNotFound
	}

	return nil
}

func (r *webauthnRepository) CountByUser(ctx context.Context, userID uuid.UUID) (int, *errx.Error) {
	query := `SELECT count(*) FROM webauthn_credentials WHERE user_id = $1`

	params := []any{userID}

	var count int
	if err := r.DB.QueryRow(ctx, query, params...).Scan(&count); err != nil {
		db.CaptureError(err, query, params, "queryrow")
		return 0, errx.InternalError()
	}

	return count, nil
}
