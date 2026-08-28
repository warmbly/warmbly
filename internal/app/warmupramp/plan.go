package warmupramp

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// Reader is the repository surface a plan needs. Declared here so the policy
// does not depend on the repository package.
type Reader interface {
	SpamPlacementsSince(ctx context.Context, accountID uuid.UUID, since time.Time) ([]time.Time, error)
	SumWarmupSentSince(ctx context.Context, accountID uuid.UUID, since time.Time) (int, error)
}

// Input is everything about a mailbox the plan reads off the row.
type Input struct {
	AccountID       uuid.UUID
	WarmupStart     time.Time
	ActivelyWarming bool
	Base            int
	Increase        int
	Max             int
	InCampaign      bool
	Health          models.WarmupHealthState
	Now             time.Time
}

// Plan is the resolved warmup volume for a mailbox, and why.
type Plan struct {
	// Target is the day's volume after ramp, cut and band. Callers may clamp
	// it further; they must never raise it.
	Target int
	// Placements and Sends cover SoftSignalWindow.
	Placements int
	Sends      int
	// FrozenUntil outlives the volume cut (FreezeWindow > SoftSignalWindow), so
	// a mailbox can be back at full volume while still not climbing.
	FrozenUntil *time.Time
}

// Cut reports whether the early signal reduced today's volume.
func (p Plan) Cut() bool { return p.Placements > 0 }

// HealthVolumeMultiplier lives beside the ramp so the scheduler and the
// dashboard cannot apply different bands.
func HealthVolumeMultiplier(state models.WarmupHealthState) float64 {
	switch state {
	case models.WarmupHealthThrottled:
		return 0.5
	case models.WarmupHealthWatch:
		return 0.7
	default:
		return 1.0
	}
}

// Resolve computes the plan. Reads fail open to "no signal". The early cut
// applies only while Healthy: a banded mailbox is already dampened harder, and
// stacking both would cut it twice for one problem.
func Resolve(ctx context.Context, r Reader, in Input) Plan {
	var plan Plan
	if !in.ActivelyWarming {
		plan.Target = Target(false, in.Base, in.Increase, in.Max, 0, in.InCampaign)
		return plan
	}

	var placements []time.Time
	if r != nil {
		if got, err := r.SpamPlacementsSince(ctx, in.AccountID, in.Now.Add(-LookbackWindow)); err == nil {
			placements = got
		}
	}

	since := in.Now.Add(-SoftSignalWindow)
	for _, p := range placements {
		if !p.Before(since) {
			plan.Placements++
		}
	}
	if r != nil && plan.Placements > 0 {
		if sends, err := r.SumWarmupSentSince(ctx, in.AccountID, since); err == nil {
			plan.Sends = sends
		}
	}

	days := Days(in.WarmupStart, placements, in.Now, FreezeWindow)
	plan.FrozenUntil = FrozenUntil(placements, in.Now, FreezeWindow)

	target := Target(true, in.Base, in.Increase, in.Max, days, in.InCampaign)
	if in.Health == models.WarmupHealthHealthy || in.Health == "" {
		target = Apply(target, SoftAdjust(plan.Placements))
	}
	plan.Target = Apply(target, HealthVolumeMultiplier(in.Health))
	return plan
}
