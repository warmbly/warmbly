package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// Every store read and write a workspace drives is scoped by organization_id,
// and one workspace's GraphKit object never rewrites another's row.
func TestLiveStoreRepositoryIsOrganizationScoped(t *testing.T) {
	handle, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 401)
	ctx := context.Background()
	repo := NewStoreRepository(handle)
	user, org, other := uuid.New(), uuid.New(), uuid.New()
	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, q, args...); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	mustExec(`INSERT INTO users (id, first_name, last_name, email) VALUES ($1, 'Store', 'Test', $2)`, user, "store-"+uuid.NewString()+"@example.test")
	mustExec(`INSERT INTO organizations (id, name, owner_user_id) VALUES ($1, 'Store A', $3), ($2, 'Store B', $3)`, org, other, user)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = ANY($1)`, []uuid.UUID{org, other})
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, user)
	})

	if err := repo.UpsertAccount(ctx, org, "acct_1", json.RawMessage(`{"first_name":"Ada"}`)); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertAccount(ctx, org, "", nil); err != nil {
		t.Fatal(err)
	}
	acct, err := repo.GetAccount(ctx, org)
	if err != nil || acct == nil || acct.GraphKitAccountID != "acct_1" || len(acct.Registrant) == 0 {
		t.Fatalf("account kept = %+v, %v", acct, err)
	}

	quote := &models.StoreQuote{ID: uuid.New(), OrganizationID: org, GraphKitQuoteID: "qt_1", Snapshot: json.RawMessage(`{}`), ExpiresAt: time.Now().Add(time.Minute)}
	if err := repo.CreateQuote(ctx, quote); err != nil {
		t.Fatal(err)
	}
	if q, _ := repo.GetQuote(ctx, other, quote.ID); q != nil {
		t.Fatal("another workspace read the quote")
	}
	order := &models.StoreOrder{ID: uuid.New(), OrganizationID: org, QuoteID: quote.ID, Status: models.StoreOrderCharging,
		Quote: json.RawMessage(`{}`), DueNowMicros: 14_170_000, ChargeKey: "store:order:" + uuid.NewString(), CreatedBy: &user}
	got, existed, err := repo.CreateOrder(ctx, order)
	if err != nil || existed || got.ID != order.ID {
		t.Fatalf("create order = %+v %v %v", got, existed, err)
	}
	again, existed, err := repo.CreateOrder(ctx, &models.StoreOrder{ID: uuid.New(), OrganizationID: org, QuoteID: quote.ID,
		Status: models.StoreOrderCharging, Quote: json.RawMessage(`{}`), ChargeKey: "store:order:" + uuid.NewString()})
	if err != nil || !existed || again.ID != order.ID {
		t.Fatalf("second order for the quote = %+v %v %v, want the first", again, existed, err)
	}
	if _, _, err := repo.CreateOrder(ctx, &models.StoreOrder{ID: uuid.New(), OrganizationID: other, QuoteID: quote.ID,
		Status: models.StoreOrderCharging, Quote: json.RawMessage(`{}`), ChargeKey: "store:order:" + uuid.NewString()}); err == nil {
		t.Fatal("another workspace took over the quote's order")
	}
	if o, _ := repo.GetOrder(ctx, other, order.ID); o != nil {
		t.Fatal("another workspace read the order")
	}
	if err := repo.MarkOrderPlaced(ctx, other, order.ID, "ord_x", models.StoreOrderProcessing, json.RawMessage(`[]`)); err != nil {
		t.Fatal(err)
	}
	if o, _ := repo.GetOrder(ctx, org, order.ID); o.GraphKitOrderID != nil {
		t.Fatal("another workspace placed the order")
	}
	if err := repo.MarkOrderPlaced(ctx, org, order.ID, "ord_1", models.StoreOrderProcessing, json.RawMessage(`[]`)); err != nil {
		t.Fatal(err)
	}
	if fresh, _ := repo.AddOrderRefund(ctx, org, order.ID, "k:1", 3_000_000); !fresh {
		t.Fatal("first refund not recorded")
	}
	if fresh, _ := repo.AddOrderRefund(ctx, org, order.ID, "k:1", 3_000_000); fresh {
		t.Fatal("the same refund was recorded twice")
	}
	if o, _ := repo.GetOrderByGraphKitID(ctx, org, "ord_1"); o == nil || o.RefundedMicros != 3_000_000 {
		t.Fatalf("order by upstream id = %+v", o)
	}

	exp := time.Now().Add(400 * 24 * time.Hour).UTC().Truncate(time.Second)
	dom, err := repo.UpsertDomain(ctx, &models.StoreDomain{ID: uuid.New(), OrganizationID: org, OrderID: &order.ID, GraphKitDomainID: "dom_" + uuid.NewString(),
		Name: "acmeoutreach.com", Status: "active", Provider: "google", ExpiresAt: &exp, RenewRetailMicros: 11_170_000})
	if err != nil || dom == nil || !dom.AutoRenew || dom.LastSeenRenewMicros != 11_170_000 {
		t.Fatalf("domain = %+v, %v", dom, err)
	}
	if hijack, _ := repo.UpsertDomain(ctx, &models.StoreDomain{ID: uuid.New(), OrganizationID: other, GraphKitDomainID: dom.GraphKitDomainID,
		Name: "stolen.com", Status: "active"}); hijack != nil {
		t.Fatal("another workspace's event rewrote the domain")
	}
	if d, _ := repo.GetDomain(ctx, other, dom.ID); d != nil {
		t.Fatal("another workspace read the domain")
	}
	if list, _ := repo.ListDomains(ctx, other, nil, 10); len(list) != 0 {
		t.Fatal("another workspace listed the domain")
	}
	if won, _ := repo.SetDomainNotice(ctx, dom.ID, "upcoming:1", 12_000_000, true); !won {
		t.Fatal("notice not recorded")
	}
	if won, _ := repo.SetDomainNotice(ctx, dom.ID, "upcoming:1", 12_000_000, true); won {
		t.Fatal("notice recorded twice")
	}
	now := time.Now()
	due, err := repo.DueDomainRenewals(ctx, now.Add(500*24*time.Hour), now, now.Add(time.Minute), now, 10)
	if err != nil || len(due) != 1 {
		t.Fatalf("due renewals = %d, %v", len(due), err)
	}
	moved := exp.AddDate(1, 0, 0)
	if _, err := repo.UpsertDomain(ctx, &models.StoreDomain{ID: uuid.New(), OrganizationID: org, GraphKitDomainID: dom.GraphKitDomainID,
		Name: dom.Name, Status: "active", ExpiresAt: &moved}); err != nil {
		t.Fatal(err)
	}
	if d, _ := repo.GetDomain(ctx, org, dom.ID); d.RenewalNoticeAt != nil || d.LastNotice != "" || d.RenewRetailMicros != 11_170_000 {
		t.Fatalf("a new expiry did not restart notices: %+v", d)
	}

	paid := time.Now().Add(-time.Hour)
	box, err := repo.UpsertMailbox(ctx, &models.StoreMailbox{ID: uuid.New(), OrganizationID: org, DomainID: dom.ID, OrderID: &order.ID,
		GraphKitMailboxID: "mbx_" + uuid.NewString(), Email: "ada@acmeoutreach.com", Provider: "google", Status: "active",
		MonthlyRetailMicros: 3_000_000, PaidThrough: paid, ConnectMethod: "token"})
	if err != nil || box == nil || box.ConnectState != models.StoreConnectWaiting {
		t.Fatalf("mailbox = %+v, %v", box, err)
	}
	if m, _ := repo.GetMailboxByEmail(ctx, other, "ada@acmeoutreach.com"); m != nil {
		t.Fatal("another workspace found the mailbox by address")
	}
	if ok, _ := repo.ClaimConnect(ctx, other, box.ID, time.Now()); ok {
		t.Fatal("another workspace claimed the connect")
	}
	if ok, _ := repo.ClaimConnect(ctx, org, box.ID, time.Now()); !ok {
		t.Fatal("claim failed")
	}
	if ok, _ := repo.ClaimConnect(ctx, org, box.ID, time.Now()); ok {
		t.Fatal("a held claim was taken twice")
	}
	if pending, _ := repo.PendingConnects(ctx, time.Now(), 10); len(pending) != 0 {
		t.Fatal("a fresh claim is pending again")
	}
	billable, err := repo.MailboxesDue(ctx, org, time.Now().AddDate(0, 2, 0), time.Now())
	if err != nil || len(billable) != 1 || billable[0].ID != box.ID {
		t.Fatalf("due mailboxes = %+v, %v", billable, err)
	}
	if other, _ := repo.MailboxesDue(ctx, other, time.Now().AddDate(0, 2, 0), time.Now()); len(other) != 0 {
		t.Fatal("another workspace's mailbox is due")
	}
	if n, _ := repo.ReserveMailboxCharge(ctx, other, []models.StoreMailbox{*box}, "k-other"); n != 0 {
		t.Fatal("another workspace reserved the mailbox")
	}
	if n, _ := repo.ReserveMailboxCharge(ctx, org, []models.StoreMailbox{*box}, "k-1"); n != 1 {
		t.Fatal("reservation failed")
	}
	if n, _ := repo.ReserveMailboxCharge(ctx, org, []models.StoreMailbox{*box}, "k-2"); n != 0 {
		t.Fatal("a reserved mailbox joined a second batch")
	}
	if n, err := repo.SettleMailboxCharge(ctx, org, "k-1", time.Now()); err != nil || n != 1 {
		t.Fatalf("settled %d, %v", n, err)
	}
	if m, _ := repo.GetMailbox(ctx, org, box.ID); m.PendingChargeKey != nil || !m.PaidThrough.After(time.Now()) {
		t.Fatalf("settled mailbox = %+v", m)
	}

	if fresh, _ := repo.RecordWebhookEvent(ctx, "evt_"+order.ID.String()); !fresh {
		t.Fatal("event not new")
	}
	if seen, _ := repo.WebhookEventSeen(ctx, "evt_"+order.ID.String()); !seen {
		t.Fatal("event not seen")
	}
	_, _ = pool.Exec(ctx, `DELETE FROM store_webhook_events WHERE event_id = $1`, "evt_"+order.ID.String())
}
