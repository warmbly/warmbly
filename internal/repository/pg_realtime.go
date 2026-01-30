package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
)

// RealtimeRepository handles realtime event persistence
type RealtimeRepository interface {
	// Create stores a new realtime event
	Create(ctx context.Context, event *models.RealtimeEvent) error

	// GetPendingForUser retrieves undelivered events for a user
	GetPendingForUser(ctx context.Context, userID uuid.UUID, limit int) ([]models.RealtimeEvent, error)

	// GetPendingForOrg retrieves undelivered events for an organization
	GetPendingForOrg(ctx context.Context, orgID uuid.UUID, limit int) ([]models.RealtimeEvent, error)

	// MarkDelivered marks events as delivered
	MarkDelivered(ctx context.Context, eventIDs []uuid.UUID) error

	// CleanupExpired removes expired events
	CleanupExpired(ctx context.Context) (int64, error)

	// GetEventsSince retrieves events since a timestamp for catch-up
	GetEventsSince(ctx context.Context, userID uuid.UUID, since time.Time, limit int) ([]models.RealtimeEvent, error)
}

type realtimeRepository struct {
	db *pgxpool.Pool
}

// NewRealtimeRepository creates a new realtime repository
func NewRealtimeRepository(db *pgxpool.Pool) RealtimeRepository {
	return &realtimeRepository{db: db}
}

// Create stores a new realtime event
func (r *realtimeRepository) Create(ctx context.Context, event *models.RealtimeEvent) error {
	query := `
		INSERT INTO realtime_events (id, user_id, org_id, event_type, priority, payload, delivered, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`

	if event.ID == uuid.Nil {
		event.ID = uuid.New()
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}
	if event.ExpiresAt.IsZero() {
		event.ExpiresAt = event.CreatedAt.Add(24 * time.Hour)
	}

	_, err := r.db.Exec(ctx, query,
		event.ID,
		event.UserID,
		event.OrgID,
		event.EventType,
		event.Priority,
		event.Payload,
		event.Delivered,
		event.CreatedAt,
		event.ExpiresAt,
	)
	return err
}

// GetPendingForUser retrieves undelivered events for a user
func (r *realtimeRepository) GetPendingForUser(ctx context.Context, userID uuid.UUID, limit int) ([]models.RealtimeEvent, error) {
	query := `
		SELECT id, user_id, org_id, event_type, priority, payload, delivered, created_at, expires_at
		FROM realtime_events
		WHERE user_id = $1
		  AND delivered = FALSE
		  AND expires_at > NOW()
		ORDER BY created_at ASC
		LIMIT $2
	`

	rows, err := r.db.Query(ctx, query, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []models.RealtimeEvent
	for rows.Next() {
		var e models.RealtimeEvent
		if err := rows.Scan(
			&e.ID, &e.UserID, &e.OrgID, &e.EventType, &e.Priority,
			&e.Payload, &e.Delivered, &e.CreatedAt, &e.ExpiresAt,
		); err != nil {
			return nil, err
		}
		events = append(events, e)
	}

	return events, rows.Err()
}

// GetPendingForOrg retrieves undelivered events for an organization
func (r *realtimeRepository) GetPendingForOrg(ctx context.Context, orgID uuid.UUID, limit int) ([]models.RealtimeEvent, error) {
	query := `
		SELECT id, user_id, org_id, event_type, priority, payload, delivered, created_at, expires_at
		FROM realtime_events
		WHERE org_id = $1
		  AND delivered = FALSE
		  AND expires_at > NOW()
		ORDER BY created_at ASC
		LIMIT $2
	`

	rows, err := r.db.Query(ctx, query, orgID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []models.RealtimeEvent
	for rows.Next() {
		var e models.RealtimeEvent
		if err := rows.Scan(
			&e.ID, &e.UserID, &e.OrgID, &e.EventType, &e.Priority,
			&e.Payload, &e.Delivered, &e.CreatedAt, &e.ExpiresAt,
		); err != nil {
			return nil, err
		}
		events = append(events, e)
	}

	return events, rows.Err()
}

// MarkDelivered marks events as delivered
func (r *realtimeRepository) MarkDelivered(ctx context.Context, eventIDs []uuid.UUID) error {
	if len(eventIDs) == 0 {
		return nil
	}

	query := `UPDATE realtime_events SET delivered = TRUE WHERE id = ANY($1)`
	_, err := r.db.Exec(ctx, query, eventIDs)
	return err
}

// CleanupExpired removes expired events
func (r *realtimeRepository) CleanupExpired(ctx context.Context) (int64, error) {
	query := `DELETE FROM realtime_events WHERE expires_at < NOW()`
	result, err := r.db.Exec(ctx, query)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

// GetEventsSince retrieves events since a timestamp for catch-up
func (r *realtimeRepository) GetEventsSince(ctx context.Context, userID uuid.UUID, since time.Time, limit int) ([]models.RealtimeEvent, error) {
	query := `
		SELECT id, user_id, org_id, event_type, priority, payload, delivered, created_at, expires_at
		FROM realtime_events
		WHERE user_id = $1
		  AND created_at > $2
		  AND expires_at > NOW()
		ORDER BY created_at ASC
		LIMIT $3
	`

	rows, err := r.db.Query(ctx, query, userID, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []models.RealtimeEvent
	for rows.Next() {
		var e models.RealtimeEvent
		if err := rows.Scan(
			&e.ID, &e.UserID, &e.OrgID, &e.EventType, &e.Priority,
			&e.Payload, &e.Delivered, &e.CreatedAt, &e.ExpiresAt,
		); err != nil {
			return nil, err
		}
		events = append(events, e)
	}

	return events, rows.Err()
}

// CreateCriticalEvent is a helper to create and persist a critical event
func CreateCriticalEvent(ctx context.Context, repo RealtimeRepository, userID uuid.UUID, orgID *uuid.UUID, eventType models.RealtimeEventType, payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	event := &models.RealtimeEvent{
		ID:        uuid.New(),
		UserID:    userID,
		OrgID:     orgID,
		EventType: eventType,
		Priority:  models.PriorityCritical,
		Payload:   data,
		Delivered: false,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}

	return repo.Create(ctx, event)
}
