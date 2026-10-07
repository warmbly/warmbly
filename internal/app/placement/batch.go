package placement

import (
	"context"
	"math"
	"math/rand/v2"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/mailhost"
	"github.com/warmbly/warmbly/internal/repository"
)

// BatchInput is one request to run a placement test from many senders.
// Exactly one of SenderAccountIDs and Scope chooses the senders.
type BatchInput struct {
	OrgID            uuid.UUID
	UserID           *uuid.UUID
	SenderAccountIDs []uuid.UUID
	Scope            *models.PlacementSenderScope
	Sample           models.PlacementSample
	// AllowedSenders is an API key's mailbox restriction; nil allows every one.
	AllowedSenders []uuid.UUID

	CampaignID    *uuid.UUID
	SequenceID    *uuid.UUID
	ContactID     *uuid.UUID
	Subject       string
	BodyHTML      string
	BodyPlain     string
	Tracking      string
	Panel         string
	Pace          string
	Families      []string
	SeedIDs       []uuid.UUID
	OnUnavailable string
	// MaxCredits is the most the caller agreed to pay across the whole batch
	// for tests past the monthly free allowance.
	MaxCredits int
}

// BatchGroupCount is how many selected senders share a provider.
type BatchGroupCount struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Senders int    `json:"senders"`
}

// BatchPreview is what a batch would do, before it is started.
type BatchPreview struct {
	// Matched is how many senders the scope resolved to, Selected how many
	// the sample kept.
	Matched   int               `json:"matched"`
	Selected  int               `json:"selected"`
	Inactive  int               `json:"inactive"`
	Domains   int               `json:"domains"`
	Providers []BatchGroupCount `json:"providers"`
	// Variants is two for a tracking comparison, which doubles every count.
	Variants     int `json:"variants"`
	Tests        int `json:"tests"`
	SeedsPerTest int `json:"seeds_per_test"`
	// MaxSends is the most probes the batch sends; each sender's own daily
	// limit can only make it fewer.
	MaxSends int `json:"max_sends"`
	// FreeTests and PaidTests split Tests against the monthly allowance, and
	// Credits is the most the paid ones cost. Unmetered panels leave all three
	// zero.
	Metered    bool                  `json:"metered"`
	FreeTests  int                   `json:"free_tests"`
	PaidTests  int                   `json:"paid_tests"`
	Credits    int                   `json:"credits"`
	Usage      models.PlacementUsage `json:"usage"`
	SendersMax int                   `json:"senders_max"`
	// Concurrency is how many senders send at once in this workspace.
	Concurrency int `json:"concurrency"`
}

// BatchView is a batch with its progress and headline placement.
type BatchView struct {
	models.PlacementBatch
	Progress models.PlacementBatchProgress `json:"progress"`
	Summary  models.PlacementCounts        `json:"summary"`
}

// BatchGroup is a batch's placement for one sending domain or provider.
type BatchGroup struct {
	Key     string                 `json:"key"`
	Label   string                 `json:"label"`
	Senders int                    `json:"senders"`
	Tested  int                    `json:"tested"`
	Counts  models.PlacementCounts `json:"counts"`
}

// BatchMatrixRow is one sending domain's placement per recipient provider.
type BatchMatrixRow struct {
	Domain     string                         `json:"domain"`
	Recipients []models.PlacementFamilyCounts `json:"recipients"`
}

// BatchDetail is one batch in full.
type BatchDetail struct {
	BatchView
	// Untracked is the untracked half of a tracking comparison; Summary is
	// the tracked half, the copy the campaign really sends.
	Untracked  *models.PlacementCounts        `json:"untracked,omitempty"`
	Domains    []BatchGroup                   `json:"domains"`
	Providers  []BatchGroup                   `json:"providers"`
	Recipients []models.PlacementFamilyCounts `json:"recipients"`
	Matrix     []BatchMatrixRow               `json:"matrix"`
	Content    ContentCheck                   `json:"content"`
}

// BatchSenderView is one sender of a batch with where its copies landed.
type BatchSenderView struct {
	models.PlacementBatchSender
	SenderFamilyLabel string                 `json:"sender_family_label"`
	Summary           models.PlacementCounts `json:"summary"`
	TestIDs           []uuid.UUID            `json:"test_ids"`
}

// batchCandidate is a resolved sender with the groups sampling reads.
type batchCandidate struct {
	repository.PlacementBatchCandidate
	family string
	domain string
}

func (s *service) batchesReady() *errx.Error {
	if s.Batches == nil {
		return errx.New(errx.NotImplemented, "placement batches are not configured")
	}
	return nil
}

// validateBatch normalizes everything a batch request carries except the
// senders, and resolves its copy once so every sender tests the same email.
func (s *service) validateBatch(ctx context.Context, in *BatchInput, copyGiven bool) ([]variant, *errx.Error) {
	if in.Panel == "" {
		in.Panel = models.PlacementPanelInstance
	}
	if !models.ValidPlacementPanel(in.Panel) {
		return nil, errx.New(errx.BadRequest, "panel must be instance, workspace or cloud")
	}
	if len(in.SeedIDs) > 0 && in.Panel != models.PlacementPanelWorkspace {
		return nil, errx.New(errx.BadRequest, "seed_ids needs panel workspace")
	}
	if len(in.SeedIDs) > config.PlacementSeedsPerWorkspaceMax {
		return nil, errx.New(errx.BadRequest, "seed_ids has more entries than a workspace can have seed inboxes")
	}
	// A batch runs for hours anyway; quick copies from many senders at once
	// would only reach the shared seeds as a burst.
	if in.Pace == "" {
		in.Pace = models.PlacementPaceSpaced
	}
	if in.Pace != models.PlacementPaceSpaced {
		return nil, errx.New(errx.BadRequest, "a batch always sends spaced; pace quick is for a single test")
	}
	if in.Tracking == "" {
		in.Tracking = models.PlacementTrackingCampaign
	}
	switch in.Tracking {
	case models.PlacementTrackingCampaign, models.PlacementTrackingOn, models.PlacementTrackingOff, models.PlacementTrackingCompare:
	default:
		return nil, errx.New(errx.BadRequest, "tracking must be campaign, on, off or compare")
	}
	if in.OnUnavailable == "" {
		in.OnUnavailable = models.PlacementUnavailableDefer
	}
	if in.OnUnavailable != models.PlacementUnavailableDefer && in.OnUnavailable != models.PlacementUnavailableSkip {
		return nil, errx.New(errx.BadRequest, "on_unavailable must be skip or defer")
	}
	if in.MaxCredits < 0 {
		return nil, errx.New(errx.BadRequest, "max_credits cannot be negative")
	}
	families, xerr := normalizeFamilies(in.Families)
	if xerr != nil {
		return nil, xerr
	}
	in.Families = families

	if s.Gate != nil && !config.SelfHosted() {
		if ok, _ := s.Gate.CanSendCampaignEmail(ctx, in.OrgID); !ok {
			return nil, placementErr(errx.PaymentRequired, "placement_not_entitled", "Placement tests need an active trial or subscription.")
		}
	}

	// A preview asked before the copy is chosen counts senders alone; the
	// copy is checked once any of it is given, and always on create.
	var variants []variant
	if copyGiven || in.CampaignID != nil || in.SequenceID != nil || strings.TrimSpace(in.Subject) != "" || in.BodyHTML != "" || in.BodyPlain != "" {
		spec := copySpec{
			CampaignID: in.CampaignID, SequenceID: in.SequenceID, ContactID: in.ContactID,
			Subject: in.Subject, BodyHTML: in.BodyHTML, BodyPlain: in.BodyPlain, Panel: in.Panel,
		}
		if xerr := s.resolveCopy(ctx, in.OrgID, &spec); xerr != nil {
			return nil, xerr
		}
		in.ContactID, in.Subject, in.BodyHTML, in.BodyPlain = spec.ContactID, spec.Subject, spec.BodyHTML, spec.BodyPlain
		if variants, xerr = trackingVariants(in.Tracking, spec.campaign); xerr != nil {
			return nil, xerr
		}
	} else if variants, xerr = trackingVariants(in.Tracking, nil); xerr != nil {
		return nil, xerr
	}

	// A panel no sender could test on refuses the batch, not every sender.
	switch in.Panel {
	case models.PlacementPanelCloud:
		if s.Cloud == nil {
			return nil, placementErr(errx.Conflict, "placement_panel_unavailable", "Link this instance to Warmbly Cloud to test on its seed panel.")
		}
	default:
		scope, orgFilter := models.SeedScopeInstance, (*uuid.UUID)(nil)
		if in.Panel == models.PlacementPanelWorkspace {
			scope, orgFilter = models.SeedScopeWorkspace, &in.OrgID
		}
		all, err := s.Repo.ListSeeds(ctx, scope, orgFilter, false)
		if err != nil {
			errs.CaptureException(err)
			return nil, errx.InternalError()
		}
		if len(in.SeedIDs) > 0 {
			known := map[uuid.UUID]bool{}
			for _, r := range all {
				known[r.ID] = true
			}
			for _, id := range in.SeedIDs {
				if !known[id] {
					return nil, placementErr(errx.BadRequest, "placement_invalid_seeds",
						"Every chosen seed inbox has to be a seed inbox of this workspace.")
				}
			}
		}
		if len(inFamilies(all, families)) == 0 {
			return nil, placementErr(errx.Conflict, "placement_no_seeds", noSeedsMessage(in.Panel))
		}
	}
	return variants, nil
}

// resolveSenders turns the request's senders into the candidates the batch
// will snapshot, before sampling, and reports how many matched.
func (s *service) resolveSenders(ctx context.Context, in BatchInput) ([]batchCandidate, *errx.Error) {
	if (len(in.SenderAccountIDs) > 0) == (in.Scope != nil) {
		return nil, errx.New(errx.BadRequest, "choose the senders with either sender_account_ids or sender_scope")
	}
	allowed := func(id uuid.UUID) bool { return in.AllowedSenders == nil || slices.Contains(in.AllowedSenders, id) }

	var filter repository.PlacementCandidateFilter
	var providers, domains []string
	if len(in.SenderAccountIDs) > 0 {
		ids := uniqueUUIDs(in.SenderAccountIDs)
		if len(ids) > config.PlacementBatchSendersMaxCeiling {
			return nil, placementErr(errx.BadRequest, "placement_batch_too_large", "sender_account_ids names more mailboxes than any batch may hold.")
		}
		for _, id := range ids {
			if !allowed(id) {
				return nil, errx.New(errx.Forbidden, "this API key cannot send from one of those mailboxes")
			}
		}
		// Chosen by hand, so a disconnected one is kept and reported by name
		// when its turn comes.
		filter = repository.PlacementCandidateFilter{IDs: ids, IncludeInactive: true}
		rows, err := s.Batches.ListBatchCandidates(ctx, in.OrgID, filter)
		if err != nil {
			errs.CaptureException(err)
			return nil, errx.InternalError()
		}
		if len(rows) != len(ids) {
			return nil, errx.New(errx.NotFound, "a sending mailbox was not found in this workspace, or is a seed inbox")
		}
		return candidates(rows), nil
	}

	sc := in.Scope
	if len(sc.Providers) > config.PlacementFamiliesMax {
		return nil, errx.New(errx.BadRequest, "sender_scope.providers names too many providers")
	}
	if len(sc.Domains) > 1000 {
		return nil, errx.New(errx.BadRequest, "sender_scope.domains names too many domains")
	}
	if len(sc.TagIDs) > 200 {
		return nil, errx.New(errx.BadRequest, "sender_scope.tag_ids names too many tags")
	}
	if sc.UntestedDays < 0 || sc.UntestedDays > 365 {
		return nil, errx.New(errx.BadRequest, "sender_scope.untested_days must be between 0 and 365")
	}
	for _, p := range sc.Providers {
		p = strings.ToLower(strings.TrimSpace(p))
		if p != "" {
			providers = append(providers, p)
		}
	}
	for _, d := range sc.Domains {
		d = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(d), "@")))
		if d != "" {
			domains = append(domains, d)
		}
	}
	filter = repository.PlacementCandidateFilter{TagIDs: sc.TagIDs, IncludeInactive: sc.IncludeInactive}
	if sc.UntestedDays > 0 {
		since := s.now().AddDate(0, 0, -sc.UntestedDays)
		filter.UntestedSince = &since
	}
	switch sc.Type {
	case models.PlacementScopeWorkspace:
	case models.PlacementScopeCampaign:
		if sc.CampaignID == nil {
			return nil, errx.New(errx.BadRequest, "sender_scope.campaign_id is required for a campaign scope")
		}
		campaign, xerr := s.ownedCampaign(ctx, in.OrgID, *sc.CampaignID)
		if xerr != nil {
			return nil, xerr
		}
		pool, xerr := repository.ResolveCampaignSenderPool(ctx, s.Emails, campaign)
		if xerr != nil {
			return nil, xerr
		}
		filter.IDs = make([]uuid.UUID, 0, len(pool.Accounts))
		for _, a := range pool.Accounts {
			filter.IDs = append(filter.IDs, a.ID)
		}
	default:
		return nil, errx.New(errx.BadRequest, "sender_scope.type must be campaign or workspace")
	}
	rows, err := s.Batches.ListBatchCandidates(ctx, in.OrgID, filter)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	out := make([]batchCandidate, 0, len(rows))
	for _, c := range candidates(rows) {
		if !allowed(c.ID) {
			continue
		}
		if len(providers) > 0 && !slices.Contains(providers, c.family) {
			continue
		}
		if len(domains) > 0 && !slices.Contains(domains, c.domain) {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

func candidates(rows []repository.PlacementBatchCandidate) []batchCandidate {
	out := make([]batchCandidate, 0, len(rows))
	for _, r := range rows {
		out = append(out, batchCandidate{
			PlacementBatchCandidate: r,
			family:                  string(mailhost.ForMailbox(r.MailHost, r.Provider, r.Email)),
			domain:                  domainOf(r.SendFrom()),
		})
	}
	return out
}

func uniqueUUIDs(ids []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]bool, len(ids))
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id != uuid.Nil && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// validateSample checks a sampling request.
func validateSample(sp *models.PlacementSample) *errx.Error {
	if sp.Mode == "" {
		sp.Mode = models.PlacementSampleAll
	}
	switch sp.Mode {
	case models.PlacementSampleAll:
	case models.PlacementSampleRandom, models.PlacementSamplePerDomain, models.PlacementSamplePerProvider:
		if sp.Count < 1 || sp.Count > config.PlacementBatchSendersMaxCeiling {
			return errx.New(errx.BadRequest, "sample.count must be at least 1")
		}
	case models.PlacementSamplePercent:
		if sp.Percent < 1 || sp.Percent > 100 {
			return errx.New(errx.BadRequest, "sample.percent must be between 1 and 100")
		}
	default:
		return errx.New(errx.BadRequest, "sample.mode must be all, random, percent, per_domain or per_provider")
	}
	switch sp.Stratify {
	case "", "provider", "domain":
	default:
		return errx.New(errx.BadRequest, "sample.stratify must be provider or domain")
	}
	if sp.Stratify != "" && sp.Mode != models.PlacementSampleRandom && sp.Mode != models.PlacementSamplePercent {
		return errx.New(errx.BadRequest, "sample.stratify applies to a random or percent sample")
	}
	return nil
}

// sampleSenders keeps part of the candidates. A stratified sample splits its
// size across providers or domains in proportion to each one's share, largest
// remainder first, so a small group is not rounded away.
func sampleSenders(cands []batchCandidate, sp models.PlacementSample, rng *rand.Rand) []batchCandidate {
	shuffled := slices.Clone(cands)
	rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
	groupOf := func(c batchCandidate, by string) string {
		if by == "domain" {
			return c.domain
		}
		return c.family
	}
	switch sp.Mode {
	case models.PlacementSampleRandom, models.PlacementSamplePercent:
		n := min(sp.Count, len(shuffled))
		if sp.Mode == models.PlacementSamplePercent {
			n = int(math.Ceil(float64(len(shuffled)) * float64(sp.Percent) / 100))
		}
		if sp.Stratify == "" {
			return shuffled[:n]
		}
		groups, keys := groupCandidates(shuffled, func(c batchCandidate) string { return groupOf(c, sp.Stratify) })
		sizes := make([]int, len(keys))
		for i, k := range keys {
			sizes[i] = len(groups[k])
		}
		quota := allocate(n, sizes)
		var out []batchCandidate
		for i, k := range keys {
			out = append(out, groups[k][:quota[i]]...)
		}
		return out
	case models.PlacementSamplePerDomain, models.PlacementSamplePerProvider:
		by := "provider"
		if sp.Mode == models.PlacementSamplePerDomain {
			by = "domain"
		}
		groups, keys := groupCandidates(shuffled, func(c batchCandidate) string { return groupOf(c, by) })
		var out []batchCandidate
		for _, k := range keys {
			g := groups[k]
			out = append(out, g[:min(sp.Count, len(g))]...)
		}
		return out
	}
	return shuffled
}

// groupCandidates buckets candidates by key, keeping their order, and returns
// the keys sorted.
func groupCandidates(cands []batchCandidate, key func(batchCandidate) string) (map[string][]batchCandidate, []string) {
	groups := map[string][]batchCandidate{}
	var keys []string
	for _, c := range cands {
		k := key(c)
		if _, ok := groups[k]; !ok {
			keys = append(keys, k)
		}
		groups[k] = append(groups[k], c)
	}
	sort.Strings(keys)
	return groups, keys
}

// allocate splits n across groups of the given sizes in proportion, by the
// largest remainder, never giving a group more than it has.
func allocate(n int, sizes []int) []int {
	total := 0
	for _, s := range sizes {
		total += s
	}
	out := make([]int, len(sizes))
	if total == 0 || n <= 0 {
		return out
	}
	n = min(n, total)
	type rem struct {
		i    int
		frac float64
	}
	rems := make([]rem, len(sizes))
	given := 0
	for i, size := range sizes {
		exact := float64(n) * float64(size) / float64(total)
		out[i] = int(math.Floor(exact))
		given += out[i]
		rems[i] = rem{i, exact - float64(out[i])}
	}
	sort.SliceStable(rems, func(a, b int) bool {
		if rems[a].frac != rems[b].frac {
			return rems[a].frac > rems[b].frac
		}
		return sizes[rems[a].i] > sizes[rems[b].i]
	})
	for k := 0; given < n; k = (k + 1) % len(rems) {
		i := rems[k].i
		if out[i] < sizes[i] {
			out[i]++
			given++
		}
	}
	return out
}

// staggerSenders orders a batch so consecutive starts rotate across sending
// providers, and within a provider across domains, rather than running one
// provider's mailboxes back to back.
func staggerSenders(cands []batchCandidate, rng *rand.Rand) []batchCandidate {
	shuffled := slices.Clone(cands)
	rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
	byFamily, families := groupCandidates(shuffled, func(c batchCandidate) string { return c.family })
	queues := make([][]batchCandidate, len(families))
	for i, f := range families {
		queues[i] = roundRobin(groupCandidates(byFamily[f], func(c batchCandidate) string { return c.domain }))
	}
	out := make([]batchCandidate, 0, len(cands))
	for len(out) < len(cands) {
		for i := range queues {
			if len(queues[i]) > 0 {
				out = append(out, queues[i][0])
				queues[i] = queues[i][1:]
			}
		}
	}
	return out
}

func roundRobin(groups map[string][]batchCandidate, keys []string) []batchCandidate {
	var out []batchCandidate
	for {
		added := false
		for _, k := range keys {
			if len(groups[k]) > 0 {
				out = append(out, groups[k][0])
				groups[k] = groups[k][1:]
				added = true
			}
		}
		if !added {
			return out
		}
	}
}

// batchCost is what a batch of tests costs against the monthly allowance.
type batchCost struct {
	metered bool
	usage   models.PlacementUsage
	free    int
	paid    int
	credits int
}

func (s *service) batchCost(ctx context.Context, orgID uuid.UUID, panel string, tests int) (batchCost, *errx.Error) {
	var c batchCost
	switch panel {
	case models.PlacementPanelInstance:
		usage, xerr := s.usage(ctx, orgID)
		if xerr != nil {
			return c, xerr
		}
		c.usage = usage
	case models.PlacementPanelCloud:
		if s.Cloud != nil {
			if panel, xerr := s.Cloud.PlacementPanel(ctx, orgID); xerr == nil && panel != nil {
				c.usage = panel.Usage
			}
		}
	default:
		return c, nil
	}
	if c.usage.Limit == nil {
		return c, nil
	}
	c.metered = true
	c.free = min(tests, c.usage.Remaining())
	c.paid = tests - c.free
	c.credits = c.paid * c.usage.CreditsPerTest
	return c, nil
}

// planBatch validates a request and resolves the senders it would run.
func (s *service) planBatch(ctx context.Context, in *BatchInput, requireCopy bool) ([]batchCandidate, int, []variant, *errx.Error) {
	if xerr := s.batchesReady(); xerr != nil {
		return nil, 0, nil, xerr
	}
	if xerr := validateSample(&in.Sample); xerr != nil {
		return nil, 0, nil, xerr
	}
	variants, xerr := s.validateBatch(ctx, in, requireCopy)
	if xerr != nil {
		return nil, 0, nil, xerr
	}
	cands, xerr := s.resolveSenders(ctx, *in)
	if xerr != nil {
		return nil, 0, nil, xerr
	}
	matched := len(cands)
	return sampleSenders(cands, in.Sample, rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))), matched, variants, nil
}

// PreviewBatch reports how many senders, tests, sends and credits a batch
// request comes to, without starting anything. The copy may be left out.
func (s *service) PreviewBatch(ctx context.Context, in BatchInput) (*BatchPreview, *errx.Error) {
	selected, matched, variants, xerr := s.planBatch(ctx, &in, false)
	if xerr != nil {
		return nil, xerr
	}
	pol := s.policy(ctx)
	p := &BatchPreview{
		Matched:      matched,
		Selected:     len(selected),
		Variants:     len(variants),
		Tests:        len(selected) * len(variants),
		SeedsPerTest: pol.SeedsPerTest,
		SendersMax:   pol.BatchSendersMax,
		Concurrency:  pol.BatchSenderConcurrency,
		Providers:    []BatchGroupCount{},
	}
	perTest := pol.SeedsPerTest
	if len(in.SeedIDs) > 0 {
		perTest = len(in.SeedIDs)
	}
	p.SeedsPerTest = perTest
	p.MaxSends = p.Tests * perTest
	families := map[string]int{}
	domains := map[string]bool{}
	for _, c := range selected {
		families[c.family]++
		domains[c.domain] = true
		if c.Status != "active" || c.WorkerID == nil {
			p.Inactive++
		}
	}
	p.Domains = len(domains)
	for f, n := range families {
		p.Providers = append(p.Providers, BatchGroupCount{Key: f, Label: familyLabel(f), Senders: n})
	}
	sort.Slice(p.Providers, func(i, j int) bool { return p.Providers[i].Senders > p.Providers[j].Senders })
	cost, xerr := s.batchCost(ctx, in.OrgID, in.Panel, p.Tests)
	if xerr != nil {
		return nil, xerr
	}
	p.Metered, p.Usage, p.FreeTests, p.PaidTests, p.Credits = cost.metered, cost.usage, cost.free, cost.paid, cost.credits
	return p, nil
}

// CreateBatch snapshots the senders and queues the batch. Nothing is sent
// here; the runner starts senders a few at a time.
func (s *service) CreateBatch(ctx context.Context, in BatchInput) (*BatchView, *errx.Error) {
	selected, matched, variants, xerr := s.planBatch(ctx, &in, true)
	if xerr != nil {
		return nil, xerr
	}
	pol := s.policy(ctx)
	if len(selected) == 0 {
		return nil, placementErr(errx.BadRequest, "placement_batch_empty", "No sending mailbox matches this selection.")
	}
	if len(selected) > pol.BatchSendersMax {
		return nil, placementErr(errx.BadRequest, "placement_batch_too_large",
			"This batch has "+strconv.Itoa(len(selected))+" senders and this instance allows up to "+
				strconv.Itoa(pol.BatchSendersMax)+" in one batch. Narrow the selection or take a sample.")
	}
	open, err := s.Batches.CountOpenBatches(ctx, in.OrgID)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	if open >= config.PlacementBatchOpenPerOrgMax {
		return nil, placementErr(errx.TooManyRequests, "placement_too_many_batches",
			"This workspace already has "+strconv.Itoa(open)+" placement batches running. Wait for one to finish, or cancel one.")
	}

	tests := len(selected) * len(variants)
	cost, xerr := s.batchCost(ctx, in.OrgID, in.Panel, tests)
	if xerr != nil {
		return nil, xerr
	}
	if cost.paid > 0 {
		if cost.usage.CreditsPerTest == 0 {
			return nil, placementErr(errx.PaymentRequired, "placement_quota_exceeded",
				"This batch needs "+strconv.Itoa(tests)+" tests and the workspace has "+strconv.Itoa(cost.free)+
					" free this month. Take a smaller sample or test on your own seed inboxes.")
		}
		if in.MaxCredits < cost.credits {
			return nil, placementErr(errx.PaymentRequired, "placement_quota_exceeded",
				"This batch can cost up to "+strconv.Itoa(cost.credits)+" credits past the free tests; start it again agreeing to pay that many.")
		}
		if bal := cost.usage.CreditBalance; bal != nil && *bal < cost.credits {
			return nil, placementErr(errx.PaymentRequired, "insufficient_credits",
				"This batch can cost up to "+strconv.Itoa(cost.credits)+" credits and the workspace has "+strconv.Itoa(*bal)+".")
		}
	} else {
		// Nothing agreed is ever charged when the batch fits the free tests.
		in.MaxCredits = 0
	}

	now := s.now()
	ordered := staggerSenders(selected, rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())))
	b := models.PlacementBatch{
		ID:             uuid.New(),
		OrganizationID: in.OrgID,
		CreatedBy:      in.UserID,
		CampaignID:     in.CampaignID,
		SequenceID:     in.SequenceID,
		ContactID:      in.ContactID,
		Subject:        in.Subject,
		BodyHTML:       in.BodyHTML,
		BodyPlain:      in.BodyPlain,
		Tracking:       in.Tracking,
		Panel:          in.Panel,
		Pace:           in.Pace,
		Families:       in.Families,
		SeedIDs:        in.SeedIDs,
		OnUnavailable:  in.OnUnavailable,
		Selection: models.PlacementBatchSelection{
			SenderAccountIDs: len(in.SenderAccountIDs),
			Scope:            in.Scope,
			Sample:           in.Sample,
			Matched:          matched,
		},
		SenderCount: len(ordered),
		MaxCredits:  in.MaxCredits,
		Status:      models.PlacementBatchQueued,
		RetryUntil:  now.AddDate(0, 0, config.PlacementBatchRetryDays),
	}
	senders := make([]models.PlacementBatchSender, len(ordered))
	for i, c := range ordered {
		id := c.ID
		senders[i] = models.PlacementBatchSender{
			ID:             uuid.New(),
			EmailAccountID: &id,
			SenderEmail:    c.SendFrom(),
			SenderDomain:   c.domain,
			SenderFamily:   c.family,
			Position:       i,
			Status:         models.PlacementSenderQueued,
		}
	}
	if err := s.Batches.CreateBatch(ctx, &b, senders); err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	s.publishBatch(ctx, &b)
	v := BatchView{PlacementBatch: b, Summary: models.PlacementCounts{}}
	v.Progress.Add(models.PlacementSenderQueued, len(senders))
	v.Summary.Finish()
	stripBody(&v.PlacementBatch)
	return &v, nil
}

func stripBody(b *models.PlacementBatch) {
	b.BodyHTML, b.BodyPlain = "", ""
}

func (s *service) publishBatch(ctx context.Context, b *models.PlacementBatch) {
	if s.Publisher != nil {
		s.Publisher.PublishPlacementBatch(ctx, b.OrganizationID, b.ID, b.Status)
	}
}

// ListBatches lists a workspace's batches, newest first.
func (s *service) ListBatches(ctx context.Context, orgID uuid.UUID, limit, offset int) ([]BatchView, int, *errx.Error) {
	if xerr := s.batchesReady(); xerr != nil {
		return nil, 0, xerr
	}
	batches, total, err := s.Batches.ListBatches(ctx, orgID, limit, offset)
	if err != nil {
		errs.CaptureException(err)
		return nil, 0, errx.InternalError()
	}
	views, xerr := s.batchViews(ctx, batches)
	if xerr != nil {
		return nil, 0, xerr
	}
	return views, total, nil
}

func (s *service) batchViews(ctx context.Context, batches []models.PlacementBatch) ([]BatchView, *errx.Error) {
	ids := make([]uuid.UUID, len(batches))
	for i, b := range batches {
		ids[i] = b.ID
	}
	progress, err := s.Batches.BatchProgress(ctx, ids)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	summaries, err := s.Batches.BatchSummaries(ctx, ids)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	out := make([]BatchView, 0, len(batches))
	for _, b := range batches {
		stripBody(&b)
		sum := summaries[b.ID]
		sum.Finish()
		out = append(out, BatchView{PlacementBatch: b, Progress: progress[b.ID], Summary: sum})
	}
	return out, nil
}

// GetBatch is one batch with its placement overall, by sending domain and
// provider, by recipient provider, and as a domain by recipient matrix.
func (s *service) GetBatch(ctx context.Context, orgID, id uuid.UUID) (*BatchDetail, *errx.Error) {
	if xerr := s.batchesReady(); xerr != nil {
		return nil, xerr
	}
	b, err := s.Batches.GetBatch(ctx, orgID, id)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	if b == nil {
		return nil, errx.New(errx.NotFound, "placement batch not found")
	}
	content := contentCheck(models.PlacementTest{Subject: b.Subject, BodyHTML: b.BodyHTML, BodyPlain: b.BodyPlain})
	views, xerr := s.batchViews(ctx, []models.PlacementBatch{*b})
	if xerr != nil {
		return nil, xerr
	}
	rows, err := s.Batches.BatchBreakdown(ctx, id)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	sizes, err := s.Batches.BatchGroupSizes(ctx, id)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	d := buildBatchDetail(views[0], rows, sizes)
	d.Content = content
	return d, nil
}

// buildBatchDetail folds the breakdown rows into the detail's groups.
func buildBatchDetail(v BatchView, rows []repository.PlacementBreakdownRow, sizes []repository.PlacementBatchGroupSize) *BatchDetail {
	d := &BatchDetail{BatchView: v, Domains: []BatchGroup{}, Providers: []BatchGroup{}, Recipients: []models.PlacementFamilyCounts{}, Matrix: []BatchMatrixRow{}}
	compare := v.Tracking == models.PlacementTrackingCompare
	var headline, untracked models.PlacementCounts
	domains := map[string]*BatchGroup{}
	providers := map[string]*BatchGroup{}
	recipients := map[string]*models.PlacementCounts{}
	matrix := map[string]map[string]*models.PlacementCounts{}
	group := func(m map[string]*BatchGroup, key, label string) *BatchGroup {
		g, ok := m[key]
		if !ok {
			g = &BatchGroup{Key: key, Label: label}
			m[key] = g
		}
		return g
	}
	for _, r := range rows {
		switch r.Set {
		case "overall":
			if compare && !r.Tracked {
				untracked.AddN(r.Folder, r.Count)
			} else {
				headline.AddN(r.Folder, r.Count)
			}
		case "domain":
			group(domains, r.SenderDomain, r.SenderDomain).Counts.AddN(r.Folder, r.Count)
		case "provider":
			group(providers, r.SenderFamily, familyLabel(r.SenderFamily)).Counts.AddN(r.Folder, r.Count)
		case "recipient":
			c, ok := recipients[r.RecipientFamily]
			if !ok {
				c = &models.PlacementCounts{}
				recipients[r.RecipientFamily] = c
			}
			c.AddN(r.Folder, r.Count)
		case "matrix":
			row, ok := matrix[r.SenderDomain]
			if !ok {
				row = map[string]*models.PlacementCounts{}
				matrix[r.SenderDomain] = row
			}
			c, ok := row[r.RecipientFamily]
			if !ok {
				c = &models.PlacementCounts{}
				row[r.RecipientFamily] = c
			}
			c.AddN(r.Folder, r.Count)
		}
	}
	for _, sz := range sizes {
		if sz.ByDomain {
			g := group(domains, sz.Domain, sz.Domain)
			g.Senders, g.Tested = sz.Senders, sz.Completed
		} else {
			g := group(providers, sz.Family, familyLabel(sz.Family))
			g.Senders, g.Tested = sz.Senders, sz.Completed
		}
	}
	headline.Finish()
	d.Summary = headline
	if compare {
		untracked.Finish()
		d.Untracked = &untracked
	}
	flatten := func(m map[string]*BatchGroup) []BatchGroup {
		out := make([]BatchGroup, 0, len(m))
		for _, g := range m {
			g.Counts.Finish()
			out = append(out, *g)
		}
		sort.Slice(out, func(i, j int) bool { return worseGroup(out[i], out[j]) })
		return out
	}
	d.Domains = flatten(domains)
	d.Providers = flatten(providers)
	for fam, c := range recipients {
		c.Finish()
		d.Recipients = append(d.Recipients, models.PlacementFamilyCounts{Family: fam, Label: familyLabel(fam), Counts: *c})
	}
	sort.Slice(d.Recipients, func(i, j int) bool { return d.Recipients[i].Label < d.Recipients[j].Label })
	for _, g := range d.Domains {
		row, ok := matrix[g.Key]
		if !ok {
			continue
		}
		mr := BatchMatrixRow{Domain: g.Key, Recipients: make([]models.PlacementFamilyCounts, 0, len(d.Recipients))}
		for _, rc := range d.Recipients {
			c := models.PlacementCounts{}
			if got, ok := row[rc.Family]; ok {
				c = *got
			}
			c.Finish()
			mr.Recipients = append(mr.Recipients, models.PlacementFamilyCounts{Family: rc.Family, Label: rc.Label, Counts: c})
		}
		d.Matrix = append(d.Matrix, mr)
	}
	return d
}

// worseGroup orders groups lowest inbox rate first, untested last, then the
// larger group first.
func worseGroup(a, b BatchGroup) bool {
	ra, rb := a.Counts.InboxRate, b.Counts.InboxRate
	switch {
	case ra != nil && rb == nil:
		return true
	case ra == nil && rb != nil:
		return false
	case ra != nil && rb != nil && *ra != *rb:
		return *ra < *rb
	}
	if a.Senders != b.Senders {
		return a.Senders > b.Senders
	}
	return a.Key < b.Key
}

// ListBatchSenders lists a batch's senders with where each one's copies
// landed, worst inbox rate first by default.
func (s *service) ListBatchSenders(ctx context.Context, orgID, id uuid.UUID, f repository.PlacementBatchSenderFilter) ([]BatchSenderView, int, *errx.Error) {
	if xerr := s.batchesReady(); xerr != nil {
		return nil, 0, xerr
	}
	switch f.Sort {
	case "", "worst", "best", "email", "status":
	default:
		return nil, 0, errx.New(errx.BadRequest, "sort must be worst, best, email or status")
	}
	switch f.Status {
	case "", models.PlacementSenderQueued, models.PlacementSenderDeferred, models.PlacementSenderRunning,
		models.PlacementSenderCompleted, models.PlacementSenderSkipped, models.PlacementSenderFailed, models.PlacementSenderCancelled:
	default:
		return nil, 0, errx.New(errx.BadRequest, "invalid status")
	}
	if len(f.Search) > 200 {
		return nil, 0, errx.New(errx.BadRequest, "search is too long")
	}
	b, err := s.Batches.GetBatch(ctx, orgID, id)
	if err != nil {
		errs.CaptureException(err)
		return nil, 0, errx.InternalError()
	}
	if b == nil {
		return nil, 0, errx.New(errx.NotFound, "placement batch not found")
	}
	rows, total, err := s.Batches.ListBatchSenders(ctx, orgID, id, f)
	if err != nil {
		errs.CaptureException(err)
		return nil, 0, errx.InternalError()
	}
	out := make([]BatchSenderView, 0, len(rows))
	for _, r := range rows {
		out = append(out, BatchSenderView{
			PlacementBatchSender: r.PlacementBatchSender,
			SenderFamilyLabel:    familyLabel(r.SenderFamily),
			Summary:              r.Counts,
			TestIDs:              r.TestIDs,
		})
	}
	return out, total, nil
}

// CancelBatch stops a batch: no sender starts again, copies not sent yet are
// cancelled, and copies already sent keep being classified.
func (s *service) CancelBatch(ctx context.Context, orgID, id uuid.UUID) (*BatchView, *errx.Error) {
	if xerr := s.batchesReady(); xerr != nil {
		return nil, xerr
	}
	ok, running, err := s.Batches.CancelBatch(ctx, orgID, id)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	for _, testID := range running {
		if _, err := s.Repo.CancelTest(ctx, orgID, testID); err != nil {
			errs.CaptureException(err)
		}
	}
	b, err := s.Batches.GetBatch(ctx, orgID, id)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	if b == nil {
		return nil, errx.New(errx.NotFound, "placement batch not found")
	}
	if !ok {
		return nil, placementErr(errx.Conflict, "placement_batch_not_running", "This placement batch is no longer running.")
	}
	s.publishBatch(ctx, b)
	views, xerr := s.batchViews(ctx, []models.PlacementBatch{*b})
	if xerr != nil {
		return nil, xerr
	}
	return &views[0], nil
}

// Coverage is how much of the workspace's connected fleet delivered a
// placement test recently.
func (s *service) Coverage(ctx context.Context, orgID uuid.UUID) (*repository.PlacementCoverage, *errx.Error) {
	if xerr := s.batchesReady(); xerr != nil {
		return nil, xerr
	}
	c, err := s.Batches.Coverage(ctx, orgID, s.now())
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	return &c, nil
}
