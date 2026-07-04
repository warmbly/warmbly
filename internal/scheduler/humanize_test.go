package scheduler

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestHumanizeSeconds_RandomisesSubMinute(t *testing.T) {
	base := time.Date(2026, 1, 1, 10, 30, 0, 0, time.UTC)
	sawNonZero := false
	for i := 0; i < 100; i++ {
		got := humanizeSeconds(base)
		if !got.Truncate(time.Minute).Equal(base) {
			t.Fatalf("humanizeSeconds changed the minute: %v", got)
		}
		if got.Second() != 0 {
			sawNonZero = true
		}
	}
	if !sawNonZero {
		t.Error("humanizeSeconds never produced a non-zero second — fleet still sends at :00")
	}
}

func TestDailyVolumeFactor_StableWithinDayAndRanged(t *testing.T) {
	id := uuid.New()
	day := time.Date(2026, 1, 15, 9, 0, 0, 0, time.UTC)

	a := dailyVolumeFactor(id, day)
	b := dailyVolumeFactor(id, day.Add(6*time.Hour)) // same calendar day
	if a != b {
		t.Errorf("factor not stable within a day: %v vs %v", a, b)
	}

	for i := 0; i < 500; i++ {
		f := dailyVolumeFactor(uuid.New(), day)
		if f < 0.55 || f > 1.10 {
			t.Fatalf("factor out of expected range: %v", f)
		}
	}
}

func TestDailyVolumeFactor_VariesAcrossDays(t *testing.T) {
	id := uuid.New()
	seen := map[float64]bool{}
	for d := 0; d < 30; d++ {
		day := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, d)
		seen[dailyVolumeFactor(id, day)] = true
	}
	if len(seen) < 5 {
		t.Errorf("daily factor barely varies across a month (%d distinct) — looks fixed", len(seen))
	}
}
