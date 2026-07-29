package tasks

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"
)

// warmupReconcileBatch caps how many mailboxes a single reconcile pass will
// (re)seed. Plenty for steady state; the next tick mops up any overflow.
const warmupReconcileBatch = 500

// ReconcileWarmupSchedules (re)seeds warmup chains for mailboxes that should
// be warming but have no pending warmup task — either because warmup was just
// enabled, the mailbox joined a live campaign (health-check lane), or a prior
// chain wound down. Returns the number of chains seeded this pass.
//
// This is the single bootstrap for warmup: enabling warmup or starting a
// campaign does not itself enqueue a task, so without this pass a freshly
// enabled mailbox would never start warming.
func (s *tasksService) ReconcileWarmupSchedules(ctx context.Context, limit int) (int, error) {
	// Same lost-callback backstop as the campaign reconciler: a pending
	// warmup task stranded past its slot blocks the candidate query below.
	if n, err := s.taskRepo.CancelOverduePendingTasks(ctx, "warmup", overduePendingGrace); err == nil && n > 0 {
		log.Info().Int64("cancelled", n).Msg("warmup reconcile: cancelled overdue pending tasks")
	}

	ids, err := s.emailRepo.ListWarmupScheduleCandidates(ctx, limit)
	if err != nil {
		return 0, err
	}

	seeded := 0
	for _, id := range ids {
		account, xerr := s.emailRepo.GetByID(ctx, id)
		if xerr != nil || account == nil || account.OrganizationID == nil {
			continue
		}
		if s.featureGate != nil {
			canWarmup, _ := s.featureGate.CanUseWarmup(ctx, *account.OrganizationID)
			if !canWarmup {
				if s.warmupHealth != nil {
					_ = s.warmupHealth.RemovePoolMembership(ctx, account.ID, s.resolveWarmupPoolType(ctx, account))
				}
				continue
			}
		}

		// EnsureWarmupScheduled is idempotent and returns ErrWarmupNotEnabled
		// for mailboxes that raced out of an eligible state — both benign, so
		// we skip rather than abort the whole pass.
		if err := s.EnsureWarmupScheduled(ctx, id); err != nil {
			continue
		}
		seeded++
	}
	return seeded, nil
}

// StartWarmupReconciler runs ReconcileWarmupSchedules on an interval until the
// context is cancelled. Mirrors the other background sweeps (warmup health,
// dead-worker) and is started from the backend, which owns Cloud Tasks.
func (s *tasksService) StartWarmupReconciler(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Seed once on boot so chains recover promptly after a restart instead of
	// waiting a full interval.
	s.reconcileOnce(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.reconcileOnce(ctx)
		}
	}
}

func (s *tasksService) reconcileOnce(ctx context.Context) {
	rctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	seeded, err := s.ReconcileWarmupSchedules(rctx, warmupReconcileBatch)
	if err != nil {
		log.Warn().Err(err).Msg("warmup reconcile pass failed")
		return
	}
	if seeded > 0 {
		log.Info().Int("seeded", seeded).Msg("warmup reconcile seeded chains")
	}
}
