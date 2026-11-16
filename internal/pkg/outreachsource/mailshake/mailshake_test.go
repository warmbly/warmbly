package mailshake

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

const testKey = "ms-test-key"

// fake serves the documented responses from testdata.
type fake struct {
	t       *testing.T
	limited int // answer this many requests with limit_reached first
	mu      sync.Mutex
	posts   int
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if user, pass, ok := r.BasicAuth(); !ok || user != testKey || pass != "" {
		// Mailshake documents the error code, not the HTTP status it comes with.
		w.WriteHeader(http.StatusBadRequest)
		f.write(w, "invalid_key.json")
		return
	}
	f.mu.Lock()
	if f.limited > 0 {
		f.limited--
		f.mu.Unlock()
		w.WriteHeader(http.StatusBadRequest)
		f.write(w, "limit_reached.json")
		return
	}
	f.mu.Unlock()
	q := r.URL.Query()
	switch p := r.URL.Path; {
	case p == "/me":
		f.write(w, "me.json")
	case p == "/senders/list":
		if q.Get("nextToken") == "senders-2" {
			f.write(w, "senders_page2.json")
		} else {
			f.write(w, "senders_page1.json")
		}
	case p == "/campaigns/list":
		f.write(w, "campaigns_list.json")
	case p == "/campaigns/get":
		f.write(w, "campaign_"+q.Get("campaignID")+".json")
	case p == "/recipients/list" && r.Method == http.MethodPost:
		f.mu.Lock()
		f.posts++
		f.mu.Unlock()
		var body struct {
			CampaignID int64  `json:"campaignID"`
			NextToken  string `json:"nextToken"`
			Filter     struct {
				Action            string `json:"action"`
				CampaignMessageID int64  `json:"campaignMessageID"`
			} `json:"filter"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.CampaignID != 1 {
			f.t.Errorf("filter body: %+v %v", body, err)
		}
		switch a := body.Filter.Action; {
		case a == "wasSent" && body.Filter.CampaignMessageID == 1 && body.NextToken == "":
			f.write(w, "filter_sent_1_page1.json")
		case a == "wasSent" && body.Filter.CampaignMessageID == 1:
			f.write(w, "filter_sent_1_page2.json")
		case a == "wasSent" && body.Filter.CampaignMessageID == 2:
			f.write(w, "filter_sent_2.json")
		case a == "replied" || a == "bounced":
			f.write(w, "filter_"+a+".json")
		default:
			f.t.Errorf("unexpected filter %+v", body.Filter)
		}
	case p == "/recipients/list":
		if q.Get("nextToken") == "recipients-2" {
			f.write(w, "recipients_page2.json")
		} else {
			f.write(w, "recipients_page1.json")
		}
	case p == "/activity/replies":
		if q.Get("replyType") != "unsubscribe" || q.Get("perPage") != "25" {
			f.t.Errorf("replies query = %v", q)
		}
		f.write(w, "replies_unsubscribe.json")
	case p == "/leads/list":
		f.write(w, "leads.json")
	default:
		f.t.Errorf("unexpected request %s %s", r.Method, r.URL)
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fake) write(w http.ResponseWriter, name string) {
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		f.t.Fatal(err)
	}
	_, _ = w.Write(b)
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
	s, err := p.Open(context.Background(), outreachsource.Credentials{"api_key": testKey})
	if err != nil {
		t.Fatalf("good key: %v", err)
	}
	acct, _ := s.Account(context.Background())
	if acct.Name != "Jane Doe" || acct.Email != "me@example.com" {
		t.Fatalf("account = %+v", acct)
	}
	_, err = p.Open(context.Background(), outreachsource.Credentials{"api_key": "wrong-key"})
	if !errors.Is(err, outreachsource.ErrKey) {
		t.Fatalf("bad key: got %v, want ErrKey", err)
	}
	if strings.Contains(err.Error(), "wrong-key") {
		t.Fatalf("error leaks the key: %v", err)
	}
	if _, err := p.Open(context.Background(), outreachsource.Credentials{"api_key": " "}); !errors.Is(err, outreachsource.ErrKey) {
		t.Fatalf("blank key: got %v", err)
	}
	info := New().Info()
	if info.ID != "mailshake" || info.Supports(outreachsource.KindBlocklist) || info.Workspaces || info.MailboxSecrets {
		t.Fatalf("info = %+v", info)
	}
	for _, n := range append(info.Notes, info.KeyHelp) {
		if strings.Contains(n, "—") {
			t.Errorf("em dash in %q", n)
		}
	}
}

func TestLimitReachedRetried(t *testing.T) {
	s, waits := open(t, &fake{t: t, limited: 2})
	ws, err := s.Workspaces(context.Background())
	if err != nil || len(ws) != 1 || ws[0].ID != "" || ws[0].Name != "My Team" {
		t.Fatalf("workspaces = %+v %v", ws, err)
	}
	// The retry waits until the time limit_reached names, capped by the client.
	if len(*waits) != 2 || (*waits)[0] != 60*time.Second {
		t.Fatalf("waits = %v", *waits)
	}
}

func TestMailboxes(t *testing.T) {
	s, _ := open(t, &fake{t: t})
	boxes, err := s.Mailboxes(context.Background(), outreachsource.Workspace{})
	if err != nil {
		t.Fatal(err)
	}
	if len(boxes) != 2 {
		t.Fatalf("got %d senders across two pages", len(boxes))
	}
	if b := boxes[0]; b.ID != "abc" || b.Email != "mysender@company.com" || b.FromName != "Firm ABC" || b.Kind != outreachsource.MailboxUnknown || b.HasSecrets() {
		t.Fatalf("sender = %+v", b)
	}
	if boxes[1].Kind != outreachsource.MailboxGoogle {
		t.Fatalf("gmail sender = %+v", boxes[1])
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
	if c.ID != "1" || c.Name != "My campaign" || c.Status != outreachsource.CampaignActive || !c.StopOnReply || c.CreatedAt == nil || len(c.Senders) != 1 || c.Senders[0] != "mysender@company.com" {
		t.Fatalf("campaign = %+v", c)
	}
	if len(c.Steps) != 2 {
		t.Fatalf("steps = %+v", c.Steps)
	}
	if got := c.Steps[0].Variants[0].Subject; got != `Quick question for {{.FirstName}} at {{.Company | default "your team"}}` {
		t.Fatalf("subject = %q", got)
	}
	if f := c.Steps[1]; f.Variants[0].Subject != "" || f.NewThread {
		t.Fatalf("follow-up = %+v", f)
	}
	notes := strings.Join(c.UnmappedNotes, "\n")
	for _, want := range []string{"does not return email bodies", `Drip email "Here's a drip email"`, `On-click email "You clicked my link"`} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes lack %q:\n%s", want, notes)
		}
	}
	// Listed without messages, so read through campaigns/get.
	a := camps[1]
	if a.Status != outreachsource.CampaignArchived || len(a.Steps) != 2 || !a.Steps[1].Variants[0].Disabled || a.Steps[0].Variants[0].Subject != "Hello {{.name}}" {
		t.Fatalf("archived campaign = %+v", a)
	}
}

func TestLeads(t *testing.T) {
	f := &fake{t: t}
	s, _ := open(t, f)
	var leads []outreachsource.Lead
	cursor, pages := "", 0
	for {
		page, next, err := s.Leads(context.Background(), outreachsource.Workspace{}, "1", cursor)
		if err != nil {
			t.Fatal(err)
		}
		pages++
		leads = append(leads, page...)
		if next == "" {
			break
		}
		cursor = next
	}
	if pages != 2 || len(leads) != 8 {
		t.Fatalf("got %d leads in %d pages", len(leads), pages)
	}
	want := map[string]struct {
		status outreachsource.LeadStatus
		steps  int
	}{
		"john@doe.com": {outreachsource.LeadInProgress, 1},
		"jane@doe.com": {outreachsource.LeadNotInterest, 2},
		"sam@doe.com":  {outreachsource.LeadPaused, 1},
		"bob@doe.com":  {outreachsource.LeadBounced, 0},
		"eve@doe.com":  {outreachsource.LeadUnsubscribed, 1},
		"max@doe.com":  {outreachsource.LeadNotStarted, 0},
		"ann@doe.com":  {outreachsource.LeadCompleted, 2},
		"kim@doe.com":  {outreachsource.LeadInterested, 1},
	}
	for _, l := range leads {
		w := want[l.Email]
		if l.Status != w.status || l.StepsSent != w.steps || !l.StepsExact {
			t.Errorf("%s: status %s steps %d exact %v, want %s %d", l.Email, l.Status, l.StepsSent, l.StepsExact, w.status, w.steps)
		}
	}
	john := leads[0]
	if john.FirstName != "John" || john.Company != "J. Doe and Co." || john.Phone != "5555555555" || john.Title != "CEO" {
		t.Fatalf("john = %+v", john)
	}
	if john.Custom["favorite_color"] != "Red" || john.Custom["name"] != "John Doe" || len(john.Custom) != 2 {
		t.Fatalf("custom = %v", john.Custom)
	}
	if leads[5].Custom != nil {
		t.Fatalf("empty custom = %v", leads[5].Custom)
	}
	// Progress is read once per campaign, not once per page.
	if f.posts != 5 {
		t.Fatalf("filtered list calls = %d, want 5", f.posts)
	}
	if page, next, err := s.Leads(context.Background(), outreachsource.Workspace{}, "", ""); page != nil || next != "" || err != nil {
		t.Fatalf("leads outside campaigns = %v %q %v", page, next, err)
	}
}

func TestBlocklistUnsupported(t *testing.T) {
	s, _ := open(t, &fake{t: t})
	_, _, err := s.Blocklist(context.Background(), outreachsource.Workspace{}, "")
	if !errors.Is(err, outreachsource.ErrUnsupported) {
		t.Fatalf("got %v", err)
	}
}

func TestInventory(t *testing.T) {
	s, _ := open(t, &fake{t: t})
	inv, err := s.Inventory(context.Background(), outreachsource.Workspace{})
	if err != nil {
		t.Fatal(err)
	}
	if inv.Mailboxes != 2 || inv.Campaigns != 2 || inv.Leads != -1 || inv.Blocklist != -1 || len(inv.Summaries) != 2 {
		t.Fatalf("inventory = %+v", inv)
	}
	if sm := inv.Summaries[0]; sm.Steps != 2 || sm.Leads != -1 || sm.ID != "1" {
		t.Fatalf("summary = %s", fmt.Sprint(sm))
	}
}
