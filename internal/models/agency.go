package models

import (
	"time"

	"github.com/google/uuid"
)

// The seeded agency plans (migration 000377).
var (
	AgencyPlanID    = uuid.MustParse("00000000-0000-0000-0000-0000000000a1")
	AgencyProPlanID = uuid.MustParse("00000000-0000-0000-0000-0000000000a2")
	// FreeTrialPlanID is the plan_id a client subscription row starts on (migration 000080).
	FreeTrialPlanID = uuid.MustParse("00000000-0000-0000-0000-000000000001")
)

const (
	// AgencyTopupSends is one top-up pack.
	AgencyTopupSends = 50_000
	// AgencyGrantGrace keeps client entitlements across a late renewal webhook.
	AgencyGrantGrace = 3 * 24 * time.Hour
	// AgencyGrantReason is the managed_reason written on every agency grant.
	AgencyGrantReason = "Billed through an agency"
)

// AgencyTopupPriceCents is what one pack costs on the agency's plan.
func AgencyTopupPriceCents(planID *uuid.UUID) int64 {
	if planID != nil && *planID == AgencyPlanID {
		return 8_500
	}
	return 6_000
}

// AgencyRole is a staff member's role in the agency.
type AgencyRole string

const (
	AgencyRoleOwner  AgencyRole = "owner"
	AgencyRoleAdmin  AgencyRole = "admin"
	AgencyRoleMember AgencyRole = "member"
)

func (r AgencyRole) Valid() bool {
	switch r {
	case AgencyRoleOwner, AgencyRoleAdmin, AgencyRoleMember:
		return true
	}
	return false
}

// CanManage reports a role that may change clients, staff and billing.
func (r AgencyRole) CanManage() bool {
	return r == AgencyRoleOwner || r == AgencyRoleAdmin
}

// WorkspaceRole is the seeded role staff hold in every client workspace.
func (r AgencyRole) WorkspaceRole() Role {
	if r.CanManage() {
		return RoleAdmin
	}
	return RoleManager
}

// WorkspaceRoleName is the seeded role's name in a client workspace.
func (r AgencyRole) WorkspaceRoleName() string {
	if r.CanManage() {
		return "Admin"
	}
	return "Manager"
}

type Agency struct {
	ID                   uuid.UUID          `json:"id"`
	Name                 string             `json:"name"`
	OwnerUserID          uuid.UUID          `json:"owner_user_id"`
	PlanID               *uuid.UUID         `json:"plan_id,omitempty"`
	StripeCustomerID     *string            `json:"-"`
	StripeSubscriptionID *string            `json:"-"`
	Status               SubscriptionStatus `json:"status"`
	CurrentPeriodStart   *time.Time         `json:"current_period_start,omitempty"`
	CurrentPeriodEnd     *time.Time         `json:"current_period_end,omitempty"`
	CancelAtPeriodEnd    bool               `json:"cancel_at_period_end"`
	PoolAlertLevel       int                `json:"-"`
	PoolAlertPeriod      *time.Time         `json:"-"`
	CreatedAt            time.Time          `json:"created_at"`
	UpdatedAt            time.Time          `json:"updated_at"`

	// Joined: the plan, and the caller's role when listed for a user.
	Plan        *Plan      `json:"plan,omitempty"`
	Role        AgencyRole `json:"role,omitempty"`
	ClientCount int        `json:"client_count"`
	// HasBilling reports a Stripe customer, so the dashboard can offer the portal.
	HasBilling bool `json:"has_billing"`

	// A plan an operator gave the agency without Stripe; see Granted.
	ManagedPlanID *uuid.UUID `json:"-"`
	ManagedAt     *time.Time `json:"-"`
	ManagedBy     *uuid.UUID `json:"-"`
	ManagedReason *string    `json:"-"`
	ManagedUntil  *time.Time `json:"-"`
	// What an operator set for this agency in place of its plan's pool and AI allowance.
	PoolOverride        *int       `json:"-"`
	AIAllowanceOverride *Micro     `json:"-"`
	OverrideReason      *string    `json:"-"`
	OverrideBy          *uuid.UUID `json:"-"`
	OverrideAt          *time.Time `json:"-"`
	// Grant is set while an operator's plan is in force, for the dashboard.
	Grant *AgencyGrant `json:"granted,omitempty"`
	// BillingInterval is "month" or "year" while a subscription is in force, read off its period.
	BillingInterval string `json:"billing_interval,omitempty"`
}

// SubscriptionInterval is the agency's own billing period: a paid period longer than two months is a yearly one.
func (a *Agency) SubscriptionInterval() string {
	if a == nil || !a.Status.IsActive() || a.CurrentPeriodStart == nil || a.CurrentPeriodEnd == nil {
		return ""
	}
	if a.CurrentPeriodEnd.Sub(*a.CurrentPeriodStart) > 62*24*time.Hour {
		return "year"
	}
	return "month"
}

// AgencyGrant is the operator's plan an agency holds now; a nil Until has no end.
type AgencyGrant struct {
	PlanID uuid.UUID  `json:"plan_id"`
	Until  *time.Time `json:"until,omitempty"`
}

// Granted reports an operator's plan in force.
func (a *Agency) Granted(now time.Time) bool {
	return a != nil && a.ManagedAt != nil && a.ManagedPlanID != nil && (a.ManagedUntil == nil || a.ManagedUntil.After(now))
}

// EffectivePlanID is the plan the agency's pool, allowance and prices read: the operator's while in force, else the subscription's.
func (a *Agency) EffectivePlanID(now time.Time) *uuid.UUID {
	if a.Granted(now) {
		return a.ManagedPlanID
	}
	return a.PlanID
}

// AgencyMember is one staff member, joined with the user's name.
type AgencyMember struct {
	AgencyID  uuid.UUID  `json:"agency_id"`
	UserID    uuid.UUID  `json:"user_id"`
	Role      AgencyRole `json:"role"`
	CreatedAt time.Time  `json:"created_at"`
	Email     string     `json:"email"`
	FirstName string     `json:"first_name"`
	LastName  string     `json:"last_name"`
}

// AgencyClient is one client workspace, joined with the workspace's name.
type AgencyClient struct {
	OrganizationID   uuid.UUID `json:"organization_id"`
	AgencyID         uuid.UUID `json:"agency_id"`
	MonthlySendLimit *int      `json:"monthly_send_limit,omitempty"`
	MonthlyAILimit   *Micro    `json:"-"`
	// MonthlyAILimitUSD caps the client's spend from the agency's AI balance per period.
	MonthlyAILimitUSD *float64   `json:"monthly_ai_limit_usd,omitempty"`
	AddedAt           time.Time  `json:"added_at"`
	AddedBy           *uuid.UUID `json:"added_by,omitempty"`
	Name              string     `json:"name"`
	Slug              *string    `json:"slug,omitempty"`
	AvatarURL         *string    `json:"avatar_url,omitempty"`
	OwnerUserID       uuid.UUID  `json:"owner_user_id"`
}

type AgencyPoolTopup struct {
	ID                    uuid.UUID `json:"id"`
	AgencyID              uuid.UUID `json:"agency_id"`
	Sends                 int       `json:"sends"`
	AmountCents           int64     `json:"amount_cents"`
	StripePaymentIntentID *string   `json:"-"`
	PeriodStart           time.Time `json:"period_start"`
	CreatedAt             time.Time `json:"created_at"`
	// GrantedBy and Reason are set on sends an operator added, which have no payment.
	GrantedBy *uuid.UUID `json:"-"`
	Reason    *string    `json:"-"`
}

// AgencyRef names the agency a workspace belongs to.
type AgencyRef struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	// CanLeave is false when the workspace's owner also owns the agency, who detaches it from the agency instead.
	CanLeave bool `json:"can_leave"`
}

// IsAgencyPlan reports a plan sold to agencies rather than to workspaces.
func (p *Plan) IsAgencyPlan() bool {
	return p != nil && p.MonthlySendPool != nil
}

// AgencyEntitlement is the plan an agency's clients hold now and when that
// ends: an operator's plan while in force (a nil end never lapses), else the
// subscription's until its period end plus the grace.
func AgencyEntitlement(a *Agency, now time.Time) (uuid.UUID, *time.Time, bool) {
	if a.Granted(now) {
		return *a.ManagedPlanID, a.ManagedUntil, true
	}
	if a == nil || a.PlanID == nil || a.CurrentPeriodEnd == nil {
		return uuid.Nil, nil, false
	}
	switch a.Status {
	case SubscriptionStatusActive, SubscriptionStatusTrialing, SubscriptionStatusPastDue:
		if until := a.CurrentPeriodEnd.Add(AgencyGrantGrace); until.After(now) {
			return *a.PlanID, &until, true
		}
	}
	return uuid.Nil, nil, false
}

// AgencyPeriod is the pool's current period: the month of the billing period
// that covers now (a yearly plan's pool still renews monthly), rolled forward
// by months when a renewal is late, else the calendar month in UTC.
func AgencyPeriod(start, end *time.Time, now time.Time) (time.Time, time.Time) {
	if start != nil && end != nil && end.After(*start) && !now.Before(*start) {
		for i := 0; i < 240; i++ {
			s := addMonthsClamped(*start, i)
			if !s.Before(*end) {
				break
			}
			e := addMonthsClamped(*start, i+1)
			if e.After(*end) {
				e = *end
			}
			if now.Before(e) {
				return s, e
			}
		}
		for i := 0; i < 120; i++ {
			s, e := addMonthsClamped(*end, i), addMonthsClamped(*end, i+1)
			if !now.Before(s) && now.Before(e) {
				return s, e
			}
		}
	}
	now = now.UTC()
	s := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	return s, s.AddDate(0, 1, 0)
}

// AgencyPoolDay is one day of pool use: its date, and the instant it starts.
type AgencyPoolDay struct {
	Day   time.Time
	Start time.Time
}

// AgencyPoolDays are the pool days for a period starting at periodStart: they
// start at its time of day, so a period is a whole number of them.
type AgencyPoolDays struct{ offset time.Duration }

func NewAgencyPoolDays(periodStart time.Time) AgencyPoolDays {
	return AgencyPoolDays{offset: periodStart.Sub(periodStart.Truncate(24 * time.Hour))}
}

// At is the pool day holding t.
func (d AgencyPoolDays) At(t time.Time) AgencyPoolDay {
	day := t.Add(-d.offset).UTC().Truncate(24 * time.Hour)
	return AgencyPoolDay{Day: day, Start: day.Add(d.offset)}
}

// Recent is the pool day holding now and the one before it, which the rollup refreshes.
func (d AgencyPoolDays) Recent(now time.Time) []AgencyPoolDay {
	today := d.At(now)
	return []AgencyPoolDay{d.At(today.Start.Add(-time.Nanosecond)), today}
}

// addMonthsClamped adds n months, holding the day to the target month's last day as Stripe does.
func addMonthsClamped(t time.Time, n int) time.Time {
	y, m, d := t.Date()
	first := time.Date(y, m+time.Month(n), 1, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
	last := first.AddDate(0, 1, -1).Day()
	return first.AddDate(0, 0, min(d, last)-1)
}

// AgencyPool is an agency's shared monthly send pool in one period, as seen
// by one client workspace when ClientUsed and ClientLimit are set.
type AgencyPool struct {
	Unlimited   bool
	Size        int64
	Topups      int64
	Used        int64
	ClientUsed  int64
	ClientLimit *int64
}

// Total is the period's pool including top-ups.
func (p AgencyPool) Total() int64 { return p.Size + p.Topups }

// Remaining is what the whole agency may still send this period.
func (p AgencyPool) Remaining() int64 {
	return max(0, p.Total()-p.Used)
}

// Low reports a capped pool with less than percent of it left.
func (p AgencyPool) Low(percent int64) bool {
	return !p.Unlimited && p.Total() > 0 && p.Remaining()*100 < p.Total()*percent
}

// Allowance is what this client may still send: the pool's remainder, held
// to the client's own monthly limit when it has one.
func (p AgencyPool) Allowance() int64 {
	left := p.Remaining()
	if p.ClientLimit != nil {
		left = min(left, max(0, *p.ClientLimit-p.ClientUsed))
	}
	return left
}

// DailyLimit is the campaign cap the sender enforces today for a client of
// this pool: what it already sent today plus what is left, or -1 unlimited.
func (p AgencyPool) DailyLimit(sentToday int) int {
	if p.Unlimited {
		if p.ClientLimit == nil {
			return -1
		}
		return sentToday + int(max(0, *p.ClientLimit-p.ClientUsed))
	}
	return sentToday + int(p.Allowance())
}

// PoolAlertLevel is the highest threshold (80 or 100 percent) use has reached.
func PoolAlertLevel(used, total int64) int {
	if total <= 0 {
		return 0
	}
	switch {
	case used >= total:
		return 100
	case used*100 >= total*80:
		return 80
	}
	return 0
}

// ProjectPoolUse extrapolates this period's use to its end at the pace so far.
func ProjectPoolUse(used int64, start, end, now time.Time) int64 {
	total := end.Sub(start)
	elapsed := now.Sub(start)
	if total <= 0 || elapsed <= 0 {
		return used
	}
	if elapsed >= total {
		return used
	}
	// At least one day of pace, so an hour-old period does not project wildly.
	elapsed = max(elapsed, 24*time.Hour)
	return int64(float64(used) * float64(total) / float64(elapsed))
}

// AgencyUsage is the pool meter the dashboard draws.
type AgencyUsage struct {
	Unlimited   bool                `json:"unlimited"`
	PeriodStart time.Time           `json:"period_start"`
	PeriodEnd   time.Time           `json:"period_end"`
	DaysLeft    int                 `json:"days_left"`
	PoolSize    int64               `json:"pool_size"`
	Topups      int64               `json:"topups"`
	Total       int64               `json:"total"`
	Used        int64               `json:"used"`
	Remaining   int64               `json:"remaining"`
	Projected   int64               `json:"projected"`
	Clients     []AgencyClientUsage `json:"clients"`
	TopupSends  int                 `json:"topup_sends"`
	TopupCents  int64               `json:"topup_price_cents"`
}

type AgencyClientUsage struct {
	OrganizationID   uuid.UUID `json:"organization_id"`
	Name             string    `json:"name"`
	Used             int64     `json:"used"`
	MonthlySendLimit *int      `json:"monthly_send_limit,omitempty"`
}

// AdminAgency is one row of the admin panel's agency list.
type AdminAgency struct {
	Agency
	OwnerEmail string `json:"owner_email"`
	PoolUsed   int64  `json:"pool_used"`
	PoolTotal  int64  `json:"pool_total"`
	Unlimited  bool   `json:"unlimited"`
	// Entitled reports a plan in force, from Stripe or an operator.
	Entitled bool `json:"entitled"`
	// ManagedPlan is the operator's plan as recorded, lapsed ones included.
	ManagedPlan *AdminAgencyManagedPlan `json:"managed_plan,omitempty"`
	Overrides   *AdminAgencyOverrides   `json:"overrides,omitempty"`
}

// AdminAgencyManagedPlan is an operator's plan on an agency, for the admin panel.
type AdminAgencyManagedPlan struct {
	PlanID    uuid.UUID  `json:"plan_id"`
	PlanName  string     `json:"plan_name"`
	Reason    string     `json:"reason"`
	Until     *time.Time `json:"until,omitempty"`
	GrantedAt time.Time  `json:"granted_at"`
	GrantedBy *uuid.UUID `json:"granted_by,omitempty"`
	Active    bool       `json:"active"`
}

// AdminAgencyOverrides is what an operator set for one agency in place of its plan's values.
type AdminAgencyOverrides struct {
	MonthlySendPool *int       `json:"monthly_send_pool,omitempty"`
	AIAllowanceUSD  *float64   `json:"ai_allowance_usd,omitempty"`
	Reason          string     `json:"reason"`
	UpdatedAt       *time.Time `json:"updated_at,omitempty"`
	UpdatedBy       *uuid.UUID `json:"updated_by,omitempty"`
}

const (
	// AgencyPoolOverrideMax bounds a pool an operator sets for one agency.
	AgencyPoolOverrideMax = 100_000_000
	// AgencyPoolCreditMax bounds the sends an operator adds to one period at once.
	AgencyPoolCreditMax = 10_000_000
	// AgencyAIOperatorMax bounds an AI allowance or credit an operator sets, in micro-dollars.
	AgencyAIOperatorMax Micro = 100_000 * MicroPerUSD
)

// AgencyInvitation is staff invited by an address no account uses yet.
type AgencyInvitation struct {
	ID        uuid.UUID  `json:"id"`
	AgencyID  uuid.UUID  `json:"agency_id"`
	Email     string     `json:"email"`
	Role      AgencyRole `json:"role"`
	InvitedBy *uuid.UUID `json:"invited_by,omitempty"`
	ExpiresAt time.Time  `json:"expires_at"`
	CreatedAt time.Time  `json:"created_at"`
}

// AgencyInvitationTTL is how long a staff invitation waits for its account.
const AgencyInvitationTTL = 14 * 24 * time.Hour

// AgencyAIPeriod is the period the shared AI balance is in and what its
// allowance refills to at the period's start.
type AgencyAIPeriod struct {
	Start     time.Time
	End       time.Time
	Allowance Micro
}

// AgencyAIPeriodOf is the agency's AI balance period: the send pool's period,
// refilled only while a plan is in force and billed, to the operator's
// allowance for this agency when one is set, else the plan's.
func AgencyAIPeriodOf(a *Agency, planAllowance *Micro, billed bool, now time.Time) AgencyAIPeriod {
	start, end := AgencyPeriod(a.CurrentPeriodStart, a.CurrentPeriodEnd, now)
	p := AgencyAIPeriod{Start: start, End: end}
	if a.AIAllowanceOverride != nil {
		planAllowance = a.AIAllowanceOverride
	}
	if _, _, ok := AgencyEntitlement(a, now); billed && ok && planAllowance != nil {
		p.Allowance = *planAllowance
	}
	return p
}

// AgencyAILedger is the agency's shared AI balance as stored: what is left of
// the allowance for PeriodStart, and bought money, which never expires.
type AgencyAILedger struct {
	AgencyID       uuid.UUID
	Allowance      Micro
	Purchased      Micro
	TotalPurchased Micro
	PeriodStart    *time.Time
}

// In is the ledger as of period p: a stored period older than p has refilled.
func (l AgencyAILedger) In(p AgencyAIPeriod) AgencyAILedger {
	if l.PeriodStart == nil || l.PeriodStart.Before(p.Start) {
		start := p.Start
		l.Allowance, l.PeriodStart = p.Allowance, &start
	}
	return l
}

// AgencyAIClientPeriod is one client's use of the agency balance in one period.
type AgencyAIClientPeriod struct {
	Spent Micro
	Moved Micro
}

// AgencyAIBalance is the shared AI balance the agency usage page draws.
type AgencyAIBalance struct {
	// Unmetered is a self-host, where AI is not charged and the balance does not apply.
	Unmetered        bool              `json:"unmetered"`
	PeriodStart      time.Time         `json:"period_start"`
	PeriodEnd        time.Time         `json:"period_end"`
	AllowanceUSD     float64           `json:"allowance_usd"`
	AllowanceLeftUSD float64           `json:"allowance_left_usd"`
	PurchasedUSD     float64           `json:"purchased_usd"`
	BoughtUSD        float64           `json:"bought_this_period_usd"`
	SpentUSD         float64           `json:"spent_usd"`
	MovedUSD         float64           `json:"moved_usd"`
	BalanceUSD       float64           `json:"balance_usd"`
	Clients          []AgencyAIClient  `json:"clients"`
	TopUp            AgencyAITopUpInfo `json:"top_up"`
}

// AgencyAIClient is one client's share of the agency balance this period.
type AgencyAIClient struct {
	OrganizationID    uuid.UUID `json:"organization_id"`
	Name              string    `json:"name"`
	SpentUSD          float64   `json:"spent_usd"`
	MovedUSD          float64   `json:"moved_usd"`
	MonthlyAILimitUSD *float64  `json:"monthly_ai_limit_usd,omitempty"`
}

// AgencyAITopUpInfo is the range a purchase of agency balance may take.
type AgencyAITopUpInfo struct {
	MinUSD     float64 `json:"min_usd"`
	MaxUSD     float64 `json:"max_usd"`
	PresetsUSD []int   `json:"presets_usd"`
	DefaultUSD int     `json:"default_usd"`
}

// AgencyAITransfer is a move of agency balance into a client workspace's own balance.
type AgencyAITransfer struct {
	Amount           Micro
	AgencyBalance    Micro
	WorkspaceBalance Micro
	Replayed         bool
}
