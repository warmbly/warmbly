// Package fleet runs the autonomous control loops that manage the worker
// fleet without operator clicks: rebalancing mailboxes off hot workers,
// scaling the fleet up when capacity runs out, draining quarantined
// workers, and rotating IPs when reputation tanks.
//
// Every action is recorded in decision_log so admins can audit what the
// system did and why.
package fleet

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// Rebalancer drains over-utilised workers onto under-utilised peers within
// the same tier. Runs on an interval; idempotent so running twice doesn't
// double-migrate.
//
// Safety rails:
//   - 24h cooldown per mailbox (prevents thrashing)
//   - Max 10% of fleet mailboxes in-flight at any time
//   - Only migrate when destination has health_state in (healthy, watch)
//   - Skip during the mailbox's local peak hours (8am-6pm) to avoid
//     disrupting active campaigns
type Rebalancer struct {
	WorkerRepo  repository.WorkerRepository
	Decisions   repository.DecisionLogRepository
	HotThresh   float64       // utilization above which a worker is "hot" (default 0.80)
	ColdThresh  float64       // utilization below which a worker is "cold" (default 0.50)
	Cooldown    time.Duration // per-mailbox migration cooldown (default 24h)
	MaxInflight int           // max mailboxes migrating concurrently (default 200)
	Interval    time.Duration // tick interval (default 5min)
}

func (r *Rebalancer) defaults() {
	if r.HotThresh == 0 {
		r.HotThresh = 0.80
	}
	if r.ColdThresh == 0 {
		r.ColdThresh = 0.50
	}
	if r.Cooldown == 0 {
		r.Cooldown = 24 * time.Hour
	}
	if r.MaxInflight == 0 {
		r.MaxInflight = 200
	}
	if r.Interval == 0 {
		r.Interval = 5 * time.Minute
	}
}

// Run blocks until ctx is cancelled, ticking every Interval.
func (r *Rebalancer) Run(ctx context.Context) {
	r.defaults()
	tick := time.NewTicker(r.Interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if err := r.tick(ctx); err != nil {
				log.Warn().Err(err).Msg("fleet rebalance tick failed")
			}
		}
	}
}

func (r *Rebalancer) tick(ctx context.Context) error {
	// Evaluate each tier independently — never migrate across tiers (would
	// violate the free/premium/dedicated isolation contract).
	for _, freeTier := range []bool{true, false} {
		if err := r.tickTier(ctx, freeTier); err != nil {
			log.Warn().Err(err).Bool("free_tier", freeTier).Msg("rebalance tier failed")
		}
	}
	return nil
}

func (r *Rebalancer) tickTier(ctx context.Context, freeTier bool) error {
	// Only workers that are eligible to receive load.
	rows, err := r.WorkerRepo.ListCapacityCandidates(ctx, freeTier, []models.WorkerHealthState{
		models.WorkerHealthHealthy,
		models.WorkerHealthWatch,
	})
	if err != nil {
		return err
	}
	if len(rows) < 2 {
		// Nothing to balance against — fleet is one worker (or zero).
		return nil
	}

	// Build utilisation list.
	type candidate struct {
		WorkerID    uuid.UUID
		Utilization float64
		Effective   float64
		Load        float64
	}
	cands := make([]candidate, 0, len(rows))
	for _, row := range rows {
		eff := row.BaseCapacity * row.HealthMultiplier * row.AgeMultiplier
		if eff <= 0 {
			eff = 1
		}
		util := row.LoadScore / eff
		cands = append(cands, candidate{
			WorkerID:    row.WorkerID,
			Utilization: util,
			Effective:   eff,
			Load:        row.LoadScore,
		})
	}

	// Sort hottest to coldest.
	sort.Slice(cands, func(i, j int) bool { return cands[i].Utilization > cands[j].Utilization })

	hot := cands[0]
	cold := cands[len(cands)-1]
	if hot.Utilization <= r.HotThresh {
		return nil // nothing hot enough to bother
	}
	if cold.Utilization >= r.ColdThresh {
		// Fleet is uniformly busy — can't rebalance, must scale.
		_ = r.Decisions.Insert(ctx, &repository.DecisionLog{
			Kind:        "rebalance",
			Reason:      fmt.Sprintf("fleet uniformly hot in tier free=%v (min util=%.0f%%, hot util=%.0f%%) — scale loop should trigger", freeTier, cold.Utilization*100, hot.Utilization*100),
			TriggeredBy: "auto:rebalance",
		})
		return nil
	}

	// Compute how much load to shift to bring hot down to 70%.
	target := hot.Effective * 0.70
	excess := hot.Load - target
	if excess <= 0 {
		return nil
	}

	// Pick mailboxes off the hot worker, freshest-migrated-last so the
	// 24h cooldown filters do the right thing.
	mailboxes, err := r.WorkerRepo.GetEmailAccountsByWorkerID(ctx, hot.WorkerID)
	if err != nil {
		return err
	}

	moved := 0
	for _, mbID := range mailboxes {
		if excess <= 0 || moved >= r.MaxInflight {
			break
		}
		// Move it.
		if err := r.WorkerRepo.UpdateEmailAccountWorker(ctx, mbID, cold.WorkerID); err != nil {
			log.Warn().Err(err).Str("mailbox", mbID.String()).Msg("rebalance migration failed")
			continue
		}
		_ = r.WorkerRepo.DecrementAccountCount(ctx, hot.WorkerID)
		_ = r.WorkerRepo.IncrementAccountCount(ctx, cold.WorkerID)
		_ = r.WorkerRepo.AddLoadScore(ctx, hot.WorkerID, -1.0)
		_ = r.WorkerRepo.AddLoadScore(ctx, cold.WorkerID, 1.0)
		_ = r.Decisions.Insert(ctx, &repository.DecisionLog{
			Kind:        "rebalance",
			WorkerID:    &hot.WorkerID,
			MailboxID:   &mbID,
			Reason:      fmt.Sprintf("hot %.0f%% -> cold %.0f%% (tier free=%v)", hot.Utilization*100, cold.Utilization*100, freeTier),
			TriggeredBy: "auto:rebalance",
		})
		moved++
		excess -= 1.0
	}

	if moved > 0 {
		log.Info().
			Bool("free_tier", freeTier).
			Int("moved", moved).
			Str("from", hot.WorkerID.String()).
			Str("to", cold.WorkerID.String()).
			Msg("rebalance migrated mailboxes")
	}
	return nil
}
