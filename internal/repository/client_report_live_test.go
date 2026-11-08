package repository

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

// A client report reads only the campaigns its scope resolves to inside its own
// workspace, through a folder of that workspace, never a foreign id it names.
func TestLiveClientReportNeverReadsOutsideItsScope(t *testing.T) {
	handle, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 370)
	ctx := context.Background()
	userID, orgID, otherOrgID := uuid.New(), uuid.New(), uuid.New()
	inFolder, named, outside, foreign := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	folderID, foreignFolderID := uuid.New(), uuid.New()

	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	exec(`INSERT INTO users (id, first_name, last_name, email) VALUES ($1, 'Report', 'Scope', $2)`,
		userID, "report-scope-"+uuid.NewString()+"@example.test")
	exec(`INSERT INTO organizations (id, name, owner_user_id) VALUES ($1, 'Report scope', $3), ($2, 'Report scope other', $3)`,
		orgID, otherOrgID, userID)
	exec(`INSERT INTO folders (id, user_id, organization_id, title, color, position)
	      VALUES ($1, $3, $4, 'Client A', '#0ea5e9', 0), ($2, $3, $5, 'Other', '#0ea5e9', 0)`,
		folderID, foreignFolderID, userID, orgID, otherOrgID)
	for _, c := range []struct {
		id, org uuid.UUID
		name    string
		class   string
	}{
		{inFolder, orgID, "Client A - Dentists", "positive"},
		{named, orgID, "Client A - Named", "neutral"},
		{outside, orgID, "Client B", "positive"},
		{foreign, otherOrgID, "Foreign", "positive"},
	} {
		contactID, stepID := uuid.New(), uuid.New()
		exec(`INSERT INTO campaigns (id, user_id, organization_id, name, description, days, status, created_at, updated_at)
		      VALUES ($1, $2, $3, $4, '', 62, 'active', NOW(), NOW())`, c.id, userID, c.org, c.name)
		exec(`INSERT INTO contacts (id, user_id, organization_id, email, first_name, last_name, company, phone, custom_fields, subscribed, created_at, updated_at)
		      VALUES ($1, $2, $3, $4, 'Lead', 'Scope', '', '', '{}'::jsonb, true, NOW(), NOW())`,
			contactID, userID, c.org, "report-scope-lead-"+uuid.NewString()+"@example.test")
		exec(`INSERT INTO campaign_leads (campaign_id, contact_id, position) VALUES ($1, $2, 0)`, c.id, contactID)
		exec(`INSERT INTO sequences (id, campaign_id, organization_id, name, subject, body_plain, body_html, wait_after, position, kind, created_at)
		      VALUES ($1, $2, $3, 'Step', '', '', '', 0, 1, 'email', NOW())`, stepID, c.id, c.org)
		exec(`INSERT INTO campaign_contact_progress (campaign_id, contact_id, sequence_id, sent_at, replied_at, reply_class)
		      VALUES ($1, $2, $3, NOW() - interval '1 hour', NOW(), $4)`, c.id, contactID, stepID, c.class)
	}
	// This workspace's folder holds inFolder; the foreign campaign sits in the foreign folder.
	exec(`INSERT INTO campaign_folders (campaign_id, folder_id) VALUES ($1, $2), ($3, $4)`, inFolder, folderID, foreign, foreignFolderID)
	t.Cleanup(func() {
		for _, step := range []struct {
			query string
			arg   any
		}{
			{`DELETE FROM client_reports WHERE organization_id = ANY($1)`, []uuid.UUID{orgID, otherOrgID}},
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

	reports := NewClientReportRepository(pool)
	// The report names its folder and campaign, plus a foreign campaign and a foreign folder.
	access := &models.MemberAccess{Scope: models.AccessScopeRestricted,
		FolderIDs: []uuid.UUID{folderID, foreignFolderID}, CampaignIDs: []uuid.UUID{named, foreign}}
	scope, err := reports.ResolveScope(ctx, orgID, access)
	if err != nil {
		t.Fatalf("ResolveScope: %v", err)
	}
	got := map[uuid.UUID]bool{}
	for _, id := range scope.Campaigns {
		got[id] = true
	}
	if len(got) != 2 || !got[inFolder] || !got[named] {
		t.Fatalf("resolved %v, want only %s and %s", scope.Campaigns, inFolder, named)
	}

	analytics := &analyticsRepository{DB: handle}
	cs := &models.CampaignScope{CampaignIDs: scope.Campaigns}
	from, to := time.Now().Add(-24*time.Hour), time.Now().Add(time.Hour)
	table, xerr := analytics.GetReportCampaignTable(ctx, orgID, from, to, cs, 50)
	if xerr != nil {
		t.Fatalf("GetReportCampaignTable: %v", xerr)
	}
	sent := 0
	for _, row := range table {
		if row.ID != inFolder && row.ID != named {
			t.Fatalf("campaign table includes %s (%s), outside the report", row.ID, row.Name)
		}
		sent += row.Sent
	}
	overall, xerr := analytics.GetDashboardOverallStats(ctx, orgID, from, to, cs, nil)
	if xerr != nil {
		t.Fatalf("GetDashboardOverallStats: %v", xerr)
	}
	if sent != 2 || overall.TotalEmailsSent != sent {
		t.Fatalf("table sent %d, overall %d; want both 2", sent, overall.TotalEmailsSent)
	}
	replies, xerr := analytics.GetReportReplies(ctx, orgID, from, to, cs, false, false, 25)
	if xerr != nil {
		t.Fatalf("GetReportReplies: %v", xerr)
	}
	if len(replies) != 2 {
		t.Fatalf("replies = %+v, want the two in scope", replies)
	}
	positive, xerr := analytics.GetReportReplies(ctx, orgID, from, to, cs, true, false, 25)
	if xerr != nil {
		t.Fatalf("GetReportReplies positive: %v", xerr)
	}
	if len(positive) != 1 || positive[0].Campaign != "Client A - Dentists" || positive[0].Snippet != "" {
		t.Fatalf("positive replies = %+v", positive)
	}
	// Another workspace asking with this workspace's ids reads nothing.
	if table, _ := analytics.GetReportCampaignTable(ctx, otherOrgID, from, to, cs, 50); len(table) != 0 {
		t.Fatalf("another workspace read %+v", table)
	}

	// A share link resolves while live and stops on revoke.
	rep := &models.ClientReport{OrganizationID: orgID, Name: "Acme", Access: *access, Sections: models.DefaultClientReportSections(), DateRange: models.ClientReportLast7Days}
	if err := reports.Create(ctx, rep); err != nil {
		t.Fatalf("Create: %v", err)
	}
	hash := sha256.Sum256([]byte("token-" + uuid.NewString()))
	if ok, err := reports.SetLink(ctx, orgID, rep.ID, hash[:], "abc", time.Now().Add(time.Hour)); err != nil || !ok {
		t.Fatalf("SetLink: %v %v", ok, err)
	}
	if ok, _ := reports.SetLink(ctx, otherOrgID, rep.ID, hash[:], "abc", time.Now().Add(time.Hour)); ok {
		t.Fatal("another workspace set the link")
	}
	hit, err := reports.ResolveToken(ctx, hash[:], time.Now())
	if err != nil || hit == nil || hit.Report.ID != rep.ID {
		t.Fatalf("ResolveToken: %+v %v", hit, err)
	}
	if hit, _ := reports.ResolveToken(ctx, hash[:], time.Now().Add(2*time.Hour)); hit != nil {
		t.Fatal("an expired link resolved")
	}
	if ok, err := reports.RevokeLinks(ctx, orgID, rep.ID); err != nil || !ok {
		t.Fatalf("RevokeLinks: %v %v", ok, err)
	}
	if hit, _ := reports.ResolveToken(ctx, hash[:], time.Now()); hit != nil {
		t.Fatal("a revoked link resolved")
	}
}

// A due schedule runs once per slot however many replicas read it, and its delivery is claimed for one attempt at a time.
func TestLiveClientReportScheduleRunsOncePerSlot(t *testing.T) {
	_, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 370)
	ctx := context.Background()
	userID, orgID := uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, first_name, last_name, email) VALUES ($1, 'Report', 'Schedule', $2)`,
		userID, "report-schedule-"+uuid.NewString()+"@example.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO organizations (id, name, owner_user_id) VALUES ($1, 'Report schedule', $2)`, orgID, userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	})
	reports := NewClientReportRepository(pool)
	rep := &models.ClientReport{OrganizationID: orgID, Name: "Weekly", Access: *models.WorkspaceAccess(), Sections: models.DefaultClientReportSections(), DateRange: models.ClientReportLast7Days}
	if err := reports.Create(ctx, rep); err != nil {
		t.Fatal(err)
	}
	slot := time.Now().Add(-time.Minute).Truncate(time.Second)
	if err := reports.UpsertSchedule(ctx, orgID, rep.ID, &models.ClientReportSchedule{Recipients: []string{"client@example.com"},
		Cadence: models.ClientReportWeekly, Weekday: 1, MonthDay: 1, Hour: 9, Timezone: "UTC", NextRunAt: slot}); err != nil {
		t.Fatal(err)
	}
	due, err := reports.DueSchedules(ctx, time.Now(), 1000)
	if err != nil {
		t.Fatal(err)
	}
	var mine *DueClientReportSchedule
	for i := range due {
		if due[i].ReportID == rep.ID {
			mine = &due[i]
		}
	}
	if mine == nil {
		t.Fatal("the due schedule was not listed")
	}
	next := slot.Add(7 * 24 * time.Hour)
	run := func() bool {
		d := &models.ClientReportDelivery{ID: uuid.New(), RunAt: mine.Schedule.NextRunAt, DateRange: models.ClientReportLast7Days,
			Recipients: mine.Schedule.Recipients, ExpiresAt: time.Now().Add(time.Hour)}
		hash := sha256.Sum256(d.ID[:])
		ok, err := reports.RecordRun(ctx, *mine, next, d, hash[:])
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	// Two replicas holding the same due row: only the first advances it.
	if !run() || run() {
		t.Fatal("a slot ran other than exactly once")
	}
	var deliveries int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM client_report_deliveries WHERE report_id = $1`, rep.ID).Scan(&deliveries); err != nil || deliveries != 1 {
		t.Fatalf("deliveries = %d (%v), want 1", deliveries, err)
	}
	claimed, err := reports.ClaimDeliveries(ctx, time.Now(), 1000, 3)
	if err != nil {
		t.Fatal(err)
	}
	var claim *models.ClientReportDelivery
	for i := range claimed {
		if claimed[i].ReportID == rep.ID {
			claim = &claimed[i]
		}
	}
	if claim == nil || claim.Attempts != 1 || len(claim.TokenHash) != 32 {
		t.Fatalf("claim = %+v", claim)
	}
	again, err := reports.ClaimDeliveries(ctx, time.Now(), 1000, 3)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range again {
		if d.ID == claim.ID {
			t.Fatal("a claimed delivery was claimed again inside its backoff")
		}
	}
	if err := reports.MarkDeliveredTo(ctx, claim.ID, "client@example.com"); err != nil {
		t.Fatal(err)
	}
	sent := time.Now()
	if err := reports.FinishDelivery(ctx, claim.ID, &sent, ""); err != nil {
		t.Fatal(err)
	}
	got, err := reports.Get(ctx, orgID, rep.ID)
	if err != nil || got.Schedule == nil || got.Schedule.LastSentAt == nil || !got.Schedule.NextRunAt.Equal(next) {
		t.Fatalf("schedule after the run = %+v (%v)", got.Schedule, err)
	}
}
