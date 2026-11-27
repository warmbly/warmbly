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

// SocialContactBackfillKey is the admin_settings row holding the contact identity backfill's place.
const SocialContactBackfillKey = "social.contact_backfill"

// SocialRepository holds the social add-on. Every read and write a workspace
// drives is scoped by organization_id; only the billing and reconcile sweeps
// read across workspaces, and each row they return carries its own.
type SocialRepository interface {
	// OrganizationExists reports whether this instance has the workspace a GraphKit event names.
	OrganizationExists(ctx context.Context, orgID uuid.UUID) (bool, error)
	GetWorkspace(ctx context.Context, orgID uuid.UUID) (*models.SocialWorkspace, error)
	// SetWorkspace records the mode GraphKit reported; a link session id is kept unless replaced.
	SetWorkspace(ctx context.Context, orgID uuid.UUID, mode string, name *string, linkedAt *time.Time) error
	SetLinkSession(ctx context.Context, orgID uuid.UUID, sessionID *string) error

	// UpsertAccount records GraphKit's view of an account, by its GraphKit id, within the workspace. Only revive (a
	// connect that succeeded) brings back a removed store seat; a snapshot older than the stored one is ignored.
	UpsertAccount(ctx context.Context, a *models.SocialAccount, revive bool, snapshotAt *time.Time) (*models.SocialAccount, error)
	GetAccount(ctx context.Context, orgID, id uuid.UUID) (*models.SocialAccount, error)
	GetAccountByGraphKitID(ctx context.Context, orgID uuid.UUID, graphkitID string) (*models.SocialAccount, error)
	// ListAccounts is the workspace's accounts newest first; removed ones only when withRemoved.
	ListAccounts(ctx context.Context, orgID uuid.UUID, after *models.StorePage, limit int, withRemoved bool) ([]models.SocialAccount, error)
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
	SeatsByChargeKey(ctx context.Context, orgID uuid.UUID, key string) ([]models.SocialAccount, error)
	// SettleSeats moves the live seats under key a month on; it returns them.
	SettleSeats(ctx context.Context, orgID uuid.UUID, key string) ([]models.SocialAccount, error)
	DetachSeat(ctx context.Context, orgID uuid.UUID, key string, id uuid.UUID) error
	// ReleaseSeats ends the reservation under key as unpaid; it returns the seats it released.
	ReleaseSeats(ctx context.Context, orgID uuid.UUID, key string) ([]models.SocialAccount, error)
	UnpaidSeats(ctx context.Context, orgID uuid.UUID) ([]models.SocialAccount, error)
	SetUnpaidPaused(ctx context.Context, orgID, id uuid.UUID, paused bool) error
	ExtendUnmeteredSeats(ctx context.Context, orgID uuid.UUID, dueBefore time.Time) error
	SeatsDue(ctx context.Context, orgID uuid.UUID, dueBefore time.Time) ([]models.SocialAccount, error)
	SetSeatNotice(ctx context.Context, id uuid.UUID, notice string) (bool, error)
	// RemovedSeatsPaidAhead are removed seats whose last paid month had not started when they were removed; orgID narrows it.
	RemovedSeatsPaidAhead(ctx context.Context, orgID *uuid.UUID, limit int) ([]models.SocialAccount, error)
	// ReturnSeatMonth moves such a seat's paid month back once it is refunded.
	ReturnSeatMonth(ctx context.Context, orgID, id uuid.UUID, paidKey string) error
	// PausedPaidSeats are seats unpaid had paused whose month has since been paid.
	PausedPaidSeats(ctx context.Context, orgID uuid.UUID) ([]models.SocialAccount, error)

	CreateSession(ctx context.Context, s *models.SocialConnectSession) error
	GetSession(ctx context.Context, orgID uuid.UUID, id string) (*models.SocialConnectSession, error)
	// UpdateSession records a session's status and the account it connected.
	UpdateSession(ctx context.Context, orgID uuid.UUID, id, status string, accountGraphKitID *string, accountID *uuid.UUID) error
	// SetSessionMethod records the sign-in method a session last tried.
	SetSessionMethod(ctx context.Context, orgID uuid.UUID, id, method string) error
	// MarkSessionRefunded records the refund once; false when it already was.
	MarkSessionRefunded(ctx context.Context, orgID uuid.UUID, id string) (bool, error)
	// OpenSessions are sessions still in flight or whose charge is unsettled, untouched since before, across workspaces.
	OpenSessions(ctx context.Context, before time.Time, limit int) ([]models.SocialConnectSession, error)

	UpsertConversation(ctx context.Context, c *models.SocialConversation) (*models.SocialConversation, error)
	GetConversation(ctx context.Context, orgID, id uuid.UUID) (*models.SocialConversation, error)
	ListConversations(ctx context.Context, orgID uuid.UUID, f SocialConversationFilter) ([]models.SocialConversation, error)
	MarkConversationRead(ctx context.Context, orgID, id uuid.UUID) error
	// UpsertMessage records a message once by its GraphKit id; later calls move its status. inserted is true the first time.
	UpsertMessage(ctx context.Context, m *models.SocialMessage) (inserted bool, err error)
	ListMessages(ctx context.Context, orgID, conversationID uuid.UUID, after *models.StorePage, limit int) ([]models.SocialMessage, error)
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

	// BackfillContactSocial seeds LinkedIn identities from the linkedin_url custom field for up to limit contacts after the id.
	BackfillContactSocial(ctx context.Context, after uuid.UUID, limit int) (last uuid.UUID, n int, err error)
	BackfillProgress(ctx context.Context) (after uuid.UUID, done bool, err error)
	SaveBackfillProgress(ctx context.Context, after uuid.UUID, done bool) error

	// CampaignAccountIDs are the live social accounts a campaign sends its social steps from, on every channel.
	CampaignAccountIDs(ctx context.Context, orgID, campaignID uuid.UUID) ([]uuid.UUID, error)
	// LeadsWithoutHandle counts the campaign's leads with no handle on a channel one of its social steps uses.
	LeadsWithoutHandle(ctx context.Context, orgID, campaignID uuid.UUID) (int, error)
	// SetCampaignAccounts replaces them; false when the campaign or an account is not the workspace's.
	SetCampaignAccounts(ctx context.Context, orgID, campaignID uuid.UUID, ids []uuid.UUID) (bool, error)
	CampaignAccounts(ctx context.Context, orgID, campaignID uuid.UUID) ([]models.SocialAccount, error)
	// CampaignWakeAccount is a mailbox of the campaign's workspace to park its wake-ups on, when it has social accounts.
	CampaignWakeAccount(ctx context.Context, campaignID uuid.UUID) (uuid.UUID, bool, error)

	// ReserveAction writes the step's action row once and returns the row the step holds.
	ReserveAction(ctx context.Context, a *models.SocialActionRow) (*models.SocialActionRow, error)
	SetActionQueued(ctx context.Context, orgID, id, accountID uuid.UUID, graphkitID, status string) error
	CountActionAttempt(ctx context.Context, orgID, id uuid.UUID) (int, error)
	FinishAction(ctx context.Context, orgID, id uuid.UUID, status string, skipReason, errorCode *string) error
	// ApplyActionUpdate records GraphKit's status for an action; changed is false when it already had it.
	ApplyActionUpdate(ctx context.Context, orgID uuid.UUID, u SocialActionUpdate) (row *models.SocialActionRow, changed bool, err error)
	// ActionsToStop are queued campaign actions, across workspaces, whose contact replied, opted out, was suppressed or left
	// the campaign, or whose campaign was paused or deleted; contactID narrows it to one contact.
	ActionsToStop(ctx context.Context, contactID *uuid.UUID, limit int) ([]SocialActionStop, error)
	// StickyAccount is the account a contact keeps on a channel in a campaign: the one bound or used before, else one
	// connected to or inviting them.
	StickyAccount(ctx context.Context, orgID, campaignID, contactID uuid.UUID, channel string, candidates []uuid.UUID) (*uuid.UUID, error)
	// OpenActionsToday counts actions not yet run per account and type, created since.
	OpenActionsToday(ctx context.Context, orgID uuid.UUID, accountIDs []uuid.UUID, since time.Time) (map[uuid.UUID]map[string]int, error)
	ContactActions(ctx context.Context, orgID, contactID uuid.UUID, limit int) ([]models.SocialActionRow, error)
	CampaignStats(ctx context.Context, orgID uuid.UUID, campaignIDs []uuid.UUID) (map[uuid.UUID]*models.SocialCampaignStats, error)

	// NoteContactState folds one signal from an account into the channel state of each named contact in their campaigns;
	// campaignID narrows it to one campaign.
	NoteContactState(ctx context.Context, orgID uuid.UUID, campaignID *uuid.UUID, contactIDs []uuid.UUID, channel string, accountID uuid.UUID, c SocialStateChange) error
	// SetOpenProfile records a campaign contact's LinkedIn Open Profile as the profiles call read it.
	SetOpenProfile(ctx context.Context, orgID, campaignID, contactID uuid.UUID, open bool) error
	// OpenProfileChecks are campaign contacts, across workspaces, an Open Profile branch is waiting on.
	OpenProfileChecks(ctx context.Context, limit int) ([]OpenProfileCheck, error)
	// ContactIDsByHandle are the workspace's contacts with a normalized handle on a channel.
	ContactIDsByHandle(ctx context.Context, orgID uuid.UUID, channel, normalized string) ([]uuid.UUID, error)
	// ContactIdentity is a contact's identity on a channel, nil when it has none.
	ContactIdentity(ctx context.Context, orgID, contactID uuid.UUID, channel string) (*models.SocialIdentityRow, error)
	// SetContactIdentity writes one, keeping a stored consent basis the row leaves blank.
	SetContactIdentity(ctx context.Context, orgID, contactID uuid.UUID, row models.SocialIdentityRow) error
	// InviteStates is, per public id with an invite pending or a connection, whether it is connected.
	InviteStates(ctx context.Context, orgID uuid.UUID, publicIDs []string) (map[string]bool, error)
	ActiveCampaignsFor(ctx context.Context, orgID uuid.UUID, contactIDs []uuid.UUID) ([]string, error)
	// ReplyCampaign is the campaign step a social message at that time answers: the contact's latest step in a campaign that reached them on social.
	ReplyCampaign(ctx context.Context, orgID, contactID uuid.UUID, at time.Time) (campaignID, sequenceID *uuid.UUID, err error)
	// ClaimInboundReply takes an inbound message for the reply path once.
	ClaimInboundReply(ctx context.Context, orgID, messageID uuid.UUID) (bool, error)
	MessageID(ctx context.Context, orgID uuid.UUID, graphkitID string) (uuid.UUID, error)
	UnreadConversations(ctx context.Context, orgID uuid.UUID) (int, error)

	CreateImport(ctx context.Context, im *models.LinkedInImport) error
	GetImport(ctx context.Context, orgID, id uuid.UUID) (*models.LinkedInImport, error)
	// ClaimImport takes a queued import, or a running one untouched since staleBefore, across workspaces; id narrows it to one.
	ClaimImport(ctx context.Context, id *uuid.UUID, staleBefore time.Time) (*models.LinkedInImport, error)
	SaveImport(ctx context.Context, im *models.LinkedInImport) error
}

// SocialConversationFilter narrows a conversation list.
type SocialConversationFilter struct {
	AccountID  *uuid.UUID
	ContactID  *uuid.UUID
	Channel    string
	UnreadOnly bool
	// After is the last row of the previous page, by last activity.
	AfterAt *time.Time
	AfterID *uuid.UUID
	Limit   int
}

type socialRepository struct{ db *db.DB }

// NewSocialRepository builds the Postgres social repository.
func NewSocialRepository(d *db.DB) SocialRepository { return &socialRepository{db: d} }

func (r *socialRepository) exec(ctx context.Context, query string, args ...any) error {
	if _, err := r.db.Exec(ctx, query, args...); err != nil {
		db.CaptureError(err, query, nil, "exec")
		return err
	}
	return nil
}

func (r *socialRepository) affected(ctx context.Context, query string, args ...any) (int64, error) {
	tag, err := r.db.Exec(ctx, query, args...)
	if err != nil {
		db.CaptureError(err, query, nil, "exec")
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ---- workspace ------------------------------------------------------------

func (r *socialRepository) OrganizationExists(ctx context.Context, orgID uuid.UUID) (bool, error) {
	var ok bool
	err := r.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM organizations WHERE id = $1)`, orgID).Scan(&ok)
	return ok, err
}

func (r *socialRepository) GetWorkspace(ctx context.Context, orgID uuid.UUID) (*models.SocialWorkspace, error) {
	var w models.SocialWorkspace
	err := r.db.QueryRow(ctx, `SELECT organization_id, mode, workspace_name, linked_at, link_session_id, updated_at
		FROM social_workspaces WHERE organization_id = $1`, orgID).
		Scan(&w.OrganizationID, &w.Mode, &w.WorkspaceName, &w.LinkedAt, &w.LinkSessionID, &w.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}

func (r *socialRepository) SetWorkspace(ctx context.Context, orgID uuid.UUID, mode string, name *string, linkedAt *time.Time) error {
	return r.exec(ctx, `INSERT INTO social_workspaces (organization_id, mode, workspace_name, linked_at, updated_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (organization_id) DO UPDATE SET mode = EXCLUDED.mode, workspace_name = EXCLUDED.workspace_name,
			linked_at = EXCLUDED.linked_at, updated_at = now()`, orgID, mode, name, linkedAt)
}

func (r *socialRepository) SetLinkSession(ctx context.Context, orgID uuid.UUID, sessionID *string) error {
	return r.exec(ctx, `INSERT INTO social_workspaces (organization_id, link_session_id, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (organization_id) DO UPDATE SET link_session_id = EXCLUDED.link_session_id, updated_at = now()`, orgID, sessionID)
}

// ---- accounts -------------------------------------------------------------

const socialAccountColumns = `id, organization_id, user_id, channel, graphkit_id, status, status_reason, display_name, headline,
	avatar_url, profile_url, public_id, licences, snapshot, billing_mode, monthly_retail_micros, paid_through,
	billing_failures, billing_attempted_at, pending_charge_key, pending_charge_micros, paid_key, paid_micros,
	unpaid_since, unpaid_paused, last_notice, connected_at, removed_at, created_at, updated_at`

// socialSeat is a seat the workspace pays for here.
const socialSeat = `billing_mode = 'store' AND removed_at IS NULL AND paid_through IS NOT NULL`

func scanSocialAccount(row pgx.Row) (*models.SocialAccount, error) {
	var a models.SocialAccount
	err := row.Scan(&a.ID, &a.OrganizationID, &a.UserID, &a.Channel, &a.GraphKitID, &a.Status, &a.StatusReason, &a.DisplayName, &a.Headline,
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

func (r *socialRepository) accounts(ctx context.Context, query string, args ...any) ([]models.SocialAccount, error) {
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	out := []models.SocialAccount{}
	for rows.Next() {
		a, err := scanSocialAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (r *socialRepository) UpsertAccount(ctx context.Context, a *models.SocialAccount, revive bool, snapshotAt *time.Time) (*models.SocialAccount, error) {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	if a.Licences == nil {
		a.Licences = []string{}
	}
	if len(a.Snapshot) == 0 {
		a.Snapshot = json.RawMessage(`{}`)
	}
	// Billing columns are the add-on's own and only set on insert or revive; the conflict target never crosses
	// workspaces. A removed account comes back only through a connect (revive), or a linked one through its link,
	// and a snapshot older than the one stored changes nothing.
	got, err := scanSocialAccount(r.db.QueryRow(ctx, `INSERT INTO social_accounts (id, organization_id, user_id, graphkit_id, status,
			status_reason, display_name, headline, avatar_url, profile_url, public_id, licences, snapshot, billing_mode,
			monthly_retail_micros, connected_at, removed_at, channel)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $20)
		ON CONFLICT (graphkit_id) DO UPDATE SET
			status = CASE WHEN EXCLUDED.status <> 'removed' AND social_accounts.removed_at IS NOT NULL AND NOT `+revived+`
				THEN 'removed' ELSE EXCLUDED.status END,
			status_reason = EXCLUDED.status_reason,
			display_name = EXCLUDED.display_name, headline = EXCLUDED.headline, avatar_url = EXCLUDED.avatar_url,
			profile_url = EXCLUDED.profile_url, public_id = EXCLUDED.public_id, licences = EXCLUDED.licences,
			snapshot = EXCLUDED.snapshot,
			connected_at = CASE WHEN `+revivedRemoved+` THEN now() ELSE COALESCE(social_accounts.connected_at, EXCLUDED.connected_at) END,
			unpaid_paused = CASE WHEN `+revivedRemoved+` THEN false ELSE social_accounts.unpaid_paused END,
			billing_failures = CASE WHEN `+revivedRemoved+` THEN 0 ELSE social_accounts.billing_failures END,
			unpaid_since = CASE WHEN `+revivedRemoved+` THEN NULL ELSE social_accounts.unpaid_since END,
			last_notice = CASE WHEN `+revivedRemoved+` THEN '' ELSE social_accounts.last_notice END,
			removed_at = CASE WHEN EXCLUDED.status = 'removed' THEN COALESCE(social_accounts.removed_at, now())
				WHEN `+revived+` THEN NULL
				ELSE social_accounts.removed_at END,
			user_id = COALESCE(social_accounts.user_id, EXCLUDED.user_id), updated_at = now()
		WHERE social_accounts.organization_id = EXCLUDED.organization_id
		  AND ($19::timestamptz IS NULL
		    OR COALESCE(NULLIF(social_accounts.snapshot->>'updated_at', '')::timestamptz, '-infinity') <= $19)
		RETURNING `+socialAccountColumns,
		a.ID, a.OrganizationID, a.UserID, a.GraphKitID, a.Status, a.StatusReason, a.DisplayName, a.Headline, a.AvatarURL,
		a.ProfileURL, a.PublicID, a.Licences, a.Snapshot, a.BillingMode, a.MonthlyRetailMicros, a.ConnectedAt, a.RemovedAt,
		revive, snapshotAt, a.Channel))
	if err != nil || got != nil {
		return got, err
	}
	// A stale snapshot or another workspace's id wrote nothing; the stored row, if it is this workspace's, stands.
	return r.GetAccountByGraphKitID(ctx, a.OrganizationID, a.GraphKitID)
}

// revived is true for the connect that brings an account back, or for a linked account whose link reports it again.
const revived = `($18::bool OR (social_accounts.billing_mode = 'linked' AND EXCLUDED.billing_mode = 'linked'))`

// revivedRemoved is a removed store seat a connect brings back: it starts clean, unpaid state and all.
const revivedRemoved = `($18::bool AND social_accounts.removed_at IS NOT NULL AND EXCLUDED.status <> 'removed')`

func (r *socialRepository) GetAccount(ctx context.Context, orgID, id uuid.UUID) (*models.SocialAccount, error) {
	return scanSocialAccount(r.db.QueryRow(ctx, `SELECT `+socialAccountColumns+` FROM social_accounts
		WHERE organization_id = $1 AND id = $2`, orgID, id))
}

func (r *socialRepository) GetAccountByGraphKitID(ctx context.Context, orgID uuid.UUID, graphkitID string) (*models.SocialAccount, error) {
	return scanSocialAccount(r.db.QueryRow(ctx, `SELECT `+socialAccountColumns+` FROM social_accounts
		WHERE organization_id = $1 AND graphkit_id = $2`, orgID, graphkitID))
}

func (r *socialRepository) ListAccounts(ctx context.Context, orgID uuid.UUID, after *models.StorePage, limit int, withRemoved bool) ([]models.SocialAccount, error) {
	at, id := keyset(after)
	return r.accounts(ctx, `SELECT `+socialAccountColumns+` FROM social_accounts
		WHERE organization_id = $1 AND ($2::timestamptz IS NULL OR (created_at, id) < ($2, $3::uuid)) AND ($5 OR removed_at IS NULL)
		ORDER BY created_at DESC, id DESC LIMIT $4`, orgID, at, id, limit, withRemoved)
}

func (r *socialRepository) CountAccounts(ctx context.Context, orgID uuid.UUID) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `SELECT count(*) FROM social_accounts WHERE organization_id = $1 AND removed_at IS NULL`, orgID).Scan(&n)
	return n, err
}

func (r *socialRepository) MarkRemoved(ctx context.Context, orgID, id uuid.UUID, at time.Time) error {
	// A pending charge is kept, so settling its batch gives this seat's share back.
	return r.exec(ctx, `UPDATE social_accounts SET status = 'removed', removed_at = COALESCE(removed_at, $3),
		updated_at = now() WHERE organization_id = $1 AND id = $2`, orgID, id, at)
}

func (r *socialRepository) ForgetLinkedAccounts(ctx context.Context, orgID uuid.UUID, at time.Time) (int64, error) {
	return r.affected(ctx, `UPDATE social_accounts SET status = 'removed', removed_at = COALESCE(removed_at, $2), updated_at = now()
		WHERE organization_id = $1 AND billing_mode = 'linked' AND removed_at IS NULL`, orgID, at)
}

func (r *socialRepository) SetSeatBilling(ctx context.Context, orgID, id uuid.UUID, mode string, monthly models.Micro, paidThrough *time.Time, paidKey *string, paidMicros models.Micro) error {
	return r.exec(ctx, `UPDATE social_accounts SET billing_mode = $3, monthly_retail_micros = $4, paid_through = $5,
		paid_key = $6, paid_micros = $7, billing_failures = 0, unpaid_since = NULL, updated_at = now()
		WHERE organization_id = $1 AND id = $2`, orgID, id, mode, monthly, paidThrough, paidKey, paidMicros)
}

// ---- seat billing ---------------------------------------------------------

func (r *socialRepository) FirstSeatAt(ctx context.Context, orgID uuid.UUID) (*time.Time, error) {
	var t *time.Time
	err := r.db.QueryRow(ctx, `SELECT min(COALESCE(connected_at, created_at)) FROM social_accounts
		WHERE organization_id = $1 AND `+socialSeat, orgID).Scan(&t)
	return t, err
}

func (r *socialRepository) OrgsWithSeats(ctx context.Context, limit int) ([]uuid.UUID, error) {
	rows, err := r.db.Query(ctx, `SELECT DISTINCT organization_id FROM social_accounts WHERE `+socialSeat+` LIMIT $1`, limit)
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

func (r *socialRepository) PendingSeatKeys(ctx context.Context, orgID uuid.UUID, reservedBefore time.Time) ([]string, error) {
	rows, err := r.db.Query(ctx, `SELECT DISTINCT pending_charge_key FROM social_accounts WHERE organization_id = $1
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

func (r *socialRepository) ReserveSeats(ctx context.Context, orgID uuid.UUID, key string, price models.Micro, dueBefore, retryBefore time.Time) (int, error) {
	n, err := r.affected(ctx, `UPDATE social_accounts SET pending_charge_key = $2, pending_charge_micros = $3,
		monthly_retail_micros = $3, billing_attempted_at = now(), updated_at = now()
		WHERE organization_id = $1 AND `+socialSeat+` AND pending_charge_key IS NULL
		AND ((billing_failures = 0 AND paid_through < $4)
			OR (billing_failures > 0 AND (billing_attempted_at IS NULL OR billing_attempted_at < $5)))`,
		orgID, key, price, dueBefore, retryBefore)
	return int(n), err
}

func (r *socialRepository) SeatsByChargeKey(ctx context.Context, orgID uuid.UUID, key string) ([]models.SocialAccount, error) {
	return r.accounts(ctx, `SELECT `+socialAccountColumns+` FROM social_accounts
		WHERE organization_id = $1 AND pending_charge_key = $2 ORDER BY id`, orgID, key)
}

func (r *socialRepository) SettleSeats(ctx context.Context, orgID uuid.UUID, key string) ([]models.SocialAccount, error) {
	return r.accounts(ctx, `UPDATE social_accounts SET paid_through = paid_through + interval '1 month', pending_charge_key = NULL,
		paid_key = $2, paid_micros = pending_charge_micros, billing_failures = 0, unpaid_since = NULL, last_notice = '', updated_at = now()
		WHERE organization_id = $1 AND pending_charge_key = $2 AND removed_at IS NULL
		RETURNING `+socialAccountColumns, orgID, key)
}

func (r *socialRepository) DetachSeat(ctx context.Context, orgID uuid.UUID, key string, id uuid.UUID) error {
	return r.exec(ctx, `UPDATE social_accounts SET pending_charge_key = NULL, updated_at = now()
		WHERE organization_id = $1 AND id = $2 AND pending_charge_key = $3`, orgID, id, key)
}

func (r *socialRepository) ReleaseSeats(ctx context.Context, orgID uuid.UUID, key string) ([]models.SocialAccount, error) {
	return r.accounts(ctx, `UPDATE social_accounts SET pending_charge_key = NULL, billing_failures = billing_failures + 1,
		unpaid_since = COALESCE(unpaid_since, now()), updated_at = now()
		WHERE organization_id = $1 AND pending_charge_key = $2 RETURNING `+socialAccountColumns, orgID, key)
}

func (r *socialRepository) UnpaidSeats(ctx context.Context, orgID uuid.UUID) ([]models.SocialAccount, error) {
	return r.accounts(ctx, `SELECT `+socialAccountColumns+` FROM social_accounts
		WHERE organization_id = $1 AND `+socialSeat+` AND billing_failures > 0 AND pending_charge_key IS NULL
		ORDER BY paid_through, id`, orgID)
}

func (r *socialRepository) PausedPaidSeats(ctx context.Context, orgID uuid.UUID) ([]models.SocialAccount, error) {
	return r.accounts(ctx, `SELECT `+socialAccountColumns+` FROM social_accounts
		WHERE organization_id = $1 AND `+socialSeat+` AND unpaid_paused AND billing_failures = 0 AND pending_charge_key IS NULL
		AND paid_through > now() ORDER BY id`, orgID)
}

func (r *socialRepository) SetUnpaidPaused(ctx context.Context, orgID, id uuid.UUID, paused bool) error {
	return r.exec(ctx, `UPDATE social_accounts SET unpaid_paused = $3, updated_at = now()
		WHERE organization_id = $1 AND id = $2`, orgID, id, paused)
}

func (r *socialRepository) ExtendUnmeteredSeats(ctx context.Context, orgID uuid.UUID, dueBefore time.Time) error {
	return r.exec(ctx, `UPDATE social_accounts SET paid_through = paid_through + interval '1 month', billing_failures = 0,
		unpaid_since = NULL, updated_at = now()
		WHERE organization_id = $1 AND `+socialSeat+` AND pending_charge_key IS NULL AND paid_through < $2`, orgID, dueBefore)
}

func (r *socialRepository) SeatsDue(ctx context.Context, orgID uuid.UUID, dueBefore time.Time) ([]models.SocialAccount, error) {
	return r.accounts(ctx, `SELECT `+socialAccountColumns+` FROM social_accounts
		WHERE organization_id = $1 AND `+socialSeat+` AND paid_through < $2 ORDER BY paid_through, id`, orgID, dueBefore)
}

// paidAheadOfRemoval is a removed seat whose last paid month starts after it was removed.
const paidAheadOfRemoval = `billing_mode = 'store' AND removed_at IS NOT NULL AND paid_key IS NOT NULL AND paid_micros > 0
	AND paid_through IS NOT NULL AND paid_through - interval '1 month' > removed_at`

func (r *socialRepository) RemovedSeatsPaidAhead(ctx context.Context, orgID *uuid.UUID, limit int) ([]models.SocialAccount, error) {
	return r.accounts(ctx, `SELECT `+socialAccountColumns+` FROM social_accounts
		WHERE ($1::uuid IS NULL OR organization_id = $1) AND `+paidAheadOfRemoval+` ORDER BY removed_at, id LIMIT $2`, orgID, limit)
}

func (r *socialRepository) ReturnSeatMonth(ctx context.Context, orgID, id uuid.UUID, paidKey string) error {
	return r.exec(ctx, `UPDATE social_accounts SET paid_through = paid_through - interval '1 month', paid_key = NULL, paid_micros = 0,
		updated_at = now() WHERE organization_id = $1 AND id = $2 AND paid_key = $3 AND `+paidAheadOfRemoval, orgID, id, paidKey)
}

func (r *socialRepository) SetSeatNotice(ctx context.Context, id uuid.UUID, notice string) (bool, error) {
	n, err := r.affected(ctx, `UPDATE social_accounts SET last_notice = $2 WHERE id = $1 AND last_notice <> $2`, id, notice)
	return n > 0, err
}

// ---- connect sessions -----------------------------------------------------

const socialSessionColumns = `id, organization_id, user_id, channel, kind, method, account_id, charge_key, charged_micros, status,
	account_graphkit_id, refunded_at, created_at, updated_at`

func scanSocialSession(row pgx.Row) (*models.SocialConnectSession, error) {
	var s models.SocialConnectSession
	err := row.Scan(&s.ID, &s.OrganizationID, &s.UserID, &s.Channel, &s.Kind, &s.Method, &s.AccountID, &s.ChargeKey, &s.ChargedMicros, &s.Status,
		&s.AccountGraphKitID, &s.RefundedAt, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *socialRepository) CreateSession(ctx context.Context, s *models.SocialConnectSession) error {
	return r.exec(ctx, `INSERT INTO social_connect_sessions (id, organization_id, user_id, kind, account_id, charge_key,
		charged_micros, status, channel) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) ON CONFLICT (id) DO NOTHING`,
		s.ID, s.OrganizationID, s.UserID, s.Kind, s.AccountID, s.ChargeKey, s.ChargedMicros, s.Status, s.Channel)
}

func (r *socialRepository) GetSession(ctx context.Context, orgID uuid.UUID, id string) (*models.SocialConnectSession, error) {
	return scanSocialSession(r.db.QueryRow(ctx, `SELECT `+socialSessionColumns+` FROM social_connect_sessions
		WHERE organization_id = $1 AND id = $2`, orgID, id))
}

func (r *socialRepository) UpdateSession(ctx context.Context, orgID uuid.UUID, id, status string, accountGraphKitID *string, accountID *uuid.UUID) error {
	return r.exec(ctx, `UPDATE social_connect_sessions SET status = $3,
		account_graphkit_id = COALESCE($4, account_graphkit_id), account_id = COALESCE($5, account_id), updated_at = now()
		WHERE organization_id = $1 AND id = $2`, orgID, id, status, accountGraphKitID, accountID)
}

func (r *socialRepository) SetSessionMethod(ctx context.Context, orgID uuid.UUID, id, method string) error {
	return r.exec(ctx, `UPDATE social_connect_sessions SET method = $3, updated_at = now() WHERE organization_id = $1 AND id = $2`, orgID, id, method)
}

func (r *socialRepository) MarkSessionRefunded(ctx context.Context, orgID uuid.UUID, id string) (bool, error) {
	n, err := r.affected(ctx, `UPDATE social_connect_sessions SET refunded_at = now(), updated_at = now()
		WHERE organization_id = $1 AND id = $2 AND refunded_at IS NULL`, orgID, id)
	return n > 0, err
}

func (r *socialRepository) OpenSessions(ctx context.Context, before time.Time, limit int) ([]models.SocialConnectSession, error) {
	query := `SELECT ` + socialSessionColumns + ` FROM social_connect_sessions
		WHERE (status IN ('sign_in', 'checkpoint') OR (charged_micros > 0 AND refunded_at IS NULL AND account_id IS NULL))
		AND updated_at < $1 ORDER BY updated_at LIMIT $2`
	rows, err := r.db.Query(ctx, query, before, limit)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	var out []models.SocialConnectSession
	for rows.Next() {
		s, err := scanSocialSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

// ---- conversations --------------------------------------------------------

const socialConversationColumns = `id, organization_id, social_account_id, channel, graphkit_id, contact_id, participant, kind,
	unread_count, last_message_at, last_message_preview, created_at, updated_at`

func scanSocialConversation(row pgx.Row) (*models.SocialConversation, error) {
	var c models.SocialConversation
	err := row.Scan(&c.ID, &c.OrganizationID, &c.AccountID, &c.Channel, &c.GraphKitID, &c.ContactID, &c.Participant, &c.Kind,
		&c.UnreadCount, &c.LastMessageAt, &c.LastMessagePreview, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *socialRepository) UpsertConversation(ctx context.Context, c *models.SocialConversation) (*models.SocialConversation, error) {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	if len(c.Participant) == 0 {
		c.Participant = json.RawMessage(`{}`)
	}
	return scanSocialConversation(r.db.QueryRow(ctx, `INSERT INTO social_conversations (id, organization_id, social_account_id,
			graphkit_id, contact_id, participant, kind, unread_count, last_message_at, last_message_preview, channel)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (graphkit_id) DO UPDATE SET participant = EXCLUDED.participant, kind = EXCLUDED.kind,
			unread_count = EXCLUDED.unread_count, contact_id = COALESCE(EXCLUDED.contact_id, social_conversations.contact_id),
			last_message_at = GREATEST(social_conversations.last_message_at, EXCLUDED.last_message_at),
			last_message_preview = CASE WHEN EXCLUDED.last_message_at IS NOT NULL AND (social_conversations.last_message_at IS NULL
				OR EXCLUDED.last_message_at >= social_conversations.last_message_at)
				THEN EXCLUDED.last_message_preview ELSE social_conversations.last_message_preview END,
			updated_at = now()
		WHERE social_conversations.organization_id = EXCLUDED.organization_id
		RETURNING `+socialConversationColumns,
		c.ID, c.OrganizationID, c.AccountID, c.GraphKitID, c.ContactID, c.Participant, c.Kind, c.UnreadCount, c.LastMessageAt, c.LastMessagePreview, c.Channel))
}

func (r *socialRepository) GetConversation(ctx context.Context, orgID, id uuid.UUID) (*models.SocialConversation, error) {
	return scanSocialConversation(r.db.QueryRow(ctx, `SELECT `+socialConversationColumns+` FROM social_conversations
		WHERE organization_id = $1 AND id = $2`, orgID, id))
}

func (r *socialRepository) ListConversations(ctx context.Context, orgID uuid.UUID, f SocialConversationFilter) ([]models.SocialConversation, error) {
	query := `SELECT ` + socialConversationColumns + ` FROM social_conversations c
		WHERE organization_id = $1 AND ($2::uuid IS NULL OR social_account_id = $2) AND ($3::uuid IS NULL OR contact_id = $3)
		AND (NOT $4 OR unread_count > 0) AND ($8 = '' OR c.channel = $8)
		AND ($5::uuid IS NULL OR (COALESCE(last_message_at, '-infinity'::timestamptz), id) < (COALESCE($6::timestamptz, '-infinity'::timestamptz), $5::uuid))
		AND EXISTS (SELECT 1 FROM social_accounts a WHERE a.id = c.social_account_id AND a.organization_id = $1 AND a.removed_at IS NULL)
		ORDER BY COALESCE(last_message_at, '-infinity'::timestamptz) DESC, id DESC LIMIT $7`
	rows, err := r.db.Query(ctx, query, orgID, f.AccountID, f.ContactID, f.UnreadOnly, f.AfterID, f.AfterAt, f.Limit, f.Channel)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	out := []models.SocialConversation{}
	for rows.Next() {
		c, err := scanSocialConversation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (r *socialRepository) MarkConversationRead(ctx context.Context, orgID, id uuid.UUID) error {
	return r.exec(ctx, `UPDATE social_conversations SET unread_count = 0, updated_at = now()
		WHERE organization_id = $1 AND id = $2`, orgID, id)
}

const socialMessageColumns = `id, organization_id, conversation_id, graphkit_id, direction, body_sealed, sent_at, status, kind,
	social_action_id, sent_by_user_id, created_at`

func (r *socialRepository) UpsertMessage(ctx context.Context, m *models.SocialMessage) (bool, error) {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	var inserted bool
	err := r.db.QueryRow(ctx, `INSERT INTO social_messages (id, organization_id, conversation_id, graphkit_id, direction,
			body_sealed, sent_at, status, kind, social_action_id, sent_by_user_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (graphkit_id) DO UPDATE SET status = EXCLUDED.status, sent_at = COALESCE(EXCLUDED.sent_at, social_messages.sent_at),
			sent_by_user_id = COALESCE(social_messages.sent_by_user_id, EXCLUDED.sent_by_user_id)
		WHERE social_messages.organization_id = EXCLUDED.organization_id
		RETURNING (xmax = 0)`,
		m.ID, m.OrganizationID, m.ConversationID, m.GraphKitID, m.Direction, m.BodySealed, m.SentAt, m.Status, m.Kind,
		m.ActionID, m.SentByUserID).Scan(&inserted)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return inserted, err
}

func (r *socialRepository) ListMessages(ctx context.Context, orgID, conversationID uuid.UUID, after *models.StorePage, limit int) ([]models.SocialMessage, error) {
	at, id := keyset(after)
	query := `SELECT ` + socialMessageColumns + ` FROM social_messages
		WHERE organization_id = $1 AND conversation_id = $2
		AND ($3::timestamptz IS NULL OR (COALESCE(sent_at, created_at), id) < ($3, $4::uuid))
		ORDER BY COALESCE(sent_at, created_at) DESC, id DESC LIMIT $5`
	rows, err := r.db.Query(ctx, query, orgID, conversationID, at, id, limit)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	out := []models.SocialMessage{}
	for rows.Next() {
		var m models.SocialMessage
		if err := rows.Scan(&m.ID, &m.OrganizationID, &m.ConversationID, &m.GraphKitID, &m.Direction, &m.BodySealed, &m.SentAt,
			&m.Status, &m.Kind, &m.ActionID, &m.SentByUserID, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *socialRepository) LatestMessageAt(ctx context.Context, orgID uuid.UUID, accountID uuid.UUID) (*time.Time, error) {
	var t *time.Time
	err := r.db.QueryRow(ctx, `SELECT max(last_message_at) FROM social_conversations
		WHERE organization_id = $1 AND social_account_id = $2`, orgID, accountID).Scan(&t)
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

func (r *socialRepository) ListSavedSearches(ctx context.Context, orgID uuid.UUID, after *models.StorePage, limit int) ([]models.LinkedInSavedSearch, error) {
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

func (r *socialRepository) GetSavedSearch(ctx context.Context, orgID, id uuid.UUID) (*models.LinkedInSavedSearch, error) {
	return scanLinkedInSaved(r.db.QueryRow(ctx, `SELECT `+linkedInSavedColumns+` FROM linkedin_saved_searches
		WHERE organization_id = $1 AND id = $2`, orgID, id))
}

func (r *socialRepository) CreateSavedSearch(ctx context.Context, s *models.LinkedInSavedSearch) error {
	return r.db.QueryRow(ctx, `INSERT INTO linkedin_saved_searches (id, organization_id, name, query, created_by)
		VALUES ($1, $2, $3, $4, $5) RETURNING created_at, updated_at`, s.ID, s.OrganizationID, s.Name, s.Query, s.CreatedBy).
		Scan(&s.CreatedAt, &s.UpdatedAt)
}

func (r *socialRepository) UpdateSavedSearch(ctx context.Context, orgID, id uuid.UUID, name *string, query json.RawMessage) (*models.LinkedInSavedSearch, error) {
	var q any
	if len(query) > 0 {
		q = query
	}
	return scanLinkedInSaved(r.db.QueryRow(ctx, `UPDATE linkedin_saved_searches SET name = COALESCE($3, name),
		query = COALESCE($4::jsonb, query), updated_at = now() WHERE organization_id = $1 AND id = $2
		RETURNING `+linkedInSavedColumns, orgID, id, name, q))
}

func (r *socialRepository) DeleteSavedSearch(ctx context.Context, orgID, id uuid.UUID) (bool, error) {
	n, err := r.affected(ctx, `DELETE FROM linkedin_saved_searches WHERE organization_id = $1 AND id = $2`, orgID, id)
	return n > 0, err
}

// ---- contacts -------------------------------------------------------------

func (r *socialRepository) ContactMatches(ctx context.Context, orgID uuid.UUID, publicIDs []string) (map[string]models.LinkedInContactMatch, error) {
	out := map[string]models.LinkedInContactMatch{}
	if len(publicIDs) == 0 {
		return out, nil
	}
	query := `SELECT DISTINCT ON (si.normalized) si.normalized, c.id, c.company,
			COALESCE((SELECT jsonb_agg(jsonb_build_object('id', ca.id, 'name', ca.name, 'status', ca.status) ORDER BY ca.name)
				FROM campaign_leads cl JOIN campaigns ca ON ca.id = cl.campaign_id AND ca.organization_id = $1
				WHERE cl.contact_id = c.id), '[]'::jsonb),
			EXISTS (SELECT 1 FROM campaign_contact_progress p JOIN campaigns ca ON ca.id = p.campaign_id AND ca.organization_id = $1
				WHERE p.contact_id = c.id AND p.sent_at IS NOT NULL),
			EXISTS (SELECT 1 FROM campaign_contact_progress p JOIN campaigns ca ON ca.id = p.campaign_id AND ca.organization_id = $1
				WHERE p.contact_id = c.id AND p.replied_at IS NOT NULL)
		FROM contact_social_identities si JOIN contacts c ON c.id = si.contact_id AND c.organization_id = $1
		WHERE si.organization_id = $1 AND si.channel = 'linkedin' AND si.normalized = ANY($2)
		ORDER BY si.normalized, c.created_at`
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
		m.Campaigns = []models.SocialCampaignRef{}
		_ = json.Unmarshal(camps, &m.Campaigns)
		out[m.PublicID] = m
	}
	return out, rows.Err()
}

func (r *socialRepository) CompanyContacts(ctx context.Context, orgID uuid.UUID, companies []string) (map[string]int, error) {
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

func (r *socialRepository) ConversationStates(ctx context.Context, orgID uuid.UUID, publicIDs []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(publicIDs) == 0 {
		return out, nil
	}
	query := `SELECT lower(c.participant->>'public_id'),
			bool_or(EXISTS (SELECT 1 FROM social_messages m WHERE m.conversation_id = c.id AND m.organization_id = $1 AND m.direction = 'inbound'))
		FROM social_conversations c WHERE c.organization_id = $1 AND c.channel = 'linkedin'
			AND lower(c.participant->>'public_id') = ANY($2) GROUP BY 1`
	lower := make([]string, len(publicIDs))
	for i, id := range publicIDs {
		lower[i] = strings.ToLower(id)
	}
	rows, err := r.db.Query(ctx, query, orgID, lower)
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

func (r *socialRepository) ContactEmails(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]string, error) {
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

func (r *socialRepository) BackfillContactSocial(ctx context.Context, after uuid.UUID, limit int) (uuid.UUID, int, error) {
	query := `WITH page AS (SELECT id FROM contacts WHERE id > $1 ORDER BY id LIMIT $2)
		SELECT page.id, c.organization_id, c.custom_fields->>'linkedin_url'
		FROM page JOIN contacts c ON c.id = page.id ORDER BY page.id`
	rows, err := r.db.Query(ctx, query, after, limit)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return after, 0, err
	}
	type seed struct {
		contact, org uuid.UUID
		handle       string
	}
	var seeds []seed
	last, n := after, 0
	for rows.Next() {
		var id uuid.UUID
		var org *uuid.UUID
		var raw *string
		if err := rows.Scan(&id, &org, &raw); err != nil {
			rows.Close()
			return after, 0, err
		}
		last, n = id, n+1
		if org == nil || raw == nil {
			continue
		}
		if id := models.LinkedInPublicID(*raw); id != "" {
			seeds = append(seeds, seed{contact: last, org: *org, handle: id})
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return after, 0, err
	}
	// An identity already written, by hand or by an import, is the contact's own and stays.
	for _, s := range seeds {
		if err := r.exec(ctx, `INSERT INTO contact_social_identities (contact_id, channel, organization_id, handle, normalized)
			VALUES ($1, 'linkedin', $2, $3, $3) ON CONFLICT (contact_id, channel) DO NOTHING`, s.contact, s.org, s.handle); err != nil {
			return after, 0, err
		}
	}
	return last, n, nil
}

func (r *socialRepository) BackfillProgress(ctx context.Context) (uuid.UUID, bool, error) {
	var raw []byte
	err := r.db.QueryRow(ctx, `SELECT value FROM admin_settings WHERE key = $1`, SocialContactBackfillKey).Scan(&raw)
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

func (r *socialRepository) SaveBackfillProgress(ctx context.Context, after uuid.UUID, done bool) error {
	raw, _ := json.Marshal(map[string]any{"after": after, "done": done})
	return r.exec(ctx, `INSERT INTO admin_settings (key, value, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`, SocialContactBackfillKey, raw)
}
