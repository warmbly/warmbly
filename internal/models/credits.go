package models

import (
	"time"

	"github.com/google/uuid"
)

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
	PurchasedDelta        int       `json:"purchased_delta"`
	PurchasedBalanceAfter int       `json:"purchased_balance_after"`
	IdempotencyKey        *string   `json:"idempotency_key,omitempty"`
	CreatedAt             time.Time `json:"created_at"`
}
