package worker

import (
	"context"
	"log"
	"time"
)

func (s *WorkerService) Heartbeat(ctx context.Context) {
	ticker := time.NewTicker(90 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Stopping heartbeat for", s.ID)
			return
		case <-ticker.C:
			if err := s.heartbeat(ctx); err != nil {
				log.Println("Failed to do heartbeat", err)
			}
		}
	}
}
