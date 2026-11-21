package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// LinkedInContactBackfillKey is the admin_settings row holding the contact LinkedIn backfill's place.
const LinkedInContactBackfillKey = "linkedin.contact_backfill"

// LinkedInRepository holds the LinkedIn add-on. Every read and write a workspace
// drives is scoped by organization_id; only the billing and reconcile sweeps
// read across workspaces, and each row they return carries its own.
type LinkedInRepository interface {
	GetWorkspace(ctx context.Context, orgID uuid.UUID) (*models.LinkedInWorkspace, error)
	// SetWorkspace records the mode GraphKit reported; a link session id is kept unless replaced.
	SetWorkspace(ctx context.Context, orgID uuid.UUID, mode string, name *string, linkedAt *time.Time) error
	SetLinkSession(ctx context.Context, orgID uuid.UUID, sessionID *string) error

	// UpsertAccount records GraphKit's view of an account, by its GraphKit id, within the workspace.
	UpsertAccount(ctx context.Context, a *models.LinkedInAccount) (*models.LinkedInAccount, error)
	GetAccount(ctx context.Context, orgID, id uuid.UUID) (*models.LinkedInAccount, error)
	GetAccountByGraphKitID(ctx context.Context, orgID uuid.UUID, graphkitID string) (*models.LinkedInAccount, error)
	// ListAccounts is the workspace's accounts newest first; removed ones only when withRemoved.
	ListAccounts(ctx context.Context, orgID uuid.UUID, after *models.StorePage, limit int, withRemoved bool) ([]models.LinkedInAccount, error)
	CountAccounts(ctx context.Context, orgID uuid.UUID) (int, error)
	MarkRemoved(ctx context.Context, orgID, id uuid.UUID, at time.Time) error
	// ForgetLinkedAccounts marks removed every linked-mode account of a workspace whose link ended.
	ForgetLinkedAccounts(ctx context.Context, orgID uuid.UUID, at time.Time) (int64, error)
	// SetSeatBilling starts a store-billed seat's month: paid through until, collected by key for micros.
	SetSeatBilling(ctx context.Context, orgID, id uuid.UUID, mode string, monthly models.Micro, paidThrough *time.Time, paidKey *string, paidMicros models.Micro) error

	FirstSeatAt(ctx context.Context, orgID uuid.UUID) (*time.Time, error)
	OrgsWithSeats(ctx context.Context, limit int) ([]uuid.UUID, error)
	PendingSeatKeys(ctx context.Context, orgID uuid.UUID, reservedBefore time.Time) ([]string, error)
	// ReserveSeats reserves under key, at price, the seats due before dueBefore and the unpaid ones last tried before retryBefore.
	ReserveSeats(ctx context.Context, orgID uuid.UUID, key string, price models.Micro, dueBefore, retryBefore time.Time) (int, error)
	SeatsByChargeKey(ctx context.Context, orgID uuid.UUID, key string) ([]models.LinkedInAccount, error)
	// SettleSeats moves the live seats under key a month on; it returns them.
	SettleSeats(ctx context.Context, orgID uuid.UUID, key string) ([]models.LinkedInAccount, error)
	DetachSeat(ctx context.Context, orgID uuid.UUID, key string, id uuid.UUID) error
	// ReleaseSeats ends the reservation under key as unpaid; it returns the seats it released.
	ReleaseSeats(ctx context.Context, orgID uuid.UUID, key string) ([]models.LinkedInAccount, error)
	UnpaidSeats(ctx context.Context, orgID uuid.UUID) ([]models.LinkedInAccount, error)
	SetUnpaidPaused(ctx context.Context, orgID, id uuid.UUID, paused bool) error
	ExtendUnmeteredSeats(ctx context.Context, orgID uuid.UUID, dueBefore time.Time) error
	SeatsDue(ctx context.Context, orgID uuid.UUID, dueBefore time.Time) ([]models.LinkedInAccount, error)
	SetSeatNotice(ctx context.Context, id uuid.UUID, notice string) (bool, error)

	CreateSession(ctx context.Context, s *models.LinkedInConnectSession) error
	GetSession(ctx context.Context, orgID uuid.UUID, id string) (*models.LinkedInConnectSession, error)
	// UpdateSession records a session's status and the account it connected.
	UpdateSession(ctx context.Context, orgID uuid.UUID, id, status string, accountGraphKitID *string, accountID *uuid.UUID) error
	// MarkSessionRefunded records the refund once; false when it already was.
	MarkSessionRefunded(ctx context.Context, orgID uuid.UUID, id string) (bool, error)
	// OpenSessions are sessions still in flight or whose charge is unsettled, untouched since before, across workspaces.
	OpenSessions(ctx context.Context, before time.Time, limit int) ([]models.LinkedInConnectSession, error)

	UpsertConversation(ctx context.Context, c *models.LinkedInConversation) (*models.LinkedInConversation, error)
	GetConversation(ctx context.Context, orgID, id uuid.UUID) (*models.LinkedInConversation, error)
	ListConversations(ctx context.Context, orgID uuid.UUID, f LinkedInConversationFilter) ([]models.LinkedInConversation, error)
	MarkConversationRead(ctx context.Context, orgID, id uuid.UUID) error
	// UpsertMessage records a message once by its GraphKit id; later calls move its status. inserted is true the first time.
	UpsertMessage(ctx context.Context, m *models.LinkedInMessage) (inserted bool, err error)
	ListMessages(ctx context.Context, orgID, conversationID uuid.UUID, after *models.StorePage, limit int) ([]models.LinkedInMessage, error)
	LatestMessageAt(ctx context.Context, orgID uuid.UUID, accountID uuid.UUID) (*time.Time, error)

	ListSavedSearches(ctx context.Context, orgID uuid.UUID, after *models.StorePage, limit int) ([]models.LinkedInSavedSearch, error)
	GetSavedSearch(ctx context.Context, orgID, id uuid.UUID) (*models.LinkedInSavedSearch, error)
	CreateSavedSearch(ctx context.Context, s *models.LinkedInSavedSearch) error
	UpdateSavedSearch(ctx context.Context, orgID, id uuid.UUID, name *string, query json.RawMessage) (*models.LinkedInSavedSearch, error)
	DeleteSavedSearch(ctx context.Context, orgID, id uuid.UUID) (bool, error)

	// ContactMatches are the workspace's contacts for these LinkedIn public ids, with their campaigns and email outreach.
	ContactMatches(ctx context.Context, orgID uuid.UUID, publicIDs []string) (map[string]models.LinkedInContactMatch, error)
	// CompanyContacts counts the workspace's contacts per company name, lowercased.
	CompanyContacts(ctx context.Context, orgID uuid.UUID, companies []string) (map[string]int, error)
	// ConversationStates is, per participant public id, whether a conversation exists and whether they replied.
	ConversationStates(ctx context.Context, orgID uuid.UUID, publicIDs []string) (map[string]bool, error)
	// ContactEmails are the email of each of the workspace's contacts by id.
	ContactEmails(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]string, error)

	// BackfillContactLinkedIn derives linkedin_url for up to limit contacts after the id that predate the column.
	BackfillContactLinkedIn(ctx context.Context, after uuid.UUID, limit int) (last uuid.UUID, n int, err error)
	BackfillProgress(ctx context.Context) (after uuid.UUID, done bool, err error)
	SaveBackfillProgress(ctx context.Context, after uuid.UUID, done bool) error
}

// LinkedInConversationFilter narrows a conversation list.
type LinkedInConversationFilter struct {
	AccountID  *uuid.UUID
	ContactID  *uuid.UUID
	UnreadOnly bool
	// After is the last row of the previous page, by last activity.
	AfterAt *time.Time
	AfterID *uuid.UUID
	Limit   int
}

type linkedInRepository struct{ db *db.DB }

// NewLinkedInRepository builds the Postgres LinkedIn repository.
func NewLinkedInRepository(d *db.DB) LinkedInRepository { return &linkedInRepository{db: d} }

func (r *linkedInRepository) exec(ctx context.Context, query string, args ...any) error {
	if _, err := r.db.Exec(ctx, query, args...); err != nil {
		db.CaptureError(err, query, nil, "exec")
		return err
	}
	return nil
}

func (r *linkedInRepository) affected(ctx context.Context, query string, args ...any) (int64, error) {
	tag, err := r.db.Exec(ctx, query, args...)
	if err != nil {
		db.CaptureError(err, query, nil, "exec")
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ---- workspace ------------------------------------------------------------

func (r *linkedInRepository) GetWorkspace(ctx context.Context, orgID uuid.UUID) (*models.LinkedInWorkspace, error) {
	var w models.LinkedInWorkspace
	err := r.db.QueryRow(ctx, `SELECT organization_id, mode, workspace_name, linked_at, link_session_id, updated_at
		FROM linkedin_workspaces WHERE organization_id = $1`, orgID).
		Scan(&w.OrganizationID, &w.Mode, &w.WorkspaceName, &w.LinkedAt, &w.LinkSessionID, &w.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}

func (r *linkedInRepository) SetWorkspace(ctx context.Context, orgID uuid.UUID, mode string, name *string, linkedAt *time.Time) error {
	return r.exec(ctx, `INSERT INTO linkedin_workspaces (organization_id, mode, workspace_name, linked_at, updated_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (organization_id) DO UPDATE SET mode = EXCLUDED.mode, workspace_name = EXCLUDED.workspace_name,
			linked_at = EXCLUDED.linked_at, updated_at = now()`, orgID, mode, name, linkedAt)
}

func (r *linkedInRepository) SetLinkSession(ctx context.Context, orgID uuid.UUID, sessionID *string) error {
	return r.exec(ctx, `INSERT INTO linkedin_workspaces (organization_id, link_session_id, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (organization_id) DO UPDATE SET link_session_id = EXCLUDED.link_session_id, updated_at = now()`, orgID, sessionID)
}

// ---- accounts -------------------------------------------------------------

const linkedInAccountColumns = `id, organization_id, user_id, graphkit_id, status, status_reason, display_name, headline,
	avatar_url, profile_url, public_id, licences, snapshot, billing_mode, monthly_retail_micros, paid_through,
	billing_failures, billing_attempted_at, pending_charge_key, pending_charge_micros, paid_key, paid_micros,
	unpaid_since, unpaid_paused, last_notice, connected_at, removed_at, created_at, updated_at`

// linkedInSeat is a seat the workspace pays for here.
const linkedInSeat = `billing_mode = 'store' AND removed_at IS NULL AND paid_through IS NOT NULL`

func scanLinkedInAccount(row pgx.Row) (*models.LinkedInAccount, error) {
	var a models.LinkedInAccount
	err := row.Scan(&a.ID, &a.OrganizationID, &a.UserID, &a.GraphKitID, &a.Status, &a.StatusReason, &a.DisplayName, &a.Headline,
		&a.AvatarURL, &a.ProfileURL, &a.PublicID, &a.Licences, &a.Snapshot, &a.BillingMode, &a.MonthlyRetailMicros, &a.PaidThrough,
		&a.BillingFailures, &a.BillingAttemptedAt, &a.PendingChargeKey, &a.PendingChargeMicros, &a.PaidKey, &a.PaidMicros,
		&a.UnpaidSince, &a.UnpaidPaused, &a.LastNotice, &a.ConnectedAt, &a.RemovedAt, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *linkedInRepository) accounts(ctx context.Context, query string, args ...any) ([]models.LinkedInAccount, error) {
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	out := []models.LinkedInAccount{}
	for rows.Next() {
		a, err := scanLinkedInAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (r *linkedInRepository) UpsertAccount(ctx context.Context, a *models.LinkedInAccount) (*models.LinkedInAccount, error) {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	if a.Licences == nil {
		a.Licences = []string{}
	}
	if len(a.Snapshot) == 0 {
		a.Snapshot = json.RawMessage(`{}`)
	}
	// Billing columns are the add-on's own and only set on insert; the conflict target never crosses workspaces.
	return scanLinkedInAccount(r.db.QueryRow(ctx, `INSERT INTO linkedin_accounts (id, organization_id, user_id, graphkit_id, status,
			status_reason, display_name, headline, avatar_url, profile_url, public_id, licences, snapshot, billing_mode,
			monthly_retail_micros, connected_at, removed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		ON CONFLICT (graphkit_id) DO UPDATE SET status = EXCLUDED.status, status_reason = EXCLUDED.status_reason,
			display_name = EXCLUDED.display_name, headline = EXCLUDED.headline, avatar_url = EXCLUDED.avatar_url,
			profile_url = EXCLUDED.profile_url, public_id = EXCLUDED.public_id, licences = EXCLUDED.licences,
			snapshot = EXCLUDED.snapshot, connected_at = COALESCE(linkedin_accounts.connected_at, EXCLUDED.connected_at),
			removed_at = CASE WHEN EXCLUDED.status = 'removed' THEN COALESCE(linkedin_accounts.removed_at, now())
				WHEN linkedin_accounts.removed_at IS NOT NULL AND EXCLUDED.status <> 'removed' THEN NULL
				ELSE linkedin_accounts.removed_at END,
			user_id = COALESCE(linkedin_accounts.user_id, EXCLUDED.user_id), updated_at = now()
		WHERE linkedin_accounts.organization_id = EXCLUDED.organization_id
		RETURNING `+linkedInAccountColumns,
		a.ID, a.OrganizationID, a.UserID, a.GraphKitID, a.Status, a.StatusReason, a.DisplayName, a.Headline, a.AvatarURL,
		a.ProfileURL, a.PublicID, a.Licences, a.Snapshot, a.BillingMode, a.MonthlyRetailMicros, a.ConnectedAt, a.RemovedAt))
}

func (r *linkedInRepository) GetAccount(ctx context.Context, orgID, id uuid.UUID) (*models.LinkedInAccount, error) {
	return scanLinkedInAccount(r.db.QueryRow(ctx, `SELECT `+linkedInAccountColumns+` FROM linkedin_accounts
		WHERE organization_id = $1 AND id = $2`, orgID, id))
}

func (r *linkedInRepository) GetAccountByGraphKitID(ctx context.Context, orgID uuid.UUID, graphkitID string) (*models.LinkedInAccount, error) {
	return scanLinkedInAccount(r.db.QueryRow(ctx, `SELECT `+linkedInAccountColumns+` FROM linkedin_accounts
		WHERE organization_id = $1 AND graphkit_id = $2`, orgID, graphkitID))
}

func (r *linkedInRepository) ListAccounts(ctx context.Context, orgID uuid.UUID, after *models.StorePage, limit int, withRemoved bool) ([]models.LinkedInAccount, error) {
	at, id := keyset(after)
	return r.accounts(ctx, `SELECT `+linkedInAccountColumns+` FROM linkedin_accounts
		WHERE organization_id = $1 AND ($2::timestamptz IS NULL OR (created_at, id) < ($2, $3::uuid)) AND ($5 OR removed_at IS NULL)
		ORDER BY created_at DESC, id DESC LIMIT $4`, orgID, at, id, limit, withRemoved)
}

func (r *linkedInRepository) CountAccounts(ctx context.Context, orgID uuid.UUID) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `SELECT count(*) FROM linkedin_accounts WHERE organization_id = $1 AND removed_at IS NULL`, orgID).Scan(&n)
	return n, err
}

func (r *linkedInRepository) MarkRemoved(ctx context.Context, orgID, id uuid.UUID, at time.Time) error {
	return r.exec(ctx, `UPDATE linkedin_accounts SET status = 'removed', removed_at = COALESCE(removed_at, $3),
		pending_charge_key = NULL, updated_at = now() WHERE organization_id = $1 AND id = $2`, orgID, id, at)
}

func (r *linkedInRepository) ForgetLinkedAccounts(ctx context.Context, orgID uuid.UUID, at time.Time) (int64, error) {
	return r.affected(ctx, `UPDATE linkedin_accounts SET status = 'removed', removed_at = COALESCE(removed_at, $2), updated_at = now()
		WHERE organization_id = $1 AND billing_mode = 'linked' AND removed_at IS NULL`, orgID, at)
}

func (r *linkedInRepository) SetSeatBilling(ctx context.Context, orgID, id uuid.UUID, mode string, monthly models.Micro, paidThrough *time.Time, paidKey *string, paidMicros models.Micro) error {
	return r.exec(ctx, `UPDATE linkedin_accounts SET billing_mode = $3, monthly_retail_micros = $4, paid_through = $5,
		paid_key = $6, paid_micros = $7, billing_failures = 0, unpaid_since = NULL, updated_at = now()
		WHERE organization_id = $1 AND id = $2`, orgID, id, mode, monthly, paidThrough, paidKey, paidMicros)
}

// ---- seat billing ---------------------------------------------------------

func (r *linkedInRepository) FirstSeatAt(ctx context.Context, orgID uuid.UUID) (*time.Time, error) {
	var t *time.Time
	err := r.db.QueryRow(ctx, `SELECT min(COALESCE(connected_at, created_at)) FROM linkedin_accounts
		WHERE organization_id = $1 AND `+linkedInSeat, orgID).Scan(&t)
	return t, err
}

func (r *linkedInRepository) OrgsWithSeats(ctx context.Context, limit int) ([]uuid.UUID, error) {
	rows, err := r.db.Query(ctx, `SELECT DISTINCT organization_id FROM linkedin_accounts WHERE `+linkedInSeat+` LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (r *linkedInRepository) PendingSeatKeys(ctx context.Context, orgID uuid.UUID, reservedBefore time.Time) ([]string, error) {
	rows, err := r.db.Query(ctx, `SELECT DISTINCT pending_charge_key FROM linkedin_accounts WHERE organization_id = $1
		AND pending_charge_key IS NOT NULL AND (billing_attempted_at IS NULL OR billing_attempted_at < $2)`, orgID, reservedBefore)
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

func (r *linkedInRepository) ReserveSeats(ctx context.Context, orgID uuid.UUID, key string, price models.Micro, dueBefore, retryBefore time.Time) (int, error) {
	n, err := r.affected(ctx, `UPDATE linkedin_accounts SET pending_charge_key = $2, pending_charge_micros = $3,
		monthly_retail_micros = $3, billing_attempted_at = now(), updated_at = now()
		WHERE organization_id = $1 AND `+linkedInSeat+` AND pending_charge_key IS NULL
		AND ((billing_failures = 0 AND paid_through < $4)
			OR (billing_failures > 0 AND (billing_attempted_at IS NULL OR billing_attempted_at < $5)))`,
		orgID, key, price, dueBefore, retryBefore)
	return int(n), err
}

func (r *linkedInRepository) SeatsByChargeKey(ctx context.Context, orgID uuid.UUID, key string) ([]models.LinkedInAccount, error) {
	return r.accounts(ctx, `SELECT `+linkedInAccountColumns+` FROM linkedin_accounts
		WHERE organization_id = $1 AND pending_charge_key = $2 ORDER BY id`, orgID, key)
}

func (r *linkedInRepository) SettleSeats(ctx context.Context, orgID uuid.UUID, key string) ([]models.LinkedInAccount, error) {
	return r.accounts(ctx, `UPDATE linkedin_accounts SET paid_through = paid_through + interval '1 month', pending_charge_key = NULL,
		paid_key = $2, paid_micros = pending_charge_micros, billing_failures = 0, unpaid_since = NULL, last_notice = '', updated_at = now()
		WHERE organization_id = $1 AND pending_charge_key = $2 AND removed_at IS NULL
		RETURNING `+linkedInAccountColumns, orgID, key)
}

func (r *linkedInRepository) DetachSeat(ctx context.Context, orgID uuid.UUID, key string, id uuid.UUID) error {
	return r.exec(ctx, `UPDATE linkedin_accounts SET pending_charge_key = NULL, updated_at = now()
		WHERE organization_id = $1 AND id = $2 AND pending_charge_key = $3`, orgID, id, key)
}

func (r *linkedInRepository) ReleaseSeats(ctx context.Context, orgID uuid.UUID, key string) ([]models.LinkedInAccount, error) {
	return r.accounts(ctx, `UPDATE linkedin_accounts SET pending_charge_key = NULL, billing_failures = billing_failures + 1,
		unpaid_since = COALESCE(unpaid_since, now()), updated_at = now()
		WHERE organization_id = $1 AND pending_charge_key = $2 RETURNING `+linkedInAccountColumns, orgID, key)
}

func (r *linkedInRepository) UnpaidSeats(ctx context.Context, orgID uuid.UUID) ([]models.LinkedInAccount, error) {
	return r.accounts(ctx, `SELECT `+linkedInAccountColumns+` FROM linkedin_accounts
		WHERE organization_id = $1 AND `+linkedInSeat+` AND billing_failures > 0 AND pending_charge_key IS NULL
		ORDER BY paid_through, id`, orgID)
}

func (r *linkedInRepository) SetUnpaidPaused(ctx context.Context, orgID, id uuid.UUID, paused bool) error {
	return r.exec(ctx, `UPDATE linkedin_accounts SET unpaid_paused = $3, updated_at = now()
		WHERE organization_id = $1 AND id = $2`, orgID, id, paused)
}

func (r *linkedInRepository) ExtendUnmeteredSeats(ctx context.Context, orgID uuid.UUID, dueBefore time.Time) error {
	return r.exec(ctx, `UPDATE linkedin_accounts SET paid_through = paid_through + interval '1 month', billing_failures = 0,
		unpaid_since = NULL, updated_at = now()
		WHERE organization_id = $1 AND `+linkedInSeat+` AND pending_charge_key IS NULL AND paid_through < $2`, orgID, dueBefore)
}

func (r *linkedInRepository) SeatsDue(ctx context.Context, orgID uuid.UUID, dueBefore time.Time) ([]models.LinkedInAccount, error) {
	return r.accounts(ctx, `SELECT `+linkedInAccountColumns+` FROM linkedin_accounts
		WHERE organization_id = $1 AND `+linkedInSeat+` AND paid_through < $2 ORDER BY paid_through, id`, orgID, dueBefore)
}

func (r *linkedInRepository) SetSeatNotice(ctx context.Context, id uuid.UUID, notice string) (bool, error) {
	n, err := r.affected(ctx, `UPDATE linkedin_accounts SET last_notice = $2 WHERE id = $1 AND last_notice <> $2`, id, notice)
	return n > 0, err
}

// ---- connect sessions -----------------------------------------------------

const linkedInSessionColumns = `id, organization_id, user_id, kind, account_id, charge_key, charged_micros, status,
	account_graphkit_id, refunded_at, created_at, updated_at`

func scanLinkedInSession(row pgx.Row) (*models.LinkedInConnectSession, error) {
	var s models.LinkedInConnectSession
	err := row.Scan(&s.ID, &s.OrganizationID, &s.UserID, &s.Kind, &s.AccountID, &s.ChargeKey, &s.ChargedMicros, &s.Status,
		&s.AccountGraphKitID, &s.RefundedAt, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *linkedInRepository) CreateSession(ctx context.Context, s *models.LinkedInConnectSession) error {
	return r.exec(ctx, `INSERT INTO linkedin_connect_sessions (id, organization_id, user_id, kind, account_id, charge_key,
		charged_micros, status) VALUES ($1, $2, $3, $4, $5, $6, $7, $8) ON CONFLICT (id) DO NOTHING`,
		s.ID, s.OrganizationID, s.UserID, s.Kind, s.AccountID, s.ChargeKey, s.ChargedMicros, s.Status)
}

func (r *linkedInRepository) GetSession(ctx context.Context, orgID uuid.UUID, id string) (*models.LinkedInConnectSession, error) {
	return scanLinkedInSession(r.db.QueryRow(ctx, `SELECT `+linkedInSessionColumns+` FROM linkedin_connect_sessions
		WHERE organization_id = $1 AND id = $2`, orgID, id))
}

func (r *linkedInRepository) UpdateSession(ctx context.Context, orgID uuid.UUID, id, status string, accountGraphKitID *string, accountID *uuid.UUID) error {
	return r.exec(ctx, `UPDATE linkedin_connect_sessions SET status = $3,
		account_graphkit_id = COALESCE($4, account_graphkit_id), account_id = COALESCE($5, account_id), updated_at = now()
		WHERE organization_id = $1 AND id = $2`, orgID, id, status, accountGraphKitID, accountID)
}

func (r *linkedInRepository) MarkSessionRefunded(ctx context.Context, orgID uuid.UUID, id string) (bool, error) {
	n, err := r.affected(ctx, `UPDATE linkedin_connect_sessions SET refunded_at = now(), updated_at = now()
		WHERE organization_id = $1 AND id = $2 AND refunded_at IS NULL`, orgID, id)
	return n > 0, err
}

func (r *linkedInRepository) OpenSessions(ctx context.Context, before time.Time, limit int) ([]models.LinkedInConnectSession, error) {
	query := `SELECT ` + linkedInSessionColumns + ` FROM linkedin_connect_sessions
		WHERE (status IN ('sign_in', 'checkpoint') OR (charged_micros > 0 AND refunded_at IS NULL AND account_id IS NULL))
		AND updated_at < $1 ORDER BY updated_at LIMIT $2`
	rows, err := r.db.Query(ctx, query, before, limit)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	var out []models.LinkedInConnectSession
	for rows.Next() {
		s, err := scanLinkedInSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

// ---- conversations --------------------------------------------------------

const linkedInConversationColumns = `id, organization_id, linkedin_account_id, graphkit_id, contact_id, participant, kind,
	unread_count, last_message_at, last_message_preview, created_at, updated_at`

func scanLinkedInConversation(row pgx.Row) (*models.LinkedInConversation, error) {
	var c models.LinkedInConversation
	err := row.Scan(&c.ID, &c.OrganizationID, &c.AccountID, &c.GraphKitID, &c.ContactID, &c.Participant, &c.Kind,
		&c.UnreadCount, &c.LastMessageAt, &c.LastMessagePreview, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *linkedInRepository) UpsertConversation(ctx context.Context, c *models.LinkedInConversation) (*models.LinkedInConversation, error) {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	if len(c.Participant) == 0 {
		c.Participant = json.RawMessage(`{}`)
	}
	return scanLinkedInConversation(r.db.QueryRow(ctx, `INSERT INTO linkedin_conversations (id, organization_id, linkedin_account_id,
			graphkit_id, contact_id, participant, kind, unread_count, last_message_at, last_message_preview)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (graphkit_id) DO UPDATE SET participant = EXCLUDED.participant, kind = EXCLUDED.kind,
			unread_count = EXCLUDED.unread_count, contact_id = COALESCE(EXCLUDED.contact_id, linkedin_conversations.contact_id),
			last_message_at = GREATEST(linkedin_conversations.last_message_at, EXCLUDED.last_message_at),
			last_message_preview = CASE WHEN EXCLUDED.last_message_at IS NOT NULL AND (linkedin_conversations.last_message_at IS NULL
				OR EXCLUDED.last_message_at >= linkedin_conversations.last_message_at)
				THEN EXCLUDED.last_message_preview ELSE linkedin_conversations.last_message_preview END,
			updated_at = now()
		WHERE linkedin_conversations.organization_id = EXCLUDED.organization_id
		RETURNING `+linkedInConversationColumns,
		c.ID, c.OrganizationID, c.AccountID, c.GraphKitID, c.ContactID, c.Participant, c.Kind, c.UnreadCount, c.LastMessageAt, c.LastMessagePreview))
}

func (r *linkedInRepository) GetConversation(ctx context.Context, orgID, id uuid.UUID) (*models.LinkedInConversation, error) {
	return scanLinkedInConversation(r.db.QueryRow(ctx, `SELECT `+linkedInConversationColumns+` FROM linkedin_conversations
		WHERE organization_id = $1 AND id = $2`, orgID, id))
}

func (r *linkedInRepository) ListConversations(ctx context.Context, orgID uuid.UUID, f LinkedInConversationFilter) ([]models.LinkedInConversation, error) {
	query := `SELECT ` + linkedInConversationColumns + ` FROM linkedin_conversations c
		WHERE organization_id = $1 AND ($2::uuid IS NULL OR linkedin_account_id = $2) AND ($3::uuid IS NULL OR contact_id = $3)
		AND (NOT $4 OR unread_count > 0)
		AND ($5::uuid IS NULL OR (COALESCE(last_message_at, '-infinity'::timestamptz), id) < (COALESCE($6::timestamptz, '-infinity'::timestamptz), $5::uuid))
		AND EXISTS (SELECT 1 FROM linkedin_accounts a WHERE a.id = c.linkedin_account_id AND a.organization_id = $1 AND a.removed_at IS NULL)
		ORDER BY COALESCE(last_message_at, '-infinity'::timestamptz) DESC, id DESC LIMIT $7`
	rows, err := r.db.Query(ctx, query, orgID, f.AccountID, f.ContactID, f.UnreadOnly, f.AfterID, f.AfterAt, f.Limit)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	out := []models.LinkedInConversation{}
	for rows.Next() {
		c, err := scanLinkedInConversation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (r *linkedInRepository) MarkConversationRead(ctx context.Context, orgID, id uuid.UUID) error {
	return r.exec(ctx, `UPDATE linkedin_conversations SET unread_count = 0, updated_at = now()
		WHERE organization_id = $1 AND id = $2`, orgID, id)
}

const linkedInMessageColumns = `id, organization_id, conversation_id, graphkit_id, direction, body_sealed, sent_at, status, kind,
	linkedin_action_id, sent_by_user_id, created_at`

func (r *linkedInRepository) UpsertMessage(ctx context.Context, m *models.LinkedInMessage) (bool, error) {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	var inserted bool
	err := r.db.QueryRow(ctx, `INSERT INTO linkedin_messages (id, organization_id, conversation_id, graphkit_id, direction,
			body_sealed, sent_at, status, kind, linkedin_action_id, sent_by_user_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (graphkit_id) DO UPDATE SET status = EXCLUDED.status, sent_at = COALESCE(EXCLUDED.sent_at, linkedin_messages.sent_at),
			sent_by_user_id = COALESCE(linkedin_messages.sent_by_user_id, EXCLUDED.sent_by_user_id)
		WHERE linkedin_messages.organization_id = EXCLUDED.organization_id
		RETURNING (xmax = 0)`,
		m.ID, m.OrganizationID, m.ConversationID, m.GraphKitID, m.Direction, m.BodySealed, m.SentAt, m.Status, m.Kind,
		m.ActionID, m.SentByUserID).Scan(&inserted)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return inserted, err
}

func (r *linkedInRepository) ListMessages(ctx context.Context, orgID, conversationID uuid.UUID, after *models.StorePage, limit int) ([]models.LinkedInMessage, error) {
	at, id := keyset(after)
	query := `SELECT ` + linkedInMessageColumns + ` FROM linkedin_messages
		WHERE organization_id = $1 AND conversation_id = $2
		AND ($3::timestamptz IS NULL OR (COALESCE(sent_at, created_at), id) < ($3, $4::uuid))
		ORDER BY COALESCE(sent_at, created_at) DESC, id DESC LIMIT $5`
	rows, err := r.db.Query(ctx, query, orgID, conversationID, at, id, limit)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	out := []models.LinkedInMessage{}
	for rows.Next() {
		var m models.LinkedInMessage
		if err := rows.Scan(&m.ID, &m.OrganizationID, &m.ConversationID, &m.GraphKitID, &m.Direction, &m.BodySealed, &m.SentAt,
			&m.Status, &m.Kind, &m.ActionID, &m.SentByUserID, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *linkedInRepository) LatestMessageAt(ctx context.Context, orgID uuid.UUID, accountID uuid.UUID) (*time.Time, error) {
	var t *time.Time
	err := r.db.QueryRow(ctx, `SELECT max(last_message_at) FROM linkedin_conversations
		WHERE organization_id = $1 AND linkedin_account_id = $2`, orgID, accountID).Scan(&t)
	return t, err
}

// ---- saved searches -------------------------------------------------------

const linkedInSavedColumns = `id, organization_id, name, query, created_by, created_at, updated_at`

func scanLinkedInSaved(row pgx.Row) (*models.LinkedInSavedSearch, error) {
	var s models.LinkedInSavedSearch
	err := row.Scan(&s.ID, &s.OrganizationID, &s.Name, &s.Query, &s.CreatedBy, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *linkedInRepository) ListSavedSearches(ctx context.Context, orgID uuid.UUID, after *models.StorePage, limit int) ([]models.LinkedInSavedSearch, error) {
	at, id := keyset(after)
	query := `SELECT ` + linkedInSavedColumns + ` FROM linkedin_saved_searches
		WHERE organization_id = $1 AND ($2::timestamptz IS NULL OR (created_at, id) < ($2, $3::uuid))
		ORDER BY created_at DESC, id DESC LIMIT $4`
	rows, err := r.db.Query(ctx, query, orgID, at, id, limit)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	out := []models.LinkedInSavedSearch{}
	for rows.Next() {
		s, err := scanLinkedInSaved(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

func (r *linkedInRepository) GetSavedSearch(ctx context.Context, orgID, id uuid.UUID) (*models.LinkedInSavedSearch, error) {
	return scanLinkedInSaved(r.db.QueryRow(ctx, `SELECT `+linkedInSavedColumns+` FROM linkedin_saved_searches
		WHERE organization_id = $1 AND id = $2`, orgID, id))
}

func (r *linkedInRepository) CreateSavedSearch(ctx context.Context, s *models.LinkedInSavedSearch) error {
	return r.db.QueryRow(ctx, `INSERT INTO linkedin_saved_searches (id, organization_id, name, query, created_by)
		VALUES ($1, $2, $3, $4, $5) RETURNING created_at, updated_at`, s.ID, s.OrganizationID, s.Name, s.Query, s.CreatedBy).
		Scan(&s.CreatedAt, &s.UpdatedAt)
}

func (r *linkedInRepository) UpdateSavedSearch(ctx context.Context, orgID, id uuid.UUID, name *string, query json.RawMessage) (*models.LinkedInSavedSearch, error) {
	var q any
	if len(query) > 0 {
		q = query
	}
	return scanLinkedInSaved(r.db.QueryRow(ctx, `UPDATE linkedin_saved_searches SET name = COALESCE($3, name),
		query = COALESCE($4::jsonb, query), updated_at = now() WHERE organization_id = $1 AND id = $2
		RETURNING `+linkedInSavedColumns, orgID, id, name, q))
}

func (r *linkedInRepository) DeleteSavedSearch(ctx context.Context, orgID, id uuid.UUID) (bool, error) {
	n, err := r.affected(ctx, `DELETE FROM linkedin_saved_searches WHERE organization_id = $1 AND id = $2`, orgID, id)
	return n > 0, err
}

// ---- contacts -------------------------------------------------------------

func (r *linkedInRepository) ContactMatches(ctx context.Context, orgID uuid.UUID, publicIDs []string) (map[string]models.LinkedInContactMatch, error) {
	out := map[string]models.LinkedInContactMatch{}
	if len(publicIDs) == 0 {
		return out, nil
	}
	query := `SELECT DISTINCT ON (c.linkedin_public_id) c.linkedin_public_id, c.id, c.company,
			COALESCE((SELECT jsonb_agg(jsonb_build_object('id', ca.id, 'name', ca.name, 'status', ca.status) ORDER BY ca.name)
				FROM campaign_leads cl JOIN campaigns ca ON ca.id = cl.campaign_id AND ca.organization_id = $1
				WHERE cl.contact_id = c.id), '[]'::jsonb),
			EXISTS (SELECT 1 FROM campaign_contact_progress p JOIN campaigns ca ON ca.id = p.campaign_id AND ca.organization_id = $1
				WHERE p.contact_id = c.id AND p.sent_at IS NOT NULL),
			EXISTS (SELECT 1 FROM campaign_contact_progress p JOIN campaigns ca ON ca.id = p.campaign_id AND ca.organization_id = $1
				WHERE p.contact_id = c.id AND p.replied_at IS NOT NULL)
		FROM contacts c WHERE c.organization_id = $1 AND c.linkedin_public_id = ANY($2)
		ORDER BY c.linkedin_public_id, c.created_at`
	rows, err := r.db.Query(ctx, query, orgID, publicIDs)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var m models.LinkedInContactMatch
		var camps []byte
		if err := rows.Scan(&m.PublicID, &m.ContactID, &m.Company, &camps, &m.Contacted, &m.Replied); err != nil {
			return nil, err
		}
		m.Campaigns = []models.LinkedInCampaignRef{}
		_ = json.Unmarshal(camps, &m.Campaigns)
		out[m.PublicID] = m
	}
	return out, rows.Err()
}

func (r *linkedInRepository) CompanyContacts(ctx context.Context, orgID uuid.UUID, companies []string) (map[string]int, error) {
	out := map[string]int{}
	if len(companies) == 0 {
		return out, nil
	}
	lower := make([]string, len(companies))
	for i, c := range companies {
		lower[i] = strings.ToLower(strings.TrimSpace(c))
	}
	query := `SELECT lower(company), count(*) FROM contacts WHERE organization_id = $1 AND lower(company) = ANY($2) GROUP BY 1`
	rows, err := r.db.Query(ctx, query, orgID, lower)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var n int
		if err := rows.Scan(&name, &n); err != nil {
			return nil, err
		}
		out[name] = n
	}
	return out, rows.Err()
}

func (r *linkedInRepository) ConversationStates(ctx context.Context, orgID uuid.UUID, publicIDs []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(publicIDs) == 0 {
		return out, nil
	}
	query := `SELECT c.participant->>'public_id',
			bool_or(EXISTS (SELECT 1 FROM linkedin_messages m WHERE m.conversation_id = c.id AND m.organization_id = $1 AND m.direction = 'inbound'))
		FROM linkedin_conversations c WHERE c.organization_id = $1 AND c.participant->>'public_id' = ANY($2) GROUP BY 1`
	rows, err := r.db.Query(ctx, query, orgID, publicIDs)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var replied bool
		if err := rows.Scan(&id, &replied); err != nil {
			return nil, err
		}
		out[strings.ToLower(id)] = replied
	}
	return out, rows.Err()
}

func (r *linkedInRepository) ContactEmails(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	out := map[uuid.UUID]string{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.db.Query(ctx, `SELECT id, email FROM contacts WHERE organization_id = $1 AND id = ANY($2)`, orgID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var email string
		if err := rows.Scan(&id, &email); err != nil {
			return nil, err
		}
		out[id] = email
	}
	return out, rows.Err()
}

// ---- contact backfill -----------------------------------------------------

func (r *linkedInRepository) BackfillContactLinkedIn(ctx context.Context, after uuid.UUID, limit int) (uuid.UUID, int, error) {
	// Writing custom_fields back to itself runs the trigger that derives the columns.
	query := `WITH page AS (SELECT id FROM contacts WHERE id > $1 ORDER BY id LIMIT $2),
		touched AS (UPDATE contacts c SET custom_fields = c.custom_fields FROM page
			WHERE c.id = page.id AND c.linkedin_public_id IS NULL AND c.custom_fields ? 'linkedin_url' RETURNING c.id)
		SELECT (SELECT id FROM page ORDER BY id DESC LIMIT 1), (SELECT count(*) FROM page), (SELECT count(*) FROM touched)`
	var last *uuid.UUID
	var n, touched int
	if err := r.db.QueryRow(ctx, query, after, limit).Scan(&last, &n, &touched); err != nil {
		db.CaptureError(err, query, nil, "query")
		return after, 0, err
	}
	if last == nil {
		return after, 0, nil
	}
	return *last, n, nil
}

func (r *linkedInRepository) BackfillProgress(ctx context.Context) (uuid.UUID, bool, error) {
	var raw []byte
	err := r.db.QueryRow(ctx, `SELECT value FROM admin_settings WHERE key = $1`, LinkedInContactBackfillKey).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, err
	}
	var p struct {
		After uuid.UUID `json:"after"`
		Done  bool      `json:"done"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return uuid.Nil, false, nil
	}
	return p.After, p.Done, nil
}

func (r *linkedInRepository) SaveBackfillProgress(ctx context.Context, after uuid.UUID, done bool) error {
	raw, _ := json.Marshal(map[string]any{"after": after, "done": done})
	return r.exec(ctx, `INSERT INTO admin_settings (key, value, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`, LinkedInContactBackfillKey, raw)
}
