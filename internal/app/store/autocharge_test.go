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

// The harness's cards know nothing of automatic reload, as the real path no longer asks it.
func TestShortfallIsChargedToTheCardWithoutAutoReload(t *testing.T) {
	h := newHarness(t)
	h.fund(5)
	h.cards.card = true
	_, o := h.buy(t, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 2))
	due := int64(11_170_000 + 2*3_000_000)
	if o.ChargedMicros != due {
		t.Fatalf("charged %d, want %d", o.ChargedMicros, due)
	}
	if len(h.cards.topUps) != 1 || int64(h.cards.topUps[0]) != due-5_000_000 || h.credits.balance[h.org] != 0 {
		t.Fatalf("card charges %v, balance %d: want exactly the shortfall", h.cards.topUps, h.credits.balance[h.org])
	}
	if !strings.HasPrefix(h.cards.keys[0], "store:card:store:order:") {
		t.Fatalf("card key %q does not derive from the order's charge key", h.cards.keys[0])
	}
}

func TestCrashBetweenCardAndDebitNeverChargesTheCardTwice(t *testing.T) {
	h := newHarness(t)
	h.cards.card = true
	ctx := context.Background()
	q, err := h.s.CreateQuote(ctx, h.org, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 1))
	if err != nil {
		t.Fatal(err)
	}
	due := q.DueNowMicros
	h.credits.failNext = errors.New("connection reset")
	if _, err := h.s.PlaceOrder(ctx, h.org, h.user, OrderRequest{QuoteID: q.ID, ExpectedDueNowMicros: &due}); err == nil {
		t.Fatal("the debit failure was swallowed")
	}
	if len(h.cards.topUps) != 1 || int64(h.credits.balance[h.org]) != due {
		t.Fatalf("after the crash: card %v, balance %d; want the card charged once and the money on the balance", h.cards.topUps, h.credits.balance[h.org])
	}
	o, err := h.s.PlaceOrder(ctx, h.org, h.user, OrderRequest{QuoteID: q.ID, ExpectedDueNowMicros: &due})
	if err != nil || o.ChargedMicros != due {
		t.Fatalf("retry = %+v, %v", o, err)
	}
	if len(h.cards.topUps) != 1 || h.credits.balance[h.org] != 0 || int64(h.credits.net(h.org)) != due {
		t.Fatalf("after the retry: card %v, balance %d, net %d; want one card charge and one debit", h.cards.topUps, h.credits.balance[h.org], h.credits.net(h.org))
	}
}

func TestDeclinedRenewalRetriesOnceTheCardIsUpdated(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	ctx := context.Background()
	d := purchasedDomain(t, h, "acmeoutreach.com")
	h.credits.balance[h.org] = 0
	h.cards.card, h.cards.decline = true, "card_declined"
	// Five days out the renewal is charged at once, with nobody present.
	exp := h.clock.now().Add(5 * 24 * time.Hour)
	h.fake.ExpireDomain(d.GraphKitDomainID, exp)
	live, _ := h.fake.GetDomain(ctx, d.GraphKitDomainID)
	if _, err := h.s.upsertDomain(ctx, h.org, nil, live); err != nil {
		t.Fatal(err)
	}
	if err := h.s.Bill(ctx); err != nil {
		t.Fatal(err)
	}
	after, _ := h.repo.GetDomain(ctx, h.org, d.ID)
	if !after.ExpiresAt.Equal(exp) || after.RenewalFailures != 1 {
		t.Fatalf("after the decline: expiry %v failures %d", after.ExpiresAt, after.RenewalFailures)
	}
	var notice string
	for i, title := range h.notes.titles {
		if strings.Contains(title, "renewal payment failed") {
			notice = h.notes.bodies[i]
		}
	}
	if !strings.Contains(notice, "declined") || !strings.Contains(notice, "Update your card") {
		t.Fatalf("notice = %q, want the decline and the way to fix it", notice)
	}

	// The card is replaced: the renewal goes through now, not tomorrow.
	h.cards.decline = ""
	if err := h.s.RetryUnpaid(ctx, h.org); err != nil {
		t.Fatal(err)
	}
	after, _ = h.repo.GetDomain(ctx, h.org, d.ID)
	if !after.ExpiresAt.After(exp) || len(h.cards.keys) != 2 || h.cards.keys[0] == h.cards.keys[1] {
		t.Fatalf("after the card update: expiry %v, card keys %v; want renewed under a fresh key", after.ExpiresAt, h.cards.keys)
	}
	if h.credits.net(h.org) != 2*11_170_000 {
		t.Fatalf("net = %d, want registration plus one renewal", h.credits.net(h.org))
	}
}

func TestDeclinedMailboxMonthRetriesOnceTheCardIsUpdated(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	ctx := context.Background()
	h.buy(t, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 2))
	h.settle(t)
	h.credits.balance[h.org] = 0
	h.cards.card, h.cards.decline = true, "authentication_required"
	h.clock.advance(32 * 24 * time.Hour)
	before := h.credits.net(h.org)
	if err := h.s.Bill(ctx); err != nil {
		t.Fatal(err)
	}
	if h.credits.net(h.org) != before {
		t.Fatal("a declined month was debited")
	}
	if body := strings.Join(h.notes.bodies, "\n"); !strings.Contains(body, "bank asked to confirm") {
		t.Fatalf("notices = %q", body)
	}
	h.cards.decline = ""
	if err := h.s.RetryUnpaid(ctx, h.org); err != nil {
		t.Fatal(err)
	}
	if got := h.credits.net(h.org) - before; got != models.Micro(2*3_000_000) {
		t.Fatalf("charged %d after the card update, want the month", got)
	}
}

func TestCheckoutResumesAfterCardSetupOnlyAtTheSameTotal(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	q, err := h.s.CreateQuote(ctx, h.org, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 1))
	if err != nil {
		t.Fatal(err)
	}
	due := q.DueNowMicros
	var method *PaymentMethodError
	if _, err := h.s.PlaceOrder(ctx, h.org, h.user, OrderRequest{QuoteID: q.ID, ExpectedDueNowMicros: &due}); !errors.As(err, &method) {
		t.Fatalf("no card: %v", err)
	}
	// Back from card setup, the dashboard places the same quote at the total it showed.
	h.cards.card = true
	o, err := h.s.PlaceOrder(ctx, h.org, h.user, OrderRequest{QuoteID: q.ID, ExpectedDueNowMicros: &due})
	if err != nil || o.ChargedMicros != due || len(h.cards.topUps) != 1 {
		t.Fatalf("resumed order = %+v, %v; card %v", o, err, h.cards.topUps)
	}

	// A quote whose price moved while the buyer was away waits for them.
	q2, err := h.s.CreateQuote(ctx, h.org, domainAndMailboxes("acmeinbox.com", graphkit.ProviderGoogle, 1))
	if err != nil {
		t.Fatal(err)
	}
	h.clock.advance(20 * time.Minute)
	four := int64(4_000_000)
	h.retail.r.MailboxMonthlyMicros = &four
	shown := q2.DueNowMicros
	var changed *PriceChangedError
	if _, err := h.s.PlaceOrder(ctx, h.org, h.user, OrderRequest{QuoteID: q2.ID, ExpectedDueNowMicros: &shown}); !errors.As(err, &changed) {
		t.Fatalf("changed price: %v", err)
	}
	if len(h.cards.topUps) != 1 {
		t.Fatalf("charged %v on a changed price", h.cards.topUps)
	}
}

// The plan's AI allowance is not money the workspace added: a store charge never draws on it.
func TestStoreChargesNeverSpendTheAIAllowance(t *testing.T) {
	h := newHarness(t)
	h.cards.card = true
	h.credits.monthly[h.org] = models.USDToMicro(20)
	ctx := context.Background()
	q, err := h.s.CreateQuote(ctx, h.org, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 1))
	if err != nil {
		t.Fatal(err)
	}
	if p := q.Payment; p.BalanceMicros != 0 || p.FromBalanceMicros != 0 || p.CardChargeMicros != 14_170_000 || p.BalanceAfterMicros != 0 {
		t.Fatalf("quote payment = %+v, want the allowance left out", p)
	}
	due := q.DueNowMicros
	if _, err := h.s.PlaceOrder(ctx, h.org, h.user, OrderRequest{QuoteID: q.ID, ExpectedDueNowMicros: &due}); err != nil {
		t.Fatal(err)
	}
	if len(h.cards.topUps) != 1 || h.cards.topUps[0] != 14_170_000 {
		t.Fatalf("card charges = %v, want the whole order", h.cards.topUps)
	}
	if h.credits.monthly[h.org] != models.USDToMicro(20) || h.credits.balance[h.org] != 0 {
		t.Fatalf("allowance %d, purchased %d: want the allowance untouched", h.credits.monthly[h.org], h.credits.balance[h.org])
	}
}

func TestStoreChargesComeFromThePurchasedPool(t *testing.T) {
	h := newHarness(t)
	h.cards.card = true
	h.fund(20)
	h.credits.monthly[h.org] = models.USDToMicro(20)
	ctx := context.Background()
	q, err := h.s.CreateQuote(ctx, h.org, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 1))
	if err != nil {
		t.Fatal(err)
	}
	if p := q.Payment; p.BalanceMicros != 20_000_000 || p.FromBalanceMicros != 14_170_000 || p.CardChargeMicros != 0 || p.BalanceAfterMicros != 5_830_000 {
		t.Fatalf("quote payment = %+v", p)
	}
	due := q.DueNowMicros
	if _, err := h.s.PlaceOrder(ctx, h.org, h.user, OrderRequest{QuoteID: q.ID, ExpectedDueNowMicros: &due}); err != nil {
		t.Fatal(err)
	}
	if len(h.cards.topUps) != 0 || h.credits.balance[h.org] != 5_830_000 || h.credits.monthly[h.org] != models.USDToMicro(20) {
		t.Fatalf("card %v, purchased %d, allowance %d", h.cards.topUps, h.credits.balance[h.org], h.credits.monthly[h.org])
	}
}
