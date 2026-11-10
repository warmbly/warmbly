package models

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// CreditContext names exactly what an AI charge ran for. Every field is
// optional; only the ones that apply to the feature are set. Stored as jsonb
// on the transaction row and rendered in the billing transaction log, so a
// spend is always traceable to the campaign/step/contact, automation/node/run,
// thread, or assistant session that caused it.
type CreditContext struct {
	CampaignID     string `json:"campaign_id,omitempty"`
	CampaignName   string `json:"campaign_name,omitempty"`
	StepID         string `json:"step_id,omitempty"`
	ContactID      string `json:"contact_id,omitempty"`
	ContactEmail   string `json:"contact_email,omitempty"`
	AutomationID   string `json:"automation_id,omitempty"`
	AutomationName string `json:"automation_name,omitempty"`
	NodeID         string `json:"node_id,omitempty"`
	RunID          string `json:"run_id,omitempty"`
	ThreadID       string `json:"thread_id,omitempty"`
	SessionID      string `json:"session_id,omitempty"`
	// Detail is a short free-form note (e.g. what an Ask AI branch asked).
	Detail string `json:"detail,omitempty"`
}

// Empty reports whether nothing was attributed.
func (c CreditContext) Empty() bool {
	return c == CreditContext{}
}

// CreditMeta is the attribution attached to a charge: the user who triggered
// it (uuid.Nil for scheduled/system work) and the structured context.
type CreditMeta struct {
	ActorID uuid.UUID
	Context CreditContext
}

type creditMetaKey struct{}

// WithCreditMeta attaches charge attribution to a context. The credit
// repository reads it back when writing transaction rows, so call sites
// annotate spends without threading extra parameters through every layer —
// refunds and usage settles made with the same context inherit it too.
func WithCreditMeta(ctx context.Context, meta CreditMeta) context.Context {
	return context.WithValue(ctx, creditMetaKey{}, meta)
}

// CreditMetaFrom returns the attached attribution, or a zero value.
func CreditMetaFrom(ctx context.Context) CreditMeta {
	if meta, ok := ctx.Value(creditMetaKey{}).(CreditMeta); ok {
		return meta
	}
	return CreditMeta{}
}

// CreditLedger is the authoritative per-organization balance, one row per
// organization. Two pools: Monthly is the plan's allowance (set to the plan's
// amount every billing cycle) and Purchased is money the workspace added,
// which never expires and survives resets. Usage drains Monthly first.
// Balances move only inside row-locked repository transactions backstopped by
// CHECK constraints, so neither pool can go negative under concurrency.
type CreditLedger struct {
	OrgID          uuid.UUID
	Monthly        Micro
	Purchased      Micro
	MonthResetAt   time.Time
	TotalPurchased Micro
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Total is the spendable balance across both pools.
func (l *CreditLedger) Total() Micro {
	return l.Monthly + l.Purchased
}

// SavedCardCharge is one charge on a workspace's saved card, an automatic
// reload or a one-click top-up. Its id derives every Stripe idempotency key,
// so a retried job converges on the same charge.
type SavedCardCharge struct {
	ID              uuid.UUID
	OrgID           uuid.UUID
	Kind            string // SavedCardAutoReload | SavedCardTopUp
	Amount          Micro
	AmountCents     int64
	Currency        string
	Status          string // pending | succeeded | failed
	InvoiceID       string
	CustomerID      string
	PaymentMethodID string
	Tries           int
	RetryAt         *time.Time
	FailureCode     string
	CreatedAt       time.Time
	CompletedAt     *time.Time
	// Legacy marks a credit-pack charge the previous release started; it
	// settles through its own payment intent and is never resumed.
	Legacy bool
}

const (
	SavedCardAutoReload = "auto_reload"
	SavedCardTopUp      = "top_up"
)

// Auto reload failure codes, stored on the settings row when a reload turns
// itself off.
const (
	ReloadCardDeclined           = "card_declined"
	ReloadAuthenticationRequired = "authentication_required"
	ReloadNoPaymentMethod        = "no_payment_method"
	ReloadPaymentFailed          = "payment_failed"
	ReloadUnavailable            = "unavailable"
)

// Failure codes only a charge row carries, never the settings row.
const (
	// ChargeCanceled closes an automatic reload no longer wanted: reload off or the balance back over the threshold.
	ChargeCanceled = "canceled"
	// ChargeExpired closes a credit-pack charge the previous release left pending.
	ChargeExpired = "expired"
)

// AISpendSettings is an org's spend-control row: optional spend limits per
// calendar day, ISO week and calendar month for the workspace and for each
// member (nil = no limit), the low-balance notice threshold, and automatic
// reload (add Amount when the balance falls below Threshold, at most
// MonthlyLimit a calendar month, on the saved card).
type AISpendSettings struct {
	OrgID              uuid.UUID `json:"-"`
	SpendLimitDaily    *Micro    `json:"-"`
	SpendLimitWeekly   *Micro    `json:"-"`
	SpendLimitMonthly  *Micro    `json:"-"`
	MemberLimitDaily   *Micro    `json:"-"`
	MemberLimitWeekly  *Micro    `json:"-"`
	MemberLimitMonthly *Micro    `json:"-"`

	LowBalanceThreshold   Micro      `json:"-"`
	LowBalanceNotifiedAt  *time.Time `json:"-"`
	BalanceOutNotifiedAt  *time.Time `json:"-"`
	AutoReloadEnabled     bool       `json:"-"`
	AutoReloadThreshold   Micro      `json:"-"`
	AutoReloadAmount      Micro      `json:"-"`
	AutoReloadMonthlyMax  Micro      `json:"-"`
	AutoReloadFailureCode string     `json:"-"`
	AutoReloadFailedAt    *time.Time `json:"-"`
	CreatedAt             time.Time  `json:"-"`
	UpdatedAt             time.Time  `json:"-"`
}

// CreditUsagePoint is one day of spend for the usage chart.
type CreditUsagePoint struct {
	Date   string // YYYY-MM-DD (UTC)
	Amount Micro
	Tokens int
}

// CreditUsageBucket is one row of a usage breakdown (by feature reason or by
// model): amount spent, tokens metered, and the number of charges.
type CreditUsageBucket struct {
	Key    string
	Amount Micro
	Tokens int
	Count  int
}

// CreditDebit is one charge against the balance. A strict debit refuses when
// the balance is short; a clamped one takes what is there, because the work
// it pays for was already delivered.
type CreditDebit struct {
	Amount       Micro
	Clamp        bool
	Reason       string
	Model        string
	InputTokens  int
	OutputTokens int
	Quantity     int
	// Cost is what the provider charged, before the markup.
	Cost           Micro
	IdempotencyKey string
	// AgencyAllowance is true where agency plans are billed, so an agency's per-period allowance is in force.
	AgencyAllowance bool
}

// CreditTransaction is one append-only row in the credit audit trail. Amount is
// negative for consumption and positive for grants/purchases. BalanceAfter is
// the resulting ledger balance captured atomically with the mutation.
// IdempotencyKey is nil for non-idempotent operations (grants, monthly resets)
// and set to the caller's Idempotency-Key for consumption so retries don't
// double-charge.
type CreditTransaction struct {
	ID           uuid.UUID
	OrgID        uuid.UUID
	Amount       Micro
	Reason       string
	ModelUsed    string
	InputTokens  int
	OutputTokens int
	Quantity     int
	Cost         Micro
	// MonthlyAfter and PurchasedAfter are both pools after the change, and
	// PurchasedDelta the part of Amount applied to the purchased pool, so the
	// trail reconstructs both pools.
	MonthlyAfter   Micro
	PurchasedDelta Micro
	PurchasedAfter Micro
	// AgencyDelta is the part of Amount the agency's shared balance paid (negative) or got back (positive).
	AgencyDelta    Micro
	IdempotencyKey *string
	// ActorUserID is the teammate who triggered the charge (nil for scheduled
	// or system work); Context names exactly what ran.
	ActorUserID *uuid.UUID
	Context     CreditContext
	CreatedAt   time.Time
}
