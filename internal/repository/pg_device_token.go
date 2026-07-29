package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/models"
)

// DeviceTokenRepository stores APNs device registrations for mobile push.
type DeviceTokenRepository interface {
	// Upsert registers a token, moving it to userID when the device switched
	// accounts (token is globally unique).
	Upsert(ctx context.Context, userID uuid.UUID, platform, token, environment string) error
	// Delete removes one of the user's tokens (sign-out on that device).
	Delete(ctx context.Context, userID uuid.UUID, token string) error
	// DeleteToken removes a token regardless of owner (APNs said Unregistered).
	DeleteToken(ctx context.Context, token string) error
	// ListByUser returns every registered device for a user.
	ListByUser(ctx context.Context, userID uuid.UUID) ([]models.DeviceToken, error)
}

type deviceTokenRepository struct {
	db *pgxpool.Pool
}

func NewDeviceTokenRepository(db *pgxpool.Pool) DeviceTokenRepository {
	return &deviceTokenRepository{db: db}
}

func (r *deviceTokenRepository) Upsert(ctx context.Context, userID uuid.UUID, platform, token, environment string) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO device_tokens (user_id, platform, token, environment)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (token) DO UPDATE
		SET user_id = EXCLUDED.user_id,
		    platform = EXCLUDED.platform,
		    environment = EXCLUDED.environment,
		    last_seen_at = now()`,
		userID, platform, token, environment)
	return err
}

func (r *deviceTokenRepository) Delete(ctx context.Context, userID uuid.UUID, token string) error {
	_, err := r.db.Exec(ctx, `DELETE FROM device_tokens WHERE user_id = $1 AND token = $2`, userID, token)
	return err
}

func (r *deviceTokenRepository) DeleteToken(ctx context.Context, token string) error {
	_, err := r.db.Exec(ctx, `DELETE FROM device_tokens WHERE token = $1`, token)
	return err
}

func (r *deviceTokenRepository) ListByUser(ctx context.Context, userID uuid.UUID) ([]models.DeviceToken, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, user_id, platform, token, environment, created_at, last_seen_at
		FROM device_tokens
		WHERE user_id = $1
		ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.DeviceToken
	for rows.Next() {
		var t models.DeviceToken
		if err := rows.Scan(&t.ID, &t.UserID, &t.Platform, &t.Token, &t.Environment, &t.CreatedAt, &t.LastSeenAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
