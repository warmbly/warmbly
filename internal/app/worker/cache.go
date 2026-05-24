package worker

import (
	"context"
	"fmt"
	"time"
)

const heartbeatTTL = 3 * time.Minute // Workers heartbeat every 90s, so 3min TTL means 1 missed beat is OK

func (s *WorkerService) heartbeat(ctx context.Context) error {
	key := fmt.Sprintf("worker:heartbeat:%s", s.ID)
	return s.Cache.Set(ctx, key, time.Now().UTC().Format(time.RFC3339), heartbeatTTL).Err()
}
