package sendingdomain

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/domainproof"
	"github.com/warmbly/warmbly/internal/repository"
)

type fakeDNS struct {
	txt   map[string][]string
	cname map[string]string
	ips   map[string][]string
	fail  bool
}

func (f *fakeDNS) LookupTXT(_ context.Context, name string) ([]string, error) {
	if f.fail {
		return nil, &net.DNSError{Err: "timeout", Name: name, IsTimeout: true}
	}
	if v, ok := f.txt[name]; ok {
		return v, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
}

func (f *fakeDNS) LookupCNAME(_ context.Context, host string) (string, error) {
	if f.fail {
		return "", &net.DNSError{Err: "timeout", Name: host, IsTimeout: true}
	}
	if v, ok := f.cname[host]; ok {
		return v + ".", nil
	}
	if _, ok := f.ips[host]; ok {
		return host + ".", nil
	}
	return "", &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
}

func (f *fakeDNS) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	if f.fail {
		return nil, &net.DNSError{Err: "timeout", Name: host, IsTimeout: true}
	}
	if c, ok := f.cname[host]; ok {
		host = c
	}
	var out []net.IPAddr
	for _, ip := range f.ips[host] {
		out = append(out, net.IPAddr{IP: net.ParseIP(ip)})
	}
	if len(out) == 0 {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	return out, nil
}

type memRedirects struct {
	rows map[uuid.UUID]*models.DomainRedirect
}

func (m *memRedirects) Upsert(_ context.Context, r *models.DomainRedirect, _ uuid.UUID) error {
	for _, e := range m.rows {
		if e.OrganizationID == r.OrganizationID && e.Domain == r.Domain {
			e.TargetURL, e.IncludeWWW = r.TargetURL, r.IncludeWWW
			*r = *e
			return nil
		}
	}
	cp := *r
	cp.CreatedAt = time.Now()
	m.rows[r.ID] = &cp
	return nil
}
func (m *memRedirects) Get(_ context.Context, org uuid.UUID, domain string) (*models.DomainRedirect, error) {
	for _, e := range m.rows {
		if e.OrganizationID == org && e.Domain == domain {
			cp := *e
			return &cp, nil
		}
	}
	return nil, nil
}
func (m *memRedirects) List(context.Context, uuid.UUID) ([]models.DomainRedirect, error) {
	return nil, nil
}
func (m *memRedirects) Delete(context.Context, uuid.UUID, string) (bool, error) { return true, nil }
func (m *memRedirects) SetCheck(_ context.Context, id uuid.UUID, verified bool, last string) error {
	for _, e := range m.rows {
		if e.ID != id && e.Domain == m.rows[id].Domain && e.Verified && verified {
			return repository.ErrRedirectTaken
		}
	}
	m.rows[id].Verified, m.rows[id].LastError = verified, last
	return nil
}
func (m *memRedirects) Due(context.Context, int) ([]models.DomainRedirect, error) { return nil, nil }
func (m *memRedirects) Lookup(context.Context, string) (string, bool, error)      { return "", false, nil }

type fakeMailboxes struct{ counts map[string]int }

func (f *fakeMailboxes) DomainsOverview(context.Context, uuid.UUID) ([]models.SendingDomain, *errx.Error) {
	return nil, nil
}
func (f *fakeMailboxes) CountDomainMailboxes(_ context.Context, _ uuid.UUID, domain string) (int, *errx.Error) {
	return f.counts[domain], nil
}
func (f *fakeMailboxes) SetDomainTracking(context.Context, uuid.UUID, string, string, bool, *time.Time) (int, *errx.Error) {
	return 3, nil
}

func newTest(dns *fakeDNS) (*Service, *memRedirects) {
	repo := &memRedirects{rows: map[uuid.UUID]*models.DomainRedirect{}}
	s := NewService(repo, &fakeMailboxes{counts: map[string]int{"acme.io": 2}}, dns, domainproof.New("test-secret"))
	s.target = func() string { return "track.warmbly.test" }
	return s, repo
}

func baseDNS() *fakeDNS {
	return &fakeDNS{txt: map[string][]string{}, cname: map[string]string{}, ips: map[string][]string{"track.warmbly.test": {"203.0.113.10"}}}
}

func TestCleanTarget(t *testing.T) {
	if got, xerr := cleanTarget("acme.com", "acme.io"); xerr != nil || got != "https://acme.com" {
		t.Fatalf("got %q, %v", got, xerr)
	}
	for _, bad := range []string{"javascript:alert(1)", "https://acme.io/x", "https://www.acme.io", "https://user@evil.com", ""} {
		if _, xerr := cleanTarget(bad, "acme.io"); xerr == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

func TestRedirectVerifiesOnlyWithOwnershipAndAddress(t *testing.T) {
	dns := baseDNS()
	s, repo := newTest(dns)
	org := uuid.New()
	r, xerr := s.SetRedirect(context.Background(), org, uuid.New(), "ACME.io", RedirectInput{TargetURL: "https://acme.com"})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if r.Verified {
		t.Fatal("verified with no DNS at all")
	}
	var txt, root models.DNSRecord
	for _, rec := range r.Records {
		switch rec.Purpose {
		case "ownership":
			txt = rec
		case "root":
			root = rec
		}
	}
	if txt.Name != "_warmbly.acme.io" || !strings.HasPrefix(txt.Value, "warmbly-verify=") || root.Type != "A" || root.Value != "203.0.113.10" {
		t.Fatalf("records = %+v", r.Records)
	}

	// Pointing the root here without the TXT record proves nothing about who owns it.
	dns.ips["acme.io"] = []string{"203.0.113.10"}
	if r, _ = s.VerifyRedirect(context.Background(), org, "acme.io"); r.Verified {
		t.Fatal("verified without the ownership record")
	}
	dns.txt["_warmbly.acme.io"] = []string{txt.Value}
	if r, _ = s.VerifyRedirect(context.Background(), org, "acme.io"); !r.Verified {
		t.Fatalf("not verified with both records: %s", r.LastError)
	}

	// A resolver outage does not take a verified redirect down; a removed record does.
	dns.fail = true
	if r, _ = s.VerifyRedirect(context.Background(), org, "acme.io"); !r.Verified {
		t.Fatal("a lookup failure unverified the redirect")
	}
	dns.fail = false
	delete(dns.txt, "_warmbly.acme.io")
	if r, _ = s.VerifyRedirect(context.Background(), org, "acme.io"); r.Verified {
		t.Fatal("still verified after the ownership record was removed")
	}
	_ = repo
}

func TestRedirectRefusesForeignAndSharedDomains(t *testing.T) {
	s, _ := newTest(baseDNS())
	org := uuid.New()
	if _, xerr := s.SetRedirect(context.Background(), org, uuid.New(), "other.io", RedirectInput{TargetURL: "https://x.com"}); xerr == nil || xerr.Identifier != ErrIDNotYours {
		t.Fatalf("a domain with no mailbox = %v", xerr)
	}
	if _, xerr := s.SetRedirect(context.Background(), org, uuid.New(), "gmail.com", RedirectInput{TargetURL: "https://x.com"}); xerr == nil || xerr.Identifier != ErrIDConsumerDomain {
		t.Fatalf("a shared provider domain = %v", xerr)
	}
}

func TestTrackingSuggestionFindsAnExistingRecord(t *testing.T) {
	dns := baseDNS()
	s, _ := newTest(dns)
	if sug := s.TrackingSuggestion(context.Background(), "acme.io", nil); sug.Status != "suggested" || sug.Host != "link.acme.io" {
		t.Fatalf("suggestion = %+v", sug)
	}
	dns.cname["track.acme.io"] = "track.warmbly.test"
	if sug := s.TrackingSuggestion(context.Background(), "acme.io", nil); sug.Status != "found" || sug.Host != "track.acme.io" {
		t.Fatalf("suggestion = %+v", sug)
	}
	inUse := []models.TrackingDomainUse{{Host: "t.acme.io", Verified: true, Mailboxes: 2}}
	if sug := s.TrackingSuggestion(context.Background(), "acme.io", inUse); sug.Status != "active" || sug.Host != "t.acme.io" {
		t.Fatalf("suggestion = %+v", sug)
	}
}

func TestRedirectProofIsNotTheOneAnotherWorkspacePublished(t *testing.T) {
	dns := baseDNS()
	s, _ := newTest(dns)
	victim, attacker := uuid.New(), uuid.New()
	dns.ips["acme.io"] = []string{"203.0.113.10"}
	// The victim's record is public; publishing it proves nothing for another workspace.
	dns.txt["_warmbly.acme.io"] = []string{s.proof.Value(victim, "acme.io")}
	r, xerr := s.SetRedirect(context.Background(), attacker, uuid.New(), "acme.io", RedirectInput{TargetURL: "https://evil.example"})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if r.Verified {
		t.Fatal("another workspace's proof verified the redirect")
	}
}

type fakeVendors struct {
	links    map[string]models.VendorDomainLink
	forwards map[string]string
	records  []string
}

func (f *fakeVendors) DomainLinks(context.Context, uuid.UUID) (map[string]models.VendorDomainLink, *errx.Error) {
	return f.links, nil
}
func (f *fakeVendors) SetForwarding(_ context.Context, _ uuid.UUID, domain, target string) (*models.VendorDomainLink, *errx.Error) {
	f.forwards[domain] = target
	l := f.links[domain]
	l.Forwarding = target
	return &l, nil
}
func (f *fakeVendors) UpsertDNS(_ context.Context, _ uuid.UUID, domain, typ, name, value string) *errx.Error {
	f.records = append(f.records, domain+" "+typ+" "+name+" "+value)
	return nil
}

func TestVendorDomainsTakeTheEasyPath(t *testing.T) {
	s, repo := newTest(baseDNS())
	v := &fakeVendors{links: map[string]models.VendorDomainLink{"acme.io": {Vendor: "zapmail", CanForward: true, CanDNS: true}}, forwards: map[string]string{}}
	s.WireVendors(v)
	org, user := uuid.New(), uuid.New()
	ctx := context.Background()

	if xerr := s.AutoRedirect(ctx, org, user, "acme.io", "acme.com"); xerr != nil {
		t.Fatal(xerr)
	}
	if v.forwards["acme.io"] != "https://acme.com" || len(repo.rows) != 0 {
		t.Fatalf("a vendor domain was not forwarded by its vendor: %v, %d redirect rows", v.forwards, len(repo.rows))
	}
	if _, xerr := s.VendorForward(ctx, org, user, "other.io", "https://x.com"); xerr == nil || xerr.Identifier != ErrIDNotYours {
		t.Fatalf("forwarded a domain with no mailbox: %v", xerr)
	}
	if _, xerr := s.VendorForward(ctx, org, user, "acme.io", "https://acme.io/loop"); xerr == nil {
		t.Fatal("forwarded a domain to itself")
	}

	s.AutoTracking(ctx, org, "acme.io", "track.acme.io")
	if len(v.records) != 1 || v.records[0] != "acme.io CNAME track.acme.io track.warmbly.test" {
		t.Fatalf("records = %v", v.records)
	}
	if _, _, xerr := s.VendorTracking(ctx, org, "acme.io", "track.elsewhere.com"); xerr == nil || xerr.Identifier != ErrIDTarget {
		t.Fatalf("wrote a record for a host outside the domain: %v", xerr)
	}
	for _, host := range []string{"selector1._domainkey.acme.io", "_dmarc.acme.io", "x y.acme.io"} {
		if _, _, xerr := s.VendorTracking(ctx, org, "acme.io", host); xerr == nil {
			t.Fatalf("%q was accepted", host)
		}
	}
	if len(v.records) != 1 {
		t.Fatalf("a refused host reached the vendor: %v", v.records)
	}

	// Without a vendor, the redirect is served here once DNS proves the domain.
	s.WireVendors(&fakeVendors{forwards: map[string]string{}})
	if xerr := s.AutoRedirect(ctx, org, user, "acme.io", "acme.com"); xerr != nil || len(repo.rows) != 1 {
		t.Fatalf("no redirect row without a vendor: %v", xerr)
	}
}

func TestBulkSetupTakesEachDomainsEasiestPath(t *testing.T) {
	s, repo := newTest(baseDNS())
	s.mailboxes = &fakeMailboxes{counts: map[string]int{"acme.io": 2, "acme.co": 1}}
	v := &fakeVendors{links: map[string]models.VendorDomainLink{"acme.io": {Vendor: "inboxkit", CanForward: true, CanDNS: true, DNSTypes: []string{"CNAME"}}}, forwards: map[string]string{}}
	s.WireVendors(v)
	org, user := uuid.New(), uuid.New()
	ctx := context.Background()

	for _, in := range []BulkInput{
		{TrackingLabel: "link"},
		{Domains: []string{"acme.io"}},
		{Domains: []string{"acme.io"}, TrackingLabel: "link.x"},
		{Domains: []string{"acme.io"}, TrackingLabel: "-link"},
	} {
		if _, xerr := s.BulkSetup(ctx, org, user, in); xerr == nil || xerr.Identifier != ErrIDBulkInvalid {
			t.Fatalf("%+v was accepted: %v", in, xerr)
		}
	}

	rows, xerr := s.BulkSetup(ctx, org, user, BulkInput{Domains: []string{"ACME.io", "acme.co", "other.io", "www.acme.io"}, TrackingLabel: "link", RedirectURL: "acme.com"})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if len(rows) != 3 {
		t.Fatalf("duplicates were not folded: %+v", rows)
	}
	by := map[string]BulkResult{}
	for _, r := range rows {
		by[r.Domain] = r
	}
	if r := by["acme.io"]; r.Tracking.Via != "vendor" || r.Tracking.Host != "link.acme.io" || r.Redirect.Via != "vendor" || r.Redirect.Error != "" {
		t.Fatalf("vendor domain = %+v %+v", r.Tracking, r.Redirect)
	}
	if len(v.records) != 1 || v.records[0] != "acme.io CNAME link.acme.io track.warmbly.test" || v.forwards["acme.io"] != "https://acme.com" {
		t.Fatalf("vendor was not used: %v %v", v.records, v.forwards)
	}
	if r := by["acme.co"]; r.Tracking.Via != "dns" || r.Tracking.Error != "" || r.Redirect.Via != "dns" || r.Redirect.Error != "" || len(repo.rows) != 1 {
		t.Fatalf("DNS domain = %+v %+v, %d redirect rows", r.Tracking, r.Redirect, len(repo.rows))
	}
	if r := by["other.io"]; r.Tracking.Code != ErrIDNotYours || r.Redirect.Code != ErrIDNotYours {
		t.Fatalf("a domain outside the workspace = %+v %+v", r.Tracking, r.Redirect)
	}

	// A domain's own value wins over the shared one; the others keep the shared one.
	rows, xerr = s.BulkSetup(ctx, org, user, BulkInput{
		Domains: []string{"acme.io", "acme.co"}, TrackingLabel: "link", RedirectURL: "acme.com",
		TrackingHosts: map[string]string{"ACME.co": "go.acme.co"}, RedirectURLs: map[string]string{"acme.co": "https://acme.co.uk"},
	})
	if xerr != nil {
		t.Fatal(xerr)
	}
	for _, r := range rows {
		want, wantURL := "link."+r.Domain, "https://acme.com"
		if r.Domain == "acme.co" {
			want, wantURL = "go.acme.co", "https://acme.co.uk"
		}
		if r.Tracking.Host != want || (r.Redirect.Via == "dns" && r.Redirect.TargetURL != wantURL) {
			t.Fatalf("%s = %+v %+v", r.Domain, r.Tracking, r.Redirect)
		}
	}
	if v.forwards["acme.io"] != "https://acme.com" {
		t.Fatalf("the shared website did not reach acme.io: %v", v.forwards)
	}
}
