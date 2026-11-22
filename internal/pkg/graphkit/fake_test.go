package graphkit

import (
	"context"
	"testing"
	"time"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }
func fakeAt(t time.Time) (*Fake, *clock) {
	f, c := NewFake(), &clock{t: t}
	f.SetClock(c.now)
	return f, c
}
func contactFor() *Contact {
	return &Contact{FirstName: "Ada", LastName: "Lovelace", Email: "ada@acme.test", Phone: "+1.4155550100", Address1: "1 Market St", City: "SF", PostalCode: "94105", Country: "US"}
}
func setupAccount(t *testing.T, f *Fake) {
	t.Helper()
	if _, err := f.UpsertAccount(context.Background(), "org", AccountInput{Name: "Acme", Registrant: contactFor()}, "a"); err != nil {
		t.Fatal(err)
	}
}

func TestFakeOrderWalksStepsAndConnects(t *testing.T) {
	f, c := fakeAt(time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC))
	ctx := context.Background()
	setupAccount(t, f)
	// No client id of the instance's own: the account's Google mailboxes connect by token.
	if _, err := f.PutAccountDelegation(ctx, "org", Delegation{Google: &GoogleDelegation{Scopes: []string{"https://mail.google.com/"}}}, "d"); err != nil {
		t.Fatal(err)
	}
	q, err := f.CreateQuote(ctx, "org", []QuoteItem{
		{Type: ItemDomain, Domain: "acmeoutreach.com", Years: 1},
		{Type: ItemMailboxes, Domain: "acmeoutreach.com", Provider: ProviderGoogle, Mailboxes: []NewMailbox{{LocalPart: "ada", FirstName: "Ada", LastName: "Lovelace"}}},
	}, "q")
	if err != nil {
		t.Fatal(err)
	}
	if q.DueNowMicros != 11_170_000+fakeMailboxMonthly {
		t.Fatalf("due now = %d", q.DueNowMicros)
	}
	o, err := f.CreateOrder(ctx, "org", q.ID, "", "o")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.CreateOrder(ctx, "org", q.ID, "", "o2"); CodeOf(err) != CodeQuoteMismatch {
		t.Fatalf("reordering a quote: %v, want quote_mismatch", err)
	}
	c.advance(20 * time.Second)
	got, err := f.GetOrder(ctx, "org", o.ID)
	if err != nil || got.Status != OrderCompleted {
		t.Fatalf("order after 20s = %+v, %v", got, err)
	}
	m, _ := f.GetMailbox(ctx, got.Items[1].MailboxIDs[0])
	if m.Status != MailboxActive || m.Connection.Method != ConnectToken || !m.Connection.Ready || !m.IsAdmin {
		t.Fatalf("mailbox = %+v", m)
	}
	tok, err := f.MailboxAccessToken(ctx, m.ID, nil, "t1")
	if err != nil || tok.AccessToken == "" || !tok.ExpiresAt.After(c.t) {
		t.Fatalf("token = %+v, %v", tok, err)
	}
	// The admin mailbox answers credentials whatever its method, with the admin console.
	creds, err := f.MailboxCredentials(ctx, m.ID)
	if err != nil || creds.AdminConsoleURL != "https://admin.google.com" {
		t.Fatalf("admin credentials = %+v, %v", creds, err)
	}
}

func TestFakeFailedRegistrationRefundsItems(t *testing.T) {
	f, c := fakeAt(time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC))
	ctx := context.Background()
	setupAccount(t, f)
	before, _ := f.Balance(ctx)
	q, _ := f.CreateQuote(ctx, "org", []QuoteItem{
		{Type: ItemDomain, Domain: "acme-fail-register.com"},
		{Type: ItemMailboxes, Domain: "acme-fail-register.com", Provider: ProviderMicrosoft, Mailboxes: []NewMailbox{{LocalPart: "ada", FirstName: "Ada", LastName: "Lovelace"}, {LocalPart: "bob", FirstName: "Ada", LastName: "Lovelace"}}},
	}, "q")
	o, err := f.CreateOrder(ctx, "org", q.ID, "", "o")
	if err != nil {
		t.Fatal(err)
	}
	c.advance(time.Minute)
	got, _ := f.GetOrder(ctx, "org", o.ID)
	if got.Status != OrderFailed {
		t.Fatalf("status = %s", got.Status)
	}
	for _, it := range got.Items {
		if it.RefundedMicros != it.AmountMicros || it.ErrorCode != CodeProvisioningFailed {
			t.Fatalf("item = %+v, want refunded", it)
		}
	}
	after, _ := f.Balance(ctx)
	if after.BalanceMicros != before.BalanceMicros {
		t.Fatalf("partner balance %d -> %d, want refunded", before.BalanceMicros, after.BalanceMicros)
	}
}

func order(t *testing.T, f *Fake, c *clock, items ...QuoteItem) *Order {
	t.Helper()
	ctx := context.Background()
	q, err := f.CreateQuote(ctx, "org", items, "q-"+items[0].Domain+time.Duration(c.t.UnixNano()).String())
	if err != nil {
		t.Fatal(err)
	}
	o, err := f.CreateOrder(ctx, "org", q.ID, "", "o-"+q.ID)
	if err != nil {
		t.Fatal(err)
	}
	c.advance(time.Minute)
	got, err := f.GetOrder(ctx, "org", o.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestFakeConnectionFollowsIdentities(t *testing.T) {
	f, c := fakeAt(time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC))
	ctx := context.Background()
	setupAccount(t, f)
	if err := f.PutDelegation(ctx, Delegation{Google: &GoogleDelegation{Scopes: []string{"https://mail.google.com/"}}}, "p0"); CodeOf(err) != CodeInvalidRequest {
		t.Fatalf("partner Google identity without a client id: %v, want invalid_request", err)
	}
	o := order(t, f, c, QuoteItem{Type: ItemMailboxes, Domain: "acmeidentity.com", Provider: ProviderGoogle, Mailboxes: []NewMailbox{{LocalPart: "ada", FirstName: "Ada", LastName: "Lovelace"}}},
		QuoteItem{Type: ItemDomain, Domain: "acmeidentity.com"})
	m, _ := f.GetMailbox(ctx, o.Items[1].MailboxIDs[0])
	if m.Connection != (Connection{Method: ConnectDelegation, Ready: false}) {
		t.Fatalf("before any identity = %+v", m.Connection)
	}
	if err := f.PutDelegation(ctx, Delegation{Google: &GoogleDelegation{ClientID: "1234567890", Scopes: []string{"https://mail.google.com/"}}}, "p1"); err != nil {
		t.Fatal(err)
	}
	if m, _ = f.GetMailbox(ctx, m.ID); m.Connection != (Connection{Method: ConnectDelegation, Ready: true}) {
		t.Fatalf("with the partner's client id = %+v", m.Connection)
	}
	if _, err := f.PutAccountDelegation(ctx, "org", Delegation{Google: &GoogleDelegation{Scopes: []string{"https://mail.google.com/"}}}, "a1"); err != nil {
		t.Fatal(err)
	}
	if m, _ = f.GetMailbox(ctx, m.ID); m.Connection != (Connection{Method: ConnectToken, Ready: true}) {
		t.Fatalf("with the account's scopes only = %+v", m.Connection)
	}
	if _, err := f.PutAccountDelegation(ctx, "org", Delegation{}, "a2"); CodeOf(err) != CodeInvalidRequest {
		t.Fatalf("account PUT with neither provider: %v", err)
	}
	if err := f.DeleteAccountDelegation(ctx, "org", "a3"); err != nil {
		t.Fatal(err)
	}
	if m, _ = f.GetMailbox(ctx, m.ID); m.Connection.Method != ConnectDelegation {
		t.Fatalf("after the account identity is dropped = %+v", m.Connection)
	}
}

func TestFakeAdminMailbox(t *testing.T) {
	f, c := fakeAt(time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC))
	ctx := context.Background()
	setupAccount(t, f)
	two := []NewMailbox{{LocalPart: "ada", FirstName: "Ada", LastName: "Lovelace", Admin: true}, {LocalPart: "bob", FirstName: "Ada", LastName: "Lovelace", Admin: true}}
	if _, err := f.CreateQuote(ctx, "org", []QuoteItem{{Type: ItemDomain, Domain: "acmeadmins.com"},
		{Type: ItemMailboxes, Domain: "acmeadmins.com", Provider: ProviderGoogle, Mailboxes: two}}, "q1"); CodeOf(err) != CodeInvalidRequest {
		t.Fatalf("two admin flags: %v", err)
	}
	q, err := f.CreateQuote(ctx, "org", []QuoteItem{{Type: ItemDomain, Domain: "acmeadmins.com"},
		{Type: ItemMailboxes, Domain: "acmeadmins.com", Provider: ProviderMicrosoft, Mailboxes: []NewMailbox{{LocalPart: "ada", FirstName: "Ada", LastName: "Lovelace"}, {LocalPart: "bob", FirstName: "Ada", LastName: "Lovelace", Admin: true}}}}, "q2")
	if err != nil || q.Lines[1].AdminEmail != "bob@acmeadmins.com" {
		t.Fatalf("quote = %+v, %v", q, err)
	}
	o := order(t, f, c, QuoteItem{Type: ItemDomain, Domain: "acmeadmins.com"},
		QuoteItem{Type: ItemMailboxes, Domain: "acmeadmins.com", Provider: ProviderMicrosoft, Mailboxes: []NewMailbox{{LocalPart: "ada", FirstName: "Ada", LastName: "Lovelace"}, {LocalPart: "bob", FirstName: "Ada", LastName: "Lovelace", Admin: true}}})
	if o.Status != OrderCompleted {
		t.Fatalf("order = %s", o.Status)
	}
	if _, err := f.CreateQuote(ctx, "org", []QuoteItem{{Type: ItemMailboxes, Domain: "acmeadmins.com", Provider: ProviderMicrosoft,
		Mailboxes: []NewMailbox{{LocalPart: "cy", FirstName: "Ada", LastName: "Lovelace", Admin: true}}}}, "q3"); CodeOf(err) != CodeInvalidRequest {
		t.Fatalf("flagging a second admin: %v", err)
	}
	more, err := f.CreateQuote(ctx, "org", []QuoteItem{{Type: ItemMailboxes, Domain: "acmeadmins.com", Provider: ProviderMicrosoft,
		Mailboxes: []NewMailbox{{LocalPart: "cy", FirstName: "Ada", LastName: "Lovelace"}}}}, "q4")
	if err != nil || more.Lines[0].AdminEmail != "" {
		t.Fatalf("a domain with an admin names none: %+v, %v", more, err)
	}
}

func TestFakeExistingDomainLifecycle(t *testing.T) {
	f, c := fakeAt(time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC))
	ctx := context.Background()
	setupAccount(t, f)
	d, err := f.ConnectDomain(ctx, "org", "acme-hasmail.com", ConnectionNameservers, "c1")
	if err != nil || d.Status != DomainConnecting || d.ConnectionStatus != ConnectionPending || len(d.Nameservers) != 2 || len(d.DNSConflicts) != 3 {
		t.Fatalf("connect = %+v, %v", d, err)
	}
	if _, err := f.ConnectDomain(ctx, "org", "acme-hasmail.com", ConnectionNameservers, "c2"); err == nil {
		t.Fatal("connecting a name twice should fail")
	} else if ge, _ := AsError(err); ge.Code != CodeDomainUnavailable || ge.DetailReason() != "already_added" {
		t.Fatalf("second connect = %v", err)
	}
	mbx := QuoteItem{Type: ItemMailboxes, Domain: "acme-hasmail.com", Provider: ProviderGoogle, Mailboxes: []NewMailbox{{LocalPart: "ada", FirstName: "Ada", LastName: "Lovelace"}}}
	if _, err := f.CreateQuote(ctx, "org", []QuoteItem{mbx}, "q1"); CodeOf(err) != CodeDomainNotReady {
		t.Fatalf("quote while pending: %v", err)
	}
	if _, err := f.RenewDomain(ctx, d.ID, 1, c.t, "r1"); CodeOf(err) != CodeNotRegisteredHere {
		t.Fatalf("renewing an existing domain: %v", err)
	}
	c.advance(fakeSeenAfter)
	f.Tick()
	d, _ = f.GetDomain(ctx, d.ID)
	if d.Status != DomainActive || d.ConnectionStatus != ConnectionActive {
		t.Fatalf("after the delegation is seen = %+v", d)
	}
	if _, err := f.CreateQuote(ctx, "org", []QuoteItem{mbx}, "q2"); err == nil {
		t.Fatal("conflicting records should refuse the quote")
	} else if ge, _ := AsError(err); ge.Code != CodeDNSRecordConflict || len(ge.Conflicts()) != 3 {
		t.Fatalf("quote with conflicts = %v", err)
	}
	for i, cf := range d.DNSConflicts {
		if err := f.DeleteDNS(ctx, d.ID, *cf.RecordID, "del"+string(rune('a'+i))); err != nil {
			t.Fatal(err)
		}
	}
	if o := order(t, f, c, mbx); o.Status != OrderCompleted || o.Steps[0].Status != StepSkipped {
		t.Fatalf("order on the existing domain = %+v", o)
	}
	if err := f.RemoveDomain(ctx, d.ID, "rm1"); CodeOf(err) != CodeStateConflict {
		t.Fatalf("removing a domain with mailboxes: %v", err)
	}
}

func TestFakeExistingDomainExpiresAndRegenerates(t *testing.T) {
	f, c := fakeAt(time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC))
	ctx := context.Background()
	setupAccount(t, f)
	d, err := f.ConnectDomain(ctx, "org", "acme-nsstuck.com", ConnectionNameservers, "c1")
	if err != nil {
		t.Fatal(err)
	}
	first := d.Nameservers
	c.advance(3 * time.Minute)
	f.Tick()
	if d, _ = f.GetDomain(ctx, d.ID); d.ConnectionStatus != ConnectionExpired {
		t.Fatalf("nsstuck after 3 minutes = %s", d.ConnectionStatus)
	}
	d, err = f.RegenerateConnection(ctx, d.ID, "g1")
	if err != nil || d.ConnectionStatus != ConnectionPending || d.Nameservers[0] == first[0] {
		t.Fatalf("regenerate = %+v, %v (first %v)", d, err, first)
	}
	if err := f.RemoveDomain(ctx, d.ID, "rm"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.GetDomain(ctx, d.ID); CodeOf(err) != CodeNotFound {
		t.Fatalf("a removed domain: %v", err)
	}
}

func TestFakeManualConnectionAndRecords(t *testing.T) {
	f, c := fakeAt(time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC))
	ctx := context.Background()
	setupAccount(t, f)
	d, err := f.ConnectDomain(ctx, "org", "acme-records.com", ConnectionManual, "c1")
	if err != nil || d.DNSHost != DNSHostCustomer {
		t.Fatalf("manual connect = %+v, %v", d, err)
	}
	recs, _ := f.ListDNS(ctx, d.ID)
	if len(recs) != 1 || recs[0].Purpose != "ownership" || !recs[0].Managed {
		t.Fatalf("manual records = %+v", recs)
	}
	if _, err := f.AddDNS(ctx, d.ID, DNSRecordInput{Type: "CNAME", Name: "link", Content: "t.example.net"}, "a1"); CodeOf(err) != CodeStateConflict {
		t.Fatalf("adding to a zone the customer holds: %v", err)
	}
	// A registered domain's zone is GraphKit's: records come and go.
	o := order(t, f, c, QuoteItem{Type: ItemDomain, Domain: "acmezone.com"},
		QuoteItem{Type: ItemMailboxes, Domain: "acmezone.com", Provider: ProviderGoogle, Mailboxes: []NewMailbox{{LocalPart: "ada", FirstName: "Ada", LastName: "Lovelace"}}})
	zone := o.Items[0].DomainID
	if dom, _ := f.GetDomain(ctx, zone); len(dom.Nameservers) != 2 || dom.Connection != ConnectionRegistered || dom.RegistrantVerification == nil {
		t.Fatalf("registered domain = %+v", dom)
	}
	r, err := f.AddDNS(ctx, zone, DNSRecordInput{Type: "CNAME", Name: "link", Content: "t.example.net", TTL: 300}, "a2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.AddDNS(ctx, zone, DNSRecordInput{Type: "MX", Name: "@", Content: "mx.other.example"}, "a3"); CodeOf(err) != CodeDNSRecordConflict {
		t.Fatalf("a second MX: %v", err)
	}
	content := "t2.example.net"
	if got, err := f.UpdateDNS(ctx, zone, r.ID, DNSRecordPatch{Content: &content}, "u1"); err != nil || got.Content != content {
		t.Fatalf("update = %+v, %v", got, err)
	}
	recs, _ = f.ListDNS(ctx, zone)
	for _, rec := range recs {
		if rec.Managed {
			if err := f.DeleteDNS(ctx, zone, rec.ID, "dm"); CodeOf(err) != CodeDNSRecordManaged {
				t.Fatalf("deleting a managed record: %v", err)
			}
			break
		}
	}
	check, err := f.CheckDNS(ctx, zone, "k1")
	if err != nil || len(check.Data) != len(recs) || check.Data[0].Status != "ok" {
		t.Fatalf("check = %+v, %v", check, err)
	}
	if err := f.DeleteDNS(ctx, zone, r.ID, "d1"); err != nil {
		t.Fatal(err)
	}
	if err := f.RemoveDomain(ctx, zone, "rm"); CodeOf(err) != CodeStateConflict {
		t.Fatalf("removing a registered domain: %v", err)
	}
}

func TestFakeWebhookSecretIsStable(t *testing.T) {
	f, _ := fakeAt(time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC))
	ctx := context.Background()
	a, err := f.PutWebhook(ctx, "https://example.test/hook", nil, "w1")
	if err != nil || a.Secret == nil {
		t.Fatalf("first put = %+v, %v", a, err)
	}
	b, _ := f.PutWebhook(ctx, "https://example.test/hook2", nil, "w2")
	if b.Secret == nil || *b.Secret != *a.Secret {
		t.Fatal("a later PUT must return the current secret")
	}
	r, _ := f.RotateWebhook(ctx, "r1")
	if *r.Secret == *a.Secret || r.PreviousSecretExpiresAt == nil {
		t.Fatalf("rotate = %+v", r)
	}
	if got, _ := f.GetWebhook(ctx); got.Secret != nil {
		t.Fatal("GET must omit the secret")
	}
	if err := f.DeleteWebhook(ctx, "d1"); err != nil {
		t.Fatal(err)
	}
	if err := f.DeleteWebhook(ctx, "d2"); CodeOf(err) != CodeNotFound {
		t.Fatalf("deleting nothing: %v", err)
	}
	again, _ := f.PutWebhook(ctx, "https://example.test/hook3", []string{"order.updated"}, "w3")
	if again.Secret == nil || *again.Secret != *r.Secret {
		t.Fatal("the secret must survive a delete")
	}
}

func TestFakeSuspendedPartner(t *testing.T) {
	f, _ := fakeAt(time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC))
	ctx := context.Background()
	f.Suspend(true)
	if _, err := f.Search(ctx, "acme", nil, 5, false); CodeOf(err) != CodePartnerSuspended {
		t.Fatalf("search while suspended: %v", err)
	}
	if _, err := f.UpsertAccount(ctx, "org", AccountInput{Name: "x"}, "k"); err == nil {
		t.Fatal("writes are refused while suspended")
	} else if ge, _ := AsError(err); ge.Status != 403 || !ge.AccountLevel() {
		t.Fatalf("suspended = %+v", ge)
	}
}

func TestFakeMinimumYears(t *testing.T) {
	f, _ := fakeAt(time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC))
	ctx := context.Background()
	setupAccount(t, f)
	p, _ := f.Pricing(ctx)
	if p.MinYears("ai") != 2 || p.MinYears("com") != 1 {
		t.Fatalf("min years = %d, %d", p.MinYears("ai"), p.MinYears("com"))
	}
	if _, err := f.CreateQuote(ctx, "org", []QuoteItem{{Type: ItemDomain, Domain: "acmerobots.ai", Years: 1}}, "q1"); CodeOf(err) != CodeInvalidRequest {
		t.Fatalf("one year of .ai: %v", err)
	}
	q, err := f.CreateQuote(ctx, "org", []QuoteItem{{Type: ItemDomain, Domain: "acmerobots.ai"}}, "q2")
	if err != nil || q.Lines[0].Years != 2 {
		t.Fatalf("default years = %+v, %v", q, err)
	}
}

func TestFakeTransferRequestedAndAway(t *testing.T) {
	f, c := fakeAt(time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC))
	ctx := context.Background()
	setupAccount(t, f)
	o := order(t, f, c, QuoteItem{Type: ItemDomain, Domain: "acme-transferaway.com"})
	id := o.Items[0].DomainID
	t1, err := f.TransferOut(ctx, id, "t1")
	if err != nil || t1.Domain == nil || t1.Domain.Transfer == nil {
		t.Fatalf("transfer = %+v, %v", t1, err)
	}
	c.advance(2 * time.Minute)
	f.Tick()
	if d, _ := f.GetDomain(ctx, id); d.Status != DomainTransferredOut {
		t.Fatalf("transferaway after 2 minutes = %s", d.Status)
	}
}

func TestFakeMailboxesNeedBothNames(t *testing.T) {
	f, _ := fakeAt(time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC))
	setupAccount(t, f)
	_, err := f.CreateQuote(context.Background(), "org", []QuoteItem{{Type: ItemDomain, Domain: "acmenames.com"},
		{Type: ItemMailboxes, Domain: "acmenames.com", Provider: ProviderGoogle, Mailboxes: []NewMailbox{{LocalPart: "ada", FirstName: "Ada"}}}}, "q")
	if CodeOf(err) != CodeInvalidRequest {
		t.Fatalf("a mailbox without a last name: %v", err)
	}
}
