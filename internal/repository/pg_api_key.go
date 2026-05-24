package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

type APIKeyRepository interface {
	Create(ctx context.Context, orgID, userID uuid.UUID, data *models.CreateAPIKey, keyPrefix, keySuffix, keyHash string) (*models.APIKey, *errx.Error)
	GetByHash(ctx context.Context, keyHash string) (*models.APIKey, *errx.Error)
	GetByID(ctx context.Context, orgID, keyID uuid.UUID) (*models.APIKey, *errx.Error)
	List(ctx context.Context, orgID uuid.UUID, limit int, cursor *uuid.UUID) (*models.APIKeysResult, *errx.Error)
	Update(ctx context.Context, orgID, keyID uuid.UUID, data *models.UpdateAPIKey) (*models.APIKey, *errx.Error)
	Revoke(ctx context.Context, orgID, keyID uuid.UUID, reason string) *errx.Error
	UpdateLastUsed(ctx context.Context, keyID uuid.UUID) error
	LogUsage(ctx context.Context, log *models.APIKeyUsageLog) error
}

type apiKeyRepository struct {
	DB *db.DB
}

func NewAPIKeyRepository(db *db.DB) APIKeyRepository {
	return &apiKeyRepository{DB: db}
}

const API_KEY_SELECT = `id, user_id, organization_id, name, key_prefix, key_suffix, permissions,
	allowed_ips, allowed_email_accounts,
	status, last_used_at, expires_at, revoked_at, revoked_reason,
	created_at, updated_at`

func scanAPIKey(row db.Scannable, key *models.APIKey) error {
	var allowedIPs, allowedAccounts []string
	err := row.Scan(
		&key.ID, &key.UserID, &key.OrganizationID, &key.Name, &key.KeyPrefix, &key.KeySuffix, &key.Permissions,
		&allowedIPs, &allowedAccounts,
		&key.Status, &key.LastUsedAt, &key.ExpiresAt, &key.RevokedAt, &key.RevokedReason,
		&key.CreatedAt, &key.UpdatedAt,
	)
	if err != nil {
		return err
	}
	key.AllowedIPs = allowedIPs
	key.AllowedEmailAccounts = make([]uuid.UUID, 0, len(allowedAccounts))
	for _, acc := range allowedAccounts {
		if id, err := uuid.Parse(acc); err == nil {
			key.AllowedEmailAccounts = append(key.AllowedEmailAccounts, id)
		}
	}
	return nil
}

func (r *apiKeyRepository) Create(ctx context.Context, orgID, userID uuid.UUID, data *models.CreateAPIKey, keyPrefix, keySuffix, keyHash string) (*models.APIKey, *errx.Error) {
	query := fmt.Sprintf(`
		INSERT INTO api_keys (user_id, organization_id, name, key_prefix, key_suffix, key_hash, permissions, allowed_ips, allowed_email_accounts, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING %s
	`, API_KEY_SELECT)

	var allowedAccountsStr []string
	for _, acc := range data.AllowedEmailAccounts {
		allowedAccountsStr = append(allowedAccountsStr, acc.String())
	}

	params := []any{
		userID,
		orgID,
		data.Name,
		keyPrefix,
		keySuffix,
		keyHash,
		data.Permissions,
		data.AllowedIPs,
		allowedAccountsStr,
		data.ExpiresAt,
	}

	var key models.APIKey
	row := r.DB.QueryRow(ctx, query, params...)
	if err := scanAPIKey(row, &key); err != nil {
		db.CaptureError(err, query, params, "queryrow")
		return nil, errx.InternalError()
	}

	return &key, nil
}

func (r *apiKeyRepository) GetByHash(ctx context.Context, keyHash string) (*models.APIKey, *errx.Error) {
	query := fmt.Sprintf(`
		SELECT %s FROM api_keys
		WHERE key_hash = $1 AND status = 'active'
	`, API_KEY_SELECT)

	var key models.APIKey
	row := r.DB.QueryRow(ctx, query, keyHash)
	if err := scanAPIKey(row, &key); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errx.ErrAuth
		}
		db.CaptureError(err, query, []any{keyHash}, "queryrow")
		return nil, errx.InternalError()
	}

	// Check if expired
	if key.ExpiresAt != nil && key.ExpiresAt.Before(time.Now()) {
		return nil, errx.ErrAuth
	}

	return &key, nil
}

func (r *apiKeyRepository) GetByID(ctx context.Context, orgID, keyID uuid.UUID) (*models.APIKey, *errx.Error) {
	query := fmt.Sprintf(`
		SELECT %s FROM api_keys
		WHERE organization_id = $1 AND id = $2
	`, API_KEY_SELECT)

	params := []any{orgID, keyID}

	var key models.APIKey
	row := r.DB.QueryRow(ctx, query, params...)
	if err := scanAPIKey(row, &key); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errx.ErrNotFound
		}
		db.CaptureError(err, query, params, "queryrow")
		return nil, errx.InternalError()
	}

	return &key, nil
}

func (r *apiKeyRepository) List(ctx context.Context, orgID uuid.UUID, limit int, cursor *uuid.UUID) (*models.APIKeysResult, *errx.Error) {
	query := fmt.Sprintf(`
		SELECT %s FROM api_keys
		WHERE organization_id = $1
		  AND ($2::uuid IS NULL OR (created_at, id) < (
			SELECT created_at, id FROM api_keys WHERE id = $2
		  ))
		ORDER BY created_at DESC, id DESC
		LIMIT $3
	`, API_KEY_SELECT)

	params := []any{orgID, cursor, limit + 1}

	rows, err := r.DB.Query(ctx, query, params...)
	if err != nil {
		db.CaptureError(err, query, params, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()

	keys := make([]models.APIKey, 0, limit)
	for rows.Next() {
		var key models.APIKey
		if err := scanAPIKey(rows, &key); err != nil {
			db.CaptureError(err, "", nil, "scan")
			return nil, errx.InternalError()
		}
		keys = append(keys, key)
	}

	var nextCursor *uuid.UUID
	hasMore := false
	if len(keys) > limit {
		hasMore = true
		nextCursor = &keys[limit].ID
		keys = keys[:limit]
	}

	return &models.APIKeysResult{
		Data: keys,
		Pagination: models.Pagination{
			NextCursor: nextCursor,
			HasMore:    hasMore,
		},
	}, nil
}

func (r *apiKeyRepository) Update(ctx context.Context, orgID, keyID uuid.UUID, data *models.UpdateAPIKey) (*models.APIKey, *errx.Error) {
	setClauses := []string{}
	args := []any{orgID, keyID}
	argPos := 3

	if data.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argPos))
		args = append(args, *data.Name)
		argPos++
	}
	if data.Permissions != nil {
		setClauses = append(setClauses, fmt.Sprintf("permissions = $%d", argPos))
		args = append(args, *data.Permissions)
		argPos++
	}
	if data.AllowedIPs != nil {
		setClauses = append(setClauses, fmt.Sprintf("allowed_ips = $%d", argPos))
		args = append(args, data.AllowedIPs)
		argPos++
	}
	if data.AllowedEmailAccounts != nil {
		var allowedAccountsStr []string
		for _, acc := range data.AllowedEmailAccounts {
			allowedAccountsStr = append(allowedAccountsStr, acc.String())
		}
		setClauses = append(setClauses, fmt.Sprintf("allowed_email_accounts = $%d", argPos))
		args = append(args, allowedAccountsStr)
		argPos++
	}

	if len(setClauses) == 0 {
		return nil, errx.ErrNotEnough
	}

	setClauses = append(setClauses, "updated_at = now()")

	query := fmt.Sprintf(`
		UPDATE api_keys SET %s
		WHERE organization_id = $1 AND id = $2 AND status = 'active'
		RETURNING %s
	`, strings.Join(setClauses, ", "), API_KEY_SELECT)

	var key models.APIKey
	row := r.DB.QueryRow(ctx, query, args...)
	if err := scanAPIKey(row, &key); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errx.ErrNotFound
		}
		db.CaptureError(err, query, args, "queryrow")
		return nil, errx.InternalError()
	}

	return &key, nil
}

func (r *apiKeyRepository) Revoke(ctx context.Context, orgID, keyID uuid.UUID, reason string) *errx.Error {
	query := `
		UPDATE api_keys
		SET status = 'revoked', revoked_at = now(), revoked_reason = $3, updated_at = now()
		WHERE organization_id = $1 AND id = $2 AND status = 'active'
	`

	params := []any{orgID, keyID, reason}

	cmd, err := r.DB.Exec(ctx, query, params...)
	if err != nil {
		db.CaptureError(err, query, params, "exec")
		return errx.InternalError()
	}

	if cmd.RowsAffected() == 0 {
		return errx.ErrNotFound
	}

	return nil
}

func (r *apiKeyRepository) UpdateLastUsed(ctx context.Context, keyID uuid.UUID) error {
	query := `UPDATE api_keys SET last_used_at = now() WHERE id = $1`
	_, err := r.DB.Exec(ctx, query, keyID)
	return err
}

func (r *apiKeyRepository) LogUsage(ctx context.Context, log *models.APIKeyUsageLog) error {
	query := `
		INSERT INTO api_key_usage_logs (api_key_id, endpoint, method, ip_address, user_agent, response_status, response_time_ms)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	params := []any{
		log.APIKeyID,
		log.Endpoint,
		log.Method,
		log.IPAddress,
		log.UserAgent,
		log.ResponseCode,
		log.ResponseTime,
	}

	_, err := r.DB.Exec(ctx, query, params...)
	if err != nil {
		db.CaptureError(err, query, params, "exec")
	}
	return err
}
