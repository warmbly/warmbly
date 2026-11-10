package leadfinder

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/credits"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
	"github.com/warmbly/warmbly/internal/pkg/webread"
)

type stubReader struct {
	html  string
	err   error
	reads []string
}

func (r *stubReader) Name() string { return "stub" }

func (r *stubReader) Read(_ context.Context, target string) (*webread.Page, error) {
	r.reads = append(r.reads, target)
	if r.err != nil {
		return nil, r.err
	}
	return &webread.Page{HTML: r.html, URL: target, Via: "stub"}, nil
}

// blockingSite answers 403 to every page but robots.txt.
func blockingSite(t *testing.T) *Crawler {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	c := NewCrawler("test")
	c.client = &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
		},
	}}
	return c
}

func TestCrawlReadsABlockedSiteThroughTheReader(t *testing.T) {
	r := &stubReader{html: `<html><head><title>Acme Plumbing</title></head><body>
		<a href="/contact">Contact</a><p>Write to office@acmeplumbing.com</p></body></html>`}
	res, err := blockingSite(t).withFallback(r).Crawl(context.Background(), "http://acmeplumbing.com/")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Site.Reachable || res.Site.ReadVia != "stub" || res.Site.Title != "Acme Plumbing" {
		t.Fatalf("site = %+v", res.Site)
	}
	if len(res.Emails) != 1 || res.Emails[0].Email != "office@acmeplumbing.com" {
		t.Fatalf("emails = %+v", res.Emails)
	}
	// The contact page goes straight to the reader too.
	if len(r.reads) != 2 || r.reads[1] != "http://acmeplumbing.com/contact" {
		t.Fatalf("reads = %v", r.reads)
	}
}

func TestCrawlSaysTheReaderFailedToo(t *testing.T) {
	res, err := blockingSite(t).withFallback(&stubReader{err: errors.New("no")}).Crawl(context.Background(), "http://acmeplumbing.com/")
	if err != nil {
		t.Fatal(err)
	}
	if res.Site.Reachable || res.Site.Unread != models.LeadSiteBlocked || res.Site.Error != "the website answered 403, and the reader service could not get past it either" {
		t.Fatalf("site = %+v", res.Site)
	}
	plain, _ := blockingSite(t).Crawl(context.Background(), "http://acmeplumbing.com/")
	if plain.Site.Error != "the website answered 403" || plain.Site.Unread != models.LeadSiteBlocked {
		t.Fatalf("without a reader: %q", plain.Site.Error)
	}
}

func TestSearchedHostsSkipDirectories(t *testing.T) {
	got := searchedHosts([]generation.SearchResult{
		{URL: "https://www.yelp.com/biz/acme-plumbing-boise"},
		{URL: "https://www.facebook.com/acmeplumbing"},
		{URL: "https://www.acmeplumbing.com/services"},
		{URL: "https://maps.google.co.uk/?q=acme"},
		{URL: "mailto:office@acmeplumbing.com"},
		{URL: "https://www.acmeplumbing.com/"},
		{URL: "https://acme-boise.net/"},
	})
	if len(got) != 2 || got[0] != "www.acmeplumbing.com" || got[1] != "acme-boise.net" {
		t.Fatalf("hosts = %v", got)
	}
}

func TestSearchedEmailsKeepTheBusinessDomain(t *testing.T) {
	got := searchedEmails([]generation.SearchResult{
		{URL: "https://dir.example/acme", Snippet: "Email office [at] acmeplumbing [dot] com or jobs@acmeplumbing.com"},
		{URL: "https://other.example/", Title: "Contact sales@someoneelse.com", Snippet: "office@acmeplumbing.com"},
	}, "acmeplumbing.com")
	if len(got) != 2 || got[0].Email != "office@acmeplumbing.com" || got[1].Email != "jobs@acmeplumbing.com" {
		t.Fatalf("emails = %+v", got)
	}
	if got[0].Source != emailSourceSearch || got[0].URL != "https://dir.example/acme" {
		t.Fatalf("first = %+v", got[0])
	}
}

type countingSearch struct {
	res   []generation.SearchResult
	calls int
}

func (c *countingSearch) Search(context.Context, string, int) ([]generation.SearchResult, error) {
	c.calls++
	return c.res, nil
}

func TestSearchPrefersTheWorkspaceSource(t *testing.T) {
	own := &countingSearch{}
	inst := &countingSearch{res: []generation.SearchResult{{URL: "https://acme.com/"}}}
	s := &Service{}
	sources := []searchSource{{name: "mcp", client: own}, {name: "instance", client: inst}}
	if got := s.search(context.Background(), uuid.Nil, sources, "q", "run:lead"); len(got) != 1 || own.calls != 1 || inst.calls != 1 {
		t.Fatalf("got %v, own %d, instance %d", got, own.calls, inst.calls)
	}
	own.res = []generation.SearchResult{{URL: "https://own.com/"}}
	if got := s.search(context.Background(), uuid.Nil, sources, "q", "run:lead"); got[0].URL != "https://own.com/" || inst.calls != 1 {
		t.Fatalf("the instance search ran after the workspace's answered: %v", got)
	}
}

func TestSiteQueryNamesThePlace(t *testing.T) {
	if q := siteQuery(&models.Lead{Name: "Acme Plumbing", City: "Boise", Street: "1 Main St"}); q != `"Acme Plumbing" Boise` {
		t.Fatalf("query = %q", q)
	}
}

type ledger struct {
	charged, refunded []string
}

func (l *ledger) Quote(context.Context, credits.Usage) models.Micro { return 10 }

func (l *ledger) ChargeUpfront(_ context.Context, _ uuid.UUID, _ credits.Usage, _, key string) (models.Micro, error) {
	l.charged = append(l.charged, key)
	return 10, nil
}

func (l *ledger) RefundCharge(_ context.Context, _ uuid.UUID, key, _ string) (models.Micro, error) {
	l.refunded = append(l.refunded, key)
	return 10, nil
}

func (l *ledger) Unmetered() bool { return false }

func TestAnInstanceSearchThatFindsNothingIsRefunded(t *testing.T) {
	l := &ledger{}
	s := &Service{Deps: Deps{Credits: l}}
	empty := []searchSource{{name: "instance", client: &countingSearch{}, charged: true}}
	if got := s.search(context.Background(), uuid.Nil, empty, "q", "r:l"); got != nil {
		t.Fatalf("got %v", got)
	}
	if len(l.charged) != 1 || len(l.refunded) != 1 || l.charged[0] != l.refunded[0] {
		t.Fatalf("charged %v, refunded %v", l.charged, l.refunded)
	}
	found := []searchSource{{name: "instance", client: &countingSearch{res: []generation.SearchResult{{URL: "https://a.com/"}}}, charged: true}}
	s.search(context.Background(), uuid.Nil, found, "q", "r:m")
	if len(l.charged) != 2 || len(l.refunded) != 1 {
		t.Fatalf("a search that found something was refunded: %v", l.refunded)
	}
}

func TestHTTPRetrySkipsTheReaderItAlreadyTried(t *testing.T) {
	c := NewCrawler("test").withFallback(&stubReader{})
	if c.httpRetry(&blockedError{status: 403, reader: true}).fallback != nil {
		t.Fatal("the http retry would ask the reader again")
	}
	if c.httpRetry(errors.New("the website did not answer")).fallback == nil {
		t.Fatal("an unanswered https origin lost its reader for the http retry")
	}
}

func TestASiteMakesAtMostFourReaderRequests(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			http.NotFound(w, r)
		case "/":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<a href="/contact">Contact</a><a href="/team">Team</a><a href="/about">About</a>
				<a href="/impressum">Impressum</a><a href="/staff">Staff</a><a href="/people">People</a>`))
		default:
			http.Error(w, "forbidden", http.StatusForbidden)
		}
	}))
	defer srv.Close()
	c := NewCrawler("test")
	c.client = &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
		},
	}}
	r := &stubReader{err: errors.New("no")}
	if _, err := c.withFallback(r).Crawl(context.Background(), "http://acme.com/"); err != nil {
		t.Fatal(err)
	}
	if len(r.reads) != crawlMaxReaderPages {
		t.Fatalf("reader requests = %d", len(r.reads))
	}
}
