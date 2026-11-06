package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
)

// WorkerLivenessWindow is how stale a worker's heartbeat may be before
// placement stops considering it. Workers heartbeat every 30s, so this
// tolerates a handful of missed beats.
//
// Without it, rows for workers that no longer exist stay active and healthy
// forever and get selected, and the command sent to them times out with no
// consumer. That is easy to hit: a worker with no WORKER_ID set generates a
// fresh UUID on every boot, so each container recreate leaves another dead row
// behind.
const WorkerLivenessWindow = "10 minutes"

var ErrWorkerAssignmentChanged = errors.New("email account worker assignment changed")

// ErrMailboxInstanceExecuted refuses a worker for a mailbox a linked instance executes.
var ErrMailboxInstanceExecuted = errors.New("email account is executed by a linked instance")

// EmailAccountWorkerInfo contains worker info for an email account
type EmailAccountWorkerInfo struct {
	EmailAccountID uuid.UUID
	WorkerID       *uuid.UUID
	UserID         uuid.UUID
	OrganizationID *uuid.UUID
	WorkerRegion   string
}

// EmailAccountPlacementHint carries the small slice of mailbox metadata the
// assignment service needs to compute a mailbox weight (provider +
// warmup-only flag). Fetched once per assignment so we don't have to thread
// the data through every caller.
type EmailAccountPlacementHint struct {
	Provider string
	IsWarmup bool
}

type WorkerRepository interface {
	// Worker queries
	GetByID(ctx context.Context, id uuid.UUID) (*models.Worker, error)
	// ListPlaceableWorkers returns every live worker, least loaded first. It
	// is the fallback path for when the capacity view has no rows yet.
	ListPlaceableWorkers(ctx context.Context) ([]models.Worker, error)
	// IsWorkerLive reports whether a worker is active and heartbeating inside
	// WorkerLivenessWindow, i.e. whether it can still receive commands.
	IsWorkerLive(ctx context.Context, id uuid.UUID) (bool, error)
	GetAllActiveWorkers(ctx context.Context) ([]models.Worker, error)
	// GetIdleUnboundWorker finds an active worker carrying no mailboxes and
	// bound to no organization, so it can be reserved for one that is
	// entitled to isolated egress.
	GetIdleUnboundWorker(ctx context.Context) (*models.Worker, error)
	IncrementAccountCount(ctx context.Context, workerID uuid.UUID) error
	DecrementAccountCount(ctx context.Context, workerID uuid.UUID) error

	// Dedicated worker assignments
	CreateDedicatedAssignment(ctx context.Context, assignment *models.DedicatedWorkerAssignment) error
	CreateDedicatedAssignmentIfNotExists(ctx context.Context, assignment *models.DedicatedWorkerAssignment) (bool, error)
	GetActiveDedicatedAssignment(ctx context.Context, userID uuid.UUID) (*models.DedicatedWorkerAssignment, error)
	GetDedicatedWorkerByOrgID(ctx context.Context, orgID uuid.UUID) (*models.Worker, error)
	ReleaseDedicatedAssignment(ctx context.Context, userID uuid.UUID) error
	// ReleaseDedicatedAssignmentByID releases one specific binding; false when
	// it was already released, so a caller never releases a newer one by accident.
	ReleaseDedicatedAssignmentByID(ctx context.Context, id uuid.UUID) (bool, error)

	// Email account worker queries
	GetEmailAccountsByWorkerID(ctx context.Context, workerID uuid.UUID) ([]uuid.UUID, error)
	GetEmailAccountsByUserID(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error)
	GetEmailAccountsByOrganizationID(ctx context.Context, orgID uuid.UUID) ([]uuid.UUID, error)
	GetEmailAccountWorkerInfo(ctx context.Context, emailAccountID uuid.UUID) (*EmailAccountWorkerInfo, error)
	MoveEmailAccountWorker(ctx context.Context, emailAccountID uuid.UUID, from *uuid.UUID, to uuid.UUID, weight float64) error
	ClearEmailAccountWorker(ctx context.Context, emailAccountID uuid.UUID) error
	UpdateEmailAccountWarmupPoolType(ctx context.Context, emailAccountID uuid.UUID, poolType models.WarmupPoolType) error

	// Worker rows. A worker is created by its node enrolling, never by an
	// admin form, so there is no create here: EnsureWorkerRow is called by the
	// heartbeat once a node declares itself a worker.
	EnsureWorkerRow(ctx context.Context, id uuid.UUID) error
	GetWorkerDetail(ctx context.Context, id uuid.UUID) (*models.Worker, error)
	ListWorkersDetail(ctx context.Context) ([]models.Worker, error)

	// Per-mailbox reputation banding. Drives warmup partner selection and
	// pacing; it deliberately does NOT drive worker placement, because the
	// worker is not the sending identity.
	SetEmailAccountRiskBand(ctx context.Context, emailAccountID uuid.UUID, band models.EmailRiskBand) error
	GetEmailAccountRiskBand(ctx context.Context, emailAccountID uuid.UUID) (models.EmailRiskBand, error)
	ListRiskCandidates(ctx context.Context, limit int) ([]RiskCandidate, error)

	// Tags
	GetWorkerTags(ctx context.Context, workerID uuid.UUID) ([]string, error)
	SetWorkerTags(ctx context.Context, workerID uuid.UUID, tags []string) error
	ListAllWorkerTags(ctx context.Context) ([]string, error)
	HydrateWorkerTags(ctx context.Context, workers []*models.Worker) error

	// Health and capacity
	InsertWorkerHealthSample(ctx context.Context, sample *models.WorkerHealthSample) error
	ListCapacityCandidates(ctx context.Context, allowedStates []models.WorkerHealthState) ([]WorkerCapacityRowDB, error)
	// ListPlacementCandidates is ListCapacityCandidates plus the neighbour
	// counts the placement score needs, resolved in one round trip.
	ListPlacementCandidates(ctx context.Context, orgID uuid.UUID, provider string, allowedStates []models.WorkerHealthState) ([]PlacementCandidateRow, error)
	CountOrgMailboxes(ctx context.Context, orgID uuid.UUID) (int, error)
	ListRotationCandidates(ctx context.Context, hotUtilization float64, limit int) ([]MailboxPlacementState, error)
	GetCapacityRow(ctx context.Context, workerID uuid.UUID) (*WorkerCapacityRowDB, error)
	AddLoadScore(ctx context.Context, workerID uuid.UUID, delta float64) error
	SetWorkerHealthState(ctx context.Context, workerID uuid.UUID, state models.WorkerHealthState) error
	RefreshWorkerCapacityView(ctx context.Context) error
	GetEmailAccountPlacementHint(ctx context.Context, emailAccountID uuid.UUID) (*EmailAccountPlacementHint, error)
}

type workerRepository struct {
	db *pgxpool.Pool
}

func NewWorkerRepository(db *pgxpool.Pool) WorkerRepository {
	return &workerRepository{db: db}
}

// workerSelect is the single projection every worker read goes through. The
// placement columns come from `workers`, the machine columns from the node it
// is; keeping it in one place is what stops the two halves drifting apart.
const workerSelect = `
	SELECT w.id, w.account_count, w.health_state, w.load_score,
	       n.name, n.notes, n.address, n.active, n.last_seen_at, n.last_error,
	       n.version, n.region, w.created_at, w.updated_at
	  FROM workers w
	  JOIN fleet_nodes n ON n.id = w.id`

func scanWorker(row pgx.Row) (*models.Worker, error) {
	var w models.Worker
	err := row.Scan(
		&w.ID, &w.AccountCount, &w.HealthState, &w.LoadScore,
		&w.Name, &w.Notes, &w.IPAddr, &w.Active, &w.LastSeenAt, &w.LastError,
		&w.Version, &w.Region, &w.CreatedAt, &w.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &w, nil
}

func scanWorkers(rows pgx.Rows) ([]models.Worker, error) {
	defer rows.Close()
	var out []models.Worker
	for rows.Next() {
		w, err := scanWorker(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *w)
	}
	return out, rows.Err()
}

// EnsureWorkerRow creates the placement half for a node that has declared
// itself a worker. Idempotent: every heartbeat calls it, and only the first
// one does anything.
func (r *workerRepository) EnsureWorkerRow(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO workers (id) VALUES ($1)
		ON CONFLICT (id) DO NOTHING`, id)
	return err
}

func (r *workerRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.Worker, error) {
	w, err := scanWorker(r.db.QueryRow(ctx, workerSelect+` WHERE w.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return w, err
}

// IsWorkerLive reports whether a worker is still a valid placement target:
// active, and heartbeating inside WorkerLivenessWindow. Liveness is a property
// of the node, so it is read there and nowhere else.
func (r *workerRepository) IsWorkerLive(ctx context.Context, id uuid.UUID) (bool, error) {
	var live bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM fleet_nodes
			WHERE id = $1 AND role = 'worker' AND active
			  AND last_seen_at > now() - $2::interval
		)`, id, WorkerLivenessWindow).Scan(&live)
	return live, err
}

func (r *workerRepository) ListPlaceableWorkers(ctx context.Context) ([]models.Worker, error) {
	rows, err := r.db.Query(ctx, workerSelect+`
		 WHERE n.active AND n.last_seen_at > now() - $1::interval
		 ORDER BY w.account_count ASC`, WorkerLivenessWindow)
	if err != nil {
		return nil, err
	}
	return scanWorkers(rows)
}

// GetAllActiveWorkers retrieves every active worker, live or not.
func (r *workerRepository) GetAllActiveWorkers(ctx context.Context) ([]models.Worker, error) {
	rows, err := r.db.Query(ctx, workerSelect+` WHERE n.active ORDER BY w.created_at`)
	if err != nil {
		return nil, err
	}
	return scanWorkers(rows)
}

// GetIdleUnboundWorker finds an active worker that carries no mailboxes and is
// bound to no organization, so it can be reserved for one entitled to isolated
// egress. There is no worker category to check: any idle worker will do.
func (r *workerRepository) GetIdleUnboundWorker(ctx context.Context) (*models.Worker, error) {
	w, err := scanWorker(r.db.QueryRow(ctx, workerSelect+`
		 WHERE n.active
		   AND n.last_seen_at > now() - $1::interval
		   AND w.account_count = 0
		   AND NOT EXISTS (
		       SELECT 1 FROM dedicated_worker_assignments dwa
		        WHERE dwa.worker_id = w.id AND dwa.released_at IS NULL
		   )
		 ORDER BY w.created_at ASC
		 LIMIT 1`, WorkerLivenessWindow))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return w, err
}

// GetWorkerDetail and ListWorkersDetail are the admin reads. They differ from
// GetByID/GetAllActiveWorkers only in including inactive workers, so an
// operator can still see a machine that went away.
func (r *workerRepository) GetWorkerDetail(ctx context.Context, id uuid.UUID) (*models.Worker, error) {
	w, err := scanWorker(r.db.QueryRow(ctx, workerSelect+` WHERE w.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return w, err
}

func (r *workerRepository) ListWorkersDetail(ctx context.Context) ([]models.Worker, error) {
	rows, err := r.db.Query(ctx, workerSelect+` ORDER BY n.active DESC, n.last_seen_at DESC NULLS LAST, n.name`)
	if err != nil {
		return nil, err
	}
	return scanWorkers(rows)
}

func (r *workerRepository) IncrementAccountCount(ctx context.Context, workerID uuid.UUID) error {
	query := `UPDATE workers SET account_count = account_count + 1, updated_at = NOW() WHERE id = $1`
	_, err := r.db.Exec(ctx, query, workerID)
	return err
}

func (r *workerRepository) DecrementAccountCount(ctx context.Context, workerID uuid.UUID) error {
	query := `UPDATE workers SET account_count = GREATEST(account_count - 1, 0), updated_at = NOW() WHERE id = $1`
	_, err := r.db.Exec(ctx, query, workerID)
	return err
}

// CreateDedicatedAssignment creates a new dedicated worker assignment
func (r *workerRepository) CreateDedicatedAssignment(ctx context.Context, assignment *models.DedicatedWorkerAssignment) error {
	query := `
		INSERT INTO dedicated_worker_assignments (id, worker_id, organization_id, subscription_id, assigned_at)
		VALUES ($1, $2, $3, $4, $5)
	`

	_, err := r.db.Exec(ctx, query,
		assignment.ID,
		assignment.WorkerID,
		assignment.OrganizationID,
		assignment.SubscriptionID,
		assignment.AssignedAt,
	)
	return err
}

// CreateDedicatedAssignmentIfNotExists atomically creates a dedicated worker assignment
// only if no active (released_at IS NULL) assignment exists for the organization.
// Returns (true, nil) if created, (false, nil) if already exists.
func (r *workerRepository) CreateDedicatedAssignmentIfNotExists(ctx context.Context, assignment *models.DedicatedWorkerAssignment) (bool, error) {
	query := `
		INSERT INTO dedicated_worker_assignments (id, worker_id, organization_id, subscription_id, assigned_at)
		SELECT $1, $2, $3, $4, $5
		WHERE NOT EXISTS (
			SELECT 1 FROM dedicated_worker_assignments
			WHERE organization_id = $3 AND released_at IS NULL
		)
	`
	result, err := r.db.Exec(ctx, query,
		assignment.ID,
		assignment.WorkerID,
		assignment.OrganizationID,
		assignment.SubscriptionID,
		assignment.AssignedAt,
	)
	if err != nil {
		return false, err
	}
	return result.RowsAffected() > 0, nil
}

// GetActiveDedicatedAssignment retrieves the active dedicated assignment for an organization
func (r *workerRepository) GetActiveDedicatedAssignment(ctx context.Context, userID uuid.UUID) (*models.DedicatedWorkerAssignment, error) {
	query := `
		SELECT id, worker_id, organization_id, subscription_id, assigned_at, released_at
		FROM dedicated_worker_assignments
		WHERE organization_id = $1 AND released_at IS NULL
	`

	var a models.DedicatedWorkerAssignment
	err := r.db.QueryRow(ctx, query, userID).Scan(
		&a.ID, &a.WorkerID, &a.OrganizationID, &a.SubscriptionID, &a.AssignedAt, &a.ReleasedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// GetDedicatedWorkerByOrgID retrieves the dedicated worker assigned to an organization
func (r *workerRepository) GetDedicatedWorkerByOrgID(ctx context.Context, orgID uuid.UUID) (*models.Worker, error) {
	w, err := scanWorker(r.db.QueryRow(ctx, workerSelect+`
		  JOIN dedicated_worker_assignments dwa ON dwa.worker_id = w.id
		 WHERE dwa.organization_id = $1 AND dwa.released_at IS NULL
		 LIMIT 1`, orgID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return w, err
}

func (r *workerRepository) ReleaseDedicatedAssignment(ctx context.Context, userID uuid.UUID) error {
	query := `
		UPDATE dedicated_worker_assignments
		SET released_at = $1
		WHERE organization_id = $2 AND released_at IS NULL
	`

	_, err := r.db.Exec(ctx, query, time.Now(), userID)
	return err
}

func (r *workerRepository) ReleaseDedicatedAssignmentByID(ctx context.Context, id uuid.UUID) (bool, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE dedicated_worker_assignments
		SET released_at = now()
		WHERE id = $1 AND released_at IS NULL
	`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// GetEmailAccountsByWorkerID retrieves all email account IDs assigned to a worker
func (r *workerRepository) GetEmailAccountsByWorkerID(ctx context.Context, workerID uuid.UUID) ([]uuid.UUID, error) {
	query := `SELECT id FROM email_accounts WHERE worker_id = $1`

	rows, err := r.db.Query(ctx, query, workerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}

	return ids, rows.Err()
}

// GetEmailAccountsByUserID retrieves all email account IDs for a user
func (r *workerRepository) GetEmailAccountsByUserID(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	query := `SELECT id FROM email_accounts WHERE user_id = $1`

	rows, err := r.db.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}

	return ids, rows.Err()
}

// GetEmailAccountsByOrganizationID retrieves all email account IDs for an organization
func (r *workerRepository) GetEmailAccountsByOrganizationID(ctx context.Context, orgID uuid.UUID) ([]uuid.UUID, error) {
	query := `SELECT id FROM email_accounts WHERE organization_id = $1`

	rows, err := r.db.Query(ctx, query, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}

	return ids, rows.Err()
}

// MoveEmailAccountWorker updates the assignment and both workers' capacity counters atomically.
func (r *workerRepository) MoveEmailAccountWorker(
	ctx context.Context,
	emailAccountID uuid.UUID,
	from *uuid.UUID,
	to uuid.UUID,
	weight float64,
) error {
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var current *uuid.UUID
	var executed bool
	if err := tx.QueryRow(ctx, `SELECT worker_id, `+instanceExecutedSQL("email_accounts.id")+` FROM email_accounts WHERE id = $1 FOR UPDATE`, emailAccountID).Scan(&current, &executed); err != nil {
		return err
	}
	if executed {
		return fmt.Errorf("%w: %s", ErrMailboxInstanceExecuted, emailAccountID)
	}
	if (current == nil) != (from == nil) || (current != nil && from != nil && *current != *from) {
		return fmt.Errorf("%w for mailbox %s", ErrWorkerAssignmentChanged, emailAccountID)
	}
	if current != nil && *current == to {
		return tx.Commit(ctx)
	}

	workerIDs := []uuid.UUID{to}
	if current != nil {
		workerIDs = append(workerIDs, *current)
	}
	rows, err := tx.Query(ctx, `SELECT id FROM workers WHERE id = ANY($1::uuid[]) ORDER BY id FOR UPDATE`, workerIDs)
	if err != nil {
		return err
	}
	locked := make(map[uuid.UUID]struct{}, len(workerIDs))
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		locked[id] = struct{}{}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range workerIDs {
		if _, ok := locked[id]; !ok {
			return fmt.Errorf("worker %s not found", id)
		}
	}

	if current != nil {
		if _, err := tx.Exec(ctx, `
			UPDATE workers
			   SET account_count = GREATEST(account_count - 1, 0),
			       load_score = GREATEST(0, load_score - $2),
			       updated_at = NOW()
			 WHERE id = $1`, *current, weight); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE workers
		   SET account_count = account_count + 1,
		       load_score = load_score + $2,
		       updated_at = NOW()
		 WHERE id = $1`, to, weight); err != nil {
		return err
	}
	cmd, err := tx.Exec(ctx, `
		UPDATE email_accounts
		   SET worker_id = $1,
		       worker_assigned_at = NOW(),
		       updated_at = NOW()
		 WHERE id = $2 AND NOT `+instanceExecutedSQL("email_accounts.id"), to, emailAccountID)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return fmt.Errorf("%w: %s", ErrMailboxInstanceExecuted, emailAccountID)
	}

	return tx.Commit(ctx)
}

// ClearEmailAccountWorker removes worker assignment from an email account
func (r *workerRepository) ClearEmailAccountWorker(ctx context.Context, emailAccountID uuid.UUID) error {
	query := `UPDATE email_accounts SET worker_id = NULL, worker_assigned_at = NULL, updated_at = NOW() WHERE id = $1`
	_, err := r.db.Exec(ctx, query, emailAccountID)
	return err
}

// UpdateEmailAccountWarmupPoolType writes the tier and moves eligible participation atomically.
func (r *workerRepository) UpdateEmailAccountWarmupPoolType(ctx context.Context, emailAccountID uuid.UUID, poolType models.WarmupPoolType) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`UPDATE email_accounts SET warmup_pool_type = $1, updated_at = NOW() WHERE id = $2`,
		poolType, emailAccountID); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE warmup_pool_participants wpp
		SET pool_id = wp.id
		FROM warmup_pools wp
		WHERE wp.pool_type = $1::warmup_pool_type
		  AND wpp.email_account_id = $2::uuid
		  AND wpp.pool_id <> wp.id
		  AND ($1::text <> 'premium' OR NOT EXISTS (
		        SELECT 1
		        FROM email_accounts ea
		        JOIN organizations o ON o.id = ea.organization_id
		        WHERE ea.id = $2::uuid
		          AND o.risk_state IN ('restricted', 'suspended')
		      ))`,
		poolType, emailAccountID); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// GetEmailAccountWorkerInfo retrieves worker info for an email account
func (r *workerRepository) GetEmailAccountWorkerInfo(ctx context.Context, emailAccountID uuid.UUID) (*EmailAccountWorkerInfo, error) {
	query := `
		SELECT ea.id, ea.worker_id, ea.user_id, ea.organization_id, COALESCE(node.region, '')
		FROM email_accounts ea
		LEFT JOIN fleet_nodes node ON node.id = ea.worker_id
		WHERE ea.id = $1
	`

	var info EmailAccountWorkerInfo
	err := r.db.QueryRow(ctx, query, emailAccountID).Scan(
		&info.EmailAccountID, &info.WorkerID, &info.UserID, &info.OrganizationID, &info.WorkerRegion,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &info, nil
}

// DisableWarmupByUserID disables warmup for all email accounts belonging to a user
func DisableWarmupByUserID(ctx context.Context, db *pgxpool.Pool, userID uuid.UUID) error {
	query := `UPDATE email_accounts SET warmup = NULL, updated_at = NOW() WHERE user_id = $1`
	_, err := db.Exec(ctx, query, userID)
	return err
}

// PauseCampaignsByUserID pauses all active campaigns for a user
func PauseCampaignsByUserID(ctx context.Context, db *pgxpool.Pool, userID uuid.UUID, reason string) error {
	query := `UPDATE campaigns SET status = $1, updated_at = NOW() WHERE user_id = $2 AND status = 'active'`
	_, err := db.Exec(ctx, query, reason, userID)
	return err
}

// GetExpiredTrialsWithoutPayment retrieves subscriptions with expired free trials and no paid subscription
func GetExpiredTrialsWithoutPayment(ctx context.Context, db *pgxpool.Pool) ([]models.Subscription, error) {
	query := `
		SELECT s.id, s.user_id, s.organization_id, s.plan_id, s.stripe_customer_id, s.stripe_subscription_id,
		       s.stripe_price_id, s.status, s.current_period_start, s.current_period_end,
		       s.cancel_at_period_end, s.canceled_at, s.trial_start, s.trial_end,
		       s.free_trial_started_at, s.free_trial_ends_at, s.is_enterprise, s.created_at, s.updated_at,
		       u.email
		FROM subscriptions s
		LEFT JOIN users u ON s.user_id = u.id
		WHERE s.free_trial_ends_at IS NOT NULL
		  AND s.free_trial_ends_at < NOW()
		  AND s.stripe_subscription_id IS NULL
		  -- An operator-granted plan in force is paid without Stripe; its trial has nothing to expire.
		  AND NOT (s.managed_at IS NOT NULL AND (s.managed_until IS NULL OR s.managed_until > NOW()))
		  AND s.status NOT IN ('canceled', 'incomplete_expired')
	`

	rows, err := db.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var subs []models.Subscription
	for rows.Next() {
		var s models.Subscription
		if err := rows.Scan(
			&s.ID, &s.UserID, &s.OrganizationID, &s.PlanID, &s.StripeCustomerID, &s.StripeSubscriptionID,
			&s.StripePriceID, &s.Status, &s.CurrentPeriodStart, &s.CurrentPeriodEnd,
			&s.CancelAtPeriodEnd, &s.CanceledAt, &s.TrialStart, &s.TrialEnd,
			&s.FreeTrialStartedAt, &s.FreeTrialEndsAt, &s.IsEnterprise, &s.CreatedAt, &s.UpdatedAt,
			&s.UserEmail,
		); err != nil {
			return nil, err
		}
		subs = append(subs, s)
	}

	return subs, rows.Err()
}

// PauseCampaignsByOrganizationID pauses all active campaigns for an organization
func PauseCampaignsByOrganizationID(ctx context.Context, db *pgxpool.Pool, orgID uuid.UUID, reason string) error {
	query := `UPDATE campaigns SET status = $1, updated_at = NOW() WHERE organization_id = $2 AND status = 'active'`
	_, err := db.Exec(ctx, query, reason, orgID)
	return err
}

// PauseTrialWarmup pauses the organization's warming mailboxes at the trial end, which ResumeTrialWarmup matches on.
func PauseTrialWarmup(ctx context.Context, db *pgxpool.Pool, orgID uuid.UUID, trialEndsAt time.Time) error {
	query := `UPDATE email_accounts SET warmup_paused_at = $2, updated_at = NOW() WHERE organization_id = $1 AND warmup IS NOT NULL AND warmup_paused_at IS NULL`
	_, err := db.Exec(ctx, query, orgID, trialEndsAt)
	return err
}

// MarkSubscriptionTrialExpired marks a subscription as expired trial
func MarkSubscriptionTrialExpired(ctx context.Context, db *pgxpool.Pool, subID uuid.UUID) error {
	query := `UPDATE subscriptions SET status = 'incomplete_expired', updated_at = NOW() WHERE id = $1`
	_, err := db.Exec(ctx, query, subID)
	return err
}
