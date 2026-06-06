package models

import (
	"time"

	"github.com/google/uuid"
)

type Duration string

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
	ID              uuid.UUID `json:"id"`
	Name            *string   `json:"name,omitempty"`
	MaxContacts     uint      `json:"max_contacts"`
	DailyEmails     uint      `json:"daily_emails"`
	AIGeneration    bool      `json:"ai_generation"`
	AccountLimit    uint      `json:"account_limit"`
	Price           float32   `json:"price"`
	DiscountedPrice float32   `json:"discounted_price"`
	Duration        Duration  `json:"duration"`
	Savings         uint8     `json:"savings"`
	Public          bool      `json:"public"`

	// Stripe integration
	StripePriceID       *string `json:"stripe_price_id,omitempty"`
	StripePriceIDYearly *string `json:"stripe_price_id_yearly,omitempty"`
	StripeProductID     *string `json:"stripe_product_id,omitempty"`

	// Worker tier settings
	DedicatedWorkers   int  `json:"dedicated_workers"`
	DailyCampaignLimit *int `json:"daily_campaign_limit,omitempty"`

	// Organization limits
	MaxCampaigns       *int `json:"max_campaigns,omitempty"`
	MaxActiveCampaigns *int `json:"max_active_campaigns,omitempty"`
	MaxTeamMembers     *int `json:"max_team_members,omitempty"`
	MaxEmailAccounts   *int `json:"max_email_accounts,omitempty"`

	// AI writing-assistant monthly credit grant for this plan.
	MonthlyCredits int `json:"monthly_credits"`

	UpdatedAt time.Time `json:"updated_at"`
	CreatedAt time.Time `json:"created_at"`
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

	// Stripe trial info
	TrialStart *time.Time `json:"trial_start,omitempty"`
	TrialEnd   *time.Time `json:"trial_end,omitempty"`

	// Free trial info (internal, not Stripe)
	FreeTrialStartedAt *time.Time `json:"free_trial_started_at,omitempty"`
	FreeTrialEndsAt    *time.Time `json:"free_trial_ends_at,omitempty"`

	// Enterprise flag
	IsEnterprise bool `json:"is_enterprise"`

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
	return s.StripeSubscriptionID != nil && s.Status.IsActive()
}

// CanSendEmails returns true if user can send campaign emails
func (s *Subscription) CanSendEmails() bool {
	// Active paid subscription
	if s.HasPaidSubscription() {
		return true
	}
	// In free trial
	if s.IsInFreeTrial() {
		return true
	}
	return false
}

// CanUseWarmup returns true if user can use warmup feature.
// Free-trial orgs get warmup access during their 14-day window so they can
// try the product; once the trial expires they need a paid subscription.
func (s *Subscription) CanUseWarmup() bool {
	return s.HasPaidSubscription() || s.IsInFreeTrial()
}

// CanUseUnibox returns true if user can use unibox feature.
// Same trial allowance as warmup so a free-trial user can interact with
// their connected mailbox while evaluating Warmbly.
func (s *Subscription) CanUseUnibox() bool {
	return s.HasPaidSubscription() || s.IsInFreeTrial()
}

// FreeTrialInboxLimit caps how many email accounts a free-trial org may
// connect. Paid orgs have no inbox-count cap (their plan governs sending,
// not connections). Free-trial users get up to two inboxes so they can
// evaluate the unified inbox across more than one mailbox without seeding
// the warmup pool with throwaway accounts.
const FreeTrialInboxLimit = 2

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
