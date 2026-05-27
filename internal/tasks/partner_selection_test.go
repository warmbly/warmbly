package tasks

import (
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

func TestPickWeightedPartner_FallsBackToUniformWithoutDomains(t *testing.T) {
	a := uuid.New()
	b := uuid.New()
	got := pickWeightedPartner([]uuid.UUID{a, b}, nil, nil, nil, "", nil)
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
		picked := pickWeightedPartner([]uuid.UUID{saturatedID, freshID}, domainsByID, domainCounts, nil, "", nil)
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
	got := pickWeightedPartner([]uuid.UUID{id}, map[uuid.UUID]string{id: "x.com"}, map[string]int{"x.com": 5}, nil, "", nil)
	if got != id {
		t.Errorf("single candidate should be returned; got %v", got)
	}
}

func TestPickWeightedPartner_RoutingRulePrefersProviderMatch(t *testing.T) {
	googleRecipient := uuid.New()
	microsoftRecipient := uuid.New()
	domainsByID := map[uuid.UUID]string{
		googleRecipient:    "gmail.com",
		microsoftRecipient: "outlook.com",
	}
	emailsByID := map[uuid.UUID]string{
		googleRecipient:    "g@gmail.com",
		microsoftRecipient: "m@outlook.com",
	}
	rules := []models.WarmupRoutingRule{
		{
			Enabled:             true,
			Name:                "google-to-google",
			Priority:            1,
			SenderMatchType:     models.WarmupMatchProvider,
			SenderMatchValue:    "google",
			RecipientMatchType:  models.WarmupMatchProvider,
			RecipientMatchValue: "google",
			Weight:              10.0,
		},
	}

	googleHits := 0
	iterations := 2000
	for i := 0; i < iterations; i++ {
		picked := pickWeightedPartner(
			[]uuid.UUID{googleRecipient, microsoftRecipient},
			domainsByID, nil, rules, "sender@gmail.com", emailsByID,
		)
		if picked == googleRecipient {
			googleHits++
		}
	}
	// 10x preference should clearly dominate.
	if googleHits < int(float64(iterations)*0.85) {
		t.Errorf("routing rule should heavily favor Google→Google; got %d/%d", googleHits, iterations)
	}
}

func TestPickWeightedPartner_RoutingRuleZeroWeightExcludes(t *testing.T) {
	allowedID := uuid.New()
	blockedID := uuid.New()
	domainsByID := map[uuid.UUID]string{
		allowedID: "good.com",
		blockedID: "blocked.com",
	}
	emailsByID := map[uuid.UUID]string{
		allowedID: "a@good.com",
		blockedID: "b@blocked.com",
	}
	rules := []models.WarmupRoutingRule{
		{
			Enabled:             true,
			Name:                "exclude-blocked",
			Priority:            1,
			SenderMatchType:     models.WarmupMatchAny,
			RecipientMatchType:  models.WarmupMatchDomain,
			RecipientMatchValue: "blocked.com",
			Weight:              0,
		},
	}

	for i := 0; i < 500; i++ {
		picked := pickWeightedPartner(
			[]uuid.UUID{allowedID, blockedID},
			domainsByID, nil, rules, "sender@whatever.io", emailsByID,
		)
		if picked == blockedID {
			t.Fatalf("weight=0 rule should exclude blocked partner")
		}
	}
}
