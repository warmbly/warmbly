package jobs

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/jobrun"
	"github.com/warmbly/warmbly/internal/repository"
)

// stuckSendBatch caps how many reservations one pass resolves. The next tick
// mops up any overflow.
const stuckSendBatch = 200

// A campaign step is reserved before its SEND_EMAIL goes on the bus and is
// resolved by the worker's answer: EMAIL_SENT stamps it, EMAIL_FAILED walks it
// back. When neither ever arrives — the worker died mid-send, or the result was
// lost — the reservation would hold the lead in flight forever, because routing
// deliberately refuses to offer a step whose outcome is unknown.
//
// This sweep is what ends that state. After
// config.CampaignSendReclaimAfterMinutes it treats the outcome as lost and
// walks the step back through the same path a worker failure takes: the
// attempt is counted, the day's counters are given back, the failure is written
// to the campaign's activity log, and the lead is dropped once the retry cap is
// spent. A worker that did answer with a Message-ID is believed instead, and
// its step is stamped rather than retried.
func (s *JobsService) StartStuckSendReclaimer(ctx context.Context, interval time.Duration) {
	if s.TaskRepo == nil || s.CampaignProgressRepo == nil {
		return
	}
	jobrun.Loop(ctx, "stuck_send_reclaimer", interval, false, func(ctx context.Context) error {
		sweepCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		s.reclaimStuckSends(sweepCtx)
		return nil
	})
}

func (s *JobsService) reclaimStuckSends(ctx context.Context) {
	window := time.Duration(config.CampaignSendReclaimAfterMinutes) * time.Minute
	stuck, err := s.CampaignProgressRepo.ListStuckDispatches(ctx, window, stuckSendBatch)
	if err != nil {
		log.Warn().Err(err).Msg("stuck send reclaim: could not list unresolved dispatches")
		return
	}
	reclaimed, stamped := 0, 0
	for _, d := range stuck {
		outcome, rerr := s.reclaimStuckSend(ctx, d)
		if rerr != nil {
			log.Warn().Err(rerr).Str("campaign_id", d.CampaignID.String()).Str("contact_id", d.ContactID.String()).Msg("stuck send reclaim: could not resolve dispatch")
			continue
		}
		switch outcome {
		case "stamped":
			stamped++
		case "reclaimed":
			reclaimed++
		}
	}
	if reclaimed > 0 || stamped > 0 {
		log.Info().Int("reclaimed", reclaimed).Int("stamped", stamped).Msg("stuck send reclaim: resolved unanswered dispatches")
	}
}

// reclaimStuckSend resolves one unanswered reservation. Returns what it did so
// the sweep can report it: "stamped" (the worker had in fact reported a
// Message-ID), "reclaimed" (walked back for retry), or "" (nothing to do).
func (s *JobsService) reclaimStuckSend(ctx context.Context, d repository.StuckDispatch) (string, error) {
	var task *repository.Task
	if d.TaskID != nil {
		var err error
		if task, err = s.TaskRepo.GetTask(ctx, *d.TaskID); err != nil {
			return "", err
		}
	}
	if task == nil {
		// Nothing left to attribute the outcome to: the task was never
		// recorded, or it aged out of retention. Walk the step back directly
		// rather than leaving the lead in flight for good.
		_, _, rolled, err := s.CampaignProgressRepo.RecordSendFailure(ctx, d.CampaignID, d.ContactID, d.SequenceID,
			"the send was never answered by a worker")
		if err != nil {
			return "", err
		}
		if rolled {
			return "reclaimed", nil
		}
		return "", nil
	}
	if task.MessageID != "" {
		// The worker put a Message-ID on the wire, so the email did leave; only
		// the stamp was lost. Never retry this one.
		ok, serr := s.CampaignProgressRepo.StampDispatchedSend(ctx, d.CampaignID, d.ContactID, d.SequenceID)
		if serr != nil {
			return "", serr
		}
		if ok {
			return "stamped", nil
		}
		return "", nil
	}

	reason := "the sending worker never reported an outcome for this email"
	if err := s.TaskRepo.RecordTaskFailure(ctx, task.ID, "Send outcome lost", reason); err != nil {
		return "", err
	}
	dispatchedAt := d.DispatchedAt
	if err := s.failCampaignSend(ctx, task, reason, "SEND_OUTCOME_LOST", &dispatchedAt); err != nil {
		return "", err
	}
	return "reclaimed", nil
}
