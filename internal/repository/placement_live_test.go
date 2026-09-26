package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// Run against a migrated scratch database:
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/<db>?sslmode=disable \
//	  go test ./internal/repository/ -run LivePlacement -v

type placementFixture struct {
	t        *testing.T
	repo     PlacementRepository
	tasks    TaskRepository
	exec     func(string, ...any)
	owner    uuid.UUID
	org      uuid.UUID
	sender   uuid.UUID
	opOwner  uuid.UUID
	opOrg    uuid.UUID
	seeds    []uuid.UUID
	sameHost uuid.UUID
}

func newPlacementFixture(t *testing.T) *placementFixture {
	t.Helper()
	handle, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 217)
	ctx := context.Background()
	f := &placementFixture{
		t:     t,
		repo:  NewPlacementRepository(handle),
		tasks: NewTaskRepository(pool),
		owner: uuid.New(), org: uuid.New(), sender: uuid.New(),
		opOwner: uuid.New(), opOrg: uuid.New(), sameHost: uuid.New(),
	}
	f.exec = func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	tag := uuid.NewString()[:8]
	for _, u := range []struct {
		id  uuid.UUID
		org uuid.UUID
	}{{f.owner, f.org}, {f.opOwner, f.opOrg}} {
		f.exec(`INSERT INTO users (id, first_name, last_name, email) VALUES ($1, 'Placement', 'Live', $2)`,
			u.id, "placement-"+uuid.NewString()+"@example.test")
		f.exec(`INSERT INTO organizations (id, name, owner_user_id) VALUES ($1, 'Placement live', $2)`, u.org, u.id)
	}
	mailbox := func(id, user, org uuid.UUID, address, provider string) {
		f.exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain, signature_html, provider, campaign_limit)
		        VALUES ($1, $2, $3, $4, 'Placement', '', '', $5, 50)`, id, user, org, address, provider)
	}
	mailbox(f.sender, f.owner, f.org, "sender-"+tag+"@acme-"+tag+".test", "gmail")
	for i := range 2 {
		id := uuid.New()
		f.seeds = append(f.seeds, id)
		mailbox(id, f.opOwner, f.opOrg, "seed"+string(rune('a'+i))+"-"+tag+"@gmail.com", "gmail")
	}
	mailbox(f.sameHost, f.opOwner, f.opOrg, "seed-"+tag+"@acme-"+tag+".test", "gmail")
	t.Cleanup(func() {
		c := context.Background()
		for _, q := range []struct {
			sql string
			arg uuid.UUID
		}{
			{`DELETE FROM placement_monitors WHERE organization_id = $1`, f.org},
			{`DELETE FROM placement_tests WHERE organization_id = $1`, f.org},
			{`DELETE FROM tasks WHERE email_account_id = $1`, f.sender},
			{`DELETE FROM unibox_emails WHERE user_id = $1`, f.opOwner},
			{`DELETE FROM email_accounts WHERE organization_id = $1`, f.org},
			{`DELETE FROM email_accounts WHERE organization_id = $1`, f.opOrg},
			{`DELETE FROM organizations WHERE id = $1`, f.org},
			{`DELETE FROM organizations WHERE id = $1`, f.opOrg},
			{`DELETE FROM users WHERE id = $1`, f.owner},
			{`DELETE FROM users WHERE id = $1`, f.opOwner},
		} {
			if _, err := pool.Exec(c, q.sql, q.arg); err != nil {
				t.Errorf("cleanup: %v", err)
			}
		}
	})
	return f
}

// newTest writes a running test with one scheduled probe per seed.
func (f *placementFixture) newTest(seeds []uuid.UUID, scheduled time.Time) (models.PlacementTest, []models.PlacementResult, []Task) {
	f.t.Helper()
	sender := f.sender
	test := models.PlacementTest{
		ID: uuid.New(), OrganizationID: &f.org, SenderAccountID: &sender, SenderEmail: "sender@example.test",
		Subject: "Quick question", BodyPlain: "Hi there", Origin: models.PlacementOriginManual,
		Panel: models.PlacementPanelInstance, Status: models.PlacementStatusRunning,
	}
	var results []models.PlacementResult
	var tasks []Task
	for i, seed := range seeds {
		seed := seed
		taskID := uuid.New()
		at := scheduled.Add(time.Duration(i) * time.Second)
		tasks = append(tasks, Task{ID: taskID, TaskType: "placement", EmailAccountID: f.sender, Status: "pending", ScheduledAt: &at})
		results = append(results, models.PlacementResult{
			ID: uuid.New(), SeedAccountID: &seed, SeedAddress: seed.String() + "@gmail.com", Family: "gmail",
			TaskID: &taskID, Folder: models.PlacementFolderPending, ScheduledAt: &at,
		})
	}
	if err := f.repo.CreateTest(context.Background(), &test, results, tasks); err != nil {
		f.t.Fatalf("CreateTest: %v", err)
	}
	return test, results, tasks
}

func TestLivePlacementProbesResolveByMessageID(t *testing.T) {
	f := newPlacementFixture(t)
	ctx := context.Background()
	now := time.Now()

	for _, id := range append([]uuid.UUID{f.sameHost}, f.seeds...) {
		if err := f.repo.SetSeedScope(ctx, id, models.SeedScopeInstance); err != nil {
			t.Fatalf("SetSeedScope: %v", err)
		}
	}
	if scope, err := f.repo.SeedScope(ctx, f.seeds[0]); err != nil || scope != models.SeedScopeInstance {
		t.Fatalf("SeedScope = %q, %v", scope, err)
	}
	all, err := f.repo.ListSeeds(ctx, models.SeedScopeInstance, &f.opOrg, false)
	if err != nil || len(all) != 3 {
		t.Fatalf("ListSeeds = %d, %v; want 3", len(all), err)
	}
	// None is on a worker, so none can take a test.
	if live, err := f.repo.ListSeeds(ctx, models.SeedScopeInstance, &f.opOrg, true); err != nil || len(live) != 0 {
		t.Fatalf("active ListSeeds = %d, %v; want 0 without a worker", len(live), err)
	}

	test, results, tasks := f.newTest(f.seeds, now.Add(-3*time.Hour))
	probe, err := f.repo.GetProbeByTask(ctx, tasks[0].ID)
	if err != nil || probe == nil || probe.Result.ID != results[0].ID || probe.Test.ID != test.ID {
		t.Fatalf("GetProbeByTask = %+v, %v", probe, err)
	}
	if busy, err := f.repo.SenderBusy(ctx, f.sender); err != nil || !busy {
		t.Fatalf("SenderBusy = %v, %v; want true with unsent probes", busy, err)
	}

	// Probe one arrived in spam with brackets stripped on the seed's side;
	// probe two left three hours ago and never showed up.
	if err := f.repo.MarkProbeSent(ctx, results[0].ID, "<one-"+test.ID.String()+"@acme.test>", now.Add(-10*time.Minute)); err != nil {
		t.Fatalf("MarkProbeSent: %v", err)
	}
	if err := f.repo.MarkProbeSent(ctx, results[1].ID, "<two-"+test.ID.String()+"@acme.test>", now.Add(-3*time.Hour)); err != nil {
		t.Fatalf("MarkProbeSent: %v", err)
	}
	f.exec(`INSERT INTO unibox_emails (id, user_id, email_id, folder, provider_folder, message_id, thread_id,
	            from_addr, subject, body_text, in_reply_to, internal_date, flags)
	        VALUES ($1, $2, $3, 'spam', 'spam', $4, $5, ARRAY['sender@acme.test'], 'Quick question', 'Hi there', '{}', $6, ARRAY['CATEGORY_PROMOTIONS'])`,
		uuid.New(), f.opOwner, f.seeds[0], "one-"+test.ID.String()+"@acme.test", uuid.NewString(), now.Add(-9*time.Minute))

	landings, err := f.repo.FindLandings(ctx, 100)
	if err != nil {
		t.Fatalf("FindLandings: %v", err)
	}
	var mine []PlacementLanding
	for _, l := range landings {
		if l.TestID == test.ID {
			mine = append(mine, l)
		}
	}
	if len(mine) != 1 || mine[0].ResultID != results[0].ID || mine[0].Folder != "spam" {
		t.Fatalf("landings = %+v; want probe one in spam", mine)
	}
	folder := models.ClassifyPlacementLanding(mine[0].Folder, mine[0].Flags)
	if folder != models.PlacementFolderSpam {
		t.Fatalf("classified %q, want spam over the Promotions label", folder)
	}
	if err := f.repo.RecordLanding(ctx, mine[0].ResultID, folder, "", now); err != nil {
		t.Fatalf("RecordLanding: %v", err)
	}
	if err := f.repo.ExpireProbes(ctx, now.Add(-2*time.Hour), now.Add(-2*time.Hour), now.Add(-6*time.Hour)); err != nil {
		t.Fatalf("ExpireProbes: %v", err)
	}

	finished, err := f.repo.FinishTests(ctx)
	if err != nil {
		t.Fatalf("FinishTests: %v", err)
	}
	var closed *PlacementFinished
	for i := range finished {
		if finished[i].ID == test.ID {
			closed = &finished[i]
		}
	}
	if closed == nil || closed.Status != models.PlacementStatusCompleted {
		t.Fatalf("FinishTests closed %+v; want the test completed", closed)
	}
	byTest, err := f.repo.ListResults(ctx, []uuid.UUID{test.ID})
	if err != nil {
		t.Fatalf("ListResults: %v", err)
	}
	var counts models.PlacementCounts
	for _, r := range byTest[test.ID] {
		counts.Add(r.Folder)
	}
	counts.Finish()
	if counts.Spam != 1 || counts.Missing != 1 || counts.Delivered != 2 {
		t.Fatalf("counts = %+v; want one spam and one missing", counts)
	}
}

func TestLivePlacementProbesChargeTheSendersDay(t *testing.T) {
	f := newPlacementFixture(t)
	ctx := context.Background()

	_, results, tasks := f.newTest(f.seeds[:1], time.Now())
	f.exec(`UPDATE tasks SET status = 'completed', completed_at = NOW(), message_id = '<probe@acme.test>' WHERE id = $1`, tasks[0].ID)
	if err := f.repo.MarkProbeSent(ctx, results[0].ID, "<probe@acme.test>", time.Now()); err != nil {
		t.Fatalf("MarkProbeSent: %v", err)
	}

	sent, err := f.tasks.CountCampaignEmailsSentToday(ctx, f.sender)
	if err != nil || sent != 1 {
		t.Fatalf("CountCampaignEmailsSentToday = %d, %v; want the probe counted", sent, err)
	}
	byAccount, err := f.tasks.CountCampaignEmailsSentTodayByAccounts(ctx, []uuid.UUID{f.sender})
	if err != nil || byAccount[f.sender] != 1 {
		t.Fatalf("CountCampaignEmailsSentTodayByAccounts = %v, %v; want 1", byAccount, err)
	}
	if copy, err := f.repo.IsProbeSentCopy(ctx, f.sender, "probe@acme.test"); err != nil || !copy {
		t.Fatalf("IsProbeSentCopy = %v, %v; want the sender's own copy recognised", copy, err)
	}
	if copy, err := f.repo.IsProbeSentCopy(ctx, f.seeds[0], "probe@acme.test"); err != nil || copy {
		t.Fatalf("IsProbeSentCopy on the seed = %v, %v; a seed's copy must be kept", copy, err)
	}
	if n, err := f.repo.CountMeteredTests(ctx, f.org, time.Now().Add(-time.Hour)); err != nil || n != 1 {
		t.Fatalf("CountMeteredTests = %d, %v; want 1", n, err)
	}
}

func TestLivePlacementCancelStopsUnsentProbes(t *testing.T) {
	f := newPlacementFixture(t)
	ctx := context.Background()

	test, results, tasks := f.newTest(f.seeds, time.Now().Add(time.Hour))
	if err := f.repo.MarkProbeSent(ctx, results[0].ID, "<sent@acme.test>", time.Now()); err != nil {
		t.Fatalf("MarkProbeSent: %v", err)
	}
	if ok, err := f.repo.CancelTest(ctx, f.opOrg, test.ID); err != nil || ok {
		t.Fatalf("CancelTest from another workspace = %v, %v; want refused", ok, err)
	}
	if ok, err := f.repo.CancelTest(ctx, f.org, test.ID); err != nil || !ok {
		t.Fatalf("CancelTest = %v, %v", ok, err)
	}
	byTest, err := f.repo.ListResults(ctx, []uuid.UUID{test.ID})
	if err != nil {
		t.Fatalf("ListResults: %v", err)
	}
	folders := map[uuid.UUID]string{}
	for _, r := range byTest[test.ID] {
		folders[r.ID] = r.Folder
	}
	if folders[results[0].ID] != models.PlacementFolderPending || folders[results[1].ID] != models.PlacementFolderCancelled {
		t.Fatalf("folders = %v; want the sent probe still pending and the unsent one cancelled", folders)
	}
	task, err := f.tasks.GetTask(ctx, tasks[1].ID)
	if err != nil || task == nil || task.Status != "cancelled" {
		t.Fatalf("unsent task = %+v, %v; want cancelled", task, err)
	}
	if running, err := f.repo.CountRunning(ctx, f.org); err != nil || running != 0 {
		t.Fatalf("CountRunning = %d, %v; want 0 after cancel", running, err)
	}
}

func TestLivePlacementMonitorRoundTrip(t *testing.T) {
	f := newPlacementFixture(t)
	ctx := context.Background()
	campaignID := uuid.New()
	f.exec(`INSERT INTO campaigns (id, user_id, organization_id, name, description, days, status, updated_at, created_at)
	        VALUES ($1, $2, $3, 'Placement live', '', 62, 'active', NOW(), NOW())`, campaignID, f.owner, f.org)

	m := &models.PlacementMonitor{
		OrganizationID: f.org, CampaignID: campaignID, CreatedBy: &f.owner, Enabled: true,
		IntervalDays: 7, Panel: models.PlacementPanelInstance, AlertBelow: 70, NextRunAt: time.Now().Add(-time.Minute),
	}
	if err := f.repo.UpsertMonitor(ctx, m); err != nil {
		t.Fatalf("UpsertMonitor: %v", err)
	}
	// Another workspace cannot take the campaign's monitor over.
	stolen := *m
	stolen.ID, stolen.OrganizationID = uuid.New(), f.opOrg
	if err := f.repo.UpsertMonitor(ctx, &stolen); err == nil {
		t.Fatalf("UpsertMonitor from another workspace succeeded")
	}
	due, err := f.repo.ListDueMonitors(ctx, time.Now(), 500)
	if err != nil {
		t.Fatalf("ListDueMonitors: %v", err)
	}
	found := false
	for _, d := range due {
		found = found || d.ID == m.ID
	}
	if !found {
		t.Fatalf("monitor not due")
	}
	next := time.Now().Add(7 * 24 * time.Hour)
	if err := f.repo.MarkMonitorRun(ctx, m.ID, next, nil, &f.sender, ""); err != nil {
		t.Fatalf("MarkMonitorRun: %v", err)
	}
	got, err := f.repo.GetMonitor(ctx, f.org, campaignID)
	if err != nil || got == nil || got.LastSenderID == nil || *got.LastSenderID != f.sender || got.LastRunAt == nil {
		t.Fatalf("GetMonitor = %+v, %v", got, err)
	}
	if ok, err := f.repo.DeleteMonitor(ctx, f.opOrg, campaignID); err != nil || ok {
		t.Fatalf("DeleteMonitor from another workspace = %v, %v", ok, err)
	}
	if ok, err := f.repo.DeleteMonitor(ctx, f.org, campaignID); err != nil || !ok {
		t.Fatalf("DeleteMonitor = %v, %v", ok, err)
	}
	f.exec(`DELETE FROM campaigns WHERE id = $1`, campaignID)
}
