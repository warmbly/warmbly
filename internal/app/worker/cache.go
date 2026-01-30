package worker

import (
	"context"
)

func (s *WorkerService) heartbeat(ctx context.Context) error {
	if err := s.Cache.Publish(ctx, "worker:heartbeat", s.ID).Err(); err != nil {
		return err
	}

	return nil
}
