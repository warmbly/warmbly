package scheduler

import (
	"testing"

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
