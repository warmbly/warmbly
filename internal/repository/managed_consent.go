package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/models"
)

type CloudManagedConsentRepository interface {
	CreateManagedConsent(context.Context, *models.CloudManagedConsent) error
	GetManagedConsent(context.Context, uuid.UUID, uuid.UUID, string) (*models.CloudManagedConsent, error)
	ListManagedConsents(context.Context, uuid.UUID) ([]models.CloudManagedConsent, error)
	SetManagedConsentState(context.Context, uuid.UUID, uuid.UUID, string, *uuid.UUID) error
	BindManagedCloudAccount(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error
	RevokeManagedConsents(context.Context, uuid.UUID, *uuid.UUID) error
	ManagedConsentAuthorized(context.Context, *models.CloudManagedConsent) (bool, error)
	FindManagedAdoption(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID) (*models.CloudManagedConsent, error)
	CanBrokerManagedToken(context.Context, uuid.UUID) (bool, error)
}

func (r *cloudLinkRepository) BindManagedCloudAccount(ctx context.Context, orgID, consentID, cloudID uuid.UUID) error {
	tag, err := r.db.Exec(ctx, `UPDATE cloud_managed_consents SET cloud_account_id=$3 WHERE organization_id=$1 AND id=$2
 AND instance_id IS NOT NULL AND consent_state IN ('pending','unknown','active') AND (cloud_account_id IS NULL OR cloud_account_id=$3)`, orgID, consentID, cloudID)
	if err == nil && tag.RowsAffected() != 1 {
		return errors.New("managed cloud account identity changed")
	}
	return err
}

const managedConsentColumns = `id, organization_id, user_id, instance_id, remote_id, session_hash, kind, provider,
 cloud_account_id, planned_account_id, email_account_id, consent_state, created_at, expires_at`

func scanManagedConsent(row pgx.Row) (*models.CloudManagedConsent, error) {
	var c models.CloudManagedConsent
	err := row.Scan(&c.ID, &c.OrganizationID, &c.UserID, &c.InstanceID, &c.RemoteID, &c.SessionHash,
		&c.Kind, &c.Provider, &c.CloudAccountID, &c.PlannedAccountID, &c.AccountID, &c.State, &c.CreatedAt, &c.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &c, err
}

func (r *cloudLinkRepository) ManagedConsentAuthorized(ctx context.Context, c *models.CloudManagedConsent) (bool, error) {
	var allowed bool
	err := r.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM organizations o JOIN cloud_link l ON (l.organization_id = o.id OR l.organization_id IS NULL)
 WHERE o.id = $1 AND o.risk_state IN ('trusted', 'watch') AND l.instance_id = $2 AND NOT l.disconnect_pending
 AND (o.owner_user_id = $3 OR EXISTS (SELECT 1 FROM organization_members m
 WHERE m.organization_id = o.id AND m.user_id = $3 AND m.accepted_at IS NOT NULL AND (m.permissions::integer & $4) <> 0)))`,
		c.OrganizationID, c.InstanceID, c.UserID, int(models.PermManageEmails)).Scan(&allowed)
	return allowed, err
}

func (r *cloudLinkRepository) CreateManagedConsent(ctx context.Context, c *models.CloudManagedConsent) error {
	allowed, err := r.ManagedConsentAuthorized(ctx, c)
	if err != nil {
		return err
	}
	if !allowed {
		return errors.New("managed consent authority unavailable")
	}
	_, err = r.db.Exec(ctx, `INSERT INTO cloud_managed_consents
 (id, organization_id, user_id, instance_id, remote_id, session_hash, kind, provider, cloud_account_id, planned_account_id, consent_state, expires_at)
 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'pending',$11)`, c.ID, c.OrganizationID, c.UserID, c.InstanceID,
		c.RemoteID, c.SessionHash, c.Kind, c.Provider, c.CloudAccountID, c.PlannedAccountID, c.ExpiresAt)
	return err
}

func (r *cloudLinkRepository) GetManagedConsent(ctx context.Context, orgID, userID uuid.UUID, sessionHash string) (*models.CloudManagedConsent, error) {
	return scanManagedConsent(r.db.QueryRow(ctx, `SELECT `+managedConsentColumns+` FROM cloud_managed_consents
 WHERE organization_id = $1 AND user_id = $2 AND session_hash = $3`, orgID, userID, sessionHash))
}

func (r *cloudLinkRepository) ListManagedConsents(ctx context.Context, instanceID uuid.UUID) ([]models.CloudManagedConsent, error) {
	rows, err := r.db.Query(ctx, `SELECT `+managedConsentColumns+` FROM cloud_managed_consents
 WHERE instance_id = $1 AND consent_state IN ('pending','unknown','pending_remove') ORDER BY created_at`, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.CloudManagedConsent
	for rows.Next() {
		c, err := scanManagedConsent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (r *cloudLinkRepository) FindManagedAdoption(ctx context.Context, orgID, userID, instanceID, cloudID uuid.UUID) (*models.CloudManagedConsent, error) {
	return scanManagedConsent(r.db.QueryRow(ctx, `SELECT `+managedConsentColumns+` FROM cloud_managed_consents
 WHERE organization_id = $1 AND user_id = $2 AND instance_id = $3 AND cloud_account_id = $4
 AND kind = 'adopt' AND consent_state IN ('pending','unknown','active') ORDER BY created_at DESC LIMIT 1`, orgID, userID, instanceID, cloudID))
}

func (r *cloudLinkRepository) CanBrokerManagedToken(ctx context.Context, accountID uuid.UUID) (bool, error) {
	var allowed bool
	err := r.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM email_accounts ea
 JOIN organizations o ON o.id = ea.organization_id JOIN cloud_link_mailboxes clm ON clm.email_account_id = ea.id
 JOIN cloud_link l ON l.instance_id = clm.instance_id AND (l.organization_id = o.id OR l.organization_id IS NULL)
 LEFT JOIN LATERAL (`+warmupStandingSQL("ea.id")+`) standing ON true
 WHERE ea.id = $1 AND ea.status = 'active' AND o.risk_state IN ('trusted','watch')
 AND clm.managed AND clm.enrollment_state = 'active' AND NOT l.disconnect_pending
 AND clm.standing_observed_at > NOW() - INTERVAL '15 minutes'
 AND clm.standing_observed_at <= NOW()
 AND (standing.health_state NOT IN ('blocked','quarantined') OR standing.blocked_until <= NOW())
 AND NOT EXISTS (SELECT 1 FROM cloud_managed_consents c WHERE c.instance_id = l.instance_id
 AND (c.email_account_id = ea.id OR c.planned_account_id = ea.id) AND c.consent_state <> 'active'))`, accountID).Scan(&allowed)
	return allowed, err
}

func (r *cloudLinkRepository) SetManagedConsentState(ctx context.Context, orgID, id uuid.UUID, state string, accountID *uuid.UUID) error {
	tag, err := r.db.Exec(ctx, `UPDATE cloud_managed_consents SET consent_state = $3,
 email_account_id = COALESCE($4, email_account_id) WHERE organization_id = $1 AND id = $2
 AND (consent_state IN ('pending','unknown') OR consent_state = $3 OR $3 IN ('pending_remove','revoked','expired'))
 AND ($3 <> 'active' OR (cloud_account_id IS NOT NULL AND planned_account_id=$4 AND EXISTS (SELECT 1 FROM email_accounts a
 WHERE a.id=$4 AND a.organization_id=cloud_managed_consents.organization_id AND a.provider=cloud_managed_consents.provider AND a.status='active' AND a.auth_method='oauth')))
 AND ($3 <> 'active' OR EXISTS (SELECT 1 FROM organizations o JOIN cloud_link l ON (l.organization_id = o.id OR l.organization_id IS NULL)
 WHERE o.id = cloud_managed_consents.organization_id AND o.risk_state IN ('trusted','watch')
 AND l.instance_id = cloud_managed_consents.instance_id AND NOT l.disconnect_pending
 AND (o.owner_user_id = cloud_managed_consents.user_id OR EXISTS (SELECT 1 FROM organization_members m
 WHERE m.organization_id = o.id AND m.user_id = cloud_managed_consents.user_id AND m.accepted_at IS NOT NULL AND (m.permissions::integer & $5) <> 0))))`, orgID, id, state, accountID, int(models.PermManageEmails))
	if err == nil && tag.RowsAffected() != 1 {
		return errors.New("managed consent state changed")
	}
	return err
}

func (r *cloudLinkRepository) RevokeManagedConsents(ctx context.Context, instanceID uuid.UUID, accountID *uuid.UUID) error {
	_, err := r.db.Exec(ctx, `UPDATE cloud_managed_consents SET consent_state = 'pending_remove'
 WHERE instance_id = $1 AND ($2::uuid IS NULL OR email_account_id = $2 OR planned_account_id = $2)
 AND consent_state NOT IN ('revoked','expired')`, instanceID, accountID)
	return err
}

type PoolLinkManagedRepository interface {
	ManagedInstanceAuthorized(context.Context, uuid.UUID, uuid.UUID) (bool, error)
	WithManagedOperationLock(context.Context, uuid.UUID, func() error) error
	CreateManagedOperation(context.Context, *models.PoolLinkManagedOperation) error
	GetManagedOperation(context.Context, uuid.UUID, uuid.UUID) (*models.PoolLinkManagedOperation, error)
	ClaimManagedOperation(context.Context, uuid.UUID, uuid.UUID) (bool, error)
	CompleteManagedOperation(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error
	CompleteManagedActivation(context.Context, uuid.UUID, uuid.UUID) error
	RevokeManagedOperations(context.Context, uuid.UUID, *uuid.UUID) error
}

func (r *poolLinkRepository) ManagedInstanceAuthorized(ctx context.Context, instanceID, orgID uuid.UUID) (bool, error) {
	var allowed bool
	err := r.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pool_link_instances i JOIN organizations o ON o.id=i.organization_id
 WHERE i.id=$1 AND i.organization_id=$2 AND i.revoked_at IS NULL AND o.risk_state IN ('trusted','watch'))`, instanceID, orgID).Scan(&allowed)
	return allowed, err
}

func (r *poolLinkRepository) WithManagedOperationLock(ctx context.Context, instanceID uuid.UUID, fn func() error) error {
	conn, err := pgx.ConnectConfig(ctx, r.db.Config().ConnConfig)
	if err != nil {
		return err
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = conn.Close(closeCtx)
	}()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(266, hashtext($1))`, instanceID.String()); err != nil {
		return err
	}
	var orgID uuid.UUID
	if err := conn.QueryRow(ctx, `SELECT organization_id FROM pool_link_instances WHERE id = $1`, instanceID).Scan(&orgID); err != nil {
		return err
	}
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(267, hashtext($1))`, orgID.String()); err != nil {
		return err
	}
	return fn()
}

func (r *poolLinkRepository) CreateManagedOperation(ctx context.Context, c *models.PoolLinkManagedOperation) error {
	tag, err := r.db.Exec(ctx, `INSERT INTO pool_link_managed_operations
 (organization_id, instance_id, remote_id, session_hash, kind, provider, planned_account_id, consent_state, expires_at, activation_pending)
 SELECT $1,$2,$3,$4,$5,$6,$7,'pending',$8,true WHERE EXISTS (SELECT 1 FROM pool_link_instances i JOIN organizations o ON o.id=i.organization_id
 WHERE i.id = $2 AND i.organization_id = $1 AND i.revoked_at IS NULL AND o.risk_state IN ('trusted','watch'))
 ON CONFLICT (instance_id, remote_id) DO NOTHING`, c.OrganizationID, c.InstanceID, c.RemoteID, c.SessionHash,
		c.Kind, c.Provider, c.PlannedAccountID, c.ExpiresAt)
	if err == nil && tag.RowsAffected() == 0 {
		existing, readErr := r.GetManagedOperation(ctx, *c.InstanceID, *c.RemoteID)
		if readErr != nil {
			return readErr
		}
		if existing == nil || existing.Kind != c.Kind || existing.Provider != c.Provider ||
			(existing.SessionHash == nil) != (c.SessionHash == nil) ||
			(existing.SessionHash != nil && *existing.SessionHash != *c.SessionHash) ||
			(c.Kind == "adopt" && (existing.PlannedAccountID == nil || c.PlannedAccountID == nil || *existing.PlannedAccountID != *c.PlannedAccountID)) {
			return errors.New("managed operation identity conflict")
		}
	}
	return err
}

func (r *poolLinkRepository) GetManagedOperation(ctx context.Context, instanceID, remoteID uuid.UUID) (*models.PoolLinkManagedOperation, error) {
	var c models.PoolLinkManagedOperation
	err := r.db.QueryRow(ctx, `SELECT id, organization_id, instance_id, remote_id, session_hash, kind, COALESCE(provider::text,''),
 planned_account_id, email_account_id, consent_state, created_at, expires_at, completed_at, activation_pending
 FROM pool_link_managed_operations WHERE instance_id = $1 AND remote_id = $2`, instanceID, remoteID).
		Scan(&c.ID, &c.OrganizationID, &c.InstanceID, &c.RemoteID, &c.SessionHash, &c.Kind, &c.Provider,
			&c.PlannedAccountID, &c.AccountID, &c.State, &c.CreatedAt, &c.ExpiresAt, &c.CompletedAt, &c.ActivationPending)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &c, err
}

func (r *poolLinkRepository) ClaimManagedOperation(ctx context.Context, instanceID, remoteID uuid.UUID) (bool, error) {
	tag, err := r.db.Exec(ctx, `UPDATE pool_link_managed_operations SET consent_state = 'exchanging'
 WHERE instance_id = $1 AND remote_id = $2 AND consent_state = 'pending' AND expires_at > NOW()
 AND EXISTS (SELECT 1 FROM pool_link_instances i JOIN organizations o ON o.id=i.organization_id
 WHERE i.id=$1 AND i.revoked_at IS NULL AND o.risk_state IN ('trusted','watch'))`, instanceID, remoteID)
	return tag.RowsAffected() == 1, err
}

func (r *poolLinkRepository) CompleteManagedOperation(ctx context.Context, instanceID, remoteID, accountID uuid.UUID) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var orgID uuid.UUID
	err = tx.QueryRow(ctx, `SELECT c.organization_id FROM pool_link_managed_operations c
 JOIN pool_link_instances i ON i.id = c.instance_id AND i.organization_id = c.organization_id
 JOIN organizations o ON o.id=c.organization_id AND o.risk_state IN ('trusted','watch')
 JOIN email_accounts a ON a.id = $3 AND a.organization_id = c.organization_id AND a.provider = c.provider AND a.status = 'active' AND a.auth_method='oauth'
 WHERE c.instance_id = $1 AND c.remote_id = $2 AND c.planned_account_id = $3
 AND c.consent_state IN ('pending','exchanging','completed') AND (c.expires_at > NOW() OR c.consent_state = 'completed')
 AND i.revoked_at IS NULL FOR UPDATE OF c`, instanceID, remoteID, accountID).Scan(&orgID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO pool_link_mailboxes (instance_id, remote_id, email_account_id, managed)
 VALUES ($1,$2,$3,true) ON CONFLICT (instance_id, remote_id) DO NOTHING`, instanceID, remoteID, accountID)
	if err != nil {
		return err
	}
	var matches bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pool_link_mailboxes
 WHERE instance_id = $1 AND remote_id = $2 AND email_account_id = $3 AND managed)`, instanceID, remoteID, accountID).Scan(&matches); err != nil {
		return err
	}
	if !matches {
		return errors.New("managed enrollment identity conflict")
	}
	if _, err := tx.Exec(ctx, `UPDATE pool_link_managed_operations SET consent_state = 'completed',
 email_account_id = $3, completed_at = COALESCE(completed_at,NOW()) WHERE instance_id = $1 AND remote_id = $2`, instanceID, remoteID, accountID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *poolLinkRepository) RevokeManagedOperations(ctx context.Context, instanceID uuid.UUID, remoteID *uuid.UUID) error {
	if remoteID != nil {
		_, err := r.db.Exec(ctx, `INSERT INTO pool_link_managed_operations
 (organization_id, instance_id, remote_id, kind, consent_state, expires_at)
 SELECT organization_id, id, $2, 'revocation', 'revoked', NOW() FROM pool_link_instances WHERE id = $1
 ON CONFLICT (instance_id,remote_id) DO UPDATE SET consent_state = 'revoked'`, instanceID, remoteID)
		return err
	}
	_, err := r.db.Exec(ctx, `UPDATE pool_link_managed_operations SET consent_state = 'revoked'
 WHERE instance_id = $1 AND ($2::uuid IS NULL OR remote_id = $2)`, instanceID, remoteID)
	return err
}

func (r *poolLinkRepository) CompleteManagedActivation(ctx context.Context, instanceID, remoteID uuid.UUID) error {
	tag, err := r.db.Exec(ctx, `UPDATE pool_link_managed_operations SET activation_pending=false
 WHERE instance_id=$1 AND remote_id=$2 AND consent_state='completed'`, instanceID, remoteID)
	if err == nil && tag.RowsAffected() != 1 {
		return errors.New("managed operation authority changed")
	}
	return err
}
