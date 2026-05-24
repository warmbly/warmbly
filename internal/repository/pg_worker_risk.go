package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

// SetWorkerRiskPool changes a worker's risk_pool. The rebalancer doesn't
// touch this — only admins do, via the UI. Misconfiguring a pool just
// means mailboxes get reassigned on the next rebalancer tick, so the
// blast radius is low.
func (r *workerRepository) SetWorkerRiskPool(ctx context.Context, workerID uuid.UUID, pool models.WorkerRiskPool) error {
	_, err := r.db.Exec(ctx, `
		UPDATE workers SET risk_pool = $2, updated_at = NOW() WHERE id = $1
	`, workerID, pool)
	return err
}

// SetEmailAccountRiskBand records a mailbox's classification along with
// the timestamp it was evaluated. Used by the rebalancer.
func (r *workerRepository) SetEmailAccountRiskBand(ctx context.Context, emailAccountID uuid.UUID, band models.EmailRiskBand) error {
	_, err := r.db.Exec(ctx, `
		UPDATE email_accounts
		SET risk_band = $2, risk_evaluated_at = NOW()
		WHERE id = $1
	`, emailAccountID, band)
	return err
}

// GetSharedWorkersByTierAndPool is the assignment service's primary lookup:
// give me the least-loaded shared worker for this tier AND risk pool.
// Falls back to "any pool" via separate caller logic when no match exists.
func (r *workerRepository) GetSharedWorkersByTierAndPool(ctx context.Context, freeTier bool, pool models.WorkerRiskPool) ([]models.Worker, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, ip_addr, active, free_tier, worker_type, account_count, created_at, updated_at
		FROM workers
		WHERE worker_type = 'shared'
		  AND active = true
		  AND free_tier = $1
		  AND risk_pool = $2
		ORDER BY account_count ASC
	`, freeTier, pool)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var workers []models.Worker
	for rows.Next() {
		var w models.Worker
		if err := rows.Scan(
			&w.ID, &w.IPAddr, &w.Active, &w.FreeTier, &w.WorkerType, &w.AccountCount,
			&w.CreatedAt, &w.UpdatedAt,
		); err != nil {
			return nil, err
		}
		w.RiskPool = pool // already filtered, set explicitly so the model is complete
		workers = append(workers, w)
	}
	return workers, rows.Err()
}

// RiskCandidate is one mailbox the rebalancer might need to act on.
// Returned by ListRiskCandidates: includes the current band, the new band
// derived from health state, and the current worker so the rebalancer can
// decide whether to migrate.
type RiskCandidate struct {
	EmailAccountID uuid.UUID
	UserID         uuid.UUID
	OrgID          *uuid.UUID
	CurrentBand    models.EmailRiskBand
	HealthState    models.WarmupHealthState // may be empty if no health row
	WorkerID       *uuid.UUID
	WorkerRiskPool models.WorkerRiskPool
	WorkerFreeTier bool
	WorkerType     models.WorkerType
	LastEvaluated  *time.Time
}

// ListRiskCandidates joins email_accounts with warmup_health and workers
// so the rebalancer can compute the new band, compare to the current one,
// and decide migrations in a single scan. Dedicated workers are excluded
// — they serve one customer, segregation isn't applicable.
func (r *workerRepository) ListRiskCandidates(ctx context.Context, limit int) ([]RiskCandidate, error) {
	if limit <= 0 {
		limit = 500
	}
	// Health lives on warmup_pool_participants; an account can be in more
	// than one pool, so we pick its WORST state via a CASE rank.
	rows, err := r.db.Query(ctx, `
		SELECT
			ea.id,
			ea.user_id,
			ea.organization_id,
			ea.risk_band,
			COALESCE(wh.health_state, '')::text,
			ea.worker_id,
			COALESCE(w.risk_pool, 'clean'::worker_risk_pool),
			COALESCE(w.free_tier, false),
			COALESCE(w.worker_type, 'shared'::worker_type),
			ea.risk_evaluated_at
		FROM email_accounts ea
		LEFT JOIN LATERAL (
			SELECT health_state
			FROM warmup_pool_participants
			WHERE email_account_id = ea.id
			ORDER BY CASE health_state
				WHEN 'blocked' THEN 0
				WHEN 'quarantined' THEN 1
				WHEN 'throttled' THEN 2
				WHEN 'watch' THEN 3
				WHEN 'healthy' THEN 4
				ELSE 5
			END
			LIMIT 1
		) wh ON true
		LEFT JOIN workers w ON w.id = ea.worker_id
		WHERE ea.status = 'active'
		  AND (w.worker_type IS NULL OR w.worker_type = 'shared')
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []RiskCandidate
	for rows.Next() {
		var c RiskCandidate
		var hs string
		if err := rows.Scan(
			&c.EmailAccountID,
			&c.UserID,
			&c.OrgID,
			&c.CurrentBand,
			&hs,
			&c.WorkerID,
			&c.WorkerRiskPool,
			&c.WorkerFreeTier,
			&c.WorkerType,
			&c.LastEvaluated,
		); err != nil {
			return nil, err
		}
		c.HealthState = models.WarmupHealthState(hs)
		out = append(out, c)
	}
	return out, rows.Err()
}
