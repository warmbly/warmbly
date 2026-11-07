package sendingdomain

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/owndomain"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type memWebsites struct {
	rows map[string]repository.WebsiteDomain
}

func websiteKey(org uuid.UUID, domain string) string { return org.String() + "|" + domain }

func (m *memWebsites) Verified(_ context.Context, org uuid.UUID) ([]string, error) {
	var out []string
	for _, w := range m.rows {
		if w.OrganizationID == org {
			out = append(out, w.Domain)
		}
	}
	return out, nil
}
func (m *memWebsites) Mark(_ context.Context, org uuid.UUID, domain string) error {
	w, ok := m.rows[websiteKey(org, domain)]
	if !ok {
		w = repository.WebsiteDomain{OrganizationID: org, Domain: domain, VerifiedAt: time.Now()}
	}
	w.LastCheckedAt = time.Now()
	m.rows[websiteKey(org, domain)] = w
	return nil
}
func (m *memWebsites) Touch(_ context.Context, org uuid.UUID, domain string) error {
	if w, ok := m.rows[websiteKey(org, domain)]; ok {
		w.LastCheckedAt = time.Now()
		m.rows[websiteKey(org, domain)] = w
	}
	return nil
}
func (m *memWebsites) Drop(_ context.Context, org uuid.UUID, domain string) error {
	delete(m.rows, websiteKey(org, domain))
	return nil
}
func (m *memWebsites) Due(_ context.Context, before time.Time, _ int) ([]repository.WebsiteDomain, error) {
	var out []repository.WebsiteDomain
	for _, w := range m.rows {
		if w.LastCheckedAt.Before(before) {
			out = append(out, w)
		}
	}
	return out, nil
}

func (m *memWebsites) age(d time.Duration) {
	for k, w := range m.rows {
		w.LastCheckedAt = time.Now().Add(-d)
		m.rows[k] = w
	}
}

// newWebsiteTest is a workspace sending from acme.io whose main website, frostlogic.se, has no mailbox.
func newWebsiteTest() (*Service, *fakeDNS, *memWebsites, *fakeReach) {
	dns := baseDNS()
	s, _ := newTest(dns)
	mb := &fakeMailboxes{counts: map[string]int{"acme.io": 2}}
	s.mailboxes = mb
	sites := &memWebsites{rows: map[string]repository.WebsiteDomain{}}
	own := owndomain.New(mb, nil)
	own.WireWebsites(sites, s.proof, dns)
	s.WireOwnDomains(own)
	reach := &fakeReach{verdict: models.RedirectReach{Status: models.RedirectReachOK}}
	s.reach = reach
	return s, dns, sites, reach
}

func TestAWebsiteProvedWithTheWorkspacesRecordTakesRedirects(t *testing.T) {
	s, dns, _, reach := newWebsiteTest()
	ctx := context.Background()
	org := uuid.New()

	if _, xerr := s.SetRedirect(ctx, org, uuid.New(), "acme.io", RedirectInput{TargetURL: "https://www.frostlogic.se"}); xerr == nil || xerr.Identifier != owndomain.ErrIDNotOwn {
		t.Fatalf("an unproved website was accepted: %v", xerr)
	}
	proof, xerr := s.WebsiteProof(ctx, org, "acme.io", "www.frostlogic.se")
	if xerr != nil {
		t.Fatal(xerr)
	}
	if proof.Domain != "frostlogic.se" || proof.Verified || proof.Record == nil || proof.Record.Name != "_warmbly.frostlogic.se" || proof.Record.Value != s.proof.Value(org, "frostlogic.se") {
		t.Fatalf("proof = %+v", proof)
	}

	// Another workspace's record proves the website for that workspace only.
	dns.txt["_warmbly.frostlogic.se"] = []string{s.proof.Value(uuid.New(), "frostlogic.se")}
	if _, xerr := s.SetRedirect(ctx, org, uuid.New(), "acme.io", RedirectInput{TargetURL: "https://www.frostlogic.se"}); xerr == nil {
		t.Fatal("another workspace's record proved the website")
	}

	dns.txt["_warmbly.frostlogic.se"] = append(dns.txt["_warmbly.frostlogic.se"], proof.Record.Value)
	dns.ips["acme.io"] = dns.ips["track.warmbly.test"]
	dns.txt["_warmbly.acme.io"] = []string{s.proof.Value(org, "acme.io")}
	r, xerr := s.SetRedirect(ctx, org, uuid.New(), "acme.io", RedirectInput{TargetURL: "https://www.frostlogic.se"})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if !r.Verified || r.TargetActive == nil || !*r.TargetActive || r.Website == nil || !r.Website.Verified || reach.probes == 0 {
		t.Fatalf("a proved website is not live: %+v", r)
	}
	if target, ok, err := s.Lookup(ctx, "acme.io"); err != nil || !ok || target != "https://www.frostlogic.se" {
		t.Fatalf("Lookup = %q, %v, %v", target, ok, err)
	}
}

func TestAnUnprovedWebsiteIsHeldWithoutBlamingTheProxy(t *testing.T) {
	s, dns, sites, reach := newWebsiteTest()
	ctx := context.Background()
	org := uuid.New()
	dns.txt["_warmbly.frostlogic.se"] = []string{s.proof.Value(org, "frostlogic.se")}
	dns.ips["acme.io"] = dns.ips["track.warmbly.test"]
	dns.txt["_warmbly.acme.io"] = []string{s.proof.Value(org, "acme.io")}
	if _, xerr := s.SetRedirect(ctx, org, uuid.New(), "acme.io", RedirectInput{TargetURL: "frostlogic.se"}); xerr != nil {
		t.Fatal(xerr)
	}

	// A lookup that could not tell keeps the website; a record that is gone drops it.
	sites.age(7 * time.Hour)
	dns.fail = true
	s.ownDomains().Recheck(ctx)
	dns.fail = false
	if len(sites.rows) != 1 {
		t.Fatal("a failed lookup dropped the website")
	}
	if due, _ := sites.Due(ctx, time.Now().Add(-6*time.Hour), 50); len(due) != 0 {
		t.Fatal("a failed lookup left the website at the front of the queue")
	}
	sites.age(7 * time.Hour)
	delete(dns.txt, "_warmbly.frostlogic.se")
	s.ownDomains().Recheck(ctx)
	if len(sites.rows) != 0 {
		t.Fatal("a website whose record is gone stayed proved")
	}
	if _, ok, err := s.Lookup(ctx, "acme.io"); err != nil || ok {
		t.Fatalf("an unproved website is served: %v, %v", ok, err)
	}

	before := reach.probes
	r, xerr := s.VerifyRedirect(ctx, org, "acme.io")
	if xerr != nil {
		t.Fatal(xerr)
	}
	if reach.probes != before || r.Reach != nil {
		t.Fatalf("the visit ran for a redirect this instance withholds: %+v", r.Reach)
	}
	if r.TargetActive == nil || *r.TargetActive || r.Website == nil || r.Website.Verified || r.Website.Record == nil || r.Website.Record.Name != "_warmbly.frostlogic.se" {
		t.Fatalf("the website's record is not shown: %+v", r)
	}

	// Publishing it again and checking puts the redirect back.
	dns.txt["_warmbly.frostlogic.se"] = []string{s.proof.Value(org, "frostlogic.se")}
	if r, _ = s.VerifyRedirect(ctx, org, "acme.io"); r.TargetActive == nil || !*r.TargetActive || r.Reach == nil {
		t.Fatalf("a re-proved website is not live: %+v", r)
	}
}

func TestASharedProviderIsNeverOfferedAsAWebsite(t *testing.T) {
	s, _, _, _ := newWebsiteTest()
	ctx := context.Background()
	org := uuid.New()
	if _, xerr := s.WebsiteProof(ctx, org, "acme.io", "https://gmail.com"); xerr == nil || xerr.Identifier != owndomain.ErrIDNotOwn {
		t.Fatalf("a shared provider was offered a record: %v", xerr)
	}
	if w := s.website(org, "https://mail.gmail.com/x", false); w != nil {
		t.Fatalf("a shared provider got a record: %+v", w)
	}
}

func TestAWebsiteIsItsRegistrableDomain(t *testing.T) {
	cases := map[string]string{
		"www.frostlogic.se":  "frostlogic.se",
		"shop.acme.co.uk":    "acme.co.uk",
		"acme.com":           "acme.com",
		"co.uk":              "",
		"203.0.113.7":        "",
		"Deep.Blog.ACME.com": "acme.com",
	}
	for host, want := range cases {
		if got := owndomain.Website(host); got != want {
			t.Errorf("Website(%q) = %q, want %q", host, got, want)
		}
	}
}
