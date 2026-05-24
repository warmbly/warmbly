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
	UpdateLastUsed(ctx context.Context, keyID uuid.UUID, ip string) error
	LogUsage(ctx context.Context, log *models.APIKeyUsageLog) error

	// Analytics
	GetUsageSummary(ctx context.Context, orgID uuid.UUID) (*models.APIKeyUsageSummary, *errx.Error)
	GetUsageTimeseries(ctx context.Context, orgID uuid.UUID, keyID *uuid.UUID, from, to time.Time, bucket string) ([]models.APIKeyUsageBucket, *errx.Error)
	GetEndpointBreakdown(ctx context.Context, orgID uuid.UUID, keyID *uuid.UUID, from, to time.Time, limit int) ([]models.APIKeyEndpointStat, *errx.Error)
	ListUsageLogs(ctx context.Context, orgID, keyID uuid.UUID, limit int, cursor *uuid.UUID) (*models.APIKeyUsageLogsResult, *errx.Error)
}

type apiKeyRepository struct {
	DB *db.DB
}

func NewAPIKeyRepository(db *db.DB) APIKeyRepository {
	return &apiKeyRepository{DB: db}
}

const API_KEY_SELECT = `id, user_id, organization_id, name, description, key_prefix, key_suffix, permissions,
	allowed_ips, allowed_email_accounts, rate_limit_per_minute,
	status, last_used_at, last_request_ip::text, expires_at, revoked_at, revoked_reason,
	created_at, updated_at`

func scanAPIKey(row db.Scannable, key *models.APIKey) error {
	var allowedIPs, allowedAccounts []string
	err := row.Scan(
		&key.ID, &key.UserID, &key.OrganizationID, &key.Name, &key.Description, &key.KeyPrefix, &key.KeySuffix, &key.Permissions,
		&allowedIPs, &allowedAccounts, &key.RateLimitPerMinute,
		&key.Status, &key.LastUsedAt, &key.LastRequestIP, &key.ExpiresAt, &key.RevokedAt, &key.RevokedReason,
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
		INSERT INTO api_keys (user_id, organization_id, name, description, key_prefix, key_suffix, key_hash, permissions, allowed_ips, allowed_email_accounts, rate_limit_per_minute, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, COALESCE($11, 60), $12)
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
		data.Description,
		keyPrefix,
		keySuffix,
		keyHash,
		data.Permissions,
		data.AllowedIPs,
		allowedAccountsStr,
		data.RateLimitPerMinute,
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
	if data.Description != nil {
		setClauses = append(setClauses, fmt.Sprintf("description = $%d", argPos))
		args = append(args, *data.Description)
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
	if data.RateLimitPerMinute != nil {
		setClauses = append(setClauses, fmt.Sprintf("rate_limit_per_minute = $%d", argPos))
		args = append(args, *data.RateLimitPerMinute)
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

func (r *apiKeyRepository) UpdateLastUsed(ctx context.Context, keyID uuid.UUID, ip string) error {
	// Casting via NULLIF lets the same query handle "no IP available" (worker
	// background calls, tests) without erroring on an empty INET.
	query := `UPDATE api_keys
		SET last_used_at = now(),
		    last_request_ip = NULLIF($2, '')::inet
		WHERE id = $1`
	_, err := r.DB.Exec(ctx, query, keyID, ip)
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

// GetUsageSummary returns the org-level overview shown in the dashboard
// strip. Counts are over the last 24 hours so the page can show "what's
// going on right now" without needing a date picker.
func (r *apiKeyRepository) GetUsageSummary(ctx context.Context, orgID uuid.UUID) (*models.APIKeyUsageSummary, *errx.Error) {
	query := `
		WITH key_counts AS (
			SELECT
				COUNT(*) FILTER (WHERE status = 'active')  AS active_keys,
				COUNT(*) FILTER (WHERE status = 'revoked') AS revoked_keys,
				COUNT(*) FILTER (WHERE status = 'expired') AS expired_keys
			FROM api_keys
			WHERE organization_id = $1
		),
		usage AS (
			SELECT
				COUNT(*) AS total,
				COUNT(*) FILTER (WHERE l.response_status >= 400) AS errors,
				COALESCE(AVG(l.response_time_ms), 0)::float8 AS avg_latency_ms,
				MAX(l.created_at) AS last_call_at
			FROM api_key_usage_logs l
			JOIN api_keys k ON k.id = l.api_key_id
			WHERE k.organization_id = $1
			  AND l.created_at >= now() - INTERVAL '24 hours'
		)
		SELECT
			key_counts.active_keys,
			key_counts.revoked_keys,
			key_counts.expired_keys,
			usage.total,
			usage.errors,
			usage.avg_latency_ms,
			usage.last_call_at
		FROM key_counts CROSS JOIN usage
	`

	var s models.APIKeyUsageSummary
	row := r.DB.QueryRow(ctx, query, orgID)
	if err := row.Scan(
		&s.ActiveKeys, &s.RevokedKeys, &s.ExpiredKeys,
		&s.Requests24h, &s.Errors24h, &s.AvgLatencyMs24h, &s.LastCallAt,
	); err != nil {
		db.CaptureError(err, query, []any{orgID}, "queryrow")
		return nil, errx.InternalError()
	}
	return &s, nil
}

// GetUsageTimeseries returns per-bucket request counts split by status
// family (success / 4xx / 5xx). Bucket is one of "minute", "hour", "day".
// keyID is optional; nil means "across every key in the org".
func (r *apiKeyRepository) GetUsageTimeseries(ctx context.Context, orgID uuid.UUID, keyID *uuid.UUID, from, to time.Time, bucket string) ([]models.APIKeyUsageBucket, *errx.Error) {
	// Whitelist the trunc unit so we can interpolate it without opening up
	// a SQL injection through the bucket parameter.
	truncUnit := "hour"
	switch bucket {
	case "minute":
		truncUnit = "minute"
	case "hour":
		truncUnit = "hour"
	case "day":
		truncUnit = "day"
	}

	query := fmt.Sprintf(`
		SELECT
			date_trunc('%s', l.created_at) AS bucket,
			COUNT(*) AS total,
			COUNT(*) FILTER (WHERE l.response_status BETWEEN 200 AND 399) AS success,
			COUNT(*) FILTER (WHERE l.response_status BETWEEN 400 AND 499) AS client_errors,
			COUNT(*) FILTER (WHERE l.response_status >= 500) AS server_errors,
			COALESCE(AVG(l.response_time_ms), 0)::float8 AS avg_latency_ms
		FROM api_key_usage_logs l
		JOIN api_keys k ON k.id = l.api_key_id
		WHERE k.organization_id = $1
		  AND ($2::uuid IS NULL OR l.api_key_id = $2)
		  AND l.created_at >= $3
		  AND l.created_at < $4
		GROUP BY bucket
		ORDER BY bucket ASC
	`, truncUnit)

	params := []any{orgID, keyID, from, to}
	rows, err := r.DB.Query(ctx, query, params...)
	if err != nil {
		db.CaptureError(err, query, params, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()

	out := make([]models.APIKeyUsageBucket, 0, 64)
	for rows.Next() {
		var b models.APIKeyUsageBucket
		if err := rows.Scan(&b.Bucket, &b.Total, &b.Success, &b.ClientErrors, &b.ServerErrors, &b.AvgLatencyMs); err != nil {
			db.CaptureError(err, query, params, "scan")
			return nil, errx.InternalError()
		}
		out = append(out, b)
	}
	return out, nil
}

// GetEndpointBreakdown returns the top `limit` endpoints by call count for
// the given key (or the whole org if keyID is nil) over [from, to).
func (r *apiKeyRepository) GetEndpointBreakdown(ctx context.Context, orgID uuid.UUID, keyID *uuid.UUID, from, to time.Time, limit int) ([]models.APIKeyEndpointStat, *errx.Error) {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	query := `
		SELECT
			l.endpoint,
			l.method,
			COUNT(*) AS count,
			COUNT(*) FILTER (WHERE l.response_status >= 400) AS error_count,
			COALESCE(AVG(l.response_time_ms), 0)::float8 AS avg_latency_ms
		FROM api_key_usage_logs l
		JOIN api_keys k ON k.id = l.api_key_id
		WHERE k.organization_id = $1
		  AND ($2::uuid IS NULL OR l.api_key_id = $2)
		  AND l.created_at >= $3
		  AND l.created_at < $4
		GROUP BY l.endpoint, l.method
		ORDER BY count DESC
		LIMIT $5
	`

	params := []any{orgID, keyID, from, to, limit}
	rows, err := r.DB.Query(ctx, query, params...)
	if err != nil {
		db.CaptureError(err, query, params, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()

	out := make([]models.APIKeyEndpointStat, 0, limit)
	for rows.Next() {
		var s models.APIKeyEndpointStat
		if err := rows.Scan(&s.Endpoint, &s.Method, &s.Count, &s.ErrorCount, &s.AvgLatencyMs); err != nil {
			db.CaptureError(err, query, params, "scan")
			return nil, errx.InternalError()
		}
		out = append(out, s)
	}
	return out, nil
}

// ListUsageLogs returns recent raw request entries for a single key. Used
// to power the live activity table in the detail drawer.
func (r *apiKeyRepository) ListUsageLogs(ctx context.Context, orgID, keyID uuid.UUID, limit int, cursor *uuid.UUID) (*models.APIKeyUsageLogsResult, *errx.Error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	query := `
		SELECT
			l.id, l.api_key_id, l.endpoint, l.method, l.ip_address::text,
			COALESCE(l.user_agent, ''), COALESCE(l.response_status, 0),
			COALESCE(l.response_time_ms, 0), l.created_at
		FROM api_key_usage_logs l
		JOIN api_keys k ON k.id = l.api_key_id
		WHERE k.organization_id = $1
		  AND l.api_key_id = $2
		  AND ($3::uuid IS NULL OR (l.created_at, l.id) < (
			SELECT created_at, id FROM api_key_usage_logs WHERE id = $3
		  ))
		ORDER BY l.created_at DESC, l.id DESC
		LIMIT $4
	`

	params := []any{orgID, keyID, cursor, limit + 1}
	rows, err := r.DB.Query(ctx, query, params...)
	if err != nil {
		db.CaptureError(err, query, params, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()

	logs := make([]models.APIKeyUsageLog, 0, limit)
	for rows.Next() {
		var l models.APIKeyUsageLog
		if err := rows.Scan(&l.ID, &l.APIKeyID, &l.Endpoint, &l.Method, &l.IPAddress, &l.UserAgent, &l.ResponseCode, &l.ResponseTime, &l.CreatedAt); err != nil {
			db.CaptureError(err, query, params, "scan")
			return nil, errx.InternalError()
		}
		logs = append(logs, l)
	}

	var nextCursor *uuid.UUID
	hasMore := false
	if len(logs) > limit {
		hasMore = true
		nextCursor = &logs[limit].ID
		logs = logs[:limit]
	}

	return &models.APIKeyUsageLogsResult{
		Data: logs,
		Pagination: models.Pagination{
			NextCursor: nextCursor,
			HasMore:    hasMore,
		},
	}, nil
}
