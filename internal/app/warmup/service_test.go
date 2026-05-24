package warmup

import (
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/models"
)

func TestEvaluateMetricsInvalidAttemptsBlock(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)

	decision := evaluateMetrics(&models.WarmupHealthMetrics{
		SentLast7d:            5,
		SpamReportsLast7d:     0,
		SpamPlacementRate:     0,
		InvalidAttemptsLast24: 3,
	}, now)

	if decision.State != models.WarmupHealthBlocked {
		t.Fatalf("expected blocked state, got %s", decision.State)
	}
	if decision.BlockedUntil == nil || !decision.BlockedUntil.Equal(now.Add(warmupBlockDuration)) {
		t.Fatalf("expected 30d block, got %#v", decision.BlockedUntil)
	}
}

func TestEvaluateMetricsSpamThresholds(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		rate        float64
		wantState   models.WarmupHealthState
		wantBlocked time.Duration
	}{
		{name: "watch", rate: 10, wantState: models.WarmupHealthWatch},
		{name: "quarantine", rate: 20, wantState: models.WarmupHealthQuarantined, wantBlocked: warmupQuarantineDuration},
		{name: "block", rate: 40, wantState: models.WarmupHealthBlocked, wantBlocked: warmupBlockDuration},
		{name: "catastrophic", rate: 80, wantState: models.WarmupHealthBlocked, wantBlocked: warmupCatastrophicBlock},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			decision := evaluateMetrics(&models.WarmupHealthMetrics{
				SentLast7d:        20,
				SpamReportsLast7d: 4,
				SpamPlacementRate: tc.rate,
			}, now)

			if decision.State != tc.wantState {
				t.Fatalf("expected %s, got %s", tc.wantState, decision.State)
			}
			if tc.wantBlocked == 0 {
				if decision.BlockedUntil != nil {
					t.Fatalf("expected no block, got %#v", decision.BlockedUntil)
				}
				return
			}
			if decision.BlockedUntil == nil || !decision.BlockedUntil.Equal(now.Add(tc.wantBlocked)) {
				t.Fatalf("expected blocked until %s, got %#v", now.Add(tc.wantBlocked), decision.BlockedUntil)
			}
		})
	}
}

func TestEvaluateMetricsThrottleBand(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)

	decision := evaluateMetrics(&models.WarmupHealthMetrics{
		SentLast7d:        25,
		SpamPlacementRate: 16.0, // between 15% (throttle) and 20% (quarantine)
	}, now)

	if decision.State != models.WarmupHealthThrottled {
		t.Fatalf("expected throttled, got %s", decision.State)
	}
	if decision.BlockedUntil == nil {
		t.Fatal("throttled should have blocked_until set")
	}
	if !decision.BlockedUntil.Equal(now.Add(warmupThrottleDuration)) {
		t.Fatalf("expected 3-day throttle, got %v", decision.BlockedUntil.Sub(now))
	}
}

func TestEvaluateMetricsComplaintRateWatch(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)

	decision := evaluateMetrics(&models.WarmupHealthMetrics{
		SentLast7d:        5,
		DeliveredLast30d:  200,
		ComplaintsLast30d: 1,
		ComplaintRate:     0.05, // between 0.03% (watch) and 0.10% (quarantine)
	}, now)

	if decision.State != models.WarmupHealthWatch {
		t.Fatalf("expected watch from complaint rate, got %s", decision.State)
	}
}

func TestEvaluateMetricsComplaintRateQuarantine(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)

	decision := evaluateMetrics(&models.WarmupHealthMetrics{
		SentLast7d:        5,
		DeliveredLast30d:  200,
		ComplaintsLast30d: 2,
		ComplaintRate:     0.15, // > 0.10% quarantine
	}, now)

	if decision.State != models.WarmupHealthQuarantined {
		t.Fatalf("expected quarantined from complaint rate, got %s", decision.State)
	}
}

func TestEvaluateMetricsComplaintRateBlock(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)

	decision := evaluateMetrics(&models.WarmupHealthMetrics{
		SentLast7d:        5,
		DeliveredLast30d:  200,
		ComplaintsLast30d: 10,
		ComplaintRate:     0.5, // > 0.30% block
	}, now)

	if decision.State != models.WarmupHealthBlocked {
		t.Fatalf("expected blocked from complaint rate, got %s", decision.State)
	}
}

func TestEvaluateMetricsBounceRateQuarantine(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)

	decision := evaluateMetrics(&models.WarmupHealthMetrics{
		SentLast7d:       5,
		DeliveredLast30d: 200,
		BouncesLast30d:   12,
		BounceRate:       6.0, // > 5% quarantine
	}, now)

	if decision.State != models.WarmupHealthQuarantined {
		t.Fatalf("expected quarantined from bounce rate, got %s", decision.State)
	}
}

func TestEvaluateMetricsBounceRateBlock(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)

	decision := evaluateMetrics(&models.WarmupHealthMetrics{
		SentLast7d:       5,
		DeliveredLast30d: 200,
		BouncesLast30d:   25,
		BounceRate:       12.5, // > 10% block
	}, now)

	if decision.State != models.WarmupHealthBlocked {
		t.Fatalf("expected blocked from bounce rate, got %s", decision.State)
	}
}

func TestEvaluateMetricsComplaintBelowSampleIgnored(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)

	decision := evaluateMetrics(&models.WarmupHealthMetrics{
		SentLast7d:        5,
		DeliveredLast30d:  50, // below 100 minimum
		ComplaintsLast30d: 5,
		ComplaintRate:     10.0,
	}, now)

	if decision.State == models.WarmupHealthQuarantined || decision.State == models.WarmupHealthBlocked {
		t.Fatalf("should not quarantine/block with insufficient sample, got %s", decision.State)
	}
}

func TestEvaluateMetricsIgnoresSmallSamples(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)

	decision := evaluateMetrics(&models.WarmupHealthMetrics{
		SentLast7d:        19,
		SpamReportsLast7d: 19,
		SpamPlacementRate: 100,
	}, now)

	if decision.State != models.WarmupHealthHealthy {
		t.Fatalf("expected healthy for undersampled account, got %s", decision.State)
	}
	if decision.BlockedUntil != nil {
		t.Fatalf("expected no block, got %#v", decision.BlockedUntil)
	}
}
