package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/config"
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
	if isolator, ok := s.TaskRepo.(interface {
		IsolateUnknownCampaignSends(context.Context, time.Time, int) (int, error)
	}); ok && s.CampaignIsolationEnabled {
		if _, err := isolator.IsolateUnknownCampaignSends(ctx, time.Now().Add(-10*time.Minute), 200); err != nil {
			log.Warn().Err(err).Msg("automatic campaign send recovery deferred")
		}
	}
	if holds, ok := s.TaskRepo.(interface {
		RecoverUnknownSendHolds(context.Context, time.Time, time.Time, int) (int, []uuid.UUID, error)
	}); ok {
		now := time.Now()
		n, unsent, err := holds.RecoverUnknownSendHolds(ctx, now.Add(-10*time.Minute), now.Add(-time.Duration(config.CampaignSendReclaimAfterMinutes)*time.Minute), 200)
		for _, id := range unsent {
			s.notifyManualSendUnsent(ctx, id)
		}
		if err != nil {
			log.Warn().Err(err).Msg("unknown send hold recovery deferred")
		} else if n > 0 {
			log.Info().Int("recovered", n).Msg("released mailboxes held by unknown sends that can never run again")
		}
	}
	if holds, ok := s.TaskRepo.(interface {
		ReleaseProviderRestoredHolds(context.Context, time.Time, int) (int, error)
	}); ok {
		n, err := holds.ReleaseProviderRestoredHolds(ctx, time.Now().Add(-config.ProviderRestoredHoldRecheckHours*time.Hour), 200)
		if err != nil {
			log.Warn().Err(err).Msg("provider-restored send hold release deferred")
		} else if n > 0 {
			log.Info().Int("released", n).Msg("released mailboxes the provider refused that have since synced in full")
		}
	}
	if holds, ok := s.TaskRepo.(interface {
		ReleaseProbeDueHolds(context.Context, int) ([]uuid.UUID, error)
	}); ok {
		released, err := holds.ReleaseProbeDueHolds(ctx, 200)
		if err != nil {
			log.Warn().Err(err).Msg("send hold re-probe deferred")
		}
		for _, id := range released {
			s.publishAccountSynced(ctx, id)
		}
		if len(released) > 0 {
			log.Info().Int("released", len(released)).Msg("released refused mailboxes for one more send")
		}
	}
}
