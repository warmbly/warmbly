package repository

import (
	"context"

	"github.com/google/uuid"
)

// UpsertOnHeartbeat is called from the /api/v1/internal/worker/heartbeat
// handler. It registers a newly-provisioned worker on its first contact and
// keeps last_seen fresh on every subsequent ping.
//
// Tier mapping ("shared_free" / "shared_premium" / "dedicated") is collapsed
// into the existing (worker_type, free_tier) columns so the rest of the
// assignment logic keeps working unchanged.
func (r *workerRepository) UpsertOnHeartbeat(ctx context.Context, id uuid.UUID, ipAddr, tier, egressKind string) error {
	workerType, freeTier := tierToColumns(tier)
	if egressKind == "" {
		egressKind = "cold_smtp"
	}
	const q = `
		INSERT INTO workers (id, name, ip_addr, active, free_tier, worker_type,
		                     egress_kind, health_state, load_score)
		VALUES ($1, $2, $3, TRUE, $4, $5, $6, 'healthy', 0)
		ON CONFLICT (id) DO UPDATE
		   SET ip_addr = EXCLUDED.ip_addr,
		       active = TRUE,
		       install_state = CASE
		         WHEN workers.install_state IN ('pending', 'provisioning', 'error') THEN 'installed'::worker_install_state
		         ELSE workers.install_state
		       END,
		       last_seen_at = now(),
		       last_error = NULL,
		       updated_at = now()
	`
	name := "auto-registered-" + id.String()[:8]
	_, err := r.db.Exec(ctx, q, id, name, ipAddr, freeTier, workerType, egressKind)
	return err
}

// tierToColumns converts the higher-level tier name used by templates and
// the heartbeat API into the (worker_type, free_tier) tuple stored on the
// workers row.
func tierToColumns(tier string) (workerType string, freeTier bool) {
	switch tier {
	case "dedicated":
		return "dedicated", false
	case "shared_free":
		return "shared", true
	case "shared_premium":
		return "shared", false
	default:
		return "shared", false
	}
}
