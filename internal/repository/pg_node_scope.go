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

// NodeMailbox is one mailbox a worker runs, or ran within the release grace.
type NodeMailbox struct {
	ID             uuid.UUID
	UserID         uuid.UUID
	OrganizationID *uuid.UUID
}

// NodeScopeRepository reads what a fleet node is assigned, for binding its internal calls.
type NodeScopeRepository interface {
	// NodeRole is the node's enrolled role; found is false for a node with no row.
	NodeRole(ctx context.Context, id uuid.UUID) (role models.NodeRole, found bool, err error)
	// WorkerMailboxes lists the mailboxes assigned to a worker, plus those it left less than grace ago.
	WorkerMailboxes(ctx context.Context, workerID uuid.UUID, grace time.Duration) ([]NodeMailbox, error)
	// WorkerHoldsMailbox reports whether the worker runs the mailbox (owned by user, when set), or left it less than grace ago.
	WorkerHoldsMailbox(ctx context.Context, workerID, userID, mailboxID uuid.UUID, grace time.Duration) (bool, error)
	// WorkerHoldsOrganization reports whether the worker runs, or left less than grace ago, any mailbox of the organization.
	WorkerHoldsOrganization(ctx context.Context, workerID, orgID uuid.UUID, grace time.Duration) (bool, error)
	// OrganizationDataKeys returns the sealed data keys of the given organizations.
	OrganizationDataKeys(ctx context.Context, orgIDs []uuid.UUID) ([]string, error)
	// DataKeyOrganization names the organization a sealed data key belongs to.
	DataKeyOrganization(ctx context.Context, sealed string) (uuid.UUID, bool, error)
	// CampaignOrganization names the organization a campaign belongs to.
	CampaignOrganization(ctx context.Context, campaignID uuid.UUID) (uuid.UUID, bool, error)
}

type nodeScopeRepository struct {
	db *db.DB
}

func NewNodeScopeRepository(d *db.DB) NodeScopeRepository {
	return &nodeScopeRepository{db: d}
}

func (r *nodeScopeRepository) NodeRole(ctx context.Context, id uuid.UUID) (models.NodeRole, bool, error) {
	var role models.NodeRole
	err := r.db.QueryRow(ctx, `SELECT role FROM fleet_nodes WHERE id = $1`, id).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	return role, err == nil, err
}

func (r *nodeScopeRepository) WorkerMailboxes(ctx context.Context, workerID uuid.UUID, grace time.Duration) ([]NodeMailbox, error) {
	// Two index scans: idx_email_accounts_worker and idx_email_accounts_previous_worker.
	rows, err := r.db.Query(ctx, `
		SELECT id, user_id, organization_id FROM email_accounts WHERE worker_id = $1
		UNION
		SELECT id, user_id, organization_id FROM email_accounts
		 WHERE previous_worker_id = $1 AND worker_released_at > now() - make_interval(secs => $2)`,
		workerID, grace.Seconds())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NodeMailbox
	for rows.Next() {
		var m NodeMailbox
		if err := rows.Scan(&m.ID, &m.UserID, &m.OrganizationID); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// heldByWorker is the assignment predicate on email_accounts: $1 is the worker, $2 the grace in seconds.
const heldByWorker = `(worker_id = $1 OR (previous_worker_id = $1 AND worker_released_at > now() - make_interval(secs => $2)))`

func (r *nodeScopeRepository) WorkerHoldsMailbox(ctx context.Context, workerID, userID, mailboxID uuid.UUID, grace time.Duration) (bool, error) {
	var held bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM email_accounts
		 WHERE id = $3 AND ($4::uuid IS NULL OR user_id = $4) AND `+heldByWorker+`)`,
		workerID, grace.Seconds(), mailboxID, nullableUUID(userID)).Scan(&held)
	return held, err
}

func (r *nodeScopeRepository) WorkerHoldsOrganization(ctx context.Context, workerID, orgID uuid.UUID, grace time.Duration) (bool, error) {
	var held bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM email_accounts WHERE organization_id = $3 AND `+heldByWorker+`)`,
		workerID, grace.Seconds(), orgID).Scan(&held)
	return held, err
}

func (r *nodeScopeRepository) OrganizationDataKeys(ctx context.Context, orgIDs []uuid.UUID) ([]string, error) {
	if len(orgIDs) == 0 {
		return nil, nil
	}
	rows, err := r.db.Query(ctx,
		`SELECT encrypted_data_key FROM organization_encrypted_keys WHERE organization_id = ANY($1)`, orgIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (r *nodeScopeRepository) DataKeyOrganization(ctx context.Context, sealed string) (uuid.UUID, bool, error) {
	var id uuid.UUID
	err := r.db.QueryRow(ctx,
		`SELECT organization_id FROM organization_encrypted_keys WHERE encrypted_data_key = $1 LIMIT 1`, sealed).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	return id, err == nil, err
}

func (r *nodeScopeRepository) CampaignOrganization(ctx context.Context, campaignID uuid.UUID) (uuid.UUID, bool, error) {
	var id *uuid.UUID
	err := r.db.QueryRow(ctx, `SELECT organization_id FROM campaigns WHERE id = $1`, campaignID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && id == nil) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, err
	}
	return *id, true, nil
}
