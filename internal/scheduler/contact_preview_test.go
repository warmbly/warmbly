package scheduler

import (
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/models"
)

func TestProjectRampLevelMatchesDailyAdvance(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	yesterday := now.Add(-24 * time.Hour)

	c := &models.Campaign{RampEnabled: true, RampStart: 10, RampIncrement: 5, RampCeiling: 50, RampLevel: 20, RampLevelDate: &yesterday}
	projectRampLevel(c, now)
	if c.RampLevel != 25 {
		t.Fatalf("stale level must advance once: got %d", c.RampLevel)
	}
	projectRampLevel(c, now)
	if c.RampLevel != 25 {
		t.Fatalf("already advanced today must not advance again: got %d", c.RampLevel)
	}

	c = &models.Campaign{RampEnabled: true, RampStart: 10, RampIncrement: 5, RampCeiling: 50}
	projectRampLevel(c, now)
	if c.RampLevel != 10 {
		t.Fatalf("the first sending day sends ramp_start: got %d", c.RampLevel)
	}

	c = &models.Campaign{RampEnabled: true, RampStart: 10, RampIncrement: 5, RampCeiling: 8}
	projectRampLevel(c, now)
	if c.RampLevel != 8 {
		t.Fatalf("the first advance clamps to the ceiling: got %d", c.RampLevel)
	}

	c = &models.Campaign{RampEnabled: false, RampLevel: 3}
	projectRampLevel(c, now)
	if c.RampLevel != 3 || c.RampLevelDate != nil {
		t.Fatal("disabled ramp must be untouched")
	}
}
