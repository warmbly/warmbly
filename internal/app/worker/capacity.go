// Capacity is the math layer that turns a row from worker_capacity_view
// into a placement decision. Kept as plain functions so tests can drive
// every edge case without touching the database.
//
// Why compute Effective in Go instead of pushing it into the view? Two
// reasons:
//
//   1. Tests can override Base/HealthMul/AgeMul independently and verify
//      the floor/ceiling behavior without spinning up Postgres.
//   2. The view exposes the raw inputs (sends_attempted_1h, bounces, etc.)
//      so an operator or future feature can compute a different
//      effective-capacity formula without a migration.

package worker

import (
	"math"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

// WorkerCapacityRow is the in-Go representation of one row of
// worker_capacity_view. Loaded by the repository; fed into ComputeCapacity.
type WorkerCapacityRow struct {
	WorkerID         uuid.UUID
	WorkerType       models.WorkerType
	FreeTier         bool
	EgressKind       models.WorkerEgressKind
	HealthState      models.WorkerHealthState
	LoadScore        float64
	BaseCapacity     float64
	HealthMultiplier float64
	AgeMultiplier    float64
	SendsAttempted1h int64
	SendsSucceeded1h int64
	BouncesHard1h    int64
	BouncesSoft1h    int64
	Complaints1h     int64
	AuthErrors1h     int64
}

// Capacity is the derived placement view of a worker. All fields are
// dimensionless except Effective (mailbox-equivalents) and Load (sum of
// mailbox weights). Utilization is Load/Effective and is the value the
// scheduler sorts on when picking the next worker.
type Capacity struct {
	Base        float64
	HealthMul   float64
	AgeMul      float64
	Effective   float64
	Load        float64
	Utilization float64
}

// ComputeCapacity is the placement math: Base * Health * Age, floored at
// 1, then load is divided through to give a utilization ratio. The floor
// matters because a brand-new worker with zero history would otherwise
// have Effective=0 and never get probed.
func ComputeCapacity(row WorkerCapacityRow) Capacity {
	c := Capacity{
		Base:      row.BaseCapacity,
		HealthMul: clampUnit(row.HealthMultiplier),
		AgeMul:    clampUnit(row.AgeMultiplier),
		Load:      row.LoadScore,
	}
	c.Effective = math.Floor(c.Base * c.HealthMul * c.AgeMul)
	if c.Effective <= 0 {
		c.Effective = 1
	}
	if c.Effective > 0 {
		c.Utilization = c.Load / c.Effective
	}
	return c
}

// clampUnit pins x into [0, 1]. Negatives are surprisingly easy to feed
// in - PostgreSQL's NULLIF/divide-by-zero handling can leak NaN through
// in pathological cases - so we coerce defensively here.
func clampUnit(x float64) float64 {
	if math.IsNaN(x) || x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

// MailboxWeight is the per-mailbox load contribution. The numbers are
// deliberately on different scales:
//
//   - Cold SMTP mailboxes are the bottleneck (one inbox, one IP-facing
//     conversation, a hard ~50/day ceiling per CLAUDE.md sending policy).
//     Weight = 1.0 so a cold_smtp worker with Base=16 caps at ~16 cold
//     mailboxes.
//
//   - OAuth API mailboxes (Gmail API, Microsoft Graph) push through the
//     provider's own infrastructure and don't bottleneck on a single
//     IMAP/SMTP conversation. Weight = 0.05 so an oauth_api worker with
//     Base=400 can carry hundreds of API mailboxes.
//
//   - Warmup-only assignments are the cheapest because warmup volume is
//     small and bursty by design. Weight = 0.4 regardless of provider so
//     warmup-only workers don't get crowded out by their own cold-style
//     mailbox accounting.
func MailboxWeight(provider string, isWarmup bool) float64 {
	if isWarmup {
		return 0.4
	}
	switch provider {
	case "gmail-api", "graph-api":
		return 0.05
	default:
		return 1.0
	}
}
