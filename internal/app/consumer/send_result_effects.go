package jobs

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/models"
)

func (s *JobsService) runSendResultEffects(ctx context.Context) {
	outbox, ok := s.AdvancedService.(interface{ DeliverSendResultEffects(context.Context) error })
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		pass, cancel := context.WithTimeout(ctx, 30*time.Second)
		s.reconcileRecordedSendResults(pass)
		if ok {
			if err := outbox.DeliverSendResultEffects(pass); err != nil {
				log.Warn().Msg("send result outbox delivery deferred")
			}
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *JobsService) reconcileRecordedSendResults(ctx context.Context) {
	reader, ok := s.TaskRepo.(interface {
		ListUnappliedSendResults(context.Context, int) ([]models.SendEmailResult, error)
	})
	if !ok {
		return
	}
	results, err := reader.ListUnappliedSendResults(ctx, 200)
	if err != nil {
		log.Warn().Err(err).Msg("recorded send outcomes could not be read")
		return
	}
	for _, result := range results {
		if result.Success {
			err = s.HandleEmailSent(ctx, result)
		} else {
			err = s.HandleEmailFailed(ctx, result)
		}
		if err != nil {
			log.Warn().Err(err).Str("task_id", result.TaskID.String()).Msg("recorded send outcome reconciliation deferred")
		}
	}
}
