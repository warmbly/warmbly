package warmupramp

import (
	"testing"
	"time"
)

func TestSoftAdjustCutsOnAnyPlacement(t *testing.T) {
	if got := SoftAdjust(0); got != 1.0 {
		t.Errorf("SoftAdjust(0) = %v, want 1.0", got)
	}
	// One placement is enough on purpose: waiting for a rate to clear a sample
	// floor is the delay this path exists to remove.
	for _, n := range []int{1, 9} {
		if got := SoftAdjust(n); got != SoftThrottleMultiplier {
			t.Errorf("SoftAdjust(%d) = %v, want %v", n, got, SoftThrottleMultiplier)
		}
	}
}

func TestDays(t *testing.T) {
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	at := func(days float64) time.Time { return start.Add(time.Duration(days * float64(24*time.Hour))) }
	ptr := func(t time.Time) *time.Time { return &t }
	freeze := 72 * time.Hour

	tests := []struct {
		name          string
		lastPlacement *time.Time
		now           time.Time
		want          int
	}{
		{"no placement ramps normally", nil, at(10), 10},
		{"placement before warmup started is not ours", ptr(start.Add(-48 * time.Hour)), at(10), 10},
		{"inside the freeze holds at the placement day", ptr(at(5)), at(6), 5},
		{"still holding on the last hour of the freeze", ptr(at(5)), at(7.9), 5},
		// The point: the three held days are LOST, not caught up. Resuming at 8
		// and jumping straight to 8 would be the overnight spike itself.
		{"resumes from the held level, never catches up", ptr(at(5)), at(9), 6},
		{"a week later it is still three days behind", ptr(at(5)), at(15), 12},
		{"a placement on day zero holds at zero", ptr(at(0)), at(1), 0},
		{"clock skew before warmup start floors at zero", nil, start.Add(-time.Hour), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Days(start, tt.lastPlacement, tt.now, freeze); got != tt.want {
				t.Errorf("Days() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestDaysNeverExceedsRealElapsed(t *testing.T) {
	// A resumed ramp must never claim more days than actually passed, or a
	// freeze would end up ACCELERATING the mailbox.
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	placement := start.Add(24 * time.Hour)
	for d := 1; d < 40; d++ {
		now := start.Add(time.Duration(d) * 24 * time.Hour)
		if got := Days(start, &placement, now, 72*time.Hour); got > d {
			t.Fatalf("day %d: Days = %d, more than the %d elapsed", d, got, d)
		}
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
		t.Errorf("in-campaign = %d, want the campaign cap of %d", got, ActiveCampaignCap)
	}
	if got := Target(false, 10, 1, 40, 500, false); got != ActiveCampaignCap {
		t.Errorf("monitor lane = %d, want the campaign cap of %d", got, ActiveCampaignCap)
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
