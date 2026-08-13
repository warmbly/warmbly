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
	// last_seen_at is set on insert too, not just on conflict: this call IS a
	// heartbeat, and placement skips workers whose heartbeat is stale, so a
	// newly-registered worker would otherwise be unselectable until its second
	// ping.
	const q = `
		INSERT INTO workers (id, name, ip_addr, active, free_tier, worker_type,
		                     egress_kind, health_state, load_score, last_seen_at)
		VALUES ($1, $2, $3, TRUE, $4, $5, $6, 'healthy', 0, now())
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

// DeactivateWorker marks a worker inactive without deleting it, so its history
// and mailbox assignments survive. Called from the farewell heartbeat a worker
// sends on shutdown; a worker that comes back flips active to TRUE again on its
// next beat.
func (r *workerRepository) DeactivateWorker(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `
		UPDATE workers SET active = FALSE, updated_at = now() WHERE id = $1
	`, id)
	return err
}

// TierDedicated is the reserved tier the control plane allocates on its own
// (see worker.AssignDedicatedWorker, which promotes a spare shared worker to
// dedicated on demand). It must never be requested by a worker heartbeat or
// an admin provisioning template/job — clients only pick a shared tier.
const TierDedicated = "dedicated"

// IsClientRequestableTier reports whether a tier string may be supplied by a
// client: a worker heartbeat or an admin provisioning request. Everything
// except the reserved "dedicated" tier is allowed; a blank tier is treated as
// the shared-premium default by tierToColumns, so existing workers that don't
// set WORKER_TIER keep auto-registering normally.
func IsClientRequestableTier(tier string) bool {
	return tier != TierDedicated
}

// tierToColumns converts the higher-level tier name used by templates and
// the heartbeat API into the (worker_type, free_tier) tuple stored on the
// workers row. The "dedicated" case stays for defensive completeness, but the
// request paths now reject that tier before it reaches here (dedicated workers
// are created by promoting a shared worker, not by self-designation).
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
