package tasks

import (
	"context"
	"time"

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
	if recovery, ok := s.taskRepo.(interface {
		RecoverAbandonedWarmupPreparations(context.Context, time.Time, int) (int, error)
	}); ok {
		n, err := recovery.RecoverAbandonedWarmupPreparations(ctx, time.Now().Add(-10*time.Minute), limit)
		if err != nil {
			return 0, err
		}
		if n > 0 {
			log.Info().Int("recovered", n).Msg("warmup reconcile retired abandoned preparations")
		}
	}
	var recoverBatch func(context.Context, time.Time, int) (int, int, error)
	if recovery, ok := s.taskRepo.(interface {
		RecoverUnstartedWarmupDispatchBatch(context.Context, time.Time, int) (int, int, error)
	}); ok {
		recoverBatch = recovery.RecoverUnstartedWarmupDispatchBatch
	} else if recovery, ok := s.taskRepo.(interface {
		RecoverUnstartedWarmupDispatches(context.Context, time.Time, int) (int, error)
	}); ok {
		recoverBatch = func(ctx context.Context, before time.Time, limit int) (int, int, error) {
			n, err := recovery.RecoverUnstartedWarmupDispatches(ctx, before, limit)
			return n, n, err
		}
	}
	if recoverBatch != nil {
		recovered := 0
		for batch := 0; batch < 10 && ctx.Err() == nil; batch++ {
			n, selected, err := recoverBatch(ctx, time.Now().Add(-10*time.Minute), limit)
			recovered += n
			if err != nil {
				if recovered > 0 {
					log.Info().Int("recovered", recovered).Msg("warmup reconcile retired unstarted dispatches before failing")
				}
				return 0, err
			}
			if selected < limit || limit <= 0 {
				break
			}
		}
		if recovered > 0 {
			log.Info().Int("recovered", recovered).Msg("warmup reconcile retired unstarted dispatches")
		}
	}
	if recovery, ok := s.taskRepo.(interface {
		RetireUnknownWarmupSends(context.Context, time.Time, int) (int, error)
	}); ok {
		n, err := recovery.RetireUnknownWarmupSends(ctx, time.Now().Add(-10*time.Minute), limit)
		if err != nil {
			return 0, err
		}
		if n > 0 {
			log.Info().Int("retired", n).Msg("warmup reconcile permanently retired unknown sends without replay or refund")
		}
	}
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
