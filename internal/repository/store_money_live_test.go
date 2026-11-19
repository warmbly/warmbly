package repository

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/models"
)

// The store's money statements against a fresh, migrated database:
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LiveStoreMoney -v

type storeMoneyFixture struct {
	t       *testing.T
	pool    *pgxpool.Pool
	store   StoreRepository
	credits CreditRepository
	user    uuid.UUID
	org     uuid.UUID
}

func newStoreMoneyFixture(t *testing.T) *storeMoneyFixture {
	handle, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 410)
	f := &storeMoneyFixture{t: t, pool: pool, store: NewStoreRepository(handle), credits: NewCreditRepository(handle), user: uuid.New(), org: uuid.New()}
	f.exec(`INSERT INTO users (id, first_name, last_name, email) VALUES ($1, 'Store', 'Money', $2)`, f.user, "money-"+uuid.NewString()+"@example.test")
	f.exec(`INSERT INTO organizations (id, name, owner_user_id) VALUES ($1, 'Store Money', $2)`, f.org, f.user)
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = pool.Exec(bg, `DELETE FROM credit_ledger_transactions WHERE org_id = $1`, f.org)
		_, _ = pool.Exec(bg, `DELETE FROM credit_ledger WHERE org_id = $1`, f.org)
		_, _ = pool.Exec(bg, `DELETE FROM organizations WHERE id = $1`, f.org)
		_, _ = pool.Exec(bg, `DELETE FROM users WHERE id = $1`, f.user)
	})
	return f
}

func (f *storeMoneyFixture) exec(query string, args ...any) {
	f.t.Helper()
	if _, err := f.pool.Exec(context.Background(), query, args...); err != nil {
		f.t.Fatalf("fixture: %v\n%s", err, query)
	}
}

func (f *storeMoneyFixture) debit(d models.CreditDebit) {
	f.t.Helper()
	if _, _, _, err := f.credits.Debit(context.Background(), f.org, d); err != nil {
		f.t.Fatalf("debit %s: %v", d.IdempotencyKey, err)
	}
}

func genDebit(amount models.Micro, key string) models.CreditDebit {
	return models.CreditDebit{Amount: amount, Reason: "generation", Cost: amount, IdempotencyKey: key}
}

// Store purchases are not AI spend: a $30 domain under a $20 monthly AI limit leaves the limit's spend at what AI took.
func TestLiveStoreMoneyChargesStayOutOfSpendLimits(t *testing.T) {
	f := newStoreMoneyFixture(t)
	ctx := models.WithCreditMeta(context.Background(), models.CreditMeta{ActorID: f.user})
	if _, _, err := f.credits.GrantPurchased(ctx, f.org, 100*usd1, "top_up", "invoice:in_money"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := f.credits.Debit(ctx, f.org, genDebit(15*usd1, "gen:1")); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := f.credits.Debit(ctx, f.org, storeDebit(30*usd1, "store:order:o1:0")); err != nil {
		t.Fatal(err)
	}
	day, week, month := windowStartsForTest()
	if d, w, m, err := f.credits.SpentInWindows(ctx, f.org, day, week, month); err != nil || d != 15*usd1 || w != 15*usd1 || m != 15*usd1 {
		t.Fatalf("workspace spend = %d/%d/%d, %v; want the AI's 15 only", d, w, m, err)
	}
	if d, w, m, err := f.credits.MemberSpentInWindows(ctx, f.org, f.user, day, week, month); err != nil || d != 15*usd1 || w != 15*usd1 || m != 15*usd1 {
		t.Fatalf("member spend = %d/%d/%d, %v; want the AI's 15 only", d, w, m, err)
	}
}

func windowStartsForTest() (day, week, month time.Time) {
	now := time.Now().UTC()
	day = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	week = day.AddDate(0, 0, -int((now.Weekday()+6)%7))
	month = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	return day, week, month
}

// A refund this month of an AI charge made last month frees nothing under this month's cap on AI spending added money.
func TestLiveStoreMoneyOverageNetsARefundAgainstItsOwnMonth(t *testing.T) {
	f := newStoreMoneyFixture(t)
	ctx := context.Background()
	if _, _, err := f.credits.GrantPurchased(ctx, f.org, 50*usd1, "top_up", "invoice:in_overage"); err != nil {
		t.Fatal(err)
	}
	f.debit(genDebit(8*usd1, "gen:last-month"))
	f.exec(`UPDATE credit_ledger_transactions SET created_at = date_trunc('month', now()) - interval '3 days'
		WHERE org_id = $1 AND idempotency_key = $2`, f.org, f.org.String()+":gen:last-month")
	if back, _, err := f.credits.RefundSpend(ctx, f.org, "gen:last-month", "gen:last-month:refund", "Refund"); err != nil || back != 8*usd1 {
		t.Fatalf("refund %d, %v", back, err)
	}
	f.debit(genDebit(5*usd1, "gen:this-month"))
	if used, err := f.credits.AIFromPurchasedThisMonth(ctx, f.org); err != nil || used != 5*usd1 {
		t.Fatalf("this month's AI from added money = %d, %v; want 5", used, err)
	}
	// A refund in the charge's own month still nets.
	if _, _, err := f.credits.RefundSpend(ctx, f.org, "gen:this-month", "gen:this-month:refund", "Refund"); err != nil {
		t.Fatal(err)
	}
	if used, _ := f.credits.AIFromPurchasedThisMonth(ctx, f.org); used != 0 {
		t.Fatalf("after refunding this month's charge = %d, want 0", used)
	}
}

// Every refund of an order, however it is made, stops at what the order's lines took.
func TestLiveStoreMoneyRefundsNeverExceedTheCharge(t *testing.T) {
	f := newStoreMoneyFixture(t)
	ctx := context.Background()
	if _, _, err := f.credits.GrantPurchased(ctx, f.org, 50*usd1, "top_up", "invoice:in_cap"); err != nil {
		t.Fatal(err)
	}
	prefix := "store:order:" + uuid.NewString() + ":"
	f.debit(storeDebit(11_170_000, prefix+"0"))
	f.debit(storeDebit(6*usd1, prefix+"1"))
	if c, err := f.credits.KeyedCharge(ctx, f.org, prefix+"0"); err != nil || c == nil || c.Taken != 11_170_000 || c.Refunded != 0 {
		t.Fatalf("keyed charge = %+v, %v", c, err)
	}
	if c, err := f.credits.KeyedCharge(ctx, f.org, prefix+"9"); err != nil || c != nil {
		t.Fatalf("a key never charged = %+v, %v", c, err)
	}
	if got, err := f.credits.RefundCapped(ctx, f.org, prefix, "", prefix+"admin-refund:k", 10*usd1, "Refund"); err != nil || got != 10*usd1 {
		t.Fatalf("amount refund = %d, %v", got, err)
	}
	if got, _ := f.credits.RefundCapped(ctx, f.org, prefix, "", prefix+"admin-refund:k", 10*usd1, "Refund"); got != 10*usd1 {
		t.Fatalf("a replayed amount refund = %d, want the first answer", got)
	}
	if got, _ := f.credits.RefundCapped(ctx, f.org, prefix, prefix+"0", prefix+"0:refund", 0, "Refund"); got != 7_170_000 {
		t.Fatalf("line refund after the amount refund = %d, want what was left", got)
	}
	if got, _ := f.credits.RefundCapped(ctx, f.org, prefix, prefix+"1", prefix+"1:refund", 0, "Refund"); got != 0 {
		t.Fatalf("a refund past the charge gave %d", got)
	}
	if c, _ := f.credits.KeyedCharge(ctx, f.org, prefix+"0"); c == nil || c.Refunded != 7_170_000 {
		t.Fatalf("keyed charge after its refund = %+v", c)
	}
	if l, _ := f.credits.GetBalance(ctx, f.org); l.Purchased != 50*usd1 {
		t.Fatalf("purchased %d, want everything back and nothing more", l.Purchased)
	}

	// Concurrent refunds under different keys still stop at the charge.
	other := "store:order:" + uuid.NewString() + ":"
	f.debit(storeDebit(10*usd1, other+"0"))
	var wg sync.WaitGroup
	var mu sync.Mutex
	var total models.Micro
	for i := range 8 {
		wg.Go(func() {
			got, err := f.credits.RefundCapped(ctx, f.org, other, "", other+"admin-refund:"+string(rune('a'+i)), 3*usd1, "Refund")
			if err != nil {
				t.Error(err)
			}
			mu.Lock()
			total += got
			mu.Unlock()
		})
	}
	wg.Wait()
	if total != 10*usd1 {
		t.Fatalf("concurrent refunds gave %d of a %d charge", total, 10*usd1)
	}
}

func (f *storeMoneyFixture) mailbox(domainID uuid.UUID, paid time.Time) *models.StoreMailbox {
	f.t.Helper()
	m, err := f.store.UpsertMailbox(context.Background(), &models.StoreMailbox{ID: uuid.New(), OrganizationID: f.org, DomainID: domainID,
		GraphKitMailboxID: "mbx_" + uuid.NewString(), Email: uuid.NewString()[:8] + "@money.test", Provider: "google", Status: "active",
		MonthlyRetailMicros: 3_000_000, PaidThrough: paid, PeriodEnd: &paid})
	if err != nil || m == nil {
		f.t.Fatalf("mailbox: %+v, %v", m, err)
	}
	return m
}

func (f *storeMoneyFixture) domain() *models.StoreDomain {
	f.t.Helper()
	exp := time.Now().Add(20 * 24 * time.Hour).UTC().Truncate(time.Second)
	d, err := f.store.UpsertDomain(context.Background(), &models.StoreDomain{ID: uuid.New(), OrganizationID: f.org, GraphKitDomainID: "dom_" + uuid.NewString(),
		Name: uuid.NewString()[:8] + "-money.test", Status: "active", ExpiresAt: &exp})
	if err != nil || d == nil {
		f.t.Fatalf("domain: %+v, %v", d, err)
	}
	return d
}

// A month is reserved once, a run happens once, a cancelled mailbox is not moved on, and a month is refunded once.
func TestLiveStoreMoneyMailboxMonths(t *testing.T) {
	f := newStoreMoneyFixture(t)
	ctx := context.Background()
	repo := f.store
	if err := repo.UpsertAccount(ctx, f.org, "acct_money", nil); err != nil {
		t.Fatal(err)
	}
	anchor := time.Date(2030, 1, 16, 0, 0, 0, 0, time.UTC)
	if err := repo.SetBillAnchor(ctx, f.org, 16, anchor); err != nil {
		t.Fatal(err)
	}
	d := f.domain()
	paid := time.Now().Add(48 * time.Hour).UTC().Truncate(time.Microsecond)
	a, b := f.mailbox(d.ID, paid), f.mailbox(d.ID, paid)

	// A listing read before another pass moved the month on reserves nothing.
	stale := *a
	stale.PaidThrough = paid.Add(-time.Hour)
	if n, err := repo.ReserveMailboxCharge(ctx, f.org, []models.StoreMailbox{stale}, "k-stale"); err != nil || n != 0 {
		t.Fatalf("stale reservation = %d, %v", n, err)
	}

	// One run per date, and its mailboxes reserved with it.
	next := anchor.AddDate(0, 1, 0)
	if ran, err := repo.ReserveRun(ctx, f.org, []models.StoreMailbox{*a, *b}, "k-run", anchor, next); err != nil || !ran {
		t.Fatalf("run = %v, %v", ran, err)
	}
	if ran, _ := repo.ReserveRun(ctx, f.org, []models.StoreMailbox{*a, *b}, "k-run-2", anchor, next); ran {
		t.Fatal("the same run happened twice")
	}
	if acct, _ := repo.GetAccount(ctx, f.org); acct.NextBillAnchor == nil || !acct.NextBillAnchor.Equal(next) {
		t.Fatalf("anchor = %v", acct.NextBillAnchor)
	}
	rows, _ := repo.MailboxesByChargeKey(ctx, f.org, "k-run")
	if len(rows) != 2 || rows[0].PendingChargeMicros != 3_000_000 {
		t.Fatalf("reserved = %+v", rows)
	}
	if keys, _ := repo.PendingChargeKeys(ctx, f.org, time.Now().Add(-time.Minute)); len(keys) != 0 {
		t.Fatalf("a batch reserved just now is offered for replay: %v", keys)
	}
	if keys, _ := repo.PendingChargeKeys(ctx, f.org, time.Now().Add(time.Minute)); len(keys) != 1 {
		t.Fatalf("pending keys = %v", keys)
	}

	// b is cancelled by its owner while the bundle charges: it is not moved on.
	cancels := paid
	if err := repo.SetMailboxCancel(ctx, f.org, b.ID, "cancelling", &cancels); err != nil {
		t.Fatal(err)
	}
	if n, err := repo.SettleMailboxCharge(ctx, f.org, "k-run", time.Now()); err != nil || n != 1 {
		t.Fatalf("settled %d, %v", n, err)
	}
	ga, _ := repo.GetMailbox(ctx, f.org, a.ID)
	if ga.PaidKey == nil || *ga.PaidKey != "k-run" || ga.PaidMicros != 3_000_000 || !ga.PaidThrough.After(paid) {
		t.Fatalf("settled mailbox = %+v", ga)
	}
	gb, _ := repo.GetMailbox(ctx, f.org, b.ID)
	if gb.PendingChargeKey == nil || !gb.PaidThrough.Equal(paid) {
		t.Fatalf("cancelled mailbox = %+v; want it kept out and still in the batch", gb)
	}
	if err := repo.DetachMailboxCharge(ctx, f.org, "k-run", b.ID); err != nil {
		t.Fatal(err)
	}
	if gb, _ = repo.GetMailbox(ctx, f.org, b.ID); gb.PendingChargeKey != nil || gb.BillingFailures != 0 {
		t.Fatalf("detached mailbox = %+v", gb)
	}

	// The month a refunds goes back once, keyed by the charge that paid it.
	if err := repo.SetUnpaidCancel(ctx, f.org, a.ID, "cancelling", &cancels); err != nil {
		t.Fatal(err)
	}
	if won, err := repo.RefundMonth(ctx, f.org, a.ID, "k-run"); err != nil || !won {
		t.Fatalf("refund month = %v, %v", won, err)
	}
	if won, _ := repo.RefundMonth(ctx, f.org, a.ID, "k-run"); won {
		t.Fatal("a month was refunded twice")
	}
	if ga, _ = repo.GetMailbox(ctx, f.org, a.ID); ga.UnpaidCancel || ga.PaidKey != nil || !ga.PaidThrough.Equal(paid) {
		t.Fatalf("refunded mailbox = %+v", ga)
	}

	// The owner's own cancel ends an unpaid one.
	if err := repo.SetUnpaidCancel(ctx, f.org, a.ID, "cancelling", &cancels); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetMailboxCancel(ctx, f.org, a.ID, "cancelling", &cancels); err != nil {
		t.Fatal(err)
	}
	if ga, _ = repo.GetMailbox(ctx, f.org, a.ID); ga.UnpaidCancel {
		t.Fatal("the owner's cancel left the mailbox cancelled for non-payment")
	}
}

// An unpaid month being retried is still in the set the stop rule reads.
func TestLiveStoreMoneyUnpaidIncludesARetryInFlight(t *testing.T) {
	f := newStoreMoneyFixture(t)
	ctx := context.Background()
	d := f.domain()
	m := f.mailbox(d.ID, time.Now().Add(24*time.Hour).UTC().Truncate(time.Microsecond))
	f.exec(`UPDATE store_mailboxes SET billing_failures = 1 WHERE id = $1`, m.ID)
	if n, _ := f.store.ReserveMailboxCharge(ctx, f.org, []models.StoreMailbox{*m}, "k-retry"); n != 1 {
		t.Fatal("reserve")
	}
	if rows, _ := f.store.UnpaidMailboxes(ctx, f.org, time.Now(), time.Now().AddDate(10, 0, 0), false); len(rows) != 0 {
		t.Fatalf("the retry list offers a mailbox being charged: %d", len(rows))
	}
	if rows, _ := f.store.UnpaidMailboxes(ctx, f.org, time.Now(), time.Now().AddDate(10, 0, 0), true); len(rows) != 1 {
		t.Fatalf("the stop rule misses a mailbox whose retry is in flight: %d", len(rows))
	}
}

// One quote is priced again once and ordered once; refunds that did not go through and stranded orders are listed.
func TestLiveStoreMoneyQuotesAndOrders(t *testing.T) {
	f := newStoreMoneyFixture(t)
	ctx := context.Background()
	repo := f.store
	quote := func() *models.StoreQuote {
		q := &models.StoreQuote{ID: uuid.New(), OrganizationID: f.org, GraphKitQuoteID: "qt_" + uuid.NewString(), Snapshot: json.RawMessage(`{}`), ExpiresAt: time.Now().Add(time.Minute)}
		if err := repo.CreateQuote(ctx, q); err != nil {
			t.Fatal(err)
		}
		return q
	}
	order := func(q *models.StoreQuote) *models.StoreOrder {
		id := uuid.New()
		return &models.StoreOrder{ID: id, OrganizationID: f.org, QuoteID: q.ID, Status: models.StoreOrderCharging, Quote: json.RawMessage(`{}`), ChargeKey: "store:order:" + id.String()}
	}
	orig, first, second := quote(), quote(), quote()
	if got, ordered, err := repo.ReplaceQuote(ctx, f.org, orig.ID, first.ID); err != nil || ordered || got != first.ID {
		t.Fatalf("first replacement = %v, %v, %v", got, ordered, err)
	}
	if got, _, _ := repo.ReplaceQuote(ctx, f.org, orig.ID, second.ID); got != first.ID {
		t.Fatalf("second replacement = %v, want the first one kept", got)
	}
	if q, _ := repo.GetQuote(ctx, f.org, orig.ID); q.ReplacedBy == nil || *q.ReplacedBy != first.ID {
		t.Fatalf("quote = %+v", q)
	}
	if _, _, err := repo.CreateOrder(ctx, order(orig)); !errors.Is(err, ErrStoreQuoteReplaced) {
		t.Fatalf("an order on a replaced quote = %v", err)
	}
	placed := order(first)
	if _, _, err := repo.CreateOrder(ctx, placed); err != nil {
		t.Fatal(err)
	}
	if _, ordered, _ := repo.ReplaceQuote(ctx, f.org, first.ID, second.ID); !ordered {
		t.Fatal("a quote with an order was priced again")
	}

	// Still charging with nothing recorded: stranded once old enough.
	if rows, _ := repo.StrandedOrders(ctx, time.Now().Add(time.Minute), 1000); !hasOrder(rows, placed.ID) {
		t.Fatal("a charging order with no charge recorded is not listed")
	}
	if err := repo.MarkOrderFailed(ctx, f.org, placed.ID, "store_unavailable", 1_000_000, true); err != nil {
		t.Fatal(err)
	}
	if rows, _ := repo.RefundPendingOrders(ctx, time.Now().Add(time.Minute), 1000); !hasOrder(rows, placed.ID) {
		t.Fatal("an order with a refund pending is not listed")
	}
	if err := repo.MarkOrderRefunded(ctx, f.org, placed.ID, 3_000_000); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.GetOrder(ctx, f.org, placed.ID); got.RefundPending || got.RefundedMicros != 3_000_000 {
		t.Fatalf("refunded order = %+v", got)
	}
}

func hasOrder(rows []models.StoreOrder, id uuid.UUID) bool {
	for i := range rows {
		if rows[i].ID == id {
			return true
		}
	}
	return false
}

// A renewal charged and undecided is reconciled whatever auto-renew says, until it is renewed or refunded.
func TestLiveStoreMoneyRenewalInFlight(t *testing.T) {
	f := newStoreMoneyFixture(t)
	ctx := context.Background()
	d := f.domain()
	if err := f.store.SetDomainAutoRenew(ctx, f.org, d.ID, false); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	due := func() bool {
		rows, err := f.store.DueDomainRenewals(ctx, now.Add(30*24*time.Hour), now.Add(-24*time.Hour), now.Add(-7*24*time.Hour), now.Add(7*24*time.Hour), 1000)
		if err != nil {
			t.Fatal(err)
		}
		for i := range rows {
			if rows[i].ID == d.ID {
				return true
			}
		}
		return false
	}
	if due() {
		t.Fatal("a domain with auto-renew off and nothing in flight is due")
	}
	key := "store:renew:" + d.ID.String() + ":1:0"
	if err := f.store.SetRenewalCharge(ctx, f.org, d.ID, &key); err != nil {
		t.Fatal(err)
	}
	if !due() {
		t.Fatal("a renewal in flight is not reconciled once auto-renew is off")
	}
	if err := f.store.RecordDomainRenewalFailure(ctx, f.org, d.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.store.GetDomain(ctx, f.org, d.ID); got.RenewalChargeKey != nil || due() {
		t.Fatalf("after the refusal: %+v", got)
	}
}
