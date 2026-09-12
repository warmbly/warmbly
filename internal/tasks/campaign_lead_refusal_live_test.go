package tasks

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// Live checks for issue #437: a campaign that reads ACTIVE, whose leads read
// "Due" in the contact drawer, and that never sends anything.
//
// The cause was a scope mistake, not a missing gate. Placement can refuse a
// send for a reason that belongs to ONE lead — ESP-strict finds no mailbox for
// that recipient's provider, that lead's own mailbox is busy, that recipient's
// preferred hours are hours away — and the tick answered by deferring the whole
// campaign. Routing hands back the same lead on the next tick, so the campaign
// deferred, woke, refused the same lead and deferred again, forever, while
// every lead behind it sat queued.
//
// ESP-strict is the cheapest way to make placement refuse exactly one lead
// deterministically, so it is what these drive.

// espStrictOnLeadA points the campaign at same-provider-only sending and moves
// lead A (the first in routing order) onto a domain the pool cannot match: the
// fixture's only mailbox is smtp_imap, which under strict mode is never a
// Gmail match. Lead B keeps a plain domain, which is a wildcard.
func espStrictOnLeadA(t *testing.T, f *sendFixture) {
	t.Helper()
	ctx := context.Background()
	if _, err := f.pool.Exec(ctx, `UPDATE contacts SET email = $2 WHERE id = $1`,
		f.leadA, "blocked-"+f.leadA.String()[:8]+"@gmail.com"); err != nil {
		t.Fatalf("move lead A to gmail: %v", err)
	}
	if _, err := f.pool.Exec(ctx,
		`UPDATE campaigns SET esp_match_mode = 'strict' WHERE id = $1`, f.campaign); err != nil {
		t.Fatalf("enable ESP-strict: %v", err)
	}
}

// pendingWakeups counts the campaign's live chain.
func (f *sendFixture) pendingWakeups(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM tasks t
		JOIN campaign_tasks ct ON ct.task_id = t.id
		WHERE ct.campaign_id = $1 AND t.status = 'pending'`, f.campaign).Scan(&n); err != nil {
		t.Fatalf("count wakeups: %v", err)
	}
	return n
}

// logCount counts one kind of activity line on the campaign.
func (f *sendFixture) logCount(t *testing.T, eventType string) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM campaign_logs WHERE campaign_id = $1 AND event_type = $2`,
		f.campaign, eventType).Scan(&n); err != nil {
		t.Fatalf("count %q logs: %v", eventType, err)
	}
	return n
}

// The headline regression: one lead placement cannot serve must not stop the
// lead behind it from being sent, on the very first tick.
func TestLiveRefusedLeadDoesNotParkTheCampaign(t *testing.T) {
	f := newSendFixture(t)
	espStrictOnLeadA(t, f)

	f.tick(t)

	if f.sender.count() != 1 {
		t.Fatalf("the tick dispatched %d sends; the lead behind the refused one was never served", f.sender.count())
	}
	if row := f.progressFor(t, f.leadB); row == nil || row.sentAt == nil {
		t.Fatalf("lead B was not sent: %+v", row)
	}
	if row := f.progressFor(t, f.leadA); row != nil && (row.sentAt != nil || row.dispatchedAt != nil) {
		t.Fatalf("the refused lead was sent anyway from a cross-provider mailbox: %+v", row)
	}
	if n := f.pendingWakeups(t); n != 1 {
		t.Fatalf("the chain has %d pending wakeups, want exactly 1", n)
	}
}

// And the campaign stays alive around the lead it cannot serve: it does not
// complete (the lead is still there), it does not pause, and the reason is
// written to the activity log once rather than once per refused lead per tick.
func TestLiveCampaignSurvivesALeadItCannotServe(t *testing.T) {
	f := newSendFixture(t)
	espStrictOnLeadA(t, f)

	for i := 0; i < 3; i++ {
		f.tick(t)
	}

	if f.sender.count() != 1 {
		t.Fatalf("%d sends over three ticks, want exactly 1 (lead B once, lead A never)", f.sender.count())
	}
	var status string
	if err := f.pool.QueryRow(context.Background(),
		`SELECT status FROM campaigns WHERE id = $1`, f.campaign).Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}
	if status != "active" {
		t.Fatalf("campaign is %q; a lead waiting for a mailbox its provider does not have is not the end of the campaign", status)
	}
	if n := f.logCount(t, "provider_match_deferred"); n != 1 {
		t.Fatalf("the ESP-strict deferral was logged %d times; it must be once a day, not once per refused lead per tick", n)
	}
	if n := f.logCount(t, "completed"); n != 0 {
		t.Fatalf("the campaign logged %d completions while a lead was still waiting", n)
	}
}

// A campaign whose EVERY due lead is refused still parks a successor rather
// than completing or pausing, so it resumes the moment a matching mailbox
// appears.
func TestLiveCampaignWithEveryLeadRefusedDefersInsteadOfCompleting(t *testing.T) {
	f := newSendFixture(t)
	ctx := context.Background()
	espStrictOnLeadA(t, f)
	if _, err := f.pool.Exec(ctx, `UPDATE contacts SET email = $2 WHERE id = $1`,
		f.leadB, "blocked-"+uuid.New().String()[:8]+"@outlook.com"); err != nil {
		t.Fatalf("move lead B to outlook: %v", err)
	}

	f.tick(t)

	if f.sender.count() != 0 {
		t.Fatalf("%d sends went out while no mailbox matched any recipient", f.sender.count())
	}
	if n := f.pendingWakeups(t); n != 1 {
		t.Fatalf("the chain has %d pending wakeups, want exactly 1: the campaign must come back and look again", n)
	}
	var status string
	if err := f.pool.QueryRow(ctx, `SELECT status FROM campaigns WHERE id = $1`, f.campaign).Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}
	if status != "active" {
		t.Fatalf("campaign is %q, want it still active and waiting", status)
	}
}
