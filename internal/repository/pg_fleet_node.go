package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// FleetNodeRepository is the registry every Warmbly process on a machine you
// own registers into. Placement-specific worker state lives in
// WorkerRepository; this is the machine half, shared by every role.
type FleetNodeRepository interface {
	// UpsertOnHeartbeat registers a node on its first beat and refreshes it on
	// every later one. Registration IS the first heartbeat: there is no
	// separate create step, so a node that comes back after a rebuild simply
	// reappears.
	UpsertOnHeartbeat(ctx context.Context, beat models.NodeHeartbeat) error
	// Deactivate marks a node inactive without deleting it, so its history
	// survives. Sent on the farewell beat.
	Deactivate(ctx context.Context, id uuid.UUID) error

	// TouchLastSeen mirrors a heartbeat observed elsewhere (the Redis registry)
	// into the row, so the dashboard can render liveness without reading Redis.
	// Never moves the timestamp backwards.
	TouchLastSeen(ctx context.Context, id uuid.UUID, at time.Time) error

	Get(ctx context.Context, id uuid.UUID) (*models.FleetNode, error)
	List(ctx context.Context, role models.NodeRole) ([]models.FleetNode, error)
	Delete(ctx context.Context, id uuid.UUID) error

	// SetName and SetNotes are the only cosmetic edits an operator makes.
	SetName(ctx context.Context, id uuid.UUID, name string) error
	SetNotes(ctx context.Context, id uuid.UUID, notes string) error
	// SetPinnedVersion holds one node at a version, or clears the pin with "".
	SetPinnedVersion(ctx context.Context, id uuid.UUID, version string) error
}

type fleetNodeRepository struct {
	db *db.DB
}

func NewFleetNodeRepository(d *db.DB) FleetNodeRepository {
	return &fleetNodeRepository{db: d}
}

// The projection joins the placement half so the fleet view can show how much
// mail a worker is carrying without a second round trip. It is NULL for a
// consumer, which carries none.
const fleetNodeSelect = `
	SELECT n.id, n.role, n.name, n.notes, n.region, n.address, n.capacity_target, n.version, n.pinned_version,
	       n.active, n.last_seen_at, n.enrolled_at,
	       n.cpu_percent, n.memory_mb, n.goroutines, n.uptime_seconds,
	       n.cpu_scope, n.memory_scope, n.memory_used_mb, n.memory_limit_mb, n.resident_mb,
	       n.last_error, n.created_at, n.updated_at,
	       w.account_count
	  FROM fleet_nodes n
	  LEFT JOIN workers w ON w.id = n.id`

func scanFleetNode(row pgx.Row) (*models.FleetNode, error) {
	var n models.FleetNode
	err := row.Scan(
		&n.ID, &n.Role, &n.Name, &n.Notes, &n.Region, &n.Address, &n.CapacityTarget,
		&n.Version, &n.PinnedVersion,
		&n.Active, &n.LastSeenAt, &n.EnrolledAt,
		&n.Usage.CPUPercent, &n.Usage.MemoryMB, &n.Usage.Goroutines, &n.Usage.UptimeSeconds,
		&n.Usage.CPUScope, &n.Usage.MemoryScope, &n.Usage.MemoryUsedMB, &n.Usage.MemoryLimitMB, &n.Usage.ResidentMB,
		&n.LastError, &n.CreatedAt, &n.UpdatedAt,
		&n.MailboxCount,
	)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

// Identity hints tolerate omissions; address and resources reflect the latest report.
func (r *fleetNodeRepository) UpsertOnHeartbeat(ctx context.Context, beat models.NodeHeartbeat) error {
	name := beat.Name
	if name == "" {
		name = string(beat.Role) + "-" + beat.NodeID.String()[:8]
	}
	const q = `
		INSERT INTO fleet_nodes (
			id, role, name, region, address, capacity_target, version, active, last_seen_at,
			cpu_percent, memory_mb, goroutines, uptime_seconds, last_error,
			cpu_scope, memory_scope, memory_used_mb, memory_limit_mb, resident_mb, warmup_send_protocol
		) VALUES ($1, $2, $3, $4, $5, COALESCE(NULLIF($6, 0), 100), $7, TRUE, now(), $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
		ON CONFLICT (id) DO UPDATE SET
			-- Role is NOT updated. A node that re-registers under a different
			-- role would keep its workers row and the mailboxes assigned to
			-- it, while no longer doing worker work: the mail would sit on a
			-- machine that never sends it. Changing what a machine does means
			-- removing the node and joining again, which releases the
			-- mailboxes properly.
			region       = CASE WHEN EXCLUDED.region  <> '' THEN EXCLUDED.region  ELSE fleet_nodes.region  END,
			address      = EXCLUDED.address,
			capacity_target = CASE WHEN $6 > 0 THEN $6 ELSE fleet_nodes.capacity_target END,
			version      = CASE WHEN EXCLUDED.version <> '' THEN EXCLUDED.version ELSE fleet_nodes.version END,
			active       = TRUE,
			last_seen_at = now(),
			cpu_percent    = EXCLUDED.cpu_percent,
			memory_mb      = EXCLUDED.memory_mb,
			goroutines     = EXCLUDED.goroutines,
			uptime_seconds = EXCLUDED.uptime_seconds,
			cpu_scope      = EXCLUDED.cpu_scope,
			memory_scope   = EXCLUDED.memory_scope,
			memory_used_mb = EXCLUDED.memory_used_mb,
			memory_limit_mb = EXCLUDED.memory_limit_mb,
			resident_mb    = EXCLUDED.resident_mb,
			warmup_send_protocol = EXCLUDED.warmup_send_protocol,
			last_error   = EXCLUDED.last_error,
			updated_at   = now()
	`
	_, err := r.db.Exec(ctx, q,
		beat.NodeID, string(beat.Role), name, beat.Region, beat.Address, beat.CapacityTarget, beat.Version,
		beat.Usage.CPUPercent, beat.Usage.MemoryMB, beat.Usage.Goroutines, beat.Usage.UptimeSeconds,
		beat.LastError,
		beat.Usage.CPUScope, beat.Usage.MemoryScope, beat.Usage.MemoryUsedMB, beat.Usage.MemoryLimitMB, beat.Usage.ResidentMB,
		beat.WarmupSendProtocol,
	)
	return err
}

func (r *fleetNodeRepository) Deactivate(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx,
		`UPDATE fleet_nodes SET active = FALSE, updated_at = now() WHERE id = $1`, id)
	return err
}

func (r *fleetNodeRepository) TouchLastSeen(ctx context.Context, id uuid.UUID, at time.Time) error {
	_, err := r.db.Exec(ctx, `
		UPDATE fleet_nodes
		   SET last_seen_at = GREATEST(COALESCE(last_seen_at, $2), $2), updated_at = now()
		 WHERE id = $1`, id, at)
	return err
}

func (r *fleetNodeRepository) Get(ctx context.Context, id uuid.UUID) (*models.FleetNode, error) {
	n, err := scanFleetNode(r.db.QueryRow(ctx, fleetNodeSelect+` WHERE n.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return n, err
}

// List returns nodes of one role, or every node when role is empty. Ordered so
// the live ones come first and the fleet view reads top-down by usefulness.
func (r *fleetNodeRepository) List(ctx context.Context, role models.NodeRole) ([]models.FleetNode, error) {
	q := fleetNodeSelect
	args := []any{}
	if role != "" {
		q += ` WHERE n.role = $1`
		args = append(args, string(role))
	}
	q += ` ORDER BY n.active DESC, n.last_seen_at DESC NULLS LAST, n.role, n.name`

	rows, err := r.db.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]models.FleetNode, 0)
	for rows.Next() {
		n, err := scanFleetNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *n)
	}
	return out, rows.Err()
}

// Delete removes a node. The workers row cascades, so deleting a worker node
// also drops its placement state; its mailboxes are released by the foreign
// key on email_accounts and re-placed on the next rotation pass.
func (r *fleetNodeRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM fleet_nodes WHERE id = $1`, id)
	return err
}

func (r *fleetNodeRepository) SetName(ctx context.Context, id uuid.UUID, name string) error {
	_, err := r.db.Exec(ctx,
		`UPDATE fleet_nodes SET name = $2, updated_at = now() WHERE id = $1`, id, name)
	return err
}

func (r *fleetNodeRepository) SetNotes(ctx context.Context, id uuid.UUID, notes string) error {
	_, err := r.db.Exec(ctx,
		`UPDATE fleet_nodes SET notes = $2, updated_at = now() WHERE id = $1`, id, notes)
	return err
}

func (r *fleetNodeRepository) SetPinnedVersion(ctx context.Context, id uuid.UUID, version string) error {
	_, err := r.db.Exec(ctx,
		`UPDATE fleet_nodes SET pinned_version = $2, updated_at = now() WHERE id = $1`, id, version)
	return err
}
