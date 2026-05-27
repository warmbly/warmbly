// Capacity-math unit tests. The placement loop bets a lot on
// ComputeCapacity (it's the sort key for every shared-worker assignment),
// so the edge cases that historically bite - zero sends, NaN from
// divide-by-zero in the view, age younger than the ramp, perfect health
// vs disaster health - all have explicit coverage here.

package worker

import (
	"math"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

func TestComputeCapacity_TableDriven(t *testing.T) {
	type want struct {
		effective   float64
		utilization float64
		healthMul   float64
		ageMul      float64
	}
	cases := []struct {
		name string
		row  WorkerCapacityRow
		want want
	}{
		{
			name: "fresh_cold_smtp_worker_floors_at_one",
			row: WorkerCapacityRow{
				BaseCapacity:     16,
				HealthMultiplier: 1.0,
				AgeMultiplier:    0.0, // brand new
				LoadScore:        0,
			},
			// floor(16 * 1 * 0) = 0, then floored to 1
			want: want{effective: 1, utilization: 0, healthMul: 1.0, ageMul: 0.0},
		},
		{
			name: "perfect_health_full_age_oauth_worker",
			row: WorkerCapacityRow{
				BaseCapacity:     400,
				HealthMultiplier: 1.0,
				AgeMultiplier:    1.0,
				LoadScore:        100,
			},
			want: want{effective: 400, utilization: 100.0 / 400.0, healthMul: 1.0, ageMul: 1.0},
		},
		{
			name: "half_health_half_age_cold_smtp",
			row: WorkerCapacityRow{
				BaseCapacity:     16,
				HealthMultiplier: 0.5,
				AgeMultiplier:    0.5,
				LoadScore:        2,
			},
			// 16 * 0.5 * 0.5 = 4
			want: want{effective: 4, utilization: 0.5, healthMul: 0.5, ageMul: 0.5},
		},
		{
			name: "disaster_health_zero_age_warmup_only",
			row: WorkerCapacityRow{
				BaseCapacity:     25,
				HealthMultiplier: 0.0,
				AgeMultiplier:    0.0,
				LoadScore:        12,
			},
			// 25 * 0 * 0 = 0, floored to 1; utilisation 12/1
			want: want{effective: 1, utilization: 12, healthMul: 0, ageMul: 0},
		},
		{
			name: "nan_health_mul_coerced_to_zero",
			row: WorkerCapacityRow{
				BaseCapacity:     16,
				HealthMultiplier: math.NaN(),
				AgeMultiplier:    1.0,
				LoadScore:        0,
			},
			// NaN -> 0; floor(16 * 0 * 1) = 0, floored to 1
			want: want{effective: 1, utilization: 0, healthMul: 0, ageMul: 1},
		},
		{
			name: "negative_age_mul_coerced_to_zero",
			row: WorkerCapacityRow{
				BaseCapacity:     16,
				HealthMultiplier: 1.0,
				AgeMultiplier:    -0.5,
				LoadScore:        0,
			},
			want: want{effective: 1, utilization: 0, healthMul: 1, ageMul: 0},
		},
		{
			name: "over_one_health_mul_clamped",
			row: WorkerCapacityRow{
				BaseCapacity:     16,
				HealthMultiplier: 1.5,
				AgeMultiplier:    1.0,
				LoadScore:        0,
			},
			want: want{effective: 16, utilization: 0, healthMul: 1, ageMul: 1},
		},
		{
			name: "high_bounce_high_age_oauth_worker",
			row: WorkerCapacityRow{
				BaseCapacity:     400,
				HealthMultiplier: 0.2, // big bounce penalty
				AgeMultiplier:    1.0,
				LoadScore:        50,
			},
			// floor(400 * 0.2 * 1) = 80
			want: want{effective: 80, utilization: 50.0 / 80.0, healthMul: 0.2, ageMul: 1},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ComputeCapacity(tc.row)
			if got.Effective != tc.want.effective {
				t.Errorf("Effective = %v, want %v", got.Effective, tc.want.effective)
			}
			if math.Abs(got.Utilization-tc.want.utilization) > 1e-9 {
				t.Errorf("Utilization = %v, want %v", got.Utilization, tc.want.utilization)
			}
			if got.HealthMul != tc.want.healthMul {
				t.Errorf("HealthMul = %v, want %v", got.HealthMul, tc.want.healthMul)
			}
			if got.AgeMul != tc.want.ageMul {
				t.Errorf("AgeMul = %v, want %v", got.AgeMul, tc.want.ageMul)
			}
		})
	}
}

func TestComputeCapacity_NewWorkerStillEligible(t *testing.T) {
	// A brand-new worker has age_multiplier=0 and zero history. The floor
	// at Effective=1 guarantees it can still be probed instead of being
	// permanently skipped by the placement loop.
	c := ComputeCapacity(WorkerCapacityRow{
		WorkerID:         uuid.New(),
		BaseCapacity:     16,
		HealthMultiplier: 1,
		AgeMultiplier:    0,
		LoadScore:        0,
	})
	if c.Effective < 1 {
		t.Fatalf("Effective should never fall below 1, got %v", c.Effective)
	}
}

func TestMailboxWeight_TableDriven(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		warmup   bool
		want     float64
	}{
		{"warmup_overrides_provider", "gmail-api", true, 0.4},
		{"warmup_overrides_smtp_imap", "smtp_imap", true, 0.4},
		{"gmail_api_cold", "gmail-api", false, 0.05},
		{"graph_api_cold", "graph-api", false, 0.05},
		{"smtp_imap_cold", "smtp_imap", false, 1.0},
		{"empty_provider_cold", "", false, 1.0},
		{"unknown_provider_cold", "exchange-rpc", false, 1.0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MailboxWeight(tc.provider, tc.warmup); got != tc.want {
				t.Errorf("MailboxWeight(%q, %v) = %v, want %v", tc.provider, tc.warmup, got, tc.want)
			}
		})
	}
}

func TestComputeCapacity_HealthStatesArePassedThrough(t *testing.T) {
	// HealthState is metadata only inside ComputeCapacity (the SQL filter
	// already drops everything that's not healthy/watch), so verifying
	// the math doesn't accidentally use it locks the contract.
	for _, state := range []models.WorkerHealthState{
		models.WorkerHealthHealthy,
		models.WorkerHealthWatch,
		models.WorkerHealthThrottled,
		models.WorkerHealthQuarantined,
		models.WorkerHealthBlocked,
	} {
		row := WorkerCapacityRow{
			BaseCapacity:     16,
			HealthMultiplier: 1,
			AgeMultiplier:    1,
			LoadScore:        4,
			HealthState:      state,
		}
		got := ComputeCapacity(row)
		if got.Effective != 16 {
			t.Errorf("state %s: Effective changed unexpectedly (%v)", state, got.Effective)
		}
	}
}
