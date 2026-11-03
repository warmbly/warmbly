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
