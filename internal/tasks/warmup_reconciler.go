package tasks

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/tasks/proto"
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
	if lineage, ok := s.taskRepo.(repository.WarmupLineageRepository); ok {
		pending, err := lineage.UnqueuedWarmupTasks(ctx, limit)
		if err != nil {
			return 0, err
		}
		for _, v := range pending {
			handle, err := s.tasksClient.CreateTask(ctx, &proto.ProcessTask{TaskId: v.TaskID.String()}, v.At)
			if err != nil {
				return 0, err
			}
			acked, err := lineage.AckWarmupQueue(ctx, v, handle)
			if err != nil {
				return 0, err
			}
			if !acked {
				_ = s.tasksClient.DeleteTask(ctx, handle)
			} else if v.PreviousHandle != nil && *v.PreviousHandle != handle {
				_ = s.tasksClient.DeleteTask(ctx, *v.PreviousHandle)
			}
		}
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
		if s.cloudLink != nil && s.cloudLink.IsEnrolled(ctx, account.ID) {
			continue
		}
		if s.featureGate != nil {
			canWarmup, xerr := s.featureGate.CanUseWarmup(ctx, *account.OrganizationID)
			if !canWarmup {
				// Only a definite "not entitled" evicts.
				if s.warmupHealth != nil && xerr == nil {
					_ = s.warmupHealth.RemoveFromAllPools(ctx, account.ID)
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
	} else if seeded > 0 {
		log.Info().Int("seeded", seeded).Msg("warmup reconcile seeded chains")
	}

	moved, removed, err := s.ReconcileWarmupPoolMembership(rctx)
	if err != nil {
		log.Warn().Err(err).Msg("warmup pool membership reconcile pass failed")
		return
	}
	if moved > 0 || removed > 0 {
		log.Info().Int("moved", moved).Int("removed", removed).Msg("warmup pool membership reconciled")
	}
}

// ReconcileWarmupPoolMembership makes every participant's membership agree with its
// entitlement: no longer entitled leaves warmup, changed tier moves pool. The warmup task does
// this too, but only for mailboxes it still runs for, and one that stopped warming has no chain
// and is not a schedule candidate, so nothing else would ever revisit it (issue #211).
// Roles are left alone so a domain-auth demotion is not quietly promoted back.
func (s *tasksService) ReconcileWarmupPoolMembership(ctx context.Context) (moved int, removed int, err error) {
	if s.warmupRepo == nil || s.warmupHealth == nil {
		return 0, 0, nil
	}

	ids, err := s.warmupRepo.GetAllParticipantAccountIDs(ctx)
	if err != nil {
		return 0, 0, err
	}

	// One subscription lookup per organization, not per mailbox.
	entitled := map[uuid.UUID]bool{}

	for _, id := range ids {
		account, xerr := s.emailRepo.GetByID(ctx, id)
		if xerr != nil {
			continue
		}
		if account == nil || account.Status != "active" || account.OrganizationID == nil {
			// Nothing left to check an entitlement against, so it does not belong in a shared pool.
			if xerr := s.warmupHealth.RemoveFromAllPools(ctx, id); xerr == nil {
				removed++
			}
			continue
		}

		if s.featureGate != nil {
			ok, cached := entitled[*account.OrganizationID]
			if !cached {
				canWarmup, xerr := s.featureGate.CanUseWarmup(ctx, *account.OrganizationID)
				if xerr != nil {
					// Entitlement unknown: leave this mailbox exactly as it is.
					continue
				}
				ok = canWarmup
				entitled[*account.OrganizationID] = ok
			}
			if !ok {
				if xerr := s.warmupHealth.RemoveFromAllPools(ctx, account.ID); xerr == nil {
					removed++
					log.Info().
						Str("email_account_id", account.ID.String()).
						Msg("warmup reconcile: mailbox left warmup, organization is no longer entitled")
				}
				continue
			}
		}

		poolType := s.resolveWarmupPoolType(ctx, account)
		didMove, xerr := s.warmupHealth.MovePoolMembership(ctx, account.ID, poolType)
		if xerr != nil {
			continue
		}
		if didMove {
			moved++
			log.Info().
				Str("email_account_id", account.ID.String()).
				Str("pool_type", poolType).
				Msg("warmup reconcile: mailbox moved to the pool its tier belongs to")
		}
	}

	return moved, removed, nil
}
