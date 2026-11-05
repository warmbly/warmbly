package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// stop_on_meeting reads meeting_bookings straight from the routing queries, so
// the SQL is asserted against Postgres: a booking since the lead entered stops
// them, whatever its status; one from before they entered, or recorded in
// another workspace, does not.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LiveStopOnMeeting -v
func TestLiveStopOnMeetingKeepsBookedLeadsOutOfRouting(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newRoutedPairsFixture(t, pool, 5)
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%q: %v", sql[:min(70, len(sql))], err)
		}
	}
	booked, quiet, bookedBefore, bookedElsewhere, newBooked := f.leads[0], f.leads[1], f.leads[2], f.leads[3], f.leads[4]

	// Email 1 -> Email 2 with no wait, so every lead that got email 1 is due now.
	followUp := uuid.New()
	exec(`INSERT INTO sequences (id, campaign_id, organization_id, name, subject,
	          body_plain, body_html, wait_after, position, kind)
	      VALUES ($1, $2, $3, 'Email 2', 'Hi', 'Hi', '<p>Hi</p>', 0, 1, 'email')`, followUp, f.campaign, f.org)
	bc, err := json.Marshal(models.BranchConditions{Branches: []models.Branch{{BranchID: "next", TargetSequenceID: &followUp}}})
	if err != nil {
		t.Fatalf("marshal conditions: %v", err)
	}
	exec(`UPDATE sequences SET conditions = $2 WHERE id = $1`, f.step, bc)

	entered := time.Now().Add(-72 * time.Hour)
	exec(`UPDATE campaign_leads SET added_at = $2 WHERE campaign_id = $1`, f.campaign, entered)
	for _, id := range []uuid.UUID{booked, quiet, bookedBefore, bookedElsewhere} {
		exec(`INSERT INTO campaign_contact_progress (campaign_id, contact_id, sequence_id, sent_at)
		      VALUES ($1, $2, $3, NOW() - interval '2 days')`, f.campaign, id, f.step)
	}

	otherOrg := uuid.New()
	exec(`INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Elsewhere', $2, $3)`,
		otherOrg, "elsewhere-"+otherOrg.String()[:8], f.owner)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, otherOrg) })

	book := func(org, contact uuid.UUID, status string, createdAt time.Time) {
		t.Helper()
		exec(`INSERT INTO meeting_bookings (organization_id, source, external_event_id, invitee_email,
		          contact_id, status, created_at)
		      VALUES ($1, 'calendly', $2, 'lead@test.local', $3, $4, $5)`,
			org, uuid.NewString(), contact, status, createdAt)
	}
	book(f.org, booked, "booked", time.Now().Add(-time.Hour))
	book(f.org, bookedBefore, "booked", entered.Add(-24*time.Hour))
	book(otherOrg, bookedElsewhere, "booked", time.Now().Add(-time.Hour))
	// A canceled booking still ends the sequence; a person decides whether to resume.
	book(f.org, newBooked, "canceled", time.Now().Add(-time.Hour))

	offered := func() map[uuid.UUID]uuid.UUID {
		t.Helper()
		pairs, _, _ := f.find(t, nil, 10)
		out := map[uuid.UUID]uuid.UUID{}
		for _, p := range pairs {
			out[p.ContactID] = p.SequenceID
		}
		return out
	}

	got := offered()
	for _, id := range []uuid.UUID{booked, newBooked} {
		if step, ok := got[id]; ok {
			t.Fatalf("lead who booked was offered step %s", step)
		}
	}
	for _, id := range []uuid.UUID{quiet, bookedBefore, bookedElsewhere} {
		if got[id] != followUp {
			t.Fatalf("lead %s offered %v, want the follow-up %s", id, got[id], followUp)
		}
	}

	repo := NewCampaignProgressRepository(pool)
	route, err := repo.RouteContact(ctx, f.campaign, booked)
	if err != nil {
		t.Fatalf("route contact: %v", err)
	}
	if route.Target != nil || route.StoppedBy != StoppedByMeeting {
		t.Fatalf("route = target %v stopped_by %q, want stopped by the meeting", route.Target, route.StoppedBy)
	}
	if n, err := repo.CountUndeliverableLeads(ctx, f.campaign); err != nil || n != 0 {
		t.Fatalf("count undeliverable = %d, %v", n, err)
	}

	exec(`UPDATE campaigns SET stop_on_meeting = false WHERE id = $1`, f.campaign)
	got = offered()
	if got[booked] != followUp || got[newBooked] != f.step {
		t.Fatalf("with stop_on_meeting off got %v, want the booked leads routed again", got)
	}
}
