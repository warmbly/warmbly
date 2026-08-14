package dispatch

import "time"

// AdaptiveHealth is a batch health snapshot used to step the rate up or down.
// Deterministic and observable: no ML, no random jitter.
type AdaptiveHealth struct {
	// Commits in the evaluation batch (successful sends).
	Commits int
	// HardBounceRate in [0,1] over the same window.
	HardBounceRate float64
	// AuthFailure / reputation / throttle signals.
	AuthFailure            bool
	ReputationBlock        bool
	ProviderThrottle       bool
	KillSwitchActive       bool
	SystemicRecipientIssue bool
}

// AdaptiveDecision is the next rate after evaluating health.
type AdaptiveDecision struct {
	NextSendsPerHour int
	NextMinGap       time.Duration
	Action           string // "hold" | "step_up" | "step_down" | "pause"
	Reason           string
}

// Step ladder for this campaign: 10 → 15 → 20.
var adaptiveLadder = []int{10, 15, 20}

// EvaluateAdaptiveRate decides the next hourly cap from current rate + health.
// Never exceeds maxRate. Never steps up without a healthy batch of at least batchSize commits.
func EvaluateAdaptiveRate(current, start, maxRate, batchSize int, h AdaptiveHealth) AdaptiveDecision {
	if start < 1 {
		start = DefaultSendsPerHour
	}
	if maxRate < start {
		maxRate = 20
	}
	if current < 1 {
		current = start
	}
	if batchSize < 1 {
		batchSize = 20
	}
	// Pause / hard retreat conditions.
	if h.KillSwitchActive {
		return AdaptiveDecision{NextSendsPerHour: current, NextMinGap: MinGapForRate(current), Action: "pause", Reason: "kill_switch"}
	}
	if h.AuthFailure {
		down := stepDown(current, start)
		return AdaptiveDecision{NextSendsPerHour: down, NextMinGap: MinGapForRate(down), Action: "step_down", Reason: "auth_failure"}
	}
	if h.ReputationBlock {
		down := stepDown(current, start)
		return AdaptiveDecision{NextSendsPerHour: down, NextMinGap: MinGapForRate(down), Action: "step_down", Reason: "reputation_block"}
	}
	if h.ProviderThrottle {
		down := stepDown(current, start)
		return AdaptiveDecision{NextSendsPerHour: down, NextMinGap: MinGapForRate(down), Action: "step_down", Reason: "provider_throttle"}
	}
	if h.SystemicRecipientIssue {
		down := stepDown(current, start)
		return AdaptiveDecision{NextSendsPerHour: down, NextMinGap: MinGapForRate(down), Action: "step_down", Reason: "systemic_recipient_issue"}
	}
	if h.HardBounceRate > 0.02 {
		down := stepDown(current, start)
		return AdaptiveDecision{NextSendsPerHour: down, NextMinGap: MinGapForRate(down), Action: "step_down", Reason: "hard_bounce_gt_2pct"}
	}
	// Need a full healthy batch before climbing.
	if h.Commits < batchSize {
		return AdaptiveDecision{NextSendsPerHour: current, NextMinGap: MinGapForRate(current), Action: "hold", Reason: "batch_incomplete"}
	}
	if current >= maxRate {
		return AdaptiveDecision{NextSendsPerHour: maxRate, NextMinGap: MinGapForRate(maxRate), Action: "hold", Reason: "at_max"}
	}
	up := stepUp(current, maxRate)
	if up == current {
		return AdaptiveDecision{NextSendsPerHour: current, NextMinGap: MinGapForRate(current), Action: "hold", Reason: "at_max"}
	}
	return AdaptiveDecision{NextSendsPerHour: up, NextMinGap: MinGapForRate(up), Action: "step_up", Reason: "healthy_batch"}
}

func stepUp(current, maxRate int) int {
	for i, v := range adaptiveLadder {
		if current < v {
			if v > maxRate {
				return maxRate
			}
			return v
		}
		if current == v && i+1 < len(adaptiveLadder) {
			n := adaptiveLadder[i+1]
			if n > maxRate {
				return maxRate
			}
			return n
		}
	}
	if current < maxRate {
		return maxRate
	}
	return current
}

func stepDown(current, start int) int {
	// Drop one ladder step, never below start/10 floor of 5 for safety pause path.
	for i := len(adaptiveLadder) - 1; i >= 0; i-- {
		if adaptiveLadder[i] < current {
			if adaptiveLadder[i] < start {
				return start
			}
			return adaptiveLadder[i]
		}
	}
	if start > 0 {
		return start
	}
	return DefaultSendsPerHour
}

// ApplyAdaptive updates governor config in place when decision changes rate.
func (g *Governor) ApplyAdaptive(d AdaptiveDecision) {
	if g == nil {
		return
	}
	if d.NextSendsPerHour > 0 {
		g.cfg.SendsPerHour = d.NextSendsPerHour
	}
	if d.NextMinGap > 0 {
		g.cfg.MinGap = d.NextMinGap
	}
}
