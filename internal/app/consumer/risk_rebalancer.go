package jobs

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/models"
)

// StartRiskRebalancer recomputes per-mailbox risk bands from warmup health
// state and, when a mailbox's band no longer matches its worker's risk
// pool, migrates it to a worker in the right pool.
//
// Why a periodic batch instead of event-driven: warmup health state
// changes on a slow rolling-window basis (warmup_health_sweep job runs
// hourly), so reacting in real time doesn't buy much. A 1h batch keeps
// the model simple and avoids thundering-herd migrations after the warmup
// sweep finishes.
//
// Dedicated workers are exempt — their tenant boundary is the customer,
// not the risk band.
func (s *JobsService) StartRiskRebalancer(ctx context.Context, interval time.Duration) {
	if s.WorkerRepo == nil {
		return
	}
	if s.AssignmentService == nil {
		log.Info().Msg("risk rebalancer disabled: AssignmentService not configured")
		return
	}

	// Initial run right after boot so a fresh deploy converges quickly.
	go func() {
		boot, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		s.rebalanceRisk(boot)
	}()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
			s.rebalanceRisk(runCtx)
			cancel()
		}
	}
}

func (s *JobsService) rebalanceRisk(ctx context.Context) {
	candidates, err := s.WorkerRepo.ListRiskCandidates(ctx, 1000)
	if err != nil {
		log.Warn().Err(err).Msg("risk rebalancer: list candidates failed")
		return
	}

	var (
		updatedBand int
		migrated    int
		skipped     int
	)

	for _, c := range candidates {
		newBand := models.RiskBandFromHealth(c.HealthState)

		// 1. Update the stored band if it changed.
		if newBand != c.CurrentBand {
			if err := s.WorkerRepo.SetEmailAccountRiskBand(ctx, c.EmailAccountID, newBand); err != nil {
				log.Warn().Err(err).Str("account_id", c.EmailAccountID.String()).Msg("risk rebalancer: set band failed")
				continue
			}
			updatedBand++
		}

		// 2. Migrate if the new band's matching pool isn't where the mailbox
		// currently sits. Dedicated workers were already excluded by the
		// candidate query, so c.WorkerType is always shared (or nil/unset).
		if c.WorkerID == nil {
			skipped++
			continue
		}
		wantPool := newBand.MatchingRiskPool()
		if c.WorkerRiskPool == wantPool {
			continue
		}

		target, err := s.AssignmentService.SelectSharedWorkerForBand(ctx, c.WorkerFreeTier, newBand)
		if err != nil {
			log.Warn().Err(err).Str("account_id", c.EmailAccountID.String()).Msg("risk rebalancer: pick target failed")
			skipped++
			continue
		}
		if target == nil || target.ID == *c.WorkerID {
			skipped++
			continue
		}

		if err := s.WorkerRepo.UpdateEmailAccountWorker(ctx, c.EmailAccountID, target.ID); err != nil {
			log.Warn().Err(err).Str("account_id", c.EmailAccountID.String()).Msg("risk rebalancer: migrate failed")
			skipped++
			continue
		}
		_ = s.WorkerRepo.DecrementAccountCount(ctx, *c.WorkerID)
		_ = s.WorkerRepo.IncrementAccountCount(ctx, target.ID)
		migrated++

		// Audit each migration so admins can see the rebalancer's decisions.
		if s.AdminRepo != nil {
			_ = s.AdminRepo.CreateAuditLog(ctx, &models.AdminAuditLog{
				ID:          uuid.New(),
				AdminUserID: uuid.Nil,
				Action:      "risk_rebalance_migrate",
				TargetType:  "email_account",
				TargetID:    c.EmailAccountID,
				Details: map[string]any{
					"from_worker": c.WorkerID.String(),
					"to_worker":   target.ID.String(),
					"from_band":   string(c.CurrentBand),
					"to_band":     string(newBand),
					"health":      string(c.HealthState),
				},
				UserAgent: "system",
				CreatedAt: time.Now(),
			})
		}
	}

	if updatedBand > 0 || migrated > 0 {
		log.Info().
			Int("updated_bands", updatedBand).
			Int("migrated", migrated).
			Int("skipped", skipped).
			Int("scanned", len(candidates)).
			Msg("risk rebalancer pass complete")
	}
}
