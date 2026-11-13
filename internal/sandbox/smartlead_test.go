package sandbox

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	source "github.com/warmbly/warmbly/internal/pkg/outreachsource"
	"github.com/warmbly/warmbly/internal/pkg/outreachsource/smartlead"
)

// TestSmartleadMockReadsThroughAdapter proves the mock and the real adapter agree on every shape.
func TestSmartleadMockReadsThroughAdapter(t *testing.T) {
	srv := httptest.NewServer(NewVendorMock(Config{}))
	t.Cleanup(srv.Close)
	ctx := context.Background()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/smartlead/campaigns/", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no api_key: HTTP %d, want 401", resp.StatusCode)
	}
	sess, err := smartlead.NewWithBaseURL(srv.URL+"/smartlead").Open(ctx, source.Credentials{"api_key": "sandbox-key"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := sess.Account(ctx); err != nil {
		t.Fatalf("Account: %v", err)
	}
	wss, err := sess.Workspaces(ctx)
	if err != nil {
		t.Fatalf("Workspaces: %v", err)
	}
	var names []string
	for _, ws := range wss {
		names = append(names, ws.Name)
	}
	if got := strings.Join(names, ", "); got != "Northwind Studio, Brightline Legal, Not assigned to a client" {
		t.Fatalf("workspaces = %s", got)
	}

	type want struct{ mailboxes, campaigns, leads, blocklist int }
	wants := []want{{3, 2, 28, 4}, {2, 1, 18, 4}, {1, 1, 12, 2}}
	statuses := map[source.LeadStatus]int{}
	for i, ws := range wss {
		w := wants[i]
		inv, err := sess.Inventory(ctx, ws)
		if err != nil {
			t.Fatalf("%s: Inventory: %v", ws.Name, err)
		}
		if inv.Mailboxes != w.mailboxes || inv.Campaigns != w.campaigns || inv.Leads != w.leads {
			t.Errorf("%s: inventory = %+v, want %+v", ws.Name, inv, w)
		}

		boxes, err := sess.Mailboxes(ctx, ws)
		if err != nil {
			t.Fatalf("%s: Mailboxes: %v", ws.Name, err)
		}
		for _, b := range boxes {
			if b.Kind != source.MailboxGoogle && b.Kind != source.MailboxMicrosoft {
				t.Errorf("%s: mailbox %s is %s", ws.Name, b.Email, b.Kind)
			}
			if b.Email == "" || b.DailyCap == 0 || b.Warmup == nil {
				t.Errorf("%s: mailbox %+v is missing fields", ws.Name, b)
			}
		}

		var blocks []source.BlockEntry
		for cursor := ""; ; {
			page, next, err := sess.Blocklist(ctx, ws, cursor)
			if err != nil {
				t.Fatalf("%s: Blocklist: %v", ws.Name, err)
			}
			blocks = append(blocks, page...)
			if next == "" {
				break
			}
			cursor = next
		}
		if len(blocks) != w.blocklist {
			t.Errorf("%s: %d block entries, want %d: %+v", ws.Name, len(blocks), w.blocklist, blocks)
		}

		camps, err := sess.Campaigns(ctx, ws)
		if err != nil {
			t.Fatalf("%s: Campaigns: %v", ws.Name, err)
		}
		if len(camps) != w.campaigns {
			t.Fatalf("%s: %d campaigns, want %d", ws.Name, len(camps), w.campaigns)
		}
		for _, c := range camps {
			if len(c.Steps) < 2 || len(c.Senders) == 0 || c.Schedule == nil || len(c.Schedule.Days) == 0 || c.Schedule.From == "" {
				t.Errorf("%s: campaign %q incomplete: %+v", ws.Name, c.Name, c)
			}
			for _, st := range c.Steps {
				for _, v := range st.Variants {
					if strings.Contains(v.Body, "%") || strings.Contains(v.Body, "{{first_name") || strings.Contains(v.Body, "{{#") {
						t.Errorf("%s: %q has an untranslated token: %s", ws.Name, c.Name, v.Body)
					}
				}
			}
			n := 0
			for cursor := ""; ; {
				page, next, err := sess.Leads(ctx, ws, c.ID, cursor)
				if err != nil {
					t.Fatalf("%s: Leads(%s): %v", ws.Name, c.Name, err)
				}
				for _, l := range page {
					statuses[l.Status]++
					if !l.StepsExact || l.Company == "" || l.Title == "" || l.Custom["industry"] == "" {
						t.Errorf("%s: lead %+v", c.Name, l)
					}
				}
				n += len(page)
				if next == "" {
					break
				}
				cursor = next
			}
			if n != c.LeadCount {
				t.Errorf("%s: read %d leads, campaign says %d", c.Name, n, c.LeadCount)
			}
		}

		if ws.Name == "Northwind Studio" {
			first := camps[0]
			if first.Name != "Q4 brand refresh outreach" || len(first.Steps) != 3 || len(first.Steps[0].Variants) != 2 {
				t.Fatalf("Northwind's first campaign = %+v", first)
			}
			body := first.Steps[0].Variants[0].Body
			for _, s := range []string{`{Hi|Hey} {{.FirstName | default "there"}}`, "{{.Company}}", "{{.industry}}", "{{.Sender.FirstName}}", "{{unsubscribe_link}}"} {
				if !strings.Contains(body, s) {
					t.Errorf("step 1 body lacks %s: %s", s, body)
				}
			}
			if got := first.Steps[1].Variants[0].Body; !strings.Contains(got, "{{if gef .team_size 50}}") || !strings.Contains(got, "{{else}}") {
				t.Errorf("step 2 condition = %s", got)
			}
			if first.Steps[1].NewThread || !first.Steps[2].NewThread {
				t.Errorf("threading = %v, %v", first.Steps[1].NewThread, first.Steps[2].NewThread)
			}
			if first.Status != source.CampaignActive || first.TrackClicks || !first.TrackOpens || !first.Unsubscribe {
				t.Errorf("settings = %+v", first)
			}
		}
	}
	for _, st := range []source.LeadStatus{source.LeadNotStarted, source.LeadInProgress, source.LeadCompleted, source.LeadReplied,
		source.LeadInterested, source.LeadMeeting, source.LeadNotInterest, source.LeadBounced, source.LeadUnsubscribed} {
		if statuses[st] == 0 {
			t.Errorf("no lead is %s: %v", st, statuses)
		}
	}
	t.Logf("lead statuses: %v", statuses)
}
