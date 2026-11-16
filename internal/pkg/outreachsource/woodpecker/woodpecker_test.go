package woodpecker

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

const testKey = "wp-test-key"

// fake serves the documented responses from testdata.
type fake struct {
	t      *testing.T
	agency bool
	// limited answers this many requests to path with a rate limit first.
	limited map[string]int
	mu      sync.Mutex
	seen    []*http.Request
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.seen = append(f.seen, r)
	f.mu.Unlock()
	if r.Method != http.MethodGet {
		f.t.Errorf("non-GET request %s %s", r.Method, r.URL.Path)
	}
	if r.Header.Get("x-api-key") != testKey {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write(fixture(f.t, "unauthorized.json"))
		return
	}
	f.mu.Lock()
	if f.limited[r.URL.Path] > 0 {
		f.limited[r.URL.Path]--
		f.mu.Unlock()
		if strings.HasPrefix(r.URL.Path, "/v1/") {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write(fixture(f.t, "rate_limited_v1.json"))
		} else {
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(http.StatusTooManyRequests)
		}
		return
	}
	f.mu.Unlock()
	q := r.URL.Query()
	switch p := r.URL.Path; {
	case p == "/v1/me":
		_, _ = w.Write([]byte(`{}`))
	case p == "/v2/users":
		f.write(w, "users.json")
	case p == "/v2/agency/companies":
		if !f.agency {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		f.write(w, "companies.json")
	case p == "/v2/mailboxes":
		f.write(w, "mailboxes.json")
	case p == "/v1/campaign_list" && q.Get("id") == "":
		f.write(w, "campaign_list.json")
	case p == "/v1/campaign_list":
		f.write(w, "campaign_stats.json")
	case p == "/v2/campaigns/12345679":
		f.write(w, "campaign.json")
	case p == "/v2/campaigns/1235678":
		w.WriteHeader(http.StatusConflict)
		f.write(w, "campaign_unsupported.json")
	case p == "/v1/prospects":
		switch {
		case q.Get("campaigns_id") == "":
			f.write(w, "prospects_none.json")
		case q.Get("page") == "1":
			f.write(w, "prospects_page1.json")
		default:
			f.write(w, "prospects_page2.json")
		}
	case p == "/v2/blacklist/emails":
		// Pages count from 1 here: no page and page=1 are the same page.
		if pg := q.Get("page"); pg == "" || pg == "1" {
			f.write(w, "blacklist_emails_p1.json")
		} else {
			f.write(w, "blacklist_emails_p2.json")
		}
	case p == "/v2/blacklist/domains":
		f.write(w, "blacklist_domains.json")
	case p == "/v2/agency/blacklist/emails":
		f.write(w, "agency_blacklist_emails.json")
	case p == "/v2/agency/blacklist/domains":
		f.write(w, "agency_blacklist_domains.json")
	default:
		f.t.Errorf("unexpected request %s", r.URL)
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fake) write(w http.ResponseWriter, name string) { _, _ = w.Write(fixture(f.t, name)) }

func fixture(t *testing.T, name string) []byte {
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func open(t *testing.T, f *fake) (*session, *[]time.Duration) {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	var waits []time.Duration
	p := newProvider(srv.URL)
	p.sleep = func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }
	s, err := p.Open(context.Background(), outreachsource.Credentials{"api_key": testKey})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s.(*session), &waits
}

func TestOpen(t *testing.T) {
	srv := httptest.NewServer(&fake{t: t})
	defer srv.Close()
	p := newProvider(srv.URL)
	if _, err := p.Open(context.Background(), outreachsource.Credentials{"api_key": testKey}); err != nil {
		t.Fatalf("good key: %v", err)
	}
	_, err := p.Open(context.Background(), outreachsource.Credentials{"api_key": "wrong"})
	if !errors.Is(err, outreachsource.ErrKey) {
		t.Fatalf("bad key: got %v, want ErrKey", err)
	}
	if strings.Contains(err.Error(), "wrong") {
		t.Fatalf("error leaks the key: %v", err)
	}
	if _, err := p.Open(context.Background(), outreachsource.Credentials{}); !errors.Is(err, outreachsource.ErrKey) {
		t.Fatalf("no key: got %v", err)
	}
	if info := New().Info(); info.ID != "woodpecker" || !info.Workspaces || info.MailboxSecrets {
		t.Fatalf("info = %+v", info)
	}
}

func TestRateLimitRetried(t *testing.T) {
	f := &fake{t: t, limited: map[string]int{"/v1/me": 1, "/v2/mailboxes": 1, "/v1/campaign_list": 1}}
	s, waits := open(t, f)
	if _, err := s.Mailboxes(context.Background(), outreachsource.Workspace{}); err != nil {
		t.Fatalf("Mailboxes after 429: %v", err)
	}
	// v1 answers a rate limit with 409 E_TOO_MANY_REQUESTS.
	if _, err := campaignList(context.Background(), s.c); err != nil {
		t.Fatalf("campaign list after v1 409: %v", err)
	}
	if len(*waits) != 3 || (*waits)[1] != 7*time.Second {
		t.Fatalf("waits = %v", *waits)
	}
}

func TestWorkspaces(t *testing.T) {
	s, _ := open(t, &fake{t: t})
	ws, err := s.Workspaces(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ws) != 1 || ws[0].ID != "" || ws[0].Name != "Michael Scott" {
		t.Fatalf("single account workspaces = %+v", ws)
	}

	f := &fake{t: t, agency: true}
	s, _ = open(t, f)
	ws, err = s.Workspaces(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ws) != 2 || ws[0].ID != "" || ws[1].ID != "12345678" || ws[1].Name != "Dunmore High School" {
		t.Fatalf("agency workspaces = %+v", ws)
	}
	if _, err := s.Mailboxes(context.Background(), ws[1]); err != nil {
		t.Fatal(err)
	}
	last := f.seen[len(f.seen)-1]
	if last.Header.Get("x-company-id") != "12345678" {
		t.Fatalf("client request without x-company-id: %v", last.Header)
	}
}

func TestMailboxes(t *testing.T) {
	s, _ := open(t, &fake{t: t})
	boxes, err := s.Mailboxes(context.Background(), outreachsource.Workspace{})
	if err != nil {
		t.Fatal(err)
	}
	if len(boxes) != 3 {
		t.Fatalf("got %d mailboxes, want the 3 SMTP entries", len(boxes))
	}
	g, c, o := boxes[0], boxes[1], boxes[2]
	if g.Kind != outreachsource.MailboxGoogle || g.SMTP != nil || g.DailyCap != 50 || g.Warmup == nil || *g.Warmup {
		t.Fatalf("google = %+v", g)
	}
	if o.Kind != outreachsource.MailboxMicrosoft || o.Email != "erlich@usebachmanity.com" {
		t.Fatalf("office = %+v", o)
	}
	if c.Kind != outreachsource.MailboxSMTP || c.SMTP == nil || c.IMAP == nil {
		t.Fatalf("custom = %+v", c)
	}
	if c.SMTP.Host != "mail.privateemail.com" || c.SMTP.Port != 587 || c.SMTP.Security != outreachsource.SecurityStartTLS || c.IMAP.Port != 993 || c.IMAP.Security != outreachsource.SecuritySSL || c.SMTP.Username != "jared@getpiedpiper.com" {
		t.Fatalf("custom servers = %+v %+v", c.SMTP, c.IMAP)
	}
	if c.HasSecrets() || c.SMTP.Password != "" || c.Warmup == nil || !*c.Warmup || c.FromName != "Jared from Pied Piper" {
		t.Fatalf("custom = %+v", c)
	}
}

func TestCampaigns(t *testing.T) {
	s, _ := open(t, &fake{t: t})
	camps, err := s.Campaigns(context.Background(), outreachsource.Workspace{})
	if err != nil {
		t.Fatal(err)
	}
	if len(camps) != 2 {
		t.Fatalf("got %d campaigns", len(camps))
	}
	c := camps[0]
	if c.ID != "12345679" || c.Status != outreachsource.CampaignPaused || c.LeadCount != 868 || len(c.Senders) != 2 || !c.StopOnReply || c.CreatedAt == nil {
		t.Fatalf("campaign = %+v", c)
	}
	if !c.TrackOpens || !c.TrackClicks || !c.Unsubscribe {
		t.Fatalf("tracking = opens %v clicks %v unsub %v", c.TrackOpens, c.TrackClicks, c.Unsubscribe)
	}
	if len(c.Steps) != 2 {
		t.Fatalf("got %d email steps, want 2: %+v", len(c.Steps), c.Steps)
	}
	first, second := c.Steps[0], c.Steps[1]
	// The first email waits for the LinkedIn visit before it: 2 hours.
	if first.WaitDays != 0 || first.WaitMinutes != 120 || len(first.Variants) != 2 {
		t.Fatalf("first step = %+v", first)
	}
	a := first.Variants[0]
	if a.Subject != "Quick idea for {{.Company}}" || !a.HTML {
		t.Fatalf("subject = %q", a.Subject)
	}
	for _, want := range []string{`Hi {{.FirstName | default "there"}},`, `href="https://woodpecker.co"`, "{{.snippet_3}}", "{{unsubscribe_link}}"} {
		if !strings.Contains(a.Body, want) {
			t.Errorf("body %q lacks %q", a.Body, want)
		}
	}
	if b := first.Variants[1].Body; !strings.Contains(b, "{Hi|Hello|Good morning} {{.FirstName}}") {
		t.Errorf("spintax body = %q", b)
	}
	// Four days after the first email, plus one day for the skipped LinkedIn request.
	if second.WaitDays != 5 || second.WaitMinutes != 0 || second.NewThread || second.Variants[0].Subject != "" {
		t.Fatalf("second step = %+v", second)
	}
	sch := c.Schedule
	if sch == nil || sch.Timezone != "Europe/Warsaw" || sch.From != "09:00" || sch.To != "18:00" || len(sch.Days) != 4 || sch.Days[0] != 1 || sch.Days[3] != 4 {
		t.Fatalf("schedule = %+v", sch)
	}
	notes := strings.Join(c.UnmappedNotes, "\n")
	for _, want := range []string{"Step 1, a LinkedIn visit profile step", "Step 3, a LinkedIn connection request step", "own sending hours", "prospect's own timezone"} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes lack %q:\n%s", want, notes)
		}
	}
	if c.MaxNewLeadsPerDay != 20 {
		t.Errorf("MaxNewLeadsPerDay = %d, want 10 for each of 2 mailboxes", c.MaxNewLeadsPerDay)
	}
	if u := camps[1]; len(u.Steps) != 0 || len(u.UnmappedNotes) != 1 || !strings.Contains(u.UnmappedNotes[0], "IF conditions") || u.Senders[0] != "erlich@usebachmanity.com" {
		t.Fatalf("unsupported campaign = %+v", u)
	}
}

func TestLeads(t *testing.T) {
	defer func(n int) { leadPageSize = n }(leadPageSize)
	leadPageSize = 2
	s, _ := open(t, &fake{t: t})
	ws := outreachsource.Workspace{}
	page, next, err := s.Leads(context.Background(), ws, "12345679", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 2 || next != "2" {
		t.Fatalf("page 1 = %d leads, next %q", len(page), next)
	}
	e := page[0]
	if e.Status != outreachsource.LeadInterested || e.StepsSent != 1 || e.LastSent == nil || e.Location != "Palo Alto, California, United States" || e.Title != "CEO" || e.LinkedIn == "" {
		t.Fatalf("lead = %+v", e)
	}
	if !e.StepsExact || e.Custom["snippet_3"] != "You run an incubator" || e.Custom["custom_snippet"] != "You run an incubator" || e.Custom["industry"] != "IT" || e.Custom["title"] != "" {
		t.Fatalf("custom = %v", e.Custom)
	}
	if j := page[1]; j.Status != outreachsource.LeadInProgress || j.StepsSent != 2 || j.Custom != nil {
		t.Fatalf("active lead = %+v", j)
	}
	page, next, err = s.Leads(context.Background(), ws, "12345679", next)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 1 || next != "" || page[0].Status != outreachsource.LeadBounced || page[0].StepsSent != 0 || page[0].LastSent != nil {
		t.Fatalf("page 2 = %+v next %q", page, next)
	}
	// An empty answer is an object, not an array.
	page, next, err = s.Leads(context.Background(), ws, "", "")
	if err != nil || len(page) != 0 || next != "" {
		t.Fatalf("no prospects = %v %q %v", page, next, err)
	}
}

func TestBlocklist(t *testing.T) {
	defer func(n int) { blockPageSize = n }(blockPageSize)
	blockPageSize = 2
	s, _ := open(t, &fake{t: t, agency: true})
	var got []outreachsource.BlockEntry
	cursor := ""
	for i := 0; ; i++ {
		page, next, err := s.Blocklist(context.Background(), outreachsource.Workspace{ID: "12345678"}, cursor)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, page...)
		if next == "" {
			break
		}
		if i > 10 {
			t.Fatal("blocklist did not end")
		}
		cursor = next
	}
	want := []string{"andrew@notmyicp.design", "drew@nomoreemails.co", "john@finisheddeal.co.uk", "baddomain.com", "wrong@baddomain.com"}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i, w := range want {
		if got[i].Value != w {
			t.Errorf("entry %d = %q, want %q", i, got[i].Value, w)
		}
	}
	if !got[3].Domain || got[0].Domain || got[4].Reason != "Woodpecker agency blacklist" {
		t.Fatalf("entries = %+v", got)
	}
}

func TestInventory(t *testing.T) {
	s, _ := open(t, &fake{t: t})
	inv, err := s.Inventory(context.Background(), outreachsource.Workspace{})
	if err != nil {
		t.Fatal(err)
	}
	if inv.Mailboxes != 3 || inv.Campaigns != 2 || inv.Leads != 868*2 || inv.Blocklist != 5 || len(inv.Summaries) != 2 {
		t.Fatalf("inventory = %+v", inv)
	}
	if sm := inv.Summaries[0]; sm.Steps != 2 || sm.Status != outreachsource.CampaignPaused {
		t.Fatalf("summary = %+v", sm)
	}
}
