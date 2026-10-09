package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/models"
)

func (s *JobsService) runArrivalOutbox(ctx context.Context) {
	if s.ArrivalOutbox == nil {
		return
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		pass, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := s.ArrivalOutbox.DeliverArrivals(pass, func(ctx context.Context, kind models.JobEventType, body any) error {
			return s.deliverArrivalEvent(ctx, kind, body)
		})
		cancel()
		if err != nil {
			log.Warn().Msg("sync arrival delivery deferred")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *JobsService) deliverArrivalEvent(ctx context.Context, kind models.JobEventType, body any) error {
	if _, ok := s.eventHandlers[kind]; !ok {
		return fmt.Errorf("durable arrival handler unavailable: %s", kind)
	}
	return s.HandleEvent(ctx, &models.JobEvent{Type: kind, Body: body})
}
