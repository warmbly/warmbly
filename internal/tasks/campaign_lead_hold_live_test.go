package tasks

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// A campaign whose remaining leads are all held has NOT finished, and a hold
// must not be raced by a send that was already in flight (issue #470).
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/tasks/ -run LiveHeldLead -v

func (f *sendFixture) holdLead(t *testing.T, contact uuid.UUID, until *time.Time) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(),
		`UPDATE campaign_leads SET paused_at = NOW(), paused_until = $3, pause_source = 'manual', pause_reason = 'away'
		 WHERE campaign_id = $1 AND contact_id = $2`, f.campaign, contact, until); err != nil {
		t.Fatalf("hold lead: %v", err)
	}
}

// The bug: a hold with no end reports no next-due moment, which the scheduler
// read as "everything sent". The campaign closed itself and logged "Campaign
// completed: all emails sent" while every one of its leads was merely parked.
func TestLiveHeldLeadsDoNotCompleteTheCampaign(t *testing.T) {
	f := newSendFixture(t)
	f.holdLead(t, f.leadA, nil)
	f.holdLead(t, f.leadB, nil)

	for i := 0; i < 3; i++ {
		f.tick(t)
	}

	if status := f.campaignStatus(t); status != "active" {
		t.Fatalf("campaign is %q with every lead held; it has not finished, it is waiting", status)
	}
	if n := f.countLogs(t, "completed"); n != 0 {
		t.Fatalf("the campaign logged %d completion entries while its leads were parked", n)
	}
	if f.sender.count() != 0 {
		t.Fatalf("%d sends went out to held leads", f.sender.count())
	}
	if n := f.countLogs(t, CampaignHeldEventType); n == 0 {
		t.Fatal("nothing recorded that the campaign is waiting on paused leads")
	}
}

// A held lead is never dispatched. Routing is what refuses it here; the
// pre-send gate is the backstop for a hold that lands between routing's read
// and the dispatch, which is a race no in-process test can stage — so this
// asserts the outcome, not which layer produced it.
func TestLiveHeldLeadIsNeverDispatched(t *testing.T) {
	f := newSendFixture(t)
	until := time.Now().Add(72 * time.Hour)
	f.holdLead(t, f.leadA, &until)
	f.holdLead(t, f.leadB, &until)

	for i := 0; i < 3; i++ {
		f.tick(t)
	}
	if f.sender.count() != 0 {
		t.Fatalf("%d sends went out to a held lead", f.sender.count())
	}
	for _, lead := range []uuid.UUID{f.leadA, f.leadB} {
		if row := f.progressFor(t, lead); row != nil && (row.sentAt != nil || row.dispatchedAt != nil) {
			t.Fatalf("a held lead was dispatched anyway: %+v", row)
		}
	}
}

// Lifting the hold puts the lead straight back in the queue: a paused lead is
// not a dropped one.
func TestLiveResumedLeadIsSentAgain(t *testing.T) {
	f := newSendFixture(t)
	f.holdLead(t, f.leadA, nil)
	f.holdLead(t, f.leadB, nil)
	f.tick(t)
	if f.sender.count() != 0 {
		t.Fatalf("%d sends went out while both leads were held", f.sender.count())
	}

	if _, err := f.pool.Exec(context.Background(),
		`UPDATE campaign_leads SET paused_at = NULL, paused_until = NULL, pause_source = NULL, pause_reason = NULL
		 WHERE campaign_id = $1`, f.campaign); err != nil {
		t.Fatalf("resume: %v", err)
	}
	for i := 0; i < 3; i++ {
		f.tick(t)
	}
	if f.sender.count() == 0 {
		t.Fatalf("nothing was sent after the holds were lifted; campaign is %q", f.campaignStatus(t))
	}
}
