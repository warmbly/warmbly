package jobs

import (
	"context"

	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/models"
)

// HandleWorkerHealth persists one telemetry sample from a worker into
// worker_health_samples. The worker emits one of these every 30s; the
// row drives the worker_capacity_view that the assignment loop reads.
//
// Failure is logged but not returned as an error to the bus: a single
// missed sample is recoverable on the next tick, and returning an
// error here would block consumer progress on unrelated events.
func (s *JobsService) HandleWorkerHealth(ctx context.Context, sample models.WorkerHealthSample) error {
	if s.WorkerRepo == nil {
		return nil
	}
	if err := s.WorkerRepo.InsertWorkerHealthSample(ctx, &sample); err != nil {
		log.Warn().
			Err(err).
			Str("worker_id", sample.WorkerID.String()).
			Msg("failed to persist worker health sample")
		// Swallow: losing one sample is better than back-pressuring the
		// whole event loop. The next emission will refresh the view.
		return nil
	}
	return nil
}
