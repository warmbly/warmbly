package jobs

import (
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/models"
)

func acct(tz, start, end string) *models.Email {
	return &models.Email{Timezone: tz, WarmupStartTime: start, WarmupEndTime: end}
}

func TestWithinWarmupHours(t *testing.T) {
	a := acct("UTC", "09:00:00.000000", "17:00:00.000000")

	// Inside the window: untouched, so the drawn delay survives.
	inside := time.Date(2026, 3, 4, 11, 20, 0, 0, time.UTC)
	if got := withinWarmupHours(inside, a); !got.Equal(inside) {
		t.Errorf("in-window time moved: got %s want %s", got, inside)
	}

	// Before it opens: forward to the opening, same day.
	early := time.Date(2026, 3, 4, 6, 0, 0, 0, time.UTC)
	got := withinWarmupHours(early, a)
	if got.Day() != 4 || got.Hour() < 9 || got.Hour() > 9 {
		t.Errorf("early = %s, want just after 09:00 the same day", got)
	}
	if got.Before(early) == false && got.Hour() != 9 {
		t.Errorf("early = %s, want the 09:00 opening", got)
	}

	// After it closes: the next day's opening.
	late := time.Date(2026, 3, 4, 22, 0, 0, 0, time.UTC)
	got = withinWarmupHours(late, a)
	if got.Day() != 5 || got.Hour() != 9 {
		t.Errorf("late = %s, want 09:xx the next day", got)
	}
}

// A window shorter than the jitter must not push the reply past its own close.
func TestWithinWarmupHoursClampsJitterToAShortWindow(t *testing.T) {
	a := acct("UTC", "09:00", "09:20")
	closeAt := 9*60 + 20

	for i := 0; i < 200; i++ {
		for _, at := range []time.Time{
			time.Date(2026, 3, 4, 6, 0, 0, 0, time.UTC),  // before open
			time.Date(2026, 3, 4, 22, 0, 0, 0, time.UTC), // after close
		} {
			got := withinWarmupHours(at, a)
			if mins := got.Hour()*60 + got.Minute(); mins > closeAt {
				t.Fatalf("scheduled %s, past the 09:20 close", got)
			}
		}
	}
}

// An inverted window (close before open) has no reachable slot, so the time is
// left alone rather than moved somewhere arbitrary.
func TestWithinWarmupHoursWithAnInvertedWindowIsANoop(t *testing.T) {
	at := time.Date(2026, 3, 4, 3, 0, 0, 0, time.UTC)
	if got := withinWarmupHours(at, acct("UTC", "20:00", "08:00")); !got.Equal(at) {
		t.Errorf("inverted window moved the time to %s", got)
	}
}

// Unreadable times fall back to the documented 08:00-20:00 defaults rather than
// leaving the reply wherever it landed, which could be 03:00.
func TestWithinWarmupHoursFallsBackToBusinessHours(t *testing.T) {
	at := time.Date(2026, 3, 4, 3, 0, 0, 0, time.UTC)
	got := withinWarmupHours(at, acct("UTC", "", ""))
	if got.Hour() < 8 || got.Hour() > 8 {
		t.Errorf("got %s, want the 08:00 default opening", got)
	}
}
