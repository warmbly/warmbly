package jobs

import (
	"context"
	"encoding/json"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/jobrun"
	"github.com/warmbly/warmbly/internal/models"
)

// StartWarmupEngagementPoller drains due delayed-engagement rows and publishes
// them to the worker. This is the durable replacement for the worker's old
// in-process dwell timer: because the schedule lives in Postgres, a worker (or
// consumer) restart can no longer drop the delayed read/important/star signals.
func (s *JobsService) StartWarmupEngagementPoller(ctx context.Context, interval time.Duration) {
	if s.WarmupEngagementRepo == nil || s.Publisher == nil {
		return
	}
	jobrun.Loop(ctx, "warmup_engagement_poller", interval, false, func(ctx context.Context) error {
		s.drainDueEngagements(ctx)
		return nil
	})
}

func (s *JobsService) drainDueEngagements(ctx context.Context) {
	cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	due, err := s.WarmupEngagementRepo.ClaimDuePendingEngagements(cctx, 200)
	if err != nil {
		log.Warn().Err(err).Msg("warmup engagement poller: claim failed")
		return
	}

	for _, p := range due {
		var action models.WarmupEmailAction
		if err := json.Unmarshal(p.Payload, &action); err != nil {
			log.Warn().Err(err).Str("id", p.ID.String()).Msg("warmup engagement poller: bad payload, dropping")
			continue
		}

		// Re-resolve the worker at fire time so a mid-dwell reassignment routes
		// to the current worker (the payload deliberately doesn't bake one in).
		if s.EmailRepository == nil {
			continue
		}
		account, xerr := s.EmailRepository.GetByID(cctx, action.EmailID)
		if xerr != nil || account == nil || account.WorkerID == nil {
			// Mailbox now unassigned — drop (best-effort low-stakes engagement).
			continue
		}

		action.DelaySeconds = 0 // dwell already elapsed; run immediately
		s.Publisher.PublishWarmupAction(cctx, *account.WorkerID, &action)
	}
}
