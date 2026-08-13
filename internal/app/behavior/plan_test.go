package behavior

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

var testAccount = uuid.MustParse("6f1b1c4e-2b7a-4a1e-9d0e-2f3a4b5c6d7e")

func profile() models.SendingBehavior {
	b := models.DefaultSendingBehavior(testAccount)
	b.Enabled = true
	return b
}

func day(t *testing.T, loc *time.Location, y int, m time.Month, d int) time.Time {
	t.Helper()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

// A Wednesday, so the default Mon-Fri mask is working.
func wednesday(t *testing.T, loc *time.Location) time.Time {
	t.Helper()
	d := day(t, loc, 2026, time.August, 12)
	if d.Weekday() != time.Wednesday {
		t.Fatalf("fixture is %s, expected Wednesday", d.Weekday())
	}
	return d
}

func TestRollPlanIsDeterministic(t *testing.T) {
	loc := time.UTC
	d := wednesday(t, loc)

	first := RollPlan(profile(), d)
	for i := 0; i < 50; i++ {
		again := RollPlan(profile(), d)
		if again != first {
			// DailyPlan holds pointers, so compare the fields that matter.
			if again.DailyLimit != first.DailyLimit ||
				again.HourlyLimit != first.HourlyLimit ||
				again.WorkStartMinute != first.WorkStartMinute ||
				again.WorkEndMinute != first.WorkEndMinute ||
				again.HasLunch() != first.HasLunch() {
				t.Fatalf("roll %d differs: %+v vs %+v", i, again, first)
			}
			if first.HasLunch() && (*again.LunchStartMinute != *first.LunchStartMinute ||
				*again.LunchEndMinute != *first.LunchEndMinute) {
				t.Fatalf("roll %d lunch differs", i)
			}
		}
	}
}

func TestRollPlanVariesAcrossDaysAndMailboxes(t *testing.T) {
	loc := time.UTC
	b := profile()

	starts := map[int]bool{}
	for i := 0; i < 20; i++ {
		p := RollPlan(b, day(t, loc, 2026, time.August, 3+i))
		if p.IsWorkingDay {
			starts[p.WorkStartMinute] = true
		}
	}
	if len(starts) < 3 {
		t.Fatalf("work start barely varies across 20 days: %v", starts)
	}

	other := b
	other.EmailAccountID = uuid.MustParse("11111111-2222-3333-4444-555555555555")
	a := RollPlan(b, wednesday(t, loc))
	c := RollPlan(other, wednesday(t, loc))
	if a.WorkStartMinute == c.WorkStartMinute && a.DailyLimit == c.DailyLimit && a.WorkEndMinute == c.WorkEndMinute {
		t.Fatal("two different mailboxes rolled an identical day")
	}
}

func TestRollPlanStaysInsideConfiguredRanges(t *testing.T) {
	loc := time.UTC
	b := profile()

	for i := 0; i < 400; i++ {
		p := RollPlan(b, day(t, loc, 2026, time.January, 1).AddDate(0, 0, i))
		if !p.IsWorkingDay {
			if p.DailyLimit != 0 || p.HourlyLimit != 0 {
				t.Fatalf("non-working day carries a budget: %+v", p)
			}
			continue
		}
		if p.DailyLimit < b.DailyLimitMin || p.DailyLimit > b.DailyLimitMax {
			t.Fatalf("daily limit %d outside [%d,%d]", p.DailyLimit, b.DailyLimitMin, b.DailyLimitMax)
		}
		if p.HourlyLimit < b.HourlyLimitMin || p.HourlyLimit > b.HourlyLimitMax {
			t.Fatalf("hourly limit %d outside [%d,%d]", p.HourlyLimit, b.HourlyLimitMin, b.HourlyLimitMax)
		}
		if p.WorkStartMinute < b.WorkStartMin || p.WorkStartMinute > b.WorkStartMax {
			t.Fatalf("work start %d outside [%d,%d]", p.WorkStartMinute, b.WorkStartMin, b.WorkStartMax)
		}
		if p.WorkEndMinute < b.WorkEndMin || p.WorkEndMinute > b.WorkEndMax {
			t.Fatalf("work end %d outside [%d,%d]", p.WorkEndMinute, b.WorkEndMin, b.WorkEndMax)
		}
		if p.WorkEndMinute <= p.WorkStartMinute {
			t.Fatalf("workday ends before it starts: %+v", p)
		}
		if p.HasLunch() {
			ls, le := *p.LunchStartMinute, *p.LunchEndMinute
			if ls <= p.WorkStartMinute || le >= p.WorkEndMinute {
				t.Fatalf("lunch %d-%d escapes the workday %d-%d", ls, le, p.WorkStartMinute, p.WorkEndMinute)
			}
			length := le - ls
			if length < b.LunchMinMinutes || length > b.LunchMaxMinutes {
				t.Fatalf("lunch length %d outside [%d,%d]", length, b.LunchMinMinutes, b.LunchMaxMinutes)
			}
		}
	}
}

func TestRollPlanSkipsNonWorkingWeekdays(t *testing.T) {
	loc := time.UTC
	b := profile()
	// Saturday and Sunday are outside the default Mon-Fri mask.
	for _, d := range []int{15, 16} {
		p := RollPlan(b, day(t, loc, 2026, time.August, d))
		if p.IsWorkingDay {
			t.Fatalf("weekend day %d treated as a working day", d)
		}
	}
}

func TestWorksOnUsesMondayIndexedMask(t *testing.T) {
	b := profile()
	b.Weekdays = 1 // Monday only
	if !b.WorksOn(time.Monday) {
		t.Fatal("bit 0 should be Monday")
	}
	for _, wd := range []time.Weekday{time.Sunday, time.Tuesday, time.Saturday} {
		if b.WorksOn(wd) {
			t.Fatalf("%s should not be a working day for mask 1", wd)
		}
	}

	b.Weekdays = 64 // bit 6
	if !b.WorksOn(time.Sunday) {
		t.Fatal("bit 6 should be Sunday")
	}
}

// planner builds a Planner over a fixed profile, with no database in the way.
func planner(b models.SendingBehavior) Planner {
	return func(d time.Time) models.DailyPlan { return RollPlan(b, d) }
}

func TestNextOpenSnapsToWorkdayStart(t *testing.T) {
	loc := time.UTC
	b := profile()
	d := wednesday(t, loc)
	plan := RollPlan(b, d)

	from := AtMinute(d, 4*60) // 04:00, long before work
	got, _, ok := NextOpen(from, loc, planner(b))
	if !ok {
		t.Fatal("expected a slot")
	}
	if MinuteOfDay(got, loc) != plan.WorkStartMinute {
		t.Fatalf("got %s, want the workday start %d", got, plan.WorkStartMinute)
	}
}

func TestNextOpenPreservesAnInstantAlreadyInsideTheWindow(t *testing.T) {
	loc := time.UTC
	b := profile()
	d := wednesday(t, loc)
	plan := RollPlan(b, d)

	// A second-precision instant inside the morning block.
	from := AtMinute(d, plan.WorkStartMinute+10).Add(37 * time.Second)
	got, _, ok := NextOpen(from, loc, planner(b))
	if !ok {
		t.Fatal("expected a slot")
	}
	if !got.Equal(from) {
		t.Fatalf("instant inside the window was moved: %s -> %s", from, got)
	}
}

func TestNextOpenJumpsTheLunchBreak(t *testing.T) {
	loc := time.UTC
	b := profile()
	d := wednesday(t, loc)
	plan := RollPlan(b, d)
	if !plan.HasLunch() {
		t.Skip("this day rolled without a break")
	}

	from := AtMinute(d, *plan.LunchStartMinute+5)
	got, _, ok := NextOpen(from, loc, planner(b))
	if !ok {
		t.Fatal("expected a slot")
	}
	if MinuteOfDay(got, loc) != *plan.LunchEndMinute {
		t.Fatalf("got %d, want the end of the break %d", MinuteOfDay(got, loc), *plan.LunchEndMinute)
	}
}

func TestNextOpenRollsPastTheWeekend(t *testing.T) {
	loc := time.UTC
	b := profile()
	friday := day(t, loc, 2026, time.August, 14)
	if friday.Weekday() != time.Friday {
		t.Fatalf("fixture is %s", friday.Weekday())
	}

	from := AtMinute(friday, 23*60)
	got, _, ok := NextOpen(from, loc, planner(b))
	if !ok {
		t.Fatal("expected a slot")
	}
	if got.Weekday() != time.Monday {
		t.Fatalf("Friday night rolled to %s, want Monday", got.Weekday())
	}
	monday := day(t, loc, 2026, time.August, 17)
	if MinuteOfDay(got, loc) != RollPlan(b, monday).WorkStartMinute {
		t.Fatalf("did not land on Monday's rolled start: %s", got)
	}
}

func TestNextOpenGivesUpWhenNoDayIsWorking(t *testing.T) {
	loc := time.UTC
	b := profile()
	b.Weekdays = 0
	if _, _, ok := NextOpen(wednesday(t, loc), loc, planner(b)); ok {
		t.Fatal("a profile with no working days should have no slot")
	}
}

func TestNextOpenHonoursTheMailboxTimezone(t *testing.T) {
	denver, err := time.LoadLocation("America/Denver")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	b := profile()
	d := wednesday(t, denver)
	plan := RollPlan(b, d)

	// 06:00 in Denver is well before the workday there, even though the same
	// instant is mid-morning in UTC.
	from := AtMinute(d, 6*60)
	got, _, ok := NextOpen(from, denver, planner(b))
	if !ok {
		t.Fatal("expected a slot")
	}
	if MinuteOfDay(got, denver) != plan.WorkStartMinute {
		t.Fatalf("got %s local, want the Denver workday start", got.In(denver))
	}
}

func TestAtMinuteSurvivesADSTTransition(t *testing.T) {
	london, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	// 29 March 2026: clocks go forward at 01:00 local.
	d := day(t, london, 2026, time.March, 29)
	got := AtMinute(d, 9*60+15)
	if h, m := got.Hour(), got.Minute(); h != 9 || m != 15 {
		t.Fatalf("DST day produced %02d:%02d, want 09:15", h, m)
	}
}

func TestPlanContainsAndNextOpenMinuteAgree(t *testing.T) {
	loc := time.UTC
	p := RollPlan(profile(), wednesday(t, loc))
	for minute := 0; minute < models.MinutesPerDay; minute++ {
		open, ok := p.NextOpenMinute(minute)
		if p.Contains(minute) {
			if !ok || open != minute {
				t.Fatalf("minute %d is inside the window but NextOpenMinute returned (%d,%v)", minute, open, ok)
			}
			continue
		}
		if ok && !p.Contains(open) {
			t.Fatalf("minute %d resolved to %d, which is also closed", minute, open)
		}
	}
}

func TestWorkingMinutesExcludesTheBreak(t *testing.T) {
	p := RollPlan(profile(), wednesday(t, time.UTC))
	want := p.WorkEndMinute - p.WorkStartMinute
	if p.HasLunch() {
		want -= *p.LunchEndMinute - *p.LunchStartMinute
	}
	if got := p.WorkingMinutes(); got != want {
		t.Fatalf("WorkingMinutes = %d, want %d", got, want)
	}
}

func TestDrawGapStaysInRangeAndVaries(t *testing.T) {
	p := RollPlan(profile(), wednesday(t, time.UTC))
	seq := make([]float64, 0, 64)
	next := func() float64 {
		// A simple deterministic sequence standing in for math/rand.
		v := float64(len(seq)%97) / 97.0
		seq = append(seq, v)
		return v
	}

	seen := map[time.Duration]bool{}
	for i := 0; i < 64; i++ {
		g := DrawGap(p, next)
		if g < time.Duration(p.GapMinSeconds)*time.Second || g > time.Duration(p.GapMaxSeconds)*time.Second {
			t.Fatalf("gap %s outside [%ds,%ds]", g, p.GapMinSeconds, p.GapMaxSeconds)
		}
		seen[g] = true
	}
	if len(seen) < 5 {
		t.Fatalf("gaps barely vary: %d distinct values", len(seen))
	}
}

func TestDrawGapWithACollapsedRangeIsExact(t *testing.T) {
	p := RollPlan(profile(), wednesday(t, time.UTC))
	p.GapMinSeconds, p.GapMaxSeconds = 300, 300
	if got := DrawGap(p, func() float64 { return 0.5 }); got != 300*time.Second {
		t.Fatalf("collapsed range produced %s, want 300s", got)
	}
}

func TestValidateAcceptsTheDefaults(t *testing.T) {
	if err := profile().Validate(); err != nil {
		t.Fatalf("defaults should validate: %v", err)
	}
}

func TestValidateRejectsBadProfiles(t *testing.T) {
	cases := []struct {
		name  string
		mutot func(*models.SendingBehavior)
		field string
	}{
		{"inverted daily range", func(b *models.SendingBehavior) { b.DailyLimitMin, b.DailyLimitMax = 50, 20 }, "daily_limit"},
		{"inverted hourly range", func(b *models.SendingBehavior) { b.HourlyLimitMin, b.HourlyLimitMax = 9, 2 }, "hourly_limit"},
		{"gap below the floor", func(b *models.SendingBehavior) { b.GapMinSeconds = 5 }, "gap_seconds"},
		{"inverted gap range", func(b *models.SendingBehavior) { b.GapMinSeconds, b.GapMaxSeconds = 400, 100 }, "gap_seconds"},
		{"day ends before it can start", func(b *models.SendingBehavior) { b.WorkEndMin, b.WorkEndMax = 500, 520 }, "work_end"},
		{"lunch outside the workday", func(b *models.SendingBehavior) { b.LunchEarliest, b.LunchLatest = 60, 120 }, "lunch_window"},
		{"lunch overruns the day", func(b *models.SendingBehavior) { b.LunchLatest = 1030 }, "lunch_window"},
		{"enabled with no days", func(b *models.SendingBehavior) { b.Weekdays = 0 }, "weekdays"},
		{"weekday mask out of range", func(b *models.SendingBehavior) { b.Weekdays = 999 }, "weekdays"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := profile()
			tc.mutot(&b)
			err := b.Validate()
			if err == nil {
				t.Fatal("expected a validation error")
			}
			var ve *models.BehaviorValidationError
			if !asValidationError(err, &ve) {
				t.Fatalf("expected a BehaviorValidationError, got %T", err)
			}
			if ve.Field != tc.field {
				t.Fatalf("field = %q, want %q (%v)", ve.Field, tc.field, err)
			}
		})
	}
}

func TestValidateAllowsLunchOffOutsideTheDay(t *testing.T) {
	b := profile()
	b.LunchEnabled = false
	b.LunchEarliest, b.LunchLatest = 60, 120
	if err := b.Validate(); err != nil {
		t.Fatalf("a disabled break should not be range-checked against the workday: %v", err)
	}
}

func TestApplyOnlyTouchesSuppliedFields(t *testing.T) {
	b := profile()
	enabled := false
	limit := 12
	got := models.UpdateSendingBehavior{Enabled: &enabled, DailyLimitMin: &limit}.Apply(b)

	if got.Enabled {
		t.Fatal("enabled was not applied")
	}
	if got.DailyLimitMin != 12 {
		t.Fatalf("daily_limit_min = %d, want 12", got.DailyLimitMin)
	}
	if got.DailyLimitMax != b.DailyLimitMax || got.GapMinSeconds != b.GapMinSeconds || got.Weekdays != b.Weekdays {
		t.Fatalf("untouched fields changed: %+v", got)
	}
}

func asValidationError(err error, target **models.BehaviorValidationError) bool {
	ve, ok := err.(*models.BehaviorValidationError)
	if ok {
		*target = ve
	}
	return ok
}
