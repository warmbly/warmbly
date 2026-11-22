package store

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
)

// hookAPI is the fake GraphKit with chosen answers put in front of it; a nil hook, or one
// answering (nil, nil), passes the call through.
type hookAPI struct {
	*graphkit.Fake
	mu     sync.Mutex
	resume func(ctx context.Context, id, key string) (*graphkit.Mailbox, error)
	renew  func(ctx context.Context, id string, expected time.Time, key string) (*graphkit.Domain, error)
	order  func() error
}

func (a *hookAPI) ResumeMailbox(ctx context.Context, id, key string) (*graphkit.Mailbox, error) {
	a.mu.Lock()
	hook := a.resume
	a.mu.Unlock()
	if hook != nil {
		if m, err := hook(ctx, id, key); m != nil || err != nil {
			return m, err
		}
	}
	return a.Fake.ResumeMailbox(ctx, id, key)
}

func (a *hookAPI) RenewDomain(ctx context.Context, id string, years int, expected time.Time, key string) (*graphkit.Domain, error) {
	if a.renew != nil {
		if d, err := a.renew(ctx, id, expected, key); d != nil || err != nil {
			return d, err
		}
	}
	return a.Fake.RenewDomain(ctx, id, years, expected, key)
}

func (a *hookAPI) CreateOrder(ctx context.Context, externalID, quoteID, forwardingURL, key string) (*graphkit.Order, error) {
	if a.order != nil {
		if err := a.order(); err != nil {
			return nil, err
		}
	}
	return a.Fake.CreateOrder(ctx, externalID, quoteID, forwardingURL, key)
}

// hook puts a hookAPI in front of the harness's fake GraphKit.
func (h *harness) hook() *hookAPI {
	a := &hookAPI{Fake: h.fake}
	u := *h.s.current()
	u.api = a
	h.s.up.Store(&u)
	return a
}

var (
	refused  = &graphkit.Error{Status: http.StatusConflict, Code: graphkit.CodeStateConflict}
	noAnswer = &graphkit.Error{Code: graphkit.CodeTransport}
	monthFee = models.Micro(3_000_000)
	renewFee = models.Micro(11_170_000)
)

// oneMailbox buys a domain with one mailbox and returns it.
func oneMailbox(t *testing.T, h *harness, domain string) *models.StoreMailbox {
	t.Helper()
	h.buy(t, domainAndMailboxes(domain, graphkit.ProviderGoogle, 1))
	h.settle(t)
	for _, m := range h.repo.mailboxes {
		if strings.HasSuffix(m.Email, "@"+domain) {
			return cp(m)
		}
	}
	t.Fatalf("no mailbox on %s", domain)
	return nil
}

func (h *harness) box(t *testing.T, id uuid.UUID) *models.StoreMailbox {
	t.Helper()
	m, err := h.repo.GetMailbox(context.Background(), h.org, id)
	if err != nil || m == nil {
		t.Fatalf("mailbox %s: %v", id, err)
	}
	return m
}

// reserveRun is a billing run that reserved its mailboxes under key and stopped there.
func (h *harness) reserveRun(t *testing.T, key string, rows ...*models.StoreMailbox) {
	t.Helper()
	acct := h.repo.accounts[h.org]
	var list []models.StoreMailbox
	for _, m := range rows {
		list = append(list, *m)
	}
	if ran, err := h.repo.ReserveRun(context.Background(), h.org, list, key, *acct.NextBillAnchor, anchorAfter(acct.BillDay, *acct.NextBillAnchor)); err != nil || !ran {
		t.Fatalf("reserve the run: %v, %v", ran, err)
	}
}

// runTime is an hour into the workspace's next billing run.
func (h *harness) runTime() time.Time {
	acct := h.repo.accounts[h.org]
	return acct.NextBillAnchor.Add(-time.Duration(h.retail.r.MailboxBillLead())*24*time.Hour + time.Hour)
}

// monthLines is what the workspace paid for mailbox months net of their refunds: bundles, single months and what came back of them.
func monthLines(h *harness) models.Micro {
	h.credits.mu.Lock()
	defer h.credits.mu.Unlock()
	var total models.Micro
	for k, e := range h.credits.charges {
		if e.org == h.org && (strings.HasPrefix(k, "store:mailboxes:") || strings.HasPrefix(k, "store:month:")) && !e.refunded {
			total += e.amount
		}
	}
	return total
}

// creditedMonths is how many months a mailbox was moved on from a start.
func creditedMonths(from, to time.Time) int {
	n := 0
	for t := from; t.Before(to.Add(-time.Hour)); t = addMonth(t) {
		n++
	}
	return n
}

// unpaidCancelled leaves one mailbox cancelled at GraphKit for an unpaid month, the day before it stops.
func unpaidCancelled(t *testing.T, h *harness) *models.StoreMailbox {
	t.Helper()
	h.fund(50)
	m := oneMailbox(t, h, "acmeoutreach.com")
	h.credits.balance[h.org] = 0
	h.cards.card, h.cards.decline = true, "card_declined"
	h.clock.t = h.runTime()
	_ = h.s.Bill(context.Background())
	h.clock.t = m.PaidThrough.Add(-23 * time.Hour)
	_ = h.s.Bill(context.Background())
	got := h.box(t, m.ID)
	if !got.UnpaidCancel || got.Status != graphkit.MailboxCancelling {
		t.Fatalf("setup: mailbox %+v, want cancelled for the unpaid month", got)
	}
	return got
}

// #1: a late payment whose resume GraphKit refuses gives the month back once, not on every pass.
func TestLatePaymentWithARefusedResumeRefundsTheMonthOnce(t *testing.T) {
	h := newHarness(t)
	m := unpaidCancelled(t, h)
	h.hook().resume = func(context.Context, string, string) (*graphkit.Mailbox, error) { return nil, refused }
	h.cards.decline = ""
	ctx := context.Background()
	if err := h.s.RetryUnpaid(ctx, h.org); err != nil {
		t.Fatal(err)
	}
	after := h.credits.balance[h.org]
	for range 5 {
		h.clock.advance(time.Hour)
		_ = h.s.Bill(ctx)
		_ = h.s.RetryUnpaid(ctx, h.org)
	}
	if got := h.credits.balance[h.org]; got != after {
		t.Fatalf("balance went from %d to %d over later passes: the month was refunded again", after, got)
	}
	if paid := monthLines(h); paid != 0 {
		t.Fatalf("the workspace paid %d net for a month that will not start", paid)
	}
	if got := h.box(t, m.ID); got.UnpaidCancel || !got.PaidThrough.Equal(m.PaidThrough) {
		t.Fatalf("mailbox after the refund: %+v", got)
	}
}

// #2: a renewal already debited is not collected again when its retry runs; with no card it used to fail and charge again later.
func TestRenewalChargedButUndecidedIsNotCollectedAgain(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	ctx := context.Background()
	d := purchasedDomain(t, h, "acmeoutreach.com")
	h.credits.balance[h.org] = renewFee
	exp := h.clock.now().Add(5 * 24 * time.Hour)
	h.fake.ExpireDomain(d.GraphKitDomainID, exp)
	live, _ := h.fake.GetDomain(ctx, d.GraphKitDomainID)
	if _, err := h.s.upsertDomain(ctx, h.org, nil, live); err != nil {
		t.Fatal(err)
	}
	api := h.hook()
	calls := 0
	api.renew = func(context.Context, string, time.Time, string) (*graphkit.Domain, error) {
		calls++
		if calls == 1 {
			return nil, noAnswer
		}
		return nil, nil
	}
	_ = h.s.Bill(ctx)
	h.clock.advance(time.Hour)
	_ = h.s.Bill(ctx)
	after, _ := h.repo.GetDomain(ctx, h.org, d.ID)
	if !after.ExpiresAt.After(exp) || after.RenewalFailures != 0 || after.RenewalChargeKey != nil {
		t.Fatalf("after the retry: expiry %v, failures %d, in flight %v", after.ExpiresAt, after.RenewalFailures, after.RenewalChargeKey)
	}
	if net := h.credits.net(h.org); net != 2*renewFee {
		t.Fatalf("net %d, want the registration and one renewal", net)
	}
}

// #2: a bundle debited before a crash settles on replay even when the balance is now empty and there is no card.
func TestBundleDebitedBeforeACrashSettlesWithoutCollectingAgain(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	ctx := context.Background()
	m := oneMailbox(t, h, "acmeoutreach.com")
	h.clock.t = h.runTime()
	h.credits.balance[h.org] = monthFee
	key := "store:mailboxes:crashed"
	h.reserveRun(t, key, m)
	if _, err := h.credits.ChargeAmount(ctx, h.org, monthFee, "Mailboxes · 1 × $3.00", key); err != nil {
		t.Fatal(err)
	}
	h.clock.advance(replayAfter + time.Minute)
	_ = h.s.Bill(ctx)
	got := h.box(t, m.ID)
	if got.PendingChargeKey != nil || got.BillingFailures != 0 || !got.PaidThrough.After(m.PaidThrough) {
		t.Fatalf("after the replay: %+v", got)
	}
	if paid := monthLines(h); paid != monthFee {
		t.Fatalf("month charged %d, want once", paid)
	}
}

// #2: a checkout whose lines were debited before the order recorded it is placed on retry, not charged again or dropped.
func TestCheckoutDebitedBeforeItWasRecordedIsPlacedOnRetry(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	q, err := h.s.CreateQuote(ctx, h.org, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 1))
	if err != nil {
		t.Fatal(err)
	}
	h.credits.balance[h.org] = models.Micro(q.DueNowMicros)
	due := q.DueNowMicros
	h.repo.markChargedErr = errors.New("connection reset")
	if _, err := h.s.PlaceOrder(ctx, h.org, h.user, OrderRequest{QuoteID: q.ID, ExpectedDueNowMicros: &due}); err == nil {
		t.Fatal("the crash was swallowed")
	}
	o, err := h.s.PlaceOrder(ctx, h.org, h.user, OrderRequest{QuoteID: q.ID, ExpectedDueNowMicros: &due})
	if err != nil || o.ChargedMicros != due || o.Status != models.StoreOrderProcessing {
		t.Fatalf("retry = %+v, %v", o, err)
	}
	if int64(h.credits.net(h.org)) != due {
		t.Fatalf("net %d, want the order once", h.credits.net(h.org))
	}
}

// #3: a stuck batch never stops the rest of a workspace's billing.
func TestAStuckBatchDoesNotHoldUpTheBillingRun(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	ctx := context.Background()
	a := oneMailbox(t, h, "acmeoutreach.com")
	b := oneMailbox(t, h, "acmeinbox.com")
	h.credits.balance[h.org] = 0
	h.cards.card = true
	h.clock.t = h.runTime().Add(-time.Hour)
	stuck := "store:mailboxes:stuck"
	if _, err := h.repo.ReserveMailboxCharge(ctx, h.org, []models.StoreMailbox{*a}, stuck); err != nil {
		t.Fatal(err)
	}
	h.cards.fail = map[string]error{"store:card:" + stuck: errors.New("stripe: 500")}
	h.clock.t = h.runTime()
	if err := h.s.Bill(ctx); err == nil {
		t.Fatal("the stuck batch's error was lost")
	}
	if got := h.box(t, b.ID); !got.PaidThrough.After(b.PaidThrough) {
		t.Fatalf("the run did not charge the other mailbox: %+v", got)
	}
}

// #4: a run whose reservation fails is run again, so its mailboxes never fall a month behind.
func TestFailedReservationKeepsTheRunDue(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	m := oneMailbox(t, h, "acmeoutreach.com")
	h.clock.t = h.runTime()
	h.repo.reserveErr = errors.New("connection reset")
	if err := h.s.Bill(context.Background()); err == nil {
		t.Fatal("the failed reservation was swallowed")
	}
	billDaily(t, h, 8)
	if got := h.box(t, m.ID); !got.PaidThrough.After(m.PaidThrough) {
		t.Fatalf("never charged after the failed reservation: %+v", got)
	}
}

// #5: cancel, resume and cancel again refunds both collected months.
func TestCancelResumeCancelRefundsEachMonth(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	ctx := context.Background()
	m := oneMailbox(t, h, "acmeoutreach.com")
	h.clock.t = h.runTime()
	if err := h.s.Bill(ctx); err != nil {
		t.Fatal(err)
	}
	if paid := monthLines(h); paid != monthFee {
		t.Fatalf("setup: the run charged %d", paid)
	}
	for range 2 {
		if _, err := h.s.CancelMailbox(ctx, h.org, m.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := h.s.ResumeMailbox(ctx, h.org, m.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.s.CancelMailbox(ctx, h.org, m.ID); err != nil {
		t.Fatal(err)
	}
	if net := monthLines(h); net != 0 {
		t.Fatalf("paid %d net for a month that will not start", net)
	}
	if got := h.box(t, m.ID); !got.PaidThrough.Equal(m.PaidThrough) {
		t.Fatalf("paid through %v, want %v", got.PaidThrough, m.PaidThrough)
	}
}

// #6: the customer's own cancel ends an unpaid cancellation: saving a card then charges and resumes nothing.
func TestCustomerCancelEndsAnUnpaidCancellation(t *testing.T) {
	h := newHarness(t)
	m := unpaidCancelled(t, h)
	ctx := context.Background()
	if _, err := h.s.CancelMailbox(ctx, h.org, m.ID); err != nil {
		t.Fatal(err)
	}
	h.cards.decline = ""
	before := monthLines(h)
	if err := h.s.RetryUnpaid(ctx, h.org); err != nil {
		t.Fatal(err)
	}
	live, _ := h.fake.GetMailbox(ctx, m.GraphKitMailboxID)
	if monthLines(h) != before || live.Status != graphkit.MailboxCancelling {
		t.Fatalf("after saving a card: charged %d, GraphKit %s", monthLines(h)-before, live.Status)
	}
}

// #7: a resume GraphKit carried out without answering is credited once its state is known, and never charged again.
func TestResumeWithALostAnswerIsCreditedOnce(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	ctx := context.Background()
	m := oneMailbox(t, h, "acmeoutreach.com")
	if _, err := h.s.CancelMailbox(ctx, h.org, m.ID); err != nil {
		t.Fatal(err)
	}
	api := h.hook()
	api.resume = func(ctx context.Context, id, key string) (*graphkit.Mailbox, error) {
		if _, err := api.Fake.ResumeMailbox(ctx, id, key); err != nil {
			return nil, err
		}
		return nil, noAnswer
	}
	if _, err := h.s.ResumeMailbox(ctx, h.org, m.ID); err == nil {
		t.Fatal("the lost answer was swallowed")
	}
	api.mu.Lock()
	api.resume = nil
	api.mu.Unlock()
	h.clock.advance(replayAfter + time.Minute)
	if err := h.s.Bill(ctx); err != nil {
		t.Fatal(err)
	}
	got := h.box(t, m.ID)
	if got.PendingChargeKey != nil || got.CancelsAt != nil || !got.PaidThrough.Equal(addMonth(m.PaidThrough)) {
		t.Fatalf("after the replay: %+v, want paid one month on from %v", got, m.PaidThrough)
	}
	// Every month charged from here on is a month credited.
	h.clock.t = h.runTime()
	billDaily(t, h, 3)
	got = h.box(t, m.ID)
	if months := creditedMonths(m.PaidThrough, got.PaidThrough); monthLines(h) != models.Micro(months)*monthFee {
		t.Fatalf("charged %d for %d months credited", monthLines(h), months)
	}
}

// #7: a second resume while the first waits on GraphKit pays nothing and credits nothing.
func TestConcurrentResumesPayOneMonth(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	ctx := context.Background()
	m := oneMailbox(t, h, "acmeoutreach.com")
	if _, err := h.s.CancelMailbox(ctx, h.org, m.ID); err != nil {
		t.Fatal(err)
	}
	api := h.hook()
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	api.resume = func(context.Context, string, string) (*graphkit.Mailbox, error) {
		first := false
		once.Do(func() { first = true })
		if first {
			close(entered)
			<-release
		}
		return nil, nil
	}
	done := make(chan error, 1)
	go func() {
		_, err := h.s.ResumeMailbox(ctx, h.org, m.ID)
		done <- err
	}()
	<-entered
	_, second := h.s.ResumeMailbox(ctx, h.org, m.ID)
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if code(second) != CodeChargeInProgress {
		t.Fatalf("second resume = %v, want charge_in_progress", second)
	}
	got := h.box(t, m.ID)
	if paid := monthLines(h); paid != monthFee || !got.PaidThrough.Equal(addMonth(m.PaidThrough)) {
		t.Fatalf("charged %d and paid through %v for one resume (from %v)", paid, got.PaidThrough, m.PaidThrough)
	}
}

// #8: an order whose lines were debited and never recorded is placed by the reconciler.
func TestReconcilerPlacesAnOrderChargedButNeverRecorded(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	ctx := context.Background()
	q, err := h.s.CreateQuote(ctx, h.org, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 1))
	if err != nil {
		t.Fatal(err)
	}
	due := q.DueNowMicros
	h.repo.markChargedErr = errors.New("context canceled")
	if _, err := h.s.PlaceOrder(ctx, h.org, h.user, OrderRequest{QuoteID: q.ID, ExpectedDueNowMicros: &due}); err == nil {
		t.Fatal("the lost write was swallowed")
	}
	h.clock.advance(openOrderAge + time.Minute)
	if err := h.s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	row, _ := h.repo.GetOrderByQuote(ctx, h.org, q.ID)
	if row == nil || row.GraphKitOrderID == nil || int64(row.ChargedMicros) != due {
		t.Fatalf("order after the reconciler: %+v", row)
	}
}

// #9: a refund that fails when GraphKit refuses the order is retried until it goes through.
func TestRefusedOrderRefundIsRetried(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	ctx := context.Background()
	h.hook().order = func() error {
		return &graphkit.Error{Status: http.StatusUnprocessableEntity, Code: graphkit.CodeInvalidRequest}
	}
	h.credits.failRefund = errors.New("ledger unavailable")
	q, err := h.s.CreateQuote(ctx, h.org, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 1))
	if err != nil {
		t.Fatal(err)
	}
	due := q.DueNowMicros
	_, _ = h.s.PlaceOrder(ctx, h.org, h.user, OrderRequest{QuoteID: q.ID, ExpectedDueNowMicros: &due})
	h.credits.failRefund = nil
	h.clock.advance(openOrderAge + time.Minute)
	if err := h.s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if net := h.credits.net(h.org); net != 0 {
		t.Fatalf("net %d after the reconciler, want the refused order refunded", net)
	}
	row, _ := h.repo.GetOrderByQuote(ctx, h.org, q.ID)
	if row == nil || row.RefundPending || row.Status != models.StoreOrderFailed {
		t.Fatalf("order = %+v", row)
	}
}

// #9: a line that fails mid-charge whose refund also fails keeps the order so the reconciler can refund it.
func TestFailedLineWithAFailedRefundKeepsTheOrder(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	q, err := h.s.CreateQuote(ctx, h.org, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 1))
	if err != nil {
		t.Fatal(err)
	}
	due := q.DueNowMicros
	// The balance covers the order, then empties after the first line: the second is refused.
	h.credits.balance[h.org] = models.Micro(due)
	h.credits.failRefund = errors.New("ledger unavailable")
	h.credits.onDebit = func(org uuid.UUID) {
		h.credits.mu.Lock()
		h.credits.balance[org] = 0
		h.credits.mu.Unlock()
		h.credits.onDebit = nil
	}
	_, _ = h.s.PlaceOrder(ctx, h.org, h.user, OrderRequest{QuoteID: q.ID, ExpectedDueNowMicros: &due})
	row, _ := h.repo.GetOrderByQuote(ctx, h.org, q.ID)
	if row == nil || !row.RefundPending {
		t.Fatalf("order = %+v; want it kept with its refund pending", row)
	}
	h.credits.failRefund = nil
	h.clock.advance(openOrderAge + time.Minute)
	if err := h.s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if net := h.credits.net(h.org); net != 0 {
		t.Fatalf("net %d, want the charged line refunded", net)
	}
}

// #10 is a ledger query.

// #11: a declined manual renewal is charged again once the card is replaced, not refused for good.
func TestDeclinedManualRenewalGoesThroughWithANewCard(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	ctx := context.Background()
	d := purchasedDomain(t, h, "acmeoutreach.com")
	h.credits.balance[h.org] = 0
	h.cards.card, h.cards.decline = true, "card_declined"
	var card *CardError
	if _, err := h.s.RenewNow(ctx, h.org, d.ID); !errors.As(err, &card) {
		t.Fatalf("declined renewal = %v", err)
	}
	h.cards.decline = ""
	got, err := h.s.RenewNow(ctx, h.org, d.ID)
	if err != nil || !got.ExpiresAt.After(*d.ExpiresAt) {
		t.Fatalf("renewal with the new card = %+v, %v", got, err)
	}
}

// #11: a declined resume goes through once the card is replaced.
func TestDeclinedResumeGoesThroughWithANewCard(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	ctx := context.Background()
	m := oneMailbox(t, h, "acmeoutreach.com")
	if _, err := h.s.CancelMailbox(ctx, h.org, m.ID); err != nil {
		t.Fatal(err)
	}
	h.credits.balance[h.org] = 0
	h.cards.card, h.cards.decline = true, "card_declined"
	var card *CardError
	if _, err := h.s.ResumeMailbox(ctx, h.org, m.ID); !errors.As(err, &card) {
		t.Fatalf("declined resume = %v", err)
	}
	h.cards.decline = ""
	got, err := h.s.ResumeMailbox(ctx, h.org, m.ID)
	if err != nil || got.CancelsAt != nil {
		t.Fatalf("resume with the new card = %+v, %v", got, err)
	}
	if paid := monthLines(h); paid != monthFee {
		t.Fatalf("charged %d for the resumed month", paid)
	}
}

// #12: a cancel while the month's bundle is being charged gets that month back.
func TestCancelDuringAnInFlightBundleRefundsTheMonth(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	ctx := context.Background()
	m := oneMailbox(t, h, "acmeoutreach.com")
	h.clock.t = h.runTime()
	key := "store:mailboxes:inflight"
	h.reserveRun(t, key, m)
	if _, err := h.credits.ChargeAmount(ctx, h.org, monthFee, "Mailboxes · 1 × $3.00", key); err != nil {
		t.Fatal(err)
	}
	if _, err := h.s.CancelMailbox(ctx, h.org, m.ID); err != nil {
		t.Fatal(err)
	}
	h.clock.advance(replayAfter + time.Minute)
	_ = h.s.Bill(ctx)
	got := h.box(t, m.ID)
	if got.PendingChargeKey != nil || !got.PaidThrough.Equal(m.PaidThrough) {
		t.Fatalf("after the bundle settled: %+v", got)
	}
	if net := monthLines(h); net != 0 {
		t.Fatalf("paid %d net for a month the cancel keeps from starting", net)
	}
}

// #13: a renewal left undecided is still settled after auto-renew is turned off, and refunded when refused.
func TestUndecidedRenewalIsRefundedAfterAutoRenewGoesOff(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	ctx := context.Background()
	d := purchasedDomain(t, h, "acmeoutreach.com")
	exp := h.clock.now().Add(5 * 24 * time.Hour)
	h.fake.ExpireDomain(d.GraphKitDomainID, exp)
	live, _ := h.fake.GetDomain(ctx, d.GraphKitDomainID)
	if _, err := h.s.upsertDomain(ctx, h.org, nil, live); err != nil {
		t.Fatal(err)
	}
	api := h.hook()
	api.renew = func(context.Context, string, time.Time, string) (*graphkit.Domain, error) { return nil, noAnswer }
	_ = h.s.Bill(ctx)
	if net := h.credits.net(h.org); net != 2*renewFee {
		t.Fatalf("setup: net %d, want the renewal charged", net)
	}
	if err := h.repo.SetDomainAutoRenew(ctx, h.org, d.ID, false); err != nil {
		t.Fatal(err)
	}
	api.renew = func(context.Context, string, time.Time, string) (*graphkit.Domain, error) {
		return nil, &graphkit.Error{Status: http.StatusUnprocessableEntity, Code: graphkit.CodeRenewalWindowClosed}
	}
	h.clock.advance(time.Hour)
	_ = h.s.Bill(ctx)
	if net := h.credits.net(h.org); net != renewFee {
		t.Fatalf("net %d, want the refused renewal refunded", net)
	}
}

// #14: a second request for an order another is charging never deletes it.
func TestConcurrentCheckoutNeverDeletesTheOtherRequestsOrder(t *testing.T) {
	h := newHarness(t)
	h.cards.card = true
	ctx := context.Background()
	q, err := h.s.CreateQuote(ctx, h.org, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 1))
	if err != nil {
		t.Fatal(err)
	}
	// The first request created the order and is charging the card.
	stored, _ := h.repo.GetQuote(ctx, h.org, q.ID)
	rowID := uuid.New()
	if _, _, err := h.repo.CreateOrder(ctx, &models.StoreOrder{ID: rowID, OrganizationID: h.org, QuoteID: q.ID, Status: models.StoreOrderCharging,
		Quote: stored.Snapshot, DueNowMicros: models.Micro(q.DueNowMicros), ChargeKey: "store:order:" + rowID.String()}); err != nil {
		t.Fatal(err)
	}
	h.cards.inFlight = true
	due := q.DueNowMicros
	if _, err := h.s.PlaceOrder(ctx, h.org, h.user, OrderRequest{QuoteID: q.ID, ExpectedDueNowMicros: &due}); code(err) != CodeChargeInProgress {
		t.Fatalf("second request = %v", err)
	}
	if row, _ := h.repo.GetOrder(ctx, h.org, rowID); row == nil {
		t.Fatal("the second request deleted the first request's order")
	}
}

// An admin amount refund and later line refunds never give back more than the order took.
func TestOrderRefundsNeverExceedTheCharge(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	ctx := context.Background()
	_, o := h.buy(t, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 1))
	before := h.credits.balance[h.org]
	if _, err := h.s.AdminRefund(ctx, o.ID, RefundRequest{AmountMicros: 10_000_000, Key: "goodwill"}); err != nil {
		t.Fatal(err)
	}
	for line := range 2 {
		if _, err := h.s.AdminRefund(ctx, o.ID, RefundRequest{Line: &line}); err != nil {
			t.Fatal(err)
		}
	}
	if back := h.credits.balance[h.org] - before; int64(back) > o.ChargedMicros {
		t.Fatalf("refunded %d of an order that charged %d", back, o.ChargedMicros)
	}
}

// One quote, retried after it expired, yields one order and one charge however often it is sent.
func TestExpiredQuoteRetriedTwiceOrdersOnce(t *testing.T) {
	h := newHarness(t)
	h.fund(100)
	ctx := context.Background()
	q, err := h.s.CreateQuote(ctx, h.org, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 1))
	if err != nil {
		t.Fatal(err)
	}
	h.clock.advance(20 * time.Minute)
	due := q.DueNowMicros
	for range 2 {
		if _, err := h.s.PlaceOrder(ctx, h.org, h.user, OrderRequest{QuoteID: q.ID, ExpectedDueNowMicros: &due}); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(h.repo.orders); n != 1 {
		t.Fatalf("orders = %d, want one", n)
	}
	if int64(h.credits.net(h.org)) != due {
		t.Fatalf("net %d, want the order once", h.credits.net(h.org))
	}
}

// reloadIfLow is the balance's own automatic reload, set off by every debit that leaves the balance under its threshold.
func (m *memCards) reloadIfLow(org uuid.UUID) {
	cfg := m.settings(org)
	if !cfg.AutoReloadEnabled {
		return
	}
	m.credits.mu.Lock()
	whole := m.credits.balance[org] + m.credits.monthly[org]
	m.credits.mu.Unlock()
	if whole >= cfg.AutoReloadThreshold {
		return
	}
	m.mu.Lock()
	m.keys = append(m.keys, "auto_reload")
	m.topUps = append(m.topUps, cfg.AutoReloadAmount)
	m.mu.Unlock()
	m.credits.mu.Lock()
	m.credits.balance[org] += cfg.AutoReloadAmount
	m.credits.mu.Unlock()
}

// The store's top-up before the bundle and the balance's own reload after the bundle's debit never both charge the card.
func TestBundleDebitDoesNotSetOffASecondTopUp(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	h.cards.card = true
	h.buy(t, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 2))
	h.settle(t)
	h.credits.balance[h.org] = 0
	h.credits.onDebit = h.cards.reloadIfLow
	before := len(h.cards.topUps)
	var start time.Time
	for _, m := range h.repo.mailboxes {
		start = m.PaidThrough
	}
	h.clock.t = start.AddDate(0, 0, -6).Add(-24 * time.Hour)
	billDaily(t, h, 3)
	if n := len(h.cards.topUps) - before; n != 1 {
		t.Fatalf("card charges for one bundle = %v (keys %v), want one", h.cards.topUps[before:], h.cards.keys)
	}
	if lines := ledgerLines(h, "Mailboxes · 2 × $3.00"); len(lines) != 1 {
		t.Fatalf("bundle lines = %q", lines)
	}
}

// A mailbox left behind between runs (its month given back, then live again) is still paid before its period starts.
func TestMailboxBehindBetweenRunsIsPaidBeforeItsPeriod(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	m := oneMailbox(t, h, "acmeoutreach.com")
	h.clock.t = h.runTime()
	if err := h.s.Bill(context.Background()); err != nil {
		t.Fatal(err)
	}
	// The run's month went back to the balance and the mailbox carried on.
	h.repo.mu.Lock()
	row := h.repo.mailboxes[m.ID]
	row.PaidThrough, row.PaidKey, row.PaidMicros = m.PaidThrough, nil, 0
	h.repo.mu.Unlock()
	h.clock.advance(24 * time.Hour)
	billDaily(t, h, 6)
	if got := h.box(t, m.ID); !got.PaidThrough.After(m.PaidThrough) {
		t.Fatalf("never charged before its period: %+v", got)
	}
}
