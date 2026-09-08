package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/utils/paging"
)

// AdminSendsRepository reads the send outcome loop and the task queues across
// every workspace: reservations no worker answered, dead letters, task
// failures and customer webhook delivery health.
type AdminSendsRepository interface {
	// InFlight lists reserved sends without a result, oldest first, capped at
	// limit, with the age buckets computed against reclaimAfter.
	InFlight(ctx context.Context, reclaimAfter time.Duration, limit int) (*models.AdminInFlightResult, error)
	// ListDeadLetters pages task_dead_letters newest first. status "" means
	// every status; cursor is the id of the last row of the previous page.
	ListDeadLetters(ctx context.Context, status string, cursor *uuid.UUID, limit int) (*models.AdminDeadLettersResult, error)
	GetDeadLetter(ctx context.Context, id uuid.UUID) (*models.AdminDeadLetterRow, error)
	// RecentTaskFailures lists the newest task_failures rows with their task
	// and mailbox, capped at limit.
	RecentTaskFailures(ctx context.Context, limit int) ([]models.AdminTaskFailureRow, error)
	// WebhookHealth is the instance-wide delivery picture; a delivery is stale
	// when it has been in_flight longer than lease.
	WebhookHealth(ctx context.Context, lease time.Duration) (*models.AdminWebhookHealth, error)
	// OrgWebhooks lists one workspace's endpoints with their recent delivery
	// and drop counts.
	OrgWebhooks(ctx context.Context, orgID uuid.UUID) ([]models.AdminWebhookEndpointRow, error)
}

type adminSendsRepository struct {
	db *db.DB
}

func NewAdminSendsRepository(d *db.DB) AdminSendsRepository {
	return &adminSendsRepository{db: d}
}

func (r *adminSendsRepository) InFlight(ctx context.Context, reclaimAfter time.Duration, limit int) (*models.AdminInFlightResult, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	reclaimSecs := reclaimAfter.Seconds()
	out := &models.AdminInFlightResult{
		Summary: models.AdminInFlightSummary{ReclaimAfterMinutes: int(reclaimAfter / time.Minute)},
		Data:    []models.AdminInFlightSend{},
	}

	// The summary counts every reservation, not just the page.
	err := r.db.QueryRow(ctx, `
		SELECT COUNT(*),
		       COUNT(*) FILTER (WHERE dispatched_at > NOW() - INTERVAL '5 minutes'),
		       COUNT(*) FILTER (WHERE dispatched_at <= NOW() - INTERVAL '5 minutes'
		                          AND dispatched_at > NOW() - INTERVAL '30 minutes'),
		       COUNT(*) FILTER (WHERE dispatched_at <= NOW() - make_interval(secs => $1)),
		       MIN(dispatched_at)
		FROM campaign_contact_progress
		WHERE sent_at IS NULL AND dispatched_at IS NOT NULL
	`, reclaimSecs).Scan(
		&out.Summary.Total, &out.Summary.Under5m, &out.Summary.Under30m,
		&out.Summary.PastReclaimWindow, &out.Summary.OldestDispatched,
	)
	if err != nil {
		return nil, err
	}

	rows, err := r.db.Query(ctx, `
		SELECT p.campaign_id, COALESCE(c.name, ''), c.organization_id, COALESCE(o.name, ''),
		       p.contact_id, COALESCE(ct.email, ''), p.sequence_id,
		       p.dispatch_task_id, COALESCE(t.status::text, ''), COALESCE(t.message_id <> '', false),
		       t.email_account_id, COALESCE(ea.email, ''), ea.worker_id,
		       p.dispatched_at
		FROM campaign_contact_progress p
		JOIN campaigns c ON c.id = p.campaign_id
		LEFT JOIN organizations o ON o.id = c.organization_id
		LEFT JOIN contacts ct ON ct.id = p.contact_id
		LEFT JOIN tasks t ON t.id = p.dispatch_task_id
		LEFT JOIN email_accounts ea ON ea.id = t.email_account_id
		WHERE p.sent_at IS NULL AND p.dispatched_at IS NOT NULL
		ORDER BY p.dispatched_at ASC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	now := time.Now()
	for rows.Next() {
		var s models.AdminInFlightSend
		if err := rows.Scan(
			&s.CampaignID, &s.CampaignName, &s.OrganizationID, &s.OrganizationName,
			&s.ContactID, &s.ContactEmail, &s.SequenceID,
			&s.TaskID, &s.TaskStatus, &s.HasMessageID,
			&s.EmailAccountID, &s.MailboxEmail, &s.WorkerID,
			&s.DispatchedAt,
		); err != nil {
			return nil, err
		}
		s.AgeSeconds = int64(now.Sub(s.DispatchedAt).Seconds())
		out.Data = append(out.Data, s)
	}
	return out, rows.Err()
}

// deadLetterColumns is the select list shared by the list and get queries; a
// dead letter reaches its workspace through the task's mailbox.
const deadLetterColumns = `
		SELECT d.id, d.task_id, d.task_type, d.payload, d.last_error, d.attempts, d.max_attempts,
		       d.status, d.next_retry_at, d.replayed_at, d.created_at, d.updated_at,
		       ea.organization_id, COALESCE(o.name, '')
		FROM task_dead_letters d
		LEFT JOIN tasks t ON t.id = d.task_id
		LEFT JOIN email_accounts ea ON ea.id = t.email_account_id
		LEFT JOIN organizations o ON o.id = ea.organization_id`

func scanDeadLetter(row pgx.Row) (*models.AdminDeadLetterRow, error) {
	var d models.AdminDeadLetterRow
	var payload []byte
	if err := row.Scan(
		&d.ID, &d.TaskID, &d.TaskType, &payload, &d.LastError, &d.Attempts, &d.MaxAttempts,
		&d.Status, &d.NextRetryAt, &d.ReplayedAt, &d.CreatedAt, &d.UpdatedAt,
		&d.OrganizationID, &d.OrganizationName,
	); err != nil {
		return nil, err
	}
	if len(payload) > 0 {
		_ = json.Unmarshal(payload, &d.Payload)
	}
	if d.Payload == nil {
		d.Payload = map[string]interface{}{}
	}
	return &d, nil
}

func (r *adminSendsRepository) ListDeadLetters(ctx context.Context, status string, cursor *uuid.UUID, limit int) (*models.AdminDeadLettersResult, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	out := &models.AdminDeadLettersResult{
		Data:       []models.AdminDeadLetterRow{},
		Pagination: &models.Pagination{},
	}
	if err := r.db.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE status = 'pending'),
		       COUNT(*) FILTER (WHERE status = 'replayed'),
		       COUNT(*) FILTER (WHERE status = 'failed')
		FROM task_dead_letters
	`).Scan(&out.Pending, &out.Replayed, &out.Failed); err != nil {
		return nil, err
	}

	rows, err := r.db.Query(ctx, deadLetterColumns+`
		WHERE ($1 = '' OR d.status = $1)
		  AND ($2::uuid IS NULL OR (d.created_at, d.id) < (
		        SELECT created_at, id FROM task_dead_letters WHERE id = $2))
		ORDER BY d.created_at DESC, d.id DESC
		LIMIT $3
	`, status, cursor, limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		d, err := scanDeadLetter(rows)
		if err != nil {
			return nil, err
		}
		out.Data = append(out.Data, *d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out.Data) > limit {
		out.Data = out.Data[:limit]
		out.Pagination.HasMore = true
		out.Pagination.NextCursor = paging.UUIDString(out.Data[limit-1].ID)
	}
	return out, nil
}

func (r *adminSendsRepository) GetDeadLetter(ctx context.Context, id uuid.UUID) (*models.AdminDeadLetterRow, error) {
	d, err := scanDeadLetter(r.db.QueryRow(ctx, deadLetterColumns+` WHERE d.id = $1`, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return d, nil
}

// RecentTaskFailures orders by the task's updated_at: task_failures carries no
// timestamp of its own and the failure is the last write to the task.
func (r *adminSendsRepository) RecentTaskFailures(ctx context.Context, limit int) ([]models.AdminTaskFailureRow, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.db.Query(ctx, `
		SELECT f.task_id, t.task_type::text, t.status::text, f.title, f.message,
		       t.email_account_id, COALESCE(ea.email, ''), ea.organization_id, COALESCE(o.name, ''),
		       t.updated_at
		FROM task_failures f
		JOIN tasks t ON t.id = f.task_id
		LEFT JOIN email_accounts ea ON ea.id = t.email_account_id
		LEFT JOIN organizations o ON o.id = ea.organization_id
		ORDER BY t.updated_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.AdminTaskFailureRow, 0, limit)
	for rows.Next() {
		var f models.AdminTaskFailureRow
		if err := rows.Scan(
			&f.TaskID, &f.TaskType, &f.TaskStatus, &f.Title, &f.Message,
			&f.EmailAccountID, &f.MailboxEmail, &f.OrganizationID, &f.OrganizationName,
			&f.OccurredAt,
		); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// webhookEndpointColumns is the endpoint row with its 7-day counters. Drops
// are rolled up per (organization, event_type), so an endpoint's drops are
// the org's drops on the event types it subscribes to (all, when unfiltered).
const webhookEndpointColumns = `
		SELECT e.id, e.organization_id, COALESCE(o.name, ''), e.url, e.description, e.enabled,
		       e.event_types, e.consecutive_failures, e.last_success_at, e.last_failure_at,
		       COALESCE(e.last_failure_reason, ''),
		       (SELECT COUNT(*) FROM webhook_deliveries d
		         WHERE d.endpoint_id = e.id AND d.created_at >= NOW() - INTERVAL '7 days'),
		       (SELECT COUNT(*) FROM webhook_deliveries d
		         WHERE d.endpoint_id = e.id AND d.created_at >= NOW() - INTERVAL '7 days'
		           AND d.status IN ('failed', 'abandoned')),
		       (SELECT COALESCE(SUM(wd.dropped_windows), 0) FROM webhook_event_drops wd
		         WHERE wd.organization_id = e.organization_id AND wd.day >= CURRENT_DATE - 7
		           AND (cardinality(e.event_types) = 0 OR wd.event_type = ANY(e.event_types)))
		FROM webhook_endpoints e
		LEFT JOIN organizations o ON o.id = e.organization_id`

func scanWebhookEndpoints(rows pgx.Rows) ([]models.AdminWebhookEndpointRow, error) {
	defer rows.Close()
	out := []models.AdminWebhookEndpointRow{}
	for rows.Next() {
		var e models.AdminWebhookEndpointRow
		if err := rows.Scan(
			&e.ID, &e.OrganizationID, &e.OrganizationName, &e.URL, &e.Description, &e.Enabled,
			&e.EventTypes, &e.ConsecutiveFailures, &e.LastSuccessAt, &e.LastFailureAt,
			&e.LastFailureReason, &e.DeliveriesLast7d, &e.FailedLast7d, &e.DropsLast7d,
		); err != nil {
			return nil, err
		}
		if e.EventTypes == nil {
			e.EventTypes = []string{}
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// WebhookHealth calls a delivery stale by updated_at, which is what both the
// claim and ReclaimStuckDeliveries use, so the count is what a reclaim sweeps.
func (r *adminSendsRepository) WebhookHealth(ctx context.Context, lease time.Duration) (*models.AdminWebhookHealth, error) {
	out := &models.AdminWebhookHealth{
		LeaseMinutes:     int(lease / time.Minute),
		FailingEndpoints: []models.AdminWebhookEndpointRow{},
	}
	if err := r.db.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE status = 'in_flight' AND updated_at < NOW() - make_interval(secs => $1)),
		       COUNT(*) FILTER (WHERE status = 'pending' AND next_attempt_at <= NOW()),
		       COUNT(*) FILTER (WHERE status = 'delivered' AND updated_at >= NOW() - INTERVAL '24 hours'),
		       COUNT(*) FILTER (WHERE status = 'failed' AND updated_at >= NOW() - INTERVAL '24 hours'),
		       COUNT(*) FILTER (WHERE status = 'abandoned' AND updated_at >= NOW() - INTERVAL '24 hours')
		FROM webhook_deliveries
	`, lease.Seconds()).Scan(
		&out.InFlightStale, &out.PendingDue, &out.DeliveredLast24h, &out.FailedLast24h, &out.AbandonedLast24h,
	); err != nil {
		return nil, err
	}
	if err := r.db.QueryRow(ctx, `
		SELECT COALESCE(SUM(dropped_windows), 0) FROM webhook_event_drops WHERE day >= CURRENT_DATE - 7
	`).Scan(&out.DropsLast7d); err != nil {
		return nil, err
	}

	rows, err := r.db.Query(ctx, webhookEndpointColumns+`
		WHERE e.consecutive_failures > 0
		ORDER BY e.consecutive_failures DESC, e.last_failure_at DESC NULLS LAST
		LIMIT 50
	`)
	if err != nil {
		return nil, err
	}
	failing, err := scanWebhookEndpoints(rows)
	if err != nil {
		return nil, err
	}
	out.FailingEndpoints = failing
	return out, nil
}

func (r *adminSendsRepository) OrgWebhooks(ctx context.Context, orgID uuid.UUID) ([]models.AdminWebhookEndpointRow, error) {
	rows, err := r.db.Query(ctx, webhookEndpointColumns+`
		WHERE e.organization_id = $1
		ORDER BY e.created_at DESC
	`, orgID)
	if err != nil {
		return nil, err
	}
	return scanWebhookEndpoints(rows)
}
