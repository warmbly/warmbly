package scheduler

import (
	"testing"

	"github.com/warmbly/warmbly/internal/app/warmupramp"
	"github.com/warmbly/warmbly/internal/models"
)

func TestAdjustmentFor(t *testing.T) {
	tests := []struct {
		name         string
		state        models.WarmupHealthState
		wantVolMult  float64
		wantWaitMult float64
	}{
		{"healthy", models.WarmupHealthHealthy, 1.0, 1.0},
		{"watch", models.WarmupHealthWatch, 0.7, 1.5},
		{"throttled", models.WarmupHealthThrottled, 0.5, 2.0},
		{"quarantined-acts-as-healthy", models.WarmupHealthQuarantined, 1.0, 1.0},
		{"blocked-acts-as-healthy", models.WarmupHealthBlocked, 1.0, 1.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := adjustmentFor(tt.state)
			if got.volumeMultiplier != tt.wantVolMult {
				t.Errorf("volumeMultiplier = %v, want %v", got.volumeMultiplier, tt.wantVolMult)
			}
			if got.minWaitMultiplier != tt.wantWaitMult {
				t.Errorf("minWaitMultiplier = %v, want %v", got.minWaitMultiplier, tt.wantWaitMult)
			}
		})
	}
}

func TestWarmupRampTarget(t *testing.T) {
	const campCap = warmupramp.ActiveCampaignCap

	tests := []struct {
		name           string
		active         bool
		base, inc, max int
		days           int
		inCampaign     bool
		want           int
	}{
		{"ramp day 0", true, 10, 1, 40, 0, false, 10},
		{"ramp day 5", true, 10, 1, 40, 5, false, 15},
		{"ramp capped at max", true, 10, 1, 40, 100, false, 40},
		{"in-campaign caps ramp down", true, 10, 1, 40, 20, true, campCap},
		{"in-campaign leaves sub-cap ramp", true, 2, 1, 40, 0, true, 2},
		{"monitor lane (warmup off, in campaign)", false, 10, 1, 40, 0, true, campCap},
		{"not warming returns cap regardless of flag", false, 10, 1, 40, 0, false, campCap},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := warmupRampTarget(tt.active, tt.base, tt.inc, tt.max, tt.days, tt.inCampaign)
			if got != tt.want {
				t.Errorf("warmupRampTarget(active=%v base=%d inc=%d max=%d days=%d inCampaign=%v) = %d, want %d",
					tt.active, tt.base, tt.inc, tt.max, tt.days, tt.inCampaign, got, tt.want)
			}
		})
	}
}
