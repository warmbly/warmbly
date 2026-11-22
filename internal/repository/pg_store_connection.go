package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/pkg/encrypt"
)

// StoreConnection is the GraphKit connection an operator saved in the admin panel, secrets in the clear.
type StoreConnection struct {
	APIURL                string
	APIKey                string
	WebhookSecret         string
	WebhookSecretPrevious string
	// WebhookSecretRotatedAt is when WebhookSecretPrevious was replaced as the current secret.
	WebhookSecretRotatedAt *time.Time
	UpdatedBy              *uuid.UUID
	UpdatedAt              time.Time
}

// ErrStoreConnectionUnsealed refuses to store a GraphKit secret without CREDENTIALS_ENCRYPTION_KEY.
var ErrStoreConnectionUnsealed = errors.New("store connection: no instance key to seal with")

// StoreConnectionRepository keeps the one stored GraphKit connection, its secrets sealed under CREDENTIALS_ENCRYPTION_KEY.
type StoreConnectionRepository struct {
	pool   *pgxpool.Pool
	sealer *encrypt.Encrypter
}

// NewStoreConnectionRepository builds it; a nil sealer reads nothing and refuses every write.
func NewStoreConnectionRepository(pool *pgxpool.Pool, sealer *encrypt.Encrypter) *StoreConnectionRepository {
	return &StoreConnectionRepository{pool: pool, sealer: sealer}
}

// CanSeal reports whether a connection can be stored here at all.
func (r *StoreConnectionRepository) CanSeal() bool { return r != nil && r.sealer != nil }

// GetConnection is the stored connection, nil when none is.
func (r *StoreConnectionRepository) GetConnection(ctx context.Context) (*StoreConnection, error) {
	if !r.CanSeal() {
		return nil, nil
	}
	var c StoreConnection
	var key, secret, previous *string
	err := r.pool.QueryRow(ctx, `SELECT api_url, api_key, webhook_secret, webhook_secret_previous, webhook_secret_rotated_at, updated_by, updated_at
		FROM store_graphkit_connection WHERE id`).Scan(&c.APIURL, &key, &secret, &previous, &c.WebhookSecretRotatedAt, &c.UpdatedBy, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	for _, f := range []struct {
		sealed *string
		out    *string
	}{{key, &c.APIKey}, {secret, &c.WebhookSecret}, {previous, &c.WebhookSecretPrevious}} {
		if f.sealed == nil || *f.sealed == "" {
			continue
		}
		plain, err := r.sealer.Decrypt(*f.sealed)
		if err != nil {
			return nil, errors.New("store connection: a stored secret does not open under CREDENTIALS_ENCRYPTION_KEY")
		}
		*f.out = plain
	}
	return &c, nil
}

// PutConnection replaces the stored connection.
func (r *StoreConnectionRepository) PutConnection(ctx context.Context, c StoreConnection) error {
	if !r.CanSeal() {
		return ErrStoreConnectionUnsealed
	}
	seal := func(v string) (*string, error) {
		if v == "" {
			return nil, nil
		}
		s, err := r.sealer.Encrypt(v)
		return &s, err
	}
	key, err := seal(c.APIKey)
	if err != nil {
		return err
	}
	secret, err := seal(c.WebhookSecret)
	if err != nil {
		return err
	}
	previous, err := seal(c.WebhookSecretPrevious)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `INSERT INTO store_graphkit_connection (id, api_url, api_key, webhook_secret, webhook_secret_previous, webhook_secret_rotated_at, updated_by, updated_at)
		VALUES (true, $1, $2, $3, $4, $5, $6, now())
		ON CONFLICT (id) DO UPDATE SET api_url = EXCLUDED.api_url, api_key = EXCLUDED.api_key, webhook_secret = EXCLUDED.webhook_secret,
			webhook_secret_previous = EXCLUDED.webhook_secret_previous, webhook_secret_rotated_at = EXCLUDED.webhook_secret_rotated_at,
			updated_by = EXCLUDED.updated_by, updated_at = now()`,
		c.APIURL, key, secret, previous, c.WebhookSecretRotatedAt, c.UpdatedBy)
	return err
}

// DeleteConnection removes the stored connection and its secrets.
func (r *StoreConnectionRepository) DeleteConnection(ctx context.Context) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM store_graphkit_connection`)
	return err
}

// CountLivePurchases counts mailboxes not yet ended and domains not yet expired, for one workspace or, with nil, the instance.
func (r *StoreConnectionRepository) CountLivePurchases(ctx context.Context, orgID *uuid.UUID) (mailboxes, domains int, err error) {
	err = r.pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM store_mailboxes WHERE status NOT IN ('cancelled', 'failed') AND ($1::uuid IS NULL OR organization_id = $1)),
		(SELECT count(*) FROM store_domains WHERE source = 'purchased' AND status NOT IN ('failed', 'removed', 'transferring_out', 'transferred_out', 'expired')
			AND (expires_at IS NULL OR expires_at > now()) AND ($1::uuid IS NULL OR organization_id = $1))`, orgID).Scan(&mailboxes, &domains)
	return mailboxes, domains, err
}
