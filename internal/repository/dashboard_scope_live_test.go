package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

// The dashboard's campaign filter resolves folders and campaigns to one
// deduplicated set inside the selected workspace, and every campaign section
// reads only that set (issue #869).
func TestLiveDashboardScopeNarrowsCampaignSections(t *testing.T) {
	handle, pool := liveContactDB(t)
	ctx := context.Background()
	userID, orgID, otherOrgID := uuid.New(), uuid.New(), uuid.New()
	inFolderA, inFolderB, outside, foreign := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	folderID, foreignFolderID := uuid.New(), uuid.New()

	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	exec(`INSERT INTO users (id, first_name, last_name, email)
	      VALUES ($1, 'Dashboard', 'Scope', $2)`, userID, "dashboard-scope-"+uuid.NewString()+"@example.test")
	exec(`INSERT INTO organizations (id, name, owner_user_id)
	      VALUES ($1, 'Dashboard scope', $3), ($2, 'Dashboard scope other', $3)`, orgID, otherOrgID, userID)
	exec(`INSERT INTO folders (id, user_id, organization_id, title, color, position)
	      VALUES ($1, $3, $4, 'Client A', '#0ea5e9', 0), ($2, $3, $5, 'Other client', '#0ea5e9', 0)`,
		folderID, foreignFolderID, userID, orgID, otherOrgID)

	campaigns := []struct {
		id     uuid.UUID
		org    uuid.UUID
		name   string
		status string
	}{
		{inFolderA, orgID, "Client A - Dentists", "active"},
		{inFolderB, orgID, "Client A - Nurseries", "paused"},
		{outside, orgID, "Client B", "active"},
		{foreign, otherOrgID, "Foreign", "active"},
	}
	for _, c := range campaigns {
		contactID, stepID := uuid.New(), uuid.New()
		exec(`INSERT INTO campaigns (id, user_id, organization_id, name, description, days, status, created_at, updated_at)
		      VALUES ($1, $2, $3, $4, '', 62, $5, NOW(), NOW())`, c.id, userID, c.org, c.name, c.status)
		exec(`INSERT INTO contacts
		        (id, user_id, organization_id, email, first_name, last_name, company, phone, custom_fields, subscribed, created_at, updated_at)
		      VALUES ($1, $2, $3, $4, 'Lead', 'Scope', '', '', '{}'::jsonb, true, NOW(), NOW())`,
			contactID, userID, c.org, "dashboard-scope-lead-"+uuid.NewString()+"@example.test")
		exec(`INSERT INTO campaign_leads (campaign_id, contact_id, position) VALUES ($1, $2, 0)`, c.id, contactID)
		exec(`INSERT INTO sequences
		        (id, campaign_id, organization_id, name, subject, body_plain, body_html, wait_after, position, kind, created_at)
		      VALUES ($1, $2, $3, 'Step', '', '', '', 0, 1, 'email', NOW())`, stepID, c.id, c.org)
		exec(`INSERT INTO campaign_contact_progress (campaign_id, contact_id, sequence_id, sent_at, replied_at)
		      VALUES ($1, $2, $3, NOW() - interval '1 hour', NOW())`, c.id, contactID, stepID)
	}
	// The second campaign also sits in a folder of another workspace, which must not reach it.
	exec(`INSERT INTO campaign_folders (campaign_id, folder_id)
	      VALUES ($1, $3), ($2, $3), ($2, $4)`, inFolderA, inFolderB, folderID, foreignFolderID)

	t.Cleanup(func() {
		for _, step := range []struct {
			query string
			arg   any
		}{
			{`DELETE FROM campaigns WHERE organization_id = ANY($1)`, []uuid.UUID{orgID, otherOrgID}},
			{`DELETE FROM folders WHERE organization_id = ANY($1)`, []uuid.UUID{orgID, otherOrgID}},
			{`DELETE FROM contacts WHERE organization_id = ANY($1)`, []uuid.UUID{orgID, otherOrgID}},
			{`DELETE FROM organizations WHERE id = ANY($1)`, []uuid.UUID{orgID, otherOrgID}},
			{`DELETE FROM users WHERE id = $1`, userID},
		} {
			if _, err := pool.Exec(context.Background(), step.query, step.arg); err != nil {
				t.Errorf("cleanup: %v", err)
			}
		}
	})

	repo := &analyticsRepository{DB: handle}
	from, to := time.Now().Add(-24*time.Hour), time.Now().Add(24*time.Hour)

	// The folder plus one of its own campaigns, a foreign campaign and a foreign folder.
	view, scope, xerr := repo.ResolveDashboardScope(ctx, orgID, models.DashboardFilter{
		CampaignIDs: []uuid.UUID{inFolderB, foreign},
		FolderIDs:   []uuid.UUID{folderID, foreignFolderID},
	})
	if xerr != nil {
		t.Fatalf("ResolveDashboardScope: %v", xerr)
	}
	if view.CampaignCount != 2 || len(scope.CampaignIDs) != 2 {
		t.Fatalf("resolved %d campaigns (%v), want the folder's two counted once", view.CampaignCount, scope.CampaignIDs)
	}
	if len(view.Campaigns) != 1 || view.Campaigns[0].ID != inFolderB || len(view.Folders) != 1 || view.Folders[0].Name != "Client A" {
		t.Fatalf("echoed scope = %+v, want only this workspace's campaign and folder", view)
	}

	overall, xerr := repo.GetDashboardOverallStats(ctx, orgID, from, to, scope, nil)
	if xerr != nil {
		t.Fatalf("GetDashboardOverallStats: %v", xerr)
	}
	if overall.TotalEmailsSent != 2 || overall.TotalReplies != 2 || overall.ActiveCampaigns != 1 {
		t.Fatalf("scoped totals = sent %d, replies %d, active campaigns %d; want 2/2/1",
			overall.TotalEmailsSent, overall.TotalReplies, overall.ActiveCampaigns)
	}
	trend, xerr := repo.GetDashboardDailyTrend(ctx, orgID, from, to, scope)
	if xerr != nil {
		t.Fatalf("GetDashboardDailyTrend: %v", xerr)
	}
	sent := 0
	for _, d := range trend {
		sent += d.Sent
	}
	if sent != 2 {
		t.Fatalf("scoped trend sent = %d, want 2", sent)
	}
	top, xerr := repo.GetTopCampaigns(ctx, orgID, from, to, 10, "emails_sent", scope)
	if xerr != nil {
		t.Fatalf("GetTopCampaigns: %v", xerr)
	}
	recent, xerr := repo.GetRecentActivity(ctx, orgID, 20, scope)
	if xerr != nil {
		t.Fatalf("GetRecentActivity: %v", xerr)
	}
	inScope := map[uuid.UUID]bool{inFolderA: true, inFolderB: true}
	if len(top) != 2 {
		t.Fatalf("scoped top campaigns = %+v, want the two in scope", top)
	}
	for _, c := range top {
		if !inScope[c.CampaignID] {
			t.Errorf("top campaigns include %s outside the scope", c.Name)
		}
	}
	if len(recent) == 0 {
		t.Fatalf("scoped recent activity is empty, want the scoped replies")
	}
	for _, a := range recent {
		if !inScope[a.CampaignID] {
			t.Errorf("recent activity includes %s outside the scope", a.CampaignName)
		}
	}

	// Nothing of this workspace's: an empty set, never the whole workspace.
	view, scope, xerr = repo.ResolveDashboardScope(ctx, orgID, models.DashboardFilter{
		CampaignIDs: []uuid.UUID{foreign},
		FolderIDs:   []uuid.UUID{foreignFolderID},
	})
	if xerr != nil {
		t.Fatalf("ResolveDashboardScope: %v", xerr)
	}
	if view.CampaignCount != 0 || len(view.Campaigns) != 0 || len(view.Folders) != 0 {
		t.Fatalf("foreign ids resolved to %+v, want nothing", view)
	}
	overall, xerr = repo.GetDashboardOverallStats(ctx, orgID, from, to, scope, nil)
	if xerr != nil {
		t.Fatalf("GetDashboardOverallStats: %v", xerr)
	}
	if overall.TotalEmailsSent != 0 || overall.ActiveCampaigns != 0 {
		t.Fatalf("an empty scope counted sent %d, active %d; want nothing", overall.TotalEmailsSent, overall.ActiveCampaigns)
	}
	whole, xerr := repo.GetDashboardOverallStats(ctx, orgID, from, to, nil, nil)
	if xerr != nil {
		t.Fatalf("GetDashboardOverallStats: %v", xerr)
	}
	if whole.TotalEmailsSent != 3 {
		t.Fatalf("workspace sent = %d, want this workspace's three", whole.TotalEmailsSent)
	}
}
