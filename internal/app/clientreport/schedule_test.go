package clientreport

import (
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/models"
)

func mustZone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Skipf("zone %s not in this runtime: %v", name, err)
	}
	return loc
}

func TestNextRunKeepsTheLocalHourAcrossDST(t *testing.T) {
	ny := mustZone(t, "America/New_York")
	weekly := models.ClientReportSchedule{Cadence: models.ClientReportWeekly, Weekday: int(time.Monday), Hour: 9, Timezone: "America/New_York"}

	// The Monday before US DST starts (2026-03-08) is EST, the one after is EDT.
	before, err := NextRun(weekly, time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	after, err := NextRun(weekly, before)
	if err != nil {
		t.Fatal(err)
	}
	for _, at := range []time.Time{before, after} {
		local := at.In(ny)
		if local.Weekday() != time.Monday || local.Hour() != 9 || local.Minute() != 0 {
			t.Fatalf("run %v is %v local, want Monday 09:00", at, local)
		}
	}
	if before.Hour() != 14 || after.Hour() != 13 {
		t.Fatalf("UTC hours = %d then %d, want 14 (EST) then 13 (EDT)", before.Hour(), after.Hour())
	}
	if got := after.Sub(before); got != 7*24*time.Hour-time.Hour {
		t.Fatalf("the DST week is %v long, want 167h", got)
	}
}

func TestNextRunInsideTheSpringForwardGapStillRunsThatDay(t *testing.T) {
	ny := mustZone(t, "America/New_York")
	// 02:00 does not exist on 2026-03-08 in New York.
	s := models.ClientReportSchedule{Cadence: models.ClientReportWeekly, Weekday: int(time.Sunday), Hour: 2, Timezone: "America/New_York"}
	from := time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC)
	at, err := NextRun(s, from)
	if err != nil {
		t.Fatal(err)
	}
	if local := at.In(ny); local.Year() != 2026 || local.Month() != time.March || local.Day() != 8 {
		t.Fatalf("run lands on %v, want 2026-03-08", local)
	}
	if !at.After(from) {
		t.Fatalf("run %v is not after %v", at, from)
	}
}

func TestNextRunFallBackRunsOnce(t *testing.T) {
	// 01:00 happens twice on 2026-11-01 in New York; the schedule must pick one and move on a week.
	s := models.ClientReportSchedule{Cadence: models.ClientReportWeekly, Weekday: int(time.Sunday), Hour: 1, Timezone: "America/New_York"}
	first, err := NextRun(s, time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	second, err := NextRun(s, first)
	if err != nil {
		t.Fatal(err)
	}
	if d := second.Sub(first); d < 7*24*time.Hour-time.Hour || d > 7*24*time.Hour+time.Hour {
		t.Fatalf("second run %v is %v after the first, want about a week", second, d)
	}
}

func TestNextRunAcrossZones(t *testing.T) {
	cases := []struct {
		zone    string
		now     time.Time
		weekday time.Weekday
		hour    int
		want    time.Time
	}{
		// Kolkata is UTC+05:30 with no DST: Monday 09:00 is 03:30 UTC.
		{"Asia/Kolkata", time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC), time.Monday, 9, time.Date(2026, 11, 9, 3, 30, 0, 0, time.UTC)},
		// Auckland is on NZDT (UTC+13) in November: Monday 08:00 is Sunday 19:00 UTC.
		{"Pacific/Auckland", time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC), time.Monday, 8, time.Date(2026, 11, 8, 19, 0, 0, 0, time.UTC)},
		// Already past this week's slot: the next one is a week on.
		{"UTC", time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC), time.Monday, 9, time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)},
		// Exactly on the slot is not after it.
		{"UTC", time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC), time.Monday, 9, time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)},
	}
	for _, tc := range cases {
		mustZone(t, tc.zone)
		s := models.ClientReportSchedule{Cadence: models.ClientReportWeekly, Weekday: int(tc.weekday), Hour: tc.hour, Timezone: tc.zone}
		got, err := NextRun(s, tc.now)
		if err != nil {
			t.Fatalf("%s: %v", tc.zone, err)
		}
		if !got.Equal(tc.want) {
			t.Errorf("%s: next run %v, want %v", tc.zone, got, tc.want)
		}
	}
}

func TestNextRunMonthly(t *testing.T) {
	budapest := mustZone(t, "Europe/Budapest")
	s := models.ClientReportSchedule{Cadence: models.ClientReportMonthly, MonthDay: 1, Hour: 8, Timezone: "Europe/Budapest"}
	got, err := NextRun(s, time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	// April 1 is CEST (UTC+2), after the March 29 change.
	if want := time.Date(2026, 4, 1, 6, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("next monthly run %v, want %v", got, want)
	}
	// December rolls into January of the next year.
	got, err = NextRun(s, time.Date(2026, 12, 2, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if local := got.In(budapest); local.Year() != 2027 || local.Month() != time.January || local.Day() != 1 || local.Hour() != 8 {
		t.Fatalf("December run lands on %v, want 2027-01-01 08:00", local)
	}
}

func TestNormalizeScheduleRefusesWhatCannotRun(t *testing.T) {
	ok := models.SetClientReportScheduleRequest{Recipients: []string{"Client@Example.com", "client@example.com"}, Cadence: models.ClientReportWeekly, Weekday: 1, Hour: 9, Timezone: "Europe/London"}
	s, xerr := normalizeSchedule(&ok)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if len(s.Recipients) != 1 || s.Recipients[0] != "client@example.com" || s.MonthDay != 1 {
		t.Fatalf("normalized = %+v", s)
	}
	many := make([]string, models.MaxClientReportRecipients+1)
	for i := range many {
		many[i] = string(rune('a'+i)) + "@example.com"
	}
	for name, mutate := range map[string]func(r *models.SetClientReportScheduleRequest){
		"no recipients":   func(r *models.SetClientReportScheduleRequest) { r.Recipients = nil },
		"display name":    func(r *models.SetClientReportScheduleRequest) { r.Recipients = []string{"Client <client@example.com>"} },
		"not an address":  func(r *models.SetClientReportScheduleRequest) { r.Recipients = []string{"client"} },
		"too many":        func(r *models.SetClientReportScheduleRequest) { r.Recipients = many },
		"cadence":         func(r *models.SetClientReportScheduleRequest) { r.Cadence = "daily" },
		"weekday":         func(r *models.SetClientReportScheduleRequest) { r.Weekday = 7 },
		"month day":       func(r *models.SetClientReportScheduleRequest) { r.MonthDay = 31 },
		"hour":            func(r *models.SetClientReportScheduleRequest) { r.Hour = 24 },
		"unknown zone":    func(r *models.SetClientReportScheduleRequest) { r.Timezone = "Mars/Olympus" },
		"offset not zone": func(r *models.SetClientReportScheduleRequest) { r.Timezone = "+02:00" },
	} {
		req := ok
		mutate(&req)
		if _, xerr := normalizeSchedule(&req); xerr == nil || xerr.ResponseCode() != ErrInvalidSchedule {
			t.Errorf("%s: got %v, want %s", name, xerr, ErrInvalidSchedule)
		}
	}
}
