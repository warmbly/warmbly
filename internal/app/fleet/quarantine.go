package fleet

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// QuarantineEvaluator inspects worker health each tick and moves workers
// between the bands defined in CLAUDE.md (healthy / watch / throttled /
// quarantined / blocked). Quarantined and blocked workers are drained by
// the Rebalancer naturally because they're excluded from
// ListCapacityCandidates.
//
// Thresholds match the docs:
//
//	watch       complaint_1h_rate >= 0.03% OR bounce_1h_rate >= 2%
//	throttled   complaint >= 0.10% OR bounce >= 5%
//	quarantined complaint >= 0.30% OR bounce >= 10%
//	blocked     complaint >= 1.00% OR bounce >= 20%
//
// We need a minimum sample size before applying these so a freshly-booted
// worker that sends 1 email and bounces isn't immediately quarantined.
type QuarantineEvaluator struct {
	WorkerRepo repository.WorkerRepository
	Decisions  repository.DecisionLogRepository
	Interval   time.Duration // default 5min
	MinSends   int           // minimum 1h send count before bands apply (default 50)
}

func (q *QuarantineEvaluator) defaults() {
	if q.Interval == 0 {
		q.Interval = 5 * time.Minute
	}
	if q.MinSends == 0 {
		q.MinSends = 50
	}
}

func (q *QuarantineEvaluator) Run(ctx context.Context) {
	q.defaults()
	tick := time.NewTicker(q.Interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if err := q.tick(ctx); err != nil {
				log.Warn().Err(err).Msg("quarantine tick failed")
			}
		}
	}
}

func (q *QuarantineEvaluator) tick(ctx context.Context) error {
	// Evaluate all workers including the ones already throttled (they
	// might recover) and quarantined (so blocked promotion still works).
	allStates := []models.WorkerHealthState{
		models.WorkerHealthHealthy,
		models.WorkerHealthWatch,
		models.WorkerHealthThrottled,
		models.WorkerHealthQuarantined,
	}

	for _, freeTier := range []bool{true, false} {
		rows, err := q.WorkerRepo.ListCapacityCandidates(ctx, freeTier, allStates)
		if err != nil {
			return err
		}
		for _, row := range rows {
			newState := q.classify(row)
			if newState == row.HealthState {
				continue
			}
			if err := q.WorkerRepo.SetWorkerHealthState(ctx, row.WorkerID, newState); err != nil {
				log.Warn().Err(err).Str("worker", row.WorkerID.String()).Msg("set worker health state failed")
				continue
			}
			wid := row.WorkerID
			_ = q.Decisions.Insert(ctx, &repository.DecisionLog{
				Kind:        "quarantine",
				WorkerID:    &wid,
				Reason:      fmt.Sprintf("%s -> %s (bounces=%d complaints=%d sends=%d)", row.HealthState, newState, row.BouncesHard1h, row.Complaints1h, row.SendsAttempted1h),
				TriggeredBy: "auto:quarantine",
			})
			log.Info().
				Str("worker", row.WorkerID.String()).
				Str("from", string(row.HealthState)).
				Str("to", string(newState)).
				Msg("worker health state transition")
		}
	}
	return nil
}

// classify maps observed 1h rates to a health band. Conservative: if the
// sample is too small, returns the current state unchanged so noisy fresh
// workers don't get quarantined for one bounce.
func (q *QuarantineEvaluator) classify(row repository.WorkerCapacityRowDB) models.WorkerHealthState {
	if int(row.SendsAttempted1h) < q.MinSends {
		return row.HealthState
	}
	sends := float64(row.SendsAttempted1h)
	bounceRate := float64(row.BouncesHard1h) / sends
	complaintRate := float64(row.Complaints1h) / sends

	switch {
	case complaintRate >= 0.01 || bounceRate >= 0.20:
		return models.WorkerHealthBlocked
	case complaintRate >= 0.003 || bounceRate >= 0.10:
		return models.WorkerHealthQuarantined
	case complaintRate >= 0.001 || bounceRate >= 0.05:
		return models.WorkerHealthThrottled
	case complaintRate >= 0.0003 || bounceRate >= 0.02:
		return models.WorkerHealthWatch
	default:
		return models.WorkerHealthHealthy
	}
}
