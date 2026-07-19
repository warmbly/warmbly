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

// CreditLedger is the authoritative per-organization AI-credit balance. There
// is exactly one row per organization (org_id is the primary key). Two pools:
// Balance is the monthly plan allowance (reset to plan.monthly_credits every
// billing cycle), PurchasedBalance holds top-up credits that never expire and
// survive resets. Consumption drains the monthly pool first. Balances are
// mutated only inside row-locked repository transactions backstopped by DB
// CHECK constraints so neither pool can go negative under concurrency.
type CreditLedger struct {
	OrgID            uuid.UUID `json:"org_id"`
	Balance          int       `json:"balance"`
	PurchasedBalance int       `json:"purchased_balance"`
	MonthResetAt     time.Time `json:"month_reset_at"`
	TotalPurchased   int       `json:"total_purchased"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// Total is the spendable balance across both pools.
func (l *CreditLedger) Total() int {
	return l.Balance + l.PurchasedBalance
}

// AISpendSettings is an org's AI spend-control row: optional hard spend limits
// per calendar day / ISO week / calendar month (nil = no limit), the balance
// threshold that triggers a low-credit alert (with a stamp so the alert fires
// at most once per day), and the auto top-up configuration (buy a pack
// automatically when the balance dips below the threshold, bounded per month).
type AISpendSettings struct {
	OrgID             uuid.UUID `json:"org_id"`
	SpendLimitDaily   *int      `json:"spend_limit_daily"`
	SpendLimitWeekly  *int      `json:"spend_limit_weekly"`
	SpendLimitMonthly *int      `json:"spend_limit_monthly"`
	// MemberLimitMonthly caps what each individual member can spend per
	// calendar month (nil = no per-member limit). Enforced against the
	// ledger's actor attribution; scheduled/system work is not counted.
	MemberLimitMonthly   *int       `json:"member_limit_monthly"`
	LowBalanceThreshold  int        `json:"low_balance_threshold"`
	LowBalanceNotifiedAt *time.Time `json:"low_balance_notified_at,omitempty"`
	AutoTopupEnabled     bool       `json:"auto_topup_enabled"`
	AutoTopupPack        string     `json:"auto_topup_pack"`
	AutoTopupThreshold   int        `json:"auto_topup_threshold"`
	AutoTopupMaxPerMonth int        `json:"auto_topup_max_per_month"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

// CreditUsagePoint is one day of AI spend for the usage chart.
type CreditUsagePoint struct {
	Date    string `json:"date"` // YYYY-MM-DD (UTC)
	Credits int    `json:"credits"`
	Tokens  int    `json:"tokens"`
}

// CreditUsageBucket is one row of a usage breakdown (by feature reason or by
// model): credits spent, tokens metered, and the number of charges.
type CreditUsageBucket struct {
	Key     string `json:"key"`
	Credits int    `json:"credits"`
	Tokens  int    `json:"tokens"`
	Count   int    `json:"count"`
}

// CreditTransaction is one append-only row in the credit audit trail. Amount is
// negative for consumption and positive for grants/purchases. BalanceAfter is
// the resulting ledger balance captured atomically with the mutation.
// IdempotencyKey is nil for non-idempotent operations (grants, monthly resets)
// and set to the caller's Idempotency-Key for consumption so retries don't
// double-charge.
type CreditTransaction struct {
	ID           uuid.UUID `json:"id"`
	OrgID        uuid.UUID `json:"org_id"`
	Amount       int       `json:"amount"`
	Reason       string    `json:"reason"`
	ModelUsed    string    `json:"model_used"`
	TokensUsed   int       `json:"tokens_used"`
	BalanceAfter int       `json:"balance_after"`
	// PurchasedDelta is the signed portion of Amount applied to the purchased
	// pool (0 when only the monthly pool moved); PurchasedBalanceAfter is that
	// pool's resulting balance, so the log reconstructs both pools.
	PurchasedDelta        int     `json:"purchased_delta"`
	PurchasedBalanceAfter int     `json:"purchased_balance_after"`
	IdempotencyKey        *string `json:"idempotency_key,omitempty"`
	// ActorUserID is the teammate who triggered the charge (nil for scheduled
	// or system work); Context names exactly what ran.
	ActorUserID *uuid.UUID    `json:"actor_user_id,omitempty"`
	Context     CreditContext `json:"context"`
	CreatedAt   time.Time     `json:"created_at"`
}
