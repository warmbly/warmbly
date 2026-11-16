package lemlist

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	source "github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

const goodKey = "lm-test-key"

// fixtures maps a request path (and campaignId query, when set) to a testdata file.
var fixtures = map[string]string{
	"/team":                                   "team.json",
	"/campaigns":                              "campaigns.json",
	"/campaigns/cam_launch/settings":          "settings_launch.json",
	"/campaigns/cam_launch/sequences":         "sequences_launch.json",
	"/campaigns/cam_launch/schedules/":        "schedules_launch.json",
	"/sequences/seq_main/steps/stp_3/ab-test": "abtest_stp_3.json",
	"/senders":                                "senders.json",
	"/senders?cam_launch":                     "senders_launch.json",
	"/lemwarm/usm_google/settings":            "lemwarm_google.json",
	"/v2/campaigns/cam_launch/export/leads":   "export_launch.json",
	"/v2/unsubscribes/variables":              "unsubscribes.json",
}

type fake struct {
	t        *testing.T
	throttle atomic.Int32 // requests to answer 429 before serving
	calls    atomic.Int32
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.calls.Add(1)
	if r.Method != http.MethodGet {
		f.t.Errorf("non-GET request %s %s", r.Method, r.URL.Path)
	}
	user, pass, ok := r.BasicAuth()
	if !ok || user != "" || pass != goodKey {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("The authentication you supplied is incorrect"))
		return
	}
	if f.throttle.Load() > 0 {
		f.throttle.Add(-1)
		w.Header().Set("Retry-After", "2")
		w.WriteHeader(http.StatusTooManyRequests)
		return
	}
	switch r.URL.Path {
	case "/campaigns/cam_old/settings":
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write([]byte("route is available starting emailPro plan"))
		return
	case "/campaigns/cam_old/sequences":
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("Campaign has no sequence"))
		return
	case "/campaigns/cam_old/schedules/":
		_, _ = w.Write([]byte("[]"))
		return
	case "/lemwarm/usm_smtp/settings":
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("Error retrieving settings"))
		return
	case "/contacts":
		f.contacts(w, r)
		return
	}
	key := r.URL.Path
	if id := r.URL.Query().Get("campaignId"); id != "" {
		key += "?" + id
	}
	if key == "/senders?cam_old" {
		_, _ = w.Write([]byte(`{"data":{"senders":[]}}`))
		return
	}
	name, ok := fixtures[key]
	if !ok {
		f.t.Errorf("unexpected request %s", r.URL)
		w.WriteHeader(http.StatusNotFound)
		return
	}
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		f.t.Fatal(err)
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

// contacts serves 130 contacts outside any campaign, in pages.
func (f *fake) contacts(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("notInAnyCampaign") != "true" {
		f.t.Errorf("contacts listed without notInAnyCampaign")
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	var rows []string
	for i := offset; i < min(offset+limit, 130); i++ {
		unsub := i == 5
		rows = append(rows, fmt.Sprintf(`{"_id":"ctc_%d","email":"Person%d@Example.com","firstName":"P%d","jobTitle":"CTO","unsubscribed":%t}`, i, i, i, unsub))
	}
	fmt.Fprintf(w, `{"data":[%s],"total":130,"limit":%d,"offset":%d}`, strings.Join(rows, ","), limit, offset)
}

func open(t *testing.T) (source.Session, *fake) {
	t.Helper()
	f := &fake{t: t}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	p := newProvider(srv.URL)
	p.sleep = func(context.Context, time.Duration) error { return nil }
	s, err := p.Open(context.Background(), source.Credentials{"api_key": goodKey})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s, f
}

func TestOpen(t *testing.T) {
	f := &fake{t: t}
	srv := httptest.NewServer(f)
	defer srv.Close()
	p := newProvider(srv.URL)
	if _, err := p.Open(context.Background(), source.Credentials{"api_key": "wrong"}); !errors.Is(err, source.ErrKey) {
		t.Fatalf("bad key: got %v, want ErrKey", err)
	}
	if _, err := p.Open(context.Background(), source.Credentials{}); !errors.Is(err, source.ErrKey) {
		t.Fatalf("empty key: got %v, want ErrKey", err)
	}
	s, _ := open(t)
	acc, err := s.Account(context.Background())
	if err != nil || acc.Name != "Acme Outbound" {
		t.Fatalf("Account = %+v, %v", acc, err)
	}
	ws, err := s.Workspaces(context.Background())
	if err != nil || len(ws) != 1 || ws[0].ID != "" || ws[0].Name != "Acme Outbound" {
		t.Fatalf("Workspaces = %+v, %v", ws, err)
	}
}

func TestInfo(t *testing.T) {
	info := New().Info()
	if info.ID != "lemlist" || len(info.Fields) != 1 || !info.Fields[0].Secret || info.Workspaces || !info.MultiKey || info.MailboxSecrets {
		t.Fatalf("Info = %+v", info)
	}
	for _, s := range append(info.Notes, info.KeyHelp) {
		if strings.Contains(s, "—") {
			t.Errorf("em dash in %q", s)
		}
	}
}

func TestRateLimitRetried(t *testing.T) {
	s, f := open(t)
	f.throttle.Store(2)
	if _, err := s.Mailboxes(context.Background(), source.Workspace{}); err != nil {
		t.Fatalf("Mailboxes after 429: %v", err)
	}
}

func TestInventory(t *testing.T) {
	s, _ := open(t)
	inv, err := s.Inventory(context.Background(), source.Workspace{})
	if err != nil {
		t.Fatal(err)
	}
	if inv.Campaigns != 2 || inv.Mailboxes != 2 || inv.Leads != -1 || inv.Blocklist != -1 || len(inv.Summaries) != 2 {
		t.Fatalf("Inventory = %+v", inv)
	}
	if inv.Summaries[0].Status != source.CampaignActive || inv.Summaries[1].Status != source.CampaignCompleted {
		t.Fatalf("summary statuses = %+v", inv.Summaries)
	}
}

func TestMailboxes(t *testing.T) {
	s, _ := open(t)
	mbs, err := s.Mailboxes(context.Background(), source.Workspace{})
	if err != nil {
		t.Fatal(err)
	}
	if len(mbs) != 2 {
		t.Fatalf("got %d mailboxes, want 2 email channels", len(mbs))
	}
	g, sm := mbs[0], mbs[1]
	if g.Email != "john@example.com" || g.Kind != source.MailboxGoogle || g.FromName != "John Doe" || g.Warmup == nil || !*g.Warmup {
		t.Errorf("google mailbox = %+v", g)
	}
	if sm.Kind != source.MailboxSMTP || sm.Warmup != nil || sm.HasSecrets() {
		t.Errorf("smtp mailbox = %+v", sm)
	}
}

func TestCampaigns(t *testing.T) {
	s, _ := open(t)
	camps, err := s.Campaigns(context.Background(), source.Workspace{})
	if err != nil {
		t.Fatal(err)
	}
	if len(camps) != 2 {
		t.Fatalf("got %d campaigns", len(camps))
	}
	c := camps[0]
	if c.ID != "cam_launch" || c.Status != source.CampaignActive || !c.TrackOpens || c.TrackClicks || !c.StopOnReply || !c.Unsubscribe {
		t.Errorf("settings = %+v", c)
	}
	if !reflect.DeepEqual(c.Senders, []string{"john@example.com"}) {
		t.Errorf("senders = %v", c.Senders)
	}
	want := &source.Schedule{Timezone: "Europe/Paris", Days: []int{1, 2, 3, 4, 5}, From: "09:00", To: "18:00", MinGapMinutes: 20}
	if !reflect.DeepEqual(c.Schedule, want) {
		t.Errorf("schedule = %+v", c.Schedule)
	}
	if len(c.Steps) != 2 {
		t.Fatalf("steps = %+v", c.Steps)
	}
	first := c.Steps[0]
	if first.WaitDays != 0 || len(first.Variants) != 1 {
		t.Errorf("step 1 = %+v", first)
	}
	v := first.Variants[0]
	if v.Subject != `{{.FirstName | default "there"}}, quick question about {{.Company}}` {
		t.Errorf("subject = %q", v.Subject)
	}
	if v.Body != "<p>{Hi|Hello} {{.FirstName}},</p><p>{{.icebreaker}}</p><p>{{.Sender.Name}}</p>" || !v.HTML {
		t.Errorf("body = %q", v.Body)
	}
	second := c.Steps[1]
	if second.WaitDays != 5 {
		t.Errorf("step 2 wait = %d, want the LinkedIn step's 2 days carried plus 3", second.WaitDays)
	}
	if len(second.Variants) != 2 || second.Variants[0].Subject != "" || second.Variants[1].Body != `<div>Any thoughts, {{.FirstName | default "friend"}}?</div>` {
		t.Errorf("step 2 variants = %+v", second.Variants)
	}
	notes := strings.Join(c.UnmappedNotes, "\n")
	for _, want := range []string{"LinkedIn invite", "condition", "Liquid", "signature", "2 schedules"} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes missing %q:\n%s", want, notes)
		}
	}
	old := camps[1]
	if old.Status != source.CampaignCompleted || len(old.Steps) != 0 || old.Schedule != nil || len(old.UnmappedNotes) != 2 {
		t.Errorf("old campaign = %+v", old)
	}
}

func TestCampaignLeads(t *testing.T) {
	s, _ := open(t)
	page, next, err := s.Leads(context.Background(), source.Workspace{}, "cam_launch", "")
	if err != nil {
		t.Fatal(err)
	}
	if next != "" || len(page) != 7 {
		t.Fatalf("got %d leads, next %q; the lead with no email is skipped", len(page), next)
	}
	john := page[0]
	if john.Email != "john@example.com" || john.Company != "Acme Inc" || john.Title != "CEO" || john.Website != "acme.com" || john.LinkedIn == "" || john.Timezone != "Europe/Paris" {
		t.Errorf("john = %+v", john)
	}
	if !reflect.DeepEqual(john.Custom, map[string]string{"icebreaker": "Saw your post on outbound", "company_size": "51-200"}) {
		t.Errorf("custom = %v", john.Custom)
	}
	if john.StepsExact {
		t.Errorf("lemlist does not report steps sent exactly")
	}
	want := []source.LeadStatus{source.LeadReplied, source.LeadInterested, source.LeadBounced, source.LeadInProgress, source.LeadCompleted, source.LeadNotStarted, source.LeadUnsubscribed}
	for i, l := range page {
		if l.Status != want[i] {
			t.Errorf("%s status = %s, want %s", l.Email, l.Status, want[i])
		}
	}
	if page[3].Custom["score"] != "42" {
		t.Errorf("numeric custom field = %v", page[3].Custom)
	}
}

func TestLooseLeadsPaged(t *testing.T) {
	s, _ := open(t)
	first, next, err := s.Leads(context.Background(), source.Workspace{}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 100 || next != "100" {
		t.Fatalf("first page: %d leads, next %q", len(first), next)
	}
	if first[0].Email != "person0@example.com" || first[0].Title != "CTO" || first[0].Status != source.LeadNotStarted || first[5].Status != source.LeadUnsubscribed {
		t.Errorf("first page leads = %+v %+v", first[0], first[5])
	}
	second, next, err := s.Leads(context.Background(), source.Workspace{}, "", next)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 30 || next != "" {
		t.Fatalf("second page: %d leads, next %q", len(second), next)
	}
}

func TestBlocklist(t *testing.T) {
	s, _ := open(t)
	page, next, err := s.Blocklist(context.Background(), source.Workspace{}, "")
	if err != nil {
		t.Fatal(err)
	}
	want := []source.BlockEntry{
		{Value: "john.doe@example.com"},
		{Value: "competitor.com", Domain: true},
		{Value: "rival.io", Domain: true},
		{Value: "bounced@example.com", Reason: "bounced"},
	}
	if next != "" || !reflect.DeepEqual(page, want) {
		t.Fatalf("Blocklist = %+v, next %q", page, next)
	}
}
