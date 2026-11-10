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

// ErrAgencyClientTaken is a workspace that already belongs to an agency.
var ErrAgencyClientTaken = errors.New("workspace already belongs to an agency")

// AgencyBilling is the Stripe state an agency subscription event writes.
type AgencyBilling struct {
	PlanID               *uuid.UUID
	StripeCustomerID     *string
	StripeSubscriptionID *string
	Status               models.SubscriptionStatus
	CurrentPeriodStart   *time.Time
	CurrentPeriodEnd     *time.Time
	CancelAtPeriodEnd    bool
}

type AgencyRepository interface {
	// GetAgencyForOrganization is the agency a workspace is a client of; nil, nil when none.
	GetAgencyForOrganization(ctx context.Context, orgID uuid.UUID) (*models.Agency, error)
	ListClientOrganizations(ctx context.Context, agencyID uuid.UUID) ([]models.AgencyClient, error)
	// GetMember is one staff member; nil, nil when the user is not staff.
	GetMember(ctx context.Context, agencyID, userID uuid.UUID) (*models.AgencyMember, error)
	// ListAgenciesForUser lists the agencies the user is staff of, with their role.
	ListAgenciesForUser(ctx context.Context, userID uuid.UUID) ([]models.Agency, error)

	Create(ctx context.Context, a *models.Agency) error
	Get(ctx context.Context, agencyID uuid.UUID) (*models.Agency, error)
	GetByStripeSubscriptionID(ctx context.Context, subID string) (*models.Agency, error)
	Rename(ctx context.Context, agencyID uuid.UUID, name string) error
	SetStripeCustomer(ctx context.Context, agencyID uuid.UUID, customerID string) error
	UpdateBilling(ctx context.Context, agencyID uuid.UUID, b AgencyBilling) error
	// ListPage is agencies after the cursor in id order, for resumable sweeps.
	ListPage(ctx context.Context, after uuid.UUID, limit int) ([]models.Agency, error)
	ListForAdmin(ctx context.Context, search string, limit, offset int) ([]models.AdminAgency, int, error)
	// SetManagedPlan records an operator's plan on the agency; a nil until has no end.
	SetManagedPlan(ctx context.Context, agencyID, planID, by uuid.UUID, reason string, until *time.Time) error
	ClearManagedPlan(ctx context.Context, agencyID uuid.UUID) error
	// SetOverrides replaces the pool and AI allowance an operator set for the agency; nil is the plan's own.
	SetOverrides(ctx context.Context, agencyID uuid.UUID, pool *int, aiAllowance *models.Micro, by uuid.UUID, reason string) error

	ListMembers(ctx context.Context, agencyID uuid.UUID) ([]models.AgencyMember, error)
	UpsertMember(ctx context.Context, agencyID, userID uuid.UUID, role models.AgencyRole) error
	RemoveMember(ctx context.Context, agencyID, userID uuid.UUID) error
	UpsertInvitation(ctx context.Context, inv *models.AgencyInvitation) error
	ListInvitations(ctx context.Context, agencyID uuid.UUID) ([]models.AgencyInvitation, error)
	DeleteInvitation(ctx context.Context, agencyID, invitationID uuid.UUID) (bool, error)
	// ClaimInvitations turns live invitations to the address into staff and returns the agencies joined.
	ClaimInvitations(ctx context.Context, userID uuid.UUID, email string) ([]uuid.UUID, error)

	GetClient(ctx context.Context, orgID uuid.UUID) (*models.AgencyClient, error)
	AddClient(ctx context.Context, agencyID, orgID uuid.UUID, addedBy uuid.UUID) error
	RemoveClient(ctx context.Context, agencyID, orgID uuid.UUID) (bool, error)
	SetClientLimit(ctx context.Context, agencyID, orgID uuid.UUID, limit *int) (bool, error)

	// RollupPoolUsage raises each current client's row for the given pool days
	// to its live campaign send count since it joined; a row never goes down.
	RollupPoolUsage(ctx context.Context, agencyID uuid.UUID, days []models.AgencyPoolDay) error
	// PoolUsage sums rolled-up sends per workspace over pool days [from, to),
	// including workspaces no longer attached.
	PoolUsage(ctx context.Context, agencyID uuid.UUID, from, to time.Time) (map[uuid.UUID]int64, error)
	// SentTodayByClient is CountEmailsSentTodayByOrganization for every current client.
	SentTodayByClient(ctx context.Context, agencyID uuid.UUID) (map[uuid.UUID]int, error)
	// TopupSends sums the period's packs that were not refunded.
	TopupSends(ctx context.Context, agencyID uuid.UUID, periodStart time.Time) (int64, error)
	// RecordTopup is idempotent on the payment intent; false when already recorded.
	RecordTopup(ctx context.Context, t *models.AgencyPoolTopup) (bool, error)
	// RefundTopup marks the pack paid by the payment intent refunded and
	// returns its agency; false when no pack was paid by it.
	RefundTopup(ctx context.Context, paymentIntentID string) (uuid.UUID, bool, error)
	// ClaimPoolAlert raises the period's alert level; false when it was already that high.
	ClaimPoolAlert(ctx context.Context, agencyID uuid.UUID, periodStart time.Time, level int) (bool, error)

	// SyncClientGrant writes the agency's managed grant onto the workspace's
	// subscription, creating the row when the workspace has none. An operator
	// grant in force is left alone. A nil until has no end.
	SyncClientGrant(ctx context.Context, agencyID, orgID, planID uuid.UUID, until *time.Time) error
	// ClearClientGrant removes a grant only this agency wrote.
	ClearClientGrant(ctx context.Context, agencyID, orgID uuid.UUID) error
	// ClearStaleGrants removes this agency's grants from workspaces no longer its clients, returning those workspaces.
	ClearStaleGrants(ctx context.Context, agencyID uuid.UUID) ([]uuid.UUID, error)

	// MirrorStaff gives a staff member the mapped role in a client workspace.
	// A membership the agency did not add is never changed.
	MirrorStaff(ctx context.Context, agencyID, orgID, userID uuid.UUID, role models.AgencyRole, invitedBy uuid.UUID) error
	// UnmirrorStaff removes only a membership this agency added; true when one was removed.
	UnmirrorStaff(ctx context.Context, agencyID, orgID, userID uuid.UUID) (bool, error)
	// AgencyMembersOf lists users holding an agency-added membership in the workspace.
	AgencyMembersOf(ctx context.Context, agencyID, orgID uuid.UUID) ([]uuid.UUID, error)
}

type agencyRepository struct {
	db *pgxpool.Pool
}

func NewAgencyRepository(db *pgxpool.Pool) AgencyRepository {
	return &agencyRepository{db: db}
}

// agencyGrantLive is models.Agency.Granted for agencies a, read at the database's clock.
const agencyGrantLive = `(a.managed_at IS NOT NULL AND a.managed_plan_id IS NOT NULL AND (a.managed_until IS NULL OR a.managed_until > now()))`

const agencyColumns = `a.id, a.name, a.owner_user_id, a.plan_id, a.stripe_customer_id, a.stripe_subscription_id, a.status,
	a.current_period_start, a.current_period_end, a.cancel_at_period_end, a.pool_alert_level, a.pool_alert_period,
	a.created_at, a.updated_at,
	(SELECT COUNT(*) FROM agency_clients ac WHERE ac.agency_id = a.id),
	a.managed_plan_id, a.managed_at, a.managed_by, a.managed_reason, a.managed_until,
	a.pool_override, a.ai_allowance_override_micro, a.override_reason, a.override_by, a.override_at`

func scanAgency(row pgx.Row, extra ...any) (*models.Agency, error) {
	var a models.Agency
	dest := []any{&a.ID, &a.Name, &a.OwnerUserID, &a.PlanID, &a.StripeCustomerID, &a.StripeSubscriptionID, &a.Status,
		&a.CurrentPeriodStart, &a.CurrentPeriodEnd, &a.CancelAtPeriodEnd, &a.PoolAlertLevel, &a.PoolAlertPeriod,
		&a.CreatedAt, &a.UpdatedAt, &a.ClientCount,
		&a.ManagedPlanID, &a.ManagedAt, &a.ManagedBy, &a.ManagedReason, &a.ManagedUntil,
		&a.PoolOverride, &a.AIAllowanceOverride, &a.OverrideReason, &a.OverrideBy, &a.OverrideAt}
	if err := row.Scan(append(dest, extra...)...); err != nil {
		return nil, err
	}
	a.HasBilling = a.StripeCustomerID != nil && *a.StripeCustomerID != ""
	return &a, nil
}

func (r *agencyRepository) one(ctx context.Context, query string, args ...any) (*models.Agency, error) {
	a, err := scanAgency(r.db.QueryRow(ctx, query, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return a, err
}

func (r *agencyRepository) GetAgencyForOrganization(ctx context.Context, orgID uuid.UUID) (*models.Agency, error) {
	return r.one(ctx, `SELECT `+agencyColumns+` FROM agencies a JOIN agency_clients c ON c.agency_id = a.id WHERE c.organization_id = $1`, orgID)
}

func (r *agencyRepository) Get(ctx context.Context, agencyID uuid.UUID) (*models.Agency, error) {
	return r.one(ctx, `SELECT `+agencyColumns+` FROM agencies a WHERE a.id = $1`, agencyID)
}

func (r *agencyRepository) GetByStripeSubscriptionID(ctx context.Context, subID string) (*models.Agency, error) {
	return r.one(ctx, `SELECT `+agencyColumns+` FROM agencies a WHERE a.stripe_subscription_id = $1`, subID)
}

func (r *agencyRepository) ListAgenciesForUser(ctx context.Context, userID uuid.UUID) ([]models.Agency, error) {
	rows, err := r.db.Query(ctx, `SELECT `+agencyColumns+`, m.role
		FROM agencies a JOIN agency_members m ON m.agency_id = a.id
		WHERE m.user_id = $1 ORDER BY a.created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Agency
	for rows.Next() {
		var role string
		a, err := scanAgency(rows, &role)
		if err != nil {
			return nil, err
		}
		a.Role = models.AgencyRole(role)
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (r *agencyRepository) ListPage(ctx context.Context, after uuid.UUID, limit int) ([]models.Agency, error) {
	rows, err := r.db.Query(ctx, `SELECT `+agencyColumns+` FROM agencies a WHERE a.id > $1 ORDER BY a.id LIMIT $2`, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Agency
	for rows.Next() {
		a, err := scanAgency(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (r *agencyRepository) ListForAdmin(ctx context.Context, search string, limit, offset int) ([]models.AdminAgency, int, error) {
	var total int
	if err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM agencies a JOIN users u ON u.id = a.owner_user_id
		WHERE $1 = '' OR a.name ILIKE '%' || $1 || '%' OR u.email ILIKE '%' || $1 || '%'`, search).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.Query(ctx, `SELECT `+agencyColumns+`, u.email
		FROM agencies a JOIN users u ON u.id = a.owner_user_id
		WHERE $1 = '' OR a.name ILIKE '%' || $1 || '%' OR u.email ILIKE '%' || $1 || '%'
		ORDER BY a.created_at DESC LIMIT $2 OFFSET $3`, search, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []models.AdminAgency
	for rows.Next() {
		var email string
		a, err := scanAgency(rows, &email)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, models.AdminAgency{Agency: *a, OwnerEmail: email})
	}
	return out, total, rows.Err()
}

func (r *agencyRepository) Create(ctx context.Context, a *models.Agency) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := tx.QueryRow(ctx, `INSERT INTO agencies (id, name, owner_user_id, status) VALUES ($1, $2, $3, $4)
		RETURNING created_at, updated_at`, a.ID, a.Name, a.OwnerUserID, a.Status).Scan(&a.CreatedAt, &a.UpdatedAt); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO agency_members (agency_id, user_id, role) VALUES ($1, $2, 'owner')`, a.ID, a.OwnerUserID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *agencyRepository) Rename(ctx context.Context, agencyID uuid.UUID, name string) error {
	_, err := r.db.Exec(ctx, `UPDATE agencies SET name = $2, updated_at = now() WHERE id = $1`, agencyID, name)
	return err
}

func (r *agencyRepository) SetManagedPlan(ctx context.Context, agencyID, planID, by uuid.UUID, reason string, until *time.Time) error {
	_, err := r.db.Exec(ctx, `UPDATE agencies SET managed_plan_id = $2, managed_at = now(), managed_by = $3, managed_reason = $4,
			managed_until = $5, updated_at = now() WHERE id = $1`, agencyID, planID, by, reason, until)
	return err
}

func (r *agencyRepository) ClearManagedPlan(ctx context.Context, agencyID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `UPDATE agencies SET managed_plan_id = NULL, managed_at = NULL, managed_by = NULL, managed_reason = NULL,
			managed_until = NULL, updated_at = now() WHERE id = $1`, agencyID)
	return err
}

func (r *agencyRepository) SetOverrides(ctx context.Context, agencyID uuid.UUID, pool *int, aiAllowance *models.Micro, by uuid.UUID, reason string) error {
	_, err := r.db.Exec(ctx, `UPDATE agencies SET pool_override = $2, ai_allowance_override_micro = $3, override_reason = $4,
			override_by = $5, override_at = now(), updated_at = now() WHERE id = $1`, agencyID, pool, aiAllowance, reason, by)
	return err
}

func (r *agencyRepository) SetStripeCustomer(ctx context.Context, agencyID uuid.UUID, customerID string) error {
	_, err := r.db.Exec(ctx, `UPDATE agencies SET stripe_customer_id = $2, updated_at = now() WHERE id = $1`, agencyID, customerID)
	return err
}

func (r *agencyRepository) UpdateBilling(ctx context.Context, agencyID uuid.UUID, b AgencyBilling) error {
	_, err := r.db.Exec(ctx, `UPDATE agencies SET
			plan_id = COALESCE($2, plan_id),
			stripe_customer_id = COALESCE($3, stripe_customer_id),
			stripe_subscription_id = COALESCE($4, stripe_subscription_id),
			status = $5,
			current_period_start = COALESCE($6, current_period_start),
			current_period_end = COALESCE($7, current_period_end),
			cancel_at_period_end = $8,
			updated_at = now()
		WHERE id = $1`,
		agencyID, b.PlanID, b.StripeCustomerID, b.StripeSubscriptionID, string(b.Status),
		b.CurrentPeriodStart, b.CurrentPeriodEnd, b.CancelAtPeriodEnd)
	return err
}

const agencyMemberColumns = `m.agency_id, m.user_id, m.role, m.created_at, u.email, u.first_name, u.last_name`

func scanAgencyMember(row pgx.Row) (*models.AgencyMember, error) {
	var m models.AgencyMember
	var role string
	if err := row.Scan(&m.AgencyID, &m.UserID, &role, &m.CreatedAt, &m.Email, &m.FirstName, &m.LastName); err != nil {
		return nil, err
	}
	m.Role = models.AgencyRole(role)
	return &m, nil
}

func (r *agencyRepository) GetMember(ctx context.Context, agencyID, userID uuid.UUID) (*models.AgencyMember, error) {
	m, err := scanAgencyMember(r.db.QueryRow(ctx, `SELECT `+agencyMemberColumns+`
		FROM agency_members m JOIN users u ON u.id = m.user_id WHERE m.agency_id = $1 AND m.user_id = $2`, agencyID, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return m, err
}

func (r *agencyRepository) ListMembers(ctx context.Context, agencyID uuid.UUID) ([]models.AgencyMember, error) {
	rows, err := r.db.Query(ctx, `SELECT `+agencyMemberColumns+`
		FROM agency_members m JOIN users u ON u.id = m.user_id WHERE m.agency_id = $1
		ORDER BY CASE m.role WHEN 'owner' THEN 0 WHEN 'admin' THEN 1 ELSE 2 END, m.created_at`, agencyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.AgencyMember
	for rows.Next() {
		m, err := scanAgencyMember(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

func (r *agencyRepository) UpsertMember(ctx context.Context, agencyID, userID uuid.UUID, role models.AgencyRole) error {
	_, err := r.db.Exec(ctx, `INSERT INTO agency_members (agency_id, user_id, role) VALUES ($1, $2, $3)
		ON CONFLICT (agency_id, user_id) DO UPDATE SET role = EXCLUDED.role WHERE agency_members.role <> 'owner'`,
		agencyID, userID, string(role))
	return err
}

func (r *agencyRepository) RemoveMember(ctx context.Context, agencyID, userID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM agency_members WHERE agency_id = $1 AND user_id = $2 AND role <> 'owner'`, agencyID, userID)
	return err
}

const agencyClientColumns = `c.organization_id, c.agency_id, c.monthly_send_limit, c.monthly_ai_limit_micro, c.added_at, c.added_by, o.name, o.slug, o.avatar_url, o.owner_user_id`

func scanAgencyClient(row pgx.Row) (*models.AgencyClient, error) {
	var c models.AgencyClient
	if err := row.Scan(&c.OrganizationID, &c.AgencyID, &c.MonthlySendLimit, &c.MonthlyAILimit, &c.AddedAt, &c.AddedBy, &c.Name, &c.Slug, &c.AvatarURL, &c.OwnerUserID); err != nil {
		return nil, err
	}
	if c.MonthlyAILimit != nil {
		v := c.MonthlyAILimit.USD()
		c.MonthlyAILimitUSD = &v
	}
	return &c, nil
}

func (r *agencyRepository) ListClientOrganizations(ctx context.Context, agencyID uuid.UUID) ([]models.AgencyClient, error) {
	rows, err := r.db.Query(ctx, `SELECT `+agencyClientColumns+`
		FROM agency_clients c JOIN organizations o ON o.id = c.organization_id
		WHERE c.agency_id = $1 ORDER BY lower(o.name), o.id`, agencyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.AgencyClient
	for rows.Next() {
		c, err := scanAgencyClient(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (r *agencyRepository) GetClient(ctx context.Context, orgID uuid.UUID) (*models.AgencyClient, error) {
	c, err := scanAgencyClient(r.db.QueryRow(ctx, `SELECT `+agencyClientColumns+`
		FROM agency_clients c JOIN organizations o ON o.id = c.organization_id WHERE c.organization_id = $1`, orgID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return c, err
}

func (r *agencyRepository) AddClient(ctx context.Context, agencyID, orgID uuid.UUID, addedBy uuid.UUID) error {
	tag, err := r.db.Exec(ctx, `INSERT INTO agency_clients (organization_id, agency_id, added_by) VALUES ($1, $2, $3)
		ON CONFLICT (organization_id) DO NOTHING`, orgID, agencyID, addedBy)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var current uuid.UUID
		if err := r.db.QueryRow(ctx, `SELECT agency_id FROM agency_clients WHERE organization_id = $1`, orgID).Scan(&current); err != nil {
			return err
		}
		if current != agencyID {
			return ErrAgencyClientTaken
		}
	}
	return nil
}

func (r *agencyRepository) RemoveClient(ctx context.Context, agencyID, orgID uuid.UUID) (bool, error) {
	tag, err := r.db.Exec(ctx, `DELETE FROM agency_clients WHERE agency_id = $1 AND organization_id = $2`, agencyID, orgID)
	return tag.RowsAffected() == 1, err
}

func (r *agencyRepository) SetClientLimit(ctx context.Context, agencyID, orgID uuid.UUID, limit *int) (bool, error) {
	tag, err := r.db.Exec(ctx, `UPDATE agency_clients SET monthly_send_limit = $3 WHERE agency_id = $1 AND organization_id = $2`, agencyID, orgID, limit)
	return tag.RowsAffected() == 1, err
}

// rollupPoolUsageSQL counts each client's campaign sends per pool day from
// when it joined, through idx_campaign_progress_sent, and only ever raises a row.
const rollupPoolUsageSQL = `
	INSERT INTO agency_pool_usage_days (agency_id, organization_id, day, sends, updated_at)
	SELECT ac.agency_id, ac.organization_id, d.day, n.sends, now()
	FROM agency_clients ac
	CROSS JOIN unnest($2::date[], $3::timestamptz[]) AS d(day, starts)
	CROSS JOIN LATERAL (
		SELECT COUNT(*)::int AS sends
		FROM campaigns c
		JOIN campaign_contact_progress ccp ON ccp.campaign_id = c.id
		JOIN sequences s ON s.id = ccp.sequence_id AND s.kind = 'email'
		WHERE c.organization_id = ac.organization_id
		  AND ccp.sent_at IS NOT NULL
		  AND ccp.sent_at >= GREATEST(d.starts, ac.added_at)
		  AND ccp.sent_at < d.starts + interval '1 day'
	) n
	WHERE ac.agency_id = $1 AND n.sends > 0
	ON CONFLICT (agency_id, organization_id, day) DO UPDATE
		SET sends = EXCLUDED.sends, updated_at = now()
		WHERE agency_pool_usage_days.sends < EXCLUDED.sends`

func (r *agencyRepository) RollupPoolUsage(ctx context.Context, agencyID uuid.UUID, days []models.AgencyPoolDay) error {
	if len(days) == 0 {
		return nil
	}
	dates := make([]time.Time, len(days))
	starts := make([]time.Time, len(days))
	for i, d := range days {
		dates[i], starts[i] = d.Day, d.Start
	}
	_, err := r.db.Exec(ctx, rollupPoolUsageSQL, agencyID, dates, starts)
	return err
}

func (r *agencyRepository) PoolUsage(ctx context.Context, agencyID uuid.UUID, from, to time.Time) (map[uuid.UUID]int64, error) {
	rows, err := r.db.Query(ctx, `
		SELECT organization_id, SUM(sends)::bigint
		FROM agency_pool_usage_days
		WHERE agency_id = $1 AND day >= $2::date AND day < $3::date
		GROUP BY organization_id`, agencyID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]int64{}
	for rows.Next() {
		var id uuid.UUID
		var n int64
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

func (r *agencyRepository) SentTodayByClient(ctx context.Context, agencyID uuid.UUID) (map[uuid.UUID]int, error) {
	rows, err := r.db.Query(ctx, `
		SELECT c.organization_id, COUNT(*)::int
		FROM agency_clients ac
		JOIN campaigns c ON c.organization_id = ac.organization_id
		JOIN campaign_contact_progress ccp ON ccp.campaign_id = c.id
		JOIN sequences s ON s.id = ccp.sequence_id AND s.kind = 'email'
		WHERE ac.agency_id = $1
		  AND ccp.sent_at >= CURRENT_DATE::timestamptz
		  AND ccp.sent_at < (CURRENT_DATE + 1)::timestamptz
		GROUP BY c.organization_id`, agencyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]int{}
	for rows.Next() {
		var id uuid.UUID
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

func (r *agencyRepository) TopupSends(ctx context.Context, agencyID uuid.UUID, periodStart time.Time) (int64, error) {
	var n int64
	err := r.db.QueryRow(ctx, `SELECT COALESCE(SUM(sends), 0) FROM agency_pool_topups
		WHERE agency_id = $1 AND period_start = $2 AND refunded_at IS NULL`, agencyID, periodStart).Scan(&n)
	return n, err
}

func (r *agencyRepository) RefundTopup(ctx context.Context, paymentIntentID string) (uuid.UUID, bool, error) {
	var agencyID uuid.UUID
	err := r.db.QueryRow(ctx, `UPDATE agency_pool_topups SET refunded_at = COALESCE(refunded_at, now())
		WHERE stripe_payment_intent_id = $1 RETURNING agency_id`, paymentIntentID).Scan(&agencyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	return agencyID, err == nil, err
}

func (r *agencyRepository) RecordTopup(ctx context.Context, t *models.AgencyPoolTopup) (bool, error) {
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	tag, err := r.db.Exec(ctx, `INSERT INTO agency_pool_topups (id, agency_id, sends, amount_cents, stripe_payment_intent_id, period_start, granted_by, reason)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) ON CONFLICT (stripe_payment_intent_id) DO NOTHING`,
		t.ID, t.AgencyID, t.Sends, t.AmountCents, t.StripePaymentIntentID, t.PeriodStart, t.GrantedBy, t.Reason)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (r *agencyRepository) ClaimPoolAlert(ctx context.Context, agencyID uuid.UUID, periodStart time.Time, level int) (bool, error) {
	tag, err := r.db.Exec(ctx, `UPDATE agencies SET pool_alert_level = $3, pool_alert_period = $2
		WHERE id = $1 AND (pool_alert_period IS DISTINCT FROM $2 OR pool_alert_level < $3)`, agencyID, periodStart, level)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (r *agencyRepository) SyncClientGrant(ctx context.Context, agencyID, orgID, planID uuid.UUID, until *time.Time) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO subscriptions (user_id, organization_id, plan_id, stripe_customer_id, status,
			managed_at, managed_reason, managed_until, managed_plan_id, managed_by_agency_id)
		SELECT o.owner_user_id, o.id, $5, '', 'incomplete', now(), $6, $4, $3, $1
		FROM organizations o WHERE o.id = $2
		ON CONFLICT (organization_id) DO UPDATE SET
			managed_at = CASE WHEN subscriptions.managed_by_agency_id = $1 THEN subscriptions.managed_at ELSE now() END,
			managed_by = NULL,
			managed_reason = $6,
			managed_until = $4,
			managed_plan_id = $3,
			managed_by_agency_id = $1,
			updated_at = now()
		WHERE subscriptions.managed_by_agency_id = $1
		   OR subscriptions.managed_at IS NULL
		   OR (subscriptions.managed_until IS NOT NULL AND subscriptions.managed_until <= now())`,
		agencyID, orgID, planID, until, models.FreeTrialPlanID, models.AgencyGrantReason)
	return err
}

func (r *agencyRepository) ClearClientGrant(ctx context.Context, agencyID, orgID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `UPDATE subscriptions SET managed_at = NULL, managed_by = NULL, managed_reason = NULL,
			managed_until = NULL, managed_plan_id = NULL, managed_by_agency_id = NULL, updated_at = now()
		WHERE organization_id = $2 AND managed_by_agency_id = $1`, agencyID, orgID)
	return err
}

func (r *agencyRepository) ClearStaleGrants(ctx context.Context, agencyID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.db.Query(ctx, `UPDATE subscriptions s SET managed_at = NULL, managed_by = NULL, managed_reason = NULL,
			managed_until = NULL, managed_plan_id = NULL, managed_by_agency_id = NULL, updated_at = now()
		WHERE s.managed_by_agency_id = $1
		  AND NOT EXISTS (SELECT 1 FROM agency_clients ac WHERE ac.organization_id = s.organization_id AND ac.agency_id = $1)
		RETURNING s.organization_id`, agencyID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// mirrorInSyncSQL is true when the membership needs no write: absent rows,
// rows the agency did not add and owner rows are decided by the caller.
const mirrorInSyncSQL = `
	SELECT om.via_agency_id, om.role,
		COALESCE(CASE WHEN $3::uuid IS NOT NULL THEN
			om.role_id = $3
			AND om.permissions = (SELECT r.permissions FROM organization_roles r WHERE r.id = $3)
			AND om.role = (SELECT r.name FROM organization_roles r WHERE r.id = $3)
			AND (SELECT array_agg(mr.role_id) FROM organization_member_roles mr
				WHERE mr.organization_id = om.organization_id AND mr.user_id = om.user_id) = ARRAY[$3::uuid]
		ELSE
			om.role = $4 AND om.role_id IS NULL AND om.permissions = $5
			AND NOT EXISTS (SELECT 1 FROM organization_member_roles mr
				WHERE mr.organization_id = om.organization_id AND mr.user_id = om.user_id)
		END, false)
	FROM organization_members om WHERE om.organization_id = $1 AND om.user_id = $2`

func (r *agencyRepository) MirrorStaff(ctx context.Context, agencyID, orgID, userID uuid.UUID, role models.AgencyRole, invitedBy uuid.UUID) error {
	var roleID *uuid.UUID
	var rid uuid.UUID
	if err := r.db.QueryRow(ctx, `SELECT id FROM organization_roles WHERE organization_id = $1 AND name = $2 ORDER BY created_at LIMIT 1`,
		orgID, role.WorkspaceRoleName()).Scan(&rid); err == nil {
		roleID = &rid
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	wsRole := role.WorkspaceRole()

	// Read without a lock first, so a sweep over memberships already in place writes nothing.
	var via *uuid.UUID
	var current string
	var inSync bool
	err := r.db.QueryRow(ctx, mirrorInSyncSQL, orgID, userID, roleID, string(wsRole), models.RolePermissions[wsRole]).Scan(&via, &current, &inSync)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err == nil && (inSync || via == nil || *via != agencyID || current == string(models.RoleOwner)) {
		return nil
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	err = tx.QueryRow(ctx, `SELECT via_agency_id, role FROM organization_members WHERE organization_id = $1 AND user_id = $2 FOR UPDATE`, orgID, userID).Scan(&via, &current)
	exists := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	// An owner row is never re-roled, even one that began as an agency membership.
	if exists && (via == nil || *via != agencyID || current == string(models.RoleOwner)) {
		return tx.Commit(ctx)
	}

	if !exists {
		now := time.Now()
		if _, err := tx.Exec(ctx, `INSERT INTO organization_members (id, organization_id, user_id, role, role_id, permissions,
				invited_by, invited_at, accepted_at, via_agency_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8, $9)`,
			uuid.New(), orgID, userID, string(wsRole), roleID, models.RolePermissions[wsRole], invitedBy, now, agencyID); err != nil {
			return err
		}
	} else if _, err := tx.Exec(ctx, `DELETE FROM organization_member_roles WHERE organization_id = $1 AND user_id = $2`, orgID, userID); err != nil {
		return err
	}

	if roleID != nil {
		if _, err := tx.Exec(ctx, `INSERT INTO organization_member_roles (organization_id, user_id, role_id) VALUES ($1, $2, $3)
			ON CONFLICT DO NOTHING`, orgID, userID, *roleID); err != nil {
			return err
		}
		if err := recomputeMemberPermissions(ctx, tx, orgID, userID); err != nil {
			return err
		}
	} else if _, err := tx.Exec(ctx, `UPDATE organization_members SET role = $3, role_id = NULL, permissions = $4
		WHERE organization_id = $1 AND user_id = $2`, orgID, userID, string(wsRole), models.RolePermissions[wsRole]); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *agencyRepository) UnmirrorStaff(ctx context.Context, agencyID, orgID, userID uuid.UUID) (bool, error) {
	tag, err := r.db.Exec(ctx, `DELETE FROM organization_members
		WHERE organization_id = $1 AND user_id = $2 AND via_agency_id = $3 AND role <> 'owner'`, orgID, userID, agencyID)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	_, err = r.db.Exec(ctx, `DELETE FROM organization_member_roles WHERE organization_id = $1 AND user_id = $2`, orgID, userID)
	return true, err
}

func (r *agencyRepository) AgencyMembersOf(ctx context.Context, agencyID, orgID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.db.Query(ctx, `SELECT user_id FROM organization_members WHERE organization_id = $1 AND via_agency_id = $2`, orgID, agencyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (r *agencyRepository) UpsertInvitation(ctx context.Context, inv *models.AgencyInvitation) error {
	return r.db.QueryRow(ctx, `INSERT INTO agency_invitations (id, agency_id, email, role, invited_by, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (agency_id, lower(email)) DO UPDATE SET role = EXCLUDED.role, invited_by = EXCLUDED.invited_by, expires_at = EXCLUDED.expires_at
		RETURNING id, created_at`, inv.ID, inv.AgencyID, inv.Email, string(inv.Role), inv.InvitedBy, inv.ExpiresAt).Scan(&inv.ID, &inv.CreatedAt)
}

func (r *agencyRepository) ListInvitations(ctx context.Context, agencyID uuid.UUID) ([]models.AgencyInvitation, error) {
	rows, err := r.db.Query(ctx, `SELECT id, agency_id, email, role, invited_by, expires_at, created_at
		FROM agency_invitations WHERE agency_id = $1 AND expires_at > now() ORDER BY created_at`, agencyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.AgencyInvitation
	for rows.Next() {
		var inv models.AgencyInvitation
		var role string
		if err := rows.Scan(&inv.ID, &inv.AgencyID, &inv.Email, &role, &inv.InvitedBy, &inv.ExpiresAt, &inv.CreatedAt); err != nil {
			return nil, err
		}
		inv.Role = models.AgencyRole(role)
		out = append(out, inv)
	}
	return out, rows.Err()
}

func (r *agencyRepository) DeleteInvitation(ctx context.Context, agencyID, invitationID uuid.UUID) (bool, error) {
	tag, err := r.db.Exec(ctx, `DELETE FROM agency_invitations WHERE agency_id = $1 AND id = $2`, agencyID, invitationID)
	return tag.RowsAffected() == 1, err
}

func (r *agencyRepository) ClaimInvitations(ctx context.Context, userID uuid.UUID, email string) ([]uuid.UUID, error) {
	rows, err := r.db.Query(ctx, `
		WITH claimed AS (
			DELETE FROM agency_invitations WHERE lower(email) = lower($2) AND expires_at > now()
			RETURNING agency_id, role
		)
		INSERT INTO agency_members (agency_id, user_id, role)
		SELECT agency_id, $1, role FROM claimed
		ON CONFLICT (agency_id, user_id) DO NOTHING
		RETURNING agency_id`, userID, email)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
