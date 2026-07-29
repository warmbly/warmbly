package tasks

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/scheduler"
)

// campaignReconcileBatch caps how many campaigns a single reconcile pass will
// re-seed. Plenty for steady state; the next tick mops up any overflow.
const campaignReconcileBatch = 500

// overduePendingGrace is how far past its slot a pending task may run before
// the reconcilers treat its Cloud Tasks callback as lost and cancel it. Cloud
// Tasks fires within seconds of the scheduled time, so 15 minutes is far
// outside any legitimate delivery or retry window.
const overduePendingGrace = 15 * time.Minute

// ReconcileCampaignSchedules re-seeds the wakeup chain for active campaigns that
// have no pending task. A campaign chain is self-perpetuating (each tick
// enqueues the next), so a swallowed enqueue, a worker bounce mid-tick, or a
// crash between send and enqueue leaves the campaign stranded with no successor.
// Unlike warmup, campaigns have no other bootstrap once started, so this sweep
// is the backstop that keeps them from silently stalling. Returns the number of
// chains re-seeded this pass.
func (s *tasksService) ReconcileCampaignSchedules(ctx context.Context, limit int) (int, error) {
	// A pending task stranded well past its slot means the Cloud Tasks
	// callback was lost (queue wipe, emulator restart, dropped retry). It
	// blocks the "no pending task" candidate check below forever, so cancel
	// first; a late callback for a cancelled row no-ops (handler requires
	// 'pending').
	if n, err := s.taskRepo.CancelOverduePendingTasks(ctx, "campaign", overduePendingGrace); err == nil && n > 0 {
		log.Info().Int64("cancelled", n).Msg("campaign reconcile: cancelled overdue pending tasks")
	}

	ids, err := s.campaignRepo.ListCampaignScheduleCandidates(ctx, limit)
	if err != nil {
		return 0, err
	}

	seeded := 0
	for _, id := range ids {
		campaign, xerr := s.campaignRepo.GetByID(ctx, id)
		if xerr != nil || campaign == nil || campaign.Status != "active" {
			continue
		}

		// Compute the next slot the same way a normal tick does. createCampaignTask
		// holds a per-campaign advisory lock and no-ops if a pending task raced in,
		// so re-seeding is safe even if a real tick enqueues concurrently.
		nextTime, _, accountID, cerr := s.scheduler.CalculateNextCampaignTime(ctx, id)
		switch {
		case cerr == nil, errors.Is(cerr, scheduler.ErrCampaignDeferred):
			schedAt := nextTime
			if schedAt.IsZero() {
				schedAt = time.Now().UTC().Add(1 * time.Minute)
			}
			if err := s.createCampaignTask(ctx, id, accountID, schedAt); err != nil {
				log.Warn().Err(err).Str("campaign_id", id.String()).Msg("campaign reconcile: re-seed failed")
				continue
			}
			seeded++
		case errors.Is(cerr, scheduler.ErrNoEmailAccounts):
			// No mailbox to send from — pause rather than spin every pass.
			s.autoPauseCampaign(ctx, id, uuid.Nil)
		case errors.Is(cerr, scheduler.ErrCampaignCompleted), errors.Is(cerr, scheduler.ErrCampaignEnded):
			// Nothing left to send (or past its end date): close it out.
			s.campaignRepo.UpdateStatus(ctx, id, "completed")
		default:
			// Transient error (DB blip): leave it; the next pass retries.
			log.Warn().Err(cerr).Str("campaign_id", id.String()).Msg("campaign reconcile: next-time calc failed; will retry")
		}
	}
	return seeded, nil
}

// StartCampaignReconciler runs ReconcileCampaignSchedules on an interval until
// the context is cancelled. Mirrors StartWarmupReconciler and is started from
// the backend, which owns Cloud Tasks.
func (s *tasksService) StartCampaignReconciler(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Seed once on boot so chains recover promptly after a restart instead of
	// waiting a full interval.
	s.reconcileCampaignsOnce(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.reconcileCampaignsOnce(ctx)
		}
	}
}

func (s *tasksService) reconcileCampaignsOnce(ctx context.Context) {
	rctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	seeded, err := s.ReconcileCampaignSchedules(rctx, campaignReconcileBatch)
	if err != nil {
		log.Warn().Err(err).Msg("campaign reconcile pass failed")
		return
	}
	if seeded > 0 {
		log.Info().Int("seeded", seeded).Msg("campaign reconcile re-seeded chains")
	}
}
