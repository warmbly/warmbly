package store

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
)

// plans answers whether a workspace has a paid plan, as the feature gate does.
type plans struct {
	mu     sync.Mutex
	active bool
}

func (p *plans) set(on bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.active = on
}

func (p *plans) HasPremiumWarmup(context.Context, uuid.UUID) (bool, *errx.Error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.active, nil
}

type managed struct{ org uuid.UUID }

func (m managed) PartnerManaged(_ context.Context, orgID uuid.UUID) bool { return orgID == m.org }

type opsAlerts struct {
	mu   sync.Mutex
	keys []string
}

func (o *opsAlerts) NotifyOperator(key, _, summary string, _ map[string]string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.keys = append(o.keys, key+": "+summary)
}

type trackingStub struct {
	mu    sync.Mutex
	calls []string
}

func (t *trackingStub) VendorTracking(_ context.Context, _ uuid.UUID, domain, host string) (*models.TrackingDomainStatus, int, *errx.Error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.calls = append(t.calls, domain+" "+host)
	return &models.TrackingDomainStatus{TrackingDomain: host}, 1, nil
}

func TestBuyingNeedsAPlan(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	p := &plans{}
	h.s.Plans = p
	ctx := context.Background()
	st, err := h.s.Status(ctx, h.org)
	if err != nil || !st.Enabled || st.PlanActive {
		t.Fatalf("status without a plan = %+v, %v", st, err)
	}
	if _, err := h.s.CreateQuote(ctx, h.org, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 1)); code(err) != CodePlanRequired {
		t.Fatalf("quote without a plan: %v", err)
	}
	if _, err := h.s.ConnectDomain(ctx, h.org, ConnectRequest{Domain: "acme-own.com"}); code(err) != CodePlanRequired {
		t.Fatalf("connect without a plan: %v", err)
	}
	p.set(true)
	q, err := h.s.CreateQuote(ctx, h.org, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 1))
	if err != nil {
		t.Fatal(err)
	}
	// A plan that ends between the quote and the checkout charges nothing.
	p.set(false)
	due := q.DueNowMicros
	if _, err := h.s.PlaceOrder(ctx, h.org, h.user, OrderRequest{QuoteID: q.ID, ExpectedDueNowMicros: &due}); code(err) != CodePlanRequired {
		t.Fatalf("order without a plan: %v", err)
	}
	if h.credits.net(h.org) != 0 {
		t.Fatalf("charged %d without a plan", h.credits.net(h.org))
	}
}

func TestPartnerManagedWorkspaceHasNoStore(t *testing.T) {
	h := newHarness(t)
	h.s.WireManaged(managed{org: h.org})
	ctx := context.Background()
	if st, _ := h.s.Status(ctx, h.org); st.Enabled {
		t.Fatal("the store shows in a partner-managed workspace")
	}
	if _, err := h.s.CreateQuote(ctx, h.org, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 1)); code(err) != CodeManagedByPartner {
		t.Fatalf("quote in a partner-managed workspace: %v", err)
	}
}

func TestLapsedPlanPausesPurchasedMailboxesAndKeepsBilling(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	p := &plans{active: true}
	h.s.Plans = p
	ctx := context.Background()
	h.buy(t, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 2))
	h.settle(t)
	list, _ := h.s.ListMailboxes(ctx, h.org, "", 0)
	if len(list.Data) != 2 || list.Data[0].Paused {
		t.Fatalf("mailboxes with a plan = %+v", list.Data)
	}

	p.set(false)
	list, _ = h.s.ListMailboxes(ctx, h.org, "", 0)
	for _, m := range list.Data {
		if !m.Paused || m.ConnectState != models.StoreConnectConnected {
			t.Fatalf("a lapsed plan should pause a connected mailbox, not drop it: %+v", m)
		}
	}
	notes := len(h.notes.titles)
	if err := h.s.Bill(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.s.Bill(ctx); err != nil {
		t.Fatal(err)
	}
	var paused []string
	for i, title := range h.notes.titles[notes:] {
		if strings.Contains(title, "paused") {
			paused = append(paused, h.notes.bodies[notes+i])
		}
	}
	if len(paused) != 1 || !strings.Contains(paused[0], "keep billing at $3.00 a month") {
		t.Fatalf("plan notices = %q", paused)
	}

	// The month still bills: the workspace keeps the mailboxes.
	before := h.credits.net(h.org)
	h.clock.advance(32 * 24 * time.Hour)
	if err := h.s.Bill(ctx); err != nil {
		t.Fatal(err)
	}
	if got := h.credits.net(h.org) - before; got != 2*3_000_000 {
		t.Fatalf("monthly charge while lapsed = %d, want 2 × $3", got)
	}

	// A plan that comes back clears the notice, so the next lapse says it again.
	p.set(true)
	if err := h.s.Bill(ctx); err != nil {
		t.Fatal(err)
	}
	if a, _ := h.repo.GetAccount(ctx, h.org); a.PlanNoticeAt != nil {
		t.Fatal("the plan notice stayed after a plan returned")
	}
}

func TestGoogleMailboxesUseTheBrokerWithoutAServiceAccount(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	ctx := context.Background()
	h.buy(t, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 1))
	h.settle(t)
	if len(h.delegator.brokered) != 1 || !h.delegator.brokered[0] {
		t.Fatalf("brokered = %v, want the token broker", h.delegator.brokered)
	}
	d, err := h.fake.GetAccountDelegation(ctx, h.org.String())
	if err != nil || d.Google == nil || d.Google.ClientID != "" {
		t.Fatalf("account identity = %+v, %v", d, err)
	}

	// With a service account of its own the instance takes its Google mailboxes by delegation.
	h2 := newHarness(t)
	h2.delegator.googleID = "1234567890"
	if err := h2.s.PushDelegation(ctx); err != nil {
		t.Fatal(err)
	}
	h2.fund(50)
	h2.buy(t, domainAndMailboxes("acmeinbox.com", graphkit.ProviderGoogle, 1))
	h2.settle(t)
	if len(h2.delegator.brokered) != 1 || h2.delegator.brokered[0] {
		t.Fatalf("brokered = %v, want the instance's own delegation", h2.delegator.brokered)
	}
}

func TestConnectedDomainLifecycle(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	ctx := context.Background()
	d, err := h.s.ConnectDomain(ctx, h.org, ConnectRequest{Domain: "www.Acme-HasMail.com", Method: "nameservers"})
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "acme-hasmail.com" || d.Source != models.StoreDomainConnected || d.Renewable || d.Connection == nil ||
		d.Connection.Status != graphkit.ConnectionPending || len(d.Nameservers) != 2 || len(d.DNSConflicts) != 3 || len(d.DNS) == 0 {
		t.Fatalf("connected domain = %+v", d)
	}
	if _, err := h.s.ConnectDomain(ctx, h.org, ConnectRequest{Domain: "acme-hasmail.com"}); code(err) != CodeDomainUnavailable {
		t.Fatalf("connecting it twice: %v", err)
	}
	if _, err := h.s.RenewNow(ctx, h.org, d.ID); code(err) != CodeRegisteredElsewhere {
		t.Fatalf("renewing a connected domain: %v", err)
	}
	// The simulator sees the nameservers ten seconds later; the feed brings the domain up.
	h.clock.advance(11 * time.Second)
	h.fake.Tick()
	if err := h.s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := h.s.GetDomain(ctx, h.org, d.ID)
	if got.Status != graphkit.DomainActive || got.Connection.Status != graphkit.ConnectionActive {
		t.Fatalf("after the delegation is seen = %+v", got.Connection)
	}
	mbx := QuoteRequest{Items: []graphkit.QuoteItem{{Type: graphkit.ItemMailboxes, Domain: "acme-hasmail.com", Provider: graphkit.ProviderGoogle,
		Mailboxes: []graphkit.NewMailbox{{LocalPart: "ada", FirstName: "Ada", LastName: "Lovelace"}}}}}
	if _, err := h.s.CreateQuote(ctx, h.org, mbx); code(err) != CodeDNSRecordConflict {
		t.Fatalf("quote with conflicting records: %v", err)
	}
	for _, c := range got.DNSConflicts {
		if c.RecordID == nil {
			t.Fatalf("an imported conflict has no record id: %+v", c)
		}
		if err := h.s.DeleteDNSRecord(ctx, h.org, d.ID, *c.RecordID); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ = h.s.GetDomain(ctx, h.org, d.ID); len(got.DNSConflicts) != 0 {
		t.Fatalf("conflicts after removing them = %+v", got.DNSConflicts)
	}
	q, err := h.s.CreateQuote(ctx, h.org, mbx)
	if err != nil || len(q.Lines) != 1 || q.Lines[0].AdminEmail == nil || *q.Lines[0].AdminEmail != "ada@acme-hasmail.com" {
		t.Fatalf("quote = %+v, %v", q, err)
	}
	if err := h.s.RemoveDomain(ctx, h.org, d.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.s.GetDomain(ctx, h.org, d.ID); code(err) != errx.ErrNotFound.ResponseCode() {
		t.Fatalf("a removed domain: %v", err)
	}
}

func TestDNSRecordsOnAPurchasedDomain(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	ctx := context.Background()
	_, o := h.buy(t, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 1))
	h.settle(t)
	o, _ = h.s.GetOrder(ctx, h.org, o.ID)
	id := o.Domains[0].ID
	if err := h.s.RemoveDomain(ctx, h.org, id); code(err) != CodeRegisteredHere {
		t.Fatalf("removing a purchased domain: %v", err)
	}
	r, err := h.s.AddDNSRecord(ctx, h.org, id, DNSRecordInput{Type: "txt", Name: "_verify.acmeoutreach.com", Content: "token=1", TTL: 300})
	if err != nil || r.Name != "_verify" || r.Managed {
		t.Fatalf("add = %+v, %v", r, err)
	}
	if _, err := h.s.AddDNSRecord(ctx, h.org, id, DNSRecordInput{Type: "MX", Name: "@", Content: "mx.elsewhere.example"}); code(err) != CodeDNSRecordConflict {
		t.Fatalf("a second MX: %v", err)
	}
	if _, err := h.s.AddDNSRecord(ctx, h.org, id, DNSRecordInput{Type: "TXT", Name: "x", Content: "y", TTL: 5}); code(err) != CodeDNSRecordInvalid {
		t.Fatalf("a TTL of 5: %v", err)
	}
	content := "token=2"
	if got, err := h.s.UpdateDNSRecord(ctx, h.org, id, r.ID, DNSRecordPatch{Content: &content}); err != nil || got.Content != content || got.ID != r.ID {
		t.Fatalf("update = %+v, %v", got, err)
	}
	dom, _ := h.s.GetDomain(ctx, h.org, id)
	for _, rec := range dom.DNS {
		if rec.Managed {
			if err := h.s.DeleteDNSRecord(ctx, h.org, id, rec.ID); code(err) != CodeDNSRecordManaged {
				t.Fatalf("deleting a managed record: %v", err)
			}
			break
		}
	}
	check, err := h.s.CheckDNS(ctx, h.org, id)
	if err != nil || len(check.Records) != len(dom.DNS) || len(check.Nameservers) != 2 {
		t.Fatalf("check = %+v, %v", check, err)
	}
	if err := h.s.DeleteDNSRecord(ctx, h.org, id, r.ID); err != nil {
		t.Fatal(err)
	}
	if err := h.s.DeleteDNSRecord(ctx, h.org, id, r.ID); code(err) != errx.ErrNotFound.ResponseCode() {
		t.Fatalf("deleting it again: %v", err)
	}
}

func TestSuspendedPartnerAlertsTheOperatorOnce(t *testing.T) {
	h := newHarness(t)
	ops := &opsAlerts{}
	h.s.Ops = ops
	h.fake.Suspend(true)
	ctx := context.Background()
	for range 3 {
		if _, err := h.s.Search(ctx, "acme", nil, false); code(err) != CodeUnavailable {
			t.Fatalf("search while suspended: %v", err)
		}
	}
	if len(ops.keys) != 1 || !strings.Contains(ops.keys[0], "suspended") {
		t.Fatalf("operator alerts = %q", ops.keys)
	}
	h.clock.advance(2 * time.Hour)
	_, _ = h.s.Search(ctx, "acme", nil, false)
	if len(ops.keys) != 2 {
		t.Fatalf("an hour later the alert repeats once: %q", ops.keys)
	}
}

func TestPurchasedDomainGetsItsTrackingHostAndAdminSignIn(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	track := &trackingStub{}
	h.s.WireSendingDomains(track)
	ctx := context.Background()
	_, o := h.buy(t, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 2))
	h.settle(t)
	if len(track.calls) != 1 || !strings.HasPrefix(track.calls[0], "acmeoutreach.com ") {
		t.Fatalf("tracking setups = %v, want one for the domain", track.calls)
	}
	o, _ = h.s.GetOrder(ctx, h.org, o.ID)
	if o.Domains[0].TrackingHost == "" {
		t.Fatal("the domain does not show its tracking host")
	}
	var admin *Mailbox
	for i := range o.Mailboxes {
		if o.Mailboxes[i].IsAdmin {
			admin = &o.Mailboxes[i]
		}
	}
	if admin == nil || admin.Email != "ada@acmeoutreach.com" {
		t.Fatalf("admin = %+v", admin)
	}
	in, err := h.s.SignIn(ctx, h.org, admin.ID)
	if err != nil || in.Password == "" || in.AdminConsoleURL == nil {
		t.Fatalf("admin sign-in = %+v, %v", in, err)
	}
	var notified bool
	for _, title := range h.notes.titles {
		notified = notified || strings.Contains(title, "ready")
	}
	if !notified {
		t.Fatalf("no order-ready notice in %q", h.notes.titles)
	}
}

func TestSearchPricesTheMinimumTerm(t *testing.T) {
	h := newHarness(t)
	res, err := h.s.Search(context.Background(), "acmerobots", []string{"ai", "com"}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range res.Data {
		if o.Domain == "acmerobots.ai" && (o.Years != 2 || o.PriceMicros != 2*o.RenewMicros) {
			t.Fatalf(".ai offer = %+v, want two years", o)
		}
		if o.Domain == "acmerobots.com" && o.Years != 1 {
			t.Fatalf(".com offer = %+v", o)
		}
	}
}

func TestMailboxesNeedBothNames(t *testing.T) {
	h := newHarness(t)
	req := domainAndMailboxes("acmeoutreach.com", graphkit.ProviderGoogle, 1)
	req.Items[1].Mailboxes[0].LastName = ""
	if _, err := h.s.CreateQuote(context.Background(), h.org, req); code(err) != "invalid_name" {
		t.Fatalf("a mailbox without a last name: %v", err)
	}
}
