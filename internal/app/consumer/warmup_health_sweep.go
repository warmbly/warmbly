package jobs

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/jobrun"
)

// StartWarmupHealthSweep runs a periodic health evaluation across all warmup pool participants.
// Runs every interval (typically once per hour) until the context is cancelled.
func (s *JobsService) StartWarmupHealthSweep(ctx context.Context, interval time.Duration) {
	if s.WarmupService == nil {
		return
	}
	jobrun.Loop(ctx, "warmup_health_sweep", interval, false, func(ctx context.Context) error {
		sweepCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		evaluated, changes, xerr := s.WarmupService.EvaluateAllParticipants(sweepCtx)
		if xerr != nil {
			return xerr
		}
		if evaluated > 0 {
			log.Info().Int("evaluated", evaluated).Int("state_changes", changes).Msg("warmup health sweep completed")
		}
		if changes > 0 {
			s.rebalanceRisk(sweepCtx)
		}
		return nil
	})
}
