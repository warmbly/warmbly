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
