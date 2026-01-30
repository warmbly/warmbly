package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TrackingDedupeRepository handles deduplication of tracking events at the consumer level
type TrackingDedupeRepository interface {
	IsProcessed(ctx context.Context, taskID uuid.UUID, eventType, urlHash string) (bool, error)
	MarkProcessed(ctx context.Context, taskID uuid.UUID, eventType, urlHash string) error
	Cleanup(ctx context.Context, olderThanDays int) (int64, error)
}

type trackingDedupeRepository struct {
	db *pgxpool.Pool
}

// NewTrackingDedupeRepository creates a new tracking dedupe repository
func NewTrackingDedupeRepository(db *pgxpool.Pool) TrackingDedupeRepository {
	return &trackingDedupeRepository{db: db}
}

// IsProcessed checks if a tracking event has already been processed
func (r *trackingDedupeRepository) IsProcessed(ctx context.Context, taskID uuid.UUID, eventType, urlHash string) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1
			FROM tracking_events_processed
			WHERE task_id = $1
			  AND event_type = $2
			  AND COALESCE(url_hash, '') = COALESCE($3, '')
		)
	`

	var exists bool
	err := r.db.QueryRow(ctx, query, taskID, eventType, urlHash).Scan(&exists)
	return exists, err
}

// MarkProcessed marks a tracking event as processed
func (r *trackingDedupeRepository) MarkProcessed(ctx context.Context, taskID uuid.UUID, eventType, urlHash string) error {
	query := `
		INSERT INTO tracking_events_processed (task_id, event_type, url_hash, processed_at)
		VALUES ($1, $2, NULLIF($3, ''), NOW())
		ON CONFLICT (task_id, event_type, COALESCE(url_hash, ''))
		DO NOTHING
	`

	_, err := r.db.Exec(ctx, query, taskID, eventType, urlHash)
	return err
}

// Cleanup removes old processed tracking events (for scheduled job)
func (r *trackingDedupeRepository) Cleanup(ctx context.Context, olderThanDays int) (int64, error) {
	query := `
		DELETE FROM tracking_events_processed
		WHERE processed_at < NOW() - $1 * INTERVAL '1 day'
	`

	result, err := r.db.Exec(ctx, query, olderThanDays)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected(), nil
}
