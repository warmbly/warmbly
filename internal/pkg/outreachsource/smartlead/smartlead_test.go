package smartlead

import (
	"context"
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

	source "github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

const goodKey = "sl-test-key"

type fake struct {
	t  *testing.T
	mu sync.Mutex
	// hits counts requests per path.
	hits map[string]int
	// status answers a path with a bare status the first n times.
	status map[string][]int
	// body answers a path with this JSON instead of its fixture.
	body map[string]string
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		f.t.Errorf("%s %s: only reads are allowed", r.Method, r.URL.Path)
	}
	f.mu.Lock()
	f.hits[r.URL.Path]++
	var code int
	if q := f.status[r.URL.Path]; len(q) > 0 {
		code, f.status[r.URL.Path] = q[0], q[1:]
	}
	override, hasOverride := f.body[r.URL.Path]
	f.mu.Unlock()
	if r.URL.Query().Get("api_key") != goodKey {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Invalid API Key"}`))
		return
	}
	if code != 0 {
		w.WriteHeader(code)
		_, _ = w.Write([]byte(`{"message":"nope"}`))
		return
	}
	if hasOverride {
		_, _ = w.Write([]byte(override))
		return
	}
	off := r.URL.Query().Get("offset")
	file := ""
	switch p := r.URL.Path; {
	case p == "/client/":
		file = "clients.json"
	case p == "/campaigns/":
		file = "campaigns.json"
	case p == "/leads/fetch-categories":
		file = "categories.json"
	case p == "/campaigns/101/sequences":
		file = "sequences_101.json"
	case p == "/campaigns/101/email-accounts":
		file = "campaign_email_accounts_101.json"
	case p == "/campaigns/101/leads" && (off == "" || off == "0"):
		file = "leads_101_p1.json"
	case p == "/campaigns/101/leads" && off == "2":
		file = "leads_101_p2.json"
	case p == "/campaigns/101/statistics" && off == "0":
		file = "statistics_101.json"
	case strings.HasSuffix(p, "/leads"):
		_, _ = w.Write([]byte(`{"total_leads":"0","offset":0,"limit":100,"data":[]}`))
		return
	case strings.HasSuffix(p, "/statistics"), strings.HasSuffix(p, "/sequences"), strings.HasSuffix(p, "/email-accounts"):
		_, _ = w.Write([]byte(`{"ok":true,"data":[]}`))
		return
	case p == "/email-accounts/" && off == "0":
		file = "email_accounts.json"
	case p == "/email-accounts/501/":
		file = "email_account_501.json"
	case p == "/leads/get-domain-block-list" && off == "0":
		file = "block_list.json"
	case p == "/email-accounts/", p == "/leads/get-domain-block-list":
		_, _ = w.Write([]byte(`[]`))
		return
	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"Resource not found"}`))
		return
	}
	b, err := os.ReadFile(filepath.Join("testdata", file))
	if err != nil {
		f.t.Fatal(err)
	}
	_, _ = w.Write(b)
}

func setup(t *testing.T) (*fake, *provider) {
	t.Helper()
	f := &fake{t: t, hits: map[string]int{}, status: map[string][]int{}, body: map[string]string{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	p := newProvider(srv.URL)
	p.sleep = func(context.Context, time.Duration) error { return nil }
	return f, p
}

func open(t *testing.T, p *provider) source.Session {
	t.Helper()
	s, err := p.Open(context.Background(), source.Credentials{"api_key": goodKey})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s
}

var (
	acme       = source.Workspace{ID: "301", Name: "Acme Agency"}
	unassigned = source.Workspace{ID: "", Name: unassignedName}
)

func TestInfo(t *testing.T) {
	info := New().Info()
	if info.ID != "smartlead" || !info.Workspaces || info.MultiKey || !info.MailboxSecrets {
		t.Fatalf("info = %+v", info)
	}
	for _, k := range source.AllKinds {
		if !info.Supports(k) {
			t.Errorf("kind %s not supported", k)
		}
	}
	for _, s := range append([]string{info.KeyHelp, info.Fields[0].Help}, info.Notes...) {
		if strings.Contains(s, "—") {
			t.Errorf("em dash in %q", s)
		}
	}
}

func TestOpen(t *testing.T) {
	_, p := setup(t)
	open(t, p)
	_, err := p.Open(context.Background(), source.Credentials{"api_key": "wrong"})
	if !errors.Is(err, source.ErrKey) {
		t.Fatalf("bad key: %v", err)
	}
	if strings.Contains(err.Error(), "wrong") {
		t.Fatalf("error leaks the key: %v", err)
	}
	if _, err := p.Open(context.Background(), source.Credentials{}); !errors.Is(err, source.ErrKey) {
		t.Fatalf("empty key: %v", err)
	}
}

func TestRateLimitRetried(t *testing.T) {
	f, p := setup(t)
	f.status["/leads/fetch-categories"] = []int{http.StatusTooManyRequests, http.StatusTooManyRequests}
	open(t, p)
	if n := f.hits["/leads/fetch-categories"]; n != 3 {
		t.Fatalf("hits = %d, want 3", n)
	}
}

func TestWorkspaces(t *testing.T) {
	_, p := setup(t)
	got, err := open(t, p).Workspaces(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []source.Workspace{acme, {ID: "302", Name: "Beta Corp"}, unassigned}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("workspaces = %+v", got)
	}
}

func TestWorkspacesClientKey(t *testing.T) {
	f, p := setup(t)
	f.status["/client/"] = []int{http.StatusForbidden}
	s := open(t, p)
	ws, err := s.Workspaces(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ws) != 1 || ws[0].ID != "" {
		t.Fatalf("workspaces = %+v", ws)
	}
	// Nothing is filtered by client when the key sees no clients.
	camps, err := s.Campaigns(context.Background(), ws[0])
	if err != nil || len(camps) != 3 {
		t.Fatalf("campaigns = %d, %v", len(camps), err)
	}
}

func TestInventory(t *testing.T) {
	_, p := setup(t)
	inv, err := open(t, p).Inventory(context.Background(), acme)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Mailboxes != 2 || inv.Campaigns != 1 || inv.Leads != 3 || inv.Blocklist != -1 {
		t.Fatalf("inventory = %+v", inv)
	}
	want := source.CampaignSummary{ID: "101", Name: "Cold Outreach Q1", Status: source.CampaignActive, Steps: -1, Leads: 3}
	if len(inv.Summaries) != 1 || inv.Summaries[0] != want {
		t.Fatalf("summaries = %+v", inv.Summaries)
	}
}

func TestCampaigns(t *testing.T) {
	f, p := setup(t)
	s := open(t, p)
	camps, err := s.Campaigns(context.Background(), acme)
	if err != nil {
		t.Fatal(err)
	}
	if len(camps) != 1 {
		t.Fatalf("campaigns = %d", len(camps))
	}
	c := camps[0]
	if c.ID != "101" || c.MaxNewLeadsPerDay != 50 || c.Status != source.CampaignActive || !c.StopOnReply || !c.TrackOpens || c.TrackClicks || c.PlainText || !c.Unsubscribe || c.LeadCount != 3 {
		t.Fatalf("campaign = %+v", c)
	}
	if want := []string{"sarah@acme-mail.com", "david@acme.com"}; !reflect.DeepEqual(c.Senders, want) {
		t.Errorf("senders = %v", c.Senders)
	}
	start := time.Date(2025, 12, 1, 9, 0, 0, 0, time.UTC)
	wantSched := &source.Schedule{Timezone: "America/New_York", Days: []int{1, 2, 3, 4, 5, 7}, From: "09:00", To: "17:00", MinGapMinutes: 12, Start: &start}
	if !reflect.DeepEqual(c.Schedule, wantSched) {
		t.Errorf("schedule = %+v", c.Schedule)
	}
	if len(c.Steps) != 3 {
		t.Fatalf("steps = %+v", c.Steps)
	}
	one, two, three := c.Steps[0], c.Steps[1], c.Steps[2]
	if one.WaitDays != 0 || two.WaitDays != 3 || three.WaitDays != 5 {
		t.Errorf("waits = %d %d %d", one.WaitDays, two.WaitDays, three.WaitDays)
	}
	if one.NewThread || two.NewThread || !three.NewThread {
		t.Errorf("new thread = %v %v %v", one.NewThread, two.NewThread, three.NewThread)
	}
	if len(one.Variants) != 2 {
		t.Fatalf("variants = %+v", one.Variants)
	}
	a := one.Variants[0]
	if a.Subject != "Quick question about {{.Company}}" || !a.HTML ||
		a.Body != "<p>Hi {{.FirstName}},</p><p>I saw {{.company_size}} at {{.Company}}.</p><p>{{.Sender.Name}}<br>{{unsubscribe_link}}</p>" {
		t.Errorf("variant A = %+v", a)
	}
	if b := one.Variants[1]; b.Body != "{Hi|Hey} {{.FirstName}}, short one." || b.HTML {
		t.Errorf("variant B = %+v", b)
	}
	wantTwo := `<p>{{if .FirstName}}Hey {{.FirstName}},{{else}}Hey there,{{end}}</p><p>Good . {{if gef .employees 50}}Big team!{{end}}</p><p>{{.Sender.FirstName}}</p>`
	if two.Variants[0].Subject != "" || two.Variants[0].Body != wantTwo {
		t.Errorf("step 2 = %q", two.Variants[0].Body)
	}
	if three.Variants[0].HTML {
		t.Error("plain step read as HTML")
	}
	notes := strings.Join(c.UnmappedNotes, "\n")
	for _, want := range []string{"linkedin step", "%signature%", "{{sl_time_of_day}}"} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes miss %q:\n%s", want, notes)
		}
	}
	// Leads are counted once per campaign however often they are asked for.
	if _, err := s.Inventory(context.Background(), acme); err != nil {
		t.Fatal(err)
	}
	if n := f.hits["/campaigns/101/leads"]; n != 1 {
		t.Errorf("lead count requests = %d", n)
	}
}

func TestSubsequenceCampaign(t *testing.T) {
	_, p := setup(t)
	camps, err := open(t, p).Campaigns(context.Background(), unassigned)
	if err != nil {
		t.Fatal(err)
	}
	if len(camps) != 1 {
		t.Fatalf("campaigns = %+v", camps)
	}
	c := camps[0]
	if c.Status != source.CampaignDraft || c.MaxNewLeadsPerDay != 20 || c.TrackOpens || c.TrackClicks || !c.Unsubscribe || c.Schedule != nil {
		t.Errorf("campaign = %+v", c)
	}
	notes := strings.Join(c.UnmappedNotes, "\n")
	for _, want := range []string{`"Cold Outreach Q1"`, "opened an email", "40% of leads"} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes miss %q:\n%s", want, notes)
		}
	}
}

func TestLeads(t *testing.T) {
	_, p := setup(t)
	s := open(t, p)
	ctx := context.Background()
	page, next, err := s.Leads(ctx, acme, "101", "")
	if err != nil {
		t.Fatal(err)
	}
	if next != "2" || len(page) != 2 {
		t.Fatalf("page 1 = %d leads, next %q", len(page), next)
	}
	john, jane := page[0], page[1]
	sent := time.Date(2025, 12, 4, 9, 12, 0, 0, time.UTC)
	if john.Email != "john@example.com" || john.Status != source.LeadInterested || john.StepsSent != 2 || !john.StepsExact ||
		john.LastSent == nil || !john.LastSent.Equal(sent) || john.Phone != "+1234567890" || john.LinkedIn == "" {
		t.Errorf("john = %+v", john)
	}
	if john.Title != "CEO" || john.Website != "https://acme.com" || john.Location != "San Francisco, CA" {
		t.Errorf("john fields = %+v", john)
	}
	if want := map[string]string{"company_size": "50-100", "employees": "75"}; !reflect.DeepEqual(john.Custom, want) {
		t.Errorf("john custom = %v", john.Custom)
	}
	// An out-of-office category says nothing; the bounce decides.
	if jane.Status != source.LeadBounced || jane.StepsSent != 1 || !jane.StepsExact || jane.Website != "https://roe.example.org" || jane.Custom != nil {
		t.Errorf("jane = %+v", jane)
	}
	page, next, err = s.Leads(ctx, acme, "101", next)
	if err != nil {
		t.Fatal(err)
	}
	if next != "" || len(page) != 1 {
		t.Fatalf("page 2 = %d leads, next %q", len(page), next)
	}
	if max := page[0]; max.Status != source.LeadUnsubscribed || max.StepsSent != 0 || !max.StepsExact || max.Custom != nil {
		t.Errorf("max = %+v", max)
	}
	if _, _, err := s.Leads(ctx, acme, "", ""); !errors.Is(err, source.ErrUnsupported) {
		t.Errorf("leads outside a campaign: %v", err)
	}
}

func TestLeadStepsNotExact(t *testing.T) {
	cases := map[string]string{
		// The aggregate shape names no lead, so no count can be trusted.
		"aggregate": `{"success":true,"data":{"campaign_id":101,"total_leads":3,"contacted":2}}`,
		// Jane is in progress but missing from the rows.
		"missing lead": `{"total_stats":"1","data":[{"lead_email":"john@example.com","sequence_number":2,"sent_time":"2025-12-04T09:12:00Z"}]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			f, p := setup(t)
			f.body["/campaigns/101/statistics"] = body
			page, _, err := open(t, p).Leads(context.Background(), acme, "101", "")
			if err != nil {
				t.Fatal(err)
			}
			if jane := page[1]; jane.StepsExact {
				t.Errorf("jane = %+v", jane)
			}
		})
	}
}

func TestLeadStatusFromCategory(t *testing.T) {
	for name, want := range map[string]source.LeadStatus{
		"Interested": source.LeadInterested, "Meeting Request": source.LeadMeeting, "Not Interested": source.LeadNotInterest,
		"Do Not Contact": source.LeadUnsubscribed, "Information Request": source.LeadReplied,
	} {
		if got, ok := categoryStatus(category{Name: name}); !ok || got != want {
			t.Errorf("%s = %q", name, got)
		}
	}
	if _, ok := categoryStatus(category{Name: "Out Of Office"}); ok {
		t.Error("out of office mapped")
	}
}

func TestBlocklist(t *testing.T) {
	_, p := setup(t)
	s := open(t, p)
	got, next, err := s.Blocklist(context.Background(), acme, "")
	if err != nil || next != "" {
		t.Fatalf("next %q, %v", next, err)
	}
	want := []source.BlockEntry{
		{Value: "competitor.com", Domain: true, Reason: "Manual"},
		{Value: "spam@example.com", Reason: "Smartlead.ai Bounce Detection"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("acme = %+v", got)
	}
	got, _, err = s.Blocklist(context.Background(), unassigned, "")
	if err != nil || len(got) != 1 || got[0].Value != "competitor.com" {
		t.Fatalf("unassigned = %+v, %v", got, err)
	}
}

func TestMailboxes(t *testing.T) {
	f, p := setup(t)
	s := open(t, p)
	got, err := s.Mailboxes(context.Background(), acme)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("mailboxes = %+v", got)
	}
	smtp, gmail := got[0], got[1]
	if !smtp.HasSecrets() || smtp.SMTP.Password != "smtp-secret" || smtp.IMAP.Password != "smtp-secret" ||
		smtp.SMTP.Security != source.SecurityStartTLS || smtp.IMAP.Security != source.SecuritySSL ||
		smtp.IMAP.Username != "sarah@acme-mail.com" || smtp.Email != "sarah@acme-mail.com" ||
		smtp.DailyCap != 40 || smtp.ReplyTo != "replies@acme.com" || smtp.Warmup == nil || !*smtp.Warmup ||
		!reflect.DeepEqual(smtp.Tags, []string{"Winners"}) {
		t.Errorf("smtp = %+v smtp=%+v imap=%+v", smtp, smtp.SMTP, smtp.IMAP)
	}
	if gmail.Kind != source.MailboxGoogle || gmail.SMTP != nil || gmail.Warmup != nil || gmail.HasSecrets() {
		t.Errorf("gmail = %+v", gmail)
	}
	if f.hits["/email-accounts/502/"] != 0 {
		t.Error("read secrets of an OAuth mailbox")
	}
	// A mailbox whose detail cannot be read comes over without secrets.
	got, err = s.Mailboxes(context.Background(), unassigned)
	if err != nil || len(got) != 1 {
		t.Fatalf("unassigned = %+v, %v", got, err)
	}
	if ops := got[0]; ops.HasSecrets() || ops.SMTP.Password != "" || ops.SMTP.Security != source.SecuritySSL || *ops.Warmup {
		t.Errorf("ops = %+v", ops)
	}
	if f.hits["/email-accounts/"] != 1 {
		t.Errorf("account list requests = %d", f.hits["/email-accounts/"])
	}
}

func TestTranslate(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Hi {{first_name}}", "Hi {{.FirstName}}"},
		{"{{last_name}} at {{company_name}}", "{{.LastName}} at {{.Company}}"},
		{"{{#if position '==' \"founder\"}}Founder{{/if}}", `{{if eq (trim .position) "founder"}}Founder{{end}}`},
		{"{{#if reviews '<' 15}}few{{else}}many{{/if}}", "{{if ltf .reviews 15}}few{{else}}many{{end}}"},
		{"{{#unless first_name}}Hi{{/unless}}", "{{if not (.FirstName)}}Hi{{end}}"},
		{"By %Sender-Name% (%sender-mailbox%)", "By {{.Sender.Name}} ({{.Sender.Email}})"},
		{`{{sl_date "2 days from now" "MMMM Do YYYY"}} ok`, " ok"},
		{"{Hi|Hey} {{first_name|there}}", `{Hi|Hey} {{.FirstName | default "there"}}`},
		{"{{linkedin_profile}} {{company_url}} {{location}} {{phone_number}}", "{{.linkedin}} {{.website}} {{.location}} {{.Phone}}"},
	}
	for _, c := range cases {
		if got := translate(c.in).Text; got != c.want {
			t.Errorf("translate(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	tr := translate("{{#if a 'like' b}}x{{/if}} {{#if position '==' 'ceo'}}y{{/if}}")
	if len(tr.Kept) != 1 || !reflect.DeepEqual(tr.Fields, []string{"position"}) {
		t.Errorf("kept %v fields %v", tr.Kept, tr.Fields)
	}
}
