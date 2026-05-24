package jobs

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

// StartWorkerHeartbeatSync mirrors workers' Redis heartbeat timestamps into
// workers.last_seen_at so the admin dashboard can render liveness without
// needing Redis access.
//
// Runs on a tight interval (the dead-worker detection job runs every 5min and
// does heavy reassignment work — this one is the cheap visibility-only loop).
func (s *JobsService) StartWorkerHeartbeatSync(ctx context.Context, interval time.Duration) {
	if s.WorkerRepo == nil || s.Cache == nil {
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			s.syncHeartbeats(runCtx)
			cancel()
		}
	}
}

func (s *JobsService) syncHeartbeats(ctx context.Context) {
	workers, err := s.WorkerRepo.GetAllActiveWorkers(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("heartbeat sync: list workers failed")
		return
	}

	for _, w := range workers {
		key := "worker:heartbeat:" + w.ID.String()
		val, err := s.Cache.Get(ctx, key).Result()
		if errors.Is(err, redis.Nil) {
			continue // no heartbeat in window; leave last_seen_at as-is
		}
		if err != nil {
			continue
		}
		t, err := time.Parse(time.RFC3339, val)
		if err != nil {
			continue
		}
		if err := s.WorkerRepo.UpdateLastSeen(ctx, w.ID, t); err != nil {
			log.Warn().Err(err).Str("worker_id", w.ID.String()).Msg("heartbeat sync: update failed")
		}
	}
}
