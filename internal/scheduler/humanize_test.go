package scheduler

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestHumanizeSeconds_MovesZeroSecondLater(t *testing.T) {
	base := time.Date(2026, 1, 1, 10, 30, 0, 0, time.UTC)
	for i := 0; i < 200; i++ {
		got := humanizeSeconds(base)
		if !got.Truncate(time.Minute).Equal(base) {
			t.Fatalf("humanizeSeconds changed the minute: %v", got)
		}
		if got.Second() == 0 {
			t.Fatalf("humanizeSeconds left a slot on :00: %v", got)
		}
	}
}

func TestHumanizeSeconds_KeepsOffMinuteSlot(t *testing.T) {
	base := time.Date(2026, 1, 1, 10, 30, 17, 250, time.UTC)
	if got := humanizeSeconds(base); !got.Equal(base) {
		t.Fatalf("humanizeSeconds moved %v to %v", base, got)
	}
}

// A near-term slot the pacing chose must survive finalSlot as-is: rounding it
// to its minute used to drop it into the past, where the clamp added 5-59s and
// held one campaign to about 100 sends an hour (#847).
func TestFinalSlot_KeepsNearTermPacing(t *testing.T) {
	for i := 0; i < 200; i++ {
		slot := time.Now().Add(15 * time.Second)
		if slot.Second() == 0 {
			slot = slot.Add(time.Second)
		}
		if got := finalSlot(slot); !got.Equal(slot) {
			t.Fatalf("finalSlot moved a future slot %v to %v", slot, got)
		}
	}
}

func TestPoolSendInterval(t *testing.T) {
	cases := []struct {
		name                       string
		minutes, emails, gap, pool int
		draw                       float64
		want                       time.Duration
	}{
		// 121 mailboxes x 20/day over a 10h window: about 15s a send, not 0.
		{"large pool paces below a minute", 600, 2420, 600, 121, 0.5, 600 * time.Minute / 2420},
		{"single mailbox", 600, 20, 600, 1, 0, 990 * time.Second},
		{"pool spacing floors a crowded tail", 10, 100, 600, 10, 0, 60 * time.Second},
		{"no budget is not a division by zero", 60, 0, 600, 1, 0, 33 * time.Minute},
	}
	for _, c := range cases {
		got := poolSendInterval(c.minutes, c.emails, c.gap, c.pool, c.draw)
		if diff := got - c.want; diff < -time.Millisecond || diff > time.Millisecond {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// One campaign spends a large pool's budget across the whole window, neither
// running out of day nor front-loading it.
func TestPoolSendInterval_FillsWindow(t *testing.T) {
	minutes, emails := 600.0, 2420
	var elapsed time.Duration
	sent := 0
	for ; sent < emails; sent++ {
		left := int(minutes - elapsed.Minutes())
		if left <= 0 {
			break
		}
		elapsed += poolSendInterval(left, emails-sent, 600, 121, 0.5)
	}
	if sent < emails*99/100 {
		t.Errorf("window closed after %d of %d sends", sent, emails)
	}
	if elapsed < 9*time.Hour {
		t.Errorf("budget spent in %v, want it spread across the 10h window", elapsed)
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
