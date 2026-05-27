package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/models"
)

// WebhookRepository persists webhook endpoints and per-attempt delivery
// records. The dispatcher polls due deliveries from this repo.
type WebhookRepository interface {
	// Endpoints
	CreateEndpoint(ctx context.Context, endpoint *models.WebhookEndpoint, secret string) error
	UpdateEndpoint(ctx context.Context, endpoint *models.WebhookEndpoint) error
	RotateSecret(ctx context.Context, orgID, endpointID uuid.UUID, newSecret string) error
	DeleteEndpoint(ctx context.Context, orgID, endpointID uuid.UUID) error
	GetEndpoint(ctx context.Context, orgID, endpointID uuid.UUID) (*models.WebhookEndpoint, error)
	GetEndpointSecret(ctx context.Context, endpointID uuid.UUID) (string, error)
	ListEndpointsForOrg(ctx context.Context, orgID uuid.UUID) ([]models.WebhookEndpoint, error)
	// MatchingEndpoints returns enabled endpoints for the org that subscribe
	// to the given event type (either explicitly or via the wildcard empty filter).
	MatchingEndpoints(ctx context.Context, orgID uuid.UUID, eventType models.WebhookEventType) ([]models.WebhookEndpoint, error)

	// Deliveries
	EnqueueDelivery(ctx context.Context, delivery *models.WebhookDelivery) error
	ClaimDueDeliveries(ctx context.Context, limit int) ([]models.WebhookDelivery, error)
	MarkDelivered(ctx context.Context, id uuid.UUID, responseStatus int) error
	MarkRetry(ctx context.Context, id uuid.UUID, nextAttemptAt time.Time, responseStatus *int, errorReason string, bodyExcerpt string) error
	MarkAbandoned(ctx context.Context, id uuid.UUID, errorReason string) error
	ListDeliveriesForEndpoint(ctx context.Context, orgID, endpointID uuid.UUID, limit int) ([]models.WebhookDelivery, error)
	UpdateEndpointHealthOnSuccess(ctx context.Context, endpointID uuid.UUID) error
	UpdateEndpointHealthOnFailure(ctx context.Context, endpointID uuid.UUID, reason string) error
}

type webhookRepository struct {
	db *pgxpool.Pool
}

func NewWebhookRepository(db *pgxpool.Pool) WebhookRepository {
	return &webhookRepository{db: db}
}

func (r *webhookRepository) CreateEndpoint(ctx context.Context, endpoint *models.WebhookEndpoint, secret string) error {
	if endpoint.ID == uuid.Nil {
		endpoint.ID = uuid.New()
	}
	endpoint.CreatedAt = time.Now().UTC()
	endpoint.UpdatedAt = endpoint.CreatedAt

	_, err := r.db.Exec(ctx, `
		INSERT INTO webhook_endpoints (
			id, organization_id, url, description, secret, event_types,
			enabled, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)
	`,
		endpoint.ID, endpoint.OrganizationID, endpoint.URL, endpoint.Description,
		secret, endpoint.EventTypes, endpoint.Enabled, endpoint.CreatedAt,
	)
	return err
}

func (r *webhookRepository) UpdateEndpoint(ctx context.Context, endpoint *models.WebhookEndpoint) error {
	endpoint.UpdatedAt = time.Now().UTC()
	cmd, err := r.db.Exec(ctx, `
		UPDATE webhook_endpoints
		SET url = $1, description = $2, event_types = $3, enabled = $4, updated_at = $5
		WHERE id = $6 AND organization_id = $7
	`,
		endpoint.URL, endpoint.Description, endpoint.EventTypes, endpoint.Enabled,
		endpoint.UpdatedAt, endpoint.ID, endpoint.OrganizationID,
	)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return errors.New("webhook endpoint not found")
	}
	return nil
}

func (r *webhookRepository) RotateSecret(ctx context.Context, orgID, endpointID uuid.UUID, newSecret string) error {
	cmd, err := r.db.Exec(ctx,
		`UPDATE webhook_endpoints SET secret = $1, updated_at = NOW() WHERE id = $2 AND organization_id = $3`,
		newSecret, endpointID, orgID,
	)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return errors.New("webhook endpoint not found")
	}
	return nil
}

func (r *webhookRepository) DeleteEndpoint(ctx context.Context, orgID, endpointID uuid.UUID) error {
	cmd, err := r.db.Exec(ctx,
		`DELETE FROM webhook_endpoints WHERE id = $1 AND organization_id = $2`,
		endpointID, orgID,
	)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return errors.New("webhook endpoint not found")
	}
	return nil
}

func (r *webhookRepository) GetEndpoint(ctx context.Context, orgID, endpointID uuid.UUID) (*models.WebhookEndpoint, error) {
	endpoint := &models.WebhookEndpoint{}
	err := r.db.QueryRow(ctx, `
		SELECT id, organization_id, url, description, event_types, enabled,
		       last_success_at, last_failure_at, last_failure_reason, consecutive_failures,
		       created_at, updated_at
		FROM webhook_endpoints WHERE id = $1 AND organization_id = $2
	`, endpointID, orgID).Scan(
		&endpoint.ID, &endpoint.OrganizationID, &endpoint.URL, &endpoint.Description,
		&endpoint.EventTypes, &endpoint.Enabled,
		&endpoint.LastSuccessAt, &endpoint.LastFailureAt, &endpoint.LastFailureReason,
		&endpoint.ConsecutiveFailures,
		&endpoint.CreatedAt, &endpoint.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return endpoint, nil
}

func (r *webhookRepository) GetEndpointSecret(ctx context.Context, endpointID uuid.UUID) (string, error) {
	var secret string
	err := r.db.QueryRow(ctx,
		`SELECT secret FROM webhook_endpoints WHERE id = $1`, endpointID,
	).Scan(&secret)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errors.New("webhook endpoint not found")
	}
	return secret, err
}

func (r *webhookRepository) ListEndpointsForOrg(ctx context.Context, orgID uuid.UUID) ([]models.WebhookEndpoint, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, organization_id, url, description, event_types, enabled,
		       last_success_at, last_failure_at, last_failure_reason, consecutive_failures,
		       created_at, updated_at
		FROM webhook_endpoints WHERE organization_id = $1 ORDER BY created_at DESC
	`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.WebhookEndpoint
	for rows.Next() {
		var e models.WebhookEndpoint
		if err := rows.Scan(
			&e.ID, &e.OrganizationID, &e.URL, &e.Description,
			&e.EventTypes, &e.Enabled,
			&e.LastSuccessAt, &e.LastFailureAt, &e.LastFailureReason,
			&e.ConsecutiveFailures,
			&e.CreatedAt, &e.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *webhookRepository) MatchingEndpoints(ctx context.Context, orgID uuid.UUID, eventType models.WebhookEventType) ([]models.WebhookEndpoint, error) {
	// Index-friendly: empty event_types array matches everything, otherwise
	// the GIN index on event_types covers @> lookups.
	rows, err := r.db.Query(ctx, `
		SELECT id, organization_id, url, description, event_types, enabled,
		       last_success_at, last_failure_at, last_failure_reason, consecutive_failures,
		       created_at, updated_at
		FROM webhook_endpoints
		WHERE organization_id = $1
		  AND enabled
		  AND (cardinality(event_types) = 0 OR event_types @> ARRAY[$2]::text[])
	`, orgID, string(eventType))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.WebhookEndpoint
	for rows.Next() {
		var e models.WebhookEndpoint
		if err := rows.Scan(
			&e.ID, &e.OrganizationID, &e.URL, &e.Description,
			&e.EventTypes, &e.Enabled,
			&e.LastSuccessAt, &e.LastFailureAt, &e.LastFailureReason,
			&e.ConsecutiveFailures,
			&e.CreatedAt, &e.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *webhookRepository) EnqueueDelivery(ctx context.Context, delivery *models.WebhookDelivery) error {
	if delivery.ID == uuid.Nil {
		delivery.ID = uuid.New()
	}
	if delivery.MaxAttempts == 0 {
		delivery.MaxAttempts = 8
	}
	if delivery.NextAttemptAt.IsZero() {
		delivery.NextAttemptAt = time.Now().UTC()
	}
	if delivery.Status == "" {
		delivery.Status = models.WebhookDeliveryPending
	}
	_, err := r.db.Exec(ctx, `
		INSERT INTO webhook_deliveries (
			id, endpoint_id, organization_id, event_type, event_id, payload,
			status, attempt_count, max_attempts, next_attempt_at, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW(), NOW())
	`,
		delivery.ID, delivery.EndpointID, delivery.OrganizationID,
		delivery.EventType, delivery.EventID, json.RawMessage(delivery.Payload),
		string(delivery.Status), delivery.AttemptCount, delivery.MaxAttempts,
		delivery.NextAttemptAt,
	)
	return err
}

// ClaimDueDeliveries atomically picks up to `limit` deliveries due for
// dispatch, marks them in_flight, and returns them. The SKIP LOCKED clause
// lets multiple dispatcher workers run concurrently without colliding.
func (r *webhookRepository) ClaimDueDeliveries(ctx context.Context, limit int) ([]models.WebhookDelivery, error) {
	rows, err := r.db.Query(ctx, `
		WITH claimed AS (
			SELECT id FROM webhook_deliveries
			WHERE status IN ('pending')
			  AND next_attempt_at <= NOW()
			ORDER BY next_attempt_at ASC
			FOR UPDATE SKIP LOCKED
			LIMIT $1
		)
		UPDATE webhook_deliveries d
		SET status = 'in_flight',
		    attempt_count = attempt_count + 1,
		    last_attempt_at = NOW(),
		    updated_at = NOW()
		FROM claimed
		WHERE d.id = claimed.id
		RETURNING d.id, d.endpoint_id, d.organization_id, d.event_type, d.event_id,
		          d.payload, d.status, d.attempt_count, d.max_attempts,
		          d.next_attempt_at, d.last_attempt_at,
		          d.response_status, d.response_body_excerpt, d.error_reason,
		          d.created_at, d.updated_at
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.WebhookDelivery
	for rows.Next() {
		var d models.WebhookDelivery
		var status string
		if err := rows.Scan(
			&d.ID, &d.EndpointID, &d.OrganizationID, &d.EventType, &d.EventID,
			&d.Payload, &status, &d.AttemptCount, &d.MaxAttempts,
			&d.NextAttemptAt, &d.LastAttemptAt,
			&d.ResponseStatus, &d.ResponseBodyExcerpt, &d.ErrorReason,
			&d.CreatedAt, &d.UpdatedAt,
		); err != nil {
			return nil, err
		}
		d.Status = models.WebhookDeliveryStatus(status)
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *webhookRepository) MarkDelivered(ctx context.Context, id uuid.UUID, responseStatus int) error {
	_, err := r.db.Exec(ctx, `
		UPDATE webhook_deliveries
		SET status = 'delivered',
		    response_status = $1,
		    error_reason = NULL,
		    updated_at = NOW()
		WHERE id = $2
	`, responseStatus, id)
	return err
}

func (r *webhookRepository) MarkRetry(ctx context.Context, id uuid.UUID, nextAttemptAt time.Time, responseStatus *int, errorReason, bodyExcerpt string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE webhook_deliveries
		SET status = 'pending',
		    next_attempt_at = $1,
		    response_status = $2,
		    response_body_excerpt = NULLIF($3, ''),
		    error_reason = NULLIF($4, ''),
		    updated_at = NOW()
		WHERE id = $5
	`, nextAttemptAt, responseStatus, bodyExcerpt, errorReason, id)
	return err
}

func (r *webhookRepository) MarkAbandoned(ctx context.Context, id uuid.UUID, errorReason string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE webhook_deliveries
		SET status = 'abandoned',
		    error_reason = $1,
		    updated_at = NOW()
		WHERE id = $2
	`, errorReason, id)
	return err
}

func (r *webhookRepository) ListDeliveriesForEndpoint(ctx context.Context, orgID, endpointID uuid.UUID, limit int) ([]models.WebhookDelivery, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.db.Query(ctx, `
		SELECT id, endpoint_id, organization_id, event_type, event_id, payload,
		       status, attempt_count, max_attempts, next_attempt_at, last_attempt_at,
		       response_status, response_body_excerpt, error_reason,
		       created_at, updated_at
		FROM webhook_deliveries
		WHERE organization_id = $1 AND endpoint_id = $2
		ORDER BY created_at DESC
		LIMIT $3
	`, orgID, endpointID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.WebhookDelivery
	for rows.Next() {
		var d models.WebhookDelivery
		var status string
		if err := rows.Scan(
			&d.ID, &d.EndpointID, &d.OrganizationID, &d.EventType, &d.EventID,
			&d.Payload, &status, &d.AttemptCount, &d.MaxAttempts,
			&d.NextAttemptAt, &d.LastAttemptAt,
			&d.ResponseStatus, &d.ResponseBodyExcerpt, &d.ErrorReason,
			&d.CreatedAt, &d.UpdatedAt,
		); err != nil {
			return nil, err
		}
		d.Status = models.WebhookDeliveryStatus(status)
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *webhookRepository) UpdateEndpointHealthOnSuccess(ctx context.Context, endpointID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `
		UPDATE webhook_endpoints
		SET last_success_at = NOW(),
		    consecutive_failures = 0,
		    last_failure_reason = NULL,
		    updated_at = NOW()
		WHERE id = $1
	`, endpointID)
	return err
}

func (r *webhookRepository) UpdateEndpointHealthOnFailure(ctx context.Context, endpointID uuid.UUID, reason string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE webhook_endpoints
		SET last_failure_at = NOW(),
		    last_failure_reason = $1,
		    consecutive_failures = consecutive_failures + 1,
		    updated_at = NOW()
		WHERE id = $2
	`, reason, endpointID)
	return err
}
