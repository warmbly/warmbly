package placement

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/credits"
	"github.com/warmbly/warmbly/internal/app/instancesettings"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/tasks/proto"
)

func TestLegacyCloudLinkCannotStartNewPlacementTests(t *testing.T) {
	svc := &service{}
	if _, xerr := svc.RemoteStart(context.Background(), &models.PoolLinkInstance{ID: uuid.New()}, models.PlacementCloudStartRequest{Tests: 1}); xerr == nil || xerr.Identifier != "pool_link_workspace_required" {
		t.Fatalf("legacy placement = %v", xerr)
	}
}

// Each fake embeds the interface it stands in for, so a call the test does
// not expect panics instead of passing silently.

type fakeRepo struct {
	repository.PlacementRepository
	seeds    []repository.SeedAccount
	scopes   map[uuid.UUID]string
	running  int
	busy     bool
	metered  int
	created  []models.PlacementTest
	results  [][]models.PlacementResult
	tasks    [][]repository.Task
	failures []uuid.UUID
	failSave bool
}

func (f *fakeRepo) SeedScope(_ context.Context, id uuid.UUID) (string, error) {
	return f.scopes[id], nil
}
func (f *fakeRepo) CountRunning(context.Context, uuid.UUID) (int, error) { return f.running, nil }
func (f *fakeRepo) SenderBusy(context.Context, uuid.UUID) (bool, error)  { return f.busy, nil }
func (f *fakeRepo) CountMeteredTests(context.Context, uuid.UUID, time.Time) (int, error) {
	return f.metered, nil
}
func (f *fakeRepo) SampleLead(context.Context, uuid.UUID) (*uuid.UUID, error) { return nil, nil }
func (f *fakeRepo) PruneRenders(context.Context) error                        { return nil }
func (f *fakeRepo) ListSeeds(_ context.Context, scope string, org *uuid.UUID, activeOnly bool) ([]repository.SeedAccount, error) {
	var out []repository.SeedAccount
	for _, s := range f.seeds {
		if activeOnly && (s.Status != "active" || s.WorkerID == nil) {
			continue
		}
		if s.SeedScope == scope && (org == nil || (s.OrganizationID != nil && *s.OrganizationID == *org)) {
			out = append(out, s)
		}
	}
	return out, nil
}
func (f *fakeRepo) CreateTest(_ context.Context, t *models.PlacementTest, results []models.PlacementResult, tasks []repository.Task) error {
	f.created = append(f.created, *t)
	f.results = append(f.results, results)
	f.tasks = append(f.tasks, tasks)
	return nil
}
func (f *fakeRepo) CreateTests(ctx context.Context, bundles []repository.PlacementBundle) error {
	if f.failSave {
		return errors.New("write failed")
	}
	for _, b := range bundles {
		_ = f.CreateTest(ctx, b.Test, b.Results, b.Tasks)
	}
	return nil
}
func (f *fakeRepo) DeleteUnsentTests(_ context.Context, _ uuid.UUID, ids []uuid.UUID) error {
	kept := f.created[:0]
	for _, t := range f.created {
		if !slices.Contains(ids, t.ID) {
			kept = append(kept, t)
		}
	}
	f.created = kept
	return nil
}
func (f *fakeRepo) FailProbe(_ context.Context, id uuid.UUID, _ string) error {
	f.failures = append(f.failures, id)
	return nil
}

type fakeEmails struct {
	repository.EmailRepository
	accounts map[uuid.UUID]*models.Email
}

func (f *fakeEmails) GetByID(_ context.Context, id uuid.UUID) (*models.Email, *errx.Error) {
	if a, ok := f.accounts[id]; ok {
		return a, nil
	}
	return nil, errx.ErrNotFound
}

type fakeTasks struct {
	repository.TaskRepository
	sentToday int
}

func (f *fakeTasks) CountCampaignEmailsSentToday(context.Context, uuid.UUID) (int, error) {
	return f.sentToday, nil
}

type fakeScheduler struct{ scheduled int }

func (f *fakeScheduler) CreateTask(context.Context, *proto.ProcessTask, time.Time) (string, error) {
	f.scheduled++
	return "", nil
}
func (f *fakeScheduler) DeleteTask(context.Context, string) error { return nil }

type fakePolicy struct{ p instancesettings.Placement }

func (f fakePolicy) PlacementPolicy(context.Context) instancesettings.Placement { return f.p }

type harness struct {
	svc    *service
	repo   *fakeRepo
	tasks  *fakeTasks
	sched  *fakeScheduler
	org    uuid.UUID
	sender uuid.UUID
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	t.Setenv("DEPLOYMENT_MODE", "self_hosted")
	org, operator, sender, worker := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	h := &harness{org: org, sender: sender, repo: &fakeRepo{scopes: map[uuid.UUID]string{}}, tasks: &fakeTasks{}, sched: &fakeScheduler{}}
	seed := func(addr, host string) repository.SeedAccount {
		return repository.SeedAccount{ID: uuid.New(), OrganizationID: &operator, Email: addr, Provider: "smtp_imap",
			MailHost: host, Status: "active", WorkerID: &worker, SeedScope: models.SeedScopeInstance}
	}
	h.repo.seeds = []repository.SeedAccount{
		seed("a@gmail.com", "gmail"), seed("b@gmail.com", "gmail"), seed("c@gmail.com", "gmail"),
		seed("d@contoso.test", "microsoft365"), seed("e@yahoo.com", "yahoo"),
		// On the sender's own domain: never a seed for this sender.
		seed("f@acme.test", "google_workspace"),
	}
	emails := &fakeEmails{accounts: map[uuid.UUID]*models.Email{
		sender: {ID: sender, OrganizationID: &org, Email: "rep@acme.test", Status: "active", WorkerID: &worker, CampaignLimit: 50},
	}}
	policy := instancesettings.DefaultPlacement()
	policy.SeedsPerTest = 4
	h.svc = &service{Deps: Deps{
		Repo: h.repo, Emails: emails, Tasks: h.tasks, Scheduler: h.sched, Policy: fakePolicy{policy},
	}, now: time.Now}
	return h
}

func (h *harness) input() CreateInput {
	return CreateInput{OrgID: h.org, SenderAccountID: h.sender, Subject: "Quick question", BodyPlain: "Hi there"}
}

func TestCreateTestsPicksAcrossFamiliesAndSkipsTheSendersDomain(t *testing.T) {
	h := newHarness(t)
	views, xerr := h.svc.CreateTests(context.Background(), h.input())
	if xerr != nil {
		t.Fatalf("CreateTests: %v", xerr)
	}
	if len(views) != 1 || len(h.repo.results[0]) != 4 {
		t.Fatalf("got %d tests and %d probes; want one test with the 4-seed cap", len(views), len(h.repo.results[0]))
	}
	families := map[string]int{}
	for _, r := range h.repo.results[0] {
		if strings.HasSuffix(r.SeedAddress, "@acme.test") {
			t.Fatalf("seed %s is on the sender's domain", r.SeedAddress)
		}
		families[r.Family]++
	}
	if families["gmail"] != 2 || families["microsoft365"] != 1 || families["yahoo"] != 1 {
		t.Fatalf("families = %v; want every family before a second gmail", families)
	}
	if h.sched.scheduled != 4 {
		t.Fatalf("scheduled %d tasks, want 4", h.sched.scheduled)
	}
	// One task per probe, all from the sender, in order and spaced out.
	tasks := h.repo.tasks[0]
	for i, task := range tasks {
		if task.TaskType != "placement" || task.EmailAccountID != h.sender || *h.repo.results[0][i].TaskID != task.ID {
			t.Fatalf("task %d = %+v; want the sender's placement task behind probe %d", i, task, i)
		}
		if i > 0 && !task.ScheduledAt.After(*tasks[i-1].ScheduledAt) {
			t.Fatalf("probes %d and %d are not spaced", i-1, i)
		}
	}
}

func TestCreateTestsCompareSendsBothVariantsToTheSameSeeds(t *testing.T) {
	h := newHarness(t)
	in := h.input()
	in.Tracking = models.PlacementTrackingCompare
	views, xerr := h.svc.CreateTests(context.Background(), in)
	if xerr != nil {
		t.Fatalf("CreateTests: %v", xerr)
	}
	if len(views) != 2 || views[0].CompareGroupID == nil || views[1].CompareGroupID == nil || *views[0].CompareGroupID != *views[1].CompareGroupID {
		t.Fatalf("want two tests in one comparison group, got %+v", views)
	}
	if views[0].Tracked() == views[1].Tracked() {
		t.Fatalf("want one tracked and one untracked test")
	}
	seeds := func(i int) string {
		var out []string
		for _, r := range h.repo.results[i] {
			out = append(out, r.SeedAddress)
		}
		return strings.Join(out, ",")
	}
	if seeds(0) != seeds(1) {
		t.Fatalf("variants went to different seeds: %s vs %s", seeds(0), seeds(1))
	}
}

func TestCreateTestsRefusals(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(*harness, *CreateInput)
		wantID string
	}{
		{"sender in another workspace", func(h *harness, in *CreateInput) { in.OrgID = uuid.New() }, ""},
		{"sender is a seed", func(h *harness, in *CreateInput) { h.repo.scopes[h.sender] = models.SeedScopeWorkspace }, "placement_sender_unavailable"},
		{"sender still sending a test", func(h *harness, in *CreateInput) { h.repo.busy = true }, "placement_sender_busy"},
		{"too many running", func(h *harness, in *CreateInput) { h.repo.running = 3 }, "placement_too_many_running"},
		{"daily limit spent", func(h *harness, in *CreateInput) { h.tasks.sentToday = 48 }, "placement_daily_budget"},
		{"no own seeds", func(h *harness, in *CreateInput) { in.Panel = models.PlacementPanelWorkspace }, "placement_no_seeds"},
		{"no cloud link", func(h *harness, in *CreateInput) { in.Panel = models.PlacementPanelCloud }, "placement_panel_unavailable"},
		{"empty body", func(h *harness, in *CreateInput) { in.BodyPlain, in.BodyHTML = "", "<div></div>" }, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			in := h.input()
			c.setup(h, &in)
			_, xerr := h.svc.CreateTests(context.Background(), in)
			if xerr == nil {
				t.Fatalf("CreateTests succeeded; want a refusal")
			}
			if c.wantID != "" && xerr.Identifier != c.wantID {
				t.Fatalf("refused with %q (%s); want %q", xerr.Identifier, xerr.Message, c.wantID)
			}
			if len(h.repo.created) != 0 || h.sched.scheduled != 0 {
				t.Fatalf("a refused test wrote %d tests and scheduled %d tasks", len(h.repo.created), h.sched.scheduled)
			}
		})
	}
}

func TestCreateTestsMetersTheInstancePanelOnTheHostedProduct(t *testing.T) {
	h := newHarness(t)
	t.Setenv("DEPLOYMENT_MODE", "cloud")
	h.repo.metered = 3 // the trial allowance
	_, xerr := h.svc.CreateTests(context.Background(), h.input())
	if xerr == nil || xerr.Identifier != "placement_quota_exceeded" {
		t.Fatalf("got %v; want placement_quota_exceeded", xerr)
	}
	in := h.input()
	in.Origin = models.PlacementOriginAdmin
	if _, xerr := h.svc.CreateTests(context.Background(), in); xerr != nil {
		t.Fatalf("an operator's test is not metered: %v", xerr)
	}
}

func TestSummaryAndMasking(t *testing.T) {
	var c models.PlacementCounts
	for _, f := range []string{"inbox", "inbox", "promotions", "spam", "missing", "failed", "pending"} {
		c.Add(f)
	}
	c.Finish()
	if c.Delivered != 5 || percent(c.InboxRate) != 40 || percent(c.TabsRate) != 20 {
		t.Fatalf("counts = %+v", c)
	}
	if got := summaryLine(c); !strings.HasPrefix(got, "2 of 5 copies reached the inbox") {
		t.Fatalf("summary = %q", got)
	}
	if got := maskAddress("seed.one@gmail.com"); got != "s***@gmail.com" {
		t.Fatalf("mask = %q", got)
	}
	for range 200 {
		if d := jitter(30 * time.Second); d < 20*time.Second || d > 40*time.Second {
			t.Fatalf("jitter %v outside a third of 30s", d)
		}
	}
}

func TestCreateTestsShrinksToTheSendersDayAndRefusesBelowTheFloor(t *testing.T) {
	h := newHarness(t)
	operator, worker := uuid.New(), uuid.New()
	for _, addr := range []string{"g@gmail.com", "h@gmail.com", "i@gmail.com"} {
		h.repo.seeds = append(h.repo.seeds, repository.SeedAccount{ID: uuid.New(), OrganizationID: &operator, Email: addr,
			Provider: "smtp_imap", MailHost: "gmail", Status: "active", WorkerID: &worker, SeedScope: models.SeedScopeInstance})
	}
	policy := instancesettings.DefaultPlacement()
	policy.SeedsPerTest = 8
	h.svc.Policy = fakePolicy{policy}

	h.tasks.sentToday = 44 // six sends left of fifty
	if _, xerr := h.svc.CreateTests(context.Background(), h.input()); xerr != nil {
		t.Fatalf("CreateTests: %v", xerr)
	}
	if got := len(h.repo.results[0]); got != 6 {
		t.Fatalf("sent to %d seeds; want the six the day can pay for", got)
	}

	in := h.input()
	in.Tracking = models.PlacementTrackingCompare // three per half is under the five-seed floor
	h.repo.created = nil
	if _, xerr := h.svc.CreateTests(context.Background(), in); xerr == nil || xerr.Identifier != "placement_daily_budget" {
		t.Fatalf("got %v; want placement_daily_budget", xerr)
	}
	if len(h.repo.created) != 0 {
		t.Fatalf("a refused comparison wrote %d tests", len(h.repo.created))
	}
}

func TestCreateTestsSendsOnlyToTheChosenSeeds(t *testing.T) {
	h := newHarness(t)
	worker := uuid.New()
	own := func(addr, host string) repository.SeedAccount {
		return repository.SeedAccount{ID: uuid.New(), OrganizationID: &h.org, Email: addr, Provider: "smtp_imap",
			MailHost: host, Status: "active", WorkerID: &worker, SeedScope: models.SeedScopeWorkspace}
	}
	gmail, outlook, yahoo, onDomain := own("me@gmail.com", "gmail"), own("me@outlook.com", "outlook"), own("me@yahoo.com", "yahoo"), own("seed@acme.test", "google_workspace")
	h.repo.seeds = append(h.repo.seeds, gmail, outlook, yahoo, onDomain)

	in := h.input()
	in.Panel = models.PlacementPanelWorkspace
	in.SeedIDs = []uuid.UUID{gmail.ID, yahoo.ID, gmail.ID}
	if _, xerr := h.svc.CreateTests(context.Background(), in); xerr != nil {
		t.Fatalf("CreateTests: %v", xerr)
	}
	got := map[string]bool{}
	for _, r := range h.repo.results[0] {
		got[r.SeedAddress] = true
	}
	if len(h.repo.results[0]) != 2 || !got["me@gmail.com"] || !got["me@yahoo.com"] {
		t.Fatalf("sent to %v; want exactly the two chosen seeds", got)
	}

	refused := []struct {
		name   string
		panel  string
		ids    []uuid.UUID
		wantID string
	}{
		{"another workspace's seed", models.PlacementPanelWorkspace, []uuid.UUID{gmail.ID, h.repo.seeds[0].ID}, "placement_invalid_seeds"},
		{"not a seed at all", models.PlacementPanelWorkspace, []uuid.UUID{h.sender}, "placement_invalid_seeds"},
		{"only the sender's domain", models.PlacementPanelWorkspace, []uuid.UUID{onDomain.ID}, "placement_no_seeds"},
		{"instance panel", models.PlacementPanelInstance, []uuid.UUID{gmail.ID}, ""},
	}
	for _, c := range refused {
		t.Run(c.name, func(t *testing.T) {
			h.repo.created = nil
			in := h.input()
			in.Panel, in.SeedIDs = c.panel, c.ids
			_, xerr := h.svc.CreateTests(context.Background(), in)
			if xerr == nil || xerr.Identifier != c.wantID {
				t.Fatalf("got %v; want a refusal %q", xerr, c.wantID)
			}
			if len(h.repo.created) != 0 {
				t.Fatalf("a refused test wrote %d tests", len(h.repo.created))
			}
		})
	}
}

type fakeCredits struct {
	balance  int
	charges  map[string]int
	refunded map[string]bool
	refuse   bool
}

func (f *fakeCredits) Unmetered() bool { return false }
func (f *fakeCredits) GetBalance(context.Context, uuid.UUID) (int, *errx.Error) {
	return f.balance, nil
}
func (f *fakeCredits) Charge(_ context.Context, _ uuid.UUID, amount int, _ string, key string) (int, error) {
	if f.refuse || f.balance < amount {
		return 0, credits.ErrInsufficientCredits
	}
	f.balance -= amount
	f.charges[key] = amount
	return f.balance, nil
}
func (f *fakeCredits) RefundCharge(_ context.Context, _ uuid.UUID, key, _ string) (int, error) {
	if f.refunded[key] {
		return 0, nil
	}
	f.refunded[key] = true
	f.balance += f.charges[key]
	return f.charges[key], nil
}

func TestCreateTestsQuickPaceSendsSecondsApart(t *testing.T) {
	h := newHarness(t)
	in := h.input()
	in.Pace = models.PlacementPaceQuick
	views, xerr := h.svc.CreateTests(context.Background(), in)
	if xerr != nil {
		t.Fatalf("CreateTests: %v", xerr)
	}
	if views[0].Pace != models.PlacementPaceQuick {
		t.Fatalf("pace = %q; want quick", views[0].Pace)
	}
	tasks := h.repo.tasks[0]
	for i := 1; i < len(tasks); i++ {
		if gap := tasks[i].ScheduledAt.Sub(*tasks[i-1].ScheduledAt); gap <= 0 || gap > 11*time.Second {
			t.Fatalf("gap %d = %v; want a few seconds", i, gap)
		}
	}
	in.Pace = "burst"
	if _, xerr := h.svc.CreateTests(context.Background(), in); xerr == nil {
		t.Fatalf("an unknown pace was accepted")
	}
}

func TestCreateTestsKeepsOnlyTheChosenProviders(t *testing.T) {
	h := newHarness(t)
	in := h.input()
	in.Families = []string{" Yahoo ", "microsoft365"}
	if _, xerr := h.svc.CreateTests(context.Background(), in); xerr != nil {
		t.Fatalf("CreateTests: %v", xerr)
	}
	for _, r := range h.repo.results[0] {
		if r.Family != "yahoo" && r.Family != "microsoft365" {
			t.Fatalf("sent to %s at %s; want only the chosen providers", r.SeedAddress, r.Family)
		}
	}
	if len(h.repo.results[0]) != 2 {
		t.Fatalf("sent %d copies; want the two seeds at those providers", len(h.repo.results[0]))
	}
	in.Families = []string{"icloud"}
	if _, xerr := h.svc.CreateTests(context.Background(), in); xerr == nil || xerr.Identifier != "placement_no_seeds" {
		t.Fatalf("got %v; want placement_no_seeds for a provider the panel lacks", xerr)
	}
}

func TestChosenOwnSeedsAreNotCappedBySeedsPerTest(t *testing.T) {
	h := newHarness(t) // four seeds per test
	worker := uuid.New()
	var ids []uuid.UUID
	for i := range 7 {
		s := repository.SeedAccount{ID: uuid.New(), OrganizationID: &h.org, Email: "seed" + strconv.Itoa(i) + "@gmail.com",
			Provider: "smtp_imap", MailHost: "gmail", Status: "active", WorkerID: &worker, SeedScope: models.SeedScopeWorkspace}
		h.repo.seeds = append(h.repo.seeds, s)
		ids = append(ids, s.ID)
	}
	in := h.input()
	in.Panel, in.SeedIDs = models.PlacementPanelWorkspace, ids
	if _, xerr := h.svc.CreateTests(context.Background(), in); xerr != nil {
		t.Fatalf("CreateTests: %v", xerr)
	}
	if got := len(h.repo.results[0]); got != 7 {
		t.Fatalf("sent to %d seeds; want all 7 chosen", got)
	}

	// A day that cannot pay for every chosen seed refuses rather than drops.
	h.repo.created, h.tasks.sentToday = nil, 44
	if _, xerr := h.svc.CreateTests(context.Background(), in); xerr == nil || xerr.Identifier != "placement_daily_budget" {
		t.Fatalf("got %v; want placement_daily_budget with six sends left for seven seeds", xerr)
	}

	// A chosen seed that is not connected is named, not skipped.
	h.tasks.sentToday = 0
	for i := range h.repo.seeds {
		if h.repo.seeds[i].ID == ids[0] {
			h.repo.seeds[i].Status = "inactive"
		}
	}
	_, xerr := h.svc.CreateTests(context.Background(), in)
	if xerr == nil || xerr.Identifier != "placement_invalid_seeds" || !strings.Contains(xerr.Message, "seed0@gmail.com") {
		t.Fatalf("got %v; want the disconnected seed named", xerr)
	}
}

func TestCreateTestsPaysInCreditsPastTheFreeTests(t *testing.T) {
	newPaid := func(t *testing.T, balance int) (*harness, *fakeCredits) {
		h := newHarness(t)
		t.Setenv("DEPLOYMENT_MODE", "cloud")
		h.svc.Gate = fakeGate{paid: true}
		h.repo.metered = config.PlacementTestsPerMonthPaidDefault // the free tests are used
		c := &fakeCredits{balance: balance, charges: map[string]int{}, refunded: map[string]bool{}}
		h.svc.Credits = c
		return h, c
	}
	price := config.PlacementCreditsPerTestDefault

	h, c := newPaid(t, 100)
	_, xerr := h.svc.CreateTests(context.Background(), h.input())
	if xerr == nil || xerr.Identifier != "placement_quota_exceeded" || !strings.Contains(xerr.Message, strconv.Itoa(price)+" credits") {
		t.Fatalf("got %v; want a refusal naming the price", xerr)
	}
	if len(c.charges) != 0 || len(h.repo.created) != 0 {
		t.Fatalf("a refused test charged %v and wrote %d tests", c.charges, len(h.repo.created))
	}

	in := h.input()
	in.MaxCredits = 1000
	views, xerr := h.svc.CreateTests(context.Background(), in)
	if xerr != nil {
		t.Fatalf("CreateTests: %v", xerr)
	}
	if views[0].CreditsCharged != price || c.balance != 100-price || c.charges[chargeKey(views[0].ID)] != price {
		t.Fatalf("charged %d, balance %d; want %d charged under the test's key", views[0].CreditsCharged, c.balance, price)
	}

	// A comparison with one free test left pays for its second half only.
	h, c = newPaid(t, 100)
	h.repo.metered = config.PlacementTestsPerMonthPaidDefault - 1
	in = h.input()
	in.MaxCredits, in.Tracking = 1000, models.PlacementTrackingCompare
	views, xerr = h.svc.CreateTests(context.Background(), in)
	if xerr != nil {
		t.Fatalf("CreateTests: %v", xerr)
	}
	if views[0].CreditsCharged != 0 || views[1].CreditsCharged != price || len(c.charges) != 1 {
		t.Fatalf("charged %d and %d; want only the second half paid", views[0].CreditsCharged, views[1].CreditsCharged)
	}

	h, _ = newPaid(t, price-1)
	in = h.input()
	in.MaxCredits = 1000
	if _, xerr := h.svc.CreateTests(context.Background(), in); xerr == nil || xerr.Identifier != "insufficient_credits" {
		t.Fatalf("got %v; want insufficient_credits", xerr)
	}

	// Agreeing to less than the price charges nothing.
	h, c = newPaid(t, 100)
	in = h.input()
	in.MaxCredits = price - 1
	if _, xerr := h.svc.CreateTests(context.Background(), in); xerr == nil || xerr.Identifier != "placement_quota_exceeded" || len(c.charges) != 0 {
		t.Fatalf("got %v with charges %v; want a refusal below the price", xerr, c.charges)
	}

	// A charge refused after the write takes the test back out.
	h, _ = newPaid(t, 100)
	h.svc.Credits = &fakeCredits{balance: 100, charges: map[string]int{}, refunded: map[string]bool{}, refuse: true}
	in = h.input()
	in.MaxCredits = 1000
	if _, xerr := h.svc.CreateTests(context.Background(), in); xerr == nil || xerr.Identifier != "insufficient_credits" {
		t.Fatalf("got %v; want the refused charge surfaced", xerr)
	}
	if len(h.repo.created) != 0 {
		t.Fatalf("%d tests left behind after a refused charge", len(h.repo.created))
	}

	// A monitor never spends credits on its own.
	h, _ = newPaid(t, 100)
	in = h.input()
	in.MaxCredits, in.Origin = 1000, models.PlacementOriginMonitor
	if _, xerr := h.svc.CreateTests(context.Background(), in); xerr == nil || xerr.Identifier != "placement_quota_exceeded" {
		t.Fatalf("got %v; want a monitor refused past the free tests", xerr)
	}

	// A trial has no credits to pay with, so it waits for next month.
	h, c = newPaid(t, 100)
	h.svc.Gate = fakeGate{paid: false}
	h.repo.metered = config.PlacementTestsPerMonthTrialDefault
	in = h.input()
	in.MaxCredits = 1000
	if _, xerr := h.svc.CreateTests(context.Background(), in); xerr == nil || xerr.Identifier != "placement_quota_exceeded" || len(c.charges) != 0 {
		t.Fatalf("got %v with charges %v; want a trial refused without a charge", xerr, c.charges)
	}

	// Nothing written means nothing charged.
	h, c = newPaid(t, 100)
	h.repo.failSave = true
	in = h.input()
	in.MaxCredits = 1000
	if _, xerr := h.svc.CreateTests(context.Background(), in); xerr == nil {
		t.Fatalf("CreateTests succeeded with a failing write")
	}
	if c.balance != 100 || len(c.charges) != 0 {
		t.Fatalf("balance %d, charges %v after a failed write; want nothing charged", c.balance, c.charges)
	}
}

type fakeGate struct{ paid bool }

func (g fakeGate) CanSendCampaignEmail(context.Context, uuid.UUID) (bool, *errx.Error) {
	return true, nil
}
func (g fakeGate) IsPaidOrganization(context.Context, uuid.UUID) (bool, *errx.Error) {
	return g.paid, nil
}

type settleRepo struct {
	*fakeRepo
	open    []repository.PlacementSettle
	settled map[uuid.UUID]int
}

func (r *settleRepo) UnsettledPaidTests(context.Context, int) ([]repository.PlacementSettle, error) {
	return r.open, nil
}
func (r *settleRepo) SettleCredits(_ context.Context, id uuid.UUID, refunded int) (bool, error) {
	if _, done := r.settled[id]; done {
		return false, nil
	}
	r.settled[id] = refunded
	return true, nil
}

func TestSettleCreditsRefundsOnlyWhatDeliveredNothing(t *testing.T) {
	h := newHarness(t)
	org := uuid.New()
	delivered, empty := uuid.New(), uuid.New()
	repo := &settleRepo{fakeRepo: h.repo, settled: map[uuid.UUID]int{}, open: []repository.PlacementSettle{
		{ID: delivered, OrganizationID: org, Credits: 25, Delivered: true},
		{ID: empty, OrganizationID: org, Credits: 25},
	}}
	c := &fakeCredits{charges: map[string]int{chargeKey(delivered): 25, chargeKey(empty): 25}, refunded: map[string]bool{}}
	h.svc.Repo, h.svc.Credits = repo, c

	got := h.svc.settleCredits(context.Background())
	if len(got) != 1 || got[0] != empty {
		t.Fatalf("refunded %v; want only the test that delivered nothing", got)
	}
	if repo.settled[delivered] != 0 || repo.settled[empty] != 25 || c.refunded[chargeKey(delivered)] {
		t.Fatalf("settled %v, refunded %v; want the delivered test kept and the empty one refunded 25", repo.settled, c.refunded)
	}
	if again := h.svc.settleCredits(context.Background()); len(again) != 0 || c.balance != 25 {
		t.Fatalf("a second pass refunded %v (balance %d); want nothing more", again, c.balance)
	}
}
