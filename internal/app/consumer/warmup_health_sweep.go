package jobs

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"
)

// StartWarmupHealthSweep runs a periodic health evaluation across all warmup pool participants.
// Runs every interval (typically once per hour) until the context is cancelled.
func (s *JobsService) StartWarmupHealthSweep(ctx context.Context, interval time.Duration) {
	if s.WarmupService == nil {
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweepCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
			evaluated, changes, xerr := s.WarmupService.EvaluateAllParticipants(sweepCtx)
			if xerr != nil {
				cancel()
				log.Warn().Str("error", xerr.Error()).Msg("warmup health sweep failed")
				continue
			}
			if evaluated > 0 {
				log.Info().Int("evaluated", evaluated).Int("state_changes", changes).Msg("warmup health sweep completed")
			}
			if changes > 0 {
				s.rebalanceRisk(sweepCtx)
			}
			cancel()
		}
	}
}
