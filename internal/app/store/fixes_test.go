package store

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
)

// startedOrder is an order another request created and has not charged yet.
func startedOrder(t *testing.T, h *harness, q *Quote) uuid.UUID {
	t.Helper()
	stored, _ := h.repo.GetQuote(context.Background(), h.org, q.ID)
	rowID := uuid.New()
	if _, _, err := h.repo.CreateOrder(context.Background(), &models.StoreOrder{ID: rowID, OrganizationID: h.org, QuoteID: q.ID, Status: models.StoreOrderCharging,
		Quote: stored.Snapshot, DueNowMicros: models.Micro(q.DueNowMicros), ChargeKey: "store:order:" + rowID.String()}); err != nil {
		t.Fatal(err)
	}
	return rowID
}

// A second checkout of a quote whose order another request is charging charges nothing and leaves the order alone.
func TestSecondCheckoutNeverChargesAlongsideTheFirst(t *testing.T) {
	h := newHarness(t)
	h.fund(100)
	ctx := context.Background()
	q, err := h.s.CreateQuote(ctx, h.org, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 1))
	if err != nil {
		t.Fatal(err)
	}
	rowID := startedOrder(t, h, q)
	due := q.DueNowMicros
	if _, err := h.s.PlaceOrder(ctx, h.org, h.user, OrderRequest{QuoteID: q.ID, ExpectedDueNowMicros: &due}); code(err) != CodeChargeInProgress {
		t.Fatalf("second request = %v", err)
	}
	if net := h.credits.net(h.org); net != 0 {
		t.Fatalf("the second request charged %d", net)
	}
	if row, _ := h.repo.GetOrder(ctx, h.org, rowID); row == nil || row.GraphKitOrderID != nil {
		t.Fatalf("order after the second request = %+v", row)
	}
}

// An order whose lines were debited and then refunded is never placed by the reconciler.
func TestReconcilerDoesNotPlaceARefundedOrder(t *testing.T) {
	h := newHarness(t)
	h.fund(100)
	ctx := context.Background()
	q, err := h.s.CreateQuote(ctx, h.org, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 1))
	if err != nil {
		t.Fatal(err)
	}
	rowID := startedOrder(t, h, q)
	row, _ := h.repo.GetOrder(ctx, h.org, rowID)
	for i, l := range q.Lines {
		if _, err := h.credits.ChargeAmount(ctx, h.org, models.Micro(l.AmountMicros), "line", lineKey(row.ChargeKey, i)); err != nil {
			t.Fatal(err)
		}
		if _, err := h.credits.RefundCharge(ctx, h.org, lineKey(row.ChargeKey, i), "back"); err != nil {
			t.Fatal(err)
		}
	}
	h.clock.advance(openOrderAge + time.Minute)
	if err := h.s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := h.repo.GetOrder(ctx, h.org, rowID); got != nil && got.GraphKitOrderID != nil {
		t.Fatalf("a refunded order was placed: %s", got.Status)
	}
}

// A month paid late whose resume got no answer is refunded once the cancellation takes effect.
func TestLatePaymentRefundedWhenTheCancellationTakesEffect(t *testing.T) {
	h := newHarness(t)
	m := unpaidCancelled(t, h)
	h.hook().resume = func(context.Context, string, string) (*graphkit.Mailbox, error) { return nil, noAnswer }
	h.cards.decline = ""
	ctx := context.Background()
	if err := h.s.RetryUnpaid(ctx, h.org); err != nil {
		t.Fatal(err)
	}
	if got := h.box(t, m.ID); got.BillingFailures != 0 || got.PaidKey == nil {
		t.Fatalf("setup: the late month was not paid: %+v", got)
	}
	h.repo.mu.Lock()
	h.repo.mailboxes[m.ID].Status = graphkit.MailboxCancelled
	h.repo.mu.Unlock()
	h.clock.advance(24 * time.Hour)
	_ = h.s.Bill(ctx)
	if paid := monthLines(h); paid != 0 {
		t.Fatalf("the workspace paid %d net for a month that never started", paid)
	}
	if got := h.box(t, m.ID); got.UnpaidCancel || !got.PaidThrough.Equal(m.PaidThrough) {
		t.Fatalf("mailbox after the refund: %+v", got)
	}
}

// A workspace's monthly mailbox cost counts its oldest live mailbox however many ended ones came after it.
func TestMonthlyMailboxesCountsLiveRowsPastAPage(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	add := func(status string) {
		h.clock.advance(time.Minute)
		if _, err := h.repo.UpsertMailbox(ctx, &models.StoreMailbox{ID: uuid.New(), OrganizationID: h.org, GraphKitMailboxID: "mbx_" + uuid.NewString(),
			Email: uuid.NewString()[:8] + "@acme.test", Provider: "google", Status: status, MonthlyRetailMicros: monthFee, PaidThrough: h.clock.now()}); err != nil {
			t.Fatal(err)
		}
	}
	add(graphkit.MailboxActive)
	for range maxPageLimit * 5 {
		add(graphkit.MailboxCancelled)
	}
	if got := h.s.monthlyMailboxes(ctx, h.org); got != int64(monthFee) {
		t.Fatalf("monthly %d, want the one live mailbox", got)
	}
}
