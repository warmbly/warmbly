package tasks

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	warmupapp "github.com/warmbly/warmbly/internal/app/warmup"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// candidateRepo serves a fixed candidate set and a sender with no history.
type candidateRepo struct {
	repository.WarmupRepository

	candidates []models.WarmupPartnerCandidate
}

func (r candidateRepo) WarmupPartnerCandidates(context.Context, string, uuid.UUID) ([]models.WarmupPartnerCandidate, error) {
	return r.candidates, nil
}

func (candidateRepo) SenderPlacementByHost(context.Context, uuid.UUID, time.Time) (map[string]repository.HostPlacementStat, error) {
	return nil, nil
}

func (candidateRepo) GetRecentlyUsedPartners(context.Context, uuid.UUID, time.Time) ([]uuid.UUID, error) {
	return nil, nil
}

func (candidateRepo) GetRecentPartnerDomainCounts(context.Context, uuid.UUID, time.Time) (map[string]int, error) {
	return nil, nil
}

func (candidateRepo) GetRecentPartnerCounts(context.Context, uuid.UUID, time.Time) (map[uuid.UUID]int, error) {
	return nil, nil
}

// rejectingGate is pinned like the real one and refuses the listed ids.
type rejectingGate struct {
	warmupapp.Service

	poolOf   map[uuid.UUID]string
	rejected map[uuid.UUID]bool
	asked    []gateCall
}

type gateCall struct {
	id   uuid.UUID
	pool string
}

func (g *rejectingGate) CanParticipate(_ context.Context, id uuid.UUID, poolType string) (bool, string, *errx.Error) {
	g.asked = append(g.asked, gateCall{id, poolType})
	if g.poolOf[id] != poolType {
		return false, "not_in_pool", nil
	}
	if g.rejected[id] {
		return false, "blocked", nil
	}
	return true, "", nil
}

// ruleRepo serves one organization's routing rules; the rest of the interface panics.
type ruleRepo struct {
	repository.WarmupRoutingRepository

	rules []models.WarmupRoutingRule
}

func (r ruleRepo) ListForOrganization(context.Context, uuid.UUID) ([]models.WarmupRoutingRule, error) {
	return r.rules, nil
}

func premiumSelector(gate *rejectingGate, cands ...models.WarmupPartnerCandidate) (*tasksService, Email) {
	return premiumSelectorWithRules(gate, nil, cands...)
}

func premiumSelectorWithRules(gate *rejectingGate, rules []models.WarmupRoutingRule, cands ...models.WarmupPartnerCandidate) (*tasksService, Email) {
	org := uuid.New()
	s := &tasksService{
		warmupRepo:        candidateRepo{candidates: cands},
		emailRepo:         directedEmailRepo{},
		warmupHealth:      gate,
		warmupRoutingRepo: ruleRepo{rules: rules},
	}
	return s, Email{ID: uuid.New(), Email: "sender@paid.test", OrganizationID: &org, WarmupPoolType: "premium"}
}

// borrowedFree is a proven free mailbox filling in a thin premium tier.
func borrowedFree(email string) models.WarmupPartnerCandidate {
	return models.WarmupPartnerCandidate{ID: uuid.New(), Email: email, PoolType: "free", Origin: models.WarmupPartnerBorrowed}
}

// returnVisit is a paying mailbox that wrote to a free sender recently.
func returnVisit(email string) models.WarmupPartnerCandidate {
	return models.WarmupPartnerCandidate{ID: uuid.New(), Email: email, PoolType: "premium", Origin: models.WarmupPartnerReturn}
}

func freeSelector(gate *rejectingGate, cands ...models.WarmupPartnerCandidate) (*tasksService, Email) {
	org := uuid.New()
	s := &tasksService{
		warmupRepo:        candidateRepo{candidates: cands},
		emailRepo:         directedEmailRepo{},
		warmupHealth:      gate,
		warmupRoutingRepo: ruleRepo{},
	}
	return s, Email{ID: uuid.New(), Email: "sender@trial.test", OrganizationID: &org, WarmupPoolType: "free"}
}

// excludeDomain is the customer saying "never warm with this domain".
func excludeDomain(domain string) []models.WarmupRoutingRule {
	return []models.WarmupRoutingRule{{
		Enabled:             true,
		Name:                "exclude " + domain,
		Priority:            1,
		SenderMatchType:     models.WarmupMatchAny,
		RecipientMatchType:  models.WarmupMatchDomain,
		RecipientMatchValue: domain,
		Weight:              0,
	}}
}

func TestSelectWarmupPartnerDrawsOwnTierBeforeBorrowed(t *testing.T) {
	own := models.WarmupPartnerCandidate{ID: uuid.New(), Email: "own@paid.test"}
	gate := &rejectingGate{poolOf: map[uuid.UUID]string{own.ID: "premium"}}
	// Many borrowed candidates, so losing the preference is a near-certain failure, not a coin flip.
	cands := []models.WarmupPartnerCandidate{}
	for i := 0; i < 8; i++ {
		free := borrowedFree("free@trial.test")
		gate.poolOf[free.ID] = "free"
		cands = append(cands, free)
	}
	s, sender := premiumSelector(gate, append(cands, own)...)

	partner, err := s.selectWarmupPartner(context.Background(), sender)
	if err != nil {
		t.Fatal(err)
	}
	if partner.ID != own.ID {
		t.Fatalf("drew %s, want the own-tier partner %s", partner.ID, own.ID)
	}
	if len(gate.asked) != 1 || gate.asked[0] != (gateCall{own.ID, "premium"}) {
		t.Fatalf("gated %v, want the own-tier partner once in premium", gate.asked)
	}
}

// A stale own-tier row (an expired block the gate re-blocks) must not hide a
// healthy borrowed partner: the draw falls through to the next bucket.
func TestSelectWarmupPartnerFallsThroughWhenOwnTierFailsTheGate(t *testing.T) {
	stale := models.WarmupPartnerCandidate{ID: uuid.New(), Email: "stale@paid.test"}
	free := borrowedFree("free@trial.test")
	gate := &rejectingGate{
		poolOf:   map[uuid.UUID]string{stale.ID: "premium", free.ID: "free"},
		rejected: map[uuid.UUID]bool{stale.ID: true},
	}
	s, sender := premiumSelector(gate, stale, free)

	partner, err := s.selectWarmupPartner(context.Background(), sender)
	if err != nil {
		t.Fatalf("no partner although a healthy borrowed one was available: %v", err)
	}
	if partner.ID != free.ID {
		t.Fatalf("drew %s, want the borrowed partner %s", partner.ID, free.ID)
	}
	want := []gateCall{{stale.ID, "premium"}, {free.ID, "free"}}
	if len(gate.asked) != 2 || gate.asked[0] != want[0] || gate.asked[1] != want[1] {
		t.Fatalf("gated %v, want %v (each pinned to the pool it was drawn from)", gate.asked, want)
	}
}

func TestSelectWarmupPartnerReportsAnEmptyCandidateSet(t *testing.T) {
	s, sender := premiumSelector(&rejectingGate{})
	if _, err := s.selectWarmupPartner(context.Background(), sender); err != errNoWarmupPartners {
		t.Fatalf("err = %v, want errNoWarmupPartners", err)
	}
}

// The draw ends by exhaustion, not by a fixed attempt count, so many stale
// own-tier rows cannot starve a healthy borrowed partner.
func TestSelectWarmupPartnerDrawEndsByExhaustion(t *testing.T) {
	gate := &rejectingGate{poolOf: map[uuid.UUID]string{}, rejected: map[uuid.UUID]bool{}}
	cands := []models.WarmupPartnerCandidate{}
	for i := 0; i < 7; i++ {
		stale := models.WarmupPartnerCandidate{ID: uuid.New(), Email: "stale@paid.test"}
		gate.poolOf[stale.ID] = "premium"
		gate.rejected[stale.ID] = true
		cands = append(cands, stale)
	}
	free := borrowedFree("free@trial.test")
	gate.poolOf[free.ID] = "free"
	s, sender := premiumSelector(gate, append(cands, free)...)

	partner, err := s.selectWarmupPartner(context.Background(), sender)
	if err != nil {
		t.Fatalf("seven stale own-tier rows starved a healthy borrowed partner: %v", err)
	}
	if partner.ID != free.ID {
		t.Fatalf("drew %s, want the borrowed partner %s", partner.ID, free.ID)
	}
}

// A weight of 0 is an exclusion, not a weight, so it has to survive a pool of
// one: weighting cannot express "never" when there is nothing to weigh against (#501).
func TestSelectWarmupPartnerHonoursAnExclusionAgainstTheOnlyCandidate(t *testing.T) {
	only := models.WarmupPartnerCandidate{ID: uuid.New(), Email: "one@blocked.test"}
	gate := &rejectingGate{poolOf: map[uuid.UUID]string{only.ID: "premium"}}
	s, sender := premiumSelectorWithRules(gate, excludeDomain("blocked.test"), only)

	partner, err := s.selectWarmupPartner(context.Background(), sender)
	if !errors.Is(err, errAllPartnersExcluded) {
		t.Fatalf("got (%v, %v), want errAllPartnersExcluded", partner, err)
	}
	if len(gate.asked) != 0 {
		t.Fatalf("an excluded candidate reached the health gate: %v", gate.asked)
	}
}

func TestSelectWarmupPartnerHonoursAnExclusionAgainstEveryCandidate(t *testing.T) {
	var cands []models.WarmupPartnerCandidate
	gate := &rejectingGate{poolOf: map[uuid.UUID]string{}}
	for i := 0; i < 4; i++ {
		c := models.WarmupPartnerCandidate{ID: uuid.New(), Email: "p@blocked.test"}
		gate.poolOf[c.ID] = "premium"
		cands = append(cands, c)
	}
	s, sender := premiumSelectorWithRules(gate, excludeDomain("blocked.test"), cands...)

	if partner, err := s.selectWarmupPartner(context.Background(), sender); !errors.Is(err, errAllPartnersExcluded) {
		t.Fatalf("got (%v, %v), want errAllPartnersExcluded", partner, err)
	}
}

// An exclusion removes only what it names; the rest of the pool still warms.
func TestSelectWarmupPartnerDrawsTheCandidatesAnExclusionLeaves(t *testing.T) {
	blocked := models.WarmupPartnerCandidate{ID: uuid.New(), Email: "no@blocked.test"}
	allowed := models.WarmupPartnerCandidate{ID: uuid.New(), Email: "yes@allowed.test"}
	gate := &rejectingGate{poolOf: map[uuid.UUID]string{blocked.ID: "premium", allowed.ID: "premium"}}
	s, sender := premiumSelectorWithRules(gate, excludeDomain("blocked.test"), blocked, allowed)

	for i := 0; i < 20; i++ {
		partner, err := s.selectWarmupPartner(context.Background(), sender)
		if err != nil {
			t.Fatalf("pick %d: %v", i, err)
		}
		if partner.ID != allowed.ID {
			t.Fatalf("pick %d drew the excluded partner %s", i, partner.ID)
		}
	}
}

// The reply-back path commits a pair too, so the exclusion holds there: the
// thread exists only because the other side started it.
func TestDirectedWarmupPartnerRefusesAnExcludedTarget(t *testing.T) {
	target := uuid.New()
	org := uuid.New()
	gate := &pinnedGate{poolOf: map[uuid.UUID]string{target: "premium"}}
	s := &tasksService{
		taskRepo:          &directedTaskRepo{target: target},
		emailRepo:         addressedEmailRepo{email: "them@blocked.test"},
		warmupHealth:      gate,
		orgRiskRepo:       riskRepo{state: models.OrgRiskTrusted},
		warmupRoutingRepo: ruleRepo{rules: excludeDomain("blocked.test")},
	}
	sender := &Email{ID: uuid.New(), Email: "sender@paid.test", OrganizationID: &org, WarmupPoolType: "premium"}

	if partner := s.directedWarmupPartner(context.Background(), uuid.New(), sender, "premium"); partner != nil {
		t.Fatalf("replied to %s, which the customer excluded", partner.Email)
	}
}

func TestDirectedWarmupPartnerAnswersAnAllowedTarget(t *testing.T) {
	target := uuid.New()
	org := uuid.New()
	gate := &pinnedGate{poolOf: map[uuid.UUID]string{target: "premium"}}
	s := &tasksService{
		taskRepo:          &directedTaskRepo{target: target},
		emailRepo:         addressedEmailRepo{email: "them@allowed.test"},
		warmupHealth:      gate,
		orgRiskRepo:       riskRepo{state: models.OrgRiskTrusted},
		warmupRoutingRepo: ruleRepo{rules: excludeDomain("blocked.test")},
	}
	sender := &Email{ID: uuid.New(), Email: "sender@paid.test", OrganizationID: &org, WarmupPoolType: "premium"}

	if partner := s.directedWarmupPartner(context.Background(), uuid.New(), sender, "premium"); partner == nil {
		t.Fatal("refused a target no rule excludes")
	}
}

// A return visit ranks with the sender's own tier, not after it: with eight
// fresh siblings ahead of it a borrowed-ranked candidate would never be drawn,
// and the paying inbox would keep receiving nothing (#633).
func TestSelectWarmupPartnerDrawsAReturnVisitAlongsideOwnTier(t *testing.T) {
	gate := &rejectingGate{poolOf: map[uuid.UUID]string{}}
	var cands []models.WarmupPartnerCandidate
	for i := 0; i < 8; i++ {
		own := models.WarmupPartnerCandidate{ID: uuid.New(), Email: "own@trial.test", PoolType: "free", Origin: models.WarmupPartnerOwnTier}
		gate.poolOf[own.ID] = "free"
		cands = append(cands, own)
	}
	paid := returnVisit("paid@paid.test")
	// The paying inbox is owed everything it sent, so it carries the full boost.
	paid.Sent7d, paid.Received7d = 20, 0
	gate.poolOf[paid.ID] = "premium"
	s, sender := freeSelector(gate, append(cands, paid)...)

	drew := 0
	for i := 0; i < 400; i++ {
		partner, err := s.selectWarmupPartner(context.Background(), sender)
		if err != nil {
			t.Fatal(err)
		}
		if partner.ID == paid.ID {
			drew++
		}
	}
	// Eight siblings at weight 1 against one paying inbox at weight 4 is a
	// third of the draws; ranked after the own tier it would be none.
	if drew < 80 || drew > 220 {
		t.Fatalf("drew the paying inbox %d/400 times, want roughly a third", drew)
	}
	for _, call := range gate.asked {
		if call.id == paid.ID && call.pool != "premium" {
			t.Fatalf("gated the return visit in %q, want the pool it was drawn from", call.pool)
		}
	}
}

// The gate for a return visit is pinned to the pool it was drawn from; gating
// it against the free sender's own pool would refuse every one.
func TestSelectWarmupPartnerGatesAReturnVisitInItsOwnPool(t *testing.T) {
	paid := returnVisit("paid@paid.test")
	gate := &rejectingGate{poolOf: map[uuid.UUID]string{paid.ID: "premium"}}
	s, sender := freeSelector(gate, paid)

	partner, err := s.selectWarmupPartner(context.Background(), sender)
	if err != nil {
		t.Fatalf("refused the only return visit: %v", err)
	}
	if partner.ID != paid.ID {
		t.Fatalf("drew %s, want the paying inbox %s", partner.ID, paid.ID)
	}
	if len(gate.asked) != 1 || gate.asked[0] != (gateCall{paid.ID, "premium"}) {
		t.Fatalf("gated %v, want the return visit once in premium", gate.asked)
	}
}

// Traffic flows towards the inbox that is owed the most, and the boost is
// gone once it is in balance.
func TestReciprocityBoostFavoursTheStarvedInbox(t *testing.T) {
	starved := uuid.New()
	balanced := uuid.New()
	overfed := uuid.New()
	sig := partnerSignals{starvation: map[uuid.UUID]float64{starved: 1, balanced: 0, overfed: 0}}
	if got := sig.reciprocityBoost(starved); got != 1+reciprocityBoostK {
		t.Fatalf("starved boost = %v, want %v", got, 1+reciprocityBoostK)
	}
	if got := sig.reciprocityBoost(balanced); got != 1 {
		t.Fatalf("balanced boost = %v, want 1", got)
	}
	if got := sig.reciprocityBoost(uuid.New()); got != 1 {
		t.Fatalf("an unknown candidate scored %v, want the neutral 1", got)
	}

	draws := map[uuid.UUID]int{}
	for i := 0; i < 2000; i++ {
		draws[pickWeightedPartner([]uuid.UUID{starved, balanced}, sig)]++
	}
	// Weight 4 against 1: four in five draws, give or take.
	if draws[starved] < 1400 || draws[starved] > 1800 {
		t.Fatalf("starved inbox drawn %d/2000, want about 1600", draws[starved])
	}
}
