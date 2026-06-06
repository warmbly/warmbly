package models

import (
	"time"

	"github.com/google/uuid"
)

// CreditLedger is the authoritative per-organization AI-credit balance. There
// is exactly one row per organization (org_id is the primary key). Balance is
// mutated only through atomic conditional UPDATEs in the repository so it can
// never go negative under concurrent generation requests.
type CreditLedger struct {
	OrgID          uuid.UUID `json:"org_id"`
	Balance        int       `json:"balance"`
	MonthResetAt   time.Time `json:"month_reset_at"`
	TotalPurchased int       `json:"total_purchased"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// CreditTransaction is one append-only row in the credit audit trail. Amount is
// negative for consumption and positive for grants/purchases. BalanceAfter is
// the resulting ledger balance captured atomically with the mutation.
// IdempotencyKey is nil for non-idempotent operations (grants, monthly resets)
// and set to the caller's Idempotency-Key for consumption so retries don't
// double-charge.
type CreditTransaction struct {
	ID             uuid.UUID `json:"id"`
	OrgID          uuid.UUID `json:"org_id"`
	Amount         int       `json:"amount"`
	Reason         string    `json:"reason"`
	ModelUsed      string    `json:"model_used"`
	TokensUsed     int       `json:"tokens_used"`
	BalanceAfter   int       `json:"balance_after"`
	IdempotencyKey *string   `json:"idempotency_key,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}
