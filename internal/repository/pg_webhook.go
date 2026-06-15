package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/models"
)

// WebhookRepository persists webhook endpoints and per-attempt delivery
// records. The dispatcher polls due deliveries from this repo.
type WebhookRepository interface {
	// Endpoints
	CreateEndpoint(ctx context.Context, endpoint *models.WebhookEndpoint, secret, verificationToken string) error
	UpdateEndpoint(ctx context.Context, endpoint *models.WebhookEndpoint) error
	RotateSecret(ctx context.Context, orgID, endpointID uuid.UUID, newSecret string) error
	DeleteEndpoint(ctx context.Context, orgID, endpointID uuid.UUID) error
	GetEndpoint(ctx context.Context, orgID, endpointID uuid.UUID) (*models.WebhookEndpoint, error)
	GetEndpointByID(ctx context.Context, endpointID uuid.UUID) (*models.WebhookEndpoint, error)
	GetEndpointSecret(ctx context.Context, endpointID uuid.UUID) (string, error)
	ListEndpointsForOrg(ctx context.Context, orgID uuid.UUID) ([]models.WebhookEndpoint, error)
	// MatchingEndpoints returns enabled+verified endpoints for the org that
	// subscribe to the given event type. Firehose events match only when listed
	// explicitly (never via the empty-filter wildcard).
	MatchingEndpoints(ctx context.Context, orgID uuid.UUID, eventType models.WebhookEventType) ([]models.WebhookEndpoint, error)

	// App-materialized endpoints (OAuth app-level webhook subscriptions). One row
	// per (app, org); reconciliation upserts them from the app config + grants.
	UpsertAppEndpoint(ctx context.Context, orgID, appID uuid.UUID, url, secret string, eventTypes []string) error
	DeleteAppEndpointsExcept(ctx context.Context, appID uuid.UUID, keepOrgIDs []uuid.UUID) error
	ListEndpointsByApp(ctx context.Context, appID uuid.UUID) ([]models.WebhookEndpoint, error)
	// ListDeliveriesByApp is the app-developer delivery log: deliveries across all
	// of the app's materialized endpoints (every org that authorized it).
	ListDeliveriesByApp(ctx context.Context, appID uuid.UUID, filter models.WebhookDeliveryFilter) ([]models.WebhookDelivery, bool, error)

	// Verification + ownership / auto-disable bookkeeping.
	ArmVerification(ctx context.Context, orgID, endpointID uuid.UUID, token string) error
	MarkVerified(ctx context.Context, endpointID uuid.UUID, ownershipConfirmed bool) error
	GetVerificationToken(ctx context.Context, endpointID uuid.UUID) (string, error)
	DisableEndpoint(ctx context.Context, endpointID uuid.UUID, reason string) error

	// Deliveries
	EnqueueDelivery(ctx context.Context, delivery *models.WebhookDelivery) error
	ClaimDueDeliveries(ctx context.Context, limit int) ([]models.WebhookDelivery, error)
	// ReclaimStuckDeliveries re-queues deliveries left in_flight past the lease
	// (a worker that crashed after claiming but before settling). Returns count.
	ReclaimStuckDeliveries(ctx context.Context, olderThan time.Duration) (int64, error)
	MarkDelivered(ctx context.Context, id uuid.UUID, responseStatus int) error
	MarkRetry(ctx context.Context, id uuid.UUID, nextAttemptAt time.Time, responseStatus *int, errorReason string, bodyExcerpt string) error
	MarkAbandoned(ctx context.Context, id uuid.UUID, errorReason string) error
	DeferDelivery(ctx context.Context, id uuid.UUID, nextAttemptAt time.Time) error
	GetDelivery(ctx context.Context, orgID, deliveryID uuid.UUID) (*models.WebhookDelivery, error)
	// RedeliverDelivery re-arms an existing delivery row (same event_id) for a
	// fresh attempt cycle. Avoids the UNIQUE(endpoint_id,event_id) conflict that
	// a new insert would hit.
	RedeliverDelivery(ctx context.Context, orgID, deliveryID uuid.UUID) error
	ListDeliveries(ctx context.Context, orgID uuid.UUID, filter models.WebhookDeliveryFilter) ([]models.WebhookDelivery, bool, error)

	UpdateEndpointHealthOnSuccess(ctx context.Context, endpointID uuid.UUID) error
	// UpdateEndpointHealthOnFailure bumps failure counters and stamps
	// first_failure_at when a healthy endpoint starts failing. Returns the new
	// consecutive-failure count and the streak start so the worker can decide
	// whether to auto-disable.
	UpdateEndpointHealthOnFailure(ctx context.Context, endpointID uuid.UUID, reason string) (int, *time.Time, error)

	// Throttle-drop rollup (one upsert per minute-window-trip; flood-safe).
	RecordEventDrop(ctx context.Context, orgID uuid.UUID, eventType models.WebhookEventType) error
	ListEventDrops(ctx context.Context, orgID uuid.UUID, since time.Time) ([]models.WebhookEventDrop, error)
}

type webhookRepository struct {
	db *pgxpool.Pool
}

func NewWebhookRepository(db *pgxpool.Pool) WebhookRepository {
	return &webhookRepository{db: db}
}

// endpointCols is the shared column projection so every read scans identically.
const endpointCols = `id, organization_id, url, description, event_types, enabled,
	last_success_at, last_failure_at, last_failure_reason, consecutive_failures,
	oauth_application_id, created_by, verified_at, ownership_confirmed,
	auto_disabled_at, disabled_reason, created_at, updated_at`

func scanEndpoint(row pgx.Row, e *models.WebhookEndpoint) error {
	return row.Scan(
		&e.ID, &e.OrganizationID, &e.URL, &e.Description, &e.EventTypes, &e.Enabled,
		&e.LastSuccessAt, &e.LastFailureAt, &e.LastFailureReason, &e.ConsecutiveFailures,
		&e.OAuthApplicationID, &e.CreatedBy, &e.VerifiedAt, &e.OwnershipConfirmed,
		&e.AutoDisabledAt, &e.DisabledReason, &e.CreatedAt, &e.UpdatedAt,
	)
}

func (r *webhookRepository) CreateEndpoint(ctx context.Context, endpoint *models.WebhookEndpoint, secret, verificationToken string) error {
	if endpoint.ID == uuid.Nil {
		endpoint.ID = uuid.New()
	}
	endpoint.CreatedAt = time.Now().UTC()
	endpoint.UpdatedAt = endpoint.CreatedAt

	_, err := r.db.Exec(ctx, `
		INSERT INTO webhook_endpoints (
			id, organization_id, url, description, secret, event_types,
			enabled, oauth_application_id, created_by, verification_token,
			created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $11)
	`,
		endpoint.ID, endpoint.OrganizationID, endpoint.URL, endpoint.Description,
		secret, endpoint.EventTypes, endpoint.Enabled,
		endpoint.OAuthApplicationID, endpoint.CreatedBy, verificationToken,
		endpoint.CreatedAt,
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
	err := scanEndpoint(
		r.db.QueryRow(ctx, `SELECT `+endpointCols+` FROM webhook_endpoints WHERE id = $1 AND organization_id = $2`, endpointID, orgID),
		endpoint,
	)
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return endpoint, nil
}

func (r *webhookRepository) GetEndpointByID(ctx context.Context, endpointID uuid.UUID) (*models.WebhookEndpoint, error) {
	endpoint := &models.WebhookEndpoint{}
	err := scanEndpoint(
		r.db.QueryRow(ctx, `SELECT `+endpointCols+` FROM webhook_endpoints WHERE id = $1`, endpointID),
		endpoint,
	)
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
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
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
		return "", errors.New("webhook endpoint not found")
	}
	return secret, err
}

func (r *webhookRepository) GetVerificationToken(ctx context.Context, endpointID uuid.UUID) (string, error) {
	var token string
	err := r.db.QueryRow(ctx,
		`SELECT verification_token FROM webhook_endpoints WHERE id = $1`, endpointID,
	).Scan(&token)
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
		return "", errors.New("webhook endpoint not found")
	}
	return token, err
}

func (r *webhookRepository) ArmVerification(ctx context.Context, orgID, endpointID uuid.UUID, token string) error {
	cmd, err := r.db.Exec(ctx, `
		UPDATE webhook_endpoints
		SET verification_token = $1, verified_at = NULL, ownership_confirmed = false, updated_at = NOW()
		WHERE id = $2 AND organization_id = $3
	`, token, endpointID, orgID)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return errors.New("webhook endpoint not found")
	}
	return nil
}

func (r *webhookRepository) MarkVerified(ctx context.Context, endpointID uuid.UUID, ownershipConfirmed bool) error {
	_, err := r.db.Exec(ctx, `
		UPDATE webhook_endpoints
		SET verified_at = COALESCE(verified_at, NOW()),
		    ownership_confirmed = ownership_confirmed OR $2,
		    auto_disabled_at = NULL,
		    disabled_reason = NULL,
		    first_failure_at = NULL,
		    updated_at = NOW()
		WHERE id = $1
	`, endpointID, ownershipConfirmed)
	return err
}

func (r *webhookRepository) DisableEndpoint(ctx context.Context, endpointID uuid.UUID, reason string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE webhook_endpoints
		SET enabled = false, auto_disabled_at = NOW(), disabled_reason = $2, updated_at = NOW()
		WHERE id = $1
	`, endpointID, reason)
	return err
}

func (r *webhookRepository) ListEndpointsForOrg(ctx context.Context, orgID uuid.UUID) ([]models.WebhookEndpoint, error) {
	rows, err := r.db.Query(ctx, `SELECT `+endpointCols+` FROM webhook_endpoints WHERE organization_id = $1 ORDER BY created_at DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.WebhookEndpoint
	for rows.Next() {
		var e models.WebhookEndpoint
		if err := scanEndpoint(rows, &e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// UpsertAppEndpoint creates or updates the managed webhook endpoint for an
// (app, org) pair. It is created verified + enabled (the org authorized the app
// and the URL is inside the app's allowed domains), so it receives events
// immediately. event_types is the scope-filtered set the org's grant allows.
func (r *webhookRepository) UpsertAppEndpoint(ctx context.Context, orgID, appID uuid.UUID, url, secret string, eventTypes []string) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO webhook_endpoints (
			id, organization_id, url, description, secret, event_types, enabled,
			oauth_application_id, verification_token, verified_at, ownership_confirmed,
			created_at, updated_at
		) VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, true, $6, '', NOW(), true, NOW(), NOW())
		ON CONFLICT (oauth_application_id, organization_id) WHERE oauth_application_id IS NOT NULL
		DO UPDATE SET url = EXCLUDED.url, secret = EXCLUDED.secret,
		    event_types = EXCLUDED.event_types, enabled = true,
		    auto_disabled_at = NULL, disabled_reason = NULL, updated_at = NOW()
	`, orgID, url, "Managed by OAuth app", secret, eventTypes, appID)
	return err
}

// DeleteAppEndpointsExcept removes managed endpoints for the app whose org is
// NOT in keepOrgIDs (i.e. orgs that revoked the app, or all of them when the app
// disables its webhook). An empty keep list deletes every endpoint for the app.
func (r *webhookRepository) DeleteAppEndpointsExcept(ctx context.Context, appID uuid.UUID, keepOrgIDs []uuid.UUID) error {
	if len(keepOrgIDs) == 0 {
		_, err := r.db.Exec(ctx, `DELETE FROM webhook_endpoints WHERE oauth_application_id = $1`, appID)
		return err
	}
	_, err := r.db.Exec(ctx,
		`DELETE FROM webhook_endpoints WHERE oauth_application_id = $1 AND NOT (organization_id = ANY($2))`,
		appID, keepOrgIDs)
	return err
}

func (r *webhookRepository) ListEndpointsByApp(ctx context.Context, appID uuid.UUID) ([]models.WebhookEndpoint, error) {
	rows, err := r.db.Query(ctx, `SELECT `+endpointCols+` FROM webhook_endpoints WHERE oauth_application_id = $1 ORDER BY created_at DESC`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.WebhookEndpoint
	for rows.Next() {
		var e models.WebhookEndpoint
		if err := scanEndpoint(rows, &e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *webhookRepository) ListDeliveriesByApp(ctx context.Context, appID uuid.UUID, filter models.WebhookDeliveryFilter) ([]models.WebhookDelivery, bool, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	args := []any{appID}
	where := `endpoint_id IN (SELECT id FROM webhook_endpoints WHERE oauth_application_id = $1)`
	if filter.Status != "" {
		args = append(args, filter.Status)
		where += fmt.Sprintf(" AND status = $%d", len(args))
	}
	if filter.EventType != "" {
		args = append(args, filter.EventType)
		where += fmt.Sprintf(" AND event_type = $%d", len(args))
	}
	args = append(args, limit+1, filter.Offset)
	q := `SELECT ` + deliveryCols + ` FROM webhook_deliveries WHERE ` + where +
		fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	rows, err := r.db.Query(ctx, q, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var out []models.WebhookDelivery
	for rows.Next() {
		var d models.WebhookDelivery
		if err := scanDelivery(rows, &d); err != nil {
			return nil, false, err
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	hasMore := len(out) > limit
	if hasMore {
		out = out[:limit]
	}
	return out, hasMore, nil
}

func (r *webhookRepository) MatchingEndpoints(ctx context.Context, orgID uuid.UUID, eventType models.WebhookEventType) ([]models.WebhookEndpoint, error) {
	// Only enabled, verified endpoints receive events. Empty event_types matches
	// everything EXCEPT firehose events, which require an explicit subscription.
	isFirehose := models.IsFirehoseEvent(eventType)
	rows, err := r.db.Query(ctx, `
		SELECT `+endpointCols+`
		FROM webhook_endpoints
		WHERE organization_id = $1
		  AND enabled
		  AND verified_at IS NOT NULL
		  AND (event_types @> ARRAY[$2]::text[] OR (cardinality(event_types) = 0 AND NOT $3))
	`, orgID, string(eventType), isFirehose)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.WebhookEndpoint
	for rows.Next() {
		var e models.WebhookEndpoint
		if err := scanEndpoint(rows, &e); err != nil {
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

const deliveryCols = `id, endpoint_id, organization_id, event_type, event_id, payload,
	status, attempt_count, max_attempts, next_attempt_at, last_attempt_at,
	response_status, response_body_excerpt, error_reason, created_at, updated_at`

func scanDelivery(row pgx.Row, d *models.WebhookDelivery) error {
	var status string
	if err := row.Scan(
		&d.ID, &d.EndpointID, &d.OrganizationID, &d.EventType, &d.EventID,
		&d.Payload, &status, &d.AttemptCount, &d.MaxAttempts,
		&d.NextAttemptAt, &d.LastAttemptAt,
		&d.ResponseStatus, &d.ResponseBodyExcerpt, &d.ErrorReason,
		&d.CreatedAt, &d.UpdatedAt,
	); err != nil {
		return err
	}
	d.Status = models.WebhookDeliveryStatus(status)
	return nil
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
		if err := scanDelivery(rows, &d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *webhookRepository) ReclaimStuckDeliveries(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-olderThan)
	cmd, err := r.db.Exec(ctx, `
		UPDATE webhook_deliveries
		SET status = 'pending', next_attempt_at = NOW(), updated_at = NOW()
		WHERE status = 'in_flight' AND updated_at < $1
	`, cutoff)
	if err != nil {
		return 0, err
	}
	return cmd.RowsAffected(), nil
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

// DeferDelivery pushes a claimed delivery back to pending WITHOUT counting the
// attempt (used by the per-endpoint rate limiter): the work was never sent.
func (r *webhookRepository) DeferDelivery(ctx context.Context, id uuid.UUID, nextAttemptAt time.Time) error {
	_, err := r.db.Exec(ctx, `
		UPDATE webhook_deliveries
		SET status = 'pending',
		    attempt_count = GREATEST(attempt_count - 1, 0),
		    next_attempt_at = $1,
		    updated_at = NOW()
		WHERE id = $2
	`, nextAttemptAt, id)
	return err
}

func (r *webhookRepository) GetDelivery(ctx context.Context, orgID, deliveryID uuid.UUID) (*models.WebhookDelivery, error) {
	d := &models.WebhookDelivery{}
	err := scanDelivery(
		r.db.QueryRow(ctx, `SELECT `+deliveryCols+` FROM webhook_deliveries WHERE id = $1 AND organization_id = $2`, deliveryID, orgID),
		d,
	)
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return d, nil
}

func (r *webhookRepository) RedeliverDelivery(ctx context.Context, orgID, deliveryID uuid.UUID) error {
	cmd, err := r.db.Exec(ctx, `
		UPDATE webhook_deliveries
		SET status = 'pending',
		    attempt_count = 0,
		    next_attempt_at = NOW(),
		    error_reason = NULL,
		    response_status = NULL,
		    response_body_excerpt = NULL,
		    updated_at = NOW()
		WHERE id = $1 AND organization_id = $2
	`, deliveryID, orgID)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return errors.New("webhook delivery not found")
	}
	return nil
}

func (r *webhookRepository) ListDeliveries(ctx context.Context, orgID uuid.UUID, filter models.WebhookDeliveryFilter) ([]models.WebhookDelivery, bool, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	// Build the WHERE incrementally so optional filters stay index-friendly.
	args := []any{orgID}
	where := `organization_id = $1`
	if filter.EndpointID != nil {
		args = append(args, *filter.EndpointID)
		where += fmt.Sprintf(" AND endpoint_id = $%d", len(args))
	}
	if filter.Status != "" {
		args = append(args, filter.Status)
		where += fmt.Sprintf(" AND status = $%d", len(args))
	}
	if filter.EventType != "" {
		args = append(args, filter.EventType)
		where += fmt.Sprintf(" AND event_type = $%d", len(args))
	}
	// Fetch one extra row to compute has_more without a count query.
	args = append(args, limit+1, filter.Offset)
	q := `SELECT ` + deliveryCols + ` FROM webhook_deliveries WHERE ` + where +
		fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))

	rows, err := r.db.Query(ctx, q, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	var out []models.WebhookDelivery
	for rows.Next() {
		var d models.WebhookDelivery
		if err := scanDelivery(rows, &d); err != nil {
			return nil, false, err
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	hasMore := len(out) > limit
	if hasMore {
		out = out[:limit]
	}
	return out, hasMore, nil
}

func (r *webhookRepository) UpdateEndpointHealthOnSuccess(ctx context.Context, endpointID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `
		UPDATE webhook_endpoints
		SET last_success_at = NOW(),
		    consecutive_failures = 0,
		    first_failure_at = NULL,
		    last_failure_reason = NULL,
		    updated_at = NOW()
		WHERE id = $1
	`, endpointID)
	return err
}

func (r *webhookRepository) UpdateEndpointHealthOnFailure(ctx context.Context, endpointID uuid.UUID, reason string) (int, *time.Time, error) {
	var consecutive int
	var firstFailure *time.Time
	err := r.db.QueryRow(ctx, `
		UPDATE webhook_endpoints
		SET last_failure_at = NOW(),
		    last_failure_reason = $1,
		    consecutive_failures = consecutive_failures + 1,
		    first_failure_at = COALESCE(first_failure_at, NOW()),
		    updated_at = NOW()
		WHERE id = $2
		RETURNING consecutive_failures, first_failure_at
	`, reason, endpointID).Scan(&consecutive, &firstFailure)
	if err != nil {
		return 0, nil, err
	}
	return consecutive, firstFailure, nil
}

func (r *webhookRepository) RecordEventDrop(ctx context.Context, orgID uuid.UUID, eventType models.WebhookEventType) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO webhook_event_drops (organization_id, event_type, day, dropped_windows, last_dropped_at)
		VALUES ($1, $2, CURRENT_DATE, 1, NOW())
		ON CONFLICT (organization_id, event_type, day)
		DO UPDATE SET dropped_windows = webhook_event_drops.dropped_windows + 1, last_dropped_at = NOW()
	`, orgID, string(eventType))
	return err
}

func (r *webhookRepository) ListEventDrops(ctx context.Context, orgID uuid.UUID, since time.Time) ([]models.WebhookEventDrop, error) {
	rows, err := r.db.Query(ctx, `
		SELECT event_type, day, dropped_windows, last_dropped_at
		FROM webhook_event_drops
		WHERE organization_id = $1 AND day >= $2
		ORDER BY day DESC, dropped_windows DESC
	`, orgID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.WebhookEventDrop{}
	for rows.Next() {
		var d models.WebhookEventDrop
		if err := rows.Scan(&d.EventType, &d.Day, &d.DroppedWindows, &d.LastDroppedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
