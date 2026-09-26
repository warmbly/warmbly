package placement

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/instancesettings"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/tasks/proto"
)

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
func (f *fakeRepo) ListSeeds(_ context.Context, scope string, org *uuid.UUID, _ bool) ([]repository.SeedAccount, error) {
	var out []repository.SeedAccount
	for _, s := range f.seeds {
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
	for _, b := range bundles {
		_ = f.CreateTest(ctx, b.Test, b.Results, b.Tasks)
	}
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
