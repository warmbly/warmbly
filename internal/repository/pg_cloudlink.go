package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/encrypt"
)

// CloudLinkRepository is the self-hosted side of pool link.
type CloudLinkRepository interface {
	// CanStore reports whether the instance token can be sealed at all. The
	// handshake is one-time: a token that cannot be written is gone, and the
	// link is left standing on the cloud with nobody holding it.
	CanStore() error
	Get(ctx context.Context, orgID *uuid.UUID) (*models.CloudLink, error)
	GetByInstance(ctx context.Context, instanceID uuid.UUID) (*models.CloudLink, error)
	GetForRedirect(ctx context.Context, orgID uuid.UUID, domain string) (*models.CloudLink, error)
	BindRedirect(ctx context.Context, orgID uuid.UUID, domain string, instanceID uuid.UUID) error
	ListLinks(ctx context.Context) ([]models.CloudLink, error)
	Put(ctx context.Context, link *models.CloudLink) error
	Delete(ctx context.Context, instanceID uuid.UUID) error
	SetSyncResult(ctx context.Context, instanceID uuid.UUID, at time.Time, lastError string) error
	WithReconciliationLock(ctx context.Context, fn func() error) error
	SetDisconnectPending(ctx context.Context, instanceID uuid.UUID) error
	BeginEnrollment(ctx context.Context, accountID, remoteID, instanceID uuid.UUID) error
	BeginRemoval(ctx context.Context, accountID uuid.UUID) error
	InvalidateStanding(ctx context.Context, accountID uuid.UUID) error

	Enroll(ctx context.Context, accountID, remoteID, instanceID uuid.UUID, managed bool) (*models.CloudLinkMailbox, error)
	Unenroll(ctx context.Context, accountID uuid.UUID) error
	UnenrollAll(ctx context.Context, instanceID uuid.UUID) error
	GetByAccount(ctx context.Context, accountID uuid.UUID) (*models.CloudLinkMailbox, error)
	List(ctx context.Context) ([]models.CloudLinkMailbox, error)
	ListForOrg(ctx context.Context, orgID uuid.UUID, accountID *uuid.UUID) ([]models.CloudLinkMailbox, error)
	// IsEnrolled is the hot-path check the warmup task and reconciler use.
	IsEnrolled(ctx context.Context, accountID uuid.UUID) (bool, error)

	// SetStanding records the warmup standing the cloud reported and returns
	// the state it replaced ("" when none was recorded yet). initial writes
	// only a mailbox with no standing yet, leaving changes to the sync, which
	// reports them.
	SetStanding(ctx context.Context, accountID uuid.UUID, h *models.WarmupHealthInfo, initial bool) (models.WarmupHealthState, error)
	// CarryStanding raises a mailbox's local pool row to a cloud quarantine or
	// block still in force, so leaving the cloud does not lift it.
	CarryStanding(ctx context.Context, accountID uuid.UUID, h *models.WarmupHealthInfo) error
}

type cloudLinkRepository struct {
	db      *pgxpool.Pool
	encrypt *encrypt.Encrypter
}

var errNoLinkEncrypter = errors.New("credential encrypter not configured (set CREDENTIALS_ENCRYPTION_KEY)")

// NewCloudLinkRepository seals the instance token with the mailbox credential key.
func NewCloudLinkRepository(db *pgxpool.Pool, enc *encrypt.Encrypter) CloudLinkRepository {
	return &cloudLinkRepository{db: db, encrypt: enc}
}

func (r *cloudLinkRepository) CanStore() error {
	if r.encrypt == nil {
		return errNoLinkEncrypter
	}
	return nil
}

const cloudLinkColumns = `cloud_url, instance_id, token, organization_name, connected_by, connected_at, last_synced_at, last_error, organization_id, disconnect_pending`

func (r *cloudLinkRepository) scanLink(row pgx.Row) (*models.CloudLink, error) {
	var l models.CloudLink
	var sealed string
	if err := row.Scan(&l.CloudURL, &l.InstanceID, &sealed, &l.OrganizationName, &l.ConnectedBy, &l.ConnectedAt, &l.LastSyncedAt, &l.LastError, &l.OrganizationID, &l.DisconnectPending); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if r.encrypt == nil {
		return nil, errNoLinkEncrypter
	}
	plain, err := r.encrypt.Decrypt(sealed)
	if err != nil {
		return nil, err
	}
	l.Token = plain
	return &l, nil
}

func (r *cloudLinkRepository) Get(ctx context.Context, orgID *uuid.UUID) (*models.CloudLink, error) {
	query := `SELECT ` + cloudLinkColumns + ` FROM cloud_link
 WHERE organization_id = $1 OR organization_id IS NULL
 ORDER BY organization_id IS NULL LIMIT 1`
	return r.scanLink(r.db.QueryRow(ctx, query, orgID))
}

func (r *cloudLinkRepository) GetByInstance(ctx context.Context, instanceID uuid.UUID) (*models.CloudLink, error) {
	return r.scanLink(r.db.QueryRow(ctx, `SELECT `+cloudLinkColumns+` FROM cloud_link WHERE instance_id = $1`, instanceID))
}

func (r *cloudLinkRepository) ListLinks(ctx context.Context) ([]models.CloudLink, error) {
	rows, err := r.db.Query(ctx, `SELECT `+cloudLinkColumns+` FROM cloud_link ORDER BY connected_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.CloudLink{}
	for rows.Next() {
		l, err := r.scanLink(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *l)
	}
	return out, rows.Err()
}

func (r *cloudLinkRepository) GetForRedirect(ctx context.Context, orgID uuid.UUID, domain string) (*models.CloudLink, error) {
	query := `SELECT ` + cloudLinkColumns + ` FROM cloud_link WHERE instance_id = COALESCE(
  (SELECT cloud_link_instance_id FROM domain_redirects WHERE organization_id = $1 AND domain = $2),
  (SELECT instance_id FROM cloud_link WHERE organization_id = $1 LIMIT 1)
 )`
	return r.scanLink(r.db.QueryRow(ctx, query, orgID, domain))
}

func (r *cloudLinkRepository) BindRedirect(ctx context.Context, orgID uuid.UUID, domain string, instanceID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `UPDATE domain_redirects SET cloud_link_instance_id = $3
 WHERE organization_id = $1 AND domain = $2 AND served_by = 'cloud' AND cloud_link_instance_id IS NULL`, orgID, domain, instanceID)
	return err
}

func (r *cloudLinkRepository) Put(ctx context.Context, link *models.CloudLink) error {
	if r.encrypt == nil {
		return errNoLinkEncrypter
	}
	sealed, err := r.encrypt.Encrypt(link.Token)
	if err != nil {
		return err
	}
	query := `
 INSERT INTO cloud_link (cloud_url, instance_id, token, organization_name, connected_by, organization_id)
 VALUES ($1, $2, $3, $4, $5, $6)
 RETURNING connected_at
 `
	if err := r.db.QueryRow(ctx, query, link.CloudURL, link.InstanceID, sealed, link.OrganizationName, link.ConnectedBy, link.OrganizationID).Scan(&link.ConnectedAt); err != nil {
		db.CaptureError(err, query, nil, "queryrow")
		return err
	}
	return nil
}

func (r *cloudLinkRepository) Delete(ctx context.Context, instanceID uuid.UUID) error {
	if _, err := r.db.Exec(ctx, `DELETE FROM cloud_link WHERE instance_id = $1`, instanceID); err != nil {
		db.CaptureError(err, "delete cloud_link", nil, "exec")
		return err
	}
	return nil
}

func (r *cloudLinkRepository) SetSyncResult(ctx context.Context, instanceID uuid.UUID, at time.Time, lastError string) error {
	_, err := r.db.Exec(ctx, `UPDATE cloud_link SET last_synced_at = CASE WHEN $2 = '' THEN $1 ELSE last_synced_at END, last_error = $2 WHERE instance_id = $3`, at, lastError, instanceID)
	return err
}

func (r *cloudLinkRepository) Enroll(ctx context.Context, accountID, remoteID, instanceID uuid.UUID, managed bool) (*models.CloudLinkMailbox, error) {
	query := `
		INSERT INTO cloud_link_mailboxes (email_account_id, remote_id, managed, instance_id)
		SELECT ea.id, $2, $3, l.instance_id FROM email_accounts ea JOIN cloud_link l
		  ON (l.organization_id = ea.organization_id OR l.organization_id IS NULL)
		WHERE ea.id = $1 AND l.instance_id = $4 AND NOT l.disconnect_pending
		ON CONFLICT (email_account_id) DO UPDATE SET remote_id = EXCLUDED.remote_id, managed = EXCLUDED.managed, enrollment_state = 'active'
		WHERE cloud_link_mailboxes.enrollment_state <> 'pending_remove' AND cloud_link_mailboxes.instance_id = EXCLUDED.instance_id
		RETURNING ` + cloudLinkMailboxColumns
	m, err := scanCloudLinkMailbox(r.db.QueryRow(ctx, query, accountID, remoteID, managed, instanceID))
	if err != nil {
		db.CaptureError(err, query, nil, "queryrow")
		return nil, err
	}
	return m, nil
}

func (r *cloudLinkRepository) Unenroll(ctx context.Context, accountID uuid.UUID) error {
	if _, err := r.db.Exec(ctx, `DELETE FROM cloud_link_mailboxes WHERE email_account_id = $1`, accountID); err != nil {
		db.CaptureError(err, "delete cloud_link_mailboxes", []any{accountID}, "exec")
		return err
	}
	return nil
}

func (r *cloudLinkRepository) UnenrollAll(ctx context.Context, instanceID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM cloud_link_mailboxes WHERE instance_id = $1`, instanceID)
	return err
}

func (r *cloudLinkRepository) GetByAccount(ctx context.Context, accountID uuid.UUID) (*models.CloudLinkMailbox, error) {
	query := `SELECT ` + cloudLinkMailboxColumns + ` FROM cloud_link_mailboxes WHERE email_account_id = $1`
	m, err := scanCloudLinkMailbox(r.db.QueryRow(ctx, query, accountID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		db.CaptureError(err, query, []any{accountID}, "queryrow")
		return nil, err
	}
	return m, nil
}

func (r *cloudLinkRepository) List(ctx context.Context) ([]models.CloudLinkMailbox, error) {
	query := `SELECT ` + cloudLinkMailboxColumns + ` FROM cloud_link_mailboxes ORDER BY enrolled_at`
	rows, err := r.db.Query(ctx, query)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	out := []models.CloudLinkMailbox{}
	for rows.Next() {
		m, err := scanCloudLinkMailbox(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

func (r *cloudLinkRepository) IsEnrolled(ctx context.Context, accountID uuid.UUID) (bool, error) {
	var ok bool
	if err := r.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM cloud_link_mailboxes WHERE email_account_id = $1)`, accountID).Scan(&ok); err != nil {
		return false, err
	}
	return ok, nil
}

const cloudLinkMailboxColumns = `email_account_id, remote_id, enrolled_at, managed,
	health_state, health_pool_type, health_reason, health_score, blocked_until, health_evaluated_at, instance_id,
	enrollment_state, standing_observed_at`

func scanCloudLinkMailbox(row pgx.Row) (*models.CloudLinkMailbox, error) {
	var m models.CloudLinkMailbox
	var state, poolType, reason *string
	var score float64
	var blockedUntil, evaluatedAt *time.Time
	if err := row.Scan(&m.EmailAccountID, &m.RemoteID, &m.EnrolledAt, &m.Managed,
		&state, &poolType, &reason, &score, &blockedUntil, &evaluatedAt, &m.InstanceID, &m.EnrollmentState, &m.StandingObservedAt); err != nil {
		return nil, err
	}
	if state != nil {
		m.Standing = &models.WarmupHealthInfo{
			Source:       models.WarmupHealthSourceCloud,
			State:        *state,
			Score:        score,
			BlockedUntil: blockedUntil,
			EvaluatedAt:  evaluatedAt,
		}
		if poolType != nil {
			m.Standing.PoolType = *poolType
		}
		if reason != nil {
			m.Standing.Reason = *reason
		}
	}
	return &m, nil
}

func (r *cloudLinkRepository) SetStanding(ctx context.Context, accountID uuid.UUID, h *models.WarmupHealthInfo, initial bool) (models.WarmupHealthState, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	// The locked read makes a concurrent writer see this write's state as its
	// previous one, so a transition is reported once across consumers.
	query := `
		WITH prev AS (
			SELECT email_account_id, health_state
			  FROM cloud_link_mailboxes
			 WHERE email_account_id = $1
			   FOR UPDATE
		)
		UPDATE cloud_link_mailboxes c
		   SET health_state = $2,
		       health_pool_type = NULLIF($3, ''),
		       health_reason = NULLIF($4, ''),
		       health_score = $5,
		       blocked_until = $6,
		       health_evaluated_at = $7,
		       health_synced_at = NOW(),
		       standing_observed_at = NOW()
		  FROM prev
		 WHERE c.email_account_id = prev.email_account_id
		   AND (NOT $8 OR prev.health_state IS NULL)
		RETURNING COALESCE(prev.health_state, '')
	`
	var prev string
	err = tx.QueryRow(ctx, query, accountID, h.State, h.PoolType, h.Reason, h.Score, h.BlockedUntil, h.EvaluatedAt, initial).Scan(&prev)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		db.CaptureError(err, query, []any{accountID}, "queryrow")
		return "", err
	}
	if err := storeCloudHold(ctx, tx, accountID, h); err != nil {
		return "", err
	}
	return models.WarmupHealthState(prev), tx.Commit(ctx)
}

func (r *cloudLinkRepository) CarryStanding(ctx context.Context, accountID uuid.UUID, h *models.WarmupHealthInfo) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	query := `
		UPDATE warmup_pool_participants p
		   SET health_state = $2,
		       blocked_until = $3,
		       blocked_at = COALESCE(p.blocked_at, NOW()),
		       blocked_reason = NULLIF($4, ''),
		       last_health_reason = NULLIF($4, ''),
		       last_health_score = $5,
		       last_health_evaluated_at = NOW()
		 WHERE p.email_account_id = $1
		   AND (` + warmupStandingRankSQL("p.health_state::text") + ` < ` + warmupStandingRankSQL("$2::text") + `
		        OR (` + warmupStandingRankSQL("p.health_state::text") + ` = ` + warmupStandingRankSQL("$2::text") + `
		            AND p.blocked_until IS NOT NULL
		            AND ($3::timestamptz IS NULL OR $3::timestamptz > p.blocked_until)))
	`
	if _, err := tx.Exec(ctx, query, accountID, h.State, h.BlockedUntil, h.Reason, h.Score); err != nil {
		db.CaptureError(err, query, []any{accountID}, "exec")
		return err
	}
	if err := storeCloudHold(ctx, tx, accountID, h); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func storeCloudHold(ctx context.Context, tx pgx.Tx, accountID uuid.UUID, h *models.WarmupHealthInfo) error {
	if h.State != string(models.WarmupHealthBlocked) && h.State != string(models.WarmupHealthQuarantined) {
		_, err := tx.Exec(ctx, `UPDATE warmup_reputation_ledger l SET cloud_health_state = NULL,
		 cloud_blocked_until = NULL, cloud_health_reason = NULL, cloud_health_score = 0, cloud_source_account_id = NULL, cloud_source_instance_id = NULL
		 FROM email_accounts ea WHERE ea.id = $1 AND l.organization_id = ea.organization_id
		 AND l.email = lower(btrim(ea.email)) AND l.cloud_source_account_id = $1
		 AND l.cloud_source_instance_id IS NOT DISTINCT FROM (SELECT instance_id FROM cloud_link_mailboxes WHERE email_account_id = $1)`, accountID)
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO warmup_reputation_ledger
	 (organization_id, email, cloud_health_state, cloud_blocked_until, cloud_health_reason, cloud_health_score, cloud_source_account_id, cloud_source_instance_id, standing_until)
	 SELECT organization_id, lower(btrim(email)), $2, $3, $4, $5, $1, (SELECT instance_id FROM cloud_link_mailboxes WHERE email_account_id = $1), NOW() FROM email_accounts WHERE id = $1
	 ON CONFLICT (organization_id, email) DO UPDATE SET
	 cloud_health_state = EXCLUDED.cloud_health_state, cloud_blocked_until = EXCLUDED.cloud_blocked_until,
	 cloud_health_reason = EXCLUDED.cloud_health_reason, cloud_health_score = EXCLUDED.cloud_health_score,
	 cloud_source_account_id = EXCLUDED.cloud_source_account_id, cloud_source_instance_id = EXCLUDED.cloud_source_instance_id, recorded_at = NOW()
	 WHERE warmup_reputation_ledger.cloud_health_state IS NULL
	 OR warmup_reputation_ledger.cloud_blocked_until <= NOW()
	 OR (warmup_reputation_ledger.cloud_source_account_id = $1 AND warmup_reputation_ledger.cloud_source_instance_id IS NOT DISTINCT FROM EXCLUDED.cloud_source_instance_id)
	 OR (`+warmupStandingRankSQL("warmup_reputation_ledger.cloud_health_state")+` < `+warmupStandingRankSQL("EXCLUDED.cloud_health_state")+`)
	 OR (warmup_reputation_ledger.cloud_health_state = EXCLUDED.cloud_health_state
	 AND warmup_reputation_ledger.cloud_blocked_until IS NOT NULL
	 AND (EXCLUDED.cloud_blocked_until IS NULL OR EXCLUDED.cloud_blocked_until > warmup_reputation_ledger.cloud_blocked_until))`, accountID, h.State, h.BlockedUntil, h.Reason, h.Score)
	return err
}
