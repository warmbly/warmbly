package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/delegation"
	"github.com/warmbly/warmbly/internal/app/instancesettings"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
)

type tclock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *tclock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *tclock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type testRetail struct{ r instancesettings.StoreRetail }

func (t *testRetail) StoreRetail(context.Context) instancesettings.StoreRetail { return t.r }

type memDelegator struct {
	mu       sync.Mutex
	calls    []string
	brokered []bool
	// googleID is the instance's own Google service account; empty means none.
	googleID string
	// signatures are the Gmail signatures set, as "address html"; signatureFail refuses that many first.
	signatures    []string
	signatureFail int
	// awaitConsent answers every connect as a tenant still waiting on its administrator's consent.
	awaitConsent bool
	// consents are the tenant consents started, as "domain returnURL".
	consents []string
	// autoConsent answers the automatic tenant consent; nil approves the tenant, which ends awaitConsent.
	autoConsent error
	// autoConsents are the automatic consents run, as "domain admin password-given".
	autoConsents []string
}

func (d *memDelegator) ConsentPurchasedTenantWithAdmin(_ context.Context, _, _ uuid.UUID, domain, admin, password string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.autoConsents = append(d.autoConsents, fmt.Sprintf("%s %s %t", domain, admin, password != ""))
	if d.autoConsent != nil {
		return d.autoConsent
	}
	d.awaitConsent = false
	return nil
}

func (d *memDelegator) StartPurchasedMicrosoftConsent(_ context.Context, _, _ uuid.UUID, domain, returnURL string) (*delegation.MicrosoftConsent, *errx.Error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.consents = append(d.consents, domain+" "+returnURL)
	return &delegation.MicrosoftConsent{URL: "https://login.example/adminconsent?state=x", ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func (d *memDelegator) ConnectPurchased(_ context.Context, _, _ uuid.UUID, provider, address, _, _ string, brokered bool) (*models.Email, *errx.Error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, provider+":"+address)
	if d.awaitConsent {
		d.brokered = append(d.brokered, brokered)
		return nil, errx.NewWithIdentifier(errx.Unprocessable, delegation.ErrIDMicrosoftTenantConsent, "approve it")
	}
	d.brokered = append(d.brokered, brokered)
	return &models.Email{ID: uuid.New(), Email: address}, nil
}

func (d *memDelegator) SetPurchasedSignature(_ context.Context, _ uuid.UUID, address, html string, brokered bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.signatureFail > 0 {
		d.signatureFail--
		return errors.New("gmail unavailable")
	}
	d.signatures = append(d.signatures, address+" "+html)
	return nil
}

func (d *memDelegator) GoogleAppIdentity() (string, []string, bool) {
	if d.googleID == "" {
		return "", nil, false
	}
	return d.googleID, []string{"https://mail.google.com/"}, true
}

func (d *memDelegator) MicrosoftAppIdentity() (string, []string, bool) {
	return "00000000-0000-0000-0000-000000000001", []string{"Mail.ReadWrite", "Mail.Send", "User.Read.All"}, true
}

type memNotifier struct {
	mu     sync.Mutex
	titles []string
	bodies []string
}

func (n *memNotifier) NotifyOrg(_ context.Context, _ uuid.UUID, _ models.OrganizationPermission, _ uuid.UUID, _ models.NotificationCategory, title, body, _ string, _ map[string]any, _ string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.titles, n.bodies = append(n.titles, title), append(n.bodies, body)
}

type harness struct {
	s         *Service
	fake      *graphkit.Fake
	clock     *tclock
	repo      *memRepo
	credits   *memCredits
	cards     *memCards
	retail    *testRetail
	delegator *memDelegator
	notes     *memNotifier
	org, user uuid.UUID
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{fake: graphkit.NewFake(), clock: &tclock{t: time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)}, repo: newMemRepo(),
		credits: newMemCredits(), retail: &testRetail{r: instancesettings.DefaultStoreRetail()}, delegator: &memDelegator{},
		notes: &memNotifier{}, org: uuid.New(), user: uuid.New()}
	h.fake.SetClock(h.clock.now)
	h.repo.now = h.clock.now
	h.cards = &memCards{credits: h.credits}
	h.s = New(Deps{API: h.fake, Repo: h.repo, Credits: h.credits, Cards: h.cards, Retail: h.retail,
		Delegator: h.delegator, Notifier: h.notes, Now: h.clock.now})
	ctx := context.Background()
	if err := h.s.PushDelegation(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := h.s.PutRegistrant(ctx, h.org, testRegistrant()); err != nil {
		t.Fatal(err)
	}
	return h
}

func testRegistrant() Registrant {
	return Registrant{FirstName: "Ada", LastName: "Lovelace", Organization: "Acme Inc", Email: "ada@acme.test",
		Phone: "+1.4155550100", Address1: "1 Market St", City: "San Francisco", State: "CA", PostalCode: "94105", Country: "us"}
}

func (h *harness) fund(usd float64) { h.credits.balance[h.org] = models.USDToMicro(usd) }

func domainAndMailboxes(domain, provider string, n int) QuoteRequest {
	req := QuoteRequest{Items: []graphkit.QuoteItem{{Type: graphkit.ItemDomain, Domain: domain, Years: 1}}}
	if n > 0 {
		var boxes []graphkit.NewMailbox
		for _, lp := range []string{"ada", "bob", "cy", "dee", "eve"}[:n] {
			boxes = append(boxes, graphkit.NewMailbox{LocalPart: lp, FirstName: "Ada", LastName: "Lovelace"})
		}
		req.Items = append(req.Items, graphkit.QuoteItem{Type: graphkit.ItemMailboxes, Domain: domain, Provider: provider, Mailboxes: boxes})
	}
	return req
}

func (h *harness) buy(t *testing.T, req QuoteRequest) (*Quote, *Order) {
	t.Helper()
	ctx := context.Background()
	q, err := h.s.CreateQuote(ctx, h.org, req)
	if err != nil {
		t.Fatalf("quote: %v", err)
	}
	due := q.DueNowMicros
	o, err := h.s.PlaceOrder(ctx, h.org, h.user, OrderRequest{QuoteID: q.ID, ExpectedDueNowMicros: &due})
	if err != nil {
		t.Fatalf("order: %v", err)
	}
	return q, o
}

// settle lets the simulated order finish and runs the reconciler.
func (h *harness) settle(t *testing.T) {
	t.Helper()
	h.clock.advance(25 * time.Second)
	if err := h.s.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	// connectLater runs off the request: let it finish before the reconciler looks again.
	h.s.connects.Wait()
	if err := h.s.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
}

func code(err error) string {
	var xerr *errx.Error
	if errors.As(err, &xerr) {
		return xerr.ResponseCode()
	}
	return ""
}

func TestOrderChargesOncePerLineAndRetriesSafely(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	ctx := context.Background()
	q, o := h.buy(t, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 2))
	want := int64(11_170_000 + 2*3_000_000)
	if q.DueNowMicros != want || o.ChargedMicros != want || int64(h.credits.net(h.org)) != want {
		t.Fatalf("due=%d charged=%d net=%d, want %d", q.DueNowMicros, o.ChargedMicros, h.credits.net(h.org), want)
	}
	if len(h.credits.charges) != 2 {
		t.Fatalf("ledger lines = %d, want one per quote line", len(h.credits.charges))
	}
	for _, e := range h.credits.charges {
		if strings.Contains(strings.ToLower(e.reason), "graphkit") {
			t.Fatalf("ledger line names the upstream: %q", e.reason)
		}
	}
	// A retried checkout of the same quote returns the same order and charges nothing more.
	due := q.DueNowMicros
	again, err := h.s.PlaceOrder(ctx, h.org, h.user, OrderRequest{QuoteID: q.ID, ExpectedDueNowMicros: &due})
	if err != nil || again.ID != o.ID || int64(h.credits.net(h.org)) != want {
		t.Fatalf("retry = %v, %v; net %d", again, err, h.credits.net(h.org))
	}
	h.settle(t)
	got, err := h.s.GetOrder(ctx, h.org, o.ID)
	if err != nil || got.Status != models.StoreOrderCompleted || len(got.Domains) != 1 || len(got.Mailboxes) != 2 {
		t.Fatalf("order = %+v, %v", got, err)
	}
	for _, m := range got.Mailboxes {
		if m.ConnectState != models.StoreConnectConnected || m.EmailAccountID == nil {
			t.Fatalf("mailbox %s not auto-connected: %+v", m.Email, m)
		}
	}
	if len(h.delegator.calls) != 2 || !strings.HasPrefix(h.delegator.calls[0], "google:") {
		t.Fatalf("connect calls = %v", h.delegator.calls)
	}
}

func TestOrderRefusesATotalTheBuyerDidNotSee(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	ctx := context.Background()
	q, err := h.s.CreateQuote(ctx, h.org, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 1))
	if err != nil {
		t.Fatal(err)
	}
	seen := q.DueNowMicros - 10_000
	_, err = h.s.PlaceOrder(ctx, h.org, h.user, OrderRequest{QuoteID: q.ID, ExpectedDueNowMicros: &seen})
	var pc *PriceChangedError
	if !errors.As(err, &pc) || pc.Quote.DueNowMicros != q.DueNowMicros {
		t.Fatalf("err = %v, want price_changed with the current quote", err)
	}
	if h.credits.net(h.org) != 0 || len(h.repo.orders) != 0 {
		t.Fatalf("charged %d with %d orders, want nothing", h.credits.net(h.org), len(h.repo.orders))
	}
}

func TestExpiredQuoteIsRepricedBeforeCharging(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	ctx := context.Background()
	q, _ := h.s.CreateQuote(ctx, h.org, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 1))
	seen := q.DueNowMicros
	h.clock.advance(16 * time.Minute)
	// The price moved while the quote sat: refused with the fresh quote, nothing charged.
	pct := 10
	h.retail.r.DomainMarkupPercent = &pct
	_, err := h.s.PlaceOrder(ctx, h.org, h.user, OrderRequest{QuoteID: q.ID, ExpectedDueNowMicros: &seen})
	var pc *PriceChangedError
	if !errors.As(err, &pc) || pc.Quote.DueNowMicros == seen || pc.Quote.ID == q.ID {
		t.Fatalf("err = %v, want price_changed with a new quote", err)
	}
	if h.credits.net(h.org) != 0 {
		t.Fatalf("charged %d before the buyer saw the new price", h.credits.net(h.org))
	}
	// Ordering the fresh quote at the total it shows goes through.
	fresh := pc.Quote.DueNowMicros
	o, err := h.s.PlaceOrder(ctx, h.org, h.user, OrderRequest{QuoteID: pc.Quote.ID, ExpectedDueNowMicros: &fresh})
	if err != nil || int64(h.credits.net(h.org)) != fresh || o.Status != models.StoreOrderProcessing {
		t.Fatalf("order = %+v, %v; net %d want %d", o, err, h.credits.net(h.org), fresh)
	}
}

func TestShortBalanceNeedsACardAndTopsUpByTheMinimum(t *testing.T) {
	h := newHarness(t)
	h.fund(5)
	ctx := context.Background()
	q, _ := h.s.CreateQuote(ctx, h.org, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 1))
	// A $9.17 shortfall puts the $10 minimum on the card, and $0.83 stays on the balance.
	if !q.Payment.NeedsPaymentMethod || q.Payment.CardChargeMicros != 10_000_000 || q.Payment.BalanceAfterMicros != 830_000 {
		t.Fatalf("payment = %+v", q.Payment)
	}
	due := q.DueNowMicros
	_, err := h.s.PlaceOrder(ctx, h.org, h.user, OrderRequest{QuoteID: q.ID, ExpectedDueNowMicros: &due})
	var pm *PaymentMethodError
	if !errors.As(err, &pm) || h.credits.net(h.org) != 0 || len(h.repo.orders) != 0 {
		t.Fatalf("err = %v net = %d orders = %d, want payment_method_required and nothing charged", err, h.credits.net(h.org), len(h.repo.orders))
	}
	h.cards.card = true
	o, err := h.s.PlaceOrder(ctx, h.org, h.user, OrderRequest{QuoteID: q.ID, ExpectedDueNowMicros: &due})
	if err != nil || o.ChargedMicros != due {
		t.Fatalf("order = %+v, %v", o, err)
	}
	if len(h.cards.topUps) != 1 || h.cards.topUps[0] != 10_000_000 || h.credits.balance[h.org] != 830_000 {
		t.Fatalf("top-ups = %v, balance %d", h.cards.topUps, h.credits.balance[h.org])
	}
}

func TestUpstreamRefusalRefundsTheWholeOrder(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	ctx := context.Background()
	h.fake.SetBalance(0)
	q, _ := h.s.CreateQuote(ctx, h.org, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 2))
	due := q.DueNowMicros
	_, err := h.s.PlaceOrder(ctx, h.org, h.user, OrderRequest{QuoteID: q.ID, ExpectedDueNowMicros: &due})
	if code(err) != CodeUnavailable {
		t.Fatalf("err = %v, want store_unavailable", err)
	}
	if h.credits.net(h.org) != 0 || h.credits.balance[h.org] != models.USDToMicro(50) {
		t.Fatalf("net %d balance %d, want everything refunded", h.credits.net(h.org), h.credits.balance[h.org])
	}
	for _, o := range h.repo.orders {
		if o.Status != models.StoreOrderFailed {
			t.Fatalf("order status = %s", o.Status)
		}
	}
}

func TestFailedItemRefundsExactlyItsLine(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	_, o := h.buy(t, domainAndMailboxes("acme-fail-dns.com", graphkit.ProviderGoogle, 3))
	h.settle(t)
	got, _ := h.s.GetOrder(context.Background(), h.org, o.ID)
	if got.Status != models.StoreOrderPartiallyFailed {
		t.Fatalf("status = %s", got.Status)
	}
	// The domain stays and is paid for; the three mailboxes are refunded once, however often the order is applied.
	if h.credits.net(h.org) != 11_170_000 || got.RefundedMicros != 9_000_000 {
		t.Fatalf("net %d refunded %d", h.credits.net(h.org), got.RefundedMicros)
	}
	if err := h.s.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ = h.s.GetOrder(context.Background(), h.org, o.ID)
	if got.RefundedMicros != 9_000_000 || h.credits.net(h.org) != 11_170_000 {
		t.Fatalf("after a second pass: net %d refunded %d", h.credits.net(h.org), got.RefundedMicros)
	}
}

func TestUnmeteredInstanceChargesNothing(t *testing.T) {
	h := newHarness(t)
	h.credits.unmetered = true
	_, o := h.buy(t, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderMicrosoft, 1))
	if o.ChargedMicros != 0 || len(h.credits.charges) != 0 {
		t.Fatalf("charged %d", o.ChargedMicros)
	}
}

func purchasedDomain(t *testing.T, h *harness, name string) *models.StoreDomain {
	t.Helper()
	h.buy(t, domainAndMailboxes(name, graphkit.ProviderGoogle, 0))
	h.settle(t)
	for _, d := range h.repo.domains {
		if d.Name == name {
			return cp(d)
		}
	}
	t.Fatalf("%s was not recorded", name)
	return nil
}

func TestRenewalChargesTheCurrentPriceOnce(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	ctx := context.Background()
	d := purchasedDomain(t, h, "acmeoutreach.com")
	h.fake.SetRenewPrice(d.GraphKitDomainID, 12_000_000)
	stale := cp(d)
	got, err := h.s.RenewNow(ctx, h.org, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.ExpiresAt.After(*d.ExpiresAt) || got.RenewMicros != 12_000_000 {
		t.Fatalf("renewed = %+v", got)
	}
	paid := h.credits.net(h.org)
	if paid != 11_170_000+12_000_000 {
		t.Fatalf("net = %d, want registration + the new renewal price", paid)
	}
	// A pass that crashed after renewing retries from the expiry it knew: same key, no second charge, no second year.
	if _, err := h.s.renew(ctx, stale, false); err != nil {
		t.Fatal(err)
	}
	after, _ := h.repo.GetDomain(ctx, h.org, d.ID)
	if h.credits.net(h.org) != paid || !after.ExpiresAt.Equal(*got.ExpiresAt) {
		t.Fatalf("retry: net %d expiry %v, want %d and %v", h.credits.net(h.org), after.ExpiresAt, paid, got.ExpiresAt)
	}
}

func TestRenewalAdoptsARenewalThatAlreadyHappened(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	ctx := context.Background()
	d := purchasedDomain(t, h, "acmeoutreach.com")
	// The renewal went through upstream under a key this attempt does not share (it outlived the replay window).
	if _, err := h.fake.RenewDomain(ctx, d.GraphKitDomainID, 1, *d.ExpiresAt, "elsewhere"); err != nil {
		t.Fatal(err)
	}
	got, err := h.s.RenewNow(ctx, h.org, d.ID)
	if err != nil {
		t.Fatalf("renew: %v", err)
	}
	if !got.ExpiresAt.Equal(d.ExpiresAt.AddDate(1, 0, 0)) {
		t.Fatalf("expiry = %v, want the adopted one", got.ExpiresAt)
	}
	if h.credits.net(h.org) != 11_170_000+11_170_000 {
		t.Fatalf("net = %d, want one renewal charge", h.credits.net(h.org))
	}
}

func TestRenewalRefusalRefundsAndNextAttemptIsANewCharge(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	ctx := context.Background()
	d := purchasedDomain(t, h, "acme-notdue.com")
	for i := range 2 {
		_, err := h.s.RenewNow(ctx, h.org, d.ID)
		if code(err) != CodeRenewalNotDue {
			t.Fatalf("attempt %d: %v, want renewal_not_due", i, err)
		}
		if h.credits.net(h.org) != 11_170_000 {
			t.Fatalf("attempt %d: net %d, want the renewal refunded", i, h.credits.net(h.org))
		}
	}
	keys := 0
	for k := range h.credits.charges {
		if strings.HasPrefix(k, "store:renew:") {
			keys++
		}
	}
	if keys != 2 {
		t.Fatalf("renewal keys = %d, want a fresh one per refused attempt", keys)
	}
}

func TestSweepNoticesBeforeChargingAndStatesThePrice(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	ctx := context.Background()
	d := purchasedDomain(t, h, "acmeoutreach.com")
	exp := h.clock.now().Add(35 * 24 * time.Hour)
	h.fake.ExpireDomain(d.GraphKitDomainID, exp)
	h.fake.SetRenewPrice(d.GraphKitDomainID, 12_500_000)
	live, _ := h.fake.GetDomain(ctx, d.GraphKitDomainID)
	if _, err := h.s.upsertDomain(ctx, h.org, nil, live); err != nil {
		t.Fatal(err)
	}
	if err := h.s.Bill(ctx); err != nil {
		t.Fatal(err)
	}
	if h.credits.net(h.org) != 11_170_000 {
		t.Fatalf("charged before the notice had a week: net %d", h.credits.net(h.org))
	}
	body := strings.Join(h.notes.bodies, "\n")
	if !strings.Contains(body, "$12.50") || !strings.Contains(body, "went up from $11.17") {
		t.Fatalf("notice = %q, want the current price and that it went up", body)
	}
	h.clock.advance(8 * 24 * time.Hour)
	if err := h.s.Bill(ctx); err != nil {
		t.Fatal(err)
	}
	if h.credits.net(h.org) != 11_170_000+12_500_000 {
		t.Fatalf("net after the notice week = %d", h.credits.net(h.org))
	}
	if err := h.s.Bill(ctx); err != nil {
		t.Fatal(err)
	}
	if h.credits.net(h.org) != 11_170_000+12_500_000 {
		t.Fatalf("a second pass charged again: net %d", h.credits.net(h.org))
	}
}

func TestMonthlyMailboxesBillAsOneLineOnce(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	ctx := context.Background()
	h.buy(t, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 3))
	h.settle(t)
	start := h.credits.net(h.org)
	h.clock.advance(32 * 24 * time.Hour)
	// A pass that stamped its key and charged, then crashed before moving paid_through.
	var rows []models.StoreMailbox
	for _, m := range h.repo.mailboxes {
		rows = append(rows, *m)
	}
	key := "store:mailboxes:crashed"
	if _, err := h.repo.ReserveMailboxCharge(ctx, h.org, rows, key); err != nil {
		t.Fatal(err)
	}
	if _, err := h.credits.ChargeAmount(ctx, h.org, 9_000_000, "Mailboxes · 3 × $3.00 · Dec 2026", key); err != nil {
		t.Fatal(err)
	}
	// That pass was the billing run: the workspace's next run is a month on.
	acct := h.repo.accounts[h.org]
	if err := h.repo.SetBillAnchor(ctx, h.org, acct.BillDay, anchorAfter(acct.BillDay, *acct.NextBillAnchor)); err != nil {
		t.Fatal(err)
	}
	h.clock.advance(replayAfter + time.Minute)
	for range 2 {
		if err := h.s.Bill(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if got := h.credits.net(h.org) - start; got != 9_000_000 {
		t.Fatalf("month charged %d, want 9.00 once", got)
	}
	for _, m := range h.repo.mailboxes {
		if m.PendingChargeKey != nil || !m.PaidThrough.After(h.clock.now()) {
			t.Fatalf("mailbox not settled: %+v", m)
		}
	}
	h.clock.advance(31 * 24 * time.Hour)
	if err := h.s.Bill(ctx); err != nil {
		t.Fatal(err)
	}
	var reasons []string
	for _, k := range h.credits.order {
		if strings.HasPrefix(k, "store:mailboxes:") && k != key {
			reasons = append(reasons, h.credits.charges[k].reason)
		}
	}
	if len(reasons) != 1 || !strings.HasPrefix(reasons[0], "Mailboxes · 3 × $3.00 · ") {
		t.Fatalf("next month's ledger lines = %v", reasons)
	}
}

func TestUnpaidMailboxesStopBeforeTheirNextPeriod(t *testing.T) {
	h := newHarness(t)
	h.fund(20)
	ctx := context.Background()
	h.buy(t, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 1))
	h.settle(t)
	h.credits.balance[h.org] = 0
	var m *models.StoreMailbox
	for _, x := range h.repo.mailboxes {
		m = x
	}
	start := m.PaidThrough
	h.clock.t = start.AddDate(0, 0, -6)
	for range 8 {
		if err := h.s.Bill(ctx); err != nil {
			t.Fatal(err)
		}
		h.clock.advance(24 * time.Hour)
	}
	got, _ := h.repo.GetMailbox(ctx, h.org, m.ID)
	if !got.UnpaidCancel || got.CancelsAt == nil || got.CancelsAt.After(start) {
		t.Fatalf("mailbox = %+v, want cancelled at GraphKit for its unpaid period starting %v", got, start)
	}
	live, _ := h.fake.GetMailbox(ctx, m.GraphKitMailboxID)
	if live.Status != graphkit.MailboxCancelled && (live.CancelsAt == nil || live.CancelsAt.After(start)) {
		t.Fatalf("GraphKit mailbox = %+v, want stopped before the unpaid period", live)
	}
	titles := strings.Join(h.notes.titles, "\n")
	for _, want := range []string{"Mailbox payment failed", "stop on"} {
		if !strings.Contains(titles, want) {
			t.Fatalf("notices = %q, missing %q", titles, want)
		}
	}
}

func TestReadsAreScopedToTheWorkspace(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	ctx := context.Background()
	_, o := h.buy(t, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 1))
	h.settle(t)
	other := uuid.New()
	var domainID, mailboxID uuid.UUID
	for id := range h.repo.domains {
		domainID = id
	}
	for id := range h.repo.mailboxes {
		mailboxID = id
	}
	checks := map[string]error{}
	_, checks["order"] = h.s.GetOrder(ctx, other, o.ID)
	_, checks["domain"] = h.s.GetDomain(ctx, other, domainID)
	_, checks["mailbox"] = h.s.GetMailbox(ctx, other, mailboxID)
	_, checks["renew"] = h.s.RenewNow(ctx, other, domainID)
	_, checks["cancel"] = h.s.CancelMailbox(ctx, other, mailboxID)
	_, checks["transfer"] = h.s.TransferOut(ctx, other, domainID)
	for name, err := range checks {
		if !errors.Is(err, errx.ErrNotFound) {
			t.Errorf("%s from another workspace: %v, want not found", name, err)
		}
	}
	if list, _ := h.s.ListDomains(ctx, other, "", 0); len(list.Data) != 0 {
		t.Errorf("another workspace lists %d domains", len(list.Data))
	}
	if _, err := h.s.GoogleToken(ctx, other, "ada@acmeoutreach.com", nil); err == nil {
		t.Errorf("another workspace minted a token for a purchased mailbox")
	}
	if tok, err := h.s.GoogleToken(ctx, h.org, "ada@acmeoutreach.com", []string{"https://mail.google.com/"}); err != nil || tok.AccessToken == "" {
		t.Errorf("own token = %v, %v", tok, err)
	}
}

func TestQuoteReopensAnAccountGoneUpstream(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	// A fresh simulator (or another partner environment) has never heard of the recorded account.
	h.fake = graphkit.NewFake()
	h.fake.SetClock(h.clock.now)
	h.s.setAPI(h.fake)
	if _, err := h.s.CreateQuote(context.Background(), h.org, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 1)); err != nil {
		t.Fatalf("quote after the account vanished: %v", err)
	}
}

// silentOrders drops the first order call, as when GraphKit never answers.
type silentOrders struct {
	graphkit.API
	dropped bool
}

func (s *silentOrders) CreateOrder(ctx context.Context, accountExternalID, quoteID, forwardingURL, idempotencyKey string) (*graphkit.Order, error) {
	if !s.dropped {
		s.dropped = true
		return nil, context.DeadlineExceeded
	}
	return s.API.CreateOrder(ctx, accountExternalID, quoteID, forwardingURL, idempotencyKey)
}

func TestUnmeteredOrderGraphKitNeverAnsweredIsPlacedByTheReconciler(t *testing.T) {
	h := newHarness(t)
	h.credits.unmetered = true
	h.s.setAPI(&silentOrders{API: h.fake})
	ctx := context.Background()
	q, err := h.s.CreateQuote(ctx, h.org, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 1))
	if err != nil {
		t.Fatal(err)
	}
	due := q.DueNowMicros
	o, err := h.s.PlaceOrder(ctx, h.org, h.user, OrderRequest{QuoteID: q.ID, ExpectedDueNowMicros: &due})
	if err != nil || o.Status != models.StoreOrderCharging {
		t.Fatalf("a dropped order call leaves the order pending: %+v, %v", o, err)
	}
	h.clock.advance(openOrderAge + time.Second)
	h.settle(t)
	for _, o := range h.repo.orders {
		if o.GraphKitOrderID == nil || o.Status == models.StoreOrderCharging {
			t.Fatalf("order left unplaced: status %s", o.Status)
		}
	}
}

// setAPI swaps the client a test runs against, as Connect would.
func (s *Service) setAPI(api graphkit.API) {
	s.swap(&upstream{api: api, on: true, source: SourceEnvironment})
}
