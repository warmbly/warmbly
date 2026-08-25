package scheduler

import (
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/models"
)

func TestColdReadinessCeilingGraduatesFivePerDay(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	warmupStarted := now.Add(-30 * 24 * time.Hour)
	coldStarted := now.Add(-2 * 24 * time.Hour)
	account := models.Email{
		CampaignLimit:     50,
		Warmup:            &warmupStarted,
		WarmupBase:        10,
		WarmupIncrease:    1,
		WarmupMax:         40,
		ColdRampStartedAt: &coldStarted,
	}
	if got := coldReadinessCeiling(account, now, 0); got != 50 {
		t.Fatalf("coldReadinessCeiling() = %d, want 50", got)
	}
	if got := coldReadinessCeiling(account, now, 1); got != 38 {
		t.Fatalf("placement ceiling = %d, want 38", got)
	}
}

func TestColdReadinessCeilingStartsFreshMailboxLow(t *testing.T) {
	account := models.Email{CampaignLimit: 50}
	if got := coldReadinessCeiling(account, time.Now(), 0); got != 10 {
		t.Fatalf("coldReadinessCeiling() = %d, want 10", got)
	}
}
