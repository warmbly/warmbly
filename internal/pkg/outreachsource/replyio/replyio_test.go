package replyio

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	source "github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

const goodKey = "rp-test-key"

var fixtures = map[string]string{
	"/v3/whoami":                            "whoami.json",
	"/v3/sequences":                         "sequences.json",
	"/v3/sequences/123":                     "sequence_123.json",
	"/v3/sequences/124":                     "sequence_124.json",
	"/v3/sequences/123/steps/1323/variants": "variants_1323.json",
	"/v3/sequences/123/contacts/count":      "count_123.json",
	"/v3/schedules/7":                       "schedule_7.json",
	"/v3/contacts":                          "contacts.json",
	"/v3/email-accounts":                    "email_accounts.json",
	"/v3/email-accounts/1":                  "email_account_1.json",
	"/v3/email-accounts/2":                  "email_account_2.json",
	"/v3/contact-blacklist-rules/domains":   "blacklist_domains.json",
	"/v3/contact-blacklist-rules/emails":    "blacklist_emails.json",
	"/v3/sequences/123/contacts?skip=0":     "seqcontacts_123_p1.json",
	"/v3/sequences/123/contacts?skip=more":  "seqcontacts_123_p2.json",
	"/v3/sequences/124/contacts?skip=0":     "",
	"/v3/sequences/124/contacts?skip=more":  "",
	"/v3/sequences/124/contacts/count":      "404",
	"/v3/email-accounts/3":                  "404",
}

type fake struct {
	t        *testing.T
	throttle atomic.Int32
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		f.t.Errorf("non-GET request %s %s", r.Method, r.URL.Path)
	}
	if r.Header.Get("Authorization") != "Bearer "+goodKey {
		w.Header().Set("WWW-Authenticate", "Bearer")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if f.throttle.Load() > 0 {
		f.throttle.Add(-1)
		w.Header().Set("Retry-After", "30")
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"title":"Too Many Requests","status":429,"detail":"API calls quota exceeded! maximum admitted 100 per 1m."}`))
		return
	}
	key := r.URL.Path
	if strings.HasSuffix(key, "/contacts") && strings.HasPrefix(key, "/v3/sequences/") {
		if r.URL.Query().Get("skip") == "0" {
			key += "?skip=0"
		} else {
			key += "?skip=more"
		}
	}
	name, ok := fixtures[key]
	switch {
	case !ok:
		f.t.Errorf("unexpected request %s", r.URL)
		w.WriteHeader(http.StatusNotFound)
		return
	case name == "404":
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"title":"Not Found","status":404,"code":"resource.notFound","detail":"Not found."}`))
		return
	case name == "":
		_, _ = w.Write([]byte(`{"hasMore":false,"items":[]}`))
		return
	}
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		f.t.Fatal(err)
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
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
	if _, err := newProvider(srv.URL).Open(context.Background(), source.Credentials{"api_key": "nope"}); !errors.Is(err, source.ErrKey) {
		t.Fatalf("bad key: got %v, want ErrKey", err)
	}
	s, _ := open(t)
	acc, err := s.Account(context.Background())
	if err != nil || acc.Name != "jane@acme.io" || acc.Email != "jane@acme.io" {
		t.Fatalf("Account = %+v, %v", acc, err)
	}
	ws, err := s.Workspaces(context.Background())
	if err != nil || len(ws) != 1 || ws[0].ID != "" {
		t.Fatalf("Workspaces = %+v, %v", ws, err)
	}
}

func TestInfo(t *testing.T) {
	info := New().Info()
	if info.ID != "replyio" || info.Label != "Reply.io" || info.Workspaces || !info.MultiKey || info.MailboxSecrets || len(info.Kinds) != 4 {
		t.Fatalf("Info = %+v", info)
	}
	for _, s := range append(info.Notes, info.KeyHelp, info.Fields[0].Help) {
		if strings.Contains(s, "—") {
			t.Errorf("em dash in %q", s)
		}
	}
}

func TestRateLimitRetried(t *testing.T) {
	s, f := open(t)
	f.throttle.Store(3)
	inv, err := s.Inventory(context.Background(), source.Workspace{})
	if err != nil {
		t.Fatalf("Inventory after 429: %v", err)
	}
	if inv.Campaigns != 2 || inv.Mailboxes != 3 || inv.Leads != -1 || inv.Blocklist != -1 {
		t.Fatalf("Inventory = %+v", inv)
	}
	if inv.Summaries[0].Status != source.CampaignActive || inv.Summaries[1].Status != source.CampaignArchived {
		t.Fatalf("summaries = %+v", inv.Summaries)
	}
}

func TestMailboxes(t *testing.T) {
	s, _ := open(t)
	mbs, err := s.Mailboxes(context.Background(), source.Workspace{})
	if err != nil {
		t.Fatal(err)
	}
	if len(mbs) != 3 {
		t.Fatalf("got %d mailboxes", len(mbs))
	}
	g, c, m := mbs[0], mbs[1], mbs[2]
	if g.Email != "sales@acme.io" || g.Kind != source.MailboxGoogle || g.DailyCap != 45 || g.Signature != "Best regards,\nJane" || g.SMTP != nil {
		t.Errorf("gmail = %+v", g)
	}
	wantSMTP := &source.Server{Host: "smtp.acme.io", Port: 587, Security: source.SecurityStartTLS}
	wantIMAP := &source.Server{Host: "imap.acme.io", Port: 993, Security: source.SecuritySSL}
	if c.Kind != source.MailboxSMTP || !reflect.DeepEqual(c.SMTP, wantSMTP) || !reflect.DeepEqual(c.IMAP, wantIMAP) || c.HasSecrets() {
		t.Errorf("custom = %+v smtp %+v imap %+v", c, c.SMTP, c.IMAP)
	}
	if m.Kind != source.MailboxMicrosoft || m.DailyCap != 20 {
		t.Errorf("exchange = %+v", m)
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
	if c.ID != "123" || c.Status != source.CampaignActive || c.DailyLimit != 50 || c.MaxNewLeadsPerDay != 30 || !c.StopOnReply ||
		c.TrackOpens || !c.TrackClicks || !c.Unsubscribe || c.PlainText || c.LeadCount != 5 {
		t.Errorf("settings = %+v", c)
	}
	if !reflect.DeepEqual(c.Senders, []string{"sales@acme.io", "outreach@acme.io"}) {
		t.Errorf("senders = %v", c.Senders)
	}
	want := &source.Schedule{Timezone: "America/New_York", Days: []int{1, 3, 5}, From: "08:30", To: "17:00", MinGapMinutes: 2}
	if !reflect.DeepEqual(c.Schedule, want) {
		t.Errorf("schedule = %+v", c.Schedule)
	}
	if len(c.Steps) != 2 {
		t.Fatalf("steps = %+v", c.Steps)
	}
	first := c.Steps[0]
	if first.WaitDays != 0 || first.WaitMinutes != 0 || len(first.Variants) != 2 {
		t.Fatalf("step 1 = %+v", first)
	}
	b, a := first.Variants[0], first.Variants[1]
	if b.Disabled || b.Subject != "{{.FirstName}}, an idea" || !b.HTML {
		t.Errorf("enabled variant first: %+v", b)
	}
	if !a.Disabled || a.Body != `<p>{Hi|Hello} {{.FirstName | default "there"}},</p><p>{{.lead_source}}</p><p>{{.Sender.FirstName}}</p>` {
		t.Errorf("disabled variant: %+v", a)
	}
	second := c.Steps[1]
	if second.WaitDays != 5 || second.WaitMinutes != 60 {
		t.Errorf("step 2 wait = %dd %dm, want the LinkedIn step's delay carried", second.WaitDays, second.WaitMinutes)
	}
	if v := second.Variants[0]; v.Subject != "" || v.Body != `<p>Following up, {{.FirstName | default "friend"}}. {{.title}} at {{.linkedin}}</p>` {
		t.Errorf("step 2 variant = %+v", v)
	}
	notes := strings.Join(c.UnmappedNotes, "\n")
	for _, w := range []string{"LinkedIn connect", "manual", "condition", "branches were not brought over (1)", "different hours", "own time zone"} {
		if !strings.Contains(notes, w) {
			t.Errorf("notes missing %q:\n%s", w, notes)
		}
	}
	old := camps[1]
	if old.Status != source.CampaignArchived || old.StopOnReply || !old.PlainText || old.Steps[0].Variants[0].HTML || old.Schedule != nil || old.LeadCount != -1 {
		t.Errorf("old = %+v", old)
	}
}

func TestCampaignLeadsPaged(t *testing.T) {
	s, _ := open(t)
	first, next, err := s.Leads(context.Background(), source.Workspace{}, "123", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || next != "2" {
		t.Fatalf("first page: %d leads, next %q", len(first), next)
	}
	john, amy := first[0], first[1]
	if john.Status != source.LeadInProgress || john.StepsSent != 1 || john.StepsExact {
		t.Errorf("john status %s steps %d exact %v", john.Status, john.StepsSent, john.StepsExact)
	}
	if john.Location != "San Francisco, CA, United States" || john.Timezone != "America/Los_Angeles" || john.Phone == "" || john.LinkedIn == "" || john.Title != "Senior Product Manager" {
		t.Errorf("john = %+v", john)
	}
	wantCustom := map[string]string{"industry": "Software & Technology", "company_size": "501-1000", "lead_source": "Conference", "budget": "100k-250k"}
	if !reflect.DeepEqual(john.Custom, wantCustom) {
		t.Errorf("john custom = %v", john.Custom)
	}
	if amy.Status != source.LeadInterested || amy.StepsSent != 2 || amy.Timezone != "Europe/London" {
		t.Errorf("amy = %+v", amy)
	}
	second, next, err := s.Leads(context.Background(), source.Workspace{}, "123", next)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 3 || next != "" {
		t.Fatalf("second page: %d leads, next %q", len(second), next)
	}
	want := []source.LeadStatus{source.LeadNotStarted, source.LeadBounced, source.LeadUnsubscribed}
	for i, l := range second {
		if l.Status != want[i] {
			t.Errorf("%s status = %s, want %s", l.Email, l.Status, want[i])
		}
	}
	if second[0].Email != "new@gamma.dev" || second[0].StepsSent != 0 {
		t.Errorf("contact missing from the index = %+v", second[0])
	}
}

func TestLooseLeads(t *testing.T) {
	s, _ := open(t)
	page, next, err := s.Leads(context.Background(), source.Workspace{}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if next != "" || len(page) != 1 {
		t.Fatalf("loose = %+v, next %q", page, next)
	}
	l := page[0]
	if l.Email != "loose@free.io" || l.Status != source.LeadMeeting || !reflect.DeepEqual(l.Custom, map[string]string{"deal_size": "12"}) {
		t.Errorf("loose lead = %+v", l)
	}
}

func TestBlocklist(t *testing.T) {
	s, _ := open(t)
	var got []source.BlockEntry
	cursor := ""
	for i := 0; i < 5; i++ {
		page, next, err := s.Blocklist(context.Background(), source.Workspace{}, cursor)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, page...)
		if next == "" {
			break
		}
		cursor = next
	}
	want := []source.BlockEntry{
		{Value: "competitor.com", Domain: true},
		{Value: "rival.io", Domain: true},
		{Value: "ceo@example.com"},
		{Value: "spam.net", Domain: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Blocklist = %+v", got)
	}
}

func TestTranslateQuotes(t *testing.T) {
	cases := map[string]string{
		"{{FirstName | ‘there’}}":                    `{{.FirstName | default "there"}}`,
		"{{Random | \"Hi\" | 'Hey'}}":                "{Hi|Hey}",
		"{{SendingEmail.SenderName}}":                "{{.Sender.Name}}",
		"<p>{{Company | &quot;your team&quot;}}</p>": `<p>{{.Company | default "your team"}}</p>`,
	}
	for in, want := range cases {
		if got, _ := translate(in); got != want {
			t.Errorf("translate(%q) = %q, want %q", in, got, want)
		}
	}
}
