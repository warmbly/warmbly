package jobs

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/jobrun"
)

// Warmup placement sweep.
//
// A live arrival counts its own receipt the moment it is verified. This loop
// counts every receipt that path did not: receipts on file before the rollup
// existed, which is how placement history is seeded, and any a consumer wrote
// without counting (one mid-upgrade, or a failed write). Claiming is atomic, so
// the two paths never count a receipt twice.

const (
	warmupPlacementSweepBatch = 2000
	// warmupPlacementSweepBatches bounds one tick, so a large backfill spreads
	// over several minutes instead of holding the database.
	warmupPlacementSweepBatches = 20
	// warmupPlacementSweepGrace leaves a fresh receipt to the live path, which
	// knows the Gmail tab and the rescue the sweep cannot see.
	warmupPlacementSweepGrace = 10 * time.Minute
)

// StartWarmupPlacementSweep counts uncounted warmup receipts in bounded batches.
func (s *JobsService) StartWarmupPlacementSweep(ctx context.Context) {
	if s.WarmupPlacementRepo == nil {
		return
	}
	jobrun.Loop(ctx, "warmup_placement_sweep", time.Minute, true, func(ctx context.Context) error {
		total := 0
		for range warmupPlacementSweepBatches {
			batchCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
			n, err := s.WarmupPlacementRepo.SweepUnplaced(batchCtx, time.Now().Add(-warmupPlacementSweepGrace), warmupPlacementSweepBatch)
			cancel()
			if err != nil {
				return err
			}
			total += n
			if n < warmupPlacementSweepBatch {
				break
			}
		}
		if total > 0 {
			log.Info().Int("receipts", total).Msg("warmup placement sweep counted receipts")
		}
		return nil
	})
}
