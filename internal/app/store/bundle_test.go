package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
)

// paidAhead fails when GraphKit started a mailbox's period that the workspace had not paid for.
func paidAhead(t *testing.T, h *harness) {
	t.Helper()
	ctx := context.Background()
	for _, m := range h.repo.mailboxes {
		live, err := h.fake.GetMailbox(ctx, m.GraphKitMailboxID)
		if err != nil || live.Status != graphkit.MailboxActive || live.CurrentPeriodEnd == nil {
			continue
		}
		if live.CurrentPeriodEnd.After(m.PaidThrough.Add(time.Hour)) {
			t.Fatalf("%s: GraphKit's period runs to %v, paid only to %v (now %v)", m.Email, live.CurrentPeriodEnd, m.PaidThrough, h.clock.now())
		}
	}
}

// billDaily runs the billing pass once a day for days, checking nothing was ever billed unpaid.
func billDaily(t *testing.T, h *harness, days int) {
	t.Helper()
	for range days {
		_ = h.s.Bill(context.Background())
		paidAhead(t, h)
		h.clock.advance(24 * time.Hour)
	}
}

func bundleKeys(h *harness) []string {
	var out []string
	for _, k := range h.cards.keys {
		if strings.HasPrefix(k, "store:card:store:mailboxes:") {
			out = append(out, k)
		}
	}
	return out
}

func ledgerLines(h *harness, prefix string) []string {
	var out []string
	for _, k := range h.credits.order {
		// Bundle lines name no domain; a checkout's mailbox line does.
		if e := h.credits.charges[k]; e != nil && strings.HasPrefix(e.reason, prefix) && strings.Count(e.reason, " · ") == 2 {
			out = append(out, e.reason)
		}
	}
	return out
}

func TestMailboxesBoughtOnDifferentDaysPayInOneChargeAhead(t *testing.T) {
	h := newHarness(t)
	h.fund(200)
	h.buy(t, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 1))
	h.settle(t)
	h.clock.advance(10 * 24 * time.Hour)
	h.buy(t, domainAndMailboxes("acmeinbox.com", graphkit.ProviderGoogle, 2))
	h.settle(t)
	h.clock.advance(5 * 24 * time.Hour)
	h.buy(t, domainAndMailboxes("acmereach.com", graphkit.ProviderMicrosoft, 1))
	h.settle(t)
	h.credits.balance[h.org] = 0
	h.cards.card = true

	billDaily(t, h, 60)
	if keys := bundleKeys(h); len(keys) != 2 {
		t.Fatalf("card charges for mailboxes = %v, want one a month", keys)
	}
	lines := ledgerLines(h, "Mailboxes · 4 × $3.00")
	if len(lines) != 2 {
		t.Fatalf("bundle lines = %q, want every mailbox in each month's one line", ledgerLines(h, "Mailboxes"))
	}
	for _, m := range h.repo.mailboxes {
		if !m.PaidThrough.After(h.clock.now()) {
			t.Fatalf("%s is not paid ahead: %v", m.Email, m.PaidThrough)
		}
	}
}

func TestFailedBundleStopsBeforeThePeriodAndLatePaymentResumes(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	ctx := context.Background()
	h.buy(t, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 2))
	h.settle(t)
	h.credits.balance[h.org] = 0
	h.cards.card, h.cards.decline = true, "card_declined"
	var start time.Time
	for _, m := range h.repo.mailboxes {
		start = m.PaidThrough
	}
	// From the day before the billing run to the day before the period starts.
	h.clock.t = start.AddDate(0, 0, -6)
	billDaily(t, h, 5)
	// The day before the period: still time to pay, and GraphKit has the cancellation.
	if err := h.s.Bill(ctx); err != nil {
		t.Fatal(err)
	}
	for _, m := range h.repo.mailboxes {
		live, _ := h.fake.GetMailbox(ctx, m.GraphKitMailboxID)
		if !m.UnpaidCancel || live.Status != graphkit.MailboxCancelling || live.CancelsAt == nil || live.CancelsAt.After(start) {
			t.Fatalf("%s: store %+v, GraphKit %+v; want stopped before %v", m.Email, m, live, start)
		}
	}
	if titles := strings.Join(h.notes.titles, "\n"); !strings.Contains(titles, "stop on") {
		t.Fatalf("notices = %q", titles)
	}

	// The card is updated before the cancellation takes effect: charge, then resume.
	h.cards.decline = ""
	if err := h.s.RetryUnpaid(ctx, h.org); err != nil {
		t.Fatal(err)
	}
	for _, m := range h.repo.mailboxes {
		live, _ := h.fake.GetMailbox(ctx, m.GraphKitMailboxID)
		if m.UnpaidCancel || m.CancelsAt != nil || live.Status != graphkit.MailboxActive || live.CancelsAt != nil || !m.PaidThrough.After(start) {
			t.Fatalf("%s after paying: store %+v, GraphKit %+v", m.Email, m, live)
		}
	}
	billDaily(t, h, 5)
}

func TestCrashMidBundleReplaysWithoutChargingTwice(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	h.buy(t, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 2))
	h.settle(t)
	h.credits.balance[h.org] = 0
	h.cards.card = true
	var start time.Time
	for _, m := range h.repo.mailboxes {
		start = m.PaidThrough
	}
	h.clock.t = start.AddDate(0, 0, -5)
	before := h.credits.net(h.org)
	h.credits.failNext = errors.New("connection reset")
	if err := h.s.Bill(context.Background()); err == nil {
		t.Fatal("the crash was swallowed")
	}
	billDaily(t, h, 2)
	if keys := bundleKeys(h); len(keys) != 1 {
		t.Fatalf("card charges = %v, want the bundle's one", keys)
	}
	if got := h.credits.net(h.org) - before; got != 6_000_000 {
		t.Fatalf("debited %d, want the bundle once", got)
	}
}

func TestPriceRiseIsAnnouncedAndCharged(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	ctx := context.Background()
	h.buy(t, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 1))
	h.settle(t)
	var m *models.StoreMailbox
	for _, x := range h.repo.mailboxes {
		m = x
	}
	// GraphKit's next period goes from $2.00 to $2.50; Warmbly's $1.00 margin is kept.
	h.fake.SetNextMonthlyPrice(m.GraphKitMailboxID, 2_500_000)
	if err := h.s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := h.repo.GetMailbox(ctx, h.org, m.ID); got.MonthlyRetailMicros != 3_500_000 {
		t.Fatalf("price = %d, want $3.50", got.MonthlyRetailMicros)
	}
	if body := strings.Join(h.notes.bodies, "\n"); !strings.Contains(body, "from $3.00 to $3.50") {
		t.Fatalf("notices = %q", body)
	}
	h.clock.t = m.PaidThrough.AddDate(0, 0, -5)
	billDaily(t, h, 2)
	if lines := ledgerLines(h, "Mailboxes · 1 × $3.50"); len(lines) != 1 {
		t.Fatalf("bundle lines = %q", ledgerLines(h, "Mailboxes"))
	}
}

func TestReconciliationFlagsAPeriodNobodyCollected(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	ctx := context.Background()
	h.buy(t, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 1))
	h.settle(t)
	if got, _ := h.s.Uncollected(ctx); len(got) != 0 {
		t.Fatalf("a fresh purchase is flagged: %+v", got)
	}
	// GraphKit renews while no billing pass ran.
	h.clock.advance(35 * 24 * time.Hour)
	if err := h.s.refreshPeriods(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := h.s.Uncollected(ctx)
	if err != nil || len(got) != 1 || got[0].Email != "ada@acmeoutreach.com" {
		t.Fatalf("uncollected = %+v, %v", got, err)
	}
}

func TestAutoReloadTopsUpBeforeTheBundle(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	h.cards.card = true
	ctx := context.Background()
	q, err := h.s.CreateQuote(ctx, h.org, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 2))
	if err != nil {
		t.Fatal(err)
	}
	if ar := q.Payment.AutoReload; ar == nil || !ar.Enabled || !ar.StoreManaged || ar.ThresholdMicros != 6_000_000 || ar.AmountMicros != 10_000_000 {
		t.Fatalf("quote reload plan = %+v", q.Payment.AutoReload)
	}
	h.buy(t, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 2))
	h.settle(t)
	cfg := h.cards.settings(h.org)
	if !cfg.AutoReloadEnabled || cfg.AutoReloadThreshold != 6_000_000 || cfg.AutoReloadAmount != 10_000_000 {
		t.Fatalf("reload after the first purchase = %+v", cfg)
	}
	h.credits.balance[h.org] = 0
	var start time.Time
	for _, m := range h.repo.mailboxes {
		start = m.PaidThrough
	}
	// Two days before the run the balance is topped up; the run then needs no card. The top-up
	// also covers what keeps the balance at the threshold after the run ($10 + $2), so the
	// balance's own reload has nothing to add.
	h.clock.t = start.AddDate(0, 0, -6).Add(-24 * time.Hour)
	billDaily(t, h, 3)
	var reloads []string
	for _, k := range h.cards.keys {
		if strings.HasPrefix(k, "store:card:reload:") {
			reloads = append(reloads, k)
		}
	}
	if len(reloads) != 1 || len(h.cards.topUps) == 0 || h.cards.topUps[len(h.cards.topUps)-1] != 12_000_000 {
		t.Fatalf("reload charges = %v, top-ups %v", reloads, h.cards.topUps)
	}
	if keys := bundleKeys(h); len(keys) != 0 {
		t.Fatalf("the bundle charged the card: %v", keys)
	}
	if lines := ledgerLines(h, "Mailboxes · 2 × $3.00"); len(lines) != 1 {
		t.Fatalf("bundle lines = %q", ledgerLines(h, "Mailboxes"))
	}
}

func TestHandSetAutoReloadIsNeverOverwritten(t *testing.T) {
	h := newHarness(t)
	h.fund(80)
	h.cards.card = true
	ctx := context.Background()
	h.buy(t, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 1))
	h.settle(t)
	// The workspace sets its own reload.
	if _, err := h.cards.SetAutoReload(ctx, h.org, true, 20_000_000, 50_000_000, 200_000_000); err != nil {
		t.Fatal(err)
	}
	h.buy(t, domainAndMailboxes("acmeinbox.com", graphkit.ProviderGoogle, 3))
	h.settle(t)
	if err := h.s.Bill(ctx); err != nil {
		t.Fatal(err)
	}
	cfg := h.cards.settings(h.org)
	if cfg.AutoReloadThreshold != 20_000_000 || cfg.AutoReloadAmount != 50_000_000 {
		t.Fatalf("hand-set reload changed to %+v", cfg)
	}
	if acct, _ := h.repo.GetAccount(ctx, h.org); acct.ReloadManaged {
		t.Fatal("the store still manages a hand-set reload")
	}
}

func TestOptingOutLeavesAutoReloadOff(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	h.cards.card = true
	ctx := context.Background()
	q, err := h.s.CreateQuote(ctx, h.org, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 1))
	if err != nil {
		t.Fatal(err)
	}
	off, due := false, q.DueNowMicros
	if _, err := h.s.PlaceOrder(ctx, h.org, h.user, OrderRequest{QuoteID: q.ID, ExpectedDueNowMicros: &due, EnableAutoReload: &off}); err != nil {
		t.Fatal(err)
	}
	if cfg := h.cards.settings(h.org); cfg.AutoReloadEnabled {
		t.Fatalf("reload turned on against the request: %+v", cfg)
	}
}
