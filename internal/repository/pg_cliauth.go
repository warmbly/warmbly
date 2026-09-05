package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// CLIAuthRepository stores the `warmbly auth login` handshake. Rows live for
// minutes: the credential the flow produces is an ordinary API key.
type CLIAuthRepository interface {
	CreateCode(ctx context.Context, deviceCodeHash, userCode string, req models.CLIAuthStartRequest, expiresAt time.Time) (*models.CLIAuthCode, error)
	GetCodeByUserCode(ctx context.Context, userCode string) (*models.CLIAuthCode, error)
	// ApproveCode stores the minted secret for the next poll; false when the
	// code is no longer pending, which is what makes approval single-use.
	ApproveCode(ctx context.Context, userCode string, orgID, approvedBy, apiKeyID uuid.UUID, secret string) (bool, error)
	DenyCode(ctx context.Context, userCode string) (bool, error)
	// ClaimCode hands the secret out exactly once, clearing it in the same statement.
	ClaimCode(ctx context.Context, deviceCodeHash string) (*models.CLIAuthCode, string, error)
	DeleteExpiredCodes(ctx context.Context) error
}

type cliAuthRepository struct {
	db *pgxpool.Pool
}

func NewCLIAuthRepository(db *pgxpool.Pool) CLIAuthRepository {
	return &cliAuthRepository{db: db}
}

const cliAuthCodeColumns = `id, user_code, client_name, hostname, cli_version, scopes, status, organization_id, expires_at, created_at`

func scanCLIAuthCode(row pgx.Row) (*models.CLIAuthCode, error) {
	var c models.CLIAuthCode
	var scopes int64
	if err := row.Scan(&c.ID, &c.UserCode, &c.ClientName, &c.Hostname, &c.CLIVersion, &scopes, &c.Status, &c.OrganizationID, &c.ExpiresAt, &c.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	c.Scopes = uint64(scopes)
	c.ScopeNames = models.APIScopeNames(c.Scopes)
	return &c, nil
}

func (r *cliAuthRepository) CreateCode(ctx context.Context, deviceCodeHash, userCode string, req models.CLIAuthStartRequest, expiresAt time.Time) (*models.CLIAuthCode, error) {
	query := `
		INSERT INTO cli_auth_codes (device_code_hash, user_code, client_name, hostname, cli_version, scopes, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING ` + cliAuthCodeColumns
	c, err := scanCLIAuthCode(r.db.QueryRow(ctx, query, deviceCodeHash, userCode, req.ClientName, req.Hostname, req.CLIVersion, int64(req.Scopes), expiresAt))
	if err != nil {
		db.CaptureError(err, query, nil, "queryrow")
		return nil, err
	}
	return c, nil
}

func (r *cliAuthRepository) GetCodeByUserCode(ctx context.Context, userCode string) (*models.CLIAuthCode, error) {
	query := `SELECT ` + cliAuthCodeColumns + ` FROM cli_auth_codes WHERE user_code = $1 AND expires_at > NOW()`
	c, err := scanCLIAuthCode(r.db.QueryRow(ctx, query, userCode))
	if err != nil {
		db.CaptureError(err, query, []any{userCode}, "queryrow")
		return nil, err
	}
	return c, nil
}

func (r *cliAuthRepository) ApproveCode(ctx context.Context, userCode string, orgID, approvedBy, apiKeyID uuid.UUID, secret string) (bool, error) {
	query := `
		UPDATE cli_auth_codes
		SET status = 'approved', organization_id = $2, approved_by = $3, api_key_id = $4, api_key_secret = $5
		WHERE user_code = $1 AND status = 'pending' AND expires_at > NOW()
	`
	tag, err := r.db.Exec(ctx, query, userCode, orgID, approvedBy, apiKeyID, secret)
	if err != nil {
		db.CaptureError(err, query, nil, "exec")
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (r *cliAuthRepository) DenyCode(ctx context.Context, userCode string) (bool, error) {
	query := `UPDATE cli_auth_codes SET status = 'denied' WHERE user_code = $1 AND status = 'pending'`
	tag, err := r.db.Exec(ctx, query, userCode)
	if err != nil {
		db.CaptureError(err, query, []any{userCode}, "exec")
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (r *cliAuthRepository) ClaimCode(ctx context.Context, deviceCodeHash string) (*models.CLIAuthCode, string, error) {
	// The secret comes from the locked pre-update row; RETURNING would only
	// see the cleared value.
	query := `
		WITH picked AS (
			SELECT id, api_key_secret
			FROM cli_auth_codes
			WHERE device_code_hash = $1 AND status = 'approved' AND expires_at > NOW()
			FOR UPDATE
		), claimed AS (
			UPDATE cli_auth_codes p
			SET status = 'claimed', api_key_secret = NULL
			FROM picked
			WHERE p.id = picked.id
			RETURNING p.id, p.user_code, p.client_name, p.hostname, p.cli_version, p.scopes, p.status, p.organization_id, p.expires_at, p.created_at, picked.api_key_secret AS secret
		)
		SELECT id, user_code, client_name, hostname, cli_version, scopes, status, organization_id, expires_at, created_at, COALESCE(secret, '') FROM claimed
		UNION ALL
		SELECT ` + cliAuthCodeColumns + `, '' FROM cli_auth_codes
		WHERE device_code_hash = $1 AND expires_at > NOW() AND NOT EXISTS (SELECT 1 FROM claimed)
		LIMIT 1
	`
	var c models.CLIAuthCode
	var scopes int64
	var secret string
	err := r.db.QueryRow(ctx, query, deviceCodeHash).Scan(&c.ID, &c.UserCode, &c.ClientName, &c.Hostname, &c.CLIVersion, &scopes, &c.Status, &c.OrganizationID, &c.ExpiresAt, &c.CreatedAt, &secret)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", nil
		}
		db.CaptureError(err, query, nil, "queryrow")
		return nil, "", err
	}
	c.Scopes = uint64(scopes)
	c.ScopeNames = models.APIScopeNames(c.Scopes)
	// The claimed row reports its new status; the caller wants "approved".
	if secret != "" {
		c.Status = models.CLIAuthCodeApproved
	}
	return &c, secret, nil
}

// DeleteExpiredCodes both destroys the plaintext secret on any expired row and
// removes rows old enough to be of no interest.
//
// The two are separate on purpose. An approved code the CLI never came back
// for would otherwise keep a usable key in plaintext for as long as the row
// survived, which is exactly what the "held only between approval and the next
// poll" intent rules out. Blanking it the moment the code expires bounds that
// to the code's own ten minutes. The key itself stays: it was legitimately
// created and is listed under Settings > API keys, but nobody holds its secret.
func (r *cliAuthRepository) DeleteExpiredCodes(ctx context.Context) error {
	query := `UPDATE cli_auth_codes SET api_key_secret = NULL WHERE api_key_secret IS NOT NULL AND expires_at < NOW()`
	if _, err := r.db.Exec(ctx, query); err != nil {
		db.CaptureError(err, query, nil, "exec")
		return err
	}
	_, err := r.db.Exec(ctx, `DELETE FROM cli_auth_codes WHERE expires_at < NOW() - INTERVAL '1 day'`)
	return err
}
