package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// Sends made while tracking was off are in neither half of an open or click
// rate, so switching tracking on starts the rate from that moment.
func TestLiveCampaignRatesCountOnlyTrackedSends(t *testing.T) {
	handle, pool := liveContactDB(t)
	f := newSharedOrgFixture(t, pool)
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}

	// The campaign tracks opens now and never tracked clicks.
	exec(`UPDATE campaigns SET open_tracking = true, link_tracking = false, text_only = false WHERE id = $1`, f.campaign)
	step := uuid.New()
	exec(`INSERT INTO sequences (id, campaign_id, organization_id, name, subject, body_plain, body_html, kind)
	      VALUES ($1, $2, $3, 'Intro', 'Hi', 'Hello', '<p>Hello</p>', 'email')`, step, f.campaign, f.org)
	send := func(openTracked bool, opened bool) {
		t.Helper()
		contact := addLead(t, f, "tracked-"+uuid.NewString()[:8]+"@test.local", "valid", true)
		exec(`INSERT INTO campaign_contact_progress (campaign_id, contact_id, sequence_id, sent_at, opened_at, open_tracked, click_tracked)
		      VALUES ($1, $2, $3, NOW() - INTERVAL '1 hour', CASE WHEN $4 THEN NOW() END, $5, false)`,
			f.campaign, contact, step, opened, openTracked)
	}
	// Three sends while tracking was off, then four with it on, one opened.
	for range 3 {
		send(false, false)
	}
	send(true, true)
	for range 3 {
		send(true, false)
	}

	repo := &analyticsRepository{DB: handle}
	sum, xerr := repo.GetCampaignSummary(ctx, f.org, f.campaign, nil)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if sum.EmailsSent != 7 || sum.OpenTrackedSent != 4 || sum.ClickTrackedSent != 0 {
		t.Fatalf("summary sent %d open_tracked %d click_tracked %d, want 7/4/0", sum.EmailsSent, sum.OpenTrackedSent, sum.ClickTrackedSent)
	}
	if sum.OpenRate != 25 || sum.ClickRate != 0 {
		t.Fatalf("summary open %.2f click %.2f, want 25/0", sum.OpenRate, sum.ClickRate)
	}
	steps, xerr := repo.GetSequenceStats(ctx, f.campaign, nil)
	if xerr != nil || len(steps) != 1 {
		t.Fatalf("steps %v %v", steps, xerr)
	}
	if s := steps[0]; s.OpenTrackedSent != 4 || s.ClickTrackedSent != 0 || s.OpenRate != 25 {
		t.Fatalf("step open_tracked %d click_tracked %d open %.2f, want 4/0/25", s.OpenTrackedSent, s.ClickTrackedSent, s.OpenRate)
	}
}
