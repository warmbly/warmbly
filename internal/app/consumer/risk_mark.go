package jobs

import (
	"context"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/models"
)

func (s *JobsService) markRiskBandFromWarmupHealth(ctx context.Context, accountID uuid.UUID, health *models.WarmupParticipantHealth) {
	if s.WorkerRepo == nil {
		return
	}

	state := models.WarmupHealthHealthy
	if health != nil {
		state = health.HealthState
	} else if resolved, ok := s.resolveWarmupHealthState(ctx, accountID); ok {
		state = resolved
	}

	band := models.RiskBandFromHealth(state)
	if err := s.WorkerRepo.SetEmailAccountRiskBand(ctx, accountID, band); err != nil {
		log.Warn().
			Err(err).
			Str("account_id", accountID.String()).
			Str("risk_band", string(band)).
			Msg("failed to update account risk band from warmup health")
	}
}

func (s *JobsService) resolveWarmupHealthState(ctx context.Context, accountID uuid.UUID) (models.WarmupHealthState, bool) {
	if s.WarmupRepo == nil {
		return "", false
	}

	var (
		worst models.WarmupHealthState
		found bool
	)
	for _, poolType := range []string{"premium", "free"} {
		health, err := s.WarmupRepo.GetParticipantHealth(ctx, accountID, poolType)
		if err != nil || health == nil {
			continue
		}
		if !found || warmupHealthRank(health.HealthState) < warmupHealthRank(worst) {
			worst = health.HealthState
			found = true
		}
	}

	return worst, found
}

func warmupHealthRank(state models.WarmupHealthState) int {
	switch state {
	case models.WarmupHealthBlocked:
		return 0
	case models.WarmupHealthQuarantined:
		return 1
	case models.WarmupHealthThrottled:
		return 2
	case models.WarmupHealthWatch:
		return 3
	case models.WarmupHealthHealthy:
		return 4
	default:
		return 5
	}
}
