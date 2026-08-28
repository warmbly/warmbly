package warmupramp

import (
	"testing"
	"time"
)

func TestColdStartBandsOnWarmupMaturity(t *testing.T) {
	for _, tt := range []struct{ days, want int }{
		{0, coldStartUnproven}, {6, coldStartUnproven},
		{7, coldStartWarmed}, {13, coldStartWarmed},
		{14, coldStartMature}, {90, coldStartMature},
	} {
		if got := ColdStart(tt.days); got != tt.want {
			t.Errorf("ColdStart(%d) = %d, want %d", tt.days, got, tt.want)
		}
	}
}

func TestColdCeilingClimbsAndClamps(t *testing.T) {
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	at := func(d int) time.Time { return start.AddDate(0, 0, d) }

	// A mature mailbox on its first cold day: its starting volume, not the cap.
	// This is the overnight 40 -> 50 jump the gate exists to stop.
	if got := ColdCeiling(30, time.Time{}, nil, at(0), 50); got != 20 {
		t.Errorf("first cold day = %d, want the 20 start", got)
	}
	if got := ColdCeiling(30, start, nil, at(0), 50); got != 20 {
		t.Errorf("day 0 = %d, want 20", got)
	}
	if got := ColdCeiling(30, start, nil, at(2), 50); got != 30 {
		t.Errorf("day 2 = %d, want 20 + 2*5", got)
	}
	// Clamped to the mailbox's own cap, never above it.
	if got := ColdCeiling(30, start, nil, at(90), 50); got != 50 {
		t.Errorf("day 90 = %d, want the cap of 50", got)
	}
	if got := ColdCeiling(30, start, nil, at(90), 12); got != 12 {
		t.Errorf("a low mailbox cap must still bind: got %d, want 12", got)
	}
	// An unproven mailbox starts lower and takes longer to arrive.
	if got := ColdCeiling(1, start, nil, at(0), 50); got != 5 {
		t.Errorf("unproven day 0 = %d, want 5", got)
	}
}

func TestColdCeilingFreezesOnPlacement(t *testing.T) {
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	at := func(d float64) time.Time {
		return start.Add(time.Duration(d * float64(24*time.Hour)))
	}
	clean := ColdCeiling(30, start, nil, at(6), 50)
	held := ColdCeiling(30, start, []time.Time{at(4)}, at(6), 50)
	if held >= clean {
		t.Errorf("a placement did not hold the cold ramp: held %d vs clean %d", held, clean)
	}
	// Same rule as the warmup ramp: the frozen days are subtracted, not made up.
	if later := ColdCeiling(30, start, []time.Time{at(4)}, at(20), 50); later > clean+80 {
		t.Errorf("cold ramp caught up after the freeze: %d", later)
	}
	// And it can never exceed the clean ramp at the same moment.
	if got := ColdCeiling(30, start, []time.Time{at(4)}, at(20), 50); got > ColdCeiling(30, start, nil, at(20), 50) {
		t.Error("a placement raised the cold ceiling")
	}
}

// The hold the dashboard reports must count exactly the placements the ceiling
// counts, or the drawer says "paused" while the scheduler is still climbing.
func TestColdHeldUntilMatchesWhatTheCeilingCounts(t *testing.T) {
	rampStart := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	freeze := 72 * time.Hour
	now := rampStart.Add(24 * time.Hour)

	// A placement from BEFORE the mailbox ever sent cold mail is not part of
	// the cold ramp, so it must not be reported as holding it.
	before := []time.Time{rampStart.Add(-48 * time.Hour)}
	if got := ColdHeldUntil(rampStart, before, now, freeze); got != nil {
		t.Errorf("a pre-ramp placement reported as holding the cold ramp: %v", got)
	}
	if ColdCeiling(30, rampStart, before, now, 50) != ColdCeiling(30, rampStart, nil, now, 50) {
		t.Error("a pre-ramp placement changed the ceiling")
	}

	// One after it does hold, and the ceiling agrees.
	after := []time.Time{rampStart.Add(12 * time.Hour)}
	if got := ColdHeldUntil(rampStart, after, now, freeze); got == nil {
		t.Error("a placement during the ramp is not reported as holding it")
	}
	if ColdCeiling(30, rampStart, after, now, 50) >= ColdCeiling(30, rampStart, nil, now, 50) {
		t.Error("a placement during the ramp did not lower the ceiling")
	}

	// An unanchored mailbox has no ramp to hold.
	if got := ColdHeldUntil(time.Time{}, after, now, freeze); got != nil {
		t.Errorf("an unanchored mailbox reported a hold: %v", got)
	}
}
