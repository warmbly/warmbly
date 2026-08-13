package guardrail

import (
	"strings"
	"testing"

	"github.com/warmbly/warmbly/internal/repository"
)

// base is a campaign carrying the shipped defaults: 5% bounce ceiling, 0.10%
// complaint ceiling, reply floor off, 50-send sample floor.
func base() repository.GuardrailCampaign {
	return repository.GuardrailCampaign{
		BounceRateMax:    5,
		ComplaintRateMax: 0.10,
		ReplyRateMin:     0,
		MinSample:        50,
		WindowDays:       7,
		Sent:             1000,
	}
}

func TestQuietWhenEverythingIsHealthy(t *testing.T) {
	c := base()
	c.Bounced = 10 // 1%
	c.Replied = 40 // 4%
	c.Complaints = 0
	if b := Evaluate(c); b != nil {
		t.Fatalf("healthy campaign tripped %s: %s", b.Rule, b.Reason)
	}
}

func TestSampleFloorSuppressesNoise(t *testing.T) {
	c := base()
	c.Sent = 4
	c.Bounced = 1 // 25%, but on four sends
	if b := Evaluate(c); b != nil {
		t.Fatalf("tripped below the sample floor: %s", b.Reason)
	}
}

func TestZeroSendsNeverTrips(t *testing.T) {
	c := base()
	c.Sent = 0
	c.MinSample = 1
	if b := Evaluate(c); b != nil {
		t.Fatalf("tripped with no sends: %s", b.Reason)
	}
}

func TestBounceCeiling(t *testing.T) {
	c := base()
	c.Bounced = 60 // 6%
	b := Evaluate(c)
	if b == nil || b.Rule != RuleBounceRate {
		t.Fatalf("expected a bounce breach, got %+v", b)
	}
	if b.Observed < 5.9 || b.Observed > 6.1 {
		t.Fatalf("observed = %v, want ~6", b.Observed)
	}
	if !strings.Contains(b.Reason, "6.0%") || !strings.Contains(b.Reason, "5.0%") {
		t.Fatalf("reason should name both numbers: %q", b.Reason)
	}
}

func TestBounceCeilingIsInclusive(t *testing.T) {
	c := base()
	c.Bounced = 50 // exactly 5%
	if b := Evaluate(c); b == nil || b.Rule != RuleBounceRate {
		t.Fatalf("a rate exactly at the ceiling should trip, got %+v", b)
	}
}

func TestComplaintCeilingUsesTwoDecimals(t *testing.T) {
	c := base()
	c.Complaints = 2 // 0.20%
	b := Evaluate(c)
	if b == nil || b.Rule != RuleComplaintRate {
		t.Fatalf("expected a complaint breach, got %+v", b)
	}
	if !strings.Contains(b.Reason, "0.20%") {
		t.Fatalf("sub-1%% rates need two decimals to be actionable: %q", b.Reason)
	}
}

func TestComplaintsOutrankBounces(t *testing.T) {
	c := base()
	c.Complaints = 5 // 0.5%
	c.Bounced = 200  // 20%
	b := Evaluate(c)
	if b == nil || b.Rule != RuleComplaintRate {
		t.Fatalf("complaints should win when both breach, got %+v", b)
	}
}

func TestReplyFloorIsOffByDefault(t *testing.T) {
	c := base()
	c.Replied = 0
	if b := Evaluate(c); b != nil {
		t.Fatalf("reply floor should be opt-in, tripped: %s", b.Reason)
	}
}

func TestReplyFloorTripsWhenConfigured(t *testing.T) {
	c := base()
	c.ReplyRateMin = 1.0
	c.Replied = 5 // 0.5%
	b := Evaluate(c)
	if b == nil || b.Rule != RuleReplyRate {
		t.Fatalf("expected a reply-rate breach, got %+v", b)
	}
	if !strings.Contains(b.Reason, "below") {
		t.Fatalf("a floor breach should read as below, not above: %q", b.Reason)
	}
}

func TestReplyFloorIsExclusiveAtTheThreshold(t *testing.T) {
	c := base()
	c.ReplyRateMin = 1.0
	c.Replied = 10 // exactly 1%
	if b := Evaluate(c); b != nil {
		t.Fatalf("a reply rate exactly at the floor is acceptable, tripped: %s", b.Reason)
	}
}

func TestThresholdOfZeroDisablesARule(t *testing.T) {
	c := base()
	c.BounceRateMax = 0
	c.ComplaintRateMax = 0
	c.Bounced = 1000    // 100%
	c.Complaints = 1000 // 100%
	if b := Evaluate(c); b != nil {
		t.Fatalf("disabled rules should never fire: %s", b.Reason)
	}
}

func TestBreachCarriesTheSampleItWasComputedOver(t *testing.T) {
	c := base()
	c.Sent = 731
	c.Bounced = 100
	b := Evaluate(c)
	if b == nil {
		t.Fatal("expected a breach")
	}
	if b.Sample != 731 {
		t.Fatalf("sample = %d, want 731", b.Sample)
	}
	if !strings.Contains(b.Reason, "731 sends") {
		t.Fatalf("reason should state the sample: %q", b.Reason)
	}
}
