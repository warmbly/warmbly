package models

import (
	"time"

	"github.com/google/uuid"
)

// DiscountType is the kind of discount a code grants. Exactly one value field
// on the code is populated per type (see the DB CHECK in 000048).
type DiscountType string

const (
	DiscountTypePercent        DiscountType = "percent"
	DiscountTypeFixed          DiscountType = "fixed"
	DiscountTypeTrialExtension DiscountType = "trial_extension"
)

// DiscountDuration mirrors Stripe coupon duration for money discounts.
type DiscountDuration string

const (
	DiscountDurationOnce      DiscountDuration = "once"
	DiscountDurationRepeating DiscountDuration = "repeating"
	DiscountDurationForever   DiscountDuration = "forever"
)

type DiscountCodeStatus string

const (
	DiscountCodeStatusActive   DiscountCodeStatus = "active"
	DiscountCodeStatusDisabled DiscountCodeStatus = "disabled"
	DiscountCodeStatusExpired  DiscountCodeStatus = "expired"
)

type DiscountRedemptionStatus string

const (
	DiscountRedemptionStatusPending  DiscountRedemptionStatus = "pending"
	DiscountRedemptionStatusApplied  DiscountRedemptionStatus = "applied"
	DiscountRedemptionStatusCanceled DiscountRedemptionStatus = "canceled"
)

// IsMoney reports whether the discount reduces the charged amount (vs granting
// extra trial days).
func (t DiscountType) IsMoney() bool {
	return t == DiscountTypePercent || t == DiscountTypeFixed
}

// DiscountCode is an admin-managed promo code.
type DiscountCode struct {
	ID          uuid.UUID    `json:"id"`
	Code        string       `json:"code"`
	Description string       `json:"description"`
	Type        DiscountType `json:"type"`

	PercentOff         *int     `json:"percent_off,omitempty"`
	AmountOff          *float64 `json:"amount_off,omitempty"`
	Currency           *string  `json:"currency,omitempty"`
	TrialExtensionDays *int     `json:"trial_extension_days,omitempty"`

	Duration         DiscountDuration `json:"duration"`
	DurationInMonths *int             `json:"duration_in_months,omitempty"`

	MaxRedemptions  *int `json:"max_redemptions,omitempty"`
	TimesRedeemed   int  `json:"times_redeemed"`
	PerAccountLimit int  `json:"per_account_limit"`

	AppliesToAllPlans bool        `json:"applies_to_all_plans"`
	PlanIDs           []uuid.UUID `json:"plan_ids"`

	Status    DiscountCodeStatus `json:"status"`
	StartsAt  *time.Time         `json:"starts_at,omitempty"`
	ExpiresAt *time.Time         `json:"expires_at,omitempty"`

	CreatedBy *uuid.UUID `json:"created_by,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// DiscountRedemption records a single redemption of a code by an organization.
type DiscountRedemption struct {
	ID                      uuid.UUID  `json:"id"`
	DiscountCodeID          uuid.UUID  `json:"discount_code_id"`
	OrganizationID          uuid.UUID  `json:"organization_id"`
	RedeemedBy              *uuid.UUID `json:"redeemed_by,omitempty"`
	SubscriptionID          *uuid.UUID `json:"subscription_id,omitempty"`
	PlanID                  *uuid.UUID `json:"plan_id,omitempty"`
	StripeCouponID          *string    `json:"stripe_coupon_id,omitempty"`
	StripeCheckoutSessionID *string    `json:"stripe_checkout_session_id,omitempty"`

	Type               DiscountType `json:"type"`
	PercentOff         *int         `json:"percent_off,omitempty"`
	AmountOff          *float64     `json:"amount_off,omitempty"`
	Currency           *string      `json:"currency,omitempty"`
	TrialExtensionDays *int         `json:"trial_extension_days,omitempty"`

	Status     DiscountRedemptionStatus `json:"status"`
	RedeemedAt time.Time                `json:"redeemed_at"`
	AppliedAt  *time.Time               `json:"applied_at,omitempty"`

	// Joined (populated by admin list-by-code).
	Code string `json:"code,omitempty"`
}

// CreateDiscountCodeRequest is the admin create payload.
type CreateDiscountCodeRequest struct {
	Code               string             `json:"code" binding:"required"`
	Description        string             `json:"description"`
	Type               DiscountType       `json:"type" binding:"required"`
	PercentOff         *int               `json:"percent_off,omitempty"`
	AmountOff          *float64           `json:"amount_off,omitempty"`
	Currency           *string            `json:"currency,omitempty"`
	TrialExtensionDays *int               `json:"trial_extension_days,omitempty"`
	Duration           DiscountDuration   `json:"duration,omitempty"`
	DurationInMonths   *int               `json:"duration_in_months,omitempty"`
	MaxRedemptions     *int               `json:"max_redemptions,omitempty"`
	PerAccountLimit    *int               `json:"per_account_limit,omitempty"`
	AppliesToAllPlans  bool               `json:"applies_to_all_plans"`
	PlanIDs            []uuid.UUID        `json:"plan_ids,omitempty"`
	Status             DiscountCodeStatus `json:"status,omitempty"`
	StartsAt           *time.Time         `json:"starts_at,omitempty"`
	ExpiresAt          *time.Time         `json:"expires_at,omitempty"`
}

// UpdateDiscountCodeRequest is the admin partial-update payload. The discount
// `type` is immutable; recreate the code to change kinds.
type UpdateDiscountCodeRequest struct {
	Description        *string             `json:"description,omitempty"`
	PercentOff         *int                `json:"percent_off,omitempty"`
	AmountOff          *float64            `json:"amount_off,omitempty"`
	Currency           *string             `json:"currency,omitempty"`
	TrialExtensionDays *int                `json:"trial_extension_days,omitempty"`
	Duration           *DiscountDuration   `json:"duration,omitempty"`
	DurationInMonths   *int                `json:"duration_in_months,omitempty"`
	MaxRedemptions     *int                `json:"max_redemptions,omitempty"`
	PerAccountLimit    *int                `json:"per_account_limit,omitempty"`
	AppliesToAllPlans  *bool               `json:"applies_to_all_plans,omitempty"`
	PlanIDs            *[]uuid.UUID        `json:"plan_ids,omitempty"`
	Status             *DiscountCodeStatus `json:"status,omitempty"`
	StartsAt           *time.Time          `json:"starts_at,omitempty"`
	ExpiresAt          *time.Time          `json:"expires_at,omitempty"`
}

// AdminDiscountSearch filters the admin discount list.
type AdminDiscountSearch struct {
	Status string     `form:"status"`
	Search string     `form:"search"`
	Cursor *uuid.UUID `form:"cursor"`
	Limit  int        `form:"limit"`
}

// AdminDiscountsResult is the paginated admin discount list envelope.
type AdminDiscountsResult struct {
	Data       []DiscountCode `json:"data"`
	Pagination Pagination     `json:"pagination"`
}

// AdminDiscountRedemptionsResult is the paginated redemptions envelope.
type AdminDiscountRedemptionsResult struct {
	Data       []DiscountRedemption `json:"data"`
	Pagination Pagination           `json:"pagination"`
}

// ValidateDiscountRequest is the customer pre-checkout validation payload.
type ValidateDiscountRequest struct {
	Code   string     `json:"code" binding:"required"`
	PlanID *uuid.UUID `json:"plan_id,omitempty"`
}

// DiscountPreview is the customer-facing validation result. When invalid,
// Reason explains why; the discount fields are only populated when Valid.
type DiscountPreview struct {
	Valid  bool   `json:"valid"`
	Reason string `json:"reason,omitempty"`

	Code               string           `json:"code,omitempty"`
	Type               DiscountType     `json:"type,omitempty"`
	PercentOff         *int             `json:"percent_off,omitempty"`
	AmountOff          *float64         `json:"amount_off,omitempty"`
	Currency           *string          `json:"currency,omitempty"`
	TrialExtensionDays *int             `json:"trial_extension_days,omitempty"`
	Duration           DiscountDuration `json:"duration,omitempty"`
	DurationInMonths   *int             `json:"duration_in_months,omitempty"`

	// Plan-specific computation, only when a plan_id is supplied and the code is
	// a money discount.
	OriginalAmount   *float64 `json:"original_amount,omitempty"`
	DiscountedAmount *float64 `json:"discounted_amount,omitempty"`
	SavingsAmount    *float64 `json:"savings_amount,omitempty"`
}
