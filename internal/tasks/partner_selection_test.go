package tasks

import (
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func TestPickWeightedPartner_FallsBackToUniformWithoutDomains(t *testing.T) {
	a := uuid.New()
	b := uuid.New()
	got := pickWeightedPartner([]uuid.UUID{a, b}, partnerSignals{})
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
		picked := pickWeightedPartner([]uuid.UUID{saturatedID, freshID}, partnerSignals{domainsByID: domainsByID, domainCounts: domainCounts})
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
	got := pickWeightedPartner([]uuid.UUID{id}, partnerSignals{domainsByID: map[uuid.UUID]string{id: "x.com"}, domainCounts: map[string]int{"x.com": 5}})
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
	// The selector resolves each rule once, into the weight it produced.
	ruleWeight := map[uuid.UUID]float64{googleRecipient: 10.0, microsoftRecipient: 1.0}

	googleHits := 0
	iterations := 2000
	for i := 0; i < iterations; i++ {
		picked := pickWeightedPartner(
			[]uuid.UUID{googleRecipient, microsoftRecipient},
			partnerSignals{domainsByID: domainsByID, ruleWeight: ruleWeight},
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

// A candidate with no rule of its own weighs the same as one with no rules at
// all; a bare map lookup would have zeroed it and excluded everybody.
func TestPickWeightedPartner_MissingRuleWeightIsNeutral(t *testing.T) {
	ruled := uuid.New()
	unruled := uuid.New()
	sig := partnerSignals{
		domainsByID:  map[uuid.UUID]string{ruled: "a.com", unruled: "b.com"},
		domainCounts: map[string]int{"a.com": 0, "b.com": 0},
		ruleWeight:   map[uuid.UUID]float64{ruled: 1.0},
	}

	unruledHits := 0
	iterations := 2000
	for i := 0; i < iterations; i++ {
		if pickWeightedPartner([]uuid.UUID{ruled, unruled}, sig) == unruled {
			unruledHits++
		}
	}
	if unruledHits < iterations/4 {
		t.Fatalf("the unruled partner was picked %d/%d times; equal weights should be near half", unruledHits, iterations)
	}
}

func TestHostPenalty(t *testing.T) {
	partner := uuid.New()
	// Keys are mailhost.Host values: who RUNS the recipient's mail.
	sig := func(delivered, spam int) partnerSignals {
		return partnerSignals{
			hostsByID:       map[uuid.UUID]string{partner: "microsoft365"},
			placementByHost: map[string]repository.HostPlacementStat{"microsoft365": {Delivered: delivered, Spam: spam}},
		}
	}

	if got := sig(100, 0).hostPenalty(partner); got != 1.0 {
		t.Errorf("clean provider penalty = %v, want 1.0", got)
	}
	// 25% junk rate halves the weight at k=4.
	if got := sig(100, 25).hostPenalty(partner); got != 0.5 {
		t.Errorf("25%% rate penalty = %v, want 0.5", got)
	}
	// Even total failure downweights rather than excluding: a sender that stops
	// mailing a provider never learns whether it recovered.
	if got := sig(100, 100).hostPenalty(partner); got <= 0 {
		t.Errorf("total-failure penalty = %v, want a positive weight", got)
	}
	// One placement out of two sends is noise, not a pattern.
	if got := sig(2, 1).hostPenalty(partner); got != 1.0 {
		t.Errorf("below-sample penalty = %v, want 1.0", got)
	}
	if got := (partnerSignals{}).hostPenalty(partner); got != 1.0 {
		t.Errorf("no-signal penalty = %v, want 1.0", got)
	}
	if got := sig(100, 25).hostPenalty(uuid.New()); got != 1.0 {
		t.Errorf("unknown-partner penalty = %v, want 1.0", got)
	}
}

func TestPickWeightedPartner_AvoidsTheProviderItKeepsLandingInJunkAt(t *testing.T) {
	msID := uuid.New()
	googleID := uuid.New()
	sig := partnerSignals{
		// Equal domain frequency, so per-provider placement is the only signal
		// separating the two.
		domainsByID:  map[uuid.UUID]string{msID: "outlook.com", googleID: "gmail.com"},
		domainCounts: map[string]int{"outlook.com": 1, "gmail.com": 1},
		hostsByID:    map[uuid.UUID]string{msID: "microsoft365", googleID: "google_workspace"},
		placementByHost: map[string]repository.HostPlacementStat{
			"microsoft365":     {Delivered: 40, Spam: 20}, // 50% junk
			"google_workspace": {Delivered: 40, Spam: 0},  // clean
		},
	}

	googleHits := 0
	iterations := 4000
	for i := 0; i < iterations; i++ {
		if pickWeightedPartner([]uuid.UUID{msID, googleID}, sig) == googleID {
			googleHits++
		}
	}
	// Microsoft weight = 1/(1+4*0.5) = a third of Google's, so Google ≈ 75%.
	if googleHits < int(float64(iterations)*0.65) {
		t.Errorf("the failing provider should be downweighted; google got %d/%d", googleHits, iterations)
	}
	if googleHits == iterations {
		t.Error("the failing provider was excluded entirely; it must stay reachable")
	}
}

// Custom domains are told apart by who hosts them, so junk at a small host
// never costs a Workspace partner its draw weight, or the other way round.
func TestPartnerHostKeysByWhoRunsTheMail(t *testing.T) {
	cases := []struct {
		name string
		c    models.WarmupPartnerCandidate
		want string
	}{
		{"workspace over oauth", models.WarmupPartnerCandidate{Email: "a@acme.test", Provider: "gmail"}, "google_workspace"},
		{"workspace over imap", models.WarmupPartnerCandidate{Email: "b@acme.test", Provider: "smtp_imap", MailHost: "google_workspace"}, "google_workspace"},
		{"small host", models.WarmupPartnerCandidate{Email: "c@shop.test", Provider: "smtp_imap", MailHost: "hostinger"}, "hostinger"},
		{"consumer gmail", models.WarmupPartnerCandidate{Email: "d@gmail.com", Provider: "smtp_imap"}, "gmail"},
		{"undetected", models.WarmupPartnerCandidate{Email: "e@shop.test", Provider: "smtp_imap"}, "other"},
	}
	for _, tc := range cases {
		if got := partnerHost(tc.c); got != tc.want {
			t.Errorf("%s: host = %q, want %q", tc.name, got, tc.want)
		}
	}
}
