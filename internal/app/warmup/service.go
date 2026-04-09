package warmup

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

const (
	minSpamPlacementSample = 20

	spamPlacementWatchPct        = 10.0
	spamPlacementThrottlePct     = 15.0
	spamPlacementQuarantinePct   = 20.0
	spamPlacementBlockPct        = 40.0
	spamPlacementCatastrophicPct = 80.0

	complaintRateWatchPct      = 0.03
	complaintRateQuarantinePct = 0.10
	complaintRateBlockPct      = 0.30

	bounceRateQuarantinePct = 5.0
	bounceRateBlockPct      = 10.0

	minComplaintSample = 100

	invalidTokenBlockThreshold = 3

	warmupThrottleDuration   = 3 * 24 * time.Hour
	warmupQuarantineDuration = 7 * 24 * time.Hour
	warmupBlockDuration      = 30 * 24 * time.Hour
	warmupCatastrophicBlock  = 90 * 24 * time.Hour
)

type Service interface {
	EnsurePoolMembership(ctx context.Context, accountID uuid.UUID, poolType string) *errx.Error
	RemovePoolMembership(ctx context.Context, accountID uuid.UUID, poolType string) *errx.Error
	CanParticipate(ctx context.Context, accountID uuid.UUID, poolType string) (bool, string, *errx.Error)
	ApplySpamReport(ctx context.Context, reporterAccountID, reportedAccountID uuid.UUID, messageID, reportType string) (*models.WarmupParticipantHealth, *errx.Error)
	ApplyInvalidTokenAttempt(ctx context.Context, accountID uuid.UUID, attemptedToken string, scoreDelta int) (*models.WarmupParticipantHealth, *errx.Error)
	ApplyRateLimitExceeded(ctx context.Context, accountID uuid.UUID, reason string) (*models.WarmupParticipantHealth, *errx.Error)

	// Scheduled health evaluation
	EvaluateAllParticipants(ctx context.Context) (evaluated int, stateChanges int, err *errx.Error)
	GetPoolHealthSummary(ctx context.Context) (*models.WarmupPoolHealthSummary, *errx.Error)
}

type service struct {
	repo repository.WarmupRepository
	now  func() time.Time
}

func NewService(repo repository.WarmupRepository) Service {
	return &service{
		repo: repo,
		now:  time.Now,
	}
}

func (s *service) EnsurePoolMembership(ctx context.Context, accountID uuid.UUID, poolType string) *errx.Error {
	pool, err := s.repo.GetPoolByType(ctx, poolType)
	if err != nil {
		return errx.InternalError()
	}
	if pool == nil {
		return errx.New(errx.BadRequest, "warmup pool not found")
	}
	if err := s.repo.JoinPool(ctx, pool.ID, accountID); err != nil {
		return errx.InternalError()
	}
	return nil
}

func (s *service) RemovePoolMembership(ctx context.Context, accountID uuid.UUID, poolType string) *errx.Error {
	pool, err := s.repo.GetPoolByType(ctx, poolType)
	if err != nil {
		return errx.InternalError()
	}
	if pool == nil {
		return nil
	}
	if err := s.repo.LeavePool(ctx, pool.ID, accountID); err != nil {
		return errx.InternalError()
	}
	return nil
}

func (s *service) CanParticipate(ctx context.Context, accountID uuid.UUID, poolType string) (bool, string, *errx.Error) {
	health, err := s.repo.GetParticipantHealth(ctx, accountID, poolType)
	if err != nil {
		return false, "", errx.InternalError()
	}
	if health == nil {
		return false, "not_in_pool", nil
	}

	now := s.now().UTC()
	if health.BlockedUntil != nil && !health.BlockedUntil.After(now) {
		// Block period expired. Instead of snapping back to healthy, enter probation
		// (throttled state with a 3-day window at reduced volume).
		wasBlocked := health.HealthState == models.WarmupHealthQuarantined || health.HealthState == models.WarmupHealthBlocked
		health, xerr := s.evaluateAndPersist(ctx, accountID, poolType)
		if xerr != nil {
			return false, "", xerr
		}
		if health == nil {
			return false, "not_in_pool", nil
		}
		// If metrics are clean and the mailbox was previously blocked, force probation
		if wasBlocked && health.HealthState == models.WarmupHealthHealthy {
			probationEnd := now.Add(warmupThrottleDuration)
			reason := "re-entry probation after block expiry"
			if err := s.repo.UpdateParticipantHealth(ctx, accountID, models.WarmupHealthThrottled, &probationEnd, reason, 0); err != nil {
				return false, "", errx.InternalError()
			}
			return true, "throttled", nil
		}
	}

	switch health.HealthState {
	case models.WarmupHealthQuarantined, models.WarmupHealthBlocked:
		if health.BlockedUntil == nil || health.BlockedUntil.After(now) {
			if health.BlockedReason != nil && *health.BlockedReason != "" {
				return false, *health.BlockedReason, nil
			}
			return false, string(health.HealthState), nil
		}
	case models.WarmupHealthThrottled:
		// Throttled accounts can still participate but callers should reduce volume
		return true, "throttled", nil
	}

	return true, "", nil
}

func (s *service) ApplySpamReport(ctx context.Context, reporterAccountID, reportedAccountID uuid.UUID, messageID, reportType string) (*models.WarmupParticipantHealth, *errx.Error) {
	inserted, err := s.repo.RecordSpamReport(ctx, &repository.SpamReport{
		ID:                uuid.New(),
		ReporterAccountID: reporterAccountID,
		ReportedAccountID: reportedAccountID,
		MessageID:         messageID,
		ReportType:        reportType,
	})
	if err != nil {
		return nil, errx.InternalError()
	}
	if !inserted {
		return s.getParticipantForAnyPool(ctx, reportedAccountID)
	}

	if _, err := s.repo.IncrementSpamScore(ctx, reportedAccountID, 10); err != nil {
		return nil, errx.InternalError()
	}

	return s.evaluateAndPersistAnyPool(ctx, reportedAccountID)
}

func (s *service) ApplyInvalidTokenAttempt(ctx context.Context, accountID uuid.UUID, attemptedToken string, scoreDelta int) (*models.WarmupParticipantHealth, *errx.Error) {
	if err := s.repo.RecordInvalidTokenAttempt(ctx, accountID, attemptedToken); err != nil {
		return nil, errx.InternalError()
	}
	if scoreDelta > 0 {
		if _, err := s.repo.IncrementSpamScore(ctx, accountID, scoreDelta); err != nil {
			return nil, errx.InternalError()
		}
	}
	return s.evaluateAndPersistAnyPool(ctx, accountID)
}

func (s *service) ApplyRateLimitExceeded(ctx context.Context, accountID uuid.UUID, reason string) (*models.WarmupParticipantHealth, *errx.Error) {
	blockedUntil := s.now().UTC().Add(warmupBlockDuration)
	if err := s.repo.UpdateParticipantHealth(ctx, accountID, models.WarmupHealthBlocked, &blockedUntil, reason, 100); err != nil {
		return nil, errx.InternalError()
	}
	return s.getParticipantForAnyPool(ctx, accountID)
}

func (s *service) evaluateAndPersistAnyPool(ctx context.Context, accountID uuid.UUID) (*models.WarmupParticipantHealth, *errx.Error) {
	for _, poolType := range []string{"premium", "free"} {
		health, err := s.repo.GetParticipantHealth(ctx, accountID, poolType)
		if err != nil {
			return nil, errx.InternalError()
		}
		if health == nil {
			continue
		}
		return s.evaluateAndPersist(ctx, accountID, poolType)
	}
	return nil, nil
}

func (s *service) getParticipantForAnyPool(ctx context.Context, accountID uuid.UUID) (*models.WarmupParticipantHealth, *errx.Error) {
	for _, poolType := range []string{"premium", "free"} {
		health, err := s.repo.GetParticipantHealth(ctx, accountID, poolType)
		if err != nil {
			return nil, errx.InternalError()
		}
		if health != nil {
			return health, nil
		}
	}
	return nil, nil
}

func (s *service) evaluateAndPersist(ctx context.Context, accountID uuid.UUID, poolType string) (*models.WarmupParticipantHealth, *errx.Error) {
	metrics, err := s.loadMetrics(ctx, accountID)
	if err != nil {
		return nil, errx.InternalError()
	}

	decision := evaluateMetrics(metrics, s.now().UTC())
	if err := s.repo.UpdateParticipantHealth(ctx, accountID, decision.State, decision.BlockedUntil, decision.Reason, decision.Score); err != nil {
		return nil, errx.InternalError()
	}

	health, err := s.repo.GetParticipantHealth(ctx, accountID, poolType)
	if err != nil {
		return nil, errx.InternalError()
	}
	return health, nil
}

func (s *service) loadMetrics(ctx context.Context, accountID uuid.UUID) (*models.WarmupHealthMetrics, error) {
	now := s.now().UTC()
	sentLast7d, err := s.repo.SumWarmupSentSince(ctx, accountID, now.Add(-7*24*time.Hour))
	if err != nil {
		return nil, err
	}

	spamReportsLast7d, err := s.repo.CountSpamReportsSince(ctx, accountID, now.Add(-7*24*time.Hour))
	if err != nil {
		return nil, err
	}

	invalidAttemptsLast24h, err := s.repo.CountRecentInvalidAttempts(ctx, accountID, now.Add(-24*time.Hour))
	if err != nil {
		return nil, err
	}

	spamScore, err := s.repo.GetSpamScore(ctx, accountID)
	if err != nil {
		return nil, err
	}

	rate := 0.0
	if sentLast7d > 0 {
		rate = float64(spamReportsLast7d) / float64(sentLast7d) * 100
	}

	// Load complaint and bounce counts from deliverability events (last 30 days)
	since30d := now.Add(-30 * 24 * time.Hour)
	complaintsLast30d, err := s.repo.CountDeliverabilityEventsByAccount(ctx, accountID, "complaint", since30d)
	if err != nil {
		return nil, err
	}
	bouncesLast30d, err := s.repo.CountDeliverabilityEventsByAccount(ctx, accountID, "bounce", since30d)
	if err != nil {
		return nil, err
	}
	deliveredLast30d, err := s.repo.CountDeliveredByAccount(ctx, accountID, since30d)
	if err != nil {
		return nil, err
	}

	complaintRate := 0.0
	if deliveredLast30d > 0 {
		complaintRate = float64(complaintsLast30d) / float64(deliveredLast30d) * 100
	}
	bounceRate := 0.0
	if deliveredLast30d > 0 {
		bounceRate = float64(bouncesLast30d) / float64(deliveredLast30d) * 100
	}

	return &models.WarmupHealthMetrics{
		SentLast7d:            sentLast7d,
		SpamReportsLast7d:     spamReportsLast7d,
		SpamPlacementRate:     rate,
		InvalidAttemptsLast24: invalidAttemptsLast24h,
		SpamScore:             spamScore,
		ComplaintsLast30d:     complaintsLast30d,
		DeliveredLast30d:      deliveredLast30d,
		ComplaintRate:         complaintRate,
		BouncesLast30d:        bouncesLast30d,
		BounceRate:            bounceRate,
	}, nil
}

type evaluationDecision struct {
	State        models.WarmupHealthState
	BlockedUntil *time.Time
	Reason       string
	Score        float64
}

func evaluateMetrics(metrics *models.WarmupHealthMetrics, now time.Time) evaluationDecision {
	decision := evaluationDecision{
		State: models.WarmupHealthHealthy,
		Score: metrics.SpamPlacementRate,
	}

	if metrics.InvalidAttemptsLast24 >= invalidTokenBlockThreshold {
		until := now.Add(warmupBlockDuration)
		return evaluationDecision{
			State:        models.WarmupHealthBlocked,
			BlockedUntil: &until,
			Reason:       fmt.Sprintf("invalid warmup token attempts exceeded threshold: %d in 24h", metrics.InvalidAttemptsLast24),
			Score:        maxFloat(100, metrics.SpamPlacementRate),
		}
	}

	// Evaluate complaint rate (requires minimum sample of 100 delivered in 30d)
	if metrics.DeliveredLast30d >= minComplaintSample {
		switch {
		case metrics.ComplaintRate >= complaintRateBlockPct:
			until := now.Add(warmupBlockDuration)
			return evaluationDecision{
				State:        models.WarmupHealthBlocked,
				BlockedUntil: &until,
				Reason:       fmt.Sprintf("complaint rate %.2f%% exceeded block threshold over %d delivered", metrics.ComplaintRate, metrics.DeliveredLast30d),
				Score:        maxFloat(metrics.ComplaintRate*100, metrics.SpamPlacementRate),
			}
		case metrics.ComplaintRate >= complaintRateQuarantinePct:
			until := now.Add(warmupQuarantineDuration)
			return evaluationDecision{
				State:        models.WarmupHealthQuarantined,
				BlockedUntil: &until,
				Reason:       fmt.Sprintf("complaint rate %.2f%% exceeded quarantine threshold", metrics.ComplaintRate),
				Score:        maxFloat(metrics.ComplaintRate*100, metrics.SpamPlacementRate),
			}
		case metrics.ComplaintRate >= complaintRateWatchPct:
			decision = evaluationDecision{
				State:  models.WarmupHealthWatch,
				Reason: fmt.Sprintf("complaint rate %.2f%% in watch band", metrics.ComplaintRate),
				Score:  maxFloat(metrics.ComplaintRate*100, metrics.SpamPlacementRate),
			}
		}
	}

	// Evaluate bounce rate (requires minimum sample of 100 delivered in 30d)
	if metrics.DeliveredLast30d >= minComplaintSample {
		switch {
		case metrics.BounceRate >= bounceRateBlockPct:
			until := now.Add(warmupBlockDuration)
			return evaluationDecision{
				State:        models.WarmupHealthBlocked,
				BlockedUntil: &until,
				Reason:       fmt.Sprintf("bounce rate %.1f%% exceeded block threshold over %d delivered", metrics.BounceRate, metrics.DeliveredLast30d),
				Score:        maxFloat(metrics.BounceRate, metrics.SpamPlacementRate),
			}
		case metrics.BounceRate >= bounceRateQuarantinePct:
			until := now.Add(warmupQuarantineDuration)
			return evaluationDecision{
				State:        models.WarmupHealthQuarantined,
				BlockedUntil: &until,
				Reason:       fmt.Sprintf("bounce rate %.1f%% exceeded quarantine threshold", metrics.BounceRate),
				Score:        maxFloat(metrics.BounceRate, metrics.SpamPlacementRate),
			}
		}
	}

	// Evaluate spam placement rate (requires minimum 20 warmup sends in 7d)
	if metrics.SentLast7d < minSpamPlacementSample {
		return decision
	}

	switch {
	case metrics.SpamPlacementRate >= spamPlacementCatastrophicPct:
		until := now.Add(warmupCatastrophicBlock)
		return evaluationDecision{
			State:        models.WarmupHealthBlocked,
			BlockedUntil: &until,
			Reason:       fmt.Sprintf("catastrophic warmup spam placement %.1f%% over %d sends", metrics.SpamPlacementRate, metrics.SentLast7d),
			Score:        metrics.SpamPlacementRate,
		}
	case metrics.SpamPlacementRate >= spamPlacementBlockPct:
		until := now.Add(warmupBlockDuration)
		return evaluationDecision{
			State:        models.WarmupHealthBlocked,
			BlockedUntil: &until,
			Reason:       fmt.Sprintf("warmup spam placement %.1f%% exceeded block threshold", metrics.SpamPlacementRate),
			Score:        metrics.SpamPlacementRate,
		}
	case metrics.SpamPlacementRate >= spamPlacementQuarantinePct:
		until := now.Add(warmupQuarantineDuration)
		return evaluationDecision{
			State:        models.WarmupHealthQuarantined,
			BlockedUntil: &until,
			Reason:       fmt.Sprintf("warmup spam placement %.1f%% exceeded quarantine threshold", metrics.SpamPlacementRate),
			Score:        metrics.SpamPlacementRate,
		}
	case metrics.SpamPlacementRate >= spamPlacementThrottlePct:
		until := now.Add(warmupThrottleDuration)
		return evaluationDecision{
			State:        models.WarmupHealthThrottled,
			BlockedUntil: &until,
			Reason:       fmt.Sprintf("warmup spam placement %.1f%% in throttle band", metrics.SpamPlacementRate),
			Score:        metrics.SpamPlacementRate,
		}
	case metrics.SpamPlacementRate >= spamPlacementWatchPct:
		// Only upgrade to watch if not already at a worse state from complaint checks
		if decision.State == models.WarmupHealthHealthy {
			return evaluationDecision{
				State:  models.WarmupHealthWatch,
				Reason: fmt.Sprintf("warmup spam placement %.1f%% in watch band", metrics.SpamPlacementRate),
				Score:  metrics.SpamPlacementRate,
			}
		}
		return decision
	default:
		return decision
	}
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// EvaluateAllParticipants runs a health evaluation sweep across all warmup pool participants.
// Returns the number evaluated and the number of state changes.
func (s *service) EvaluateAllParticipants(ctx context.Context) (int, int, *errx.Error) {
	accountIDs, err := s.repo.GetAllParticipantAccountIDs(ctx)
	if err != nil {
		return 0, 0, errx.InternalError()
	}

	evaluated := 0
	stateChanges := 0

	for _, accountID := range accountIDs {
		// Get current state before evaluation
		healthBefore, err := s.repo.GetParticipantHealth(ctx, accountID, "")
		if err != nil || healthBefore == nil {
			// Try both pool types
			for _, poolType := range []string{"premium", "free"} {
				healthBefore, err = s.repo.GetParticipantHealth(ctx, accountID, poolType)
				if err == nil && healthBefore != nil {
					break
				}
			}
		}

		var stateBefore models.WarmupHealthState
		if healthBefore != nil {
			stateBefore = healthBefore.HealthState
		}

		// Evaluate
		healthAfter, xerr := s.evaluateAndPersistAnyPool(ctx, accountID)
		if xerr != nil {
			continue
		}
		evaluated++

		if healthAfter != nil && healthAfter.HealthState != stateBefore {
			stateChanges++
		}
	}

	return evaluated, stateChanges, nil
}

// GetPoolHealthSummary returns an aggregate health overview across all warmup pools
func (s *service) GetPoolHealthSummary(ctx context.Context) (*models.WarmupPoolHealthSummary, *errx.Error) {
	counts, avgScore, err := s.repo.GetPoolHealthCounts(ctx)
	if err != nil {
		return nil, errx.InternalError()
	}

	total := 0
	blockedCount := 0
	atRiskCount := 0
	for state, count := range counts {
		total += count
		switch models.WarmupHealthState(state) {
		case models.WarmupHealthQuarantined, models.WarmupHealthBlocked:
			blockedCount += count
		case models.WarmupHealthWatch, models.WarmupHealthThrottled:
			atRiskCount += count
		}
	}

	return &models.WarmupPoolHealthSummary{
		TotalParticipants: total,
		ByState:           counts,
		AvgSpamScore:      avgScore,
		BlockedCount:      blockedCount,
		AtRiskCount:       atRiskCount,
	}, nil
}
