package repository

import (
	"context"
	"errors"
	"net/mail"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
)

type testerSampleFixture struct {
	orgID, userID uuid.UUID
	orgs          OrganizationRepository
	users         UserRepository
}

func newTesterSampleFixture(t *testing.T, pool *pgxpool.Pool, users UserRepository) testerSampleFixture {
	t.Helper()
	ctx := context.Background()
	until := time.Now().Add(24 * time.Hour).Truncate(time.Microsecond)
	u, err := users.CreateExemptUser(ctx, &mail.Address{Address: uuid.NewString() + "@example.test"}, "hash", "sample review", nil, until)
	if err != nil {
		t.Fatal(err)
	}
	orgs := NewOrganizationRepository(pool)
	slug := uuid.NewString()
	org := &models.Organization{ID: uuid.New(), OwnerUserID: u.ID, Name: "Sample review workspace", Slug: &slug}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM admin_audit_logs WHERE target_id = $1`, org.ID)
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, org.ID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, u.ID)
	})
	if err := orgs.Create(ctx, org); err != nil {
		t.Fatal(err)
	}
	if err := orgs.ProvisionTesterWorkspace(ctx, org.ID, u.ID, u.ID, "sample review", until); err != nil {
		t.Fatal(err)
	}
	return testerSampleFixture{org.ID, u.ID, orgs, users}
}

func sampleCount(t *testing.T, pool *pgxpool.Pool, query string, args ...any) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(), query, args...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func assertTesterSampleCounts(t *testing.T, pool *pgxpool.Pool, orgID uuid.UUID, multiplier int) {
	t.Helper()
	for _, tc := range []struct {
		query string
		count int
	}{
		{`SELECT count(*) FROM contacts WHERE organization_id = $1`, 6},
		{`SELECT count(*) FROM categories WHERE organization_id = $1`, 1},
		{`SELECT count(*) FROM campaigns WHERE organization_id = $1`, 1},
		{`SELECT count(*) FROM sequences WHERE organization_id = $1`, 2},
		{`SELECT count(*) FROM reply_templates WHERE organization_id = $1`, 2},
		{`SELECT count(*) FROM pipelines WHERE organization_id = $1`, 1},
		{`SELECT count(*) FROM deals WHERE organization_id = $1`, 2},
		{`SELECT count(*) FROM crm_tasks WHERE organization_id = $1`, 1},
		{`SELECT count(*) FROM contact_notes WHERE organization_id = $1`, 1},
		{`SELECT count(*) FROM admin_audit_logs WHERE target_id = $1 AND action = 'seed_tester_workspace'`, 1},
	} {
		if got := sampleCount(t, pool, tc.query, orgID); got != tc.count*multiplier {
			t.Errorf("%s: got %d, want %d", tc.query, got, tc.count*multiplier)
		}
	}
}

func TestLiveTesterSampleDataIsSafeAndPreservesEdits(t *testing.T) {
	db, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 264)
	ctx := context.Background()
	f := newTesterSampleFixture(t, pool, NewUserRepostory(db, nil))
	other := newTesterSampleFixture(t, pool, f.users)
	result, err := f.orgs.SeedTesterWorkspace(ctx, f.orgID, f.userID, "127.0.0.1", "sample-review")
	if err != nil || result == nil || !result.Created || result.SeededAt.IsZero() {
		t.Fatalf("seed = %+v, %v", result, err)
	}
	assertTesterSampleCounts(t, pool, f.orgID, 1)
	assertTesterSampleCounts(t, pool, other.orgID, 0)
	contact, xerr := NewContactRepostory(db).GetByEmailAndOrganization(ctx, f.orgID, "avery@review.invalid")
	if xerr != nil || contact == nil || contact.CustomFields["sample_data"] != "true" {
		t.Fatalf("sample contact cannot be read: %+v, %v", contact, xerr)
	}
	for _, query := range []string{
		`SELECT count(*) FROM contacts WHERE organization_id = $1 AND (subscribed OR email NOT LIKE '%@review.invalid')`,
		`SELECT count(*) FROM campaigns WHERE organization_id = $1 AND (status <> 'draft' OR open_tracking OR link_tracking)`,
		`SELECT count(*) FROM email_accounts WHERE organization_id = $1`,
		`SELECT count(*) FROM campaign_tasks t JOIN campaigns c ON c.id = t.campaign_id WHERE c.organization_id = $1`,
		`SELECT count(*) FROM campaign_contact_progress p JOIN campaigns c ON c.id = p.campaign_id WHERE c.organization_id = $1`,
	} {
		if got := sampleCount(t, pool, query, f.orgID); got != 0 {
			t.Errorf("unexpected side effect: %s = %d", query, got)
		}
	}
	var conditions models.BranchConditions
	var next uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT conditions FROM sequences WHERE organization_id = $1 AND position = 0`, f.orgID).Scan(&conditions); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT id FROM sequences WHERE organization_id = $1 AND position = 1`, f.orgID).Scan(&next); err != nil {
		t.Fatal(err)
	}
	if len(conditions.Branches) != 1 || conditions.Branches[0].TargetSequenceID == nil || *conditions.Branches[0].TargetSequenceID != next {
		t.Fatal("sample campaign steps are not connected")
	}
	var ip, agent string
	if err := pool.QueryRow(ctx, `SELECT ip_address, user_agent FROM admin_audit_logs WHERE target_id = $1 AND action = 'seed_tester_workspace'`, f.orgID).Scan(&ip, &agent); err != nil || ip != "127.0.0.1" || agent != "sample-review" {
		t.Fatalf("audit attribution: %s, %s, %v", ip, agent, err)
	}
	if got := sampleCount(t, pool, `SELECT balance FROM credit_ledger WHERE org_id = $1`, f.orgID); got != 100 {
		t.Fatalf("seeding changed test credits: %d", got)
	}
	list, err := f.users.ListLoginCodeExempt(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tester := range list {
		if tester.UserID == f.userID {
			found = tester.TestWorkspaceID != nil && *tester.TestWorkspaceID == f.orgID && tester.SampleDataSeededAt != nil && tester.SampleDataSeededAt.Equal(result.SeededAt)
		}
	}
	if !found {
		t.Fatal("tester list does not expose the seeded workspace")
	}
	if _, err := pool.Exec(ctx, `UPDATE campaigns SET name = 'Edited sample' WHERE organization_id = $1`, f.orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM contacts WHERE organization_id = $1 AND email = 'taylor@review.invalid'`, f.orgID); err != nil {
		t.Fatal(err)
	}
	retry, err := f.orgs.SeedTesterWorkspace(ctx, f.orgID, f.userID, "", "")
	if err != nil || retry == nil || retry.Created || !retry.SeededAt.Equal(result.SeededAt) {
		t.Fatalf("retry = %+v, %v", retry, err)
	}
	if got := sampleCount(t, pool, `SELECT count(*) FROM campaigns WHERE organization_id = $1 AND name = 'Edited sample'`, f.orgID); got != 1 {
		t.Fatal("retry overwrote edited sample data")
	}
	if got := sampleCount(t, pool, `SELECT count(*) FROM contacts WHERE organization_id = $1`, f.orgID); got != 5 {
		t.Fatal("retry restored deleted sample data")
	}
}

func TestLiveTesterSampleDataRollsBackAndSerializesRetries(t *testing.T) {
	db, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 264)
	ctx := context.Background()
	f := newTesterSampleFixture(t, pool, NewUserRepostory(db, nil))
	if _, err := f.orgs.SeedTesterWorkspace(ctx, f.orgID, uuid.New(), "", ""); err == nil {
		t.Fatal("invalid audit actor must fail the entire transaction")
	}
	assertTesterSampleCounts(t, pool, f.orgID, 0)

	results := make(chan *models.TesterSampleData, 8)
	errors := make(chan error, 8)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			result, err := f.orgs.SeedTesterWorkspace(ctx, f.orgID, f.userID, "", "")
			results <- result
			errors <- err
		})
	}
	wg.Wait()
	close(results)
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	created := 0
	for result := range results {
		if result.Created {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("concurrent actions created %d datasets", created)
	}
	assertTesterSampleCounts(t, pool, f.orgID, 1)
}

func TestLiveTesterSampleDataRejectsNonTestAndInactiveWorkspaces(t *testing.T) {
	db, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 264)
	ctx := context.Background()
	for _, tc := range []struct {
		name, query string
	}{
		{"standard workspace", `UPDATE organizations SET category = 'standard' WHERE id = $1`},
		{"revoked exemption", `UPDATE users SET login_code_exempt = false WHERE id = (SELECT owner_user_id FROM organizations WHERE id = $1)`},
		{"expired grant", `UPDATE subscriptions SET managed_until = now() - interval '1 hour' WHERE organization_id = $1`},
		{"different managed plan", `UPDATE subscriptions SET managed_plan_id = '00000000-0000-0000-0000-000000000001' WHERE organization_id = $1`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newTesterSampleFixture(t, pool, NewUserRepostory(db, nil))
			if _, err := pool.Exec(ctx, tc.query, f.orgID); err != nil {
				t.Fatal(err)
			}
			if testerListEntry(t, f).SampleDataEligible {
				t.Fatal("tester list offers sample data for an ineligible workspace")
			}
			if _, err := f.orgs.SeedTesterWorkspace(ctx, f.orgID, f.userID, "", ""); !errors.Is(err, ErrTesterWorkspaceInactive) {
				t.Fatalf("ineligible workspace returned %v", err)
			}
			assertTesterSampleCounts(t, pool, f.orgID, 0)
		})
	}
}

func testerListEntry(t *testing.T, f testerSampleFixture) models.LoginCodeExemption {
	t.Helper()
	list, err := f.users.ListLoginCodeExempt(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, tester := range list {
		if tester.UserID == f.userID {
			return tester
		}
	}
	return models.LoginCodeExemption{UserID: f.userID}
}

func testerGrantUntil(t *testing.T, pool *pgxpool.Pool, orgID uuid.UUID) time.Time {
	t.Helper()
	var until time.Time
	if err := pool.QueryRow(context.Background(), `SELECT managed_until FROM subscriptions WHERE organization_id = $1`, orgID).Scan(&until); err != nil {
		t.Fatal(err)
	}
	return until
}

// The grant, not the initial password, decides seeding: replacing or reissuing it keeps the workspace eligible.
func TestLiveTesterSampleDataSurvivesInitialPasswordReplacement(t *testing.T) {
	db, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 336)
	ctx := context.Background()
	f := newTesterSampleFixture(t, pool, NewUserRepostory(db, nil))
	auth := NewAuthRepostory(db)
	if _, err := pool.Exec(ctx, `INSERT INTO admin_audit_logs (admin_user_id, action, target_type, target_id, details)
		VALUES ($1, 'create_tester', 'user', $1, '{}'::jsonb)`, f.userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM admin_audit_logs WHERE target_id = $1`, f.userID) })
	grant := testerGrantUntil(t, pool, f.orgID)

	if ok, xerr := auth.ReplaceInitialPassword(ctx, f.userID, "hash", "chosen-hash"); xerr != nil || !ok {
		t.Fatalf("replace = %v, %v", ok, xerr)
	}
	if entry := testerListEntry(t, f); entry.PasswordExpiresAt != nil || !entry.SampleDataEligible {
		t.Fatalf("after replacement: password_expires_at %v, eligible %v", entry.PasswordExpiresAt, entry.SampleDataEligible)
	}
	if _, err := f.users.ReissueTesterPassword(ctx, f.userID, "reissued-hash"); err != nil {
		t.Fatal(err)
	}
	if ok, xerr := auth.ReplaceInitialPassword(ctx, f.userID, "reissued-hash", "second-chosen-hash"); xerr != nil || !ok {
		t.Fatalf("replace reissued = %v, %v", ok, xerr)
	}
	if got := testerGrantUntil(t, pool, f.orgID); !got.Equal(grant) {
		t.Fatalf("grant expiry moved from %v to %v", grant, got)
	}
	if !testerListEntry(t, f).SampleDataEligible {
		t.Fatal("tester list withholds sample data from an active Test workspace")
	}

	result, err := f.orgs.SeedTesterWorkspace(ctx, f.orgID, f.userID, "", "")
	if err != nil || result == nil || !result.Created {
		t.Fatalf("seed after replacement = %+v, %v", result, err)
	}
	assertTesterSampleCounts(t, pool, f.orgID, 1)

	if _, err := f.users.RevokeTester(ctx, f.userID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.orgs.SeedTesterWorkspace(ctx, f.orgID, f.userID, "", ""); !errors.Is(err, ErrTesterWorkspaceInactive) {
		t.Fatalf("revoked tester returned %v", err)
	}
}

// A tester owning several Test workspaces is listed with the one sample data can still be added to.
func TestLiveTesterSampleDataListsTheSeedableWorkspace(t *testing.T) {
	db, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 336)
	ctx := context.Background()
	f := newTesterSampleFixture(t, pool, NewUserRepostory(db, nil))
	second := newTesterSampleFixture(t, pool, f.users)
	for _, q := range []string{
		`UPDATE organizations SET created_at = now() - interval '1 day' WHERE id = $1`,
		`UPDATE subscriptions SET managed_until = now() - interval '1 hour' WHERE organization_id = $1`,
	} {
		if _, err := pool.Exec(ctx, q, f.orgID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE organizations SET owner_user_id = $2 WHERE id = $1`, second.orgID, f.userID); err != nil {
		t.Fatal(err)
	}
	listed := func(want uuid.UUID, seeded bool) {
		t.Helper()
		entry := testerListEntry(t, f)
		if entry.TestWorkspaceID == nil || *entry.TestWorkspaceID != want || !entry.SampleDataEligible || (entry.SampleDataSeededAt != nil) != seeded {
			t.Fatalf("listed %v, eligible %v, seeded %v; want %v", entry.TestWorkspaceID, entry.SampleDataEligible, entry.SampleDataSeededAt, want)
		}
	}

	listed(second.orgID, false)
	if _, err := f.orgs.SeedTesterWorkspace(ctx, second.orgID, f.userID, "", ""); err != nil {
		t.Fatal(err)
	}
	listed(second.orgID, true)
	if _, err := pool.Exec(ctx, `UPDATE subscriptions SET managed_until = now() + interval '1 day' WHERE organization_id = $1`, f.orgID); err != nil {
		t.Fatal(err)
	}
	listed(f.orgID, false)
}
