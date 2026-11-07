package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

func TestLiveBalanceChargesAndAutoReload(t *testing.T) {
	handle, pool := liveContactDB(t)
	ctx := context.Background()
	userID, orgID := uuid.New(), uuid.New()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	exec(`INSERT INTO users (id, first_name, last_name, email) VALUES ($1, 'Balance', 'Live', $2)`,
		userID, "balance-"+uuid.NewString()+"@"+"example.test")
	exec(`INSERT INTO organizations (id, name, owner_user_id) VALUES ($1, 'Balance Live', $2)`, orgID, userID)
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = pool.Exec(bg, `DELETE FROM credit_auto_topup_attempts WHERE organization_id = $1`, orgID)
		_, _ = pool.Exec(bg, `DELETE FROM credit_ledger_transactions WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(bg, `DELETE FROM credit_ledger WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(bg, `DELETE FROM org_ai_settings WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(bg, `DELETE FROM organizations WHERE id = $1`, orgID)
		_, _ = pool.Exec(bg, `DELETE FROM users WHERE id = $1`, userID)
	})

	ledger, settings, charges := NewCreditRepository(handle), NewAISettingsRepository(handle), NewSavedCardChargeRepository(handle)
	if _, _, err := ledger.GrantPurchased(ctx, orgID, 2*models.MicroPerUSD, "test", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := settings.UpsertAutoReload(ctx, orgID, true, 5*models.MicroPerUSD, 25*models.MicroPerUSD, 250*models.MicroPerUSD); err != nil {
		t.Fatal(err)
	}
	due, err := settings.AutoReloadDue(ctx, 1000)
	if err != nil || !dueHasOrg(due, orgID) {
		t.Fatalf("AutoReloadDue = %v, %v; want the org under its threshold", due, err)
	}

	c, err := charges.Create(ctx, &models.SavedCardCharge{OrgID: orgID, Kind: models.SavedCardAutoReload, Amount: 25 * models.MicroPerUSD, AmountCents: 2500, Currency: "usd", CustomerID: "cus", PaymentMethodID: "pm"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := charges.Create(ctx, &models.SavedCardCharge{OrgID: orgID, Kind: models.SavedCardTopUp, Amount: 10 * models.MicroPerUSD, AmountCents: 1000, Currency: "usd"}); !errors.Is(err, ErrChargeInFlight) {
		t.Fatalf("second pending charge: %v, want ErrChargeInFlight", err)
	}
	if due, _ := settings.AutoReloadDue(ctx, 1000); dueHasOrg(due, orgID) {
		t.Fatal("an org with a charge in flight is due again")
	}
	if got, _ := charges.ReloadedSince(ctx, orgID, time.Now().Add(-time.Hour)); got != 25*models.MicroPerUSD {
		t.Fatalf("ReloadedSince = %v; a pending reload counts against the limit", got)
	}
	if err := charges.Snooze(ctx, c.ID, time.Now().Add(-time.Second), models.ReloadCardDeclined); err != nil {
		t.Fatal(err)
	}
	if list, _ := charges.Due(ctx, time.Now(), time.Hour, 50); !containsCharge(list, c.ID) {
		t.Fatal("a charge whose retry time came is not due")
	}
	if won, _ := charges.MarkSucceeded(ctx, c.ID); !won {
		t.Fatal("first settle lost")
	}
	if won, _ := charges.MarkSucceeded(ctx, c.ID); won {
		t.Fatal("a charge settled twice")
	}
	if last, _ := charges.LastSucceeded(ctx, orgID, models.SavedCardAutoReload); last == nil || last.ID != c.ID {
		t.Fatalf("LastSucceeded = %+v", last)
	}

	if off, _ := settings.DisableAutoReload(ctx, orgID, models.ReloadCardDeclined); !off {
		t.Fatal("DisableAutoReload did not turn reload off")
	}
	if off, _ := settings.DisableAutoReload(ctx, orgID, models.ReloadCardDeclined); off {
		t.Fatal("DisableAutoReload reported turning off a reload already off")
	}
	cfg, _ := settings.Get(ctx, orgID)
	if cfg.AutoReloadEnabled || cfg.AutoReloadFailureCode != models.ReloadCardDeclined {
		t.Fatalf("settings = %+v", cfg)
	}
	month := time.Now().UTC().AddDate(0, 0, -time.Now().UTC().Day()+1)
	if won, _ := settings.StampNotice(ctx, orgID, "low", month); !won {
		t.Fatal("first low notice lost")
	}
	if won, _ := settings.StampNotice(ctx, orgID, "low", month); won {
		t.Fatal("low notice sent twice in a month")
	}
	_ = settings.RearmNotices(ctx, orgID)
	if won, _ := settings.StampNotice(ctx, orgID, "low", month); !won {
		t.Fatal("money added did not re-arm the notice")
	}
	if cfg, _ := settings.UpsertAutoReload(ctx, orgID, true, 5*models.MicroPerUSD, 25*models.MicroPerUSD, 250*models.MicroPerUSD); cfg.AutoReloadFailureCode != "" {
		t.Fatal("turning reload back on kept the failure")
	}
	// The previous release's switch stays off whatever this release saves.
	var legacyOn bool
	if err := pool.QueryRow(ctx, `SELECT auto_topup_enabled FROM org_ai_settings WHERE org_id = $1`, orgID).Scan(&legacyOn); err != nil || legacyOn {
		t.Fatalf("auto_topup_enabled = %v, %v; want false", legacyOn, err)
	}

	// A pack charge the previous release left pending frees the slot after a day.
	exec(`INSERT INTO credit_auto_topup_attempts (organization_id, pack_key, credits, created_at) VALUES ($1, 'pack_500', 500, now() - interval '25 hours')`, orgID)
	if n, err := charges.CloseLegacy(ctx, time.Now().Add(-24*time.Hour), models.ChargeExpired); err != nil || n < 1 {
		t.Fatalf("CloseLegacy = %d, %v", n, err)
	}
	if p, _ := charges.Pending(ctx, orgID); p != nil {
		t.Fatalf("a day-old legacy charge still holds the slot: %+v", p)
	}
	exec(`INSERT INTO credit_auto_topup_attempts (organization_id, pack_key, credits) VALUES ($1, 'pack_500', 500)`, orgID)
	_, _ = charges.CloseLegacy(ctx, time.Now().Add(-24*time.Hour), models.ChargeExpired)
	if p, _ := charges.Pending(ctx, orgID); p == nil || !p.Legacy {
		t.Fatal("a fresh legacy charge was closed")
	}
}

func dueHasOrg(ids []uuid.UUID, id uuid.UUID) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

func containsCharge(list []models.SavedCardCharge, id uuid.UUID) bool {
	for _, c := range list {
		if c.ID == id {
			return true
		}
	}
	return false
}

func TestLiveAnnualSubscriptionAllowanceIsDueMonthly(t *testing.T) {
	handle, pool := liveContactDB(t)
	ctx := context.Background()
	userID, orgID, monthlyOrg := uuid.New(), uuid.New(), uuid.New()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	exec(`INSERT INTO users (id, first_name, last_name, email) VALUES ($1, 'Annual', 'Live', $2)`,
		userID, "annual-"+uuid.NewString()+"@"+"example.test")
	for _, id := range []uuid.UUID{orgID, monthlyOrg} {
		exec(`INSERT INTO organizations (id, name, owner_user_id) VALUES ($1, 'Annual Live', $2)`, id, userID)
	}
	// A yearly period started 70 days ago, last reset 60 days ago; a monthly
	// period is left to its invoices.
	exec(`INSERT INTO subscriptions (user_id, organization_id, plan_id, stripe_customer_id, status, current_period_start, current_period_end)
	      VALUES ($1, $2, $3, '', 'active', now() - interval '70 days', now() + interval '295 days'),
	             ($1, $4, $3, '', 'active', now() - interval '29 days', now() + interval '1 day')`,
		userID, orgID, models.TestPlanID, monthlyOrg)
	exec(`INSERT INTO credit_ledger (org_id, balance_micro, month_reset_at) VALUES ($1, 0, now() - interval '60 days'), ($2, 0, now() - interval '29 days')`, orgID, monthlyOrg)
	t.Cleanup(func() {
		bg := context.Background()
		for _, id := range []uuid.UUID{orgID, monthlyOrg} {
			_, _ = pool.Exec(bg, `DELETE FROM credit_ledger_transactions WHERE org_id = $1`, id)
			_, _ = pool.Exec(bg, `DELETE FROM credit_ledger WHERE org_id = $1`, id)
			_, _ = pool.Exec(bg, `DELETE FROM subscriptions WHERE organization_id = $1`, id)
			_, _ = pool.Exec(bg, `DELETE FROM organizations WHERE id = $1`, id)
		}
		_, _ = pool.Exec(bg, `DELETE FROM users WHERE id = $1`, userID)
	})

	ledger := NewCreditRepository(handle)
	due, err := ledger.AnnualAllowancesDue(ctx, 1000)
	if err != nil {
		t.Fatal(err)
	}
	var found *AnnualAllowance
	for i := range due {
		switch due[i].OrgID {
		case orgID:
			found = &due[i]
		case monthlyOrg:
			t.Fatal("a monthly subscription is refreshed by the sweep instead of its invoice")
		}
	}
	if found == nil || found.Allowance <= 0 {
		t.Fatalf("annual subscription not due: %+v", due)
	}
	if _, err := ledger.ResetMonthly(ctx, orgID, found.Allowance, "annual_month:test"); err != nil {
		t.Fatal(err)
	}
	if again, _ := ledger.AnnualAllowancesDue(ctx, 1000); slicesContainsOrg(again, orgID) {
		t.Fatal("a freshly reset subscription is still due")
	}
}

func slicesContainsOrg(list []AnnualAllowance, id uuid.UUID) bool {
	for _, a := range list {
		if a.OrgID == id {
			return true
		}
	}
	return false
}
