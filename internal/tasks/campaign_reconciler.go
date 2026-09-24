package tasks

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/config"
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

	// A chain can also be alive but asleep: a deferral parked its wakeup at the
	// campaign's next-due moment, which for a "wait 3 days" step is three days
	// out, and nothing re-reads the campaign until then. Leads imported in the
	// meantime sit at "Queued / Not started". Deferrals are capped now, so this
	// only sees parks written before that or by a schedule that genuinely is
	// days out — and it moves one only when the campaign's own next slot is
	// meaningfully sooner.
	s.repark(ctx, limit)

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
			schedAt := scheduler.WakeSlot(nextTime, cerr)
			if err := s.createCampaignTask(ctx, id, accountID, schedAt); err != nil {
				log.Warn().Err(err).Str("campaign_id", id.String()).Msg("campaign reconcile: re-seed failed")
				continue
			}
			s.clearIdle(ctx, campaign)
			seeded++
		case errors.Is(cerr, scheduler.ErrNoEmailAccounts):
			// No mailbox to send from: pause rather than spin every pass, and
			// record which of the several possible causes it was.
			s.autoPauseCampaign(ctx, id, uuid.Nil, autoPauseReason(cerr))
		case errors.Is(cerr, scheduler.ErrCampaignCompleted), errors.Is(cerr, scheduler.ErrCampaignEnded):
			// Nothing left to send (or past its end date): close it out, unless
			// what is left was refused by verification, which parks it instead.
			if errors.Is(cerr, scheduler.ErrCampaignCompleted) {
				// Both counts have to be known before a campaign can be closed,
				// and a count that failed is not zero: an unreadable one leaves
				// the campaign for the next pass rather than closing it on a
				// database hiccup.
				undeliverable, uerr := s.campaignProgressRepo.CountUndeliverableLeads(ctx, id)
				held, herr := s.campaignProgressRepo.CountHeldLeads(ctx, id)
				if uerr != nil || herr != nil {
					log.Warn().AnErr("undeliverable", uerr).AnErr("held", herr).
						Str("campaign_id", id.String()).
						Msg("could not tell a finished campaign from a parked one; leaving it for the next pass")
					continue
				}
				if undeliverable > 0 {
					s.pauseUndeliverable(ctx, id, uuid.Nil, undeliverable)
					continue
				}
				// A campaign whose remaining leads are all paused has not
				// finished; it waits for them, the same as the task path.
				if held > 0 {
					s.parkHeldLeads(ctx, campaign, uuid.Nil, held)
					continue
				}
				// A continuous campaign sits here idle by design; every pass
				// re-checks it so a lead the wake path missed is picked up.
				if campaign.Continuous {
					s.idleCampaign(ctx, campaign, uuid.Nil)
					continue
				}
			}
			s.campaignRepo.UpdateStatus(ctx, id, "completed")
		default:
			// Transient error (DB blip): leave it; the next pass retries.
			log.Warn().Err(cerr).Str("campaign_id", id.String()).Msg("campaign reconcile: next-time calc failed; will retry")
		}
	}
	return seeded, nil
}

// repark pulls a stale parked wakeup forward. For each active campaign whose
// earliest pending task sits further ahead than config.CampaignStaleParkHours,
// it recomputes the campaign's next slot; when that slot is at least
// config.CampaignReparkMarginMinutes earlier than the parked one, the parked
// task is cancelled and the chain re-seeded at the sooner slot.
//
// The margin is what keeps this from fighting legitimate schedules. A campaign
// that only sends on Mondays recomputes to next Monday too, within jitter of
// where it is already parked, so nothing moves. A campaign parked three days out
// with leads that became due an hour ago recomputes to now and moves.
func (s *tasksService) repark(ctx context.Context, limit int) {
	parked, err := s.campaignRepo.ListStaleParkedCampaigns(ctx, config.CampaignStaleParkHours*time.Hour, limit)
	if err != nil {
		log.Warn().Err(err).Msg("campaign reconcile: stale-park scan failed")
		return
	}

	for _, p := range parked {
		nextTime, _, accountID, cerr := s.scheduler.CalculateNextCampaignTime(ctx, p.CampaignID)
		if cerr != nil && !errors.Is(cerr, scheduler.ErrCampaignDeferred) {
			// Anything else (completed, no mailbox, a DB blip) is the re-seed
			// loop's business, not this one's; leave the park alone.
			continue
		}
		nextTime = scheduler.WakeSlot(nextTime, cerr)
		if !nextTime.Before(p.ScheduledAt.Add(-config.CampaignReparkMarginMinutes * time.Minute)) {
			continue
		}

		// Cancel EVERY pending task on the campaign first: createCampaignTask
		// no-ops while any is left, so cancelling only the earliest would move
		// the chain's wakeup to a later one instead of forward.
		pending, perr := s.campaignRepo.GetPendingCampaignTasks(ctx, p.CampaignID)
		if perr != nil {
			continue
		}
		cancelled := true
		for i := range pending {
			if err := s.taskRepo.UpdateTaskStatus(ctx, pending[i].ID, "cancelled"); err != nil {
				log.Warn().Err(err).Str("campaign_id", p.CampaignID.String()).Msg("campaign reconcile: could not cancel a stale park")
				cancelled = false
				break
			}
		}
		if !cancelled {
			continue
		}
		if err := s.createCampaignTask(ctx, p.CampaignID, accountID, nextTime); err != nil {
			log.Warn().Err(err).Str("campaign_id", p.CampaignID.String()).Msg("campaign reconcile: re-park failed")
			continue
		}
		log.Info().
			Str("campaign_id", p.CampaignID.String()).
			Time("was", p.ScheduledAt).
			Time("now", nextTime).
			Msg("campaign reconcile: pulled a stale wakeup forward")
	}
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
