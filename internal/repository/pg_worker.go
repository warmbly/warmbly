package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
)

// EmailAccountWorkerInfo contains worker info for an email account
type EmailAccountWorkerInfo struct {
	EmailAccountID uuid.UUID
	WorkerID       *uuid.UUID
	UserID         uuid.UUID
	FreeTier       *bool
}

type WorkerRepository interface {
	// Worker queries
	GetByID(ctx context.Context, id uuid.UUID) (*models.Worker, error)
	GetSharedWorkersByTier(ctx context.Context, freeTier bool) ([]models.Worker, error)
	GetAllActiveWorkers(ctx context.Context) ([]models.Worker, error)
	GetAvailableDedicatedWorker(ctx context.Context) (*models.Worker, error)
	IncrementAccountCount(ctx context.Context, workerID uuid.UUID) error
	DecrementAccountCount(ctx context.Context, workerID uuid.UUID) error
	SetWorkerType(ctx context.Context, workerID uuid.UUID, workerType models.WorkerType) error

	// Dedicated worker assignments
	CreateDedicatedAssignment(ctx context.Context, assignment *models.DedicatedWorkerAssignment) error
	CreateDedicatedAssignmentIfNotExists(ctx context.Context, assignment *models.DedicatedWorkerAssignment) (bool, error)
	GetActiveDedicatedAssignment(ctx context.Context, userID uuid.UUID) (*models.DedicatedWorkerAssignment, error)
	GetDedicatedWorkerByUserID(ctx context.Context, userID uuid.UUID) (*models.Worker, error)
	ReleaseDedicatedAssignment(ctx context.Context, userID uuid.UUID) error

	// Email account worker queries
	GetEmailAccountsByWorkerID(ctx context.Context, workerID uuid.UUID) ([]uuid.UUID, error)
	GetEmailAccountsByUserID(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error)
	GetEmailAccountsByOrganizationID(ctx context.Context, orgID uuid.UUID) ([]uuid.UUID, error)
	GetEmailAccountWorkerInfo(ctx context.Context, emailAccountID uuid.UUID) (*EmailAccountWorkerInfo, error)
	UpdateEmailAccountWorker(ctx context.Context, emailAccountID, workerID uuid.UUID) error
	ClearEmailAccountWorker(ctx context.Context, emailAccountID uuid.UUID) error
	UpdateEmailAccountWarmupPoolType(ctx context.Context, emailAccountID uuid.UUID, poolType string) error

	// SSH-managed workers (admin-driven lifecycle)
	CreateWorker(ctx context.Context, in CreateWorkerInput) error
	GetWorkerDetail(ctx context.Context, id uuid.UUID) (*models.Worker, error)
	ListWorkersDetail(ctx context.Context) ([]models.Worker, error)
	GetWorkerSSHCredentials(ctx context.Context, id uuid.UUID) (*models.WorkerSSHCredentials, error)
	UpdateInstallState(ctx context.Context, id uuid.UUID, state models.WorkerInstallState, lastError string) error
	UpdateLastSeen(ctx context.Context, id uuid.UUID, at time.Time) error
	UpdateHostFingerprint(ctx context.Context, id uuid.UUID, fingerprint string) error
	RotateSSHKey(ctx context.Context, id uuid.UUID, publicKey, privateKeyEncrypted string) error
	DeleteWorker(ctx context.Context, id uuid.UUID) error
	ConsumeEnrollmentToken(ctx context.Context, tokenHash string) (*models.Worker, error)
	RecordEnrolledIP(ctx context.Context, id uuid.UUID, ip string) error
	AssignWorkerProfile(ctx context.Context, workerID uuid.UUID, profileID *uuid.UUID) error
	MarkConfigApplied(ctx context.Context, workerID uuid.UUID, at time.Time) error
	MarkImageVersion(ctx context.Context, workerID uuid.UUID, version string) error
	ListWorkersByProfile(ctx context.Context, profileID uuid.UUID) ([]models.Worker, error)

	// Threat-level segregation
	SetWorkerRiskPool(ctx context.Context, workerID uuid.UUID, pool models.WorkerRiskPool) error
	SetEmailAccountRiskBand(ctx context.Context, emailAccountID uuid.UUID, band models.EmailRiskBand) error
	GetSharedWorkersByTierAndPool(ctx context.Context, freeTier bool, pool models.WorkerRiskPool) ([]models.Worker, error)
	ListRiskCandidates(ctx context.Context, limit int) ([]RiskCandidate, error)

	// Tags
	GetWorkerTags(ctx context.Context, workerID uuid.UUID) ([]string, error)
	SetWorkerTags(ctx context.Context, workerID uuid.UUID, tags []string) error
	ListAllWorkerTags(ctx context.Context) ([]string, error)
	HydrateWorkerTags(ctx context.Context, workers []*models.Worker) error
}

type workerRepository struct {
	db *pgxpool.Pool
}

func NewWorkerRepository(db *pgxpool.Pool) WorkerRepository {
	return &workerRepository{db: db}
}

func (r *workerRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.Worker, error) {
	query := `
		SELECT id, ip_addr, active, free_tier, worker_type, account_count, created_at, updated_at
		FROM workers
		WHERE id = $1
	`

	var w models.Worker
	err := r.db.QueryRow(ctx, query, id).Scan(
		&w.ID, &w.IPAddr, &w.Active, &w.FreeTier, &w.WorkerType, &w.AccountCount,
		&w.CreatedAt, &w.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}

// GetSharedWorkersByTier retrieves all shared workers matching the specified tier
// Results are sorted by account_count ASC (least loaded first) for even distribution
func (r *workerRepository) GetSharedWorkersByTier(ctx context.Context, freeTier bool) ([]models.Worker, error) {
	query := `
		SELECT id, ip_addr, active, free_tier, worker_type, account_count, created_at, updated_at
		FROM workers
		WHERE worker_type = 'shared'
		  AND active = true
		  AND free_tier = $1
		ORDER BY account_count ASC
	`

	rows, err := r.db.Query(ctx, query, freeTier)
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
		workers = append(workers, w)
	}

	return workers, rows.Err()
}

// GetAllActiveWorkers retrieves all active workers regardless of tier
func (r *workerRepository) GetAllActiveWorkers(ctx context.Context) ([]models.Worker, error) {
	query := `
		SELECT id, ip_addr, active, free_tier, worker_type, account_count, created_at, updated_at
		FROM workers
		WHERE active = true
		ORDER BY created_at
	`
	rows, err := r.db.Query(ctx, query)
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
		workers = append(workers, w)
	}
	return workers, rows.Err()
}

// GetAvailableDedicatedWorker finds an available dedicated worker that is not assigned
func (r *workerRepository) GetAvailableDedicatedWorker(ctx context.Context) (*models.Worker, error) {
	query := `
		SELECT w.id, w.ip_addr, w.active, w.free_tier, w.worker_type, w.account_count, w.created_at, w.updated_at
		FROM workers w
		WHERE w.worker_type = 'dedicated'
		  AND w.active = true
		  AND w.free_tier = false
		  AND NOT EXISTS (
			SELECT 1 FROM dedicated_worker_assignments dwa
			WHERE dwa.worker_id = w.id AND dwa.released_at IS NULL
		  )
		ORDER BY w.created_at ASC
		LIMIT 1
	`

	var w models.Worker
	err := r.db.QueryRow(ctx, query).Scan(
		&w.ID, &w.IPAddr, &w.Active, &w.FreeTier, &w.WorkerType, &w.AccountCount,
		&w.CreatedAt, &w.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
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

func (r *workerRepository) SetWorkerType(ctx context.Context, workerID uuid.UUID, workerType models.WorkerType) error {
	query := `UPDATE workers SET worker_type = $1, updated_at = NOW() WHERE id = $2`
	_, err := r.db.Exec(ctx, query, workerType, workerID)
	return err
}

// CreateDedicatedAssignment creates a new dedicated worker assignment
func (r *workerRepository) CreateDedicatedAssignment(ctx context.Context, assignment *models.DedicatedWorkerAssignment) error {
	query := `
		INSERT INTO dedicated_worker_assignments (id, worker_id, user_id, subscription_id, assigned_at)
		VALUES ($1, $2, $3, $4, $5)
	`

	_, err := r.db.Exec(ctx, query,
		assignment.ID,
		assignment.WorkerID,
		assignment.UserID,
		assignment.SubscriptionID,
		assignment.AssignedAt,
	)
	return err
}

// CreateDedicatedAssignmentIfNotExists atomically creates a dedicated worker assignment
// only if no active (released_at IS NULL) assignment exists for the user.
// Returns (true, nil) if created, (false, nil) if already exists.
func (r *workerRepository) CreateDedicatedAssignmentIfNotExists(ctx context.Context, assignment *models.DedicatedWorkerAssignment) (bool, error) {
	query := `
		INSERT INTO dedicated_worker_assignments (id, worker_id, user_id, subscription_id, assigned_at)
		SELECT $1, $2, $3, $4, $5
		WHERE NOT EXISTS (
			SELECT 1 FROM dedicated_worker_assignments
			WHERE user_id = $3 AND released_at IS NULL
		)
	`
	result, err := r.db.Exec(ctx, query,
		assignment.ID,
		assignment.WorkerID,
		assignment.UserID,
		assignment.SubscriptionID,
		assignment.AssignedAt,
	)
	if err != nil {
		return false, err
	}
	return result.RowsAffected() > 0, nil
}

// GetActiveDedicatedAssignment retrieves the active dedicated assignment for a user
func (r *workerRepository) GetActiveDedicatedAssignment(ctx context.Context, userID uuid.UUID) (*models.DedicatedWorkerAssignment, error) {
	query := `
		SELECT id, worker_id, user_id, subscription_id, assigned_at, released_at
		FROM dedicated_worker_assignments
		WHERE user_id = $1 AND released_at IS NULL
	`

	var a models.DedicatedWorkerAssignment
	err := r.db.QueryRow(ctx, query, userID).Scan(
		&a.ID, &a.WorkerID, &a.UserID, &a.SubscriptionID, &a.AssignedAt, &a.ReleasedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// GetDedicatedWorkerByUserID retrieves the dedicated worker assigned to a user
func (r *workerRepository) GetDedicatedWorkerByUserID(ctx context.Context, userID uuid.UUID) (*models.Worker, error) {
	query := `
		SELECT w.id, w.ip_addr, w.active, w.free_tier, w.worker_type, w.account_count, w.created_at, w.updated_at
		FROM workers w
		JOIN dedicated_worker_assignments dwa ON w.id = dwa.worker_id
		WHERE dwa.user_id = $1 AND dwa.released_at IS NULL
	`

	var w models.Worker
	err := r.db.QueryRow(ctx, query, userID).Scan(
		&w.ID, &w.IPAddr, &w.Active, &w.FreeTier, &w.WorkerType, &w.AccountCount,
		&w.CreatedAt, &w.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}

// ReleaseDedicatedAssignment releases a dedicated worker assignment
func (r *workerRepository) ReleaseDedicatedAssignment(ctx context.Context, userID uuid.UUID) error {
	query := `
		UPDATE dedicated_worker_assignments
		SET released_at = $1
		WHERE user_id = $2 AND released_at IS NULL
	`

	_, err := r.db.Exec(ctx, query, time.Now(), userID)
	return err
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

// UpdateEmailAccountWorker assigns a worker to an email account
func (r *workerRepository) UpdateEmailAccountWorker(ctx context.Context, emailAccountID, workerID uuid.UUID) error {
	query := `UPDATE email_accounts SET worker_id = $1, updated_at = NOW() WHERE id = $2`
	_, err := r.db.Exec(ctx, query, workerID, emailAccountID)
	return err
}

// ClearEmailAccountWorker removes worker assignment from an email account
func (r *workerRepository) ClearEmailAccountWorker(ctx context.Context, emailAccountID uuid.UUID) error {
	query := `UPDATE email_accounts SET worker_id = NULL, updated_at = NOW() WHERE id = $1`
	_, err := r.db.Exec(ctx, query, emailAccountID)
	return err
}

// UpdateEmailAccountWarmupPoolType updates the warmup pool type for an email account
func (r *workerRepository) UpdateEmailAccountWarmupPoolType(ctx context.Context, emailAccountID uuid.UUID, poolType string) error {
	query := `UPDATE email_accounts SET warmup_pool_type = $1, updated_at = NOW() WHERE id = $2`
	_, err := r.db.Exec(ctx, query, poolType, emailAccountID)
	return err
}

// GetEmailAccountWorkerInfo retrieves worker info for an email account
func (r *workerRepository) GetEmailAccountWorkerInfo(ctx context.Context, emailAccountID uuid.UUID) (*EmailAccountWorkerInfo, error) {
	query := `
		SELECT ea.id, ea.worker_id, ea.user_id, w.free_tier
		FROM email_accounts ea
		LEFT JOIN workers w ON ea.worker_id = w.id
		WHERE ea.id = $1
	`

	var info EmailAccountWorkerInfo
	err := r.db.QueryRow(ctx, query, emailAccountID).Scan(
		&info.EmailAccountID, &info.WorkerID, &info.UserID, &info.FreeTier,
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

// DisableWarmupByOrganizationID disables warmup for all email accounts belonging to an organization
func DisableWarmupByOrganizationID(ctx context.Context, db *pgxpool.Pool, orgID uuid.UUID) error {
	query := `UPDATE email_accounts SET warmup = NULL, updated_at = NOW() WHERE organization_id = $1`
	_, err := db.Exec(ctx, query, orgID)
	return err
}

// MarkSubscriptionTrialExpired marks a subscription as expired trial
func MarkSubscriptionTrialExpired(ctx context.Context, db *pgxpool.Pool, subID uuid.UUID) error {
	query := `UPDATE subscriptions SET status = 'incomplete_expired', updated_at = NOW() WHERE id = $1`
	_, err := db.Exec(ctx, query, subID)
	return err
}
