package quickmail

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

const testKey = "qm-test-key"

type request struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

type fake struct {
	t      *testing.T
	mu     sync.Mutex
	limit  int // 429s still to send
	reqs   []request
	status int
}

func newFake(t *testing.T) (*fake, *httptest.Server) {
	f := &fake{t: t}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fake) write(w http.ResponseWriter, code int, file string) {
	data, err := os.ReadFile(filepath.Join("testdata", file))
	if err != nil {
		f.t.Fatalf("fixture %s: %v", file, err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(data)
}

func (f *fake) last(op string) request {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.reqs) - 1; i >= 0; i-- {
		if strings.Contains(f.reqs[i].Query, "query "+op) {
			return f.reqs[i]
		}
	}
	f.t.Fatalf("no %s query sent", op)
	return request{}
}

func (f *fake) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/v2/graphql" {
		f.t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
	}
	if r.Header.Get("Authorization") != testKey {
		f.write(w, http.StatusUnauthorized, "unauthorized.json")
		return
	}
	var req request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		f.t.Fatalf("body: %v", err)
	}
	if strings.Contains(req.Query, "mutation") || !strings.HasPrefix(strings.TrimSpace(req.Query), "query ") {
		f.t.Fatalf("not a read: %s", req.Query)
	}
	f.mu.Lock()
	f.reqs = append(f.reqs, req)
	limited := f.limit > 0
	if limited {
		f.limit--
	}
	f.mu.Unlock()
	if limited {
		w.Header().Set("Retry-After", "2")
		f.write(w, http.StatusTooManyRequests, "rate_limited.json")
		return
	}
	after, _ := req.Variables["after"].(string)
	id, _ := req.Variables["id"].(string)
	switch q := req.Query; {
	case strings.Contains(q, "query Agency"):
		f.write(w, 200, "agency.json")
	case strings.Contains(q, "query Workspaces"):
		f.write(w, 200, "workspaces.json")
	case strings.Contains(q, "query EmailAccounts"):
		if after == "Mg" {
			f.write(w, 200, "email_accounts_page2.json")
			return
		}
		f.write(w, 200, "email_accounts_page1.json")
	case strings.Contains(q, "query Campaigns"):
		f.write(w, 200, "campaigns.json")
	case strings.Contains(q, "query Steps"):
		switch {
		case id == "cmp_1" && after == "":
			f.write(w, 200, "steps_cmp_1_page1.json")
		case id == "cmp_1" && after == "Mg":
			f.write(w, 200, "steps_cmp_1_page2.json")
		case id == "cmp_2":
			f.write(w, 200, "steps_cmp_2.json")
		default:
			f.write(w, 200, "not_found.json")
		}
	case strings.Contains(q, "query Leads"):
		switch {
		case req.Variables["max"] != nil:
			f.write(w, 200, "leads_none.json")
		case after == "Mg":
			f.write(w, 200, "leads_page2.json")
		default:
			f.write(w, 200, "leads_page1.json")
		}
	default:
		f.t.Errorf("unexpected query %s", q)
	}
}

func open(t *testing.T, srv *httptest.Server) outreachsource.Session {
	t.Helper()
	p := newProvider(srv.URL + "/v2/graphql")
	p.sleep = func(context.Context, time.Duration) error { return nil }
	s, err := p.Open(context.Background(), outreachsource.Credentials{"api_key": testKey})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return s
}

var ws1 = outreachsource.Workspace{ID: "wrk_1", Name: "Client One"}

func TestInfo(t *testing.T) {
	info := New().Info()
	if info.ID != "quickmail" || info.Label != "QuickMail" || !info.Workspaces || info.MultiKey || info.MailboxSecrets {
		t.Fatalf("info = %+v", info)
	}
	for _, s := range append([]string{info.KeyHelp, info.Fields[0].Help}, info.Notes...) {
		if strings.Contains(s, "—") {
			t.Errorf("em dash in %q", s)
		}
	}
}

func TestOpen(t *testing.T) {
	_, srv := newFake(t)
	s := open(t, srv)
	if a, _ := s.Account(context.Background()); a.Name != "Acme Agency" {
		t.Errorf("account = %+v", a)
	}
	p := newProvider(srv.URL + "/v2/graphql")
	_, err := p.Open(context.Background(), outreachsource.Credentials{"api_key": "nope"})
	if !errors.Is(err, outreachsource.ErrKey) || strings.Contains(err.Error(), "nope") {
		t.Fatalf("bad key err = %v", err)
	}
	if _, err := p.Open(context.Background(), outreachsource.Credentials{"api_key": " "}); !errors.Is(err, outreachsource.ErrKey) {
		t.Errorf("empty key err = %v", err)
	}
}

func TestWorkspaces(t *testing.T) {
	_, srv := newFake(t)
	got, err := open(t, srv).Workspaces(context.Background())
	want := []outreachsource.Workspace{{ID: "wrk_1", Name: "Client One"}, {ID: "wrk_2", Name: "Client Two"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("workspaces = %+v, %v", got, err)
	}
}

func TestInventory(t *testing.T) {
	f, srv := newFake(t)
	inv, err := open(t, srv).Inventory(context.Background(), ws1)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Mailboxes != 3 || inv.Campaigns != 2 || inv.Leads != -1 || inv.Blocklist != -1 {
		t.Errorf("inventory = %+v", inv)
	}
	want := []outreachsource.CampaignSummary{
		{ID: "cmp_1", Name: "Q3 Founders", Status: outreachsource.CampaignActive, Steps: 2, Leads: 120},
		{ID: "cmp_2", Name: "Old list", Status: outreachsource.CampaignPaused, Steps: 0, Leads: 0},
	}
	if !reflect.DeepEqual(inv.Summaries, want) {
		t.Errorf("summaries = %+v", inv.Summaries)
	}
	if req := f.last("Campaigns"); req.Variables["id"] != "wrk_1" || !strings.Contains(req.Query, "workspace(id: $id)") {
		t.Errorf("campaigns query = %+v", req)
	}
}

func TestMailboxes(t *testing.T) {
	_, srv := newFake(t)
	got, err := open(t, srv).Mailboxes(context.Background(), ws1)
	if err != nil || len(got) != 3 {
		t.Fatalf("mailboxes = %+v, %v", got, err)
	}
	want := []struct {
		email string
		kind  outreachsource.MailboxKind
		cap   int
		name  string
	}{
		{"jane@acme.io", outreachsource.MailboxGoogle, 40, "Jane Smith"},
		{"sam@acme.io", outreachsource.MailboxMicrosoft, 0, "Sam Lee"},
		{"ops@acme-mail.com", outreachsource.MailboxSMTP, 0, "Ops"},
	}
	for i, w := range want {
		m := got[i]
		if m.Email != w.email || m.Kind != w.kind || m.DailyCap != w.cap || m.FromName != w.name || m.HasSecrets() || m.SMTP != nil {
			t.Errorf("mailbox %d = %+v", i, m)
		}
	}
}

func TestCampaigns(t *testing.T) {
	_, srv := newFake(t)
	got, err := open(t, srv).Campaigns(context.Background(), ws1)
	if err != nil || len(got) != 2 {
		t.Fatalf("campaigns = %+v, %v", got, err)
	}
	c := got[0]
	if c.ID != "cmp_1" || c.Status != outreachsource.CampaignActive || c.LeadCount != 120 || !c.StopOnReply || c.CreatedAt == nil {
		t.Errorf("campaign = %+v", c)
	}
	want := []outreachsource.Step{
		{WaitDays: 1, Variants: []outreachsource.Variant{
			{
				Subject: `Quick question, {{.FirstName | default "there"}}`,
				Body:    `<p>{Hi|Hello} {{.FirstName}} at {{.Company}}.</p><p>{{.opening_line}} {{.title}}</p><p>{{.Sender.FirstName}}</p><a href="{{unsubscribe_link}}">unsubscribe</a>`,
				HTML:    true,
			},
			{Subject: `Idea for {{.Company}}`, Body: `Hey {{.FirstName}}, happy .`},
		}},
		{WaitDays: 1, WaitMinutes: 360, Variants: []outreachsource.Variant{{Body: `Following up, {{.FirstName}}.`}}},
	}
	if !reflect.DeepEqual(c.Steps, want) {
		gotJSON, _ := json.MarshalIndent(c.Steps, "", "  ")
		t.Errorf("steps = %s", gotJSON)
	}
	notes := strings.Join(c.UnmappedNotes, "\n")
	for _, n := range []string{"business days", "day of the week", "Liquid filters", "signature", "Step 3 (LinkedIn message)"} {
		if !strings.Contains(notes, n) {
			t.Errorf("notes missing %q: %v", n, c.UnmappedNotes)
		}
	}

	old := got[1]
	if old.Status != outreachsource.CampaignPaused || len(old.Steps) != 1 || !strings.Contains(strings.Join(old.UnmappedNotes, " "), "Liquid conditions") {
		t.Errorf("paused campaign = %+v", old)
	}
}

func TestTranslate(t *testing.T) {
	cases := map[string]string{
		`Hi {{ lead.first_name | default: 'friend' }}`:          `Hi {{.FirstName | default "friend"}}`,
		`{{prospect.first_name}} {{prospect.custom.Job_Title}}`: `{{.FirstName}} {{.job_title}}`,
		`{{inbox.name}} / {{inbox.email}}`:                      `{{.Sender.Name}} / {{.Sender.Email}}`,
		`((Hi|Hey)) there`:                                      `{Hi|Hey} there`,
		`{{company.domain}}`:                                    `{{.company_domain}}`,
	}
	for in, want := range cases {
		if got := translate(in).text; got != want {
			t.Errorf("translate(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLeads(t *testing.T) {
	f, srv := newFake(t)
	s := open(t, srv)
	ctx := context.Background()

	page1, next, err := s.Leads(ctx, ws1, "cmp_1", "")
	if err != nil || next != "Mg" || len(page1) != 2 {
		t.Fatalf("page1 = %+v, %q, %v", page1, next, err)
	}
	req := f.last("Leads")
	if inc, _ := req.Variables["include"].([]any); len(inc) != 1 || inc[0] != "cmp_1" || req.Variables["max"] != nil || req.Variables["id"] != "wrk_1" {
		t.Errorf("leads vars = %+v", req.Variables)
	}
	pat := page1[0]
	if pat.Email != "pat@example.com" || pat.FirstName != "Pat" || pat.Title != "Head of Growth" || pat.Phone != "+1 555 0100" ||
		pat.LinkedIn != "https://www.linkedin.com/in/patlee" || pat.Status != outreachsource.LeadInProgress || pat.StepsExact || pat.StepsSent != 0 {
		t.Errorf("pat = %+v", pat)
	}
	wantCustom := map[string]string{"role": "Marketing", "language": "en", "score": "7", "tags": "2026, vip", "opening_line": "Loved your talk"}
	if !reflect.DeepEqual(pat.Custom, wantCustom) {
		t.Errorf("custom = %v", pat.Custom)
	}
	if page1[1].Status != outreachsource.LeadUnsubscribed || page1[1].Custom != nil {
		t.Errorf("dnc = %+v", page1[1])
	}

	page2, next, err := s.Leads(ctx, ws1, "cmp_1", next)
	if err != nil || next != "" || len(page2) != 1 || page2[0].Custom["company_size"] != "50-200" {
		t.Fatalf("page2 = %+v, %q, %v", page2, next, err)
	}

	loose, next, err := s.Leads(ctx, ws1, "", "")
	if err != nil || next != "" || len(loose) != 1 || loose[0].Status != outreachsource.LeadNotStarted {
		t.Fatalf("campaignless = %+v, %v", loose, err)
	}
	if req := f.last("Leads"); req.Variables["max"] != float64(0) || req.Variables["include"] != nil {
		t.Errorf("campaignless vars = %+v", req.Variables)
	}
}

func TestBlocklist(t *testing.T) {
	_, srv := newFake(t)
	s := open(t, srv)
	got, next, err := s.Blocklist(context.Background(), ws1, "")
	want := []outreachsource.BlockEntry{{Value: "dnc@example.com", Reason: "Do Not Contact in QuickMail"}}
	if err != nil || next != "Mg" || !reflect.DeepEqual(got, want) {
		t.Fatalf("blocklist = %+v, %q, %v", got, next, err)
	}
	got, next, err = s.Blocklist(context.Background(), ws1, next)
	if err != nil || next != "" || len(got) != 0 {
		t.Fatalf("blocklist page2 = %+v, %q, %v", got, next, err)
	}
}

func TestGraphQLError(t *testing.T) {
	_, srv := newFake(t)
	s := open(t, srv).(*session)
	_, err := s.steps(context.Background(), "cmp_missing")
	var ae *outreachsource.APIError
	if !errors.As(err, &ae) || !strings.Contains(err.Error(), "Campaign not found") {
		t.Fatalf("err = %v", err)
	}
}

func TestRateLimitRetried(t *testing.T) {
	f, srv := newFake(t)
	p := newProvider(srv.URL + "/v2/graphql")
	var waits []time.Duration
	p.sleep = func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		return nil
	}
	s, err := p.Open(context.Background(), outreachsource.Credentials{"api_key": testKey})
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.limit = 1
	f.mu.Unlock()
	got, err := s.Workspaces(context.Background())
	if err != nil || len(got) != 2 || len(waits) != 1 || waits[0] != 2*time.Second {
		t.Fatalf("workspaces = %+v, %v, waits %v", got, err, waits)
	}
	f.mu.Lock()
	f.limit = 10
	f.mu.Unlock()
	if _, err := s.Workspaces(context.Background()); !errors.Is(err, outreachsource.ErrRateLimited) {
		t.Errorf("exhausted err = %v", err)
	}
}
