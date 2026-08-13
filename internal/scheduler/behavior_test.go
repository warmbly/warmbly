package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/behavior"
	"github.com/warmbly/warmbly/internal/models"
)

// fakeBehavior answers the capacity questions from fixed numbers so the
// placement logic can be exercised without a database. Resolve/ResolveMany are
// unused by these tests; the schedulers call them, the placement helpers do not.
type fakeBehavior struct {
	remainingDaily  map[string]int
	remainingHourly map[string]int
	defaultDaily    int
	defaultHourly   int
}

func (f *fakeBehavior) Get(context.Context, uuid.UUID) (models.SendingBehavior, error) {
	return models.SendingBehavior{}, nil
}

func (f *fakeBehavior) Update(context.Context, uuid.UUID, models.UpdateSendingBehavior) (models.SendingBehavior, error) {
	return models.SendingBehavior{}, nil
}

func (f *fakeBehavior) Today(context.Context, uuid.UUID) (*models.DailyPlanView, error) {
	return nil, nil
}

func (f *fakeBehavior) Resolve(context.Context, *models.Email) behavior.Resolved {
	return behavior.Resolved{}
}

func (f *fakeBehavior) ResolveMany(context.Context, []models.Email) map[uuid.UUID]behavior.Resolved {
	return map[uuid.UUID]behavior.Resolved{}
}

func (f *fakeBehavior) RemainingToday(_ context.Context, r behavior.Resolved, at time.Time) int {
	if n, ok := f.remainingDaily[at.In(r.Loc).Format("2006-01-02")]; ok {
		return n
	}
	return f.defaultDaily
}

func (f *fakeBehavior) RemainingThisHour(_ context.Context, r behavior.Resolved, at time.Time) int {
	if n, ok := f.remainingHourly[at.In(r.Loc).Format("2006-01-02 15")]; ok {
		return n
	}
	return f.defaultHourly
}

func testProfile() models.SendingBehavior {
	b := models.DefaultSendingBehavior(uuid.MustParse("6f1b1c4e-2b7a-4a1e-9d0e-2f3a4b5c6d7e"))
	b.Enabled = true
	return b
}

// A Wednesday inside the default Mon-Fri mask.
func testWednesday() time.Time {
	return time.Date(2026, time.August, 12, 0, 0, 0, 0, time.UTC)
}

func atMinute(day time.Time, minute int) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day(), minute/60, minute%60, 0, 0, day.Location())
}

func minuteOf(t time.Time) int { return t.Hour()*60 + t.Minute() }

func TestPlaceWithinBehaviorIsANoOpWhenTheProfileIsOff(t *testing.T) {
	s := &schedulerService{behaviorSvc: &fakeBehavior{defaultDaily: 10, defaultHourly: 5}}
	want := atMinute(testWednesday(), 3*60)

	got, ok := s.placeWithinBehavior(context.Background(), behavior.Resolved{}, want)
	if !ok || !got.Equal(want) {
		t.Fatalf("a mailbox with no profile must not be moved: got %s ok=%v", got, ok)
	}
}

func TestPlaceWithinBehaviorSnapsToTheWorkday(t *testing.T) {
	s := &schedulerService{behaviorSvc: &fakeBehavior{defaultDaily: 10, defaultHourly: 5}}
	b := testProfile()
	r := behavior.NewStandalone(b, time.UTC)
	plan := behavior.RollPlan(b, testWednesday())

	got, ok := s.placeWithinBehavior(context.Background(), r, atMinute(testWednesday(), 5*60))
	if !ok {
		t.Fatal("expected a slot")
	}
	if minuteOf(got) != plan.WorkStartMinute {
		t.Fatalf("got %s, want the rolled start %d", got, plan.WorkStartMinute)
	}
}

func TestPlaceWithinBehaviorSkipsAFullHour(t *testing.T) {
	b := testProfile()
	r := behavior.NewStandalone(b, time.UTC)
	plan := behavior.RollPlan(b, testWednesday())
	start := atMinute(testWednesday(), plan.WorkStartMinute)

	f := &fakeBehavior{
		defaultDaily:    10,
		defaultHourly:   4,
		remainingHourly: map[string]int{start.Format("2006-01-02 15"): 0},
	}
	s := &schedulerService{behaviorSvc: f}

	got, ok := s.placeWithinBehavior(context.Background(), r, start)
	if !ok {
		t.Fatal("expected a slot")
	}
	if got.Hour() == start.Hour() {
		t.Fatalf("a full hour should be skipped, still landed at %s", got)
	}
	if !got.After(start) {
		t.Fatalf("placement moved backwards: %s -> %s", start, got)
	}
}

func TestPlaceWithinBehaviorSkipsAFullDay(t *testing.T) {
	b := testProfile()
	r := behavior.NewStandalone(b, time.UTC)
	wed := testWednesday()

	f := &fakeBehavior{
		defaultDaily:   10,
		defaultHourly:  5,
		remainingDaily: map[string]int{wed.Format("2006-01-02"): 0},
	}
	s := &schedulerService{behaviorSvc: f}

	got, ok := s.placeWithinBehavior(context.Background(), r, atMinute(wed, 10*60))
	if !ok {
		t.Fatal("expected a slot")
	}
	if got.Weekday() != time.Thursday {
		t.Fatalf("a spent day should roll to the next one, got %s", got.Weekday())
	}
}

func TestPlaceWithinBehaviorGivesUpWithNoWorkingDays(t *testing.T) {
	b := testProfile()
	b.Weekdays = 0
	s := &schedulerService{behaviorSvc: &fakeBehavior{defaultDaily: 10, defaultHourly: 5}}

	if _, ok := s.placeWithinBehavior(context.Background(), behavior.NewStandalone(b, time.UTC), testWednesday()); ok {
		t.Fatal("a profile with no working days has no slot")
	}
}

func TestBehaviorGapFallsBackToTheFixedMinWait(t *testing.T) {
	s := &schedulerService{}
	if got := s.behaviorGap(behavior.Resolved{}, time.Now(), 600); got != 600 {
		t.Fatalf("gap = %d, want the mailbox min_wait_time 600", got)
	}
}

func TestBehaviorGapUsesTheProfileRange(t *testing.T) {
	s := &schedulerService{}
	b := testProfile()
	r := behavior.NewStandalone(b, time.UTC)

	seen := map[int]bool{}
	for i := 0; i < 200; i++ {
		got := s.behaviorGap(r, testWednesday(), 600)
		if got < b.GapMinSeconds || got > b.GapMaxSeconds {
			t.Fatalf("gap %d outside [%d,%d]", got, b.GapMinSeconds, b.GapMaxSeconds)
		}
		seen[got] = true
	}
	if len(seen) < 10 {
		t.Fatalf("gaps barely vary across 200 draws: %d distinct", len(seen))
	}
}

func TestBehaviorDailyCapOnlyLowers(t *testing.T) {
	b := testProfile()
	r := behavior.NewStandalone(b, time.UTC)

	s := &schedulerService{behaviorSvc: &fakeBehavior{defaultDaily: 7, defaultHourly: 5}}
	if got := s.behaviorDailyCap(context.Background(), r, 50, testWednesday()); got != 7 {
		t.Fatalf("cap = %d, want the tighter plan budget 7", got)
	}

	s = &schedulerService{behaviorSvc: &fakeBehavior{defaultDaily: 900, defaultHourly: 5}}
	if got := s.behaviorDailyCap(context.Background(), r, 50, testWednesday()); got != 50 {
		t.Fatalf("cap = %d, a plan must never raise the cold cap above 50", got)
	}

	s = &schedulerService{behaviorSvc: &fakeBehavior{defaultDaily: 7, defaultHourly: 5}}
	if got := s.behaviorDailyCap(context.Background(), behavior.Resolved{}, 50, testWednesday()); got != 50 {
		t.Fatalf("cap = %d, a mailbox with no profile keeps its own 50", got)
	}
}

func TestSameLocalDayUsesTheMailboxTimezone(t *testing.T) {
	denver, err := time.LoadLocation("America/Denver")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	// Both instants land on 12 August in Denver, but on different UTC dates:
	// 01:00 UTC on the 13th is 19:00 on the 12th in Denver.
	evening := time.Date(2026, time.August, 13, 1, 0, 0, 0, time.UTC)
	morning := time.Date(2026, time.August, 12, 15, 0, 0, 0, time.UTC)

	if !sameLocalDay(evening, morning, denver) {
		t.Fatal("both instants fall on the same Denver day")
	}
	if sameLocalDay(evening, morning, time.UTC) {
		t.Fatal("they fall on different UTC days, which is exactly the bug this guards")
	}
}

func TestIntersectWindowsSatisfiesBothCalendars(t *testing.T) {
	b := testProfile()
	r := behavior.NewStandalone(b, time.UTC)
	plan := behavior.RollPlan(b, testWednesday())
	s := &schedulerService{behaviorSvc: &fakeBehavior{defaultDaily: 10, defaultHourly: 5}}

	// A campaign window that opens well after the mailbox's workday starts.
	var sw models.ScheduleWindows
	for wd := 0; wd < 7; wd++ {
		sw[wd] = []models.TimeInterval{{Start: 14 * 60, End: 16 * 60}}
	}

	got := s.intersectWindows(context.Background(), r, atMinute(testWednesday(), 6*60), sw, time.UTC)
	if m := minuteOf(got); m < 14*60 || m >= 16*60 {
		t.Fatalf("landed at %d, outside the campaign window", m)
	}
	if !plan.Contains(minuteOf(got)) {
		t.Fatalf("landed at %d, outside the mailbox workday %d-%d", minuteOf(got), plan.WorkStartMinute, plan.WorkEndMinute)
	}
}

func TestIntersectWindowsFallsBackToTheCampaignWindow(t *testing.T) {
	b := testProfile()
	r := behavior.NewStandalone(b, time.UTC)
	s := &schedulerService{behaviorSvc: &fakeBehavior{defaultDaily: 10, defaultHourly: 5}}

	// A campaign that only sends at night, which no default workday reaches.
	var sw models.ScheduleWindows
	for wd := 0; wd < 7; wd++ {
		sw[wd] = []models.TimeInterval{{Start: 1, End: 120}}
	}

	got := s.intersectWindows(context.Background(), r, atMinute(testWednesday(), 6*60), sw, time.UTC)
	if got.IsZero() {
		t.Fatal("a non-overlapping pair of calendars must still yield a time")
	}
	if m := minuteOf(got); m >= 120 && m < 1 {
		t.Fatalf("landed at %d, outside the campaign window", m)
	}
}

func TestIntersectWindowsIsPlainSnappingWithoutAProfile(t *testing.T) {
	s := &schedulerService{}
	var sw models.ScheduleWindows
	for wd := 0; wd < 7; wd++ {
		sw[wd] = []models.TimeInterval{{Start: 9 * 60, End: 17 * 60}}
	}

	from := atMinute(testWednesday(), 6*60)
	got := s.intersectWindows(context.Background(), behavior.Resolved{}, from, sw, time.UTC)
	if minuteOf(got) != 9*60 {
		t.Fatalf("got %d, want the campaign window start 540", minuteOf(got))
	}
}

func TestRemainingSendMinutesExcludesTheBreakAhead(t *testing.T) {
	b := testProfile()
	r := behavior.NewStandalone(b, time.UTC)
	plan := behavior.RollPlan(b, testWednesday())
	if !plan.HasLunch() {
		t.Skip("this day rolled without a break")
	}

	c := &AccountCandidate{Behavior: r}
	at := atMinute(testWednesday(), plan.WorkStartMinute)
	got, ok := remainingSendMinutes(c, at, models.ScheduleWindows{}, time.UTC)
	if !ok {
		t.Fatal("expected a window")
	}
	want := plan.WorkEndMinute - plan.WorkStartMinute - (*plan.LunchEndMinute - *plan.LunchStartMinute)
	if got != want {
		t.Fatalf("remaining = %d, want %d (the break must not be counted)", got, want)
	}
}

func TestRemainingSendMinutesAfterTheBreakCountsOnlyWhatIsLeft(t *testing.T) {
	b := testProfile()
	r := behavior.NewStandalone(b, time.UTC)
	plan := behavior.RollPlan(b, testWednesday())
	if !plan.HasLunch() {
		t.Skip("this day rolled without a break")
	}

	c := &AccountCandidate{Behavior: r}
	at := atMinute(testWednesday(), *plan.LunchEndMinute+30)
	got, ok := remainingSendMinutes(c, at, models.ScheduleWindows{}, time.UTC)
	if !ok {
		t.Fatal("expected a window")
	}
	if want := plan.WorkEndMinute - (*plan.LunchEndMinute + 30); got != want {
		t.Fatalf("remaining = %d, want %d", got, want)
	}
}

func TestRemainingSendMinutesIsClosedAfterHours(t *testing.T) {
	b := testProfile()
	r := behavior.NewStandalone(b, time.UTC)
	plan := behavior.RollPlan(b, testWednesday())

	c := &AccountCandidate{Behavior: r}
	at := atMinute(testWednesday(), plan.WorkEndMinute+5)
	if _, ok := remainingSendMinutes(c, at, models.ScheduleWindows{}, time.UTC); ok {
		t.Fatal("there is no sending time left after the workday ends")
	}
}

func TestRemainingSendMinutesFallsBackToTheCampaignWindow(t *testing.T) {
	c := &AccountCandidate{} // no profile
	var sw models.ScheduleWindows
	for wd := 0; wd < 7; wd++ {
		sw[wd] = []models.TimeInterval{{Start: 9 * 60, End: 17 * 60}}
	}
	if _, ok := remainingSendMinutes(c, testWednesday(), sw, time.UTC); !ok {
		t.Fatal("a campaign window should still be used when no profile exists")
	}
	if _, ok := remainingSendMinutes(c, testWednesday(), models.ScheduleWindows{}, time.UTC); ok {
		t.Fatal("no profile and no campaign window means no pacing window")
	}
}

/* ── rotation across mailboxes with no explicit sender row ─────────────── */

// Tag-resolved pools (and the "all active mailboxes" default) carry no
// campaign_senders row, so they have no stored cursor and no last_sent_at.
// These tests pin the fallback that keeps round-robin and least-recently-used
// from handing every send to the same mailbox.

func candidateWithoutSenderRow(id string, rotationPosition int, lastSent *time.Time) AccountCandidate {
	return AccountCandidate{
		Account:          models.Email{ID: uuid.MustParse(id)},
		RemainingToday:   10,
		Weight:           10,
		RotationPosition: rotationPosition,
		SenderLastSentAt: lastSent,
	}
}

func TestRoundRobinFallsBackToTodaysSendCount(t *testing.T) {
	// Three tag-resolved mailboxes. The lowest UUID has already sent the most
	// today, so a correct round-robin must NOT pick it.
	busiest := candidateWithoutSenderRow("11111111-1111-1111-1111-111111111111", 9, nil)
	middle := candidateWithoutSenderRow("22222222-2222-2222-2222-222222222222", 4, nil)
	idle := candidateWithoutSenderRow("33333333-3333-3333-3333-333333333333", 0, nil)

	got := selectAccountByRotationMode("round_robin", []AccountCandidate{busiest, middle, idle})
	if got == nil {
		t.Fatal("expected a selection")
	}
	if got.Account.ID != idle.Account.ID {
		t.Fatalf("picked %s, want the least-used mailbox %s", got.Account.ID, idle.Account.ID)
	}
}

func TestLeastRecentlyUsedFallsBackToRealSendHistory(t *testing.T) {
	recent := time.Now().Add(-5 * time.Minute)
	older := time.Now().Add(-3 * time.Hour)

	// Again the lowest UUID is the one that sent most recently.
	a := candidateWithoutSenderRow("11111111-1111-1111-1111-111111111111", 0, &recent)
	b := candidateWithoutSenderRow("22222222-2222-2222-2222-222222222222", 0, &older)

	got := selectAccountByRotationMode("least_recently_used", []AccountCandidate{a, b})
	if got == nil {
		t.Fatal("expected a selection")
	}
	if got.Account.ID != b.Account.ID {
		t.Fatalf("picked %s, want the longest-idle mailbox %s", got.Account.ID, b.Account.ID)
	}
}

func TestLeastRecentlyUsedPrefersAMailboxThatNeverSent(t *testing.T) {
	sent := time.Now().Add(-2 * time.Hour)
	used := candidateWithoutSenderRow("11111111-1111-1111-1111-111111111111", 0, &sent)
	fresh := candidateWithoutSenderRow("22222222-2222-2222-2222-222222222222", 0, nil)

	got := selectAccountByRotationMode("least_recently_used", []AccountCandidate{used, fresh})
	if got == nil || got.Account.ID != fresh.Account.ID {
		t.Fatalf("a mailbox with no send history should sort first, got %+v", got)
	}
}

func TestRotationRotatesAcrossRepeatedPicks(t *testing.T) {
	// The regression this guards: before the fallback, every pick returned the
	// same mailbox because all positions were 0 and the tie-break is by UUID.
	ids := []string{
		"11111111-1111-1111-1111-111111111111",
		"22222222-2222-2222-2222-222222222222",
		"33333333-3333-3333-3333-333333333333",
	}
	sentToday := map[string]int{ids[0]: 0, ids[1]: 0, ids[2]: 0}

	picked := map[string]int{}
	for i := 0; i < 9; i++ {
		pool := make([]AccountCandidate, 0, len(ids))
		for _, id := range ids {
			pool = append(pool, candidateWithoutSenderRow(id, sentToday[id], nil))
		}
		got := selectAccountByRotationMode("round_robin", pool)
		if got == nil {
			t.Fatal("expected a selection")
		}
		id := got.Account.ID.String()
		picked[id]++
		sentToday[id]++ // the send lands, so the next pass sees a higher count
	}

	if len(picked) != 3 {
		t.Fatalf("rotation stuck on %d of 3 mailboxes: %v", len(picked), picked)
	}
	for id, n := range picked {
		if n != 3 {
			t.Fatalf("mailbox %s took %d of 9 sends, want an even 3: %v", id, n, picked)
		}
	}
}

func TestNeedsRotationFallbackOnlyForCursorModes(t *testing.T) {
	for _, mode := range []string{"round_robin", "least_recently_used"} {
		if !needsRotationFallback(mode) {
			t.Fatalf("%s depends on per-sender bookkeeping", mode)
		}
	}
	// Weighted spreads by remaining capacity, so it needs no extra query.
	for _, mode := range []string{"weighted", ""} {
		if needsRotationFallback(mode) {
			t.Fatalf("%q should not trigger the extra lookup", mode)
		}
	}
}
