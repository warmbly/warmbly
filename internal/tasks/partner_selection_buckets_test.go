package tasks

import (
	"context"
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

func (candidateRepo) SenderPlacementByProvider(context.Context, uuid.UUID, time.Time) (map[string]repository.ProviderPlacementStat, error) {
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

func premiumSelector(gate *rejectingGate, cands ...models.WarmupPartnerCandidate) (*tasksService, Email) {
	org := uuid.New()
	s := &tasksService{
		warmupRepo:   candidateRepo{candidates: cands},
		emailRepo:    directedEmailRepo{},
		warmupHealth: gate,
	}
	return s, Email{ID: uuid.New(), Email: "sender@paid.test", OrganizationID: &org, WarmupPoolType: "premium"}
}

func TestSelectWarmupPartnerDrawsOwnTierBeforeBorrowed(t *testing.T) {
	own := models.WarmupPartnerCandidate{ID: uuid.New(), Email: "own@paid.test"}
	gate := &rejectingGate{poolOf: map[uuid.UUID]string{own.ID: "premium"}}
	// Many borrowed candidates, so losing the preference is a near-certain failure, not a coin flip.
	cands := []models.WarmupPartnerCandidate{}
	for i := 0; i < 8; i++ {
		free := models.WarmupPartnerCandidate{ID: uuid.New(), Email: "free@trial.test", Borrowed: true}
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
	free := models.WarmupPartnerCandidate{ID: uuid.New(), Email: "free@trial.test", Borrowed: true}
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
	free := models.WarmupPartnerCandidate{ID: uuid.New(), Email: "free@trial.test", Borrowed: true}
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
