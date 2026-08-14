package dispatch

import "testing"

func TestAdaptiveStepUp10to15to20(t *testing.T) {
	h := AdaptiveHealth{Commits: 20, HardBounceRate: 0}
	d := EvaluateAdaptiveRate(10, 10, 20, 20, h)
	if d.Action != "step_up" || d.NextSendsPerHour != 15 {
		t.Fatalf("10→15 got action=%s rate=%d", d.Action, d.NextSendsPerHour)
	}
	if d.NextMinGap.Seconds() != 240 {
		t.Fatalf("min gap want 240s got %v", d.NextMinGap)
	}
	d = EvaluateAdaptiveRate(15, 10, 20, 20, h)
	if d.NextSendsPerHour != 20 || d.NextMinGap.Seconds() != 180 {
		t.Fatalf("15→20 got rate=%d gap=%v", d.NextSendsPerHour, d.NextMinGap)
	}
	d = EvaluateAdaptiveRate(20, 10, 20, 20, h)
	if d.Action != "hold" || d.NextSendsPerHour != 20 {
		t.Fatalf("at max hold got action=%s rate=%d", d.Action, d.NextSendsPerHour)
	}
}

func TestAdaptiveRetreatOnBounceAndAuth(t *testing.T) {
	d := EvaluateAdaptiveRate(20, 10, 20, 20, AdaptiveHealth{Commits: 20, HardBounceRate: 0.03})
	if d.Action != "step_down" || d.NextSendsPerHour != 15 {
		t.Fatalf("bounce retreat got action=%s rate=%d", d.Action, d.NextSendsPerHour)
	}
	d = EvaluateAdaptiveRate(15, 10, 20, 20, AdaptiveHealth{Commits: 20, AuthFailure: true})
	if d.Action != "step_down" || d.NextSendsPerHour != 10 {
		t.Fatalf("auth retreat got action=%s rate=%d", d.Action, d.NextSendsPerHour)
	}
}

func TestAdaptiveHoldIncompleteBatch(t *testing.T) {
	d := EvaluateAdaptiveRate(10, 10, 20, 20, AdaptiveHealth{Commits: 5})
	if d.Action != "hold" || d.NextSendsPerHour != 10 {
		t.Fatalf("incomplete batch got action=%s rate=%d", d.Action, d.NextSendsPerHour)
	}
}

func TestMinGapForRate(t *testing.T) {
	if MinGapForRate(10).Seconds() != 360 {
		t.Fatal("10/h → 360s")
	}
	if MinGapForRate(15).Seconds() != 240 {
		t.Fatal("15/h → 240s")
	}
	if MinGapForRate(20).Seconds() != 180 {
		t.Fatal("20/h → 180s")
	}
}
