package warmupramp

import (
	"testing"
	"time"
)

func TestSoftAdjustCutsOnAnyPlacement(t *testing.T) {
	if got := SoftAdjust(0); got != 1.0 {
		t.Errorf("SoftAdjust(0) = %v, want 1.0", got)
	}
	// One placement is enough: waiting for a rate to clear a sample floor is
	// the delay this path exists to remove.
	for _, n := range []int{1, 9} {
		if got := SoftAdjust(n); got != SoftThrottleMultiplier {
			t.Errorf("SoftAdjust(%d) = %v, want %v", n, got, SoftThrottleMultiplier)
		}
	}
}

func TestDays(t *testing.T) {
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	at := func(days float64) time.Time { return start.Add(time.Duration(days * float64(24*time.Hour))) }
	freeze := 72 * time.Hour

	tests := []struct {
		name       string
		placements []time.Time
		now        time.Time
		want       int
	}{
		{"no placement ramps normally", nil, at(10), 10},
		{"placement from a previous warmup run is ignored", []time.Time{start.Add(-48 * time.Hour)}, at(10), 10},
		{"inside the freeze the ramp does not move", []time.Time{at(5)}, at(6), 5},
		{"still held on the last hour of the freeze", []time.Time{at(5)}, at(7.9), 5},
		// Frozen days are subtracted, not made up: 9 elapsed minus 3 frozen.
		{"resumes from the held level", []time.Time{at(5)}, at(9), 6},
		{"a week later it is still three days behind", []time.Time{at(5)}, at(15), 12},
		{"a placement on day zero holds at zero", []time.Time{at(0)}, at(1), 0},
		{"clock skew before warmup start floors at zero", nil, start.Add(-time.Hour), 0},

		// Why this is subtracted time rather than a held level: holding "at the
		// newest placement's level" would RAISE the held level every time a new
		// placement arrived mid-hold, so a mailbox landing in junk repeatedly
		// would ramp up.
		{"a second placement mid-hold does not advance the ramp", []time.Time{at(5), at(6)}, at(7), 5},
		{"and it extends the hold rather than ending it", []time.Time{at(5), at(6)}, at(8.5), 5},
		// Union, not sum: two placements an hour apart freeze ~3 days, not 6.
		// A sum would leave 14 here.
		{"overlapping freezes count once", []time.Time{at(5), at(5.04)}, at(20), 16},
		{"separated freezes each cost a window", []time.Time{at(2), at(10)}, at(20), 14},
		{"unsorted input is handled", []time.Time{at(10), at(2)}, at(20), 14},
		{"duplicate timestamps count once", []time.Time{at(5), at(5), at(5)}, at(20), 17},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Days(start, tt.placements, tt.now, freeze); got != tt.want {
				t.Errorf("Days() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestDaysIsMonotonicInPlacements(t *testing.T) {
	// Adding a placement must never RAISE the ramp. The previous "hold at the
	// newest placement" model violated exactly this.
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	now := start.Add(30 * 24 * time.Hour)
	var placements []time.Time
	prev := Days(start, placements, now, 72*time.Hour)
	for d := 1; d <= 20; d++ {
		placements = append(placements, start.Add(time.Duration(d)*24*time.Hour))
		got := Days(start, placements, now, 72*time.Hour)
		if got > prev {
			t.Fatalf("adding placement %d raised the ramp from %d to %d", d, prev, got)
		}
		prev = got
	}
}

func TestDaysNeverExceedsRealElapsed(t *testing.T) {
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	placements := []time.Time{start.Add(24 * time.Hour)}
	for d := 1; d < 40; d++ {
		now := start.Add(time.Duration(d) * 24 * time.Hour)
		if got := Days(start, placements, now, 72*time.Hour); got > d {
			t.Fatalf("day %d: Days = %d, more than the %d elapsed", d, got, d)
		}
	}
}

func TestFrozenUntilCoversTheWholeFreeze(t *testing.T) {
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	freeze := 72 * time.Hour
	p := start.Add(24 * time.Hour)

	if got := FrozenUntil(nil, start, freeze); got != nil {
		t.Errorf("no placements must not report a freeze, got %v", got)
	}
	// Still frozen at 60 hours, PAST the 48-hour volume-cut window: the ramp is
	// held while volume is already back to normal, and that must be reportable.
	if got := FrozenUntil([]time.Time{p}, p.Add(60*time.Hour), freeze); got == nil {
		t.Error("ramp is still frozen at 60h but nothing reports it")
	}
	if got := FrozenUntil([]time.Time{p}, p.Add(freeze), freeze); got != nil {
		t.Errorf("freeze expired but still reported: %v", got)
	}
	got := FrozenUntil([]time.Time{p, p.Add(24 * time.Hour)}, p.Add(30*time.Hour), freeze)
	if got == nil || !got.Equal(p.Add(24*time.Hour).Add(freeze)) {
		t.Errorf("FrozenUntil = %v, want the newest placement plus the window", got)
	}
}

func TestTargetClampsToBaseCeilingAndCampaignCap(t *testing.T) {
	if got := Target(true, 10, 1, 40, 0, false); got != 10 {
		t.Errorf("day 0 = %d, want the base of 10", got)
	}
	if got := Target(true, 10, 1, 40, 5, false); got != 15 {
		t.Errorf("day 5 = %d, want 15", got)
	}
	if got := Target(true, 10, 1, 40, 500, false); got != 40 {
		t.Errorf("day 500 = %d, want the ceiling of 40", got)
	}
	if got := Target(true, 10, 1, 40, 500, true); got != ActiveCampaignCap {
		t.Errorf("in-campaign = %d, want %d", got, ActiveCampaignCap)
	}
	if got := Target(false, 10, 1, 40, 500, false); got != ActiveCampaignCap {
		t.Errorf("monitor lane = %d, want %d", got, ActiveCampaignCap)
	}
}

func TestApplyKeepsAHeartbeat(t *testing.T) {
	if got := Apply(20, 0.75); got != 15 {
		t.Errorf("Apply(20, 0.75) = %d, want 15", got)
	}
	if got := Apply(20, 1.0); got != 20 {
		t.Errorf("Apply with no signal must not move the target, got %d", got)
	}
	// Even a fully degraded mailbox keeps one send so the health sweep has
	// fresh data to judge it on.
	if got := Apply(1, 0.75); got != 1 {
		t.Errorf("Apply(1, 0.75) = %d, want 1", got)
	}
	if got := Apply(0, 0.75); got != 0 {
		t.Errorf("Apply(0, ...) = %d, want 0", got)
	}
}
