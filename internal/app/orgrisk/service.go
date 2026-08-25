package orgrisk

import (
	"context"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/app/audit"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

const restrictedColdCap = 10

type Service interface {
	Get(ctx context.Context, organizationID uuid.UUID) (*models.OrganizationRisk, error)
	RecordSignal(ctx context.Context, organizationID uuid.UUID, key string, score int, reason string, evidence map[string]any) error
	EffectiveCap(ctx context.Context, organizationID uuid.UUID, current int) int
	WarmupPool(ctx context.Context, organizationID uuid.UUID, current string) string
	SendingSuspended(ctx context.Context, organizationID uuid.UUID) bool
	EvaluateCorrelations(ctx context.Context) error
}

type service struct {
	repo  repository.OrgRiskRepository
	audit audit.AuditService
}

func NewService(repo repository.OrgRiskRepository, auditService audit.AuditService) Service {
	return &service{repo: repo, audit: auditService}
}

func (s *service) Get(ctx context.Context, organizationID uuid.UUID) (*models.OrganizationRisk, error) {
	return s.repo.Get(ctx, organizationID)
}

func (s *service) RecordSignal(ctx context.Context, organizationID uuid.UUID, key string, score int, reason string, evidence map[string]any) error {
	before, after, err := s.repo.RecordSignal(ctx, organizationID, key, models.OrganizationRiskSignal{
		Score:      score,
		Reason:     reason,
		Evidence:   evidence,
		ObservedAt: time.Now().UTC(),
	})
	if err != nil {
		return err
	}
	if before.State != after.State && s.audit != nil {
		s.audit.LogAction(ctx, organizationID, uuid.Nil, models.AuditActionSystemUpdate, models.AuditEntityOrgRisk, &organizationID, "", "", map[string]string{
			"risk_state": string(before.State) + " -> " + string(after.State),
			"risk_score": strconv.Itoa(before.Score) + " -> " + strconv.Itoa(after.Score),
		}, map[string]string{"reason": after.Reason})
	}
	return nil
}

func (s *service) EffectiveCap(ctx context.Context, organizationID uuid.UUID, current int) int {
	risk, err := s.repo.Get(ctx, organizationID)
	if err != nil || risk == nil {
		return current
	}
	switch risk.State {
	case models.OrganizationRiskSuspended:
		return 0
	case models.OrganizationRiskRestricted:
		return min(current, restrictedColdCap)
	default:
		return current
	}
}

func (s *service) WarmupPool(ctx context.Context, organizationID uuid.UUID, current string) string {
	risk, err := s.repo.Get(ctx, organizationID)
	if err == nil && risk != nil && (risk.State == models.OrganizationRiskRestricted || risk.State == models.OrganizationRiskSuspended) {
		return "free"
	}
	return current
}

func (s *service) SendingSuspended(ctx context.Context, organizationID uuid.UUID) bool {
	risk, err := s.repo.Get(ctx, organizationID)
	return err == nil && risk != nil && risk.Has(models.BanScopeSend)
}

func (s *service) EvaluateCorrelations(ctx context.Context) error {
	findings, err := s.repo.ListCorrelationFindings(ctx)
	if err != nil {
		return err
	}
	for _, finding := range findings {
		if err := s.RecordSignal(ctx, finding.OrganizationID, finding.Key, finding.Score, finding.Reason, finding.Evidence); err != nil {
			log.Warn().Err(err).Str("organization_id", finding.OrganizationID.String()).Str("signal", finding.Key).Msg("could not record organization correlation risk")
		}
	}
	return nil
}

func StartCorrelationSweep(ctx context.Context, service Service, interval time.Duration) {
	if service == nil {
		return
	}
	go func() {
		run := func() {
			if err := service.EvaluateCorrelations(ctx); err != nil {
				log.Warn().Err(err).Msg("organization risk correlation sweep failed")
			}
		}
		run()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run()
			}
		}
	}()
}
