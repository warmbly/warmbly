package tasks

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/jobrun"
	"github.com/warmbly/warmbly/internal/repository"
)

// warmupPoolReconcileInterval spaces the membership pass; the warmup task corrects the mailboxes it runs for in between.
const warmupPoolReconcileInterval = 10 * time.Minute

// StartWarmupReconciler seeds warmup chains every interval and checks pool
// membership every warmupPoolReconcileInterval, each on one replica per pass.
func (s *tasksService) StartWarmupReconciler(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	go jobrun.Loop(ctx, "warmup_pool_membership", warmupPoolReconcileInterval, true, func(ctx context.Context) error {
		rctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		moved, removed, err := s.ReconcileWarmupPoolMembership(rctx)
		if err != nil {
			return err
		}
		if moved > 0 || removed > 0 {
			log.Info().Int("moved", moved).Int("removed", removed).Msg("warmup pool membership reconciled")
		}
		return nil
	})
	// Seeded on boot so chains recover promptly after a restart.
	jobrun.Loop(ctx, "warmup_schedule_reconcile", interval, true, func(ctx context.Context) error {
		rctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		seeded, err := s.ReconcileWarmupSchedules(rctx, warmupReconcileBatch)
		if err != nil {
			return err
		}
		if seeded > 0 {
			log.Info().Int("seeded", seeded).Msg("warmup reconcile seeded chains")
		}
		return nil
	})
}

// ReconcileWarmupPoolMembership makes every participant's membership agree with its
// entitlement: no longer entitled leaves warmup, changed tier moves pool. The warmup task does
// this too, but only for mailboxes it still runs for, and one that stopped warming has no chain
// and is not a schedule candidate, so nothing else would ever revisit it (issue #211).
// Roles are left alone so a domain-auth demotion is not quietly promoted back.
//
// One query reads every participant; entitlement, risk and tier are looked up once per
// organization, and only a mailbox in the wrong place is written.
func (s *tasksService) ReconcileWarmupPoolMembership(ctx context.Context) (moved int, removed int, err error) {
	if s.warmupRepo == nil || s.warmupHealth == nil {
		return 0, 0, nil
	}

	members, err := s.warmupRepo.ListParticipantMemberships(ctx)
	if err != nil {
		return 0, 0, err
	}

	var orgs []uuid.UUID
	seen := map[uuid.UUID]bool{}
	for _, m := range members {
		if m.OrganizationID != nil && !seen[*m.OrganizationID] {
			seen[*m.OrganizationID] = true
			orgs = append(orgs, *m.OrganizationID)
		}
	}
	restricted := s.orgsForcedToFreeWarmup(ctx, orgs)
	entitled := map[uuid.UUID]bool{}
	premium := map[uuid.UUID][2]bool{}
	hasPremium := func(org uuid.UUID) (bool, bool) {
		v, ok := premium[org]
		if !ok {
			paid, known := s.hasPremiumWarmup(ctx, org)
			v = [2]bool{paid, known}
			premium[org] = v
		}
		return v[0], v[1]
	}
	// A failed write is logged and the pass moves on; the run still reports it.
	var failed int
	var lastErr error
	fail := func(m repository.WarmupParticipantMembership, action string, xerr error) {
		failed++
		lastErr = xerr
		log.Warn().Str("email_account_id", m.EmailAccountID.String()).Str("action", action).Str("error", xerr.Error()).Msg("warmup reconcile: could not update a mailbox's membership")
	}

	for _, m := range members {
		if m.Status != "active" || m.OrganizationID == nil {
			// Nothing left to check an entitlement against, so it does not belong in a shared pool.
			if xerr := s.warmupHealth.RemoveFromAllPools(ctx, m.EmailAccountID); xerr != nil {
				fail(m, "leave", xerr)
			} else {
				removed++
			}
			continue
		}
		org := *m.OrganizationID

		if s.featureGate != nil {
			ok, cached := entitled[org]
			if !cached {
				canWarmup, xerr := s.featureGate.CanUseWarmup(ctx, org)
				if xerr != nil {
					// Entitlement unknown: leave this mailbox exactly as it is.
					continue
				}
				ok = canWarmup
				entitled[org] = ok
			}
			if !ok {
				if xerr := s.warmupHealth.RemoveFromAllPools(ctx, m.EmailAccountID); xerr != nil {
					fail(m, "leave", xerr)
				} else {
					removed++
					log.Info().
						Str("email_account_id", m.EmailAccountID.String()).
						Msg("warmup reconcile: mailbox left warmup, organization is no longer entitled")
				}
				continue
			}
		}

		poolType := warmupPoolTypeFor(m.OrganizationID, m.WarmupPoolType,
			func(uuid.UUID) bool { return restricted[org] }, hasPremium)
		if !slices.ContainsFunc(m.PoolTypes, func(p string) bool { return p != poolType }) {
			continue
		}
		didMove, xerr := s.warmupHealth.MovePoolMembership(ctx, m.EmailAccountID, poolType)
		if xerr != nil {
			fail(m, "move to "+poolType, xerr)
			continue
		}
		if didMove {
			moved++
			log.Info().
				Str("email_account_id", m.EmailAccountID.String()).
				Str("pool_type", poolType).
				Msg("warmup reconcile: mailbox moved to the pool its tier belongs to")
		}
	}

	if failed > 0 {
		return moved, removed, fmt.Errorf("%d warmup pool membership updates failed, last: %w", failed, lastErr)
	}
	return moved, removed, nil
}

// orgsForcedToFreeWarmup reads the risk state of every organization at once. Fails open, as orgSuspendedOrRestricted does.
func (s *tasksService) orgsForcedToFreeWarmup(ctx context.Context, orgs []uuid.UUID) map[uuid.UUID]bool {
	out := map[uuid.UUID]bool{}
	if s.orgRiskRepo == nil || len(orgs) == 0 {
		return out
	}
	states, err := s.orgRiskRepo.GetOrgRiskStates(ctx, orgs)
	if err != nil {
		return out
	}
	for id, state := range states {
		out[id] = state.ForcesFreeWarmupPool()
	}
	return out
}
