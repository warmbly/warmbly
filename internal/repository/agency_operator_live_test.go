package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

// What an operator sets on an agency, against a fresh, migrated database:
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LiveAgencyOperator -v

func TestLiveAgencyOperatorPlanFundsClients(t *testing.T) {
	f := newAgencyAIFixture(t)
	ctx := context.Background()
	agencies, brands := NewAgencyRepository(f.handle.Pool), NewBrandRepository(f.handle.Pool)
	admin := uuid.New()
	f.exec(`INSERT INTO users (id, first_name, last_name, email) VALUES ($1, 'O', 'P', $2)`, admin, admin.String()+"@example.test")

	// With its subscription over, the agency pays for nothing.
	f.exec(`UPDATE agencies SET status = 'canceled' WHERE id = $1`, f.agency)
	if _, _, _, err := f.credits.Debit(ctx, f.clientA, debit(usd1, false, "")); !errors.Is(err, ErrInsufficientCredits) {
		t.Fatalf("no plan in force: %v", err)
	}
	if planID, status, err := brands.AgencyPlan(ctx, f.agency); err != nil || planID == nil || *planID != models.AgencyPlanID || status != models.SubscriptionStatusCanceled {
		t.Fatalf("brand plan before the grant: %v %s %v", planID, status, err)
	}

	// An operator's Agency Pro plan refills the shared balance to that plan's $30.
	if err := agencies.SetManagedPlan(ctx, f.agency, models.AgencyProPlanID, admin, "Annual contract", nil); err != nil {
		t.Fatal(err)
	}
	a, err := agencies.Get(ctx, f.agency)
	if err != nil || a == nil {
		t.Fatal(err)
	}
	now := time.Now()
	if !a.Granted(now) || a.ManagedBy == nil || *a.ManagedBy != admin || a.ManagedReason == nil || *a.ManagedReason != "Annual contract" || a.ManagedUntil != nil {
		t.Fatalf("grant as read back: %+v", a)
	}
	if planID, _, ok := models.AgencyEntitlement(a, now); !ok || planID != models.AgencyProPlanID {
		t.Fatalf("entitlement: %s %v", planID, ok)
	}
	if planID, status, err := brands.AgencyPlan(ctx, f.agency); err != nil || planID == nil || *planID != models.AgencyProPlanID || status != models.SubscriptionStatusActive {
		t.Fatalf("brand plan under the grant: %v %s %v", planID, status, err)
	}
	if _, txn, _, err := f.credits.Debit(ctx, f.clientA, debit(3*usd1, false, "granted-1")); err != nil || txn.AgencyDelta != -3*usd1 {
		t.Fatalf("charge under the grant: %v", err)
	}
	if l := f.ledger(); l.Allowance != 27*usd1 {
		t.Fatalf("allowance left: %d, want $27 of Agency Pro's $30", l.Allowance)
	}

	// The client's grant follows with no end.
	if err := agencies.SyncClientGrant(ctx, f.agency, f.clientA, models.AgencyProPlanID, nil); err != nil {
		t.Fatal(err)
	}
	var planID uuid.UUID
	var until *time.Time
	if err := f.handle.QueryRow(ctx, `SELECT managed_plan_id, managed_until FROM subscriptions WHERE organization_id = $1`, f.clientA).Scan(&planID, &until); err != nil {
		t.Fatal(err)
	}
	if planID != models.AgencyProPlanID || until != nil {
		t.Fatalf("client grant: %s until %v", planID, until)
	}

	// A grant past its end entitles to nothing again.
	past := now.Add(-time.Minute)
	if err := agencies.SetManagedPlan(ctx, f.agency, models.AgencyProPlanID, admin, "Trial", &past); err != nil {
		t.Fatal(err)
	}
	if planID, status, _ := brands.AgencyPlan(ctx, f.agency); planID == nil || *planID != models.AgencyPlanID || status != models.SubscriptionStatusCanceled {
		t.Fatalf("brand plan after the grant lapsed: %v %s", planID, status)
	}
	if err := agencies.ClearManagedPlan(ctx, f.agency); err != nil {
		t.Fatal(err)
	}
	if a, _ := agencies.Get(ctx, f.agency); a == nil || a.ManagedAt != nil || a.ManagedPlanID != nil {
		t.Fatalf("cleared grant: %+v", a)
	}
}

func TestLiveAgencyOperatorOverridesAndCredits(t *testing.T) {
	f := newAgencyAIFixture(t)
	ctx := context.Background()
	agencies := NewAgencyRepository(f.handle.Pool)
	admin := uuid.New()
	f.exec(`INSERT INTO users (id, first_name, last_name, email) VALUES ($1, 'O', 'P', $2)`, admin, admin.String()+"@example.test")

	// The agency's own allowance stands in for its plan's $10.
	pool, allowance := 2_000_000, 50*usd1
	if err := agencies.SetOverrides(ctx, f.agency, &pool, &allowance, admin, "Agency Scale"); err != nil {
		t.Fatal(err)
	}
	a, err := agencies.Get(ctx, f.agency)
	if err != nil || a == nil || a.PoolOverride == nil || *a.PoolOverride != pool || a.AIAllowanceOverride == nil || *a.AIAllowanceOverride != allowance {
		t.Fatalf("overrides as read back: %+v %v", a, err)
	}
	if _, _, _, err := f.credits.Debit(ctx, f.clientA, debit(3*usd1, false, "override-1")); err != nil {
		t.Fatal(err)
	}
	if l := f.ledger(); l.Allowance != 47*usd1 {
		t.Fatalf("allowance left: %d, want $47 of the agency's own $50", l.Allowance)
	}

	// An operator's credit lands once per key, never expires and is no purchase.
	p := models.AgencyAIPeriodOf(a, nil, true, time.Now())
	for i := range 2 {
		if ok, err := f.ai.Credit(ctx, f.agency, 20*usd1, p, "goodwill", "k1", admin); err != nil || ok != (i == 0) {
			t.Fatalf("credit %d: %v %v", i, ok, err)
		}
	}
	if l := f.ledger(); l.Purchased != 20*usd1 || l.TotalPurchased != 0 {
		t.Fatalf("after the credit: purchased %d, total purchased %d", l.Purchased, l.TotalPurchased)
	}

	// Sends an operator adds have no payment, so two of them are two rows.
	start := time.Now().UTC().Truncate(time.Hour)
	reason := "outage"
	for range 2 {
		if ok, err := agencies.RecordTopup(ctx, &models.AgencyPoolTopup{AgencyID: f.agency, Sends: 25_000, PeriodStart: start, GrantedBy: &admin, Reason: &reason}); err != nil || !ok {
			t.Fatalf("pool credit: %v %v", ok, err)
		}
	}
	if n, err := agencies.TopupSends(ctx, f.agency, start); err != nil || n != 50_000 {
		t.Fatalf("pool credits this period: %d %v", n, err)
	}

	// Removing the overrides hands the agency back to its plan's values.
	if err := agencies.SetOverrides(ctx, f.agency, nil, nil, admin, ""); err != nil {
		t.Fatal(err)
	}
	if a, _ := agencies.Get(ctx, f.agency); a == nil || a.PoolOverride != nil || a.AIAllowanceOverride != nil {
		t.Fatalf("cleared overrides: %+v", a)
	}
}
