package scheduler

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/behavior"
	"github.com/warmbly/warmbly/internal/app/warmupramp"
	"github.com/warmbly/warmbly/internal/models"
)

func TestWarmupWindowRespectsGatewaySundayWithoutCloudWorkday(t *testing.T) {
	loc, err := time.LoadLocation("America/Phoenix")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.October, 11, 17, 40, 0, 0, time.UTC)
	a := &models.Email{ID: uuid.New(), Timezone: loc.String(), WarmupStartTime: "06:00", WarmupEndTime: "18:00", WarmupDays: 127}
	profile := models.DefaultSendingBehavior(a.ID)
	at, err := warmupDispatchWindow(a, behavior.NewStandalone(profile, loc), now)
	if err != nil || !at.Equal(now) {
		t.Fatalf("gateway Sunday was closed: at=%v err=%v", at, err)
	}
	profile.Enabled = true
	at, err = warmupDispatchWindow(a, behavior.NewStandalone(profile, loc), now)
	if err != nil || !at.Equal(now) {
		t.Fatalf("explicit Sunday selection was skipped: at=%v err=%v", at, err)
	}
	a.WarmupDays = 0
	at, err = warmupDispatchWindow(a, behavior.NewStandalone(profile, loc), now)
	if err != nil || at.In(loc).Weekday() != time.Monday || !at.After(now) {
		t.Fatalf("inherited weekday schedule changed: at=%v err=%v", at, err)
	}
}

func TestWarmupDispatchWindowClassifiesInvalidHoursWithoutOpeningThem(t *testing.T) {
	now := time.Date(2026, time.October, 10, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		start, end string
		invalid    bool
	}{
		{"08:00", "07:00", true},
		{"08:00:00", "07:00:00", true},
		{"08:00", "08:00", true},
		{"08:00:00", "19:00:00", false},
	} {
		t.Run(tc.start+"/"+tc.end, func(t *testing.T) {
			a := &models.Email{WarmupStartTime: tc.start, WarmupEndTime: tc.end, WarmupDays: 127}
			at, err := warmupDispatchWindow(a, behavior.Resolved{}, now)
			if tc.invalid {
				if !errors.Is(err, ErrWarmupTimeWindowInvalid) || !at.IsZero() {
					t.Fatalf("invalid hours opened: at=%v err=%v", at, err)
				}
			} else if err != nil || !at.Equal(now) {
				t.Fatalf("valid hours rejected: at=%v err=%v", at, err)
			}
		})
	}
}

func TestWarmupSelectedDaysKeepHoursAndBreaks(t *testing.T) {
	b := models.DefaultSendingBehavior(uuid.New())
	b.Enabled = true
	b.WorkStartMin, b.WorkStartMax = 9*60, 9*60
	b.WorkEndMin, b.WorkEndMax = 17*60, 17*60
	b.LunchEnabled = true
	b.LunchEarliest, b.LunchLatest = 12*60, 12*60
	b.LunchMinMinutes, b.LunchMaxMinutes = 60, 60
	sunday := time.Date(2026, time.October, 11, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name             string
		days, fromMinute int
		wantDay, wantMin int
	}{
		{"explicit weekend", 127, 10 * 60, 0, 10 * 60},
		{"inherited weekdays", 0, 10 * 60, 1, 9 * 60},
		{"warmup starts later", 127, 8 * 60, 0, 9 * 60},
		{"weekend lunch", 127, 12*60 + 30, 0, 13 * 60},
		{"after close on Sunday-only schedule", 64, 18 * 60, 7, 9 * 60},
		{"weekdays selected", 31, 10 * 60, 1, 9 * 60},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &models.Email{WarmupDays: tc.days, WarmupStartTime: "08:00", WarmupEndTime: "16:00"}
			at, err := warmupDispatchWindow(a, behavior.NewStandalone(b, time.UTC), sunday.Add(time.Duration(tc.fromMinute)*time.Minute))
			want := sunday.AddDate(0, 0, tc.wantDay).Add(time.Duration(tc.wantMin) * time.Minute)
			if err != nil || !at.Equal(want) {
				t.Fatalf("at=%v err=%v, want %v", at, err, want)
			}
		})
	}
	a := &models.Email{WarmupDays: 127, WarmupStartTime: "18:00", WarmupEndTime: "20:00"}
	if at, err := warmupDispatchWindow(a, behavior.NewStandalone(b, time.UTC), sunday); !errors.Is(err, ErrWarmupTimeWindowInvalid) || !at.IsZero() {
		t.Fatalf("disjoint hours must defer as a configuration error: at=%v err=%v", at, err)
	}
}

func TestAdjustmentFor(t *testing.T) {
	tests := []struct {
		name         string
		state        models.WarmupHealthState
		wantVolMult  float64
		wantWaitMult float64
	}{
		{"healthy", models.WarmupHealthHealthy, 1.0, 1.0},
		{"watch", models.WarmupHealthWatch, 0.7, 1.0},
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
