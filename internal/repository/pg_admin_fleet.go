package repository

import (
	"context"
	"sort"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// AdminFleetRepository is the operator's view of automatic placement: every
// worker with its capacity-view row, the decision log the control loops write,
// and the dedicated worker bindings.
type AdminFleetRepository interface {
	// Capacity lists every worker (active or not) left-joined to
	// worker_capacity_view, hottest first.
	Capacity(ctx context.Context) ([]models.AdminFleetWorkerRow, error)
	// Decisions lists decision_log newest first. kind "" means every kind;
	// workerID nil means every worker.
	Decisions(ctx context.Context, kind string, workerID *uuid.UUID, limit int) ([]models.AdminFleetDecision, error)
	// DedicatedAssignments lists active bindings (released_at IS NULL) with the
	// worker and workspace named and the worker's current mailbox count.
	DedicatedAssignments(ctx context.Context) ([]models.AdminDedicatedAssignment, error)
}

type adminFleetRepository struct {
	db *db.DB
}

func NewAdminFleetRepository(d *db.DB) AdminFleetRepository {
	return &adminFleetRepository{db: d}
}

// Capacity joins every worker row to the materialized capacity view; the view
// only carries active workers, so inactive ones come back with zeroed metrics.
func (r *adminFleetRepository) Capacity(ctx context.Context) ([]models.AdminFleetWorkerRow, error) {
	rows, err := r.db.Query(ctx, `
		SELECT w.id, w.name, w.ip_addr, COALESCE(w.active, false), w.free_tier, w.worker_type,
		       w.risk_pool, w.egress_kind, w.health_state, w.install_state,
		       w.last_seen_at,
		       (COALESCE(w.active, false) AND w.last_seen_at > now() - $1::interval) AS live,
		       w.account_count,
		       COALESCE(t.tags, '{}'::text[]) AS tags,
		       COALESCE(v.load_score, w.load_score, 0)::float8,
		       COALESCE(v.base_capacity, 0)::float8,
		       COALESCE(v.health_multiplier, 1)::float8,
		       COALESCE(v.age_multiplier, 1)::float8,
		       COALESCE(v.sends_attempted_1h, 0), COALESCE(v.sends_succeeded_1h, 0),
		       COALESCE(v.bounces_hard_1h, 0), COALESCE(v.bounces_soft_1h, 0),
		       COALESCE(v.complaints_1h, 0), COALESCE(v.auth_errors_1h, 0)
		  FROM workers w
		  LEFT JOIN worker_capacity_view v ON v.worker_id = w.id
		  LEFT JOIN (
		       SELECT worker_id, array_agg(tag::text ORDER BY tag) AS tags
		         FROM worker_tags GROUP BY worker_id
		  ) t ON t.worker_id = w.id
	`, WorkerLivenessWindow)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]models.AdminFleetWorkerRow, 0)
	for rows.Next() {
		var row models.AdminFleetWorkerRow
		var live *bool
		if err := rows.Scan(
			&row.WorkerID, &row.Name, &row.IPAddr, &row.Active, &row.FreeTier, &row.WorkerType,
			&row.RiskPool, &row.EgressKind, &row.HealthState, &row.InstallState,
			&row.LastSeenAt, &live, &row.AccountCount, &row.Tags,
			&row.LoadScore, &row.BaseCapacity, &row.HealthMultiplier, &row.AgeMultiplier,
			&row.SendsAttempted1h, &row.SendsSucceeded1h,
			&row.BouncesHard1h, &row.BouncesSoft1h, &row.Complaints1h, &row.AuthErrors1h,
		); err != nil {
			return nil, err
		}
		// A NULL last_seen_at makes the AND NULL; that worker has never heartbeated.
		row.Live = live != nil && *live
		if row.Tags == nil {
			row.Tags = []string{}
		}
		row.EffectiveCapacity = row.BaseCapacity * row.HealthMultiplier * row.AgeMultiplier
		if row.EffectiveCapacity <= 0 {
			row.EffectiveCapacity = 1
		}
		row.Utilization = row.LoadScore / row.EffectiveCapacity
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Utilization > out[j].Utilization })
	return out, nil
}

// Decisions lists decision_log newest first; kind and workerID are optional
// filters and limit is clamped to 1..500 with a default of 100.
func (r *adminFleetRepository) Decisions(ctx context.Context, kind string, workerID *uuid.UUID, limit int) ([]models.AdminFleetDecision, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	rows, err := r.db.Query(ctx, `
		SELECT d.id, d.kind, d.worker_id, COALESCE(w.name, ''), d.mailbox_id,
		       d.before, d.after, COALESCE(d.reason, ''), COALESCE(d.triggered_by, ''), d.created_at
		  FROM decision_log d
		  LEFT JOIN workers w ON w.id = d.worker_id
		 WHERE ($1 = '' OR d.kind = $1)
		   AND ($2::uuid IS NULL OR d.worker_id = $2)
		 ORDER BY d.created_at DESC, d.id DESC
		 LIMIT $3
	`, kind, workerID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]models.AdminFleetDecision, 0)
	for rows.Next() {
		var d models.AdminFleetDecision
		var before, after []byte
		if err := rows.Scan(
			&d.ID, &d.Kind, &d.WorkerID, &d.WorkerName, &d.MailboxID,
			&before, &after, &d.Reason, &d.TriggeredBy, &d.CreatedAt,
		); err != nil {
			return nil, err
		}
		d.Before = before
		d.After = after
		out = append(out, d)
	}
	return out, rows.Err()
}

// DedicatedAssignments lists active bindings with the worker's live mailbox
// count taken from email_accounts rather than the cached account_count.
func (r *adminFleetRepository) DedicatedAssignments(ctx context.Context) ([]models.AdminDedicatedAssignment, error) {
	rows, err := r.db.Query(ctx, `
		SELECT a.id, a.worker_id, COALESCE(w.name, ''),
		       COALESCE(w.active AND w.last_seen_at > now() - $1::interval, false),
		       a.organization_id, COALESCE(o.name, ''),
		       a.subscription_id, a.assigned_at, a.released_at,
		       (SELECT count(*) FROM email_accounts e WHERE e.worker_id = a.worker_id)
		  FROM dedicated_worker_assignments a
		  LEFT JOIN workers w ON w.id = a.worker_id
		  LEFT JOIN organizations o ON o.id = a.organization_id
		 WHERE a.released_at IS NULL
		 ORDER BY a.assigned_at DESC
	`, WorkerLivenessWindow)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]models.AdminDedicatedAssignment, 0)
	for rows.Next() {
		var a models.AdminDedicatedAssignment
		if err := rows.Scan(
			&a.ID, &a.WorkerID, &a.WorkerName, &a.WorkerLive,
			&a.OrganizationID, &a.OrganizationName,
			&a.SubscriptionID, &a.AssignedAt, &a.ReleasedAt, &a.AccountCount,
		); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
