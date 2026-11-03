package jobs

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/jobrun"
)

// StartABAutoPromoteLoop hands each step A/B split whose winner is significant to that
// winner, for campaigns with auto_promote_winner on.
func (s *JobsService) StartABAutoPromoteLoop(ctx context.Context, interval time.Duration) {
	if s.AdvancedService == nil {
		return
	}
	jobrun.Loop(ctx, "ab_auto_promote", interval, false, func(ctx context.Context) error {
		runCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		promoted, xerr := s.AdvancedService.PromoteStepWinners(runCtx)
		if xerr != nil {
			return xerr
		}
		if promoted > 0 {
			log.Info().Int("promoted", promoted).Msg("ab auto-promote handed step splits to their winners")
		}
		return nil
	})
}
