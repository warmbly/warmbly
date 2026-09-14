package repository

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// Regression cover for issue #187: adding a contact to a campaign silently did
// nothing whenever the campaign had been created by a different member of the
// same organization. Campaign membership was matched on campaigns.user_id, so
// the INSERT selected no rows, the API answered 200 with the contact's campaign
// list rendered through the same user filter, and nothing reached the Leads tab.
//
// Campaigns are organization assets. Every path below is exercised as the
// teammate who did NOT create the campaign.
//
// Run against the dev stack:
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LiveContactCampaign -v

// requireSchemaVersion fails loudly on a database behind the branch, where a
// live fixture would otherwise die three calls deep on a foreign key.
func requireSchemaVersion(t *testing.T, pool *pgxpool.Pool, min int64) {
	t.Helper()
	var version int64
	if err := pool.QueryRow(context.Background(), `SELECT version FROM schema_migrations LIMIT 1`).Scan(&version); err != nil || version < min {
		t.Fatalf("WARMBLY_TEST_DB is at schema version %d (err %v); this branch needs %d or later", version, err, min)
	}
}

func liveContactDB(t *testing.T) (*db.DB, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("WARMBLY_TEST_DB")
	if dsn == "" {
		t.Skip("WARMBLY_TEST_DB not set")
	}
	handle, err := db.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { handle.Pool.Close() })
	if err := handle.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
	requireSchemaVersion(t, handle.Pool, 156)
	return handle, handle.Pool
}

// sharedOrgFixture is one organization with two members: `owner` created the
// campaign, `mate` is the teammate acting on it.
type sharedOrgFixture struct {
	org      uuid.UUID
	owner    uuid.UUID
	mate     uuid.UUID
	campaign uuid.UUID
	other    uuid.UUID
	contact  uuid.UUID
}

func newSharedOrgFixture(t *testing.T, pool *pgxpool.Pool) *sharedOrgFixture {
	t.Helper()
	ctx := context.Background()
	f := &sharedOrgFixture{
		org:      uuid.New(),
		owner:    uuid.New(),
		mate:     uuid.New(),
		campaign: uuid.New(),
		other:    uuid.New(),
		contact:  uuid.New(),
	}

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture %q: %v", sql[:min(60, len(sql))], err)
		}
	}

	for _, u := range []struct {
		id   uuid.UUID
		name string
	}{{f.owner, "Owner"}, {f.mate, "Mate"}} {
		exec(`INSERT INTO users (id, first_name, last_name, email, password_hash)
		      VALUES ($1, $2, 'Live', $3, 'x')`,
			u.id, u.name, "i187-"+u.id.String()[:8]+"@test.local")
	}
	exec(`INSERT INTO organizations (id, name, slug, owner_user_id)
	      VALUES ($1, 'Issue 187', $2, $3)`, f.org, "i187-"+f.org.String()[:8], f.owner)
	exec(`INSERT INTO organization_members (organization_id, user_id, role, accepted_at)
	      VALUES ($1, $2, 'owner', NOW()), ($1, $3, 'admin', NOW())`, f.org, f.owner, f.mate)
	// Both campaigns belong to the org but were created by the OWNER.
	for _, c := range []struct {
		id   uuid.UUID
		name string
	}{{f.campaign, "Agency partnerships"}, {f.other, "RevOps outreach"}} {
		exec(`INSERT INTO campaigns (id, user_id, organization_id, name, description, days, updated_at, created_at)
		      VALUES ($1, $2, $3, $4, '', 62, NOW(), NOW())`, c.id, f.owner, f.org, c.name)
	}
	exec(`INSERT INTO contacts (id, user_id, organization_id, email, first_name, last_name, company, phone, custom_fields, updated_at, created_at)
	      VALUES ($1, $2, $3, $4, 'Carlos', 'Diaz', 'Pied Piper', '', '{}'::jsonb, NOW(), NOW())`,
		f.contact, f.owner, f.org, "i187-"+f.contact.String()[:8]+"@test.local")

	t.Cleanup(func() {
		c := context.Background()
		for _, step := range []struct {
			sql string
			arg any
		}{
			{`DELETE FROM campaign_leads WHERE campaign_id IN (SELECT id FROM campaigns WHERE organization_id = $1)`, f.org},
			{`DELETE FROM campaigns WHERE organization_id = $1`, f.org},
			{`DELETE FROM contacts WHERE organization_id = $1`, f.org},
			{`DELETE FROM organization_members WHERE organization_id = $1`, f.org},
			{`DELETE FROM organizations WHERE id = $1`, f.org},
			{`DELETE FROM users WHERE id = ANY($1)`, []uuid.UUID{f.owner, f.mate}},
		} {
			if _, err := pool.Exec(c, step.sql, step.arg); err != nil {
				t.Errorf("cleanup %q: %v", step.sql, err)
			}
		}
	})
	return f
}

func campaignNames(t *testing.T, campaigns []models.MiniCampaign) []string {
	t.Helper()
	out := make([]string, 0, len(campaigns))
	for _, c := range campaigns {
		out = append(out, c.Name)
	}
	return out
}

func TestLiveContactCampaignMembershipIsOrganizationWide(t *testing.T) {
	handle, pool := liveContactDB(t)
	f := newSharedOrgFixture(t, pool)
	repo := NewContactRepostory(handle)
	ctx := context.Background()
	mate := f.mate.String()

	// 1. The teammate adds the contact to a campaign they did not create.
	updated, xerr := repo.Update(ctx, mate, f.contact.String(), f.org, &models.UpdateContact{
		Campaigns: []string{f.campaign.String()},
	})
	if xerr != nil {
		t.Fatalf("update: %v", xerr)
	}
	if got := campaignNames(t, updated.Campaigns); len(got) != 1 || got[0] != "Agency partnerships" {
		t.Fatalf("response campaigns = %v, want [Agency partnerships]", got)
	}
	var leads int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM campaign_leads WHERE campaign_id = $1 AND contact_id = $2`,
		f.campaign, f.contact).Scan(&leads); err != nil {
		t.Fatalf("count leads: %v", err)
	}
	if leads != 1 {
		t.Fatalf("campaign_leads rows = %d, want 1 (the add was a silent no-op)", leads)
	}

	// 2. The campaign Leads tab is this search scoped to the one campaign.
	res, xerr := repo.Search(ctx, f.org.String(), nil, nil, models.SearchContacts{
		CampaignIDs: []string{f.campaign.String()},
	}, 25)
	if xerr != nil {
		t.Fatalf("search: %v", xerr)
	}
	if len(res.Data) != 1 || res.Data[0].ID != f.contact {
		t.Fatalf("leads search returned %d rows, want the contact", len(res.Data))
	}

	// 3. The contact 360 shows the membership to the teammate too.
	detail, xerr := repo.GetDetail(ctx, f.mate, &f.org, f.contact)
	if xerr != nil {
		t.Fatalf("detail: %v", xerr)
	}
	if got := campaignNames(t, detail.Campaigns); len(got) != 1 || got[0] != "Agency partnerships" {
		t.Fatalf("detail campaigns = %v, want [Agency partnerships]", got)
	}

	// 4. An edit that does not mention campaigns keeps membership and reports it.
	company := "Pied Piper Inc"
	updated, xerr = repo.Update(ctx, mate, f.contact.String(), f.org, &models.UpdateContact{Company: &company})
	if xerr != nil {
		t.Fatalf("update company: %v", xerr)
	}
	if got := campaignNames(t, updated.Campaigns); len(got) != 1 {
		t.Fatalf("campaigns after unrelated edit = %v, want the membership preserved", got)
	}

	// 5. The teammate can also move the contact between the owner's campaigns.
	updated, xerr = repo.Update(ctx, mate, f.contact.String(), f.org, &models.UpdateContact{
		Campaigns: []string{f.other.String()},
	})
	if xerr != nil {
		t.Fatalf("update swap: %v", xerr)
	}
	if got := campaignNames(t, updated.Campaigns); len(got) != 1 || got[0] != "RevOps outreach" {
		t.Fatalf("campaigns after swap = %v, want [RevOps outreach]", got)
	}
}

func TestLiveContactCreatedIntoATeammatesCampaign(t *testing.T) {
	handle, pool := liveContactDB(t)
	f := newSharedOrgFixture(t, pool)
	repo := NewContactRepostory(handle)
	ctx := context.Background()

	added, xerr := repo.Add(ctx, f.mate.String(), f.org, []models.AddContact{{
		FirstName: "New",
		LastName:  "Lead",
		Email:     "i187-new-" + f.org.String()[:8] + "@test.local",
		Campaigns: []string{f.campaign.String()},
	}})
	if xerr != nil {
		t.Fatalf("add: %v", xerr)
	}
	if len(added) != 1 {
		t.Fatalf("added %d contacts, want 1", len(added))
	}
	if got := campaignNames(t, added[0].Campaigns); len(got) != 1 || got[0] != "Agency partnerships" {
		t.Fatalf("new contact campaigns = %v, want [Agency partnerships]", got)
	}

	var leads int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM campaign_leads WHERE campaign_id = $1 AND contact_id = $2`,
		f.campaign, added[0].ID).Scan(&leads); err != nil {
		t.Fatalf("count leads: %v", err)
	}
	if leads != 1 {
		t.Fatalf("campaign_leads rows = %d, want 1 (the new lead never reached the campaign)", leads)
	}
}

func TestLiveContactBulkAddToATeammatesCampaign(t *testing.T) {
	handle, pool := liveContactDB(t)
	f := newSharedOrgFixture(t, pool)
	repo := NewContactRepostory(handle)
	ctx := context.Background()

	updated, xerr := repo.BulkUpdate(ctx, f.mate.String(), f.org, &models.BulkEditContactsData{
		ContactSelection: models.ContactSelection{Contacts: []string{f.contact.String()}},
		AddCampaigns:     []string{f.campaign.String()},
	})
	if xerr != nil {
		t.Fatalf("bulk update: %v", xerr)
	}
	if len(updated) != 1 {
		t.Fatalf("bulk updated %d contacts, want 1", len(updated))
	}
	if got := campaignNames(t, updated[0].Campaigns); len(got) != 1 || got[0] != "Agency partnerships" {
		t.Fatalf("bulk response campaigns = %v, want [Agency partnerships]", got)
	}

	var leads int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM campaign_leads WHERE campaign_id = $1 AND contact_id = $2`,
		f.campaign, f.contact).Scan(&leads); err != nil {
		t.Fatalf("count leads: %v", err)
	}
	if leads != 1 {
		t.Fatalf("campaign_leads rows = %d, want 1", leads)
	}
}
