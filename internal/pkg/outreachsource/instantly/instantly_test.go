package instantly

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

const (
	goodKey   = "good-key"
	campaignA = "01956fbd-0eb1-72db-a565-82977a586084"
	subWS     = "01a1d05e-1111-7000-8000-00000000c11e"
)

func TestMain(m *testing.M) {
	pageSize = 2
	os.Exit(m.Run())
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type fake struct {
	t  *testing.T
	mu sync.Mutex
	// asWorkspace records the x-as-workspace header per path.
	asWorkspace map[string]string
	leadBodies  []map[string]any
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	t := f.t
	w.Header().Set("Content-Type", "application/json")
	if r.Header.Get("Authorization") != "Bearer "+goodKey {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write(fixture(t, "unauthorized.json"))
		return
	}
	f.mu.Lock()
	f.asWorkspace[r.URL.Path] = r.Header.Get(asWorkspace)
	f.mu.Unlock()
	if r.Method != http.MethodGet && !(r.Method == http.MethodPost && r.URL.Path == "/leads/list") {
		t.Errorf("non-read request %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query()
	if r.Method == http.MethodGet && q.Get("starting_after") != "" {
		_, _ = w.Write([]byte(`{"items":[]}`))
		return
	}
	switch r.URL.Path {
	case "/workspaces/current":
		_, _ = w.Write(fixture(t, "workspace.json"))
	case "/workspace-group-members":
		_, _ = w.Write(fixture(t, "group_members.json"))
	case "/accounts":
		if q.Get("tag_ids") != "" {
			_, _ = w.Write(fixture(t, "accounts_tagged.json"))
			return
		}
		_, _ = w.Write(fixture(t, "accounts.json"))
	case "/campaigns":
		_, _ = w.Write(fixture(t, "campaigns.json"))
	case "/campaigns/analytics":
		_, _ = w.Write(fixture(t, "analytics.json"))
	case "/campaigns/" + campaignA:
		var list struct{ Items []json.RawMessage }
		_ = json.Unmarshal(fixture(t, "campaigns.json"), &list)
		_, _ = w.Write(list.Items[0])
	case "/leads/list":
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.leadBodies = append(f.leadBodies, body)
		f.mu.Unlock()
		switch {
		case body["campaign"] == "status":
			_, _ = w.Write(fixture(t, "leads_status.json"))
		case body["starting_after"] == nil:
			_, _ = w.Write(fixture(t, "leads_page1.json"))
		default:
			_, _ = w.Write(fixture(t, "leads_page2.json"))
		}
	case "/block-lists-entries":
		_, _ = w.Write(fixture(t, "blocklist.json"))
	default:
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}
}

func open(t *testing.T) (*session, *fake) {
	t.Helper()
	f := &fake{t: t, asWorkspace: map[string]string{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	p := newProvider(srv.URL)
	p.sleep = func(context.Context, time.Duration) error { return nil }
	s, err := p.Open(context.Background(), outreachsource.Credentials{"api_key": goodKey})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s.(*session), f
}

func TestOpenRejectsBadKey(t *testing.T) {
	srv := httptest.NewServer(&fake{t: t, asWorkspace: map[string]string{}})
	defer srv.Close()
	_, err := newProvider(srv.URL).Open(context.Background(), outreachsource.Credentials{"api_key": "nope"})
	if !errors.Is(err, outreachsource.ErrKey) {
		t.Fatalf("err = %v, want ErrKey", err)
	}
	if strings.Contains(err.Error(), "nope") {
		t.Fatalf("error leaks the key: %v", err)
	}
	if _, err := newProvider(srv.URL).Open(context.Background(), outreachsource.Credentials{}); !errors.Is(err, outreachsource.ErrKey) {
		t.Fatalf("empty key err = %v", err)
	}
}

func TestAccountAndWorkspaces(t *testing.T) {
	s, _ := open(t)
	a, _ := s.Account(context.Background())
	if a.Name != "Acme Agency" || a.Plan != "pid_hg_v1" {
		t.Errorf("account = %+v", a)
	}
	ws, err := s.Workspaces(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []outreachsource.Workspace{{ID: "", Name: "Acme Agency"}, {ID: subWS, Name: "Client One"}}
	if !reflect.DeepEqual(ws, want) {
		t.Errorf("workspaces = %+v, want %+v", ws, want)
	}
}

func TestWorkspacesWithoutGroup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/workspaces/current" {
			_, _ = w.Write(fixture(t, "workspace.json"))
			return
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	s, err := newProvider(srv.URL).Open(context.Background(), outreachsource.Credentials{"api_key": goodKey})
	if err != nil {
		t.Fatal(err)
	}
	ws, err := s.Workspaces(context.Background())
	if err != nil || len(ws) != 1 || ws[0].ID != "" {
		t.Fatalf("workspaces = %+v, %v", ws, err)
	}
}

func TestSubWorkspaceReadsAsIt(t *testing.T) {
	s, f := open(t)
	if _, err := s.Mailboxes(context.Background(), outreachsource.Workspace{ID: subWS}); err != nil {
		t.Fatal(err)
	}
	if got := f.asWorkspace["/accounts"]; got != subWS {
		t.Errorf("x-as-workspace = %q", got)
	}
	if _, err := s.Mailboxes(context.Background(), outreachsource.Workspace{}); err != nil {
		t.Fatal(err)
	}
	if got := f.asWorkspace["/accounts"]; got != "" {
		t.Errorf("own workspace sent x-as-workspace %q", got)
	}
}

func TestCampaigns(t *testing.T) {
	s, _ := open(t)
	cs, err := s.Campaigns(context.Background(), outreachsource.Workspace{})
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 2 {
		t.Fatalf("campaigns = %d", len(cs))
	}
	c := cs[0]
	if c.Status != outreachsource.CampaignActive || cs[1].Status != outreachsource.CampaignDraft {
		t.Errorf("status = %s, %s", c.Status, cs[1].Status)
	}
	if !c.StopOnReply || c.TrackOpens || !c.TrackClicks || c.PlainText || !c.Unsubscribe || c.DailyLimit != 120 || c.LeadCount != 150 {
		t.Errorf("settings = %+v", c)
	}
	if want := []string{"jane@acme.io", "sam@acme-mail.com", "lee@acme.onmicrosoft.com"}; !reflect.DeepEqual(c.Senders, want) {
		t.Errorf("senders = %v", c.Senders)
	}
	if c.CreatedAt == nil || c.CreatedAt.Format(time.RFC3339) != "2025-08-20T10:00:00Z" {
		t.Errorf("created = %v", c.CreatedAt)
	}
	if len(c.Steps) != 3 {
		t.Fatalf("steps = %d", len(c.Steps))
	}
	waits := [][2]int{{c.Steps[0].WaitDays, c.Steps[0].WaitMinutes}, {c.Steps[1].WaitDays, c.Steps[1].WaitMinutes}, {c.Steps[2].WaitDays, c.Steps[2].WaitMinutes}}
	if want := [][2]int{{0, 0}, {2, 0}, {1, 720}}; !reflect.DeepEqual(waits, want) {
		t.Errorf("waits = %v, want %v", waits, want)
	}
	first := c.Steps[0].Variants
	if len(first) != 2 || first[0].Disabled || !first[1].Disabled || !first[0].HTML {
		t.Errorf("variants = %+v", first)
	}
	if want := "{Quick question|Idea} for {{.Company}}"; first[0].Subject != want {
		t.Errorf("subject = %q", first[0].Subject)
	}
	if want := `Hey {{.FirstName | default "there"}},<br/><br/>{{.personalization}}<br/><br/>{{.Sender.FirstName}}<br/>`; first[0].Body != want {
		t.Errorf("body = %q", first[0].Body)
	}
	if want := "Hi {{.FirstName}}, saw {{.company_domain}}."; first[1].Body != want {
		t.Errorf("variant B = %q", first[1].Body)
	}
	if c.Steps[0].NewThread || c.Steps[1].NewThread || !c.Steps[2].NewThread {
		t.Errorf("threads = %v %v %v", c.Steps[0].NewThread, c.Steps[1].NewThread, c.Steps[2].NewThread)
	}
	if !strings.HasPrefix(c.Steps[2].Variants[0].Body, "{Hi {{.FirstName}}|Hello},") {
		t.Errorf("nested spintax = %q", c.Steps[2].Variants[0].Body)
	}
	if !strings.Contains(c.Steps[2].Variants[0].Body, "{{.Sender.Name}}") {
		t.Errorf("sender name = %q", c.Steps[2].Variants[0].Body)
	}
	sch := c.Schedule
	if sch == nil || sch.Timezone != "Europe/Belgrade" || sch.From != "09:00" || sch.To != "17:30" || sch.MinGapMinutes != 12 {
		t.Fatalf("schedule = %+v", sch)
	}
	if want := []int{1, 2, 3, 4, 5}; !reflect.DeepEqual(sch.Days, want) {
		t.Errorf("days = %v", sch.Days)
	}
	if sch.Start == nil || sch.Start.Format(time.RFC3339) != "2025-09-01T00:00:00+02:00" || sch.End != nil {
		t.Errorf("start/end = %v %v", sch.Start, sch.End)
	}
	notes := strings.Join(c.UnmappedNotes, "\n")
	for _, want := range []string{"first of 2 sending schedules", "accountSignature", "Liquid", "plain text", "whole company", "BCC"} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes miss %q:\n%s", want, notes)
		}
	}
	if strings.Contains(notes, "—") {
		t.Errorf("notes use an em dash")
	}
	if cs[1].Schedule != nil || len(cs[1].Steps) != 0 || !cs[1].TrackOpens {
		t.Errorf("draft = %+v", cs[1])
	}
}

func TestInventory(t *testing.T) {
	s, _ := open(t)
	inv, err := s.Inventory(context.Background(), outreachsource.Workspace{})
	if err != nil {
		t.Fatal(err)
	}
	if inv.Mailboxes != 3 || inv.Campaigns != 2 || inv.Leads != 150 || inv.Blocklist != -1 {
		t.Errorf("inventory = %+v", inv)
	}
	if len(inv.Summaries) != 2 || inv.Summaries[0].Steps != 3 || inv.Summaries[0].Leads != 150 {
		t.Errorf("summaries = %+v", inv.Summaries)
	}
}

func TestLeadsPages(t *testing.T) {
	s, f := open(t)
	ctx := context.Background()
	page, next, err := s.Leads(ctx, outreachsource.Workspace{}, campaignA, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 2 || next != "01a1d05e-ffec-71bd-932c-000000000002" {
		t.Fatalf("page 1 = %d leads, next %q", len(page), next)
	}
	ana := page[0]
	if ana.Email != "ana@globex.com" || ana.FirstName != "Ana" || ana.Company != "Globex" || ana.Title != "CEO" || ana.Website != "https://globex.com" || ana.Phone != "+1 555 0100" {
		t.Errorf("ana = %+v", ana)
	}
	wantCustom := map[string]string{
		"company_size": "51-200", "icp_tier": "2", "is_hot": "true",
		"personalization": "Loved your talk at SaaStr.", "company_domain": "globex.com",
	}
	if got := ana.LeadCustomKeys(); got["title"] != "CEO" || got["website"] != "https://globex.com" {
		t.Errorf("lead custom keys = %v", got)
	}
	if !reflect.DeepEqual(ana.Custom, wantCustom) {
		t.Errorf("custom = %v", ana.Custom)
	}
	if ana.Status != outreachsource.LeadInProgress || ana.StepsSent != 1 || ana.LastSent == nil {
		t.Errorf("ana status = %s steps %d last %v", ana.Status, ana.StepsSent, ana.LastSent)
	}
	if page[1].Status != outreachsource.LeadInterested {
		t.Errorf("bo = %s", page[1].Status)
	}
	page2, next, err := s.Leads(ctx, outreachsource.Workspace{}, campaignA, next)
	if err != nil {
		t.Fatal(err)
	}
	if len(page2) != 1 || next != "" || page2[0].Status != outreachsource.LeadBounced {
		t.Fatalf("page 2 = %+v next %q", page2, next)
	}
	if f.leadBodies[0]["campaign"] != campaignA || f.leadBodies[1]["starting_after"] != "01a1d05e-ffec-71bd-932c-000000000002" {
		t.Errorf("bodies = %v", f.leadBodies)
	}
	if _, _, err := s.Leads(ctx, outreachsource.Workspace{}, "", ""); err != nil {
		t.Fatal(err)
	}
	if last := f.leadBodies[len(f.leadBodies)-1]; last["in_campaign"] != false || last["campaign"] != nil {
		t.Errorf("outside-campaign body = %v", last)
	}
}

func TestLeadStatusMapping(t *testing.T) {
	s, _ := open(t)
	s.steps["status"] = 3
	page, _, err := s.Leads(context.Background(), outreachsource.Workspace{}, "status", "")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]outreachsource.LeadStatus{
		"s1": outreachsource.LeadCompleted, "s2": outreachsource.LeadUnsubscribed,
		"s3": outreachsource.LeadMeeting, "s4": outreachsource.LeadNotInterest,
		"s5": outreachsource.LeadPaused, "s6": outreachsource.LeadPaused,
		"s7": outreachsource.LeadNotStarted,
	}
	for _, l := range page {
		if l.Status != want[l.ID] {
			t.Errorf("%s = %s, want %s", l.ID, l.Status, want[l.ID])
		}
		if l.ID == "s1" && l.StepsSent != 3 {
			t.Errorf("completed steps = %d", l.StepsSent)
		}
		if l.ID == "s7" && l.StepsSent != 0 {
			t.Errorf("not started steps = %d", l.StepsSent)
		}
	}
}

func TestBlocklist(t *testing.T) {
	s, _ := open(t)
	page, next, err := s.Blocklist(context.Background(), outreachsource.Workspace{}, "")
	if err != nil {
		t.Fatal(err)
	}
	want := []outreachsource.BlockEntry{{Value: "competitor.com", Domain: true}, {Value: "ceo@bigco.com"}}
	if !reflect.DeepEqual(page, want) || next == "" {
		t.Fatalf("page = %+v next %q", page, next)
	}
	page, next, err = s.Blocklist(context.Background(), outreachsource.Workspace{}, next)
	if err != nil || len(page) != 0 || next != "" {
		t.Fatalf("last page = %+v next %q err %v", page, next, err)
	}
}

func TestMailboxes(t *testing.T) {
	s, _ := open(t)
	ms, err := s.Mailboxes(context.Background(), outreachsource.Workspace{})
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 3 {
		t.Fatalf("mailboxes = %d", len(ms))
	}
	jane, sam, lee := ms[0], ms[1], ms[2]
	if jane.Kind != outreachsource.MailboxGoogle || jane.Email != "jane@acme.io" || jane.FromName != "Jane Doe" || jane.DailyCap != 50 ||
		jane.Warmup == nil || !*jane.Warmup || jane.Signature != "<p>Jane Doe, Acme</p>" || !reflect.DeepEqual(jane.Tags, []string{"Primary"}) {
		t.Errorf("jane = %+v", jane)
	}
	if sam.Kind != outreachsource.MailboxSMTP || sam.FromName != "Sam" || sam.ReplyTo != "replies@acme.io" || sam.Warmup == nil || *sam.Warmup {
		t.Errorf("sam = %+v", sam)
	}
	if sam.HasSecrets() || sam.SMTP != nil || sam.IMAP != nil {
		t.Errorf("sam has secrets: %+v", sam)
	}
	if lee.Kind != outreachsource.MailboxMicrosoft || lee.DailyCap != 0 || lee.Warmup != nil {
		t.Errorf("lee = %+v", lee)
	}
	if New().Info().MailboxSecrets {
		t.Error("Info claims mailbox secrets")
	}
}

func TestRetriesRateLimit(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "3")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write(fixture(t, "rate_limited.json"))
			return
		}
		_, _ = w.Write(fixture(t, "workspace.json"))
	}))
	defer srv.Close()
	p := newProvider(srv.URL)
	var waited []time.Duration
	p.sleep = func(_ context.Context, d time.Duration) error {
		waited = append(waited, d)
		return nil
	}
	if _, err := p.Open(context.Background(), outreachsource.Credentials{"api_key": goodKey}); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(waited) != 1 || waited[0] != 3*time.Second {
		t.Errorf("calls = %d, waited = %v", calls, waited)
	}
}

func TestInfo(t *testing.T) {
	i := New().Info()
	if i.ID != "instantly" || !i.Workspaces || !i.MultiKey || len(i.Kinds) != 4 || len(i.Fields) != 1 || !i.Fields[0].Secret {
		t.Errorf("info = %+v", i)
	}
	for _, s := range append([]string{i.KeyHelp, i.Fields[0].Help}, i.Notes...) {
		if strings.Contains(s, "—") {
			t.Errorf("em dash in %q", s)
		}
	}
}
