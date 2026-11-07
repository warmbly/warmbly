package placement

import (
	"context"
	"fmt"
	"math/rand/v2"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type fakeBatches struct {
	repository.PlacementBatchRepository
	cands    []repository.PlacementBatchCandidate
	candOrg  map[uuid.UUID]uuid.UUID
	batches  map[uuid.UUID]*models.PlacementBatch
	senders  []*models.PlacementBatchSender
	open     int
	spent    models.Micro
	finished []string
}

func newFakeBatches() *fakeBatches {
	return &fakeBatches{candOrg: map[uuid.UUID]uuid.UUID{}, batches: map[uuid.UUID]*models.PlacementBatch{}}
}

func (f *fakeBatches) ListBatchCandidates(_ context.Context, orgID uuid.UUID, fl repository.PlacementCandidateFilter) ([]repository.PlacementBatchCandidate, error) {
	var out []repository.PlacementBatchCandidate
	for _, c := range f.cands {
		if f.candOrg[c.ID] != orgID {
			continue
		}
		if fl.IDs != nil && !slices.Contains(fl.IDs, c.ID) {
			continue
		}
		if !fl.IncludeInactive && c.Status != "active" {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}
func (f *fakeBatches) CountOpenBatches(context.Context, uuid.UUID) (int, error) { return f.open, nil }
func (f *fakeBatches) CreateBatch(_ context.Context, b *models.PlacementBatch, senders []models.PlacementBatchSender) error {
	b.Active = true
	cp := *b
	f.batches[b.ID] = &cp
	for i := range senders {
		s := senders[i]
		s.BatchID = b.ID
		f.senders = append(f.senders, &s)
	}
	return nil
}
func (f *fakeBatches) CloseInactiveBatches(context.Context) error   { return nil }
func (f *fakeBatches) SyncClosedBatchSenders(context.Context) error { return nil }
func (f *fakeBatches) RunningTestsOfCancelledBatches(context.Context, int) ([]repository.PlacementOrgTest, error) {
	return nil, nil
}
func (f *fakeBatches) ClaimBatches(context.Context, time.Time, time.Duration, int) ([]models.PlacementBatch, error) {
	var out []models.PlacementBatch
	for _, b := range f.batches {
		if b.Active {
			out = append(out, *b)
		}
	}
	return out, nil
}
func (f *fakeBatches) ReleaseBatch(context.Context, uuid.UUID) error                { return nil }
func (f *fakeBatches) SyncBatchSenders(context.Context, uuid.UUID, time.Time) error { return nil }
func (f *fakeBatches) CountSendingBatchSenders(context.Context, *uuid.UUID) (int, error) {
	n := 0
	for _, s := range f.senders {
		if s.Status == models.PlacementSenderRunning {
			n++
		}
	}
	return n, nil
}
func (f *fakeBatches) DueBatchSenders(_ context.Context, batchID uuid.UUID, now time.Time, limit int) ([]models.PlacementBatchSender, error) {
	var out []models.PlacementBatchSender
	for _, s := range f.senders {
		if s.BatchID == batchID && (s.Status == models.PlacementSenderQueued || s.Status == models.PlacementSenderDeferred) && !s.NextAttemptAt.After(now) {
			out = append(out, *s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Position < out[j].Position })
	return out[:min(limit, len(out))], nil
}
func (f *fakeBatches) sender(id uuid.UUID) *models.PlacementBatchSender {
	for _, s := range f.senders {
		if s.ID == id {
			return s
		}
	}
	return nil
}
func (f *fakeBatches) ClaimBatchSender(_ context.Context, id uuid.UUID) (bool, error) {
	s := f.sender(id)
	if s == nil || (s.Status != models.PlacementSenderQueued && s.Status != models.PlacementSenderDeferred) {
		return false, nil
	}
	s.Status, s.Attempts = models.PlacementSenderRunning, s.Attempts+1
	return true, nil
}
func (f *fakeBatches) SetBatchSenderOutcome(_ context.Context, id uuid.UUID, status, reason, detail string, next time.Time) error {
	s := f.sender(id)
	s.Status, s.Reason, s.Detail, s.NextAttemptAt = status, reason, detail, next
	return nil
}
func (f *fakeBatches) CloseOpenBatchSenders(_ context.Context, batchID uuid.UUID, from []string, status, reason, detail string) error {
	for _, s := range f.senders {
		if s.BatchID == batchID && slices.Contains(from, s.Status) {
			s.Status, s.Reason, s.Detail = status, reason, detail
		}
	}
	return nil
}
func (f *fakeBatches) AddBatchSpend(_ context.Context, _ uuid.UUID, n models.Micro) error {
	f.spent += n
	return nil
}
func (f *fakeBatches) MarkBatchStarted(_ context.Context, id uuid.UUID) error {
	f.batches[id].Status = models.PlacementBatchRunning
	return nil
}
func (f *fakeBatches) BatchProgress(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]models.PlacementBatchProgress, error) {
	out := map[uuid.UUID]models.PlacementBatchProgress{}
	for _, s := range f.senders {
		if slices.Contains(ids, s.BatchID) {
			p := out[s.BatchID]
			p.Add(s.Status, 1)
			out[s.BatchID] = p
		}
	}
	return out, nil
}
func (f *fakeBatches) BatchSummaries(context.Context, []uuid.UUID) (map[uuid.UUID]models.PlacementCounts, error) {
	return map[uuid.UUID]models.PlacementCounts{}, nil
}
func (f *fakeBatches) FinishBatch(_ context.Context, id uuid.UUID, status, _ string) (bool, error) {
	b := f.batches[id]
	if !b.Active {
		return false, nil
	}
	b.Active, b.Status = false, status
	f.finished = append(f.finished, status)
	return true, nil
}

// fleetHarness is a harness whose workspace has n extra mailboxes spread
// over domains and providers, all on the batch store as candidates.
func fleetHarness(t *testing.T, n int) (*harness, *fakeBatches) {
	t.Helper()
	h := newHarness(t)
	fb := newFakeBatches()
	h.svc.Batches = fb
	emails := h.svc.Emails.(*fakeEmails)
	worker := uuid.New()
	hosts := []string{"google_workspace", "google_workspace", "microsoft365", "other"}
	for i := range n {
		id := uuid.New()
		host := hosts[i%len(hosts)]
		addr := fmt.Sprintf("rep%d@domain%d.test", i, i%7)
		emails.accounts[id] = &models.Email{ID: id, OrganizationID: &h.org, Email: addr, Status: "active", WorkerID: &worker, CampaignLimit: 50}
		fb.cands = append(fb.cands, repository.PlacementBatchCandidate{ID: id, Email: addr, Provider: "smtp_imap", MailHost: host, Status: "active", WorkerID: &worker})
		fb.candOrg[id] = h.org
	}
	return h, fb
}

func (h *harness) batchInput() BatchInput {
	return BatchInput{OrgID: h.org, Scope: &models.PlacementSenderScope{Type: models.PlacementScopeWorkspace}, Subject: "Quick question", BodyPlain: "Hi there"}
}

func TestAllocateSplitsBySizeByLargestRemainder(t *testing.T) {
	got := allocate(135, []int{800, 400, 147})
	if !slices.Equal(got, []int{80, 40, 15}) {
		t.Fatalf("allocate(135, 800/400/147) = %v; want 80/40/15", got)
	}
	got = allocate(3, []int{100, 1, 1})
	if got[0]+got[1]+got[2] != 3 || got[0] > 100 || got[1] > 1 || got[2] > 1 {
		t.Fatalf("allocate(3, 100/1/1) = %v; want 3 in total within each size", got)
	}
	if got := allocate(10, []int{2, 3}); !slices.Equal(got, []int{2, 3}) {
		t.Fatalf("allocate beyond the total = %v; want every member", got)
	}
}

func TestSampleSendersPercentIsStratifiedByProvider(t *testing.T) {
	var cands []batchCandidate
	add := func(n int, family string) {
		for i := range n {
			cands = append(cands, batchCandidate{family: family, domain: fmt.Sprintf("%s%d.test", family, i%9)})
		}
	}
	add(800, "google_workspace")
	add(400, "other")
	add(147, "microsoft365")
	rng := rand.New(rand.NewPCG(1, 2))
	got := sampleSenders(cands, models.PlacementSample{Mode: models.PlacementSamplePercent, Percent: 10, Stratify: "provider"}, rng)
	per := map[string]int{}
	for _, c := range got {
		per[c.family]++
	}
	if len(got) != 135 || per["google_workspace"] != 80 || per["other"] != 40 || per["microsoft365"] != 15 {
		t.Fatalf("10%% stratified sample = %d (%v); want 135 split 80/40/15", len(got), per)
	}
	if got := sampleSenders(cands, models.PlacementSample{Mode: models.PlacementSampleRandom, Count: 100}, rng); len(got) != 100 {
		t.Fatalf("random 100 kept %d", len(got))
	}
	if got := sampleSenders(cands, models.PlacementSample{Mode: models.PlacementSampleAll}, rng); len(got) != len(cands) {
		t.Fatalf("all kept %d of %d", len(got), len(cands))
	}
}

func TestSampleSendersPerDomainCapsEveryDomainAlike(t *testing.T) {
	var cands []batchCandidate
	for i := range 500 {
		cands = append(cands, batchCandidate{family: "gmail", domain: "big.test", PlacementBatchCandidate: repository.PlacementBatchCandidate{Email: fmt.Sprint(i)}})
	}
	for i := range 3 {
		cands = append(cands, batchCandidate{family: "gmail", domain: "small.test", PlacementBatchCandidate: repository.PlacementBatchCandidate{Email: fmt.Sprint(i)}})
	}
	got := sampleSenders(cands, models.PlacementSample{Mode: models.PlacementSamplePerDomain, Count: 5}, rand.New(rand.NewPCG(3, 4)))
	per := map[string]int{}
	for _, c := range got {
		per[c.domain]++
	}
	if per["big.test"] != 5 || per["small.test"] != 3 {
		t.Fatalf("5 per domain = %v; want 5 from the large domain and all 3 of the small one", per)
	}
}

func TestStaggerSendersRotatesProviders(t *testing.T) {
	var cands []batchCandidate
	for i := range 30 {
		cands = append(cands, batchCandidate{family: "google_workspace", domain: fmt.Sprintf("g%d.test", i%3)})
	}
	for i := range 3 {
		cands = append(cands, batchCandidate{family: "microsoft365", domain: fmt.Sprintf("m%d.test", i)})
	}
	got := staggerSenders(cands, rand.New(rand.NewPCG(5, 6)))
	if len(got) != len(cands) {
		t.Fatalf("stagger kept %d of %d", len(got), len(cands))
	}
	for i := range 6 {
		want := "google_workspace"
		if i%2 == 1 {
			want = "microsoft365"
		}
		if got[i].family != want {
			t.Fatalf("position %d is %s; want providers to alternate while both have senders", i, got[i].family)
		}
	}
	if got[0].domain == got[2].domain {
		t.Fatalf("consecutive google senders share %s; want domains to rotate", got[0].domain)
	}
}

func TestStartAllowance(t *testing.T) {
	now := time.Now()
	half := now.Add(-30 * time.Second)
	if got := startAllowance(10, &half, now); got != 5 {
		t.Fatalf("10/min over 30s = %d; want 5", got)
	}
	if got := startAllowance(1, &half, now); got != 1 {
		t.Fatalf("a slow rate gave %d; want at least one", got)
	}
	long := now.Add(-time.Hour)
	if got := startAllowance(10, &long, now); got != 10 {
		t.Fatalf("an idle hour gave %d; want at most a minute's worth", got)
	}
}

func TestCreateBatchHoldsMoreSendersThanRunAtOnce(t *testing.T) {
	h, fb := fleetHarness(t, 1300)
	view, xerr := h.svc.CreateBatch(context.Background(), h.batchInput())
	if xerr != nil {
		t.Fatalf("CreateBatch: %v", xerr)
	}
	if view.SenderCount != 1300 || len(fb.senders) != 1300 || view.Status != models.PlacementBatchQueued {
		t.Fatalf("batch = %d senders (%d rows), %s; want 1300 queued", view.SenderCount, len(fb.senders), view.Status)
	}
	if len(h.repo.created) != 0 {
		t.Fatalf("creating the batch started %d tests; want none until the runner", len(h.repo.created))
	}
	pol := h.svc.policy(context.Background())
	pol.BatchSenderConcurrency, pol.BatchStartsPerMinute = 20, 600
	h.svc.Policy = fakePolicy{pol}

	h.svc.runBatches(context.Background())
	if len(h.repo.created) != 20 {
		t.Fatalf("one pass started %d tests; want the concurrency of 20", len(h.repo.created))
	}
	seen := map[uuid.UUID]bool{}
	for _, test := range h.repo.created {
		if test.BatchID == nil || *test.BatchID != view.ID || test.BatchSenderID == nil || test.Origin != models.PlacementOriginBatch {
			t.Fatalf("test %+v is not tied to the batch", test)
		}
		if seen[*test.SenderAccountID] {
			t.Fatalf("sender %s started twice", test.SenderAccountID)
		}
		seen[*test.SenderAccountID] = true
	}
	// The same 20 are still sending: nothing more starts.
	h.svc.runBatches(context.Background())
	if len(h.repo.created) != 20 {
		t.Fatalf("a second pass started %d tests while 20 were sending", len(h.repo.created)-20)
	}
}

func TestCreateBatchRefusals(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(*harness, *fakeBatches, *BatchInput)
		wantID string
	}{
		{"too many senders for the instance", func(h *harness, _ *fakeBatches, in *BatchInput) {
			pol := h.svc.policy(context.Background())
			pol.BatchSendersMax = 10
			h.svc.Policy = fakePolicy{pol}
		}, "placement_batch_too_large"},
		{"too many open batches", func(_ *harness, fb *fakeBatches, _ *BatchInput) { fb.open = 5 }, "placement_too_many_batches"},
		{"nothing matches", func(_ *harness, _ *fakeBatches, in *BatchInput) { in.Scope.Domains = []string{"nowhere.test"} }, "placement_batch_empty"},
		{"both ids and a scope", func(h *harness, _ *fakeBatches, in *BatchInput) { in.SenderAccountIDs = []uuid.UUID{h.sender} }, ""},
		{"another workspace's mailbox", func(_ *harness, _ *fakeBatches, in *BatchInput) {
			in.Scope, in.SenderAccountIDs = nil, []uuid.UUID{uuid.New()}
		}, ""},
		{"a stratified per-domain sample", func(_ *harness, _ *fakeBatches, in *BatchInput) {
			in.Sample = models.PlacementSample{Mode: models.PlacementSamplePerDomain, Count: 2, Stratify: "provider"}
		}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h, fb := fleetHarness(t, 40)
			in := h.batchInput()
			c.setup(h, fb, &in)
			_, xerr := h.svc.CreateBatch(context.Background(), in)
			if xerr == nil {
				t.Fatalf("CreateBatch succeeded; want a refusal")
			}
			if c.wantID != "" && xerr.Identifier != c.wantID {
				t.Fatalf("refused with %q (%s); want %q", xerr.Identifier, xerr.Message, c.wantID)
			}
			if len(fb.batches) != 0 {
				t.Fatalf("a refused batch was written")
			}
		})
	}
}

func TestCreateBatchDedupesChosenSendersAndHonoursAKeysMailboxes(t *testing.T) {
	h, fb := fleetHarness(t, 5)
	ids := []uuid.UUID{fb.cands[0].ID, fb.cands[1].ID, fb.cands[0].ID}
	in := h.batchInput()
	in.Scope, in.SenderAccountIDs = nil, ids
	view, xerr := h.svc.CreateBatch(context.Background(), in)
	if xerr != nil {
		t.Fatalf("CreateBatch: %v", xerr)
	}
	if view.SenderCount != 2 {
		t.Fatalf("batch has %d senders; want the duplicate dropped", view.SenderCount)
	}

	in = h.batchInput()
	in.AllowedSenders = []uuid.UUID{fb.cands[2].ID}
	view, xerr = h.svc.CreateBatch(context.Background(), in)
	if xerr != nil {
		t.Fatalf("CreateBatch: %v", xerr)
	}
	if view.SenderCount != 1 {
		t.Fatalf("a key limited to one mailbox resolved %d senders", view.SenderCount)
	}
	in = h.batchInput()
	in.Scope, in.SenderAccountIDs, in.AllowedSenders = nil, []uuid.UUID{fb.cands[3].ID}, []uuid.UUID{fb.cands[2].ID}
	if _, xerr := h.svc.CreateBatch(context.Background(), in); xerr == nil {
		t.Fatalf("a key named a mailbox it may not send from and the batch was created")
	}
}

func TestBatchSenderWithoutDailyHeadroomIsDeferredOrSkipped(t *testing.T) {
	for _, policy := range []string{models.PlacementUnavailableDefer, models.PlacementUnavailableSkip} {
		t.Run(policy, func(t *testing.T) {
			h, fb := fleetHarness(t, 3)
			h.tasks.sentToday = 49
			in := h.batchInput()
			in.OnUnavailable = policy
			view, xerr := h.svc.CreateBatch(context.Background(), in)
			if xerr != nil {
				t.Fatalf("CreateBatch: %v", xerr)
			}
			h.svc.runBatches(context.Background())
			for _, s := range fb.senders {
				if s.Reason != "placement_daily_budget" {
					t.Fatalf("sender %s: reason %q; want the daily budget", s.SenderEmail, s.Reason)
				}
				switch policy {
				case models.PlacementUnavailableDefer:
					if s.Status != models.PlacementSenderDeferred || !s.NextAttemptAt.After(time.Now().Add(time.Minute)) {
						t.Fatalf("sender %s is %s until %v; want deferred to the next sending day", s.SenderEmail, s.Status, s.NextAttemptAt)
					}
				default:
					if s.Status != models.PlacementSenderSkipped {
						t.Fatalf("sender %s is %s; want skipped", s.SenderEmail, s.Status)
					}
				}
			}
			b := fb.batches[view.ID]
			if policy == models.PlacementUnavailableSkip && (b.Active || b.Status != models.PlacementBatchFailed) {
				t.Fatalf("an all-skipped batch is %s (active %v); want failed", b.Status, b.Active)
			}
			if policy == models.PlacementUnavailableDefer && !b.Active {
				t.Fatalf("a deferred batch closed; want it to wait for tomorrow")
			}
		})
	}
}

func TestBatchStopsWhenTheAllowanceRunsOut(t *testing.T) {
	h, fb := fleetHarness(t, 4)
	in := h.batchInput()
	if _, xerr := h.svc.CreateBatch(context.Background(), in); xerr != nil {
		t.Fatalf("CreateBatch: %v", xerr)
	}
	// Between creation and the run, another test used the allowance up.
	t.Setenv("DEPLOYMENT_MODE", "cloud")
	h.repo.metered = 1000
	h.svc.runBatches(context.Background())
	for _, s := range fb.senders {
		if s.Status != models.PlacementSenderSkipped || s.Reason != "placement_quota_exceeded" {
			t.Fatalf("sender %s is %s (%s); want every sender skipped for the allowance", s.SenderEmail, s.Status, s.Reason)
		}
	}
	if len(h.repo.created) != 0 {
		t.Fatalf("%d tests started past the allowance", len(h.repo.created))
	}
}

func TestFinishedBatchStatus(t *testing.T) {
	for _, c := range []struct {
		name     string
		statuses []string
		want     string
	}{
		{"all completed", []string{"completed", "completed"}, models.PlacementBatchCompleted},
		{"some skipped", []string{"completed", "skipped"}, models.PlacementBatchCompletedWithWarnings},
		{"none completed", []string{"failed", "skipped"}, models.PlacementBatchFailed},
	} {
		t.Run(c.name, func(t *testing.T) {
			h, fb := fleetHarness(t, len(c.statuses))
			view, xerr := h.svc.CreateBatch(context.Background(), h.batchInput())
			if xerr != nil {
				t.Fatalf("CreateBatch: %v", xerr)
			}
			for i, s := range fb.senders {
				s.Status = c.statuses[i]
			}
			h.svc.runBatches(context.Background())
			if got := fb.batches[view.ID].Status; got != c.want {
				t.Fatalf("batch finished %s; want %s", got, c.want)
			}
		})
	}
}

func TestBuildBatchDetailGroupsWorstFirst(t *testing.T) {
	rows := []repository.PlacementBreakdownRow{
		{Set: "overall", Tracked: true, Folder: "inbox", Count: 6},
		{Set: "overall", Tracked: true, Folder: "spam", Count: 4},
		{Set: "overall", Tracked: false, Folder: "inbox", Count: 9},
		{Set: "domain", SenderDomain: "good.test", Folder: "inbox", Count: 5},
		{Set: "domain", SenderDomain: "bad.test", Folder: "inbox", Count: 1},
		{Set: "domain", SenderDomain: "bad.test", Folder: "spam", Count: 4},
		{Set: "provider", SenderFamily: "google_workspace", Folder: "inbox", Count: 6},
		{Set: "recipient", RecipientFamily: "gmail", Folder: "inbox", Count: 6},
		{Set: "matrix", SenderDomain: "bad.test", RecipientFamily: "gmail", Folder: "spam", Count: 4},
	}
	sizes := []repository.PlacementBatchGroupSize{
		{ByDomain: true, Domain: "good.test", Senders: 2, Completed: 2},
		{ByDomain: true, Domain: "bad.test", Senders: 3, Completed: 3},
		{Family: "google_workspace", Senders: 5, Completed: 5},
	}
	v := BatchView{PlacementBatch: models.PlacementBatch{Tracking: models.PlacementTrackingCompare}}
	d := buildBatchDetail(v, rows, sizes)
	if d.Summary.Inbox != 6 || d.Summary.Spam != 4 || d.Untracked == nil || d.Untracked.Inbox != 9 {
		t.Fatalf("summary %+v untracked %+v; want the tracked half as headline", d.Summary, d.Untracked)
	}
	if len(d.Domains) != 2 || d.Domains[0].Key != "bad.test" || d.Domains[0].Senders != 3 {
		t.Fatalf("domains = %+v; want bad.test first with its 3 senders", d.Domains)
	}
	if len(d.Matrix) != 1 || d.Matrix[0].Domain != "bad.test" || d.Matrix[0].Recipients[0].Counts.Spam != 4 {
		t.Fatalf("matrix = %+v", d.Matrix)
	}
	if len(d.Providers) != 1 || d.Providers[0].Senders != 5 {
		t.Fatalf("providers = %+v", d.Providers)
	}
}

func TestBatchRespectsTheInstanceWideLimit(t *testing.T) {
	h, _ := fleetHarness(t, 50)
	if _, xerr := h.svc.CreateBatch(context.Background(), h.batchInput()); xerr != nil {
		t.Fatalf("CreateBatch: %v", xerr)
	}
	pol := h.svc.policy(context.Background())
	pol.BatchSenderConcurrency, pol.BatchInstanceConcurrency, pol.BatchStartsPerMinute = 20, 5, 600
	h.svc.Policy = fakePolicy{pol}
	h.svc.runBatches(context.Background())
	if len(h.repo.created) != 5 {
		t.Fatalf("started %d tests; want the instance-wide limit of 5 below the workspace's 20", len(h.repo.created))
	}
	for _, test := range h.repo.created {
		if test.Pace != models.PlacementPaceSpaced {
			t.Fatalf("a batch test runs %s; want spaced", test.Pace)
		}
	}
}

func TestBatchRefusesQuickPace(t *testing.T) {
	h, _ := fleetHarness(t, 3)
	in := h.batchInput()
	in.Pace = models.PlacementPaceQuick
	if _, xerr := h.svc.CreateBatch(context.Background(), in); xerr == nil {
		t.Fatalf("a quick batch was created")
	}
}

func TestPreviewCountsSendersBeforeTheCopyIsChosen(t *testing.T) {
	h, _ := fleetHarness(t, 12)
	in := h.batchInput()
	in.Subject, in.BodyPlain, in.Tracking = "", "", models.PlacementTrackingCompare
	p, xerr := h.svc.PreviewBatch(context.Background(), in)
	if xerr != nil {
		t.Fatalf("PreviewBatch without copy: %v", xerr)
	}
	if p.Selected != 12 || p.Tests != 24 {
		t.Fatalf("preview = %d senders, %d tests; want 12 and 24 for a comparison", p.Selected, p.Tests)
	}
	if _, xerr := h.svc.CreateBatch(context.Background(), in); xerr == nil {
		t.Fatalf("a batch without copy was created")
	}
}
