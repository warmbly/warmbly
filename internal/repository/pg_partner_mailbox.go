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

// PartnerMailboxView is a partner mailbox with what the partner API reads of its Warmbly side.
type PartnerMailboxView struct {
	Mailbox            models.PartnerMailbox
	Status             string
	WarmupState        string
	AccountStatus      *string
	WarmupAt           *time.Time
	WarmupBase         *int
	WarmupMax          *int
	WarmupIncrease     *int
	HealthReason       *string
	WarmupSent7d       int
	CampaignSentToday  int
	PlacementMajor     int
	PlacementMajorSpam int
	// ActionRequired is the step only a person can take before the mailbox connects, nil when none.
	ActionRequired *string
}

// PartnerMailboxCursor pages a workspace's mailboxes oldest first.
type PartnerMailboxCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// PartnerConnectLink is a partner's hosted connect link, joined to the client connect link it rides on.
type PartnerConnectLink struct {
	LinkID           uuid.UUID
	PartnerID        uuid.UUID
	Mode             models.PartnerMode
	OrganizationID   uuid.UUID
	OrgExternalID    string
	PartnerMailboxID string
	Provider         string
	Enablement       models.PartnerEnablement
	LoginHint        string
	ReturnURL        string
}

// ErrPartnerMailboxTaken is a mailbox row that already holds the email account.
var ErrPartnerMailboxTaken = errors.New("partner mailbox: the mailbox already has a row")

// PartnerMailboxRepository stores the mailbox half of the partner API.
type PartnerMailboxRepository interface {
	GetPartnerMailbox(ctx context.Context, partnerID uuid.UUID, mode models.PartnerMode, orgExternalID string, id uuid.UUID) (*PartnerMailboxView, error)
	GetPartnerMailboxByPartnerID(ctx context.Context, partnerID uuid.UUID, mode models.PartnerMode, orgExternalID, partnerMailboxID string) (*PartnerMailboxView, error)
	GetPartnerMailboxByID(ctx context.Context, id uuid.UUID) (*PartnerMailboxView, error)
	// PartnerMailboxForAccount is the live row holding an email account, nil when none.
	PartnerMailboxForAccount(ctx context.Context, emailAccountID uuid.UUID) (*models.PartnerMailbox, error)
	ListPartnerMailboxes(ctx context.Context, partnerID uuid.UUID, mode models.PartnerMode, orgExternalID string, after *PartnerMailboxCursor, limit int) ([]PartnerMailboxView, error)
	ListOrganizationPartnerMailboxes(ctx context.Context, orgID uuid.UUID) ([]models.PartnerMailbox, error)
	InsertPartnerMailbox(ctx context.Context, m *models.PartnerMailbox) error
	// SavePartnerMailbox writes the attach fields of an existing row.
	SavePartnerMailbox(ctx context.Context, m *models.PartnerMailbox) error
	// SetPartnerEnablement records an enablement from at; changed is false when it already held.
	SetPartnerEnablement(ctx context.Context, id uuid.UUID, e models.PartnerEnablement, at time.Time) (changed bool, err error)
	// TurnOffPartnerOrganization sets every mailbox of a workspace off, returning the ids it changed.
	TurnOffPartnerOrganization(ctx context.Context, orgID uuid.UUID, at time.Time) ([]uuid.UUID, error)
	DetachPartnerMailbox(ctx context.Context, id uuid.UUID, at time.Time) error
	PartnerEnabledCounts(ctx context.Context, orgID uuid.UUID) (sendingWarmup, warmupOnly int, err error)
	PartnerWarmupScores(ctx context.Context, orgID uuid.UUID) (sum, n int, err error)

	// AdoptPartnerMailboxes gives every mailbox connected inside a managed workspace a row, off; returns the new ids.
	AdoptPartnerMailboxes(ctx context.Context, limit int) ([]uuid.UUID, error)
	// MarkRemovedPartnerMailboxes closes rows whose mailbox was deleted; returns their ids.
	MarkRemovedPartnerMailboxes(ctx context.Context, limit int) ([]uuid.UUID, error)
	// ChangedPartnerMailboxes are rows whose status, enablement or warmup state moved since it was published.
	ChangedPartnerMailboxes(ctx context.Context, limit int) ([]uuid.UUID, error)
	SetPartnerMailboxPublished(ctx context.Context, id uuid.UUID, state string) error
	ClaimPartnerConnects(ctx context.Context, limit int, lease time.Duration) ([]models.PartnerMailbox, error)
	FinishPartnerConnect(ctx context.Context, id uuid.UUID, accountID *uuid.UUID, state string, connectErr *string, next *time.Time) error
	// AwaitPartnerAction parks a connect on a step only a person can take, retrying at next without spending its attempts.
	AwaitPartnerAction(ctx context.Context, id uuid.UUID, action, msg string, next time.Time) error
	// WakePartnerAction makes a workspace's connects waiting on a step for these domains due now.
	WakePartnerAction(ctx context.Context, orgID uuid.UUID, action string, domains []string) error
	// PartnerMailboxOnDomain reports whether the partner has a live mailbox of provider on domain in the workspace.
	PartnerMailboxOnDomain(ctx context.Context, partnerID uuid.UUID, mode models.PartnerMode, orgExternalID, provider, domain string) (bool, error)
	// PartnerTokenMailbox is the token-method row for an address in a workspace, nil when none.
	PartnerTokenMailbox(ctx context.Context, orgID uuid.UUID, email string) (*models.PartnerMailbox, error)
	// PartnerMailboxLock is the display name of the partner holding an attached mailbox of the workspace, "" when none does.
	PartnerMailboxLock(ctx context.Context, orgID, emailAccountID uuid.UUID) (string, error)

	PartnerMailboxDays(ctx context.Context, partnerID uuid.UUID, mode models.PartnerMode, start, end, now time.Time) ([]models.PartnerMailboxDays, error)

	InsertPartnerConnectLink(ctx context.Context, l *PartnerConnectLink) error
	GetPartnerConnectLink(ctx context.Context, linkID uuid.UUID) (*PartnerConnectLink, error)

	UpsertPartnerGroup(ctx context.Context, g *models.PartnerGroup) (*models.PartnerGroup, error)
	GetPartnerGroup(ctx context.Context, partnerID uuid.UUID, mode models.PartnerMode, externalID string) (*models.PartnerGroup, error)
	// PartnerGroupForOrganization is the group a managed workspace was last sent with, nil when none.
	PartnerGroupForOrganization(ctx context.Context, orgID uuid.UUID) (*models.PartnerGroup, error)
	DuePartnerPortals(ctx context.Context, before time.Time, limit int) ([]models.PartnerGroup, error)
	SetPartnerPortalStatus(ctx context.Context, id uuid.UUID, status string, verifiedAt *time.Time) error

	GetPartnerStatement(ctx context.Context, partnerID uuid.UUID, mode models.PartnerMode, month time.Time) (*models.PartnerStatement, error)
	ListPartnerStatements(ctx context.Context, partnerID uuid.UUID, mode models.PartnerMode, limit int) ([]models.PartnerStatement, error)
	// InsertPartnerStatement writes a closed month once; a month already closed is left as it is.
	InsertPartnerStatement(ctx context.Context, st *models.PartnerStatement) error
	// FirstPartnerEnablement is when the partner's earliest enablement started, nil when it has none.
	FirstPartnerEnablement(ctx context.Context, partnerID uuid.UUID, mode models.PartnerMode) (*time.Time, error)
}

type partnerMailboxRepository struct{ db *pgxpool.Pool }

func NewPartnerMailboxRepository(db *pgxpool.Pool) PartnerMailboxRepository {
	return &partnerMailboxRepository{db: db}
}

const partnerMailboxColumns = `pm.id, pm.partner_id, pm.mode, pm.org_external_id, pm.organization_id, pm.email_account_id, pm.partner_mailbox_id,
	pm.source, pm.email, pm.provider, pm.first_name, pm.last_name, pm.enablement, pm.connection_method, pm.connect_state, pm.connect_error,
	pm.connect_attempts, pm.next_connect_at, pm.detached_at, pm.removed_at, pm.published_state, pm.created_at, pm.updated_at`

func partnerMailboxDest(m *models.PartnerMailbox) []any {
	return []any{&m.ID, &m.PartnerID, &m.Mode, &m.OrgExternalID, &m.OrganizationID, &m.EmailAccountID, &m.PartnerMailboxID,
		&m.Source, &m.Email, &m.Provider, &m.FirstName, &m.LastName, &m.Enablement, &m.ConnectionMethod, &m.ConnectState, &m.ConnectError,
		&m.ConnectAttempts, &m.NextConnectAt, &m.DetachedAt, &m.RemovedAt, &m.PublishedState, &m.CreatedAt, &m.UpdatedAt}
}

func scanPartnerMailbox(row pgx.Row) (*models.PartnerMailbox, error) {
	var m models.PartnerMailbox
	err := row.Scan(partnerMailboxDest(&m)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// partnerMailboxJoins reads a row's mailbox and its warmup standing.
const partnerMailboxJoins = `
	FROM partner_mailboxes pm
	LEFT JOIN email_accounts ea ON ea.id = pm.email_account_id
	LEFT JOIN LATERAL (SELECT p.health_state, p.last_health_reason FROM warmup_pool_participants p
		WHERE p.email_account_id = ea.id ORDER BY p.joined_at LIMIT 1) wpp ON true`

// partnerMailboxStatusSQL is the partner API's mailbox status; the one definition the view and the change feed read.
const partnerMailboxStatusSQL = `(CASE
	WHEN pm.detached_at IS NOT NULL OR pm.removed_at IS NOT NULL THEN 'disconnected'
	WHEN pm.connect_state = 'failed' THEN 'error'
	WHEN ea.id IS NULL THEN 'connecting'
	WHEN ea.status <> 'active' THEN 'error'
	WHEN ea.last_synced_at IS NULL THEN 'connecting'
	ELSE 'connected' END)`

// partnerWarmupStateSQL is the partner API's warmup state: off, ramping, or the mailbox's health band.
const partnerWarmupStateSQL = `(CASE
	WHEN pm.enablement = 'off' OR ea.id IS NULL OR ea.warmup IS NULL OR ea.warmup_paused_at IS NOT NULL
	  OR pm.detached_at IS NOT NULL OR pm.removed_at IS NOT NULL THEN 'off'
	WHEN wpp.health_state IS NOT NULL AND wpp.health_state <> 'healthy' THEN wpp.health_state::text
	WHEN ea.warmup_base + ea.warmup_increase * GREATEST(0, floor(extract(epoch FROM localtimestamp - ea.warmup) / 86400))::int < ea.warmup_max THEN 'ramping'
	ELSE 'healthy' END)`

// partnerActionRequiredSQL is the step a mailbox waits on, only while it is still waiting to connect.
const partnerActionRequiredSQL = `(CASE WHEN pm.email_account_id IS NULL AND pm.connect_state IN ('pending', 'connecting')
	AND pm.detached_at IS NULL AND pm.removed_at IS NULL THEN pm.action_required END)`

// partnerPublishedStateSQL is what a mailbox.updated announces a change of; a mailbox with nothing to do keeps its old fingerprint.
const partnerPublishedStateSQL = `(` + partnerMailboxStatusSQL + ` || '|' || pm.enablement || '|' || ` + partnerWarmupStateSQL +
	` || COALESCE('|' || ` + partnerActionRequiredSQL + `, ''))`

var partnerMailboxViewSelect = `SELECT ` + partnerMailboxColumns + `, ` + partnerMailboxStatusSQL + `, ` + partnerWarmupStateSQL + `,
	ea.status::text, ea.warmup, ea.warmup_base, ea.warmup_max, ea.warmup_increase, wpp.last_health_reason,
	CASE WHEN ea.id IS NULL THEN 0 ELSE (SELECT count(*) FROM tasks t WHERE t.email_account_id = ea.id AND t.task_type = 'warmup'
		AND t.status = 'completed' AND t.completed_at > now() - interval '7 days') END,
	CASE WHEN ea.id IS NULL THEN 0 ELSE ` + partnerCampaignSentTodaySQL("ea.id", "NULL::uuid") + ` END,
	COALESCE(pe.major, 0), COALESCE(pe.major_spam, 0), ` + partnerActionRequiredSQL + partnerMailboxJoins + `
	LEFT JOIN LATERAL (` + placementEvidenceSQL("ea.id", "now() - interval '7 days'") + `) pe ON true`

func scanPartnerMailboxView(row pgx.Row) (*PartnerMailboxView, error) {
	var v PartnerMailboxView
	dest := append(partnerMailboxDest(&v.Mailbox), &v.Status, &v.WarmupState, &v.AccountStatus, &v.WarmupAt, &v.WarmupBase, &v.WarmupMax,
		&v.WarmupIncrease, &v.HealthReason, &v.WarmupSent7d, &v.CampaignSentToday, &v.PlacementMajor, &v.PlacementMajorSpam, &v.ActionRequired)
	err := row.Scan(dest...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func (r *partnerMailboxRepository) GetPartnerMailbox(ctx context.Context, partnerID uuid.UUID, mode models.PartnerMode, orgExternalID string, id uuid.UUID) (*PartnerMailboxView, error) {
	return scanPartnerMailboxView(r.db.QueryRow(ctx, partnerMailboxViewSelect+`
		WHERE pm.partner_id = $1 AND pm.mode = $2 AND pm.org_external_id = $3 AND pm.id = $4`, partnerID, mode, orgExternalID, id))
}

func (r *partnerMailboxRepository) GetPartnerMailboxByPartnerID(ctx context.Context, partnerID uuid.UUID, mode models.PartnerMode, orgExternalID, partnerMailboxID string) (*PartnerMailboxView, error) {
	return scanPartnerMailboxView(r.db.QueryRow(ctx, partnerMailboxViewSelect+`
		WHERE pm.partner_id = $1 AND pm.mode = $2 AND pm.org_external_id = $3 AND pm.partner_mailbox_id = $4`, partnerID, mode, orgExternalID, partnerMailboxID))
}

func (r *partnerMailboxRepository) GetPartnerMailboxByID(ctx context.Context, id uuid.UUID) (*PartnerMailboxView, error) {
	return scanPartnerMailboxView(r.db.QueryRow(ctx, partnerMailboxViewSelect+` WHERE pm.id = $1`, id))
}

func (r *partnerMailboxRepository) PartnerMailboxForAccount(ctx context.Context, emailAccountID uuid.UUID) (*models.PartnerMailbox, error) {
	return scanPartnerMailbox(r.db.QueryRow(ctx, `SELECT `+partnerMailboxColumns+` FROM partner_mailboxes pm
		WHERE pm.email_account_id = $1 AND pm.detached_at IS NULL AND pm.removed_at IS NULL`, emailAccountID))
}

func (r *partnerMailboxRepository) ListPartnerMailboxes(ctx context.Context, partnerID uuid.UUID, mode models.PartnerMode, orgExternalID string, after *PartnerMailboxCursor, limit int) ([]PartnerMailboxView, error) {
	var at *time.Time
	var id *uuid.UUID
	if after != nil {
		at, id = &after.CreatedAt, &after.ID
	}
	rows, err := r.db.Query(ctx, partnerMailboxViewSelect+`
		WHERE pm.partner_id = $1 AND pm.mode = $2 AND pm.org_external_id = $3 AND pm.detached_at IS NULL AND pm.removed_at IS NULL
		  AND ($4::timestamptz IS NULL OR (pm.created_at, pm.id) > ($4::timestamptz, $5::uuid))
		ORDER BY pm.created_at, pm.id LIMIT $6`, partnerID, mode, orgExternalID, at, id, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PartnerMailboxView{}
	for rows.Next() {
		v, err := scanPartnerMailboxView(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func (r *partnerMailboxRepository) ListOrganizationPartnerMailboxes(ctx context.Context, orgID uuid.UUID) ([]models.PartnerMailbox, error) {
	rows, err := r.db.Query(ctx, `SELECT `+partnerMailboxColumns+` FROM partner_mailboxes pm
		WHERE pm.organization_id = $1 AND pm.email_account_id IS NOT NULL AND pm.removed_at IS NULL ORDER BY pm.created_at, pm.id`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.PartnerMailbox{}
	for rows.Next() {
		m, err := scanPartnerMailbox(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

func (r *partnerMailboxRepository) InsertPartnerMailbox(ctx context.Context, m *models.PartnerMailbox) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	err := r.db.QueryRow(ctx, `
		INSERT INTO partner_mailboxes (id, partner_id, mode, org_external_id, organization_id, email_account_id, partner_mailbox_id, source,
			email, provider, first_name, last_name, enablement, connection_method, connect_state, next_connect_at, published_state)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, 'off', $13, $14, $15, $16)
		RETURNING created_at, updated_at`,
		m.ID, m.PartnerID, m.Mode, m.OrgExternalID, m.OrganizationID, m.EmailAccountID, m.PartnerMailboxID, m.Source,
		m.Email, m.Provider, m.FirstName, m.LastName, m.ConnectionMethod, m.ConnectState, m.NextConnectAt, m.PublishedState).
		Scan(&m.CreatedAt, &m.UpdatedAt)
	if isUniqueViolation(err) {
		return ErrPartnerMailboxTaken
	}
	m.Enablement = models.PartnerEnablementOff
	return err
}

func (r *partnerMailboxRepository) SavePartnerMailbox(ctx context.Context, m *models.PartnerMailbox) error {
	_, err := r.db.Exec(ctx, `
		UPDATE partner_mailboxes SET email_account_id = $2, partner_mailbox_id = $3, source = $4, email = $5, provider = $6,
			first_name = $7, last_name = $8, connection_method = $9, connect_state = $10, connect_error = $11, connect_attempts = $12,
			next_connect_at = $13, detached_at = $14, removed_at = $15, updated_at = now()
		WHERE id = $1`,
		m.ID, m.EmailAccountID, m.PartnerMailboxID, m.Source, m.Email, m.Provider, m.FirstName, m.LastName, m.ConnectionMethod,
		m.ConnectState, m.ConnectError, m.ConnectAttempts, m.NextConnectAt, m.DetachedAt, m.RemovedAt)
	if isUniqueViolation(err) {
		return ErrPartnerMailboxTaken
	}
	return err
}

// setEnablementTx closes the open span and opens the new one in the caller's transaction.
func setEnablementTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, e models.PartnerEnablement, at time.Time) (bool, error) {
	var current string
	if err := tx.QueryRow(ctx, `SELECT enablement FROM partner_mailboxes WHERE id = $1 FOR UPDATE`, id).Scan(&current); err != nil {
		return false, err
	}
	if current == string(e) {
		return false, nil
	}
	if _, err := tx.Exec(ctx, `UPDATE partner_mailbox_enablements SET ended_at = GREATEST($2, started_at)
		WHERE mailbox_id = $1 AND ended_at IS NULL`, id, at); err != nil {
		return false, err
	}
	if e != models.PartnerEnablementOff {
		if _, err := tx.Exec(ctx, `INSERT INTO partner_mailbox_enablements (mailbox_id, enablement, started_at) VALUES ($1, $2, $3)`, id, e, at); err != nil {
			return false, err
		}
	}
	_, err := tx.Exec(ctx, `UPDATE partner_mailboxes SET enablement = $2, updated_at = now() WHERE id = $1`, id, e)
	return err == nil, err
}

func (r *partnerMailboxRepository) SetPartnerEnablement(ctx context.Context, id uuid.UUID, e models.PartnerEnablement, at time.Time) (bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	changed, err := setEnablementTx(ctx, tx, id, e, at)
	if err != nil {
		return false, err
	}
	return changed, tx.Commit(ctx)
}

func (r *partnerMailboxRepository) TurnOffPartnerOrganization(ctx context.Context, orgID uuid.UUID, at time.Time) ([]uuid.UUID, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	rows, err := tx.Query(ctx, `SELECT id FROM partner_mailboxes WHERE organization_id = $1 AND enablement <> 'off' ORDER BY id`, orgID)
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		if _, err := setEnablementTx(ctx, tx, id, models.PartnerEnablementOff, at); err != nil {
			return nil, err
		}
	}
	return ids, tx.Commit(ctx)
}

func (r *partnerMailboxRepository) DetachPartnerMailbox(ctx context.Context, id uuid.UUID, at time.Time) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := setEnablementTx(ctx, tx, id, models.PartnerEnablementOff, at); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE partner_mailboxes SET detached_at = $2, connect_state = CASE WHEN connect_state = 'connected' THEN 'connected' ELSE 'failed' END,
		next_connect_at = NULL, updated_at = now() WHERE id = $1`, id, at); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *partnerMailboxRepository) PartnerEnabledCounts(ctx context.Context, orgID uuid.UUID) (int, int, error) {
	var sw, wo int
	err := r.db.QueryRow(ctx, `SELECT count(*) FILTER (WHERE enablement = 'sending_warmup'), count(*) FILTER (WHERE enablement = 'warmup_only')
		FROM partner_mailboxes WHERE organization_id = $1 AND detached_at IS NULL AND removed_at IS NULL`, orgID).Scan(&sw, &wo)
	return sw, wo, err
}

// PartnerWarmupScores sums the 7-day inbox rate at the providers that judge a sender, over mailboxes with a sample.
func (r *partnerMailboxRepository) PartnerWarmupScores(ctx context.Context, orgID uuid.UUID) (int, int, error) {
	var sum, n int
	err := r.db.QueryRow(ctx, `SELECT COALESCE(sum(round(100.0 * (pe.major - pe.major_spam) / pe.major)), 0)::int, count(*)::int
		FROM email_accounts ea
		CROSS JOIN LATERAL (`+placementEvidenceSQL("ea.id", "now() - interval '7 days'")+`) pe
		WHERE ea.organization_id = $1 AND ea.warmup IS NOT NULL AND ea.warmup_paused_at IS NULL AND pe.major >= $2`,
		orgID, models.PartnerScoreMinDeliveries).Scan(&sum, &n)
	return sum, n, err
}

func (r *partnerMailboxRepository) AdoptPartnerMailboxes(ctx context.Context, limit int) ([]uuid.UUID, error) {
	rows, err := r.db.Query(ctx, `
		INSERT INTO partner_mailboxes (partner_id, mode, org_external_id, organization_id, email_account_id, source, email, provider, enablement, connect_state)
		SELECT po.partner_id, po.mode, po.external_id, po.organization_id, ea.id, 'manual', lower(ea.email),
			CASE ea.provider::text WHEN 'gmail' THEN 'google' WHEN 'outlook' THEN 'microsoft' ELSE 'smtp' END, 'off', 'connected'
		FROM email_accounts ea
		JOIN partner_organizations po ON po.organization_id = ea.organization_id
		WHERE ea.created_at < localtimestamp - interval '15 seconds'
		  AND NOT EXISTS (SELECT 1 FROM partner_mailboxes pm WHERE pm.email_account_id = ea.id)
		  AND NOT EXISTS (SELECT 1 FROM partner_mailboxes pm WHERE pm.organization_id = ea.organization_id
			AND pm.email = lower(ea.email) AND pm.connect_state IN ('pending', 'connecting') AND pm.detached_at IS NULL)
		ORDER BY ea.created_at
		LIMIT $1
		ON CONFLICT DO NOTHING
		RETURNING id`, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

func (r *partnerMailboxRepository) MarkRemovedPartnerMailboxes(ctx context.Context, limit int) ([]uuid.UUID, error) {
	rows, err := r.db.Query(ctx, `
		WITH gone AS (
			UPDATE partner_mailboxes SET removed_at = now(), enablement = 'off', updated_at = now()
			WHERE id IN (SELECT id FROM partner_mailboxes WHERE email_account_id IS NULL AND removed_at IS NULL
				AND detached_at IS NULL AND connect_state = 'connected' LIMIT $1 FOR UPDATE SKIP LOCKED)
			RETURNING id
		), closed AS (
			UPDATE partner_mailbox_enablements h SET ended_at = GREATEST(now(), h.started_at)
			FROM gone WHERE h.mailbox_id = gone.id AND h.ended_at IS NULL
		)
		SELECT id FROM gone`, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

func (r *partnerMailboxRepository) ChangedPartnerMailboxes(ctx context.Context, limit int) ([]uuid.UUID, error) {
	rows, err := r.db.Query(ctx, `SELECT pm.id`+partnerMailboxJoins+`
		WHERE pm.removed_at IS NULL AND `+partnerPublishedStateSQL+` IS DISTINCT FROM pm.published_state
		ORDER BY pm.updated_at LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// PartnerPublishedState is the fingerprint ChangedPartnerMailboxes compares against, built the same way.
func PartnerPublishedState(status string, e models.PartnerEnablement, warmupState string, action *string) string {
	out := status + "|" + string(e) + "|" + warmupState
	if action != nil {
		out += "|" + *action
	}
	return out
}

func (r *partnerMailboxRepository) SetPartnerMailboxPublished(ctx context.Context, id uuid.UUID, state string) error {
	_, err := r.db.Exec(ctx, `UPDATE partner_mailboxes SET published_state = $2 WHERE id = $1`, id, state)
	return err
}

func (r *partnerMailboxRepository) ClaimPartnerConnects(ctx context.Context, limit int, lease time.Duration) ([]models.PartnerMailbox, error) {
	rows, err := r.db.Query(ctx, `
		UPDATE partner_mailboxes pm SET connect_state = 'connecting', connect_attempts = pm.connect_attempts + 1,
			next_connect_at = now() + make_interval(secs => $2), updated_at = now()
		WHERE pm.id IN (SELECT id FROM partner_mailboxes WHERE connect_state IN ('pending', 'connecting') AND detached_at IS NULL
			AND next_connect_at <= now() ORDER BY next_connect_at LIMIT $1 FOR UPDATE SKIP LOCKED)
		RETURNING `+partnerMailboxColumns, limit, lease.Seconds())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.PartnerMailbox{}
	for rows.Next() {
		m, err := scanPartnerMailbox(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

func (r *partnerMailboxRepository) FinishPartnerConnect(ctx context.Context, id uuid.UUID, accountID *uuid.UUID, state string, connectErr *string, next *time.Time) error {
	_, err := r.db.Exec(ctx, `UPDATE partner_mailboxes SET email_account_id = COALESCE($2, email_account_id), connect_state = $3,
		connect_error = $4, next_connect_at = $5, action_required = NULL, updated_at = now() WHERE id = $1 AND detached_at IS NULL`, id, accountID, state, connectErr, next)
	if isUniqueViolation(err) {
		return ErrPartnerMailboxTaken
	}
	return err
}

func (r *partnerMailboxRepository) AwaitPartnerAction(ctx context.Context, id uuid.UUID, action, msg string, next time.Time) error {
	_, err := r.db.Exec(ctx, `UPDATE partner_mailboxes SET connect_state = 'pending', connect_error = $3, action_required = $2,
		connect_attempts = 0, next_connect_at = $4, updated_at = now()
		WHERE id = $1 AND detached_at IS NULL AND email_account_id IS NULL`, id, action, msg, next)
	return err
}

func (r *partnerMailboxRepository) WakePartnerAction(ctx context.Context, orgID uuid.UUID, action string, domains []string) error {
	_, err := r.db.Exec(ctx, `UPDATE partner_mailboxes SET next_connect_at = now(), updated_at = now()
		WHERE organization_id = $1 AND action_required = $2 AND connect_state = 'pending' AND detached_at IS NULL
		  AND email_account_id IS NULL AND split_part(email, '@', 2) = ANY($3)`, orgID, action, domains)
	return err
}

func (r *partnerMailboxRepository) PartnerMailboxOnDomain(ctx context.Context, partnerID uuid.UUID, mode models.PartnerMode, orgExternalID, provider, domain string) (bool, error) {
	var ok bool
	err := r.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM partner_mailboxes WHERE partner_id = $1 AND mode = $2 AND org_external_id = $3
		AND provider = $4 AND split_part(email, '@', 2) = $5 AND detached_at IS NULL AND removed_at IS NULL)`,
		partnerID, mode, orgExternalID, provider, domain).Scan(&ok)
	return ok, err
}

func (r *partnerMailboxRepository) PartnerTokenMailbox(ctx context.Context, orgID uuid.UUID, email string) (*models.PartnerMailbox, error) {
	return scanPartnerMailbox(r.db.QueryRow(ctx, `SELECT `+partnerMailboxColumns+` FROM partner_mailboxes pm
		WHERE pm.organization_id = $1 AND pm.email = lower($2) AND pm.connection_method = 'token'
		  AND pm.partner_mailbox_id IS NOT NULL AND pm.detached_at IS NULL AND pm.removed_at IS NULL
		ORDER BY pm.created_at DESC LIMIT 1`, orgID, email))
}

func (r *partnerMailboxRepository) PartnerMailboxLock(ctx context.Context, orgID, emailAccountID uuid.UUID) (string, error) {
	var name string
	err := r.db.QueryRow(ctx, `SELECT p.display_name FROM partner_mailboxes pm JOIN partners p ON p.id = pm.partner_id
		JOIN email_accounts ea ON ea.id = pm.email_account_id AND ea.organization_id = $1
		WHERE pm.organization_id = $1 AND pm.email_account_id = $2 AND pm.source <> 'manual' AND pm.detached_at IS NULL AND pm.removed_at IS NULL`,
		orgID, emailAccountID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return name, err
}

// PartnerMailboxDays counts each mailbox's UTC days in [start, end) with an enablement in force at any moment,
// each day once under the highest it had. Open spans run to now.
func (r *partnerMailboxRepository) PartnerMailboxDays(ctx context.Context, partnerID uuid.UUID, mode models.PartnerMode, start, end, now time.Time) ([]models.PartnerMailboxDays, error) {
	rows, err := r.db.Query(ctx, `
		WITH spans AS (
			SELECT h.mailbox_id, h.enablement, GREATEST(h.started_at, $3) AS f, LEAST(COALESCE(h.ended_at, $5), $4) AS t
			FROM partner_mailbox_enablements h JOIN partner_mailboxes pm ON pm.id = h.mailbox_id
			WHERE pm.partner_id = $1 AND pm.mode = $2 AND h.started_at < $4 AND h.started_at <= $5
			  AND (h.ended_at IS NULL OR h.ended_at > $3 OR (h.ended_at = h.started_at AND h.started_at >= $3))
		), days AS (
			SELECT s.mailbox_id, d AS day, bool_or(s.enablement = 'sending_warmup') AS sw
			FROM spans s, generate_series(date_trunc('day', s.f AT TIME ZONE 'UTC'),
				date_trunc('day', (CASE WHEN s.t > s.f THEN s.t - interval '1 microsecond' ELSE s.f END) AT TIME ZONE 'UTC'), interval '1 day') d
			WHERE s.f <= s.t
			GROUP BY 1, 2
		)
		SELECT pm.id, pm.org_external_id, pm.partner_mailbox_id, pm.email,
			count(*) FILTER (WHERE d.sw)::int, count(*) FILTER (WHERE NOT d.sw)::int
		FROM days d JOIN partner_mailboxes pm ON pm.id = d.mailbox_id
		GROUP BY pm.id ORDER BY pm.org_external_id, pm.id`, partnerID, mode, start, end, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.PartnerMailboxDays{}
	for rows.Next() {
		var d models.PartnerMailboxDays
		if err := rows.Scan(&d.MailboxID, &d.OrgExternalID, &d.PartnerMailboxID, &d.Email, &d.SendingWarmupDays, &d.WarmupOnlyDays); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *partnerMailboxRepository) InsertPartnerConnectLink(ctx context.Context, l *PartnerConnectLink) error {
	_, err := r.db.Exec(ctx, `INSERT INTO partner_connect_links (link_id, partner_id, mode, organization_id, org_external_id, partner_mailbox_id,
		provider, enablement, login_hint, return_url) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		l.LinkID, l.PartnerID, l.Mode, l.OrganizationID, l.OrgExternalID, l.PartnerMailboxID, l.Provider, l.Enablement, l.LoginHint, l.ReturnURL)
	return err
}

func (r *partnerMailboxRepository) GetPartnerConnectLink(ctx context.Context, linkID uuid.UUID) (*PartnerConnectLink, error) {
	var l PartnerConnectLink
	err := r.db.QueryRow(ctx, `SELECT link_id, partner_id, mode, organization_id, org_external_id, partner_mailbox_id, provider, enablement,
		login_hint, return_url FROM partner_connect_links WHERE link_id = $1`, linkID).
		Scan(&l.LinkID, &l.PartnerID, &l.Mode, &l.OrganizationID, &l.OrgExternalID, &l.PartnerMailboxID, &l.Provider, &l.Enablement, &l.LoginHint, &l.ReturnURL)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

const partnerGroupColumns = `g.id, g.partner_id, g.mode, g.external_id, g.name, g.product_name, g.logo_url, g.accent_color, g.website_url,
	g.hide_powered_by, g.portal_domain, g.portal_status, g.portal_checked_at, g.portal_verified_at`

func scanPartnerGroup(row pgx.Row) (*models.PartnerGroup, error) {
	var g models.PartnerGroup
	err := row.Scan(&g.ID, &g.PartnerID, &g.Mode, &g.ExternalID, &g.Name, &g.ProductName, &g.LogoURL, &g.AccentColor, &g.WebsiteURL,
		&g.HidePoweredBy, &g.PortalDomain, &g.PortalStatus, &g.PortalCheckedAt, &g.PortalVerifiedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// UpsertPartnerGroup stores the group's brand as last sent; a changed portal domain starts unverified.
func (r *partnerMailboxRepository) UpsertPartnerGroup(ctx context.Context, g *models.PartnerGroup) (*models.PartnerGroup, error) {
	status := "none"
	if g.PortalDomain != "" {
		status = "pending"
	}
	return scanPartnerGroup(r.db.QueryRow(ctx, `
		INSERT INTO partner_groups AS g (partner_id, mode, external_id, name, product_name, logo_url, accent_color, website_url,
			hide_powered_by, portal_domain, portal_status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (partner_id, mode, external_id) DO UPDATE SET name = EXCLUDED.name, product_name = EXCLUDED.product_name,
			logo_url = EXCLUDED.logo_url, accent_color = EXCLUDED.accent_color, website_url = EXCLUDED.website_url,
			hide_powered_by = EXCLUDED.hide_powered_by, portal_domain = EXCLUDED.portal_domain,
			portal_status = CASE WHEN g.portal_domain = EXCLUDED.portal_domain THEN g.portal_status ELSE EXCLUDED.portal_status END,
			portal_checked_at = CASE WHEN g.portal_domain = EXCLUDED.portal_domain THEN g.portal_checked_at END,
			portal_verified_at = CASE WHEN g.portal_domain = EXCLUDED.portal_domain THEN g.portal_verified_at END,
			updated_at = now()
		RETURNING `+partnerGroupColumns,
		g.PartnerID, g.Mode, g.ExternalID, g.Name, g.ProductName, g.LogoURL, g.AccentColor, g.WebsiteURL, g.HidePoweredBy, g.PortalDomain, status))
}

func (r *partnerMailboxRepository) GetPartnerGroup(ctx context.Context, partnerID uuid.UUID, mode models.PartnerMode, externalID string) (*models.PartnerGroup, error) {
	return scanPartnerGroup(r.db.QueryRow(ctx, `SELECT `+partnerGroupColumns+` FROM partner_groups g
		WHERE g.partner_id = $1 AND g.mode = $2 AND g.external_id = $3`, partnerID, mode, externalID))
}

func (r *partnerMailboxRepository) PartnerGroupForOrganization(ctx context.Context, orgID uuid.UUID) (*models.PartnerGroup, error) {
	return scanPartnerGroup(r.db.QueryRow(ctx, `SELECT `+partnerGroupColumns+` FROM partner_organizations po
		JOIN partner_groups g ON g.partner_id = po.partner_id AND g.mode = po.mode AND g.external_id = po.group_external_id
		WHERE po.organization_id = $1`, orgID))
}

func (r *partnerMailboxRepository) DuePartnerPortals(ctx context.Context, before time.Time, limit int) ([]models.PartnerGroup, error) {
	rows, err := r.db.Query(ctx, `SELECT `+partnerGroupColumns+` FROM partner_groups g
		WHERE g.portal_domain <> '' AND g.portal_status <> 'verified' AND (g.portal_checked_at IS NULL OR g.portal_checked_at < $1)
		ORDER BY g.portal_checked_at NULLS FIRST LIMIT $2`, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.PartnerGroup{}
	for rows.Next() {
		g, err := scanPartnerGroup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *g)
	}
	return out, rows.Err()
}

func (r *partnerMailboxRepository) SetPartnerPortalStatus(ctx context.Context, id uuid.UUID, status string, verifiedAt *time.Time) error {
	_, err := r.db.Exec(ctx, `UPDATE partner_groups SET portal_status = $2, portal_checked_at = now(),
		portal_verified_at = COALESCE($3, portal_verified_at) WHERE id = $1`, id, status, verifiedAt)
	return err
}

func (r *partnerMailboxRepository) GetPartnerStatement(ctx context.Context, partnerID uuid.UUID, mode models.PartnerMode, month time.Time) (*models.PartnerStatement, error) {
	var st models.PartnerStatement
	var m time.Time
	err := r.db.QueryRow(ctx, `SELECT id, partner_id, mode, month, sending_warmup_days, warmup_only_days, sending_warmup_day_micros,
		warmup_only_day_micros, amount_micros, closed_at FROM partner_statements WHERE partner_id = $1 AND mode = $2 AND month = $3`,
		partnerID, mode, month).Scan(&st.ID, &st.PartnerID, &st.Mode, &m, &st.SendingWarmupDays, &st.WarmupOnlyDays,
		&st.SendingWarmupDayMicros, &st.WarmupOnlyDayMicros, &st.AmountMicros, &st.ClosedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	st.Month = m.Format("2006-01")
	rows, err := r.db.Query(ctx, `SELECT org_external_id, sending_warmup_days, warmup_only_days, amount_micros
		FROM partner_statement_lines WHERE statement_id = $1 ORDER BY org_external_id`, st.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	st.Lines = []models.PartnerStatementLine{}
	for rows.Next() {
		var l models.PartnerStatementLine
		if err := rows.Scan(&l.OrgExternalID, &l.SendingWarmupDays, &l.WarmupOnlyDays, &l.AmountMicros); err != nil {
			return nil, err
		}
		st.Lines = append(st.Lines, l)
	}
	return &st, rows.Err()
}

func (r *partnerMailboxRepository) ListPartnerStatements(ctx context.Context, partnerID uuid.UUID, mode models.PartnerMode, limit int) ([]models.PartnerStatement, error) {
	rows, err := r.db.Query(ctx, `SELECT id, partner_id, mode, month, sending_warmup_days, warmup_only_days, sending_warmup_day_micros,
		warmup_only_day_micros, amount_micros, closed_at FROM partner_statements WHERE partner_id = $1 AND mode = $2
		ORDER BY month DESC LIMIT $3`, partnerID, mode, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.PartnerStatement{}
	for rows.Next() {
		var st models.PartnerStatement
		var m time.Time
		if err := rows.Scan(&st.ID, &st.PartnerID, &st.Mode, &m, &st.SendingWarmupDays, &st.WarmupOnlyDays,
			&st.SendingWarmupDayMicros, &st.WarmupOnlyDayMicros, &st.AmountMicros, &st.ClosedAt); err != nil {
			return nil, err
		}
		st.Month = m.Format("2006-01")
		out = append(out, st)
	}
	return out, rows.Err()
}

func (r *partnerMailboxRepository) InsertPartnerStatement(ctx context.Context, st *models.PartnerStatement) error {
	month, err := time.Parse("2006-01", st.Month)
	if err != nil {
		return err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var id uuid.UUID
	err = tx.QueryRow(ctx, `INSERT INTO partner_statements (partner_id, mode, month, sending_warmup_days, warmup_only_days,
		sending_warmup_day_micros, warmup_only_day_micros, amount_micros)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) ON CONFLICT (partner_id, mode, month) DO NOTHING RETURNING id`,
		st.PartnerID, st.Mode, month, st.SendingWarmupDays, st.WarmupOnlyDays, st.SendingWarmupDayMicros, st.WarmupOnlyDayMicros, st.AmountMicros).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, l := range st.Lines {
		if _, err := tx.Exec(ctx, `INSERT INTO partner_statement_lines (statement_id, org_external_id, sending_warmup_days, warmup_only_days, amount_micros)
			VALUES ($1, $2, $3, $4, $5)`, id, l.OrgExternalID, l.SendingWarmupDays, l.WarmupOnlyDays, l.AmountMicros); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *partnerMailboxRepository) FirstPartnerEnablement(ctx context.Context, partnerID uuid.UUID, mode models.PartnerMode) (*time.Time, error) {
	var at *time.Time
	err := r.db.QueryRow(ctx, `SELECT min(h.started_at) FROM partner_mailbox_enablements h JOIN partner_mailboxes pm ON pm.id = h.mailbox_id
		WHERE pm.partner_id = $1 AND pm.mode = $2`, partnerID, mode).Scan(&at)
	return at, err
}
