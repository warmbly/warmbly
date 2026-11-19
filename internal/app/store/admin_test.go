package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
	"github.com/warmbly/warmbly/internal/repository"
)

// ---- the operator's view over memRepo -----------------------------------

var _ repository.StoreAdminRepository = (*memRepo)(nil)

var memMeta = map[string]json.RawMessage{}

func (r *memRepo) OrderOrg(_ context.Context, id uuid.UUID) (uuid.UUID, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if o := r.orders[id]; o != nil {
		return o.OrganizationID, true, nil
	}
	return uuid.Nil, false, nil
}

func (r *memRepo) DomainOrg(_ context.Context, id uuid.UUID) (uuid.UUID, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if d := r.domains[id]; d != nil {
		return d.OrganizationID, true, nil
	}
	return uuid.Nil, false, nil
}

func (r *memRepo) MailboxOrg(_ context.Context, id uuid.UUID) (uuid.UUID, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if m := r.mailboxes[id]; m != nil {
		return m.OrganizationID, true, nil
	}
	return uuid.Nil, false, nil
}

func (r *memRepo) AdminListOrders(_ context.Context, f models.StoreAdminOrderFilter, _ *models.StorePage, limit int, _ time.Time) ([]models.StoreOrder, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []models.StoreOrder{}
	for _, o := range r.orders {
		if (f.OrgID == nil || *f.OrgID == o.OrganizationID) && (f.Status == "" || f.Status == o.Status) && len(out) < limit {
			out = append(out, *o)
		}
	}
	return out, nil
}

func (r *memRepo) AdminListDomains(_ context.Context, f models.StoreAdminDomainFilter, _ *models.StorePage, limit int) ([]models.StoreDomain, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []models.StoreDomain{}
	for _, d := range r.domains {
		if (f.OrgID == nil || *f.OrgID == d.OrganizationID) && len(out) < limit {
			out = append(out, *d)
		}
	}
	return out, nil
}

func (r *memRepo) AdminListMailboxes(_ context.Context, f models.StoreAdminMailboxFilter, _ *models.StorePage, limit int) ([]models.StoreMailbox, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []models.StoreMailbox{}
	for _, m := range r.mailboxes {
		if (f.OrgID == nil || *f.OrgID == m.OrganizationID) && len(out) < limit {
			out = append(out, *m)
		}
	}
	return out, nil
}

func (r *memRepo) AdminCounts(context.Context, time.Time) (*models.StoreAdminCounts, error) {
	return &models.StoreAdminCounts{}, nil
}

func (r *memRepo) LiveMailboxesByProvider(context.Context) ([]models.StoreLiveMailboxes, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	by := map[string]*models.StoreLiveMailboxes{}
	out := []models.StoreLiveMailboxes{}
	for _, m := range r.mailboxes {
		if !liveStatus(m.Status) {
			continue
		}
		if by[m.Provider] == nil {
			by[m.Provider] = &models.StoreLiveMailboxes{Provider: m.Provider}
		}
		by[m.Provider].Count++
		by[m.Provider].MonthlyMicros += m.MonthlyRetailMicros
	}
	for _, l := range by {
		out = append(out, *l)
	}
	return out, nil
}

func (r *memRepo) OrgNames(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	out := map[uuid.UUID]string{}
	for _, id := range ids {
		out[id] = "Workspace " + id.String()[:4]
	}
	return out, nil
}

func (r *memRepo) ResetConnect(_ context.Context, orgID, id uuid.UUID) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m := r.mailboxes[id]
	if m == nil || m.OrganizationID != orgID || m.EmailAccountID != nil {
		return false, nil
	}
	m.ConnectState, m.ConnectAttempts, m.ConnectError = models.StoreConnectWaiting, 0, ""
	delete(r.attempted, id)
	return true, nil
}

func (r *memRepo) OrgSummaries(_ context.Context, orgIDs []uuid.UUID, _ time.Time) ([]models.StoreOrgSummary, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []models.StoreOrgSummary{}
	for _, org := range orgIDs {
		s := models.StoreOrgSummary{OrganizationID: org}
		any := false
		for _, d := range r.domains {
			if d.OrganizationID == org {
				s.Domains++
				any = true
			}
		}
		for _, m := range r.mailboxes {
			if m.OrganizationID == org && liveStatus(m.Status) {
				s.MailboxesLive++
				s.MonthlyMicros += m.MonthlyRetailMicros
				any = true
				if m.ConnectState == models.StoreConnectFailed {
					s.ConnectFailed++
				}
			}
		}
		if any {
			out = append(out, s)
		}
	}
	return out, nil
}

func (r *memRepo) RenewalsBefore(context.Context, []uuid.UUID, time.Time, int) ([]models.StoreDomain, error) {
	return []models.StoreDomain{}, nil
}

func (r *memRepo) GetMeta(_ context.Context, key string) (json.RawMessage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return memMeta[key], nil
}

func (r *memRepo) SetMeta(_ context.Context, key string, value json.RawMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	memMeta[key] = value
	return nil
}

// GrantPurchased adds money back under a key, once.
func (c *memCredits) GrantPurchased(_ context.Context, org uuid.UUID, amount models.Micro, reason, key string) (models.Micro, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.charges[key] != nil {
		return c.balance[org], nil
	}
	c.balance[org] += amount
	c.charges[key] = &ledgerEntry{org: org, amount: -amount, reason: reason}
	return c.balance[org], nil
}

// ---- tests ---------------------------------------------------------------

func TestAdminLineRefundPaysOnce(t *testing.T) {
	h := newHarness(t)
	h.fund(100)
	ctx := context.Background()
	_, o := h.buy(t, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 2))
	before := h.credits.balance[h.org]
	line := 1 // the mailboxes line
	first, err := h.s.AdminRefund(ctx, o.ID, RefundRequest{Line: &line})
	if err != nil {
		t.Fatal(err)
	}
	if first.RefundedMicros != 6_000_000 {
		t.Fatalf("refunded %d, want the line's $6.00", first.RefundedMicros)
	}
	again, err := h.s.AdminRefund(ctx, o.ID, RefundRequest{Line: &line})
	if err != nil {
		t.Fatal(err)
	}
	if again.RefundedMicros != 0 {
		t.Fatalf("a repeated line refund paid %d more", again.RefundedMicros)
	}
	if got := h.credits.balance[h.org] - before; got != 6_000_000 {
		t.Fatalf("balance moved %d, want 6000000 once", got)
	}
	if got := again.Order.RefundedLines; len(got) != 1 || got[0] != 1 || again.Order.RefundedMicros != 6_000_000 {
		t.Fatalf("order after refunds = lines %v, refunded %d", got, again.Order.RefundedMicros)
	}
	bad := 9
	if _, err := h.s.AdminRefund(ctx, o.ID, RefundRequest{Line: &bad}); code(err) != "invalid_line" {
		t.Fatalf("a missing line: %v", err)
	}
}

func TestAdminAmountRefundNeedsAKeyAndNeverExceedsTheCharge(t *testing.T) {
	h := newHarness(t)
	h.fund(100)
	ctx := context.Background()
	_, o := h.buy(t, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 1))
	if _, err := h.s.AdminRefund(ctx, o.ID, RefundRequest{AmountMicros: 1_000_000}); code(err) != "idempotency_key_required" {
		t.Fatalf("no key: %v", err)
	}
	before := h.credits.balance[h.org]
	for range 2 {
		res, err := h.s.AdminRefund(ctx, o.ID, RefundRequest{AmountMicros: 1_500_000, Reason: "goodwill", Key: "k1"})
		if err != nil {
			t.Fatal(err)
		}
		if res.Order.RefundedMicros != 1_500_000 {
			t.Fatalf("refunded on the order = %d", res.Order.RefundedMicros)
		}
	}
	if got := h.credits.balance[h.org] - before; got != 1_500_000 {
		t.Fatalf("the same key paid %d, want 1500000 once", got)
	}
	if _, err := h.s.AdminRefund(ctx, o.ID, RefundRequest{AmountMicros: o.ChargedMicros, Key: "k2"}); code(err) != CodeRefundExceedsCharge {
		t.Fatalf("more than is left: %v", err)
	}
}

func TestAdminRefundOnAnUnmeteredInstance(t *testing.T) {
	h := newHarness(t)
	h.credits.unmetered = true
	ctx := context.Background()
	_, o := h.buy(t, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 1))
	line := 0
	if _, err := h.s.AdminRefund(ctx, o.ID, RefundRequest{Line: &line}); code(err) != CodeUnmetered {
		t.Fatalf("unmetered refund: %v", err)
	}
}

// The operator reaches a row by its id alone; the workspace it acts in is the row's own.
func TestAdminActionsActInTheRowsOwnWorkspace(t *testing.T) {
	h := newHarness(t)
	h.fund(100)
	ctx := context.Background()
	_, o := h.buy(t, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 1))
	h.settle(t)
	got, err := h.s.AdminGetOrder(ctx, o.ID)
	if err != nil || got.OrganizationID != h.org {
		t.Fatalf("admin order = %+v, %v", got, err)
	}
	if _, err := h.s.AdminGetOrder(ctx, uuid.New()); !errors.Is(err, errx.ErrNotFound) {
		t.Fatalf("unknown order: %v", err)
	}
	var domainID, mailboxID uuid.UUID
	for id := range h.repo.domains {
		domainID = id
	}
	for id := range h.repo.mailboxes {
		mailboxID = id
	}
	d, err := h.s.AdminSetAutoRenew(ctx, domainID, false)
	if err != nil || d.AutoRenew || d.OrganizationID != h.org {
		t.Fatalf("auto-renew off = %+v, %v", d, err)
	}
	if again, err := h.s.AdminSetAutoRenew(ctx, domainID, false); err != nil || again.AutoRenew {
		t.Fatalf("repeated auto-renew off = %+v, %v", again, err)
	}
	m, err := h.s.AdminCancelMailbox(ctx, mailboxID)
	if err != nil || m.CancelsAt == nil {
		t.Fatalf("cancel = %+v, %v", m, err)
	}
	if again, err := h.s.AdminCancelMailbox(ctx, mailboxID); err != nil || again.CancelsAt == nil {
		t.Fatalf("repeated cancel = %+v, %v", again, err)
	}
	if m, err := h.s.AdminResumeMailbox(ctx, mailboxID); err != nil || m.CancelsAt != nil {
		t.Fatalf("resume = %+v, %v", m, err)
	}
	ws, err := h.s.AdminWorkspace(ctx, h.org)
	if err != nil || len(ws.Orders) != 1 || len(ws.Domains) != 1 || len(ws.Mailboxes) != 1 {
		t.Fatalf("workspace = %+v, %v", ws, err)
	}
	other, err := h.s.AdminWorkspace(ctx, uuid.New())
	if err != nil || len(other.Orders)+len(other.Domains)+len(other.Mailboxes) != 0 {
		t.Fatalf("another workspace sees %+v, %v", other, err)
	}
}

func TestAdminPlaceRefusesAPlacedOrder(t *testing.T) {
	h := newHarness(t)
	h.fund(100)
	_, o := h.buy(t, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 1))
	if _, err := h.s.AdminPlaceOrder(context.Background(), o.ID); code(err) != CodeStoreStateConflict {
		t.Fatalf("placing a placed order: %v", err)
	}
}

func TestAgencyOverviewLeavesOutManagedWorkspaces(t *testing.T) {
	h := newHarness(t)
	h.fund(100)
	ctx := context.Background()
	h.buy(t, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 2))
	h.settle(t)
	out, err := h.s.AgencyOverview(ctx, []AgencyClient{{ID: h.org, Name: "Acme"}, {ID: uuid.New(), Name: "Empty"}})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Enabled || len(out.Clients) != 1 || out.Clients[0].MailboxesLive != 2 || out.Totals.MonthlySpendMicros != 6_000_000 {
		t.Fatalf("overview = %+v", out)
	}
	h.s.WireManaged(managed{org: h.org})
	out, err = h.s.AgencyOverview(ctx, []AgencyClient{{ID: h.org, Name: "Acme"}})
	if err != nil || len(out.Clients) != 0 || out.Enabled {
		t.Fatalf("a partner-managed client shows, or an agency of only managed clients offers the store: %+v, %v", out, err)
	}
}
