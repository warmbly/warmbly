package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// DangerZoneRepository persists scheduled deletions and toggles the
// matching deletion timestamp on the parent table.
type DangerZoneRepository interface {
	// CreatePending inserts a new pending deletion and stamps the parent
	// resource's deletion_scheduled_* columns in the same transaction.
	// Returns ErrPendingDeletionExists if one already exists.
	CreatePending(ctx context.Context, d *models.ScheduledDeletion) error

	// Cancel marks a deletion as cancelled and clears the parent's
	// deletion timestamps. No-op if status != pending.
	Cancel(ctx context.Context, id uuid.UUID, cancelledByUserID uuid.UUID, reason string) error

	// GetActive returns the active (pending) deletion for a resource, if any.
	GetActive(ctx context.Context, rt models.DeletionResourceType, resourceID uuid.UUID) (*models.ScheduledDeletion, error)

	// GetByID returns any deletion by id.
	GetByID(ctx context.Context, id uuid.UUID) (*models.ScheduledDeletion, error)

	// ListDue returns pending deletions whose execute_after is <= now.
	ListDue(ctx context.Context, now time.Time, limit int) ([]models.ScheduledDeletion, error)

	// ListPendingForReminders returns pending deletions whose
	// execute_after is within the next windowEnd duration and that have
	// not yet had bit set in notifications_sent.
	ListPendingForReminders(ctx context.Context, within time.Duration, notSetBit int, limit int) ([]models.ScheduledDeletion, error)

	// MarkExecuting flips status from pending to executing (atomic guard).
	MarkExecuting(ctx context.Context, id uuid.UUID) (bool, error)

	// MarkCompleted records the finalized hard delete.
	MarkCompleted(ctx context.Context, id uuid.UUID) error

	// MarkFailed records an execution error so the next tick can retry.
	MarkFailed(ctx context.Context, id uuid.UUID, errMsg string) error

	// MarkWaiting records why a deletion was refused and holds it until retryAt.
	MarkWaiting(ctx context.Context, id uuid.UUID, reason string, retryAt time.Time) error

	// SetNotifBit OR's bit into notifications_sent (idempotent reminder gate).
	SetNotifBit(ctx context.Context, id uuid.UUID, bit int) error

	// HardDeleteOrganization removes the organization row, relying on
	// existing ON DELETE CASCADE FKs to clean up dependents. It returns the
	// worker assignments of the mailboxes it destroyed, which the caller must
	// evict; see CollectMailboxPlacements.
	HardDeleteOrganization(ctx context.Context, orgID uuid.UUID) ([]MailboxPlacement, error)

	// HardDeleteUser removes the user row and every workspace it owns,
	// returning the same placements to evict as HardDeleteOrganization. It
	// refuses with ErrAccountHoldsSharedWork, ErrOwnedWorkspaceBilled or
	// ErrOwnedAgencyBilled. confirm, when set, is asked about every owned
	// subscription still on record while the workspaces are locked.
	HardDeleteUser(ctx context.Context, userID uuid.UUID, confirm BillingCheck) ([]MailboxPlacement, error)
}

// ErrPendingDeletionExists is returned when CreatePending is called for
// a resource that already has an active pending deletion.
var ErrPendingDeletionExists = errors.New("pending deletion already exists for this resource")

type dangerZoneRepository struct {
	db *pgxpool.Pool
}

// NewDangerZoneRepository constructs a DangerZoneRepository.
func NewDangerZoneRepository(db *pgxpool.Pool) DangerZoneRepository {
	return &dangerZoneRepository{db: db}
}

const scheduledDeletionColumns = `
	id, resource_type, resource_id, organization_id,
	requested_by_user_id, reason,
	scheduled_at, execute_after, grace_days, status,
	cancelled_at, cancelled_by_user_id, cancelled_reason,
	executed_at, execution_error,
	notifications_sent, last_reminder_at
`

func scanScheduledDeletion(row pgx.Row) (*models.ScheduledDeletion, error) {
	var d models.ScheduledDeletion
	var status string
	err := row.Scan(
		&d.ID, &d.ResourceType, &d.ResourceID, &d.OrganizationID,
		&d.RequestedByUserID, &d.Reason,
		&d.ScheduledAt, &d.ExecuteAfter, &d.GraceDays, &status,
		&d.CancelledAt, &d.CancelledByUserID, &d.CancelledReason,
		&d.ExecutedAt, &d.ExecutionError,
		&d.NotificationsSent, &d.LastReminderAt,
	)
	if err != nil {
		return nil, err
	}
	d.Status = models.DeletionStatus(status)
	return &d, nil
}

func (r *dangerZoneRepository) CreatePending(ctx context.Context, d *models.ScheduledDeletion) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var linked bool
	var check string
	switch d.ResourceType {
	case models.DeletionResourceOrganization:
		check = `SELECT EXISTS (SELECT 1 FROM cloud_link WHERE organization_id = $1)`
	case models.DeletionResourceUser:
		check = `SELECT EXISTS (SELECT 1 FROM cloud_link cl JOIN organizations o ON o.id = cl.organization_id WHERE o.owner_user_id = $1)`
	}
	if check != "" {
		if err := tx.QueryRow(ctx, check, d.ResourceID).Scan(&linked); err != nil {
			return err
		}
		if linked {
			return ErrOrganizationCloudLinked
		}
	}
	if d.ResourceType == models.DeletionResourceUser {
		owned, err := ownedWorkspaces(ctx, tx, d.ResourceID, false)
		if err != nil {
			return err
		}
		if err := accountDeletable(ctx, tx, d.ResourceID, owned); err != nil {
			return err
		}
	}

	const insertQ = `
		INSERT INTO scheduled_deletions (
			id, resource_type, resource_id, organization_id,
			requested_by_user_id, reason,
			scheduled_at, execute_after, grace_days, status,
			notifications_sent
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'pending', 0)
	`

	_, err = tx.Exec(ctx, insertQ,
		d.ID, d.ResourceType, d.ResourceID, d.OrganizationID,
		d.RequestedByUserID, d.Reason,
		d.ScheduledAt, d.ExecuteAfter, d.GraceDays,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrPendingDeletionExists
		}
		return err
	}

	// Stamp the parent row so list endpoints can show "pending deletion"
	// without joining the scheduled_deletions table on every read.
	switch d.ResourceType {
	case models.DeletionResourceOrganization:
		_, err = tx.Exec(ctx,
			`UPDATE organizations
			   SET deletion_scheduled_at = $2,
			       deletion_scheduled_for = $3,
			       updated_at = NOW()
			 WHERE id = $1`,
			d.ResourceID, d.ScheduledAt, d.ExecuteAfter,
		)
	case models.DeletionResourceUser:
		_, err = tx.Exec(ctx,
			`UPDATE users
			   SET deletion_scheduled_at = $2,
			       deletion_scheduled_for = $3,
			       updated_at = NOW()
			 WHERE id = $1`,
			d.ResourceID, d.ScheduledAt, d.ExecuteAfter,
		)
	}
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (r *dangerZoneRepository) Cancel(ctx context.Context, id uuid.UUID, cancelledByUserID uuid.UUID, reason string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var resourceType string
	var resourceID uuid.UUID
	err = tx.QueryRow(ctx, `
		UPDATE scheduled_deletions
		   SET status = 'cancelled',
		       cancelled_at = NOW(),
		       cancelled_by_user_id = $2,
		       cancelled_reason = NULLIF($3, '')
		 WHERE id = $1 AND status = 'pending'
		 RETURNING resource_type, resource_id
	`, id, cancelledByUserID, reason).Scan(&resourceType, &resourceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Not pending; nothing to do.
			return nil
		}
		return err
	}

	switch models.DeletionResourceType(resourceType) {
	case models.DeletionResourceOrganization:
		_, err = tx.Exec(ctx,
			`UPDATE organizations
			   SET deletion_scheduled_at = NULL,
			       deletion_scheduled_for = NULL,
			       updated_at = NOW()
			 WHERE id = $1`,
			resourceID,
		)
	case models.DeletionResourceUser:
		_, err = tx.Exec(ctx,
			`UPDATE users
			   SET deletion_scheduled_at = NULL,
			       deletion_scheduled_for = NULL,
			       updated_at = NOW()
			 WHERE id = $1`,
			resourceID,
		)
	}
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (r *dangerZoneRepository) GetActive(ctx context.Context, rt models.DeletionResourceType, resourceID uuid.UUID) (*models.ScheduledDeletion, error) {
	row := r.db.QueryRow(ctx, `
		SELECT `+scheduledDeletionColumns+`
		  FROM scheduled_deletions
		 WHERE resource_type = $1 AND resource_id = $2 AND status = 'pending'
		 LIMIT 1
	`, rt, resourceID)
	d, err := scanScheduledDeletion(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return d, nil
}

func (r *dangerZoneRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.ScheduledDeletion, error) {
	row := r.db.QueryRow(ctx, `
		SELECT `+scheduledDeletionColumns+`
		  FROM scheduled_deletions WHERE id = $1
	`, id)
	d, err := scanScheduledDeletion(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return d, nil
}

func (r *dangerZoneRepository) ListDue(ctx context.Context, now time.Time, limit int) ([]models.ScheduledDeletion, error) {
	rows, err := r.db.Query(ctx, `
		SELECT `+scheduledDeletionColumns+`
		  FROM scheduled_deletions
		 WHERE status = 'pending' AND execute_after <= $1
		 ORDER BY execute_after ASC
		 LIMIT $2
	`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.ScheduledDeletion
	for rows.Next() {
		d, err := scanScheduledDeletion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, nil
}

func (r *dangerZoneRepository) ListPendingForReminders(ctx context.Context, within time.Duration, notSetBit int, limit int) ([]models.ScheduledDeletion, error) {
	deadline := time.Now().Add(within)
	rows, err := r.db.Query(ctx, `
		SELECT `+scheduledDeletionColumns+`
		  FROM scheduled_deletions
		 WHERE status = 'pending'
		   AND execute_after > NOW()
		   AND execute_after <= $1
		   AND (notifications_sent & $2) = 0
		 ORDER BY execute_after ASC
		 LIMIT $3
	`, deadline, notSetBit, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.ScheduledDeletion
	for rows.Next() {
		d, err := scanScheduledDeletion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, nil
}

func (r *dangerZoneRepository) MarkExecuting(ctx context.Context, id uuid.UUID) (bool, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE scheduled_deletions
		   SET status = 'executing'
		 WHERE id = $1 AND status = 'pending'
	`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (r *dangerZoneRepository) MarkCompleted(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `
		UPDATE scheduled_deletions
		   SET status = 'completed', executed_at = NOW(), execution_error = NULL
		 WHERE id = $1
	`, id)
	return err
}

func (r *dangerZoneRepository) MarkFailed(ctx context.Context, id uuid.UUID, errMsg string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE scheduled_deletions
		   SET status = 'pending',
		       execution_error = $2
		 WHERE id = $1
	`, id, errMsg)
	return err
}

func (r *dangerZoneRepository) MarkWaiting(ctx context.Context, id uuid.UUID, reason string, retryAt time.Time) error {
	_, err := r.db.Exec(ctx, `
		UPDATE scheduled_deletions
		   SET status = 'pending',
		       execution_error = $2,
		       execute_after = $3
		 WHERE id = $1
	`, id, reason, retryAt)
	return err
}

func (r *dangerZoneRepository) SetNotifBit(ctx context.Context, id uuid.UUID, bit int) error {
	_, err := r.db.Exec(ctx, `
		UPDATE scheduled_deletions
		   SET notifications_sent = notifications_sent | $2,
		       last_reminder_at = NOW()
		 WHERE id = $1
	`, id, bit)
	return err
}

func (r *dangerZoneRepository) HardDeleteOrganization(ctx context.Context, orgID uuid.UUID) ([]MailboxPlacement, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	placements, err := eraseAndDelete(ctx, tx, `a.organization_id = $1`, []any{orgID},
		deletion{`DELETE FROM organizations WHERE id = $1`, []any{orgID}})
	if err != nil {
		return nil, err
	}
	return placements, tx.Commit(ctx)
}

// BillingCheck asks the billing provider about subscriptions whose local
// state can lag it: it returns ErrOwnedWorkspaceBilled for one that will renew.
type BillingCheck func(ctx context.Context, subscriptionIDs []string) error

func (r *dangerZoneRepository) HardDeleteUser(ctx context.Context, userID uuid.UUID, confirm BillingCheck) ([]MailboxPlacement, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	// Conflicts with the key-share lock every insert referencing the user takes,
	// so nothing new can be attached to it between the checks and the delete.
	if _, err := tx.Exec(ctx, `SELECT 1 FROM users WHERE id = $1 FOR UPDATE`, userID); err != nil {
		return nil, err
	}
	// Locked so an ownership transfer cannot take a workspace out from under the erasure queued for it.
	owned, err := ownedWorkspaces(ctx, tx, userID, true)
	if err != nil {
		return nil, err
	}
	if err := accountDeletable(ctx, tx, userID, owned); err != nil {
		return nil, err
	}
	if confirm != nil {
		// The local row learns of a resumed subscription only from a webhook.
		if err := confirmNotBilled(ctx, tx, confirm, `
			SELECT stripe_subscription_id FROM subscriptions
			 WHERE organization_id = ANY($1) AND stripe_subscription_id IS NOT NULL
			   AND status NOT IN ('canceled', 'incomplete_expired')`, owned); err != nil {
			return nil, err
		}
		// Agencies cascade with their owner, and nothing here cancels their subscription.
		err := confirmNotBilled(ctx, tx, confirm, `
			SELECT stripe_subscription_id FROM agencies
			 WHERE owner_user_id = $1 AND stripe_subscription_id IS NOT NULL
			   AND status NOT IN ('canceled', 'incomplete_expired')`, userID)
		if errors.Is(err, ErrOwnedWorkspaceBilled) {
			err = ErrOwnedAgencyBilled
		}
		if err != nil {
			return nil, err
		}
	}
	// A subscription keeps the user who bought it through ownership transfers; it belongs to the current owner.
	if _, err := tx.Exec(ctx, `
		UPDATE subscriptions s SET user_id = o.owner_user_id
		  FROM organizations o
		 WHERE o.id = s.organization_id AND s.user_id = $1 AND s.organization_id <> ALL($2)`, userID, owned); err != nil {
		return nil, err
	}
	// organizations.owner_user_id deliberately has no delete action, so the
	// workspaces go by id; one created since the lock fails the user delete.
	placements, err := eraseAndDelete(ctx, tx, `a.user_id = $1 OR a.organization_id = ANY($2)`, []any{userID, owned},
		deletion{`DELETE FROM organizations WHERE id = ANY($1)`, []any{owned}},
		deletion{`DELETE FROM users WHERE id = $1`, []any{userID}})
	if err != nil {
		return nil, err
	}
	return placements, tx.Commit(ctx)
}

// confirmNotBilled asks the billing provider about the subscription ids the query returns.
func confirmNotBilled(ctx context.Context, tx pgx.Tx, confirm BillingCheck, query string, arg any) error {
	rows, err := tx.Query(ctx, query, arg)
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil || len(ids) == 0 {
		return err
	}
	return confirm(ctx, ids)
}

// ErrAccountHoldsSharedWork refuses an account deletion that would take a
// workspace's assets with it: the user's mailboxes, contacts, campaigns and
// their attachments, templates, API keys, apps and shared assistant conversations cascade with
// the user row, in every workspace, owned or not.
var ErrAccountHoldsSharedWork = errors.New("account holds assets in workspaces it does not own")

// ErrOwnedWorkspaceBilled refuses an account deletion while an owned workspace
// still has a subscription that will renew, which nothing here cancels.
var ErrOwnedWorkspaceBilled = errors.New("an owned workspace has a renewing subscription")

// ErrOwnedAgencyBilled refuses an account deletion while an agency the account
// owns has a subscription that will renew; the agency goes with the account.
var ErrOwnedAgencyBilled = errors.New("an owned agency has a renewing subscription")

func ownedWorkspaces(ctx context.Context, tx pgx.Tx, userID uuid.UUID, lock bool) ([]uuid.UUID, error) {
	q := `SELECT id FROM organizations WHERE owner_user_id = $1 ORDER BY id`
	if lock {
		q += ` FOR UPDATE`
	}
	rows, err := tx.Query(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	owned, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if owned == nil {
		owned = []uuid.UUID{}
	}
	return owned, err
}

func accountDeletable(ctx context.Context, tx pgx.Tx, userID uuid.UUID, owned []uuid.UUID) error {
	var shared, billed, agencyBilled bool
	err := tx.QueryRow(ctx, `
		SELECT
			EXISTS (SELECT 1 FROM email_accounts x WHERE x.user_id = $1 AND x.organization_id <> ALL($2))
			OR EXISTS (SELECT 1 FROM contacts x WHERE x.user_id = $1 AND x.organization_id <> ALL($2))
			OR EXISTS (SELECT 1 FROM campaigns x WHERE x.user_id = $1 AND x.organization_id <> ALL($2))
			OR EXISTS (SELECT 1 FROM reply_templates x WHERE x.user_id = $1 AND x.organization_id <> ALL($2))
			OR EXISTS (SELECT 1 FROM api_keys x WHERE x.user_id = $1 AND x.organization_id <> ALL($2))
			OR EXISTS (SELECT 1 FROM oauth_applications x WHERE x.created_by = $1 AND x.organization_id <> ALL($2))
			OR EXISTS (SELECT 1 FROM campaign_attachments x JOIN campaigns c ON c.id = x.campaign_id
				WHERE x.user_id = $1 AND c.organization_id <> ALL($2))
			OR EXISTS (SELECT 1 FROM agent_sessions x JOIN organizations o ON o.id = x.org_id
				WHERE x.user_id = $1 AND x.org_id <> ALL($2) AND o.assistant_shared_history),
			EXISTS (SELECT 1 FROM subscriptions s WHERE s.organization_id = ANY($2)
				AND s.stripe_subscription_id IS NOT NULL AND NOT s.cancel_at_period_end
				AND s.status NOT IN ('canceled', 'incomplete_expired')),
			EXISTS (SELECT 1 FROM agencies a WHERE a.owner_user_id = $1
				AND a.stripe_subscription_id IS NOT NULL AND NOT a.cancel_at_period_end
				AND a.status NOT IN ('canceled', 'incomplete_expired'))`,
		userID, owned).Scan(&shared, &billed, &agencyBilled)
	switch {
	case err != nil:
		return err
	case shared:
		return ErrAccountHoldsSharedWork
	case billed:
		return ErrOwnedWorkspaceBilled
	case agencyBilled:
		return ErrOwnedAgencyBilled
	}
	return nil
}

// MailboxPlacement is one mailbox and the worker that was executing it, read
// before the row is deleted because nothing afterwards can say which worker
// held it.
type MailboxPlacement struct {
	EmailID  uuid.UUID
	UserID   uuid.UUID
	WorkerID uuid.UUID
}

// CollectMailboxPlacements returns the worker assignments of the mailboxes in
// scope, for the caller to evict once the delete has committed.
//
// A worker keeps its mailboxes in memory and only filters on status when it
// starts, so a row disappearing underneath it changes nothing: it keeps
// authenticating, failing and reporting, once every sync interval, forever.
// Only a REMOVE_EMAIL stops it, and after the delete there is no assignment
// left to address one to. Same reasoning as EnqueueMailboxErasures above.
//
// Two things here are not incidental. FOR UPDATE holds the rows until the
// delete commits: worker_id is mutable and rotation moves mailboxes between
// workers on its own schedule, so without the lock a rotation committing
// between this read and the cascade leaves the new worker holding a mailbox
// nobody evicts. And every mailbox in scope is locked, not just the assigned
// ones, because an unassigned mailbox that gains a worker in that same window
// is the same bug with an extra step. EnqueueMailboxErasures needs neither: it
// reads only id and user_id, and those do not change.
func CollectMailboxPlacements(ctx context.Context, tx pgx.Tx, scope string, args ...any) ([]MailboxPlacement, error) {
	q := `SELECT a.id, a.user_id, a.worker_id FROM email_accounts a WHERE (` + scope + `) FOR UPDATE OF a`
	rows, err := tx.Query(ctx, q, args...)
	if err != nil {
		db.CaptureError(err, q, args, "query")
		return nil, err
	}
	defer rows.Close()

	var out []MailboxPlacement
	for rows.Next() {
		var (
			p        MailboxPlacement
			workerID *uuid.UUID
		)
		if err := rows.Scan(&p.EmailID, &p.UserID, &workerID); err != nil {
			db.CaptureError(err, q, args, "scan")
			return nil, err
		}
		// Locked above so it cannot gain one; nothing to evict it from.
		if workerID == nil {
			continue
		}
		p.WorkerID = *workerID
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		db.CaptureError(err, q, args, "rows")
		return nil, err
	}
	return out, nil
}

// deletion is one statement of a hard delete with its own arguments.
type deletion struct {
	sql  string
	args []any
}

// eraseAndDelete removes a workspace or a person and everything the cascades
// take with them, having first written down the erasure that the cascades
// cannot do.
//
// Deleting the rows is not deleting the data. Every mailbox in scope has an
// OAuth grant still live at its provider and message bodies still in object
// storage, and both references disappear with the rows, so the enqueue has to
// happen inside this transaction and before the delete. Without it, a customer
// who closes their account leaves Warmbly connected to their Gmail.
//
// mailboxScope selects those mailboxes as a WHERE clause over email_accounts
// aliased `a`; it is written here, never by a caller.
func eraseAndDelete(ctx context.Context, tx pgx.Tx, mailboxScope string, scopeArgs []any, deletes ...deletion) ([]MailboxPlacement, error) {
	if _, err := EnqueueMailboxErasures(ctx, tx, mailboxScope, scopeArgs...); err != nil {
		return nil, err
	}

	// Read while the assignments still exist; returned to the caller to act on
	// only once the delete has committed, because evicting a mailbox from its
	// worker on a transaction that then rolls back would stop a live one.
	placements, err := CollectMailboxPlacements(ctx, tx, mailboxScope, scopeArgs...)
	if err != nil {
		return nil, err
	}

	// The threads those mailboxes hold, read before the rows go. Deleting a
	// workspace does not delete its members, so their labels and snoozes
	// outlive the messages they were attached to.
	threads, err := CollectMailboxThreadState(ctx, tx, mailboxScope, scopeArgs...)
	if err != nil {
		return nil, err
	}

	for _, d := range deletes {
		if _, err := tx.Exec(ctx, d.sql, d.args...); err != nil {
			return nil, err
		}
	}

	if err := DeleteOrphanedThreadState(ctx, tx, threads); err != nil {
		return nil, err
	}
	return placements, nil
}

// IsForeignKeyViolation detects Postgres SQLSTATE 23503 (foreign_key_violation),
// which for a write keyed on a mailbox or a user means the parent row was
// deleted before the write landed. Matched through errors.As so a wrapped error
// still reports.
func IsForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23503"
	}
	return false
}

// isDeadlock detects Postgres SQLSTATE 40P01 (deadlock_detected). The whole
// transaction is aborted when this happens, so the caller has to redo it from
// the beginning rather than retry the one statement.
func isDeadlock(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "40P01"
	}
	return false
}

// isUniqueViolation detects Postgres SQLSTATE 23505 (unique_violation).
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	type pgErr interface {
		SQLState() string
	}
	if pe, ok := err.(pgErr); ok {
		return pe.SQLState() == "23505"
	}
	return false
}

// IsUniqueViolation is isUniqueViolation for callers outside this package.
func IsUniqueViolation(err error) bool { return isUniqueViolation(err) }
