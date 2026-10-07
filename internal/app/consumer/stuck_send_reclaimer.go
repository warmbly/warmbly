package jobs

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/jobrun"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// stuckSendBatch caps how many reservations one pass resolves. The next tick
// mops up any overflow.
const stuckSendBatch = 200

// Missing worker outcomes stay held; a confirmed Message-ID permits reconciliation.
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
	if recovery, ok := s.TaskRepo.(repository.SendResultRecovery); ok {
		if task == nil {
			return "", nil
		}
		result := models.SendEmailResult{TaskID: task.ID, Success: task.MessageID != "", MessageID: task.MessageID}
		err := recovery.ApplySendResult(ctx, result, func(ctx context.Context) error {
			if result.Success {
				return s.applyEmailSent(ctx, result)
			}
			return nil
		})
		if result.Success {
			return "stamped", err
		}
		return "held", err
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
	if err := s.failCampaignSend(ctx, task, reason, "SEND_OUTCOME_LOST", "", &dispatchedAt); err != nil {
		return "", err
	}
	return "reclaimed", nil
}
