package repository

import (
	"context"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/models"
)

// A contact whose sequence is still running in a launched campaign is left out
// of another campaign when the caller asks; finished elsewhere, it joins.
func TestLiveBulkAddSkipsContactsActiveInAnotherCampaign(t *testing.T) {
	handle, pool := liveContactDB(t)
	f := newSharedOrgFixture(t, pool)
	repo := NewContactRepostory(handle)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `UPDATE campaigns SET status = 'active' WHERE id = $1`, f.other); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO campaign_leads (contact_id, campaign_id) VALUES ($1, $2)`, f.contact, f.other); err != nil {
		t.Fatal(err)
	}
	isLead := func() bool {
		var n int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM campaign_leads WHERE campaign_id = $1 AND contact_id = $2`, f.campaign, f.contact).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n == 1
	}
	add := func(skip bool) {
		t.Helper()
		if _, xerr := repo.BulkUpdate(ctx, f.owner.String(), f.org, &models.BulkEditContactsData{
			ContactSelection:           models.ContactSelection{Contacts: []string{f.contact.String()}},
			AddCampaigns:               []string{f.campaign.String()},
			SkipActiveInOtherCampaigns: skip,
		}); xerr != nil {
			t.Fatalf("bulk add: %v", xerr)
		}
	}

	add(true)
	if isLead() {
		t.Fatal("a contact queued in an active campaign was enrolled with the skip on")
	}

	// A draft campaign is not running yet, so it does not hold the contact back.
	if _, err := pool.Exec(ctx, `UPDATE campaigns SET status = 'draft' WHERE id = $1`, f.other); err != nil {
		t.Fatal(err)
	}
	add(true)
	if !isLead() {
		t.Fatal("a contact only in a draft campaign was skipped")
	}
}

// A reply holds the contact everywhere except the campaign it answered.
func TestLiveHoldLeadElsewhereLeavesTheAnsweredCampaign(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newSharedOrgFixture(t, pool)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO campaign_leads (contact_id, campaign_id) VALUES ($1, $2), ($1, $3)`, f.contact, f.campaign, f.other); err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(365 * 24 * time.Hour)
	held, err := NewCampaignProgressRepository(pool).HoldLeadElsewhere(ctx, f.contact, f.campaign, &until,
		"replied to another campaign", models.LeadHoldSourceInboxTagging)
	if err != nil {
		t.Fatal(err)
	}
	if len(held) != 1 || held[0] != f.other {
		t.Fatalf("held %v, want only %s", held, f.other)
	}
	var answeredHeld bool
	if err := pool.QueryRow(ctx, `SELECT paused_at IS NOT NULL FROM campaign_leads WHERE campaign_id = $1 AND contact_id = $2`, f.campaign, f.contact).Scan(&answeredHeld); err != nil {
		t.Fatal(err)
	}
	if answeredHeld {
		t.Fatal("the answered campaign was held too")
	}
}

// A segment enrol with the skip on leaves out a member queued in another
// active campaign, and a hand-picked add with the skip on does not claim that
// member's segment lead as manual.
func TestLiveSegmentAndClaimHonourTheSkip(t *testing.T) {
	handle, pool := liveContactDB(t)
	f := newSharedOrgFixture(t, pool)
	ctx := context.Background()
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM segments WHERE organization_id = $1`, f.org); err != nil {
			t.Errorf("cleanup segments: %v", err)
		}
	})
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE campaigns SET status = 'active' WHERE id = $1`, f.other)
	exec(`INSERT INTO campaign_leads (contact_id, campaign_id) VALUES ($1, $2)`, f.contact, f.other)

	segments := NewSegmentRepository(handle)
	seg, xerr := segments.Create(ctx, f.org, &f.owner, &models.Segment{
		Name: "Pied Piper", Color: "#0284c7", Match: models.SegmentMatchAll,
		Conditions: []models.SegmentCondition{{Field: "company", Operator: "equals", Value: "pied piper"}},
	})
	if xerr != nil {
		t.Fatalf("create segment: %v", xerr)
	}
	out, xerr := segments.AddToCampaign(ctx, f.org, f.owner.String(), seg.ID, f.campaign, true)
	if xerr != nil || out.Members != 1 || out.Added != 0 {
		t.Fatalf("skip on = %+v, %v; want the one member left out", out, xerr)
	}
	out, xerr = segments.AddToCampaign(ctx, f.org, f.owner.String(), seg.ID, f.campaign, false)
	if xerr != nil || out.Added != 1 {
		t.Fatalf("skip off = %+v, %v; want the member added", out, xerr)
	}

	exec(`UPDATE campaign_leads SET source = 'segment' WHERE campaign_id = $1 AND contact_id = $2`, f.campaign, f.contact)
	source := func() string {
		var s string
		if err := pool.QueryRow(ctx, `SELECT source FROM campaign_leads WHERE campaign_id = $1 AND contact_id = $2`, f.campaign, f.contact).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	add := func(skip bool) {
		t.Helper()
		if _, xerr := NewContactRepostory(handle).BulkUpdate(ctx, f.owner.String(), f.org, &models.BulkEditContactsData{
			ContactSelection:           models.ContactSelection{Contacts: []string{f.contact.String()}},
			AddCampaigns:               []string{f.campaign.String()},
			SkipActiveInOtherCampaigns: skip,
		}); xerr != nil {
			t.Fatalf("bulk add: %v", xerr)
		}
	}
	add(true)
	if got := source(); got != "segment" {
		t.Fatalf("source = %q after a skipped add, want the segment lead left as it was", got)
	}
	add(false)
	if got := source(); got != "manual" {
		t.Fatalf("source = %q after a plain add, want manual", got)
	}
}
