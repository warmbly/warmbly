package saleshandy

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	source "github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

const testKey = "sh-test-key"

type fakeAPI struct {
	t        *testing.T
	limitOne atomic.Bool // answer the next request with a 429
	requests []string
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)
	if r.Header.Get("x-api-key") != testKey {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write(fixture(f.t, "invalid_token.json"))
		return
	}
	if f.limitOne.CompareAndSwap(true, false) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
		return
	}
	if r.Method == http.MethodPost && r.URL.Path == "/v1/email-accounts" {
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		if json.Unmarshal(body, &req) != nil || req["page"] == nil {
			f.t.Errorf("email-accounts body = %s", body)
		}
	}
	page := r.URL.Query().Get("page")
	var name string
	switch r.Method + " " + r.URL.Path {
	case "GET /v1/sequences":
		if page != "1" {
			_, _ = w.Write([]byte(`{"message":"ok","payload":[]}`))
			return
		}
		name = "sequences.json"
	case "GET /v1/clients":
		name = "clients.json"
	case "GET /v1/sequences/JA5YdAr9wy/steps":
		name = "steps.json"
	case "GET /v1/sequences/Q2wE3rT4yU/steps":
		_, _ = w.Write([]byte(`{"message":"ok","payload":[]}`))
		return
	case "GET /v1/sequences/JA5YdAr9wy/steps/Zx8Wv7Ut6S", "GET /v1/sequences/Q2wE3rT4yU/steps/aaaa111111":
		name = "step_variants.json"
	case "GET /v1/sequences/JA5YdAr9wy/settings", "GET /v1/sequences/Q2wE3rT4yU/settings":
		name = "settings.json"
	case "GET /v1/schedules":
		name = "schedules.json"
	case "GET /v1/sequences/JA5YdAr9wy/email-accounts", "GET /v1/sequences/Q2wE3rT4yU/email-accounts":
		name = "sequence_email_accounts.json"
	case "POST /v1/analytics/stats":
		name = "stats.json"
	case "POST /v1/email-accounts":
		name = "email_accounts.json"
	case "GET /v1/prospects":
		name = "prospects_" + page + ".json"
	case "GET /v1/dnc":
		name = "dnc_lists.json"
	case "GET /v1/dnc/vWjzRkZwAq":
		name = "dnc_items_" + page + ".json"
	default:
		f.t.Errorf("unexpected request %s %s", r.Method, r.URL)
		w.WriteHeader(http.StatusNotFound)
		return
	}
	_, _ = w.Write(fixture(f.t, name))
}

func open(t *testing.T, key string) (*fakeAPI, source.Session, error) {
	t.Helper()
	old := pageSize
	pageSize = 2
	t.Cleanup(func() { pageSize = old })
	api := &fakeAPI{t: t}
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	p := newProvider(srv.URL)
	p.sleep = func(context.Context, time.Duration) error { return nil }
	s, err := p.Open(context.Background(), source.Credentials{"api_key": key})
	return api, s, err
}

func mustOpen(t *testing.T) (*fakeAPI, source.Session) {
	t.Helper()
	api, s, err := open(t, testKey)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return api, s
}

var (
	account = source.Workspace{ID: "", Name: "Saleshandy account"}
	acme    = source.Workspace{ID: "g9bzZq4aMd", Name: "Acme"}
)

func TestInfo(t *testing.T) {
	info := New().Info()
	if info.ID != "saleshandy" || info.Label != "Saleshandy" || !info.Workspaces || info.MailboxSecrets || info.MultiKey {
		t.Errorf("info = %+v", info)
	}
	for _, s := range append(info.Notes, info.KeyHelp) {
		if strings.Contains(s, "—") {
			t.Errorf("em dash in %q", s)
		}
	}
}

func TestOpenRejectsKey(t *testing.T) {
	_, _, err := open(t, "wrong")
	if !errors.Is(err, source.ErrKey) {
		t.Fatalf("err = %v, want ErrKey", err)
	}
	if strings.Contains(err.Error(), "wrong") {
		t.Errorf("error leaks the key: %v", err)
	}
	if _, _, err := open(t, " "); !errors.Is(err, source.ErrKey) {
		t.Errorf("blank key err = %v", err)
	}
}

func TestWorkspaces(t *testing.T) {
	_, s := mustOpen(t)
	ws, err := s.Workspaces(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := []source.Workspace{account, acme}; !reflect.DeepEqual(ws, want) {
		t.Errorf("workspaces = %+v, want %+v", ws, want)
	}
}

func TestCampaigns(t *testing.T) {
	_, s := mustOpen(t)
	cs, err := s.Campaigns(context.Background(), account)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 1 {
		t.Fatalf("account campaigns = %d, want only the one without a client", len(cs))
	}
	c := cs[0]
	if c.ID != "JA5YdAr9wy" || c.Name != "Founders Q3" || c.Status != source.CampaignActive || c.LeadCount != 25 {
		t.Errorf("campaign = %+v", c)
	}
	if c.CreatedAt == nil || c.CreatedAt.Year() != 2025 {
		t.Errorf("created = %v", c.CreatedAt)
	}
	if !c.StopOnReply || !c.TrackOpens || c.TrackClicks || !c.Unsubscribe || c.PlainText {
		t.Errorf("settings = reply %v opens %v clicks %v unsub %v plain %v", c.StopOnReply, c.TrackOpens, c.TrackClicks, c.Unsubscribe, c.PlainText)
	}
	if !reflect.DeepEqual(c.Senders, []string{"john@sender.example"}) {
		t.Errorf("senders = %v", c.Senders)
	}
	want := &source.Schedule{Timezone: "America/New_York", Days: []int{1, 2, 3, 4, 5}, From: "08:30", To: "17:00"}
	if !reflect.DeepEqual(c.Schedule, want) {
		t.Errorf("schedule = %+v, want %+v", c.Schedule, want)
	}
	if len(c.Steps) != 2 {
		t.Fatalf("steps = %+v", c.Steps)
	}
	first := c.Steps[0]
	if first.WaitDays != 0 || first.WaitMinutes != 0 || len(first.Variants) != 2 {
		t.Errorf("step 1 = %+v", first)
	}
	a := first.Variants[0]
	if a.Subject != `{Quick question|Idea} for {{.Company}}` {
		t.Errorf("subject = %q", a.Subject)
	}
	if wantBody := `<p>Hi {{.FirstName | default "there"}},</p><p>Saw {{.website}}.</p><p>{{.Sender.FirstName}}</p>`; a.Body != wantBody || !a.HTML || a.Disabled {
		t.Errorf("body = %q", a.Body)
	}
	if b := first.Variants[1]; b.Subject != "Hello {{.FirstName}}" || !b.Disabled {
		t.Errorf("variant B = %+v", b)
	}
	second := c.Steps[1]
	if second.WaitDays != 3 || second.WaitMinutes != 30 || len(second.Variants) != 1 || second.Variants[0].Subject != "" {
		t.Errorf("step 3 = %+v", second)
	}
	if second.Variants[0].Body != "<div>Following up, {{.FirstName}}.</div>" {
		t.Errorf("step 3 body = %q", second.Variants[0].Body)
	}
	notes := strings.Join(c.UnmappedNotes, "\n")
	for _, n := range []string{"Step 2 is a LinkedIn connection request step", "Signature tags were removed", "preheaders", "widest window"} {
		if !strings.Contains(notes, n) {
			t.Errorf("notes %q missing %q", notes, n)
		}
	}

	cs, err = s.Campaigns(context.Background(), acme)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 1 || cs[0].ID != "Q2wE3rT4yU" || cs[0].Status != source.CampaignPaused || len(cs[0].Steps) != 1 {
		t.Errorf("client campaigns = %+v", cs)
	}
}

func TestLeads(t *testing.T) {
	_, s := mustOpen(t)
	ctx := context.Background()
	page, next, err := s.Leads(ctx, account, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if next != "2" || len(page) != 2 {
		t.Fatalf("page 1 = %d leads, next %q", len(page), next)
	}
	ada := page[0]
	want := source.Lead{
		ID: "jzRkmgMdwA", Email: "ada@lovelace.example", FirstName: "Ada", LastName: "Lovelace",
		Company: "Analytical", Phone: "+44 20 7946 0000", Title: "Engineer", Website: "analytical.example",
		LinkedIn: "https://linkedin.com/in/ada", Location: "London", Status: source.LeadNotStarted,
		Custom: map[string]string{"country": "UK", "deal_stage": "Warm"},
	}
	if !reflect.DeepEqual(ada, want) {
		t.Errorf("lead = %+v\nwant %+v", ada, want)
	}
	page, next, err = s.Leads(ctx, account, "", next)
	if err != nil || next != "" || len(page) != 1 || page[0].Custom["state"] != "Bletchley" {
		t.Errorf("page 2 = %+v next %q err %v", page, next, err)
	}
	if _, _, err := s.Leads(ctx, account, "JA5YdAr9wy", ""); !errors.Is(err, source.ErrUnsupported) {
		t.Errorf("campaign leads err = %v, want ErrUnsupported", err)
	}
	if page, next, err := s.Leads(ctx, acme, "", ""); err != nil || len(page) != 0 || next != "" {
		t.Errorf("client leads = %v %q %v", page, next, err)
	}
}

func TestBlocklist(t *testing.T) {
	_, s := mustOpen(t)
	ctx := context.Background()
	var all []source.BlockEntry
	cursor := ""
	for i := 0; i < 5; i++ {
		page, next, err := s.Blocklist(ctx, acme, cursor)
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, page...)
		if next == "" {
			break
		}
		cursor = next
	}
	reason := "Saleshandy do-not-contact list: Global Do Not Contact List"
	want := []source.BlockEntry{
		{Value: "blocked@example.com", Reason: reason},
		{Value: "competitor.example", Domain: true, Reason: reason},
		{Value: "no@thanks.example", Reason: reason},
	}
	if !reflect.DeepEqual(all, want) {
		t.Errorf("blocklist = %+v", all)
	}
}

func TestMailboxes(t *testing.T) {
	_, s := mustOpen(t)
	mbs, err := s.Mailboxes(context.Background(), account)
	if err != nil {
		t.Fatal(err)
	}
	want := []source.Mailbox{
		{ID: "XdPd8p1PVo", Email: "john@sender.example", FromName: "John Doe", Kind: source.MailboxGoogle},
		{ID: "YePe9q2QWp", Email: "ops@outlook-sender.example", FromName: "Ops", Kind: source.MailboxMicrosoft},
	}
	if !reflect.DeepEqual(mbs, want) {
		t.Errorf("mailboxes = %+v", mbs)
	}
	mbs, err = s.Mailboxes(context.Background(), acme)
	if err != nil || len(mbs) != 1 || mbs[0].Kind != source.MailboxSMTP || mbs[0].HasSecrets() {
		t.Errorf("client mailboxes = %+v err %v", mbs, err)
	}
}

func TestInventory(t *testing.T) {
	_, s := mustOpen(t)
	inv, err := s.Inventory(context.Background(), account)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Mailboxes != 2 || inv.Campaigns != 1 || inv.Leads != -1 || inv.Blocklist != 3 {
		t.Errorf("inventory = %+v", inv)
	}
	want := []source.CampaignSummary{{ID: "JA5YdAr9wy", Name: "Founders Q3", Status: source.CampaignActive, Steps: 3, Leads: 25}}
	if !reflect.DeepEqual(inv.Summaries, want) {
		t.Errorf("summaries = %+v", inv.Summaries)
	}
}

func TestRetriesRateLimit(t *testing.T) {
	api, s := mustOpen(t)
	var waited []time.Duration
	s.(*session).c.Sleep = func(_ context.Context, d time.Duration) error {
		waited = append(waited, d)
		return nil
	}
	api.limitOne.Store(true)
	ws, err := s.Workspaces(context.Background())
	if err != nil || len(ws) != 2 {
		t.Fatalf("workspaces = %v err %v", ws, err)
	}
	if len(waited) != 1 || waited[0] != 7*time.Second {
		t.Errorf("waited = %v, want one 7s Retry-After", waited)
	}
}
