package tasks

import (
	"testing"

	"github.com/google/uuid"
)

func TestPickWeightedPartner_FallsBackToUniformWithoutDomains(t *testing.T) {
	a := uuid.New()
	b := uuid.New()
	got := pickWeightedPartner([]uuid.UUID{a, b}, nil, nil)
	if got != a && got != b {
		t.Errorf("expected a or b, got %v", got)
	}
}

func TestPickWeightedPartner_PrefersUnderRepresentedDomain(t *testing.T) {
	saturatedID := uuid.New()
	freshID := uuid.New()
	domainsByID := map[uuid.UUID]string{
		saturatedID: "gmail.com",
		freshID:     "fastmail.com",
	}
	domainCounts := map[string]int{
		"gmail.com":    99, // very saturated
		"fastmail.com": 0,  // never used
	}

	freshHits := 0
	iterations := 2000
	for i := 0; i < iterations; i++ {
		picked := pickWeightedPartner([]uuid.UUID{saturatedID, freshID}, domainsByID, domainCounts)
		if picked == freshID {
			freshHits++
		}
	}

	// fresh weight = 1.0, saturated weight = 1/100 = 0.01.
	// Expected fresh share ≈ 1.0 / 1.01 ≈ 99%.
	if freshHits < int(float64(iterations)*0.9) {
		t.Errorf("fresh domain should dominate selection; got %d/%d", freshHits, iterations)
	}
}

func TestPickWeightedPartner_SingleCandidateReturnsIt(t *testing.T) {
	id := uuid.New()
	got := pickWeightedPartner([]uuid.UUID{id}, map[uuid.UUID]string{id: "x.com"}, map[string]int{"x.com": 5})
	if got != id {
		t.Errorf("single candidate should be returned; got %v", got)
	}
}
