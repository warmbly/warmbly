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
	requireSchemaVersion(t, pool, 230)
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
	due, err := f.repo.ClaimDueMonitors(ctx, time.Now(), 500)
	if err != nil {
		t.Fatalf("ClaimDueMonitors: %v", err)
	}
	found := false
	for _, d := range due {
		found = found || d.ID == m.ID
	}
	if !found {
		t.Fatalf("monitor not due")
	}
	// A second replica asking at the same moment gets nothing: the claim moved it.
	again, err := f.repo.ClaimDueMonitors(ctx, time.Now(), 500)
	if err != nil {
		t.Fatalf("ClaimDueMonitors: %v", err)
	}
	for _, d := range again {
		if d.ID == m.ID {
			t.Fatalf("monitor claimed twice")
		}
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

func TestLivePlacementPaidTestRefundsToThePoolsItDrewFrom(t *testing.T) {
	f := newPlacementFixture(t)
	handle, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 224)
	ctx := context.Background()
	credits := NewCreditRepository(handle)
	f.exec(`INSERT INTO credit_ledger (org_id, balance_micro, purchased_micro) VALUES ($1, 10, 100)`, f.org)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM credit_ledger_transactions WHERE org_id = $1`, f.org)
		_, _ = pool.Exec(context.Background(), `DELETE FROM credit_ledger WHERE org_id = $1`, f.org)
	})

	test, _, _ := f.newTest(f.seeds, time.Now().Add(time.Hour))
	key := "placement:" + test.ID.String()
	if _, _, _, err := credits.Debit(ctx, f.org, models.CreditDebit{Amount: 25, Reason: "placement_test", IdempotencyKey: key}); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	f.exec(`UPDATE placement_tests SET credits_charged = 25 WHERE id = $1`, test.ID)

	since := time.Now().Add(-time.Hour)
	if n, err := f.repo.CountMeteredTests(ctx, f.org, since); err != nil || n != 0 {
		t.Fatalf("CountMeteredTests = %d, %v; a paid test uses no free test", n, err)
	}
	if open, _ := f.repo.UnsettledPaidTests(ctx, 1000); findSettle(open, test.ID) != nil {
		t.Fatalf("a running test is up for settling")
	}

	// Cancelled before any copy left: nothing was delivered, so it is owed.
	if ok, err := f.repo.CancelTest(ctx, f.org, test.ID); err != nil || !ok {
		t.Fatalf("CancelTest = %v, %v", ok, err)
	}
	if _, err := f.repo.FinishTests(ctx); err != nil {
		t.Fatalf("FinishTests: %v", err)
	}
	// A free test that ended without a copy leaving uses no free test.
	free, _, _ := f.newTest(f.seeds, time.Now().Add(time.Hour))
	if n, _ := f.repo.CountMeteredTests(ctx, f.org, since); n != 1 {
		t.Fatalf("CountMeteredTests = %d; want the running free test counted", n)
	}
	if ok, err := f.repo.CancelTest(ctx, f.org, free.ID); err != nil || !ok {
		t.Fatalf("CancelTest = %v, %v", ok, err)
	}
	if _, err := f.repo.FinishTests(ctx); err != nil {
		t.Fatalf("FinishTests: %v", err)
	}
	if n, _ := f.repo.CountMeteredTests(ctx, f.org, since); n != 0 {
		t.Fatalf("CountMeteredTests = %d; a test that delivered nothing uses no free test", n)
	}
	if open, _ := f.repo.UnsettledPaidTests(ctx, 1000); findSettle(open, free.ID) != nil {
		t.Fatalf("a free test is up for settling")
	}

	open, err := f.repo.UnsettledPaidTests(ctx, 1000)
	if got := findSettle(open, test.ID); err != nil || got == nil || got.Delivered {
		t.Fatalf("UnsettledPaidTests = %v, %v; want the cancelled paid test, undelivered", open, err)
	}

	for range 2 {
		refunded, _, err := credits.RefundSpend(ctx, f.org, key, key+":refund", "placement_test_refund")
		if err != nil || refunded != 25 {
			t.Fatalf("RefundSpend = %d, %v; want 25 every time and applied once", refunded, err)
		}
	}
	ledger, err := credits.GetBalance(ctx, f.org)
	if err != nil || ledger.Monthly != 10 || ledger.Purchased != 100 {
		t.Fatalf("ledger = %+v, %v; want 10 monthly and 100 purchased back", ledger, err)
	}
	if ok, err := f.repo.SettleCredits(ctx, test.ID, 25); err != nil || !ok {
		t.Fatalf("SettleCredits = %v, %v", ok, err)
	}
	if ok, _ := f.repo.SettleCredits(ctx, test.ID, 25); ok {
		t.Fatalf("a test was settled twice")
	}
	if open, _ := f.repo.UnsettledPaidTests(ctx, 1000); findSettle(open, test.ID) != nil {
		t.Fatalf("a settled test is still open")
	}
	// Charged in credits only, as the previous release writes it; the refund is recorded in micro-dollars.
	if got, _ := f.repo.GetTest(ctx, test.ID); got == nil || got.RefundedMicro != 25 || got.CreditsSettledAt == nil ||
		got.ChargedMicro != 25*models.LegacyCreditMicro {
		t.Fatalf("test = %+v; want 25 refunded, the credit charge read in dollars, and a settle time", got)
	}
	if n, _, _ := credits.RefundSpend(ctx, f.org, "placement:"+uuid.NewString(), "x:refund", "placement_test_refund"); n != 0 {
		t.Fatalf("refunded %d for a charge that never happened", n)
	}
	day, _, _, err := credits.SpentInWindows(ctx, f.org, since, since, since)
	if err != nil || day != 0 {
		t.Fatalf("SpentInWindows = %d, %v; a refunded charge counts against no spend limit", day, err)
	}
	// A refund nets against its own charge, never against a later window's.
	if day, _, _, _ := credits.SpentInWindows(ctx, f.org, time.Now().Add(time.Minute), since, since); day != 0 {
		t.Fatalf("a window after the refunded charge reads %d", day)
	}

	// Monthly credits spent before a reset would have expired with it, so a
	// refund after the reset gives back only the purchased part.
	key2 := "placement:" + uuid.NewString()
	if _, _, _, err := credits.Debit(ctx, f.org, models.CreditDebit{Amount: 25, Reason: "placement_test", IdempotencyKey: key2}); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	f.exec(`UPDATE credit_ledger SET month_reset_at = NOW() + INTERVAL '1 second' WHERE org_id = $1`, f.org)
	if refunded, _, err := credits.RefundSpend(ctx, f.org, key2, key2+":refund", "placement_test_refund"); err != nil || refunded != 15 {
		t.Fatalf("RefundSpend after a reset = %d, %v; want only the 15 purchased", refunded, err)
	}
	// The 10 monthly credits that expired stay spent in the window.
	if day, _, _, _ := credits.SpentInWindows(ctx, f.org, since, since, since); day != 10 {
		t.Fatalf("SpentInWindows = %d; want the 10 that did not come back", day)
	}
	ledger, _ = credits.GetBalance(ctx, f.org)
	if ledger.Monthly != 0 || ledger.Purchased != 100 {
		t.Fatalf("ledger = %+v; want the reset month untouched and purchased whole", ledger)
	}
}

func findSettle(open []PlacementSettle, id uuid.UUID) *PlacementSettle {
	for i := range open {
		if open[i].ID == id {
			return &open[i]
		}
	}
	return nil
}

func TestLivePlacementDeleteUnsentTestsTakesItsTasks(t *testing.T) {
	f := newPlacementFixture(t)
	ctx := context.Background()
	test, _, tasks := f.newTest(f.seeds, time.Now().Add(time.Hour))
	other, _, _ := f.newTest(f.seeds, time.Now().Add(time.Hour))

	// Another workspace's id deletes nothing.
	if err := f.repo.DeleteUnsentTests(ctx, f.opOrg, []uuid.UUID{test.ID}); err != nil {
		t.Fatalf("DeleteUnsentTests: %v", err)
	}
	if got, _ := f.repo.GetTest(ctx, test.ID); got == nil {
		t.Fatalf("a test was deleted through another workspace")
	}
	if err := f.repo.DeleteUnsentTests(ctx, f.org, []uuid.UUID{test.ID}); err != nil {
		t.Fatalf("DeleteUnsentTests: %v", err)
	}
	if got, _ := f.repo.GetTest(ctx, test.ID); got != nil {
		t.Fatalf("the test survived")
	}
	_, pool := liveContactDB(t)
	ids := make([]uuid.UUID, len(tasks))
	for i, task := range tasks {
		ids[i] = task.ID
	}
	var left int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM tasks WHERE id = ANY($1)`, ids).Scan(&left); err != nil || left != 0 {
		t.Fatalf("%d tasks survived their test (%v)", left, err)
	}
	if got, _ := f.repo.GetTest(ctx, other.ID); got == nil {
		t.Fatalf("an unrelated test was deleted")
	}
}

// A tracking comparison freezes one copy per seed: the first write wins, a
// case-folded address names the same pair, another workspace reads nothing,
// and the copy goes once nothing in its comparison is running.
func TestLivePlacementRenderFreezesOnceAndPrunes(t *testing.T) {
	f := newPlacementFixture(t)
	ctx := context.Background()
	a, _, _ := f.newTest(f.seeds[:1], time.Now().Add(time.Hour))
	b, _, _ := f.newTest(f.seeds[:1], time.Now().Add(time.Hour))
	group := uuid.New()
	f.exec(`UPDATE placement_tests SET compare_group_id = $1 WHERE id IN ($2, $3)`, group, a.ID, b.ID)

	if got, err := f.repo.GetPlacementRender(ctx, f.org, group, "Seed@Gmail.com"); err != nil || got != "" {
		t.Fatalf("empty pair read %q, %v", got, err)
	}
	contents := make(chan string, 8)
	for i := range 8 {
		go func() {
			got, err := f.repo.FreezePlacementRender(ctx, f.org, group, "Seed@Gmail.com", "copy-"+string(rune('a'+i)))
			if err != nil {
				t.Errorf("FreezePlacementRender: %v", err)
			}
			contents <- got
		}()
	}
	first := <-contents
	for range 7 {
		if got := <-contents; got != first {
			t.Fatalf("racing freezes returned %q and %q", first, got)
		}
	}
	if got, _ := f.repo.FreezePlacementRender(ctx, f.org, group, "seed@gmail.com", "later"); got != first {
		t.Fatalf("a later freeze replaced the copy: %q", got)
	}
	if got, _ := f.repo.GetPlacementRender(ctx, f.opOrg, group, "seed@gmail.com"); got != "" {
		t.Fatalf("another workspace read the copy: %q", got)
	}

	if err := f.repo.PruneRenders(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.repo.GetPlacementRender(ctx, f.org, group, "seed@gmail.com"); got != first {
		t.Fatalf("pruned a running comparison's copy")
	}
	f.exec(`UPDATE placement_tests SET status = 'completed', finished_at = NOW() WHERE id = $1`, a.ID)
	_ = f.repo.PruneRenders(ctx)
	if got, _ := f.repo.GetPlacementRender(ctx, f.org, group, "seed@gmail.com"); got != first {
		t.Fatalf("pruned the copy while one half still runs")
	}
	f.exec(`UPDATE placement_tests SET status = 'cancelled', finished_at = NOW() WHERE id = $1`, b.ID)
	_ = f.repo.PruneRenders(ctx)
	if got, _ := f.repo.GetPlacementRender(ctx, f.org, group, "seed@gmail.com"); got != "" {
		t.Fatalf("kept the copy of a finished comparison: %q", got)
	}
}
