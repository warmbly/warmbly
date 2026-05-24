package jobs

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"
)

// StartDLQRetryLoop polls for retryable dead-lettered tasks and replays them.
// Runs every interval until the context is cancelled.
func (s *JobsService) StartDLQRetryLoop(ctx context.Context, interval time.Duration) {
	if s.AdvancedService == nil {
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			retryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			retried, xerr := s.AdvancedService.ProcessRetryableDeadLetters(retryCtx)
			cancel()
			if xerr != nil {
				log.Warn().Str("error", xerr.Error()).Msg("DLQ retry processing failed")
				continue
			}
			if retried > 0 {
				log.Info().Int("retried", retried).Msg("DLQ auto-retry processed dead letters")
			}
		}
	}
}
