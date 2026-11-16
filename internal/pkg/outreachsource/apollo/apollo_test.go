package apollo

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

const testKey = "test-key"

type fake struct {
	t *testing.T
	// status overrides a path's answer, consumed once per entry.
	mu     sync.Mutex
	status map[string][]int
	calls  map[string]int
}

func newFake(t *testing.T) (*fake, *httptest.Server) {
	f := &fake{t: t, status: map[string][]int{}, calls: map[string]int{}}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fake) fail(path string, codes ...int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status[path] = append(f.status[path], codes...)
}

func (f *fake) count(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[path]
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

func (f *fake) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.calls[r.URL.Path]++
	var code int
	if q := f.status[r.URL.Path]; len(q) > 0 {
		code, f.status[r.URL.Path] = q[0], q[1:]
	}
	f.mu.Unlock()
	if r.Header.Get("x-api-key") != testKey {
		f.write(w, http.StatusUnauthorized, "unauthorized.json")
		return
	}
	switch code {
	case 0:
	case http.StatusTooManyRequests:
		w.Header().Set("Retry-After", "3")
		f.write(w, code, "rate_limited.json")
		return
	default:
		w.WriteHeader(code)
		_, _ = w.Write([]byte(`{"error":"no"}`))
		return
	}
	route := r.Method + " " + r.URL.Path
	switch route {
	case "GET /users/api_profile":
		f.write(w, 200, "profile.json")
	case "GET /email_accounts":
		f.write(w, 200, "email_accounts.json")
	case "POST /emailer_campaigns/search":
		if r.URL.Query().Get("page") != "1" {
			f.t.Errorf("sequence page = %q", r.URL.Query().Get("page"))
		}
		f.write(w, 200, "sequences.json")
	case "GET /emailer_schedules":
		f.write(w, 200, "schedules.json")
	case "GET /emailer_messages/search":
		if got := r.URL.Query()["emailer_campaign_ids[]"]; len(got) != 1 {
			f.t.Errorf("messages filter = %v", got)
		}
		if r.URL.Query().Get("emailer_campaign_ids[]") == "66e9e215ece19801b219997f" {
			f.write(w, 200, "messages.json")
			return
		}
		_, _ = w.Write([]byte(`{"emailer_messages":[],"emailer_steps":[]}`))
	case "GET /typed_custom_fields":
		f.write(w, 200, "custom_fields.json")
	case "POST /contacts/search":
		var body struct {
			Page    int `json:"page"`
			PerPage int `json:"per_page"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			f.t.Errorf("contacts body: %v", err)
		}
		if body.PerPage == 1 {
			f.write(w, 200, "contacts_page1.json")
			return
		}
		if body.Page == 2 {
			f.write(w, 200, "contacts_page2.json")
			return
		}
		f.write(w, 200, "contacts_page1.json")
	default:
		f.t.Errorf("unexpected %s", route)
		w.WriteHeader(http.StatusNotFound)
	}
}

func open(t *testing.T, srv *httptest.Server) outreachsource.Session {
	t.Helper()
	p := newProvider(srv.URL)
	p.sleep = func(context.Context, time.Duration) error { return nil }
	s, err := p.Open(context.Background(), outreachsource.Credentials{"api_key": " " + testKey + " "})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return s
}

func TestInfo(t *testing.T) {
	info := New().Info()
	if info.ID != "apollo" || info.Label != "Apollo" || info.MailboxSecrets || info.Workspaces {
		t.Fatalf("info = %+v", info)
	}
	if info.Supports(outreachsource.KindBlocklist) || !info.Supports(outreachsource.KindCampaigns) {
		t.Errorf("kinds = %v", info.Kinds)
	}
	for _, s := range append([]string{info.KeyHelp}, info.Notes...) {
		if strings.Contains(s, "—") {
			t.Errorf("em dash in %q", s)
		}
	}
}

func TestOpen(t *testing.T) {
	_, srv := newFake(t)
	s := open(t, srv)
	acct, err := s.Account(context.Background())
	if err != nil || acct.Name != "John Doe" || acct.Email != "john.doe@example.com" {
		t.Fatalf("account = %+v, %v", acct, err)
	}

	p := newProvider(srv.URL)
	_, err = p.Open(context.Background(), outreachsource.Credentials{"api_key": "wrong"})
	if !errors.Is(err, outreachsource.ErrKey) {
		t.Fatalf("bad key err = %v", err)
	}
	if strings.Contains(err.Error(), "wrong") {
		t.Errorf("error leaks the key: %v", err)
	}
	if _, err := p.Open(context.Background(), outreachsource.Credentials{}); !errors.Is(err, outreachsource.ErrKey) {
		t.Errorf("empty key err = %v", err)
	}
}

func TestWorkspacesAndInventory(t *testing.T) {
	_, srv := newFake(t)
	s := open(t, srv)
	ctx := context.Background()
	ws, err := s.Workspaces(ctx)
	if err != nil || len(ws) != 1 || ws[0].ID != "" || ws[0].Name != "John Doe" {
		t.Fatalf("workspaces = %+v, %v", ws, err)
	}
	inv, err := s.Inventory(ctx, ws[0])
	if err != nil {
		t.Fatal(err)
	}
	if inv.Mailboxes != 2 || inv.Campaigns != 2 || inv.Leads != 7 || inv.Blocklist != -1 {
		t.Errorf("inventory = %+v", inv)
	}
	want := []outreachsource.CampaignSummary{
		{ID: "66e9e215ece19801b219997f", Name: "Target Copywriting Clients in Dublin", Status: outreachsource.CampaignActive, Steps: 3, Leads: -1},
		{ID: "684b215edf80730019db612c", Name: "Old Outreach", Status: outreachsource.CampaignArchived, Steps: 0, Leads: -1},
	}
	if !reflect.DeepEqual(inv.Summaries, want) {
		t.Errorf("summaries = %+v", inv.Summaries)
	}
}

func TestMailboxes(t *testing.T) {
	_, srv := newFake(t)
	s := open(t, srv)
	got, err := s.Mailboxes(context.Background(), outreachsource.Workspace{})
	if err != nil || len(got) != 2 {
		t.Fatalf("mailboxes = %+v, %v", got, err)
	}
	g, m := got[0], got[1]
	if g.Email != "test.liam@apollomail.io" || g.Kind != outreachsource.MailboxGoogle || g.DailyCap != 50 || g.Signature != "<div>Liam Donovan</div>" {
		t.Errorf("google = %+v", g)
	}
	if g.Warmup == nil || *g.Warmup {
		t.Errorf("google warmup = %v", g.Warmup)
	}
	if m.Kind != outreachsource.MailboxMicrosoft || m.DailyCap != 0 || m.Warmup == nil || !*m.Warmup {
		t.Errorf("microsoft = %+v", m)
	}
	for _, mb := range got {
		if mb.HasSecrets() || mb.SMTP != nil {
			t.Errorf("%s carries secrets", mb.Email)
		}
	}
	if mailboxKind("smtp") != outreachsource.MailboxSMTP {
		t.Error("unknown type is not SMTP")
	}
}

func TestCampaigns(t *testing.T) {
	_, srv := newFake(t)
	s := open(t, srv)
	got, err := s.Campaigns(context.Background(), outreachsource.Workspace{})
	if err != nil || len(got) != 2 {
		t.Fatalf("campaigns = %+v, %v", got, err)
	}
	c := got[0]
	if c.Status != outreachsource.CampaignActive || c.DailyLimit != 120 || !c.StopOnReply || !c.TrackOpens || !c.TrackClicks {
		t.Errorf("settings = %+v", c)
	}
	if c.Steps == nil || len(c.Steps) != 0 {
		t.Errorf("steps = %+v", c.Steps)
	}
	if !reflect.DeepEqual(c.Senders, []string{"test.dana@apollomail.io", "test.liam@apollomail.io"}) {
		t.Errorf("senders = %v", c.Senders)
	}
	if c.CreatedAt == nil || c.CreatedAt.Year() != 2024 {
		t.Errorf("created = %v", c.CreatedAt)
	}
	sc := c.Schedule
	if sc == nil || sc.Timezone != "America/Los_Angeles" || !reflect.DeepEqual(sc.Days, []int{1, 2, 3, 4, 5}) || sc.From != "08:00" || sc.To != "17:00" {
		t.Fatalf("schedule = %+v", sc)
	}
	notes := strings.Join(c.UnmappedNotes, "\n")
	for _, want := range []string{"3 steps", "vary across the week", "own time zone"} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes missing %q: %v", want, c.UnmappedNotes)
		}
	}

	old := got[1]
	if old.Status != outreachsource.CampaignArchived || old.Schedule != nil || old.DailyLimit != 0 || old.StopOnReply || len(old.Senders) != 0 || len(old.UnmappedNotes) != 0 {
		t.Errorf("archived = %+v", old)
	}
}

func TestCampaignsScopedKey(t *testing.T) {
	f, srv := newFake(t)
	s := open(t, srv)
	f.fail("/emailer_messages/search", http.StatusForbidden, http.StatusForbidden)
	f.fail("/emailer_schedules", http.StatusForbidden)
	got, err := s.Campaigns(context.Background(), outreachsource.Workspace{})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Schedule != nil || len(got[0].Senders) != 0 {
		t.Errorf("campaign = %+v", got[0])
	}
}

func TestLeads(t *testing.T) {
	_, srv := newFake(t)
	s := open(t, srv)
	ctx := context.Background()
	const seq = "66e9e215ece19801b219997f"

	page1, next, err := s.Leads(ctx, outreachsource.Workspace{}, seq, "")
	if err != nil || next != "2" || len(page1) != 2 {
		t.Fatalf("page1 = %+v, %q, %v", page1, next, err)
	}
	walt := page1[0]
	if walt.Email != "wwhitman@apollo.io" || walt.Company != "Apollo.io" || walt.Website != "http://www.apollo.io" ||
		walt.Phone != "+12025550114" || walt.Location != "Dublin, Ireland" || walt.Timezone != "Europe/Dublin" || walt.Title != "Associate Writer" {
		t.Errorf("walt = %+v", walt)
	}
	if walt.Status != outreachsource.LeadReplied || walt.StepsSent != 2 {
		t.Errorf("walt status = %s/%d", walt.Status, walt.StepsSent)
	}
	wantCustom := map[string]string{
		"company_domain":           "apollo.io",
		"new_product_launch_date":  "2025-01-15T00:00:00.000+00:00",
		"customer_age":             "1 year",
		"ffffffffffffffffffffffff": "42",
	}
	if !reflect.DeepEqual(walt.Custom, wantCustom) {
		t.Errorf("custom = %v", walt.Custom)
	}
	ada := page1[1]
	if ada.Status != outreachsource.LeadInProgress || ada.StepsSent != 2 || ada.Company != "Analytical Engines" || ada.Phone != "+442079460000" || ada.Location != "London, United Kingdom" {
		t.Errorf("ada = %+v", ada)
	}

	page2, next, err := s.Leads(ctx, outreachsource.Workspace{}, seq, next)
	if err != nil || next != "" || len(page2) != 2 {
		t.Fatalf("page2 = %+v, %q, %v", page2, next, err)
	}
	if page2[0].Status != outreachsource.LeadCompleted || page2[0].StepsSent != 3 {
		t.Errorf("grace = %+v", page2[0])
	}
	if page2[1].Status != outreachsource.LeadNotStarted || page2[1].StepsSent != 0 {
		t.Errorf("alan = %+v", page2[1])
	}

	loose, _, err := s.Leads(ctx, outreachsource.Workspace{}, "", "")
	if err != nil || len(loose) != 1 || loose[0].Email != "optout@example.com" || loose[0].Status != outreachsource.LeadUnsubscribed {
		t.Errorf("campaignless = %+v, %v", loose, err)
	}
	if _, _, err := s.Leads(ctx, outreachsource.Workspace{}, seq, "x"); err == nil {
		t.Error("bad cursor accepted")
	}
}

func TestLeadStatus(t *testing.T) {
	pos := func(n int) *int { return &n }
	cases := []struct {
		st    campaignStatus
		want  outreachsource.LeadStatus
		steps int
	}{
		{campaignStatus{Status: "active"}, outreachsource.LeadNotStarted, 0},
		{campaignStatus{Status: "active", StepPosition: pos(1)}, outreachsource.LeadNotStarted, 0},
		{campaignStatus{Status: "active", StepPosition: pos(4)}, outreachsource.LeadInProgress, 3},
		{campaignStatus{Status: "paused", InactiveReason: "Sequence inactive", StepPosition: pos(2)}, outreachsource.LeadPaused, 1},
		{campaignStatus{Status: "failed", InactiveReason: "User deleted", StepPosition: pos(2)}, outreachsource.LeadPaused, 1},
		{campaignStatus{Status: "finished", InactiveReason: "replied", StepPosition: pos(2)}, outreachsource.LeadReplied, 2},
		{campaignStatus{Status: "finished", InactiveReason: "Completed last step", StepPosition: pos(3)}, outreachsource.LeadCompleted, 3},
		{campaignStatus{Status: "finished", InactiveReason: "manually finished", StepPosition: pos(1)}, outreachsource.LeadCompleted, 1},
		{campaignStatus{Status: "finished", InactiveReason: "Email bounced", StepPosition: pos(1)}, outreachsource.LeadBounced, 1},
		{campaignStatus{Status: "finished", InactiveReason: "Contact unsubscribed", StepPosition: pos(2)}, outreachsource.LeadUnsubscribed, 2},
		{campaignStatus{Status: "finished", InactiveReason: "Marked as interested", StepPosition: pos(2)}, outreachsource.LeadInterested, 2},
	}
	for _, c := range cases {
		got, steps := leadStatus(c.st)
		if got != c.want || steps != c.steps {
			t.Errorf("%+v = %s/%d, want %s/%d", c.st, got, steps, c.want, c.steps)
		}
	}
}

func TestBlocklistUnsupported(t *testing.T) {
	_, srv := newFake(t)
	s := open(t, srv)
	if _, _, err := s.Blocklist(context.Background(), outreachsource.Workspace{}, ""); !errors.Is(err, outreachsource.ErrUnsupported) {
		t.Errorf("blocklist err = %v", err)
	}
}

func TestRateLimitRetried(t *testing.T) {
	f, srv := newFake(t)
	p := newProvider(srv.URL)
	var waits []time.Duration
	p.sleep = func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		return nil
	}
	s, err := p.Open(context.Background(), outreachsource.Credentials{"api_key": testKey})
	if err != nil {
		t.Fatal(err)
	}
	f.fail("/emailer_campaigns/search", http.StatusTooManyRequests)
	inv, err := s.Inventory(context.Background(), outreachsource.Workspace{})
	if err != nil || inv.Campaigns != 2 {
		t.Fatalf("inventory = %+v, %v", inv, err)
	}
	if f.count("/emailer_campaigns/search") != 2 || len(waits) != 1 || waits[0] != 3*time.Second {
		t.Errorf("calls = %d, waits = %v", f.count("/emailer_campaigns/search"), waits)
	}

	f.fail("/email_accounts", 429, 429, 429, 429, 429)
	if _, err := s.Mailboxes(context.Background(), outreachsource.Workspace{}); !errors.Is(err, outreachsource.ErrRateLimited) {
		t.Errorf("exhausted err = %v", err)
	}
}
