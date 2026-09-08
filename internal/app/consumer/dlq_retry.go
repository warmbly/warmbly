package jobs

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/jobrun"
)

// StartDLQRetryLoop polls for retryable dead-lettered tasks and replays them.
// Runs every interval until the context is cancelled.
func (s *JobsService) StartDLQRetryLoop(ctx context.Context, interval time.Duration) {
	if s.AdvancedService == nil {
		return
	}
	jobrun.Loop(ctx, "dlq_retry", interval, false, func(ctx context.Context) error {
		retryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		retried, xerr := s.AdvancedService.ProcessRetryableDeadLetters(retryCtx)
		if xerr != nil {
			return xerr
		}
		if retried > 0 {
			log.Info().Int("retried", retried).Msg("DLQ auto-retry processed dead letters")
		}
		return nil
	})
}
