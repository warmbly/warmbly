package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func (s *JobsService) runStoredSendResults(ctx context.Context) {
	if _, ok := s.TaskRepo.(repository.StoredSendResultRepository); !ok {
		return
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	var cursor uuid.UUID
	for {
		pass, cancel := context.WithTimeout(ctx, 30*time.Second)
		var err error
		cursor, err = s.recoverStoredSendResults(pass, cursor)
		if err != nil {
			log.Warn().Msg("stored send result reconciliation deferred")
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *JobsService) recoverStoredSendResults(ctx context.Context, after uuid.UUID) (uuid.UUID, error) {
	results, err := s.TaskRepo.(repository.StoredSendResultRepository).ListPendingSendResults(ctx, after, 200)
	if err != nil {
		return after, err
	}
	if len(results) == 0 {
		return uuid.Nil, nil
	}
	var first error
	for _, stored := range results {
		if err := ctx.Err(); err != nil {
			return after, err
		}
		after = stored.TaskID
		var result models.SendEmailResult
		err := json.Unmarshal(stored.Payload, &result)
		if err == nil && (result.TaskID == uuid.Nil || result.TaskID != stored.TaskID) {
			err = errors.New("stored send result task mismatch")
		}
		if err == nil {
			if result.Success {
				err = s.HandleEmailSent(ctx, result)
			} else {
				err = s.HandleEmailFailed(ctx, result)
			}
		}
		if err != nil && first == nil {
			first = err
		}
	}
	return after, first
}

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
