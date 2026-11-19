package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

// A purchased-only debit (what the store charges) against a fresh, migrated database:
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LivePurchasedOnly -v

func storeDebit(amount models.Micro, key string) models.CreditDebit {
	return models.CreditDebit{Amount: amount, Reason: "Mailboxes · 1 × $3.00 · Dec 2026", Cost: amount, IdempotencyKey: key, PurchasedOnly: true}
}

func pools(t *testing.T, f *agencyAIFixture, org uuid.UUID) (monthly, purchased models.Micro) {
	t.Helper()
	l, err := f.credits.GetBalance(context.Background(), org)
	if err != nil {
		t.Fatal(err)
	}
	if l == nil {
		return 0, 0
	}
	return l.Monthly, l.Purchased
}

func TestLivePurchasedOnlyNeverSpendsTheAIAllowance(t *testing.T) {
	f := newAgencyAIFixture(t)
	ctx := context.Background()
	org := f.outsider
	if _, _, err := f.credits.Grant(ctx, org, 20*usd1, "monthly_allowance", ""); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := f.credits.Debit(ctx, org, storeDebit(3*usd1, "store:month:a")); !errors.Is(err, ErrInsufficientCredits) {
		t.Fatalf("a $20 allowance and no purchased money paid a store charge: %v", err)
	}
	if m, p := pools(t, f, org); m != 20*usd1 || p != 0 {
		t.Fatalf("allowance %d, purchased %d after a refused store charge", m, p)
	}
	// What the card adds lands in the purchased pool, and the store charge takes it from there.
	if _, _, err := f.credits.GrantPurchased(ctx, org, 10*usd1, "store_card", "invoice:in_1"); err != nil {
		t.Fatal(err)
	}
	after, txn, _, err := f.credits.Debit(ctx, org, storeDebit(3*usd1, "store:month:a"))
	if err != nil {
		t.Fatal(err)
	}
	if after != 27*usd1 || txn.PurchasedDelta != -3*usd1 || txn.MonthlyAfter != 20*usd1 {
		t.Fatalf("after %d, purchased delta %d, allowance %d", after, txn.PurchasedDelta, txn.MonthlyAfter)
	}
	// The refund goes back to the purchased pool, never the allowance.
	if back, _, err := f.credits.RefundSpend(ctx, org, "store:month:a", "store:month:a:refund", "Refund"); err != nil || back != 3*usd1 {
		t.Fatalf("refund %d, %v", back, err)
	}
	if m, p := pools(t, f, org); m != 20*usd1 || p != 10*usd1 {
		t.Fatalf("after the refund: allowance %d, purchased %d", m, p)
	}
}

func TestLivePurchasedOnlyNeverTouchesAgencyFunding(t *testing.T) {
	f := newAgencyAIFixture(t)
	ctx := context.Background()
	before := f.ledger()
	if _, _, _, err := f.credits.Debit(ctx, f.clientA, storeDebit(3*usd1, "store:order:a:0")); !errors.Is(err, ErrInsufficientCredits) {
		t.Fatalf("an agency client's store charge drew on the agency: %v", err)
	}
	if _, _, err := f.credits.GrantPurchased(ctx, f.clientA, 5*usd1, "store_card", "invoice:in_2"); err != nil {
		t.Fatal(err)
	}
	_, txn, _, err := f.credits.Debit(ctx, f.clientA, storeDebit(3*usd1, "store:order:a:0"))
	if err != nil {
		t.Fatal(err)
	}
	if txn.AgencyDelta != 0 || txn.PurchasedDelta != -3*usd1 {
		t.Fatalf("agency delta %d, purchased delta %d", txn.AgencyDelta, txn.PurchasedDelta)
	}
	if l := f.ledger(); l.Allowance != before.Allowance || l.Purchased != before.Purchased {
		t.Fatalf("agency funding moved: %+v -> %+v", before, l)
	}
}

// AI debits are as they were: the allowance first, then purchased money, then the agency.
func TestLivePurchasedOnlyLeavesAIDebitsAlone(t *testing.T) {
	f := newAgencyAIFixture(t)
	ctx := context.Background()
	org := f.outsider
	if _, _, err := f.credits.Grant(ctx, org, 2*usd1, "monthly_allowance", ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.credits.GrantPurchased(ctx, org, 5*usd1, "credit_topup", "evt_1"); err != nil {
		t.Fatal(err)
	}
	_, txn, _, err := f.credits.Debit(ctx, org, debit(3*usd1, false, "ai-1"))
	if err != nil {
		t.Fatal(err)
	}
	if txn.MonthlyAfter != 0 || txn.PurchasedDelta != -usd1 || txn.PurchasedAfter != 4*usd1 {
		t.Fatalf("AI debit: allowance %d, purchased delta %d, purchased %d", txn.MonthlyAfter, txn.PurchasedDelta, txn.PurchasedAfter)
	}
	_, txn, _, err = f.credits.Debit(ctx, f.clientB, debit(2*usd1, false, "ai-2"))
	if err != nil || txn == nil || txn.AgencyDelta != -2*usd1 {
		t.Fatalf("an agency client's AI debit no longer reaches the agency: %+v, %v", txn, err)
	}
}

func aiDebit(amount models.Micro, key string, allowanceOnly bool, limit *models.Micro) models.CreditDebit {
	return models.CreditDebit{Amount: amount, Reason: "writing_assistant", IdempotencyKey: key, AllowanceOnly: allowanceOnly, OverageMonthlyLimit: limit}
}

func TestLiveAIOverageOffSpendsOnlyTheAllowance(t *testing.T) {
	f := newAgencyAIFixture(t)
	ctx := context.Background()
	org := f.outsider
	if _, _, err := f.credits.Grant(ctx, org, 2*usd1, "monthly_allowance", ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.credits.GrantPurchased(ctx, org, 10*usd1, "credit_topup", "evt_ov1"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := f.credits.Debit(ctx, org, aiDebit(2*usd1, "ai-a", true, nil)); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := f.credits.Debit(ctx, org, aiDebit(usd1, "ai-b", true, nil)); !errors.Is(err, ErrAllowanceUsed) || !errors.Is(err, ErrInsufficientCredits) {
		t.Fatalf("the next AI debit past the allowance: %v", err)
	}
	// A clamped charge (work already done) takes only what the allowance has: nothing.
	if _, txn, _, err := f.credits.Debit(ctx, org, models.CreditDebit{Amount: usd1, Clamp: true, Reason: "writing_assistant", IdempotencyKey: "ai-c", AllowanceOnly: true}); err != nil || txn != nil {
		t.Fatalf("clamped past the allowance: %+v, %v", txn, err)
	}
	if m, p := pools(t, f, org); m != 0 || p != 10*usd1 {
		t.Fatalf("allowance %d, purchased %d: purchased must be untouched", m, p)
	}
	// Store debits ignore the AI setting.
	if _, _, _, err := f.credits.Debit(ctx, org, storeDebit(3*usd1, "store:month:ov")); err != nil {
		t.Fatal(err)
	}
	if _, p := pools(t, f, org); p != 7*usd1 {
		t.Fatalf("purchased after the store charge = %d", p)
	}
}

func TestLiveAIOverageLimitCapsWhatAITakesFromPurchased(t *testing.T) {
	f := newAgencyAIFixture(t)
	ctx := context.Background()
	org := f.outsider
	limit := 5 * usd1
	if _, _, err := f.credits.Grant(ctx, org, usd1, "monthly_allowance", ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.credits.GrantPurchased(ctx, org, 20*usd1, "credit_topup", "evt_ov2"); err != nil {
		t.Fatal(err)
	}
	// $1 allowance, then $4 of the $5 limit.
	if _, txn, _, err := f.credits.Debit(ctx, org, aiDebit(5*usd1, "ai-1", false, &limit)); err != nil || txn.PurchasedDelta != -4*usd1 {
		t.Fatalf("first: %+v, %v", txn, err)
	}
	// Store charges do not count toward the AI limit.
	if _, _, _, err := f.credits.Debit(ctx, org, storeDebit(3*usd1, "store:order:ov:0")); err != nil {
		t.Fatal(err)
	}
	if _, txn, _, err := f.credits.Debit(ctx, org, aiDebit(usd1, "ai-2", false, &limit)); err != nil || txn.PurchasedDelta != -usd1 {
		t.Fatalf("up to the limit: %+v, %v", txn, err)
	}
	if _, _, _, err := f.credits.Debit(ctx, org, aiDebit(usd1/2, "ai-3", false, &limit)); !errors.Is(err, ErrOverageLimitReached) {
		t.Fatalf("past the limit: %v", err)
	}
	// A refund of AI spend gives that room back.
	if _, _, err := f.credits.RefundSpend(ctx, org, "ai-2", "ai-2:refund", "writing_assistant_refund"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := f.credits.Debit(ctx, org, aiDebit(usd1, "ai-4", false, &limit)); err != nil {
		t.Fatalf("after a refund: %v", err)
	}
	if _, p := pools(t, f, org); p != 12*usd1 {
		t.Fatalf("purchased = %d, want 20 - 5 AI - 3 store", p)
	}
}

// A workspace that never saved settings reads overage on with no limit, as before.
func TestLiveAIOverageDefaultsKeepTodaysBehaviour(t *testing.T) {
	f := newAgencyAIFixture(t)
	ctx := context.Background()
	settings := NewAISettingsRepository(f.handle)
	f.exec(`INSERT INTO org_ai_settings (org_id) VALUES ($1)`, f.outsider)
	cfg, err := settings.Get(ctx, f.outsider)
	if err != nil || cfg == nil || !cfg.AIOverageEnabled || cfg.AIOverageMonthlyLimit != nil {
		t.Fatalf("defaults = %+v, %v", cfg, err)
	}
	limit := 5 * usd1
	if cfg, err = settings.UpsertOverage(ctx, f.outsider, false, &limit); err != nil || cfg.AIOverageEnabled || cfg.AIOverageMonthlyLimit == nil || *cfg.AIOverageMonthlyLimit != limit {
		t.Fatalf("saved = %+v, %v", cfg, err)
	}
	// Saving the spend limits leaves the overage choice alone.
	if cfg, err = settings.UpsertLimits(ctx, &models.AISpendSettings{OrgID: f.outsider, LowBalanceThreshold: usd1}); err != nil || cfg.AIOverageEnabled {
		t.Fatalf("after saving limits = %+v, %v", cfg, err)
	}
}
