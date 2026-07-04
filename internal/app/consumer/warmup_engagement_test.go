package jobs

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

func TestHumanizeFireAt_DefersNightToMorning(t *testing.T) {
	// 3am local — a read at this hour is a bot signature.
	night := time.Date(2026, 1, 1, 3, 0, 0, 0, time.UTC)
	got := humanizeFireAt(night, "UTC")
	h := got.Hour()
	if h < 7 || h > 9 {
		t.Errorf("night fire time not deferred into 07:30-09:30 window: hour=%d (%v)", h, got)
	}
	if !got.After(night) {
		t.Errorf("deferred time should be later than the night time: %v", got)
	}
}

func TestHumanizeFireAt_KeepsDaytime(t *testing.T) {
	day := time.Date(2026, 1, 1, 14, 0, 0, 0, time.UTC)
	if got := humanizeFireAt(day, "UTC"); !got.Equal(day) {
		t.Errorf("daytime fire time should be unchanged, got %v", got)
	}
}

func TestHumanizeFireAt_LateNightRollsToNextMorning(t *testing.T) {
	// 23:00 should roll to the NEXT day's morning window.
	late := time.Date(2026, 1, 1, 23, 0, 0, 0, time.UTC)
	got := humanizeFireAt(late, "UTC")
	if got.Day() != 2 {
		t.Errorf("23:00 should defer to next day, got day %d (%v)", got.Day(), got)
	}
	if got.Hour() < 7 || got.Hour() > 9 {
		t.Errorf("deferred hour outside morning window: %d", got.Hour())
	}
}

func TestDwellSeconds_WithinBounds(t *testing.T) {
	for i := 0; i < 500; i++ {
		d := dwellSeconds(45, 3600, 1.0)
		if d < 45 || d > 3600 {
			t.Fatalf("dwell out of bounds: %d", d)
		}
	}
}

func TestDwellSeconds_HeavyTailedBias(t *testing.T) {
	// The u^2.2 curve should concentrate mass low: median well under the mid.
	var below int
	const n = 2000
	for i := 0; i < n; i++ {
		if dwellSeconds(45, 3600, 1.0) < (45+3600)/2 {
			below++
		}
	}
	// The u^2.2 curve puts its median around u=0.73, so ~73% of samples fall
	// below the midpoint (vs 50% for a uniform draw). Assert clearly above
	// uniform without hugging the exact figure.
	if below < n*13/20 {
		t.Errorf("expected heavy-tailed (>=65%% below midpoint), got %d/%d", below, n)
	}
}

func TestEngagementPlan_AlwaysFoldersFirst(t *testing.T) {
	set := models.WarmupEngagementSettings{
		SpamRescueRate: 85, MarkReadRate: 95, MarkImportantRate: 30, StarRate: 20,
		MinDwellSeconds: 45, MaxDwellSeconds: 3600,
	}
	for i := 0; i < 100; i++ {
		actions, delay := engagementPlan(uuid.New(), set)
		if len(actions) == 0 || actions[0] != "move_to_warmbly" {
			t.Fatalf("foldering must always be first: %v", actions)
		}
		if delay < 45 || delay > 3600 {
			t.Fatalf("delay out of bounds: %d", delay)
		}
	}
}

func TestEngagementPlan_SometimesNeglects(t *testing.T) {
	set := models.WarmupEngagementSettings{
		SpamRescueRate: 0, MarkReadRate: 100, MarkImportantRate: 100, StarRate: 100,
		MinDwellSeconds: 45, MaxDwellSeconds: 3600,
	}
	// With all positive rates at 100%, a plan with ONLY move_to_warmbly can only
	// happen via the neglect path. Over many mailboxes/messages it must occur.
	neglected := 0
	for i := 0; i < 2000; i++ {
		actions, _ := engagementPlan(uuid.New(), set)
		if len(actions) == 1 {
			neglected++
		}
	}
	if neglected == 0 {
		t.Error("engagement is never neglected — every message gets perfect engagement")
	}
}
