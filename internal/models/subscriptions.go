package models

import (
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/config"
)

type Duration string

var TestPlanID = uuid.MustParse("00000000-0000-0000-0000-0000000000e1")

const (
	DurationMonth Duration = "month"
	DurationYear  Duration = "year"
)

type SubscriptionStatus string

const (
	SubscriptionStatusTrialing          SubscriptionStatus = "trialing"
	SubscriptionStatusActive            SubscriptionStatus = "active"
	SubscriptionStatusPastDue           SubscriptionStatus = "past_due"
	SubscriptionStatusCanceled          SubscriptionStatus = "canceled"
	SubscriptionStatusUnpaid            SubscriptionStatus = "unpaid"
	SubscriptionStatusIncomplete        SubscriptionStatus = "incomplete"
	SubscriptionStatusIncompleteExpired SubscriptionStatus = "incomplete_expired"
	SubscriptionStatusPaused            SubscriptionStatus = "paused"
)

// IsActive returns true if the subscription allows access
func (s SubscriptionStatus) IsActive() bool {
	return s == SubscriptionStatusActive || s == SubscriptionStatusTrialing
}

type Plan struct {
	ID           uuid.UUID `json:"id"`
	Name         *string   `json:"name,omitempty"`
	MaxContacts  uint      `json:"max_contacts"`
	DailyEmails  uint      `json:"daily_emails"`
	AIGeneration bool      `json:"ai_generation"`
	AccountLimit uint      `json:"account_limit"`
	Price        float32   `json:"price"`
	// PriceYearly is what StripePriceIDYearly charges. Nil means the plan is
	// monthly only, so nothing may offer it yearly.
	PriceYearly     *float32 `json:"price_yearly,omitempty"`
	DiscountedPrice float32  `json:"discounted_price"`
	Duration        Duration `json:"duration"`
	Savings         uint8    `json:"savings"`
	Public          bool     `json:"public"`

	// Stripe integration
	StripePriceID       *string `json:"stripe_price_id,omitempty"`
	StripePriceIDYearly *string `json:"stripe_price_id_yearly,omitempty"`
	StripeProductID     *string `json:"stripe_product_id,omitempty"`

	// Worker tier settings
	// DedicatedWorkers is the isolated-egress entitlement, kept under its
	// original column name. Read it through IsolatedEgress() rather than
	// comparing it directly: the number never meant "how many machines you
	// get", only "does this plan reserve egress for you".
	DedicatedWorkers   int  `json:"dedicated_workers"`
	DailyCampaignLimit *int `json:"daily_campaign_limit,omitempty"`

	// Organization limits
	MaxCampaigns       *int `json:"max_campaigns,omitempty"`
	MaxActiveCampaigns *int `json:"max_active_campaigns,omitempty"`
	MaxTeamMembers     *int `json:"max_team_members,omitempty"`
	MaxEmailAccounts   *int `json:"max_email_accounts,omitempty"`

	// MonthlyAllowanceUSD is the usage this plan includes each month, in
	// dollars. MonthlyCredits is the same allowance in legacy credits, which
	// the previous release reads; writes derive it from the dollars.
	MonthlyAllowanceUSD float64 `json:"monthly_allowance_usd"`
	MonthlyCredits      int     `json:"monthly_credits"`

	// Referral reward: the percentage of this plan's first-month-equivalent
	// price a referrer earns when an invitee converts to it (100 = a full
	// month-equivalent). Defaults to 100 for every plan.
	ReferralRewardPercent int `json:"referral_reward_percent"`

	// MonthlySendPool marks an agency plan: the sends shared by all its
	// clients each period. Zero is an agency plan with no pool cap.
	MonthlySendPool *int `json:"monthly_send_pool,omitempty"`
	// AgencyAIAllowance is what an agency plan refills the agency's shared AI balance to each period.
	AgencyAIAllowance    *Micro   `json:"-"`
	AgencyAIAllowanceUSD *float64 `json:"agency_ai_allowance_usd,omitempty"`

	UpdatedAt time.Time `json:"updated_at"`
	CreatedAt time.Time `json:"created_at"`
}

// MonthlyPriceCents returns this plan's price normalized to a single month, in
// integer cents. Yearly plans are divided by 12 so a referral reward for an
// annual conversion is one month-equivalent (e.g. Pro Annual $990/yr -> ~$99),
// not the full annual invoice.
func (p *Plan) MonthlyPriceCents() int64 {
	price := float64(p.Price)
	if p.Duration == DurationYear {
		price = price / 12
	}
	return int64(math.Round(price * 100))
}

// Allowance is the plan's monthly included usage. A client that still sends
// only the credit figure is read at the credit's value.
func (p *Plan) Allowance() Micro {
	if p.MonthlyAllowanceUSD > 0 {
		return USDToMicro(p.MonthlyAllowanceUSD)
	}
	return Micro(max(p.MonthlyCredits, 0)) * LegacyCreditMicro
}

// ReferralRewardCents returns the credit a referrer earns for converting an
// invitee onto this plan: the month-equivalent price scaled by the plan's
// referral_reward_percent.
func (p *Plan) ReferralRewardCents() int64 {
	pct := p.ReferralRewardPercent
	if pct <= 0 {
		return 0
	}
	return int64(math.Round(float64(p.MonthlyPriceCents()) * float64(pct) / 100))
}

type OfferOption struct {
	Title string `json:"title"`
	Plan  string `json:"plan"`
}

type Offer struct {
	ID          string        `json:"id"`
	Title       string        `json:"title"`
	Description string        `json:"description"`
	Options     []OfferOption `json:"options"`

	UpdatedAt time.Time `json:"updated_at"`
	CreatedAt time.Time `json:"created_at"`
}

type Subscription struct {
	ID             uuid.UUID `json:"id"`
	UserID         uuid.UUID `json:"user_id"`
	OrganizationID uuid.UUID `json:"organization_id"`
	PlanID         uuid.UUID `json:"plan_id"`

	// Stripe identifiers
	StripeCustomerID     string  `json:"stripe_customer_id"`
	StripeSubscriptionID *string `json:"stripe_subscription_id,omitempty"`
	StripePriceID        *string `json:"stripe_price_id,omitempty"`

	// Subscription state
	Status SubscriptionStatus `json:"status"`

	// Billing period
	CurrentPeriodStart *time.Time `json:"current_period_start,omitempty"`
	CurrentPeriodEnd   *time.Time `json:"current_period_end,omitempty"`
	CancelAtPeriodEnd  bool       `json:"cancel_at_period_end"`
	CanceledAt         *time.Time `json:"canceled_at,omitempty"`

	// A plan an operator granted rather than Stripe: internal workspaces,
	// design partners, support gestures. ManagedUntil nil is open-ended.
	ManagedAt     *time.Time `json:"managed_at,omitempty"`
	ManagedBy     *uuid.UUID `json:"managed_by,omitempty"`
	ManagedReason *string    `json:"managed_reason,omitempty"`
	ManagedUntil  *time.Time `json:"managed_until,omitempty"`
	// ManagedPlanID is held beside PlanID, never on top of it, so a workspace
	// paying Stripe for one plan and granted another goes back to the one it
	// pays for when the grant ends.
	ManagedPlanID *uuid.UUID `json:"managed_plan_id,omitempty"`
	// ManagedByAgencyID is the agency whose subscription the grant mirrors.
	ManagedByAgencyID *uuid.UUID `json:"managed_by_agency_id,omitempty"`
	// Managed is IsManaged resolved server-side, carried on the base type so
	// every endpoint that returns a subscription reports it. A granted plan
	// never touches Stripe, so Status stays whatever it was ("incomplete" on a
	// workspace that never subscribed); a client deciding "paid" from Status
	// alone locks a workspace entitled to everything. Not a column: set by the
	// service, so a repository scan never has to know about it.
	Managed bool `json:"managed"`

	// Stripe trial info
	TrialStart *time.Time `json:"trial_start,omitempty"`
	TrialEnd   *time.Time `json:"trial_end,omitempty"`

	// Free trial info (internal, not Stripe)
	FreeTrialStartedAt *time.Time `json:"free_trial_started_at,omitempty"`
	FreeTrialEndsAt    *time.Time `json:"free_trial_ends_at,omitempty"`

	// Enterprise flag
	IsEnterprise bool `json:"is_enterprise"`

	// KeepsFreeWarmup marks a workspace that had the free warmup allowance before it was withdrawn from new ones.
	KeepsFreeWarmup bool `json:"keeps_free_warmup"`

	// Joined data
	Plan *Plan `json:"plan,omitempty"`

	// User info (populated by joins)
	UserEmail *string `json:"user_email,omitempty"`

	UpdatedAt time.Time `json:"updated_at"`
	CreatedAt time.Time `json:"created_at"`
}

// IsInFreeTrial returns true if the user is currently in their free trial period
func (s *Subscription) IsInFreeTrial() bool {
	if s.FreeTrialEndsAt == nil {
		return false
	}
	return time.Now().Before(*s.FreeTrialEndsAt)
}

// IsFreeTrialExpired returns true if the free trial has expired
func (s *Subscription) IsFreeTrialExpired() bool {
	if s.FreeTrialEndsAt == nil {
		return false
	}
	return time.Now().After(*s.FreeTrialEndsAt)
}

// HasPaidSubscription returns true if user has an active paid Stripe subscription
func (s *Subscription) HasPaidSubscription() bool {
	// A granted plan is paid without Stripe ever being involved. Checked
	// first so a workspace that later subscribes for real is not affected
	// either way.
	if s.IsManaged() {
		return true
	}
	return s.StripeSubscriptionID != nil && s.Status.IsActive()
}

// IsManaged reports whether an operator granted this plan and the grant is
// still in force. An expired ManagedUntil lapses on its own, so a time-boxed
// grant needs nobody to remember to revoke it.
func (s *Subscription) IsManaged() bool {
	if s == nil || s.ManagedAt == nil {
		return false
	}
	if s.ManagedUntil == nil {
		return true
	}
	return time.Now().Before(*s.ManagedUntil)
}

// EffectivePlanID is the plan that decides entitlements: the granted one while
// a grant is in force, otherwise the plan the workspace actually pays for.
// Every lookup of a subscription's plan goes through this; using PlanID
// directly silently ignores the grant.
func (s *Subscription) EffectivePlanID() uuid.UUID {
	if s == nil {
		return uuid.Nil
	}
	if s.IsManaged() && s.ManagedPlanID != nil {
		return *s.ManagedPlanID
	}
	return s.PlanID
}

// ManagedExpired separates "was granted, has lapsed" from "never granted", so
// the admin panel can show a grant that ran out instead of silently dropping
// the workspace back to free with no explanation.
func (s *Subscription) ManagedExpired() bool {
	if s == nil || s.ManagedAt == nil || s.ManagedUntil == nil {
		return false
	}
	return !time.Now().Before(*s.ManagedUntil)
}

// WarmupPlanID is the Warmup plan: the premium pool and no mailbox cap, nothing else.
var WarmupPlanID = uuid.MustParse(config.PoolLinkPlanID)

// IsWarmupPlan reports a plan that buys warming only: the Warmup plan and the Partnership plan.
func IsWarmupPlan(planID uuid.UUID) bool {
	return planID == WarmupPlanID || planID == PartnershipPlanID
}

// IsWarmupOnly reports a warming-only plan in force; a Stripe subscription to any other plan outranks a warming-only grant.
func (s *Subscription) IsWarmupOnly() bool {
	if s == nil || !s.HasPaidSubscription() || !IsWarmupPlan(s.EffectivePlanID()) {
		return false
	}
	return !(s.StripeSubscriptionID != nil && s.Status.IsActive() && !IsWarmupPlan(s.PlanID))
}

// HasProductPlan reports a paid plan that includes sending, the inbox, AI and integrations: every one but the warming-only plans.
func (s *Subscription) HasProductPlan() bool {
	return s != nil && s.HasPaidSubscription() && !s.IsWarmupOnly()
}

// CanSendEmails returns true if user can send campaign emails
func (s *Subscription) CanSendEmails() bool {
	if s.HasProductPlan() {
		return true
	}
	// In free trial
	if s.IsInFreeTrial() {
		return true
	}
	return false
}

// CanUseWarmup: a paid plan, a free trial in progress, or a workspace that
// kept the free allowance. Unpaid ones warm in the free pool.
func (s *Subscription) CanUseWarmup() bool {
	return s.HasPaidSubscription() || s.KeepsFreeWarmup || s.IsInFreeTrial()
}

// FreeMailboxLimit is the mailboxes an unsubscribed workspace may hold: the
// kept free allowance, TrialMailboxLimit during a trial, otherwise none.
func (s *Subscription) FreeMailboxLimit() int {
	switch {
	case s == nil:
		return 0
	case s.KeepsFreeWarmup:
		return FreeWorkspaceMailboxLimit
	case s.IsInFreeTrial():
		return TrialMailboxLimit
	}
	return 0
}

// CanUseUnibox returns true if user can use unibox feature.
// Same trial allowance as warmup so a free-trial user can interact with
// their connected mailbox while evaluating Warmbly.
func (s *Subscription) CanUseUnibox() bool {
	return s.HasProductPlan() || s.IsInFreeTrial()
}

// FreeWorkspaceMailboxLimit caps the mailboxes an unsubscribed workspace may
// hold, connected directly or through a linked self-hosted instance.
const FreeWorkspaceMailboxLimit = 10

// TrialMailboxLimit caps the mailboxes a workspace may hold during its free trial.
const TrialMailboxLimit = 2

// SubscriptionWithLimits includes rate limits for the subscription
type SubscriptionWithLimits struct {
	Subscription
	RateLimits *RealtimeRateLimits `json:"rate_limits,omitempty"`
}

// RealtimeRateLimits contains WebSocket-specific rate limits
type RealtimeRateLimits struct {
	LimitWSMessagePM int `json:"limit_ws_message_pm"`
	LimitWSJoinPM    int `json:"limit_ws_join_pm"`
	LimitWSEventPM   int `json:"limit_ws_event_pm"`
	MaxConnections   int `json:"max_connections"`
}

// CreateSubscriptionParams for creating a new subscription
type CreateSubscriptionParams struct {
	UserID           uuid.UUID `json:"user_id"`
	PlanID           uuid.UUID `json:"plan_id"`
	StripeCustomerID string    `json:"stripe_customer_id"`
}

// UpdateSubscriptionFromStripe updates subscription from Stripe webhook data
type UpdateSubscriptionFromStripe struct {
	StripeSubscriptionID string              `json:"stripe_subscription_id"`
	StripePriceID        *string             `json:"stripe_price_id,omitempty"`
	Status               *SubscriptionStatus `json:"status,omitempty"`
	CurrentPeriodStart   *time.Time          `json:"current_period_start,omitempty"`
	CurrentPeriodEnd     *time.Time          `json:"current_period_end,omitempty"`
	CancelAtPeriodEnd    *bool               `json:"cancel_at_period_end,omitempty"`
	CanceledAt           *time.Time          `json:"canceled_at,omitempty"`
	TrialStart           *time.Time          `json:"trial_start,omitempty"`
	TrialEnd             *time.Time          `json:"trial_end,omitempty"`
}

// StripeWebhookEvent for idempotency tracking
type StripeWebhookEvent struct {
	ID          string                 `json:"id"`
	EventType   string                 `json:"event_type"`
	ProcessedAt time.Time              `json:"processed_at"`
	Payload     map[string]interface{} `json:"payload,omitempty"`
}

// IsolatedEgress reports whether the plan reserves a worker for the
// organization, so its mailboxes always authenticate to their providers from
// an address no other tenant sends from.
//
// This is the deliverability shape of what used to be sold as a "dedicated
// worker". The customer-visible promise is about the sign-in address being
// theirs alone, which is the part that actually affects them: providers score
// login trust and apply per-IP auth throttles on that address, so an
// organization running many mailboxes benefits from not sharing it. It was
// never about the machine, and pinning them to one was the wrong shape - a
// reserved worker that dies used to strand the customer, where a preference
// simply re-converges.
func (p *Plan) IsolatedEgress() bool {
	return p != nil && p.DedicatedWorkers > 0
}

// ManagedPlan is the admin-facing view of an operator-granted plan. Managed
// and Expired are deliberately separate: a lapsed grant is not the same as a
// workspace that was never granted one, and the reason stays readable after
// it lapses.
type ManagedPlan struct {
	Managed   bool       `json:"managed"`
	Expired   bool       `json:"expired"`
	PlanID    uuid.UUID  `json:"plan_id"`
	GrantedAt *time.Time `json:"granted_at,omitempty"`
	GrantedBy *uuid.UUID `json:"granted_by,omitempty"`
	Reason    *string    `json:"reason,omitempty"`
	Until     *time.Time `json:"until,omitempty"`
}
