package fleet

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// Scaler evaluates fleet utilization on a slow tick (default 1h) and
// either emits a "needs more workers" alert or, when AutoProvision is
// allowed by the provisioning policy, enqueues a provisioning job from
// the active auto-template for the relevant tier.
//
// Thresholds:
//
//	info     -> fleet < 50% utilization (nothing to do)
//	warning  -> fleet >= 70% sustained, alert
//	critical -> fleet >= 85% sustained, alert + (if AUTO_PROVISION) provision
type Scaler struct {
	WorkerRepo     repository.WorkerRepository
	PolicyRepo     repository.ProvisioningPolicyRepository
	TemplateRepo   repository.ProvisioningTemplateRepository
	JobRepo        repository.ProvisioningJobRepository
	Decisions      repository.DecisionLogRepository
	Interval       time.Duration // default 1h
	CriticalThresh float64       // default 0.85
	WarningThresh  float64       // default 0.70
}

func (s *Scaler) defaults() {
	if s.Interval == 0 {
		s.Interval = time.Hour
	}
	if s.CriticalThresh == 0 {
		s.CriticalThresh = 0.85
	}
	if s.WarningThresh == 0 {
		s.WarningThresh = 0.70
	}
}

func (s *Scaler) Run(ctx context.Context) {
	s.defaults()
	tick := time.NewTicker(s.Interval)
	defer tick.Stop()
	// Run once immediately on boot so an admin doesn't wait an hour for the
	// first signal.
	_ = s.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if err := s.tick(ctx); err != nil {
				log.Warn().Err(err).Msg("fleet scale tick failed")
			}
		}
	}
}

func (s *Scaler) tick(ctx context.Context) error {
	for _, freeTier := range []bool{true, false} {
		if err := s.tickTier(ctx, freeTier); err != nil {
			log.Warn().Err(err).Bool("free_tier", freeTier).Msg("scale tier failed")
		}
	}
	return nil
}

func (s *Scaler) tickTier(ctx context.Context, freeTier bool) error {
	rows, err := s.WorkerRepo.ListCapacityCandidates(ctx, freeTier, []models.WorkerHealthState{
		models.WorkerHealthHealthy,
		models.WorkerHealthWatch,
	})
	if err != nil {
		return err
	}

	var totalLoad, totalCap float64
	for _, row := range rows {
		eff := row.BaseCapacity * row.HealthMultiplier * row.AgeMultiplier
		if eff <= 0 {
			eff = 1
		}
		totalCap += eff
		totalLoad += row.LoadScore
	}

	var util float64
	if totalCap > 0 {
		util = totalLoad / totalCap
	}

	tierName := "shared_premium"
	if freeTier {
		tierName = "shared_free"
	}

	severity := ""
	switch {
	case util >= s.CriticalThresh:
		severity = "critical"
	case util >= s.WarningThresh:
		severity = "warning"
	}

	if severity == "" {
		return nil
	}

	alertReason := fmt.Sprintf("%s utilization %.0f%% (load=%.1f cap=%.1f)", tierName, util*100, totalLoad, totalCap)
	_ = s.Decisions.Insert(ctx, &repository.DecisionLog{
		Kind:        "scale_alert",
		Reason:      alertReason,
		TriggeredBy: "auto:scale",
	})
	log.Warn().
		Str("severity", severity).
		Bool("free_tier", freeTier).
		Float64("utilization", util).
		Msg(alertReason)

	if severity != "critical" {
		return nil
	}

	// Critical — try to auto-provision if policy allows.
	pol, err := s.PolicyRepo.Get(ctx, "hetzner")
	if err != nil {
		return err
	}
	if pol == nil || !pol.AutoProvision {
		_ = s.Decisions.Insert(ctx, &repository.DecisionLog{
			Kind:        "scale_alert",
			Reason:      "auto_provision=false; admin approval required",
			TriggeredBy: "auto:scale",
		})
		return nil
	}

	tpl, err := s.TemplateRepo.GetAutoForTier(ctx, tierName)
	if err != nil {
		return err
	}
	if tpl == nil {
		_ = s.Decisions.Insert(ctx, &repository.DecisionLog{
			Kind:        "scale_alert",
			Reason:      fmt.Sprintf("no auto-template configured for tier %s", tierName),
			TriggeredBy: "auto:scale",
		})
		return nil
	}

	// Snapshot the template into a new provisioning_jobs row.
	cfgBytes, _ := json.Marshal(tpl)
	job := &repository.ProvisioningJob{
		State:       models.ProvJobPending,
		TriggeredBy: "auto:scale",
		Provider:    tpl.Provider,
		TemplateID:  &tpl.ID,
		Config:      cfgBytes,
	}
	if err := s.JobRepo.Create(ctx, job); err != nil {
		return err
	}
	_ = s.Decisions.Insert(ctx, &repository.DecisionLog{
		Kind:        "provision",
		Reason:      fmt.Sprintf("auto-provisioning from template %q (util %.0f%%)", tpl.Name, util*100),
		TriggeredBy: "auto:scale",
	})
	log.Info().
		Str("template", tpl.Name).
		Str("job", job.ID.String()).
		Msg("auto-provisioned new worker(s)")

	return nil
}
