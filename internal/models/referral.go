package models

import (
	"time"

	"github.com/google/uuid"
)

// ReferralAttributionStatus tracks an invitee's progress from signup to reward.
//
//	pending   -> attributed at signup, invitee hasn't paid yet
//	qualified -> invitee reached a paid checkout
//	rewarded  -> referrer credit granted on the invitee's first paid invoice
//	void      -> reversed (clawback, self-referral, or cap)
type ReferralAttributionStatus string

const (
	ReferralStatusPending   ReferralAttributionStatus = "pending"
	ReferralStatusQualified ReferralAttributionStatus = "qualified"
	ReferralStatusRewarded  ReferralAttributionStatus = "rewarded"
	ReferralStatusVoid      ReferralAttributionStatus = "void"
)

// ReferralCode is a user's canonical share code. The Code string is also a
// discount_codes row (10% off, 3 months) so the same string both attributes a
// signup and grants the invitee their discount at checkout.
type ReferralCode struct {
	ID             uuid.UUID  `json:"id"`
	OwnerUserID    uuid.UUID  `json:"owner_user_id"`
	OwnerOrgID     uuid.UUID  `json:"owner_org_id"`
	Code           string     `json:"code"`
	DiscountCodeID *uuid.UUID `json:"discount_code_id,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// ReferralAttribution links an invitee organization to the referrer who brought
// them in. There is at most one row per invitee org (the "referred once" guard).
type ReferralAttribution struct {
	ID             uuid.UUID                 `json:"id"`
	ReferralCodeID *uuid.UUID                `json:"referral_code_id,omitempty"`
	ReferrerUserID uuid.UUID                 `json:"referrer_user_id"`
	ReferrerOrgID  uuid.UUID                 `json:"referrer_org_id"`
	InviteeOrgID   uuid.UUID                 `json:"invitee_org_id"`
	InviteeUserID  *uuid.UUID                `json:"invitee_user_id,omitempty"`
	Status         ReferralAttributionStatus `json:"status"`
	RewardCents    int64                     `json:"reward_cents"`
	RewardCurrency string                    `json:"reward_currency"`
	QualifiedAt    *time.Time                `json:"qualified_at,omitempty"`
	RewardedAt     *time.Time                `json:"rewarded_at,omitempty"`
	VoidedAt       *time.Time                `json:"voided_at,omitempty"`
	VoidReason     *string                   `json:"void_reason,omitempty"`
	CreatedAt      time.Time                 `json:"created_at"`
	UpdatedAt      time.Time                 `json:"updated_at"`
}

// ReferralEarningsLedger is the referrer's dollar credit balance (cents).
// balance_cents is the net credit earned (rewards minus clawbacks);
// stripe_pushed_cents tracks how much of it has been mirrored onto the Stripe
// customer balance.
type ReferralEarningsLedger struct {
	OrgID               uuid.UUID `json:"org_id"`
	BalanceCents        int64     `json:"balance_cents"`
	LifetimeEarnedCents int64     `json:"lifetime_earned_cents"`
	StripePushedCents   int64     `json:"stripe_pushed_cents"`
	Currency            string    `json:"currency"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// ReferralEarningsTransaction is one append-only row in the earnings trail.
// AmountCents is positive for a reward and negative for a clawback.
type ReferralEarningsTransaction struct {
	ID                         uuid.UUID  `json:"id"`
	OrgID                      uuid.UUID  `json:"org_id"`
	AttributionID              *uuid.UUID `json:"attribution_id,omitempty"`
	AmountCents                int64      `json:"amount_cents"`
	Currency                   string     `json:"currency"`
	Reason                     string     `json:"reason"`
	BalanceAfterCents          int64      `json:"balance_after_cents"`
	StripeCustomerBalanceTxnID *string    `json:"stripe_customer_balance_txn_id,omitempty"`
	IdempotencyKey             *string    `json:"idempotency_key,omitempty"`
	CreatedAt                  time.Time  `json:"created_at"`
}

// ReferralSummary is the dashboard payload: the user's share code/link, their
// earnings totals, and a breakdown of where their referrals stand.
type ReferralSummary struct {
	Code              string `json:"code"`
	ShareURL          string `json:"share_url"`
	Currency          string `json:"currency"`
	InviteePercentOff int    `json:"invitee_percent_off"`
	InviteeMonths     int    `json:"invitee_months"`

	BalanceCents        int64 `json:"balance_cents"`
	LifetimeEarnedCents int64 `json:"lifetime_earned_cents"`

	TotalReferred int `json:"total_referred"`
	Pending       int `json:"pending"`
	Qualified     int `json:"qualified"`
	Rewarded      int `json:"rewarded"`
}

// ReferralAttributionsResult is a paginated list of a referrer's attributions.
type ReferralAttributionsResult struct {
	Data       []ReferralAttribution `json:"data"`
	Pagination CPagination           `json:"pagination"`
}

// ReferralEarningsResult is a paginated list of a referrer's earnings trail.
type ReferralEarningsResult struct {
	Data       []ReferralEarningsTransaction `json:"data"`
	Pagination CPagination                   `json:"pagination"`
}
