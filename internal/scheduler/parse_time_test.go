package scheduler

import (
	"testing"

	"github.com/warmbly/warmbly/internal/models"
)

// The Postgres renderings are the whole point: start_time, end_time,
// warmup_start_time and warmup_end_time are `time` columns, so every read
// arrives with seconds and a fraction attached.
func TestParseTimeOfDay(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"09:00", 9 * 60},
		{"09:00:00", 9 * 60},
		{"09:00:00.000000", 9 * 60}, // what pgx hands back
		{"17:30:00.000000", 17*60 + 30},
		{"23:59:59.999999", 23*60 + 59},
		{"00:00:00.000000", 0},
		{" 08:15 ", 8*60 + 15},
		{"", 0},
		{"nonsense", 0},
		{"25:00", 0}, // out of range, not a wrapped hour
		{"12:60", 0},
		{"12", 0},
		{"-1:00", 0},
	}
	for _, tt := range tests {
		if got := parseTimeOfDay(tt.in); got != tt.want {
			t.Errorf("parseTimeOfDay(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

// effectiveWindows read an unparseable time as 0 for both ends, took
// "end <= start" as "no window", and returned an empty schedule — which
// nextScheduleSlot treats as "any time is allowed". A campaign restricted to
// weekday mornings was therefore free to send at 3am on a Sunday.
func TestEffectiveWindowsHonorsPostgresRenderedTimes(t *testing.T) {
	c := campaignWithWindow("09:00:00.000000", "17:00:00.000000", 0b0011111) // Mon-Fri
	sw := effectiveWindows(c)
	if sw.IsEmpty() {
		t.Fatal("windows are empty: the campaign schedule is not being enforced at all")
	}
	for _, wd := range []int{1, 2, 3, 4, 5} { // Monday..Friday
		if len(sw[wd]) != 1 {
			t.Fatalf("weekday %d has %d intervals, want 1", wd, len(sw[wd]))
		}
		if sw[wd][0].Start != 9*60 || sw[wd][0].End != 17*60 {
			t.Errorf("weekday %d = %d-%d, want 540-1020", wd, sw[wd][0].Start, sw[wd][0].End)
		}
	}
	if len(sw[0]) != 0 || len(sw[6]) != 0 {
		t.Error("weekend has intervals; the day-of-week gate is not being applied")
	}
}

func campaignWithWindow(start, end string, days uint8) *models.Campaign {
	return &models.Campaign{StartTime: start, EndTime: end, Days: days}
}
