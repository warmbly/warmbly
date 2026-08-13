// Package behavior turns a mailbox's sending-behaviour ranges into the one
// workday it actually runs on a given local date, and answers "when may this
// mailbox send next" for the schedulers.
//
// The roll is DETERMINISTIC on (mailbox id, local date). That is what makes the
// day stable: the campaign and warmup schedulers each recompute a mailbox's
// next slot many times a day, and a fresh random workday on every call would
// mean the mailbox never actually keeps to one. Persisting the plan is
// belt-and-braces on top of that — it gives the dashboard something to show and
// leaves an audit trail for "why did this send at 17:41" — but two racing
// writers would produce byte-identical rows anyway.
//
// Nothing here touches the database or the clock; RollPlan is a pure function
// of its arguments, so the whole policy is unit-testable.
package behavior

import (
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// stream is a small deterministic PRNG. Seeded by FNV-1a over the mailbox id
// and the local date, then advanced with splitmix64 so successive draws are
// independent rather than correlated slices of one hash.
type stream struct{ s uint64 }

func newStream(accountID uuid.UUID, y int, m time.Month, d int, salt string) *stream {
	const (
		offset64 uint64 = 14695981039346656037
		prime64  uint64 = 1099511628211
	)
	h := offset64
	mix := func(b []byte) {
		for _, c := range b {
			h ^= uint64(c)
			h *= prime64
		}
	}
	id := accountID
	mix(id[:])
	mix([]byte{
		byte(y), byte(y >> 8),
		byte(int(m)), byte(d),
	})
	mix([]byte(salt))
	return &stream{s: h}
}

func (s *stream) next() uint64 {
	s.s += 0x9E3779B97F4A7C15
	z := s.s
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

// between returns a value in [lo, hi] inclusive. Equal bounds return lo, so a
// customer who sets a range to a single value gets exactly that value.
func (s *stream) between(lo, hi int) int {
	if hi <= lo {
		return lo
	}
	return lo + int(s.next()%uint64(hi-lo+1))
}

// RollPlan produces the workday for `day` (interpreted in the mailbox's own
// timezone) from the mailbox's behaviour ranges.
//
// A day the profile does not work is still returned, with IsWorkingDay false
// and a zero limit, so callers get one shape to reason about and the dashboard
// can say "Saturday: not a sending day" rather than showing nothing.
func RollPlan(b models.SendingBehavior, day time.Time) models.DailyPlan {
	y, m, d := day.Date()
	rng := newStream(b.EmailAccountID, y, m, d, "plan/v1")

	plan := models.DailyPlan{
		EmailAccountID: b.EmailAccountID,
		PlanDate:       day.Format("2006-01-02"),
		Timezone:       day.Location().String(),
		IsWorkingDay:   b.WorksOn(day.Weekday()),
		GapMinSeconds:  b.GapMinSeconds,
		GapMaxSeconds:  b.GapMaxSeconds,
	}

	// Draw in a fixed order regardless of the branch taken, so switching a
	// mailbox from Mon-Fri to Mon-Sat does not reshuffle the hours it already
	// works on the days it already worked.
	dailyLimit := rng.between(b.DailyLimitMin, b.DailyLimitMax)
	hourlyLimit := rng.between(b.HourlyLimitMin, b.HourlyLimitMax)
	workStart := rng.between(b.WorkStartMin, b.WorkStartMax)
	workEnd := rng.between(b.WorkEndMin, b.WorkEndMax)
	lunchStart := rng.between(b.LunchEarliest, b.LunchLatest)
	lunchLen := rng.between(b.LunchMinMinutes, b.LunchMaxMinutes)

	plan.WorkStartMinute = workStart
	plan.WorkEndMinute = workEnd

	if !plan.IsWorkingDay {
		return plan
	}

	plan.DailyLimit = dailyLimit
	plan.HourlyLimit = hourlyLimit

	// The break only survives if it lands wholly inside the day and still
	// leaves sending time on both sides. Validation already rejects profiles
	// where that is impossible; this is the runtime guard for the edges a
	// particular roll can produce.
	if b.LunchEnabled && lunchLen > 0 {
		start, end := lunchStart, lunchStart+lunchLen
		if start > workStart && end < workEnd {
			plan.LunchStartMinute = &start
			plan.LunchEndMinute = &end
		}
	}

	return plan
}

// PlanDateFor returns the local calendar day a timestamp falls on for a
// mailbox, as the (location-bearing) midnight of that day. Callers pass this
// straight into RollPlan.
func PlanDateFor(t time.Time, loc *time.Location) time.Time {
	local := t.In(loc)
	y, m, d := local.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

// MinuteOfDay is the minutes-since-local-midnight of a timestamp.
func MinuteOfDay(t time.Time, loc *time.Location) int {
	local := t.In(loc)
	return local.Hour()*60 + local.Minute()
}

// AtMinute rebuilds a timestamp at a given minute of the plan's local day.
// Built through time.Date rather than by adding a duration to midnight so a
// day containing a DST transition still lands on the intended wall clock.
func AtMinute(day time.Time, minute int) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day(), minute/60, minute%60, 0, 0, day.Location())
}

// Planner supplies the plan for one local day. The schedulers pass a closure
// that reads today's persisted plan and rolls future days on the fly.
type Planner func(day time.Time) models.DailyPlan

// searchDays bounds how far ahead NextOpen will look. A profile that works at
// least one day a week always resolves inside a week; the extra days cover a
// start time that already sits late on the last working day.
const searchDays = 9

// NextOpen returns the first instant at or after `from` that falls inside the
// mailbox's working window, together with that day's plan.
//
// When `from` is already inside a window its exact instant is returned
// untouched, so jitter and spacing decided by the caller survive. Otherwise the
// result is the start of the next open minute: the workday start, the end of
// the lunch break, or the start of the next working day.
//
// ok is false only when no working day exists within the search horizon, which
// means the profile has no sending days at all.
func NextOpen(from time.Time, loc *time.Location, planner Planner) (time.Time, models.DailyPlan, bool) {
	cur := from.In(loc)
	for i := 0; i < searchDays; i++ {
		day := PlanDateFor(cur, loc)
		plan := planner(day)

		minute := 0
		if i == 0 {
			minute = MinuteOfDay(cur, loc)
		}
		if open, ok := plan.NextOpenMinute(minute); ok {
			if i == 0 && open == minute {
				return cur, plan, true
			}
			return AtMinute(day, open), plan, true
		}
		cur = day.AddDate(0, 0, 1)
	}
	return time.Time{}, models.DailyPlan{}, false
}

// HourWindow returns the local clock hour containing t, as the [start, end)
// instants used to count sends against the plan's hourly ceiling.
func HourWindow(t time.Time, loc *time.Location) (time.Time, time.Time) {
	local := t.In(loc)
	start := time.Date(local.Year(), local.Month(), local.Day(), local.Hour(), 0, 0, 0, loc)
	return start, start.Add(time.Hour)
}

// DrawGap returns one send-to-send spacing for a plan. Unlike the day's shape
// this is drawn fresh per send: the point is that consecutive intervals differ,
// so the mailbox's send times do not form an arithmetic sequence.
//
// The draw is triangular rather than uniform — biased toward the middle of the
// range — because a uniform draw produces suspiciously many intervals sitting
// exactly at the configured floor and ceiling.
func DrawGap(plan models.DailyPlan, rnd func() float64) time.Duration {
	lo, hi := plan.GapMinSeconds, plan.GapMaxSeconds
	if hi <= lo {
		return time.Duration(lo) * time.Second
	}
	u := (rnd() + rnd()) / 2
	secs := float64(lo) + u*float64(hi-lo)
	return time.Duration(secs * float64(time.Second))
}
