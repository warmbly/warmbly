// Package warmupramp holds the warmup volume policy: how many emails a mailbox
// should aim to send today, and how an early adverse signal holds that number
// down. It is pure (no context, no database) and lives outside the scheduler so
// the dashboard reports the SAME target the scheduler will act on. Two copies
// of this arithmetic is how a mailbox ends up showing "target 25" while it
// sends 18.
package warmupramp

import "time"

const (
	// ActiveCampaignCap is the warmup volume for a mailbox that also backs a
	// live campaign, so warmup does not stack on production sending pressure.
	ActiveCampaignCap = 5

	// SoftThrottleMultiplier is the ~25% cut applied on the first adverse
	// signal, while the mailbox is still Healthy.
	//
	// Every band in the health model needs a sample floor (20 warmup sends in
	// 7 days, 100 delivered in 30) before it can trip, which a mailbox in its
	// first week cannot reach — so a new mailbox landing in junk would keep
	// ramping +1/day until it had sent enough to be judged. Providers advise
	// the opposite: cut about a quarter and hold at the FIRST sign.
	SoftThrottleMultiplier = 0.75

	// SoftSignalWindow is how recent a placement must be to still count as
	// degradation rather than history the mailbox has recovered from.
	SoftSignalWindow = 48 * time.Hour

	// FreezeWindow is how long the ramp holds after a placement.
	FreezeWindow = 72 * time.Hour
)

// SoftAdjust returns the volume multiplier for an early adverse signal. Any
// placement in the window cuts volume; a rate threshold is deliberately absent,
// because waiting for a rate to clear a sample floor is the delay this exists
// to remove.
func SoftAdjust(placements int) float64 {
	if placements > 0 {
		return SoftThrottleMultiplier
	}
	return 1.0
}

// Days is the ramp day-count to use, holding the ramp where it stood when a
// placement landed and resuming from there once the freeze expires. A freeze
// SHIFTS the ramp back rather than letting it catch up: holding for three days
// and then climbing three steps in one morning is the overnight spike the hold
// exists to prevent.
func Days(warmupStart time.Time, lastPlacement *time.Time, now time.Time, freeze time.Duration) int {
	days := wholeDays(now.Sub(warmupStart))
	if lastPlacement == nil || lastPlacement.Before(warmupStart) {
		return days
	}
	held := wholeDays(lastPlacement.Sub(warmupStart))
	thawAt := lastPlacement.Add(freeze)
	if now.Before(thawAt) {
		return held
	}
	// Never claim more days than actually elapsed, or a freeze would end up
	// accelerating the mailbox instead of slowing it.
	if resumed := held + wholeDays(now.Sub(thawAt)); resumed < days {
		return resumed
	}
	return days
}

// Target is the day's warmup volume before recipient-capacity, health-band and
// early-signal adjustments. An actively-warming mailbox follows its ramp,
// reduced to the in-campaign cap when it also backs a live campaign. A mailbox
// kept warm only because it backs a campaign runs at the in-campaign cap.
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

// Apply folds the early-signal cut into a target, never below one email: a
// degraded mailbox keeps a heartbeat so the health sweep has fresh data.
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
