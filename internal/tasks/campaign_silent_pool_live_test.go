package tasks

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// A campaign that is ACTIVE and sending nothing has to say why in its own
// activity feed, because that feed is where an owner looks when the dashboard
// shows a running campaign and the inbox shows no mail.
//
// One reason used to go unreported. The empty-pool branch logs "every mailbox
// has used its daily budget" only when NO mailbox was also outside its own
// 8am-8pm band — the band is routine and short and deliberately earns no line
// of its own. But one unrelated mailbox in another timezone was enough to
// suppress the budget line too, so a pool that had genuinely finished for the
// day went quiet with an empty feed.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/tasks/ -run LiveSilent -v

// closedHoursZone finds a timezone where it is currently outside the 8am-8pm
// band the scheduler holds a mailbox to, so the test does not depend on the
// hour it happens to run at. It skips rather than guesses if none matches.
func closedHoursZone(t *testing.T) string {
	t.Helper()
	for _, name := range []string{
		"Pacific/Auckland", "Australia/Brisbane", "Asia/Tokyo", "Asia/Kolkata",
		"Europe/London", "America/New_York", "America/Los_Angeles", "Pacific/Honolulu",
	} {
		loc, err := time.LoadLocation(name)
		if err != nil {
			continue
		}
		if h := time.Now().In(loc).Hour(); h < 8 || h >= 20 {
			return name
		}
	}
	t.Skip("no candidate timezone is currently outside 8am-8pm")
	return ""
}

func (f *stickyFixture) sendsRecorded(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM campaign_contact_progress WHERE campaign_id = $1 AND sent_at IS NOT NULL`,
		f.campaign).Scan(&n); err != nil {
		t.Fatalf("count sends: %v", err)
	}
	return n
}

func (f *stickyFixture) campaignLogEvents(t *testing.T) []string {
	t.Helper()
	rows, err := f.pool.Query(context.Background(),
		`SELECT event_type FROM campaign_logs WHERE campaign_id = $1 ORDER BY created_at`, f.campaign)
	if err != nil {
		t.Fatalf("read logs: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			t.Fatalf("scan log: %v", err)
		}
		out = append(out, e)
	}
	return out
}

func (f *stickyFixture) logMessage(t *testing.T, eventType string) string {
	t.Helper()
	var msg string
	if err := f.pool.QueryRow(context.Background(),
		`SELECT message FROM campaign_logs WHERE campaign_id = $1 AND event_type = $2
		 ORDER BY created_at DESC LIMIT 1`, f.campaign, eventType).Scan(&msg); err != nil {
		t.Fatalf("read %q message: %v", eventType, err)
	}
	return msg
}

// One mailbox out of budget and one outside its own hours: the campaign is done
// for the day and must say so.
func TestLiveSilentCappedPoolStillReportsItself(t *testing.T) {
	zone := closedHoursZone(t)
	f := newStickyFixture(t, 1, 1)
	ctx := context.Background()

	// Mailbox 0 has spent the day's budget; mailbox 1 is in a timezone where
	// its own sending hours are closed.
	if _, err := f.pool.Exec(ctx, `UPDATE email_accounts SET campaign_limit = 1 WHERE id = $1`, f.mailboxes[0]); err != nil {
		t.Fatalf("cap mailbox 0: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO tasks (id, task_type, email_account_id, status, message_id,
	        scheduled_at, completed_at, created_at, updated_at)
	    VALUES ($1, 'campaign', $2, 'completed', 'mid@test.local', NOW(), NOW(), NOW(), NOW())`,
		uuid.New(), f.mailboxes[0]); err != nil {
		t.Fatalf("spend the budget: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE email_accounts SET timezone = $2 WHERE id = $1`,
		f.mailboxes[1], zone); err != nil {
		t.Fatalf("move mailbox 1: %v", err)
	}

	f.tick(t)

	if n := f.sendsRecorded(t); n != 0 {
		t.Fatalf("%d sends went out with no mailbox able to take one", n)
	}
	var found bool
	for _, e := range f.campaignLogEvents(t) {
		if e == "mailboxes_unavailable" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the campaign sent nothing and logged %v; an owner has no way to find out why",
			f.campaignLogEvents(t))
	}
	msg := f.logMessage(t, "mailboxes_unavailable")
	if !strings.Contains(msg, "out of budget") || !strings.Contains(msg, "sending hours") {
		t.Fatalf("the line reads %q; it must name both the spent budget and the closed hours", msg)
	}
	t.Logf("feed says: %s", msg)
}

// The same pool with nothing out of hours keeps the plain wording, so the
// common case is not dressed up in a condition that does not apply.
func TestLiveFullyCappedPoolKeepsThePlainWording(t *testing.T) {
	f := newStickyFixture(t, 1, 1)
	ctx := context.Background()
	for _, mb := range f.mailboxes {
		if _, err := f.pool.Exec(ctx, `UPDATE email_accounts SET campaign_limit = 1 WHERE id = $1`, mb); err != nil {
			t.Fatalf("cap mailbox: %v", err)
		}
		if _, err := f.pool.Exec(ctx, `INSERT INTO tasks (id, task_type, email_account_id, status, message_id,
		        scheduled_at, completed_at, created_at, updated_at)
		    VALUES ($1, 'campaign', $2, 'completed', 'mid@test.local', NOW(), NOW(), NOW(), NOW())`,
			uuid.New(), mb); err != nil {
			t.Fatalf("spend the budget: %v", err)
		}
	}

	f.tick(t)

	msg := f.logMessage(t, "daily_cap_reached")
	if !strings.Contains(msg, "used its daily budget") || strings.Contains(msg, "sending hours") {
		t.Fatalf("the line reads %q; with nothing out of hours it must not mention hours", msg)
	}
}
