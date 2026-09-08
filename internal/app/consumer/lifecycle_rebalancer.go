package jobs

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/lifecycle"
	"github.com/warmbly/warmbly/internal/jobrun"
	"github.com/warmbly/warmbly/internal/models"
)

// StartLifecycleRebalancer moves mailboxes in and out of cold rotation on the
// warmup health signal, on the same cadence as the risk rebalancer.
//
// Separate from that one on purpose: risk_band decides which worker hosts a
// mailbox, this decides whether it is offered cold sends at all. They move on
// the same signal but mean different things, and folding them together would
// make a resting mailbox look like a dirty-IP mailbox.
func (s *JobsService) StartLifecycleRebalancer(ctx context.Context, interval time.Duration) {
	if s.LifecycleRepo == nil {
		return
	}
	jobrun.Loop(ctx, "lifecycle_rebalancer", interval, false, func(ctx context.Context) error {
		s.rebalanceLifecycles(ctx)
		return nil
	})
}

func (s *JobsService) rebalanceLifecycles(ctx context.Context) {
	candidates, err := s.LifecycleRepo.ListLifecycleCandidates(ctx, 500)
	if err != nil {
		log.Warn().Err(err).Msg("lifecycle rebalancer: list candidates failed")
		return
	}

	now := time.Now()
	rested, resumed := 0, 0
	seen := make([]uuid.UUID, 0, len(candidates))
	for _, c := range candidates {
		seen = append(seen, c.EmailAccountID)
		d := lifecycle.Decide(c.Current, c.Since, c.HealthState, now)
		if d.RestartProbation {
			if err := s.LifecycleRepo.RestartProbation(ctx, c.EmailAccountID); err != nil {
				log.Warn().Err(err).Str("email_account_id", c.EmailAccountID.String()).
					Msg("lifecycle rebalancer: could not restart probation")
			}
			continue
		}
		if !d.Changed(c.Current) {
			continue
		}
		// force=false: a mailbox its owner put in reserve is never moved here.
		moved, err := s.LifecycleRepo.SetSendLifecycle(ctx, c.EmailAccountID, d.Next, d.Reason, false)
		if err != nil {
			log.Warn().Err(err).Str("email_account_id", c.EmailAccountID.String()).
				Msg("lifecycle rebalancer: could not move mailbox")
			continue
		}
		if !moved {
			continue
		}
		if d.Next == models.SendLifecycleResting {
			rested++
		} else {
			resumed++
		}
	}
	// Stamp everything examined, so the next pass moves on rather than
	// re-reading the same page forever.
	if err := s.LifecycleRepo.MarkLifecycleChecked(ctx, seen); err != nil {
		log.Warn().Err(err).Msg("lifecycle rebalancer: could not record the checked window")
	}

	if rested > 0 || resumed > 0 {
		log.Info().Int("rested", rested).Int("resumed", resumed).Msg("lifecycle rebalancer: moved mailboxes")
	}
}
