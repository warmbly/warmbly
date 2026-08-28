package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// Issue #146: the launch gate measures the list as it is NOW, not from the
// advisor's periodic rollup. These prove the query against the real schema.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LiveCampaignAudience -v

// addLead inserts a contact with the given verification state and enrols it.
func addLead(t *testing.T, f *sharedOrgFixture, email, status string, subscribed bool) uuid.UUID {
	t.Helper()
	_, pool := liveContactDB(t)
	id := uuid.New()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO contacts (id, user_id, organization_id, email, first_name, last_name,
		     company, phone, custom_fields, verification_status, subscribed)
		 VALUES ($1, $2, $3, $4, 'A', 'B', '', '', '{}'::jsonb, $5, $6)`,
		id, f.owner, f.org, email, status, subscribed); err != nil {
		t.Fatalf("insert contact: %v", err)
	}
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO campaign_leads (campaign_id, contact_id) VALUES ($1, $2)`, f.campaign, id); err != nil {
		t.Fatalf("add lead: %v", err)
	}
	return id
}

func TestLiveCampaignAudienceCountsEachClass(t *testing.T) {
	handle, pool := liveContactDB(t)
	f := newSharedOrgFixture(t, pool)
	repo := NewCampaignAudienceRepository(handle)

	addLead(t, f, "invalid-"+uuid.New().String()[:6]+"@test.local", "invalid", true)
	addLead(t, f, "risky-"+uuid.New().String()[:6]+"@test.local", "risky", true)
	addLead(t, f, "valid-"+uuid.New().String()[:6]+"@test.local", "valid", true)
	addLead(t, f, "unknown-"+uuid.New().String()[:6]+"@test.local", "unknown", true)
	addLead(t, f, "gone-"+uuid.New().String()[:6]+"@test.local", "valid", false)
	// Role address and free-mail domain, using the same vocabularies the
	// advisor uses so a customer is never told two different numbers.
	addLead(t, f, "info@test.local", "valid", true)
	addLead(t, f, "someone-"+uuid.New().String()[:6]+"@gmail.com", "valid", true)

	got, err := repo.GetCampaignAudience(context.Background(), f.org, f.campaign)
	if err != nil {
		t.Fatalf("GetCampaignAudience: %v", err)
	}
	if got.Total < 7 {
		t.Fatalf("total = %d, want at least the 7 leads added", got.Total)
	}
	if got.Invalid != 1 {
		t.Errorf("invalid = %d, want 1", got.Invalid)
	}
	if got.Risky != 1 {
		t.Errorf("risky = %d, want 1", got.Risky)
	}
	if got.Unknown != 1 {
		t.Errorf("unknown = %d, want 1; 'unknown' is the column default for an unchecked address", got.Unknown)
	}
	if got.Unsubscribed != 1 {
		t.Errorf("unsubscribed = %d, want 1", got.Unsubscribed)
	}
	if got.RoleAddress != 1 {
		t.Errorf("role addresses = %d, want the info@ lead", got.RoleAddress)
	}
	if got.FreeMail != 1 {
		t.Errorf("free mail = %d, want the gmail.com lead", got.FreeMail)
	}
	// Counted, not derived: the unsubscribed lead is excluded once even though
	// it could also have been suppressed.
	if got.Deliverable != got.Total-got.Unsubscribed {
		t.Errorf("deliverable = %d, want %d", got.Deliverable, got.Total-got.Unsubscribed)
	}
}

// The audience is scoped to ONE campaign and ONE organization: a gate that
// counted another campaign's leads would refuse launches for the wrong reason.
func TestLiveCampaignAudienceIsScoped(t *testing.T) {
	handle, pool := liveContactDB(t)
	f := newSharedOrgFixture(t, pool)
	repo := NewCampaignAudienceRepository(handle)

	addLead(t, f, "one-"+uuid.New().String()[:6]+"@test.local", "invalid", true)

	// A different organization must see nothing for this campaign.
	got, err := repo.GetCampaignAudience(context.Background(), uuid.New(), f.campaign)
	if err != nil {
		t.Fatalf("GetCampaignAudience: %v", err)
	}
	if got.Total != 0 {
		t.Errorf("another organization saw %d leads, want 0", got.Total)
	}

	// An unknown campaign in the right organization is likewise empty.
	got, err = repo.GetCampaignAudience(context.Background(), f.org, uuid.New())
	if err != nil {
		t.Fatalf("GetCampaignAudience: %v", err)
	}
	if got.Total != 0 {
		t.Errorf("an unknown campaign returned %d leads, want 0", got.Total)
	}
}
