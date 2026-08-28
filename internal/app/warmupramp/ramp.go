// Package warmupramp holds the warmup volume policy: how many emails a mailbox
// should aim to send today, and how adverse signal holds that number down. Pure
// and outside the scheduler so the dashboard reports the same target the
// scheduler acts on.
package warmupramp

import "time"

const (
	// ActiveCampaignCap is the warmup volume for a mailbox also backing a live
	// campaign, so warmup does not stack on production sending pressure.
	ActiveCampaignCap = 5

	// SoftThrottleMultiplier is the cut applied while a placement is fresh.
	// The bands cannot do this: each needs a sample floor a new mailbox has
	// not reached (20 warmup sends in 7 days, 100 delivered in 30).
	SoftThrottleMultiplier = 0.75

	// SoftSignalWindow is how recent a placement must be to still cut volume.
	SoftSignalWindow = 48 * time.Hour

	// FreezeWindow is how long one placement holds the ramp.
	FreezeWindow = 72 * time.Hour

	// LookbackWindow bounds how far back placements are read.
	LookbackWindow = 60 * 24 * time.Hour
)

// SoftAdjust returns the volume multiplier for a fresh placement. Any placement
// cuts; a rate threshold would reintroduce the sample floor this avoids.
func SoftAdjust(placements int) float64 {
	if placements > 0 {
		return SoftThrottleMultiplier
	}
	return 1.0
}

// Days is the ramp day-count: elapsed days minus days spent frozen. Subtracted
// time rather than a held level, because freezing "at the last placement's
// level" would raise it whenever a new placement arrived mid-hold. Input need
// not be sorted or deduplicated.
func Days(warmupStart time.Time, placements []time.Time, now time.Time, freeze time.Duration) int {
	elapsed := now.Sub(warmupStart)
	if elapsed <= 0 {
		return 0
	}
	return wholeDays(elapsed - frozenDuration(warmupStart, placements, now, freeze))
}

// FrozenUntil is when the ramp climbs again, or nil when it already is. Covers
// the whole freeze, not just the shorter window that also cuts volume.
func FrozenUntil(placements []time.Time, now time.Time, freeze time.Duration) *time.Time {
	var latest time.Time
	for _, p := range placements {
		if p.After(latest) {
			latest = p
		}
	}
	if latest.IsZero() {
		return nil
	}
	until := latest.Add(freeze)
	if !until.After(now) {
		return nil
	}
	return &until
}

// frozenDuration is the union of each placement's freeze window, clipped to
// now. Union, not sum: two placements an hour apart freeze about one window.
func frozenDuration(warmupStart time.Time, placements []time.Time, now time.Time, freeze time.Duration) time.Duration {
	type span struct{ start, end time.Time }
	spans := make([]span, 0, len(placements))
	for _, p := range placements {
		if p.Before(warmupStart) {
			// From a previous warmup run: restarting warmup resets the ramp,
			// so a stale freeze must not carry into the new one.
			continue
		}
		start, end := p, p.Add(freeze)
		if end.After(now) {
			end = now
		}
		if end.After(start) {
			spans = append(spans, span{start, end})
		}
	}
	if len(spans) == 0 {
		return 0
	}
	// Insertion sort by start: placements are rare, so this stays tiny and
	// avoids pulling sort into a hot path.
	for i := 1; i < len(spans); i++ {
		for j := i; j > 0 && spans[j].start.Before(spans[j-1].start); j-- {
			spans[j], spans[j-1] = spans[j-1], spans[j]
		}
	}
	var total time.Duration
	cur := spans[0]
	for _, s := range spans[1:] {
		if s.start.After(cur.end) {
			total += cur.end.Sub(cur.start)
			cur = s
			continue
		}
		if s.end.After(cur.end) {
			cur.end = s.end
		}
	}
	return total + cur.end.Sub(cur.start)
}

// Target is the day's warmup volume before recipient-capacity, health-band and
// early-signal adjustments.
func Target(activelyWarming bool, base, increase, max, days int, inCampaign bool) int {
	if !activelyWarming {
		return ActiveCampaignCap
	}
	target := base + days*increase
	if target > max {
		target = max
	}
	if inCampaign && target > ActiveCampaignCap {
		target = ActiveCampaignCap
	}
	return target
}

// Apply folds a multiplier into a target, never below one: a degraded mailbox
// keeps a heartbeat so the health sweep has fresh data to judge it on.
func Apply(target int, multiplier float64) int {
	if multiplier >= 1.0 || target <= 0 {
		return target
	}
	cut := int(float64(target)*multiplier + 0.5)
	if cut < 1 {
		cut = 1
	}
	if cut > target {
		return target
	}
	return cut
}

func wholeDays(d time.Duration) int {
	days := int(d.Hours() / 24)
	if days < 0 {
		return 0
	}
	return days
}
