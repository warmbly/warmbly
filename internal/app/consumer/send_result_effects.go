package jobs

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"
)

func (s *JobsService) runSendResultEffects(ctx context.Context) {
	outbox, ok := s.AdvancedService.(interface{ DeliverSendResultEffects(context.Context) error })
	if !ok {
		return
	}
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		pass, cancel := context.WithTimeout(ctx, 30*time.Second)
		if err := outbox.DeliverSendResultEffects(pass); err != nil {
			log.Warn().Msg("send result outbox delivery deferred")
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
