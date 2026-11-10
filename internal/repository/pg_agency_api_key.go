package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
)

// AgencyAPIKeyRepository stores agency API keys. Every read and write but the
// hash lookup is scoped by agency_id.
type AgencyAPIKeyRepository interface {
	Create(ctx context.Context, agencyID, createdBy uuid.UUID, data *models.CreateAgencyAPIKey, prefix, suffix, hash string) (*models.AgencyAPIKey, error)
	// GetActiveByHash is a live key whose creator is not banned; nil, nil when none.
	GetActiveByHash(ctx context.Context, hash string) (*models.AgencyAPIKey, error)
	// Get is nil, nil when the agency has no such key.
	Get(ctx context.Context, agencyID, keyID uuid.UUID) (*models.AgencyAPIKey, error)
	// List pages newest first after the (createdAt, id) position when after is set.
	List(ctx context.Context, agencyID uuid.UUID, limit int, afterAt *time.Time, afterID uuid.UUID) ([]models.AgencyAPIKey, error)
	// Update changes an active key; nil, nil when the agency has no active key by that id.
	Update(ctx context.Context, agencyID, keyID uuid.UUID, data *models.UpdateAgencyAPIKey) (*models.AgencyAPIKey, error)
	// Revoke ends a key; false when the agency has no such key.
	Revoke(ctx context.Context, agencyID, keyID uuid.UUID, reason string) (bool, error)
	RevokeCreatedBy(ctx context.Context, userID uuid.UUID, reason string) (int64, error)
	TouchLastUsed(ctx context.Context, keyID uuid.UUID, ip string) error
}

type agencyAPIKeyRepository struct {
	db *pgxpool.Pool
}

func NewAgencyAPIKeyRepository(db *pgxpool.Pool) AgencyAPIKeyRepository {
	return &agencyAPIKeyRepository{db: db}
}

const agencyAPIKeyColumns = `id, agency_id, created_by, name, key_prefix, key_suffix, permissions, allowed_ips,
	rate_limit_per_minute, status, last_used_at, last_request_ip::text, expires_at, revoked_at, revoked_reason,
	created_at, updated_at`

func scanAgencyAPIKey(row pgx.Row) (*models.AgencyAPIKey, error) {
	var k models.AgencyAPIKey
	var perms int64
	err := row.Scan(&k.ID, &k.AgencyID, &k.CreatedBy, &k.Name, &k.KeyPrefix, &k.KeySuffix, &perms, &k.AllowedIPs,
		&k.RateLimitPerMinute, &k.Status, &k.LastUsedAt, &k.LastRequestIP, &k.ExpiresAt, &k.RevokedAt, &k.RevokedReason,
		&k.CreatedAt, &k.UpdatedAt)
	if err != nil {
		return nil, err
	}
	k.Permissions = uint64(perms) //nolint:gosec // stored from a uint64 bitmask
	if k.AllowedIPs == nil {
		k.AllowedIPs = []string{}
	}
	return &k, nil
}

func agencyKeyOrNil(k *models.AgencyAPIKey, err error) (*models.AgencyAPIKey, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return k, err
}

func (r *agencyAPIKeyRepository) Create(ctx context.Context, agencyID, createdBy uuid.UUID, data *models.CreateAgencyAPIKey, prefix, suffix, hash string) (*models.AgencyAPIKey, error) {
	ips := data.AllowedIPs
	if ips == nil {
		ips = []string{}
	}
	return scanAgencyAPIKey(r.db.QueryRow(ctx, `INSERT INTO agency_api_keys
			(agency_id, created_by, name, key_prefix, key_suffix, key_hash, permissions, allowed_ips, rate_limit_per_minute, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, COALESCE($9, 60), $10)
		RETURNING `+agencyAPIKeyColumns,
		agencyID, createdBy, data.Name, prefix, suffix, hash, int64(data.Permissions), ips, data.RateLimitPerMinute, data.ExpiresAt)) //nolint:gosec // bitmask
}

func (r *agencyAPIKeyRepository) GetActiveByHash(ctx context.Context, hash string) (*models.AgencyAPIKey, error) {
	return agencyKeyOrNil(scanAgencyAPIKey(r.db.QueryRow(ctx, `SELECT `+agencyAPIKeyColumns+` FROM agency_api_keys k
		WHERE key_hash = $1 AND status = 'active' AND (expires_at IS NULL OR expires_at > now())
		  AND NOT EXISTS (SELECT 1 FROM users u WHERE u.id = k.created_by AND (u.ban_scope & 1) <> 0)`, hash)))
}

func (r *agencyAPIKeyRepository) Get(ctx context.Context, agencyID, keyID uuid.UUID) (*models.AgencyAPIKey, error) {
	return agencyKeyOrNil(scanAgencyAPIKey(r.db.QueryRow(ctx, `SELECT `+agencyAPIKeyColumns+` FROM agency_api_keys
		WHERE agency_id = $1 AND id = $2`, agencyID, keyID)))
}

func (r *agencyAPIKeyRepository) List(ctx context.Context, agencyID uuid.UUID, limit int, afterAt *time.Time, afterID uuid.UUID) ([]models.AgencyAPIKey, error) {
	rows, err := r.db.Query(ctx, `SELECT `+agencyAPIKeyColumns+` FROM agency_api_keys
		WHERE agency_id = $1 AND ($2::timestamptz IS NULL OR (created_at, id) < ($2, $3))
		ORDER BY created_at DESC, id DESC LIMIT $4`, agencyID, afterAt, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.AgencyAPIKey{}
	for rows.Next() {
		k, err := scanAgencyAPIKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *k)
	}
	return out, rows.Err()
}

func (r *agencyAPIKeyRepository) Update(ctx context.Context, agencyID, keyID uuid.UUID, data *models.UpdateAgencyAPIKey) (*models.AgencyAPIKey, error) {
	return agencyKeyOrNil(scanAgencyAPIKey(r.db.QueryRow(ctx, `UPDATE agency_api_keys SET
			name = COALESCE($3, name),
			allowed_ips = COALESCE($4, allowed_ips),
			updated_at = now()
		WHERE agency_id = $1 AND id = $2 AND status = 'active'
		RETURNING `+agencyAPIKeyColumns, agencyID, keyID, data.Name, data.AllowedIPs)))
}

func (r *agencyAPIKeyRepository) Revoke(ctx context.Context, agencyID, keyID uuid.UUID, reason string) (bool, error) {
	var found bool
	err := r.db.QueryRow(ctx, `WITH revoked AS (
			UPDATE agency_api_keys SET status = 'revoked', revoked_at = now(), revoked_reason = $3, updated_at = now()
			WHERE agency_id = $1 AND id = $2 AND status = 'active' RETURNING id)
		SELECT EXISTS (SELECT 1 FROM revoked) OR EXISTS (SELECT 1 FROM agency_api_keys WHERE agency_id = $1 AND id = $2)`,
		agencyID, keyID, reason).Scan(&found)
	return found, err
}

func (r *agencyAPIKeyRepository) RevokeCreatedBy(ctx context.Context, userID uuid.UUID, reason string) (int64, error) {
	tag, err := r.db.Exec(ctx, `UPDATE agency_api_keys SET status = 'revoked', revoked_at = now(), revoked_reason = $2, updated_at = now()
		WHERE created_by = $1 AND status = 'active'`, userID, reason)
	return tag.RowsAffected(), err
}

func (r *agencyAPIKeyRepository) TouchLastUsed(ctx context.Context, keyID uuid.UUID, ip string) error {
	_, err := r.db.Exec(ctx, `UPDATE agency_api_keys SET last_used_at = now(), last_request_ip = NULLIF($2, '')::inet
		WHERE id = $1 AND (last_used_at IS NULL OR last_used_at < now() - interval '1 minute' OR last_request_ip IS DISTINCT FROM NULLIF($2, '')::inet)`,
		keyID, ip)
	return err
}
