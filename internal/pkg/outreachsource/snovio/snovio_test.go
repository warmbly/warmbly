package snovio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	source "github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

const (
	testID     = "client-id"
	testSecret = "client-secret"
)

type fakeAPI struct {
	t *testing.T

	mu       sync.Mutex
	issued   int    // tokens handed out
	token    string // the only token the API accepts
	limitOne bool   // answer the next API request with a 429
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
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.URL.Path == "/v1/oauth/access_token" {
		if r.Method != http.MethodPost || r.PostFormValue("grant_type") != "client_credentials" ||
			r.PostFormValue("client_id") != testID || r.PostFormValue("client_secret") != testSecret {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_client","error_description":"Client authentication failed"}`))
			return
		}
		f.issued++
		f.token = fmt.Sprintf("tok-%d", f.issued)
		_, _ = fmt.Fprintf(w, `{"access_token":%q,"token_type":"Bearer","expires_in":3600}`, f.token)
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+f.token {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"success":false,"message":"Unauthorized"}`))
		return
	}
	if f.limitOne {
		f.limitOne = false
		w.Header().Set("Retry-After", "3")
		w.WriteHeader(http.StatusTooManyRequests)
		return
	}
	q := r.URL.Query()
	var name string
	switch p := r.Method + " " + r.URL.Path; {
	case p == "GET /v1/get-balance":
		name = "balance.json"
	case p == "GET /v2/sender-accounts/emails":
		name = "senders.json"
	case p == "GET /v1/get-user-campaigns":
		name = "campaigns.json"
	case p == "GET /v2/campaigns/237945":
		name = "campaign.json"
	case p == "GET /v2/campaigns/237956":
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"success":false,"message":"Campaign not found"}`))
		return
	case p == "GET /v2/campaigns/schedules":
		name = "schedules.json"
	case strings.HasPrefix(p, "GET /v2/campaigns/237945/steps/"):
		name = "content_" + r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:] + ".json"
	case p == "GET /v1/get-user-lists":
		name = "lists.json"
	case p == "POST /v1/prospect-list":
		if q.Get("perPage") != "2" {
			f.t.Errorf("perPage = %q", q.Get("perPage"))
		}
		name = "prospects_" + q.Get("listId") + "_" + q.Get("page") + ".json"
	case p == "GET /v2/campaigns/237945/recipient":
		var known map[string]json.RawMessage
		_ = json.Unmarshal(fixture(f.t, "recipients.json"), &known)
		var rows []string
		for _, e := range q["emails[]"] {
			row, ok := known[strings.ToLower(e)]
			if !ok {
				f.t.Errorf("unexpected recipient %q", e)
				continue
			}
			rows = append(rows, fmt.Sprintf(`{"email":%q,%s`, e, strings.TrimPrefix(string(row), "{")))
		}
		_, _ = fmt.Fprintf(w, `{"data":[%s]}`, strings.Join(rows, ","))
		return
	case p == "GET /v2/blacklists":
		name = "blacklists.json"
	case p == "GET /v2/blacklists/8/items":
		name = "blacklist_items_" + q.Get("page") + ".json"
	default:
		f.t.Errorf("unexpected request %s %s", r.Method, r.URL)
		w.WriteHeader(http.StatusNotFound)
		return
	}
	_, _ = w.Write(fixture(f.t, name))
}

func open(t *testing.T, id, secret string) (*fakeAPI, *session, error) {
	t.Helper()
	old := pageSize
	pageSize = 2
	t.Cleanup(func() { pageSize = old })
	api := &fakeAPI{t: t}
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	p := newProvider(srv.URL)
	p.sleep = func(context.Context, time.Duration) error { return nil }
	s, err := p.Open(context.Background(), source.Credentials{"client_id": id, "client_secret": secret})
	if err != nil {
		return api, nil, err
	}
	return api, s.(*session), nil
}

func mustOpen(t *testing.T) (*fakeAPI, *session) {
	t.Helper()
	api, s, err := open(t, testID, testSecret)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return api, s
}

var ws = source.Workspace{ID: "", Name: "Snov.io account"}

func TestInfo(t *testing.T) {
	info := New().Info()
	if info.ID != "snovio" || info.Label != "Snov.io" || info.Workspaces || info.MailboxSecrets || len(info.Fields) != 2 || !info.Fields[1].Secret {
		t.Errorf("info = %+v", info)
	}
	for _, s := range append(info.Notes, info.KeyHelp) {
		if strings.Contains(s, "—") {
			t.Errorf("em dash in %q", s)
		}
	}
}

func TestOpenRejectsSecret(t *testing.T) {
	_, _, err := open(t, testID, "wrong-secret")
	if !errors.Is(err, source.ErrKey) {
		t.Fatalf("err = %v, want ErrKey", err)
	}
	if strings.Contains(err.Error(), "wrong-secret") {
		t.Errorf("error leaks the secret: %v", err)
	}
	if _, _, err := open(t, "", testSecret); !errors.Is(err, source.ErrKey) {
		t.Errorf("missing id err = %v", err)
	}
}

func TestTokenRenewal(t *testing.T) {
	api, s := mustOpen(t)
	ctx := context.Background()
	// An expired token is exchanged before the next call.
	s.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	if _, err := s.Mailboxes(ctx, ws); err != nil {
		t.Fatal(err)
	}
	if api.issued != 2 {
		t.Errorf("tokens issued = %d, want 2 after expiry", api.issued)
	}
	// A token the API stops accepting is renewed once and the call retried.
	api.mu.Lock()
	api.token = "revoked"
	api.mu.Unlock()
	s.now = func() time.Time { return time.Now().Add(-time.Hour) }
	if _, err := s.Mailboxes(ctx, ws); err != nil {
		t.Fatal(err)
	}
	if api.issued != 3 {
		t.Errorf("tokens issued = %d, want 3 after a refusal", api.issued)
	}
}

func TestWorkspaces(t *testing.T) {
	_, s := mustOpen(t)
	got, err := s.Workspaces(context.Background())
	if err != nil || !reflect.DeepEqual(got, []source.Workspace{ws}) {
		t.Errorf("workspaces = %+v err %v", got, err)
	}
}

func TestMailboxes(t *testing.T) {
	_, s := mustOpen(t)
	mbs, err := s.Mailboxes(context.Background(), ws)
	if err != nil {
		t.Fatal(err)
	}
	want := []source.Mailbox{
		{ID: "11980", Email: "Den.Johnson@sender.example", FromName: "Den Johnson", Kind: source.MailboxSMTP, DailyCap: 50, Tags: []string{"Den's account"}},
		{ID: "12355", Email: "anna@gmail-sender.example", FromName: "Anna", Kind: source.MailboxGoogle, DailyCap: 40, Tags: []string{"Work"}},
	}
	if !reflect.DeepEqual(mbs, want) {
		t.Errorf("mailboxes = %+v", mbs)
	}
	if mbs[0].HasSecrets() {
		t.Error("a mailbox without a password reports secrets")
	}
}

func TestCampaigns(t *testing.T) {
	_, s := mustOpen(t)
	cs, err := s.Campaigns(context.Background(), ws)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 2 {
		t.Fatalf("campaigns = %d", len(cs))
	}
	c := cs[0]
	if c.ID != "237945" || c.Name != "New Campaign" || c.Status != source.CampaignPaused || c.LeadCount != 3 {
		t.Errorf("campaign = %+v", c)
	}
	if c.CreatedAt == nil || c.CreatedAt.Unix() != 1639469976 {
		t.Errorf("created = %v", c.CreatedAt)
	}
	if !c.TrackOpens || c.TrackClicks || !c.StopOnReply || !c.Unsubscribe || c.DailyLimit != 120 || c.MaxNewLeadsPerDay != 30 || c.PlainText {
		t.Errorf("settings = %+v", c)
	}
	if !reflect.DeepEqual(c.Senders, []string{"den.johnson@sender.example"}) {
		t.Errorf("senders = %v", c.Senders)
	}
	sc := c.Schedule
	if sc == nil || sc.Timezone != "Europe/Kyiv" || !reflect.DeepEqual(sc.Days, []int{1, 6}) || sc.From != "09:00" || sc.To != "17:00" {
		t.Fatalf("schedule = %+v", sc)
	}
	ny, _ := time.LoadLocation("America/New_York")
	if sc.Start == nil || !sc.Start.Equal(time.Date(2027, 3, 25, 9, 0, 0, 0, ny)) || sc.End != nil {
		t.Errorf("start = %v end = %v", sc.Start, sc.End)
	}
	if len(c.Steps) != 2 {
		t.Fatalf("steps = %+v", c.Steps)
	}
	first := c.Steps[0]
	if first.WaitDays != 0 || first.WaitMinutes != 0 || len(first.Variants) != 2 {
		t.Errorf("step 1 = %+v", first)
	}
	a := first.Variants[0]
	if a.Subject != "{Quick idea|Question} for {{.Company}}" || !a.HTML || a.Disabled {
		t.Errorf("variant A = %+v", a)
	}
	if want := "<p>Hi {{.FirstName}},</p><p>{{.full_name}} at {{.title}} in {{.location}}</p><p>{{.Sender.Name}}</p>"; a.Body != want {
		t.Errorf("body = %q", a.Body)
	}
	if b := first.Variants[1]; !b.Disabled || b.Subject != "Hey {{.FirstName}}" {
		t.Errorf("variant B = %+v", b)
	}
	second := c.Steps[1]
	if second.WaitDays != 3 || second.WaitMinutes != 0 || len(second.Variants) != 1 {
		t.Errorf("step 2 = %+v", second)
	}
	if v := second.Variants[0]; v.Subject != "" || v.HTML || v.Body != "Following up, {{.FirstName}}. {{.project_name}}" {
		t.Errorf("step 2 variant = %+v", v)
	}
	notes := strings.Join(c.UnmappedNotes, "\n")
	for _, n := range []string{"widest window", "opened an email", "did not meet it", "LinkedIn step (invite)", "other branch", "Email 3 has no content"} {
		if !strings.Contains(notes, n) {
			t.Errorf("notes %q missing %q", notes, n)
		}
	}
	old := cs[1]
	if old.Status != source.CampaignDraft || len(old.Steps) != 0 || len(old.UnmappedNotes) != 1 || !strings.Contains(old.UnmappedNotes[0], "recreated") {
		t.Errorf("unreadable campaign = %+v", old)
	}
}

func TestLeads(t *testing.T) {
	_, s := mustOpen(t)
	ctx := context.Background()
	var all []source.Lead
	cursor, pages := "", 0
	for {
		page, next, err := s.Leads(ctx, ws, "237945", cursor)
		if err != nil {
			t.Fatal(err)
		}
		pages++
		all = append(all, page...)
		if next == "" {
			break
		}
		cursor = next
	}
	if pages != 3 || len(all) != 3 {
		t.Fatalf("pages = %d leads = %+v", pages, all)
	}
	want := source.Lead{ID: "226db935fc93422496fda5d5209e8cbf", Email: "andrew@exp.example", FirstName: "Andrew", LastName: "Garfiled", Custom: map[string]string{"full_name": "Andrew Garfiled"}, Status: source.LeadInProgress}
	if !reflect.DeepEqual(all[0], want) {
		t.Errorf("lead = %+v", all[0])
	}
	if all[1].Status != source.LeadUnsubscribed || all[2].Status != source.LeadNotStarted || all[0].StepsExact {
		t.Errorf("statuses = %s %s", all[1].Status, all[2].Status)
	}

	page, next, err := s.Leads(ctx, ws, "", "")
	if err != nil || next != "" || len(page) != 1 || page[0].Email != "lone@lead.example" {
		t.Errorf("loose leads = %+v next %q err %v", page, next, err)
	}
}

func TestRecipientStatus(t *testing.T) {
	for in, want := range map[string]source.LeadStatus{
		"tocheck": source.LeadNotStarted, "active": source.LeadInProgress, "Paused": source.LeadPaused,
		"finished": source.LeadCompleted, "moved": source.LeadCompleted, "unsubscribe": source.LeadUnsubscribed,
		"Unsubscribed": source.LeadUnsubscribed, "Auto-replied": source.LeadReplied, "Replied from another email": source.LeadReplied,
		"bounced": source.LeadBounced,
	} {
		if got := recipientStatus(true, in); got != want {
			t.Errorf("recipientStatus(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestBlocklist(t *testing.T) {
	_, s := mustOpen(t)
	ctx := context.Background()
	page, next, err := s.Blocklist(ctx, ws, "")
	if err != nil || next != "0:2" {
		t.Fatalf("page 1 next %q err %v", next, err)
	}
	reason := "Snov.io do-not-email list: Do-not-email List"
	want := []source.BlockEntry{{Value: "john@example.com", Reason: reason}, {Value: "example.org", Domain: true, Reason: reason}}
	if !reflect.DeepEqual(page, want) {
		t.Errorf("page 1 = %+v", page)
	}
	page, next, err = s.Blocklist(ctx, ws, next)
	if err != nil || next != "" || len(page) != 0 {
		t.Errorf("page 2 = %+v next %q err %v", page, next, err)
	}
}

func TestInventory(t *testing.T) {
	_, s := mustOpen(t)
	inv, err := s.Inventory(context.Background(), ws)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Mailboxes != 2 || inv.Campaigns != 2 || inv.Leads != 4 || inv.Blocklist != 3 {
		t.Errorf("inventory = %+v", inv)
	}
	want := []source.CampaignSummary{
		{ID: "237945", Name: "New Campaign", Status: source.CampaignPaused, Steps: 4, Leads: 3},
		{ID: "237956", Name: "Old campaign", Status: source.CampaignDraft, Steps: 0, Leads: 0},
	}
	if !reflect.DeepEqual(inv.Summaries, want) {
		t.Errorf("summaries = %+v", inv.Summaries)
	}
}

func TestRetriesRateLimit(t *testing.T) {
	api, s := mustOpen(t)
	var waited []time.Duration
	s.c.Sleep = func(_ context.Context, d time.Duration) error {
		waited = append(waited, d)
		return nil
	}
	api.mu.Lock()
	api.limitOne = true
	api.mu.Unlock()
	if _, err := s.Mailboxes(context.Background(), ws); err != nil {
		t.Fatal(err)
	}
	if len(waited) != 1 || waited[0] != 3*time.Second {
		t.Errorf("waited = %v, want one 3s Retry-After", waited)
	}
}
