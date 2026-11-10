package repository

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// The agency's shared AI balance against a fresh, migrated database:
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LiveAgencyAI -v

type agencyAIFixture struct {
	t        *testing.T
	handle   *db.DB
	credits  CreditRepository
	ai       AgencyAIRepository
	agency   uuid.UUID
	clientA  uuid.UUID
	clientB  uuid.UUID
	outsider uuid.UUID
}

const usd1 = models.MicroPerUSD

func newAgencyAIFixture(t *testing.T) *agencyAIFixture {
	pool := liveCampaignReplayDB(t)
	f := &agencyAIFixture{t: t, handle: &db.DB{Pool: pool}}
	f.credits, f.ai = NewCreditRepository(f.handle), NewAgencyAIRepository(f.handle)
	owner := uuid.New()
	f.exec(`INSERT INTO users (id, first_name, last_name, email) VALUES ($1, 'A', 'B', $2)`, owner, owner.String()+"@example.test")
	f.agency = uuid.New()
	// The seeded Agency plan, whose allowance migration 000381 sets to $10.
	f.exec(`INSERT INTO agencies (id, name, owner_user_id, status, plan_id, current_period_start, current_period_end)
		VALUES ($1, 'Agency', $2, 'active', $3, now() - interval '5 days', now() + interval '25 days')`, f.agency, owner, models.AgencyPlanID)
	f.clientA, f.clientB, f.outsider = uuid.New(), uuid.New(), uuid.New()
	for _, org := range []uuid.UUID{f.clientA, f.clientB, f.outsider} {
		f.exec(`INSERT INTO organizations (id, name, owner_user_id) VALUES ($1, 'Org', $2)`, org, owner)
	}
	for _, org := range []uuid.UUID{f.clientA, f.clientB} {
		f.exec(`INSERT INTO agency_clients (organization_id, agency_id) VALUES ($1, $2)`, org, f.agency)
	}
	return f
}

func (f *agencyAIFixture) exec(query string, args ...any) {
	f.t.Helper()
	if _, err := f.handle.Exec(context.Background(), query, args...); err != nil {
		f.t.Fatalf("fixture: %v\n%s", err, query)
	}
}

func (f *agencyAIFixture) ledger() models.AgencyAILedger {
	f.t.Helper()
	l, err := f.ai.Ledger(context.Background(), f.agency)
	if err != nil {
		f.t.Fatal(err)
	}
	return l
}

func (f *agencyAIFixture) own(org uuid.UUID) models.Micro {
	f.t.Helper()
	l, err := f.credits.GetBalance(context.Background(), org)
	if err != nil {
		f.t.Fatal(err)
	}
	if l == nil {
		return 0
	}
	return l.Total()
}

func (f *agencyAIFixture) period() models.AgencyAIPeriod {
	f.t.Helper()
	var a models.Agency
	if err := f.handle.QueryRow(context.Background(), `SELECT plan_id, status, current_period_start, current_period_end FROM agencies WHERE id = $1`, f.agency).
		Scan(&a.PlanID, &a.Status, &a.CurrentPeriodStart, &a.CurrentPeriodEnd); err != nil {
		f.t.Fatal(err)
	}
	allowance := 10 * usd1
	return models.AgencyAIPeriodOf(&a, &allowance, true, time.Now())
}

func debit(amount models.Micro, clamp bool, key string) models.CreditDebit {
	return models.CreditDebit{Amount: amount, Clamp: clamp, Reason: "writing_assistant", IdempotencyKey: key, AgencyAllowance: true}
}

func TestLiveAgencyAIOwnBalanceFirstThenAgency(t *testing.T) {
	f := newAgencyAIFixture(t)
	ctx := context.Background()
	if _, _, err := f.credits.Grant(ctx, f.clientA, usd1, "test", ""); err != nil {
		t.Fatal(err)
	}
	after, txn, _, err := f.credits.Debit(ctx, f.clientA, debit(3*usd1, false, "charge-1"))
	if err != nil {
		t.Fatal(err)
	}
	if after != 0 || txn.Amount != -3*usd1 || txn.AgencyDelta != -2*usd1 {
		t.Fatalf("own $1 then agency $2: after %d, amount %d, agency %d", after, txn.Amount, txn.AgencyDelta)
	}
	if l := f.ledger(); l.Allowance != 8*usd1 {
		t.Fatalf("agency allowance left: %d", l.Allowance)
	}
	// A workspace outside the agency pays nothing from it.
	if _, _, _, err := f.credits.Debit(ctx, f.outsider, debit(usd1, false, "")); !errors.Is(err, ErrInsufficientCredits) {
		t.Fatalf("outsider: %v", err)
	}
	// A charge past both refuses whole when strict.
	if _, _, _, err := f.credits.Debit(ctx, f.clientA, debit(9*usd1, false, "")); !errors.Is(err, ErrInsufficientCredits) {
		t.Fatalf("strict past both: %v", err)
	}
	// A replay takes nothing more.
	if _, _, replayed, err := f.credits.Debit(ctx, f.clientA, debit(3*usd1, false, "charge-1")); err != nil || !replayed {
		t.Fatalf("replay: %v %v", replayed, err)
	}
	if l := f.ledger(); l.Allowance != 8*usd1 {
		t.Fatalf("replay charged the agency again: %d", l.Allowance)
	}
	// The refund returns each part where it came from, once.
	for range 2 {
		refunded, _, err := f.credits.RefundSpend(ctx, f.clientA, "charge-1", "charge-1:refund", "writing_assistant_refund")
		if err != nil {
			t.Fatal(err)
		}
		_ = refunded
	}
	if f.own(f.clientA) != usd1 || f.ledger().Allowance != 10*usd1 {
		t.Fatalf("after refund: own %d, agency %d", f.own(f.clientA), f.ledger().Allowance)
	}
	// Spend windows count the agency's part, so workspace spend limits see it.
	day := time.Now().Add(-time.Hour)
	if d, _, _, err := f.credits.SpentInWindows(ctx, f.clientA, day, day, day); err != nil || d != 0 {
		t.Fatalf("net of the refund: %d %v", d, err)
	}
	if _, _, _, err := f.credits.Debit(ctx, f.clientA, debit(2*usd1, false, "")); err != nil {
		t.Fatal(err)
	}
	if d, _, _, _ := f.credits.SpentInWindows(ctx, f.clientA, day, day, day); d != 2*usd1 {
		t.Fatalf("spend window: %d", d)
	}
}

func TestLiveAgencyAIClientCapAndPeriodReset(t *testing.T) {
	f := newAgencyAIFixture(t)
	ctx := context.Background()
	// Without agency plans billed, no allowance is in force.
	if avail, _ := f.credits.AgencyAvailable(ctx, f.clientA, false); avail != 0 {
		t.Fatalf("unbilled allowance: %d", avail)
	}
	limit := usd1
	if ok, err := f.ai.SetClientAILimit(ctx, f.agency, f.clientB, &limit); err != nil || !ok {
		t.Fatalf("set cap: %v %v", ok, err)
	}
	if _, _, _, err := f.credits.Debit(ctx, f.clientB, debit(2*usd1, false, "")); !errors.Is(err, ErrInsufficientCredits) {
		t.Fatalf("strict charge past the cap: %v", err)
	}
	_, txn, _, err := f.credits.Debit(ctx, f.clientB, debit(2*usd1, true, ""))
	if err != nil || txn.Amount != -usd1 {
		t.Fatalf("clamped charge takes the cap: %+v %v", txn, err)
	}
	if avail, err := f.credits.AgencyAvailable(ctx, f.clientB, true); err != nil || avail != 0 {
		t.Fatalf("capped client: %d %v", avail, err)
	}
	// The cap is one client's, not the agency's.
	if avail, _ := f.credits.AgencyAvailable(ctx, f.clientA, true); avail != 9*usd1 {
		t.Fatalf("uncapped client: %d", avail)
	}

	// A renewal starts a new period: the allowance refills and the cap's count starts over.
	f.exec(`UPDATE agencies SET current_period_start = now() - interval '1 hour', current_period_end = now() + interval '30 days' WHERE id = $1`, f.agency)
	if avail, _ := f.credits.AgencyAvailable(ctx, f.clientB, true); avail != usd1 {
		t.Fatalf("cap after renewal: %d", avail)
	}
	if avail, _ := f.credits.AgencyAvailable(ctx, f.clientA, true); avail != 10*usd1 {
		t.Fatalf("allowance after renewal: %d", avail)
	}
	if _, _, _, err := f.credits.Debit(ctx, f.clientA, debit(usd1, false, "")); err != nil {
		t.Fatal(err)
	}
	l := f.ledger()
	if l.Allowance != 9*usd1 || l.PeriodStart == nil || time.Since(*l.PeriodStart) > 2*time.Hour {
		t.Fatalf("after the reset: %+v", l)
	}
	// A stale read of the old period refills nothing.
	old := models.AgencyAIPeriod{Start: l.PeriodStart.Add(-30 * 24 * time.Hour), Allowance: 10 * usd1}
	if _, err := f.ai.Transfer(ctx, f.agency, f.clientA, usd1, old, "", uuid.Nil); err != nil {
		t.Fatal(err)
	}
	if f.ledger().Allowance != 8*usd1 {
		t.Fatalf("stale period refilled the allowance: %d", f.ledger().Allowance)
	}
}

func TestLiveAgencyAITransferAndTopups(t *testing.T) {
	f := newAgencyAIFixture(t)
	ctx := context.Background()
	p := f.period()
	if _, err := f.ai.Transfer(ctx, f.agency, f.clientA, 11*usd1, p, "", uuid.Nil); !errors.Is(err, ErrAgencyAIBalanceShort) {
		t.Fatalf("more than the balance: %v", err)
	}
	if _, err := f.ai.Transfer(ctx, f.agency, f.outsider, usd1, p, "", uuid.Nil); !errors.Is(err, ErrAgencyAIClientNotFound) {
		t.Fatalf("not a client: %v", err)
	}
	if f.ledger().In(p).Allowance != 10*usd1 || f.own(f.clientA) != 0 {
		t.Fatal("a refused move changed a balance")
	}
	for i := range 2 {
		tr, err := f.ai.Transfer(ctx, f.agency, f.clientA, 4*usd1, p, "move-1", uuid.Nil)
		if err != nil {
			t.Fatal(err)
		}
		if tr.Replayed != (i == 1) || tr.AgencyBalance != 6*usd1 || tr.WorkspaceBalance != 4*usd1 {
			t.Fatalf("move %d: %+v", i, tr)
		}
	}
	use, _, err := f.ai.PeriodUse(ctx, f.agency, *f.ledger().PeriodStart)
	if err != nil || use[f.clientA].Moved != 4*usd1 || use[f.clientA].Spent != 0 {
		t.Fatalf("period use: %+v %v", use, err)
	}

	pi := "pi_" + uuid.NewString()
	for i := range 2 {
		if ok, err := f.ai.GrantPurchased(ctx, f.agency, 25*usd1, p, pi, nil); err != nil || ok != (i == 0) {
			t.Fatalf("grant %d: %v %v", i, ok, err)
		}
	}
	if l := f.ledger(); l.Purchased != 25*usd1 || l.TotalPurchased != 25*usd1 {
		t.Fatalf("after the top-up: %+v", l)
	}
	if _, bought, _ := f.ai.PeriodUse(ctx, f.agency, *f.ledger().PeriodStart); bought != 25*usd1 {
		t.Fatalf("bought: %d", bought)
	}
	// Spend drains the allowance before bought money.
	if _, _, _, err := f.credits.Debit(ctx, f.clientB, debit(10*usd1, false, "")); err != nil {
		t.Fatal(err)
	}
	if l := f.ledger(); l.Allowance != 0 || l.Purchased != 21*usd1 {
		t.Fatalf("allowance first: %+v", l)
	}
	for range 2 {
		if agency, found, err := f.ai.RefundPurchase(ctx, pi); err != nil || !found || agency != f.agency {
			t.Fatalf("refund: %v %v", found, err)
		}
	}
	// What was spent stays spent; the rest leaves.
	if l := f.ledger(); l.Purchased != 0 {
		t.Fatalf("after the refund: %+v", l)
	}
	if _, bought, _ := f.ai.PeriodUse(ctx, f.agency, *f.ledger().PeriodStart); bought != 0 {
		t.Fatalf("refunded top-up still counted: %d", bought)
	}
	if _, found, _ := f.ai.RefundPurchase(ctx, "pi_unknown"); found {
		t.Fatal("unknown payment found")
	}
}

func TestLiveAgencyAIConcurrentChargesNeverOverdraw(t *testing.T) {
	f := newAgencyAIFixture(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	var mu sync.Mutex
	taken := models.Micro(0)
	ok := 0
	for i := range 40 {
		wg.Add(1)
		go func(org uuid.UUID) {
			defer wg.Done()
			_, txn, _, err := f.credits.Debit(ctx, org, debit(usd1/2, false, ""))
			if errors.Is(err, ErrInsufficientCredits) {
				return
			}
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			// Moved money lands in clientA's own balance, which pays first, so count only the agency's part.
			taken -= txn.AgencyDelta
			ok++
			mu.Unlock()
		}([]uuid.UUID{f.clientA, f.clientB}[i%2])
	}
	// Moves race the charges for the same balance.
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tr, err := f.ai.Transfer(ctx, f.agency, f.clientA, usd1, f.period(), "", uuid.Nil)
			if errors.Is(err, ErrAgencyAIBalanceShort) {
				return
			}
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			taken += tr.Amount
			mu.Unlock()
		}()
	}
	wg.Wait()
	l := f.ledger()
	if taken+l.Allowance != 10*usd1 || l.Allowance < 0 {
		t.Fatalf("took %d with %d left of a $10 allowance (%d charges)", taken, l.Allowance, ok)
	}
	var logged models.Micro
	if err := f.handle.QueryRow(ctx, `SELECT COALESCE(-SUM(amount_micro), 0) FROM agency_ai_transactions WHERE agency_id = $1 AND kind IN ('spend', 'transfer')`, f.agency).Scan(&logged); err != nil || logged != taken {
		t.Fatalf("ledger rows %d, taken %d (%v)", logged, taken, err)
	}
}
