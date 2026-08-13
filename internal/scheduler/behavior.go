package scheduler

import (
	"context"
	"math/rand"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/behavior"
	"github.com/warmbly/warmbly/internal/models"
)

// Sending-behaviour placement. These helpers are the only place the schedulers
// touch the behaviour engine, and every one of them is a no-op when the engine
// is unwired or the mailbox has its profile switched off — a mailbox that has
// not opted in schedules exactly as it did before this feature existed.
//
// The gates compose in one direction only: behaviour can DELAY a send or LOWER
// a budget, never bring one forward or raise one. That keeps the mailbox-first
// safety invariant intact, so a human-behaviour profile can never be used to
// push a mailbox past its cold cap.

// placementAttempts bounds the hour-by-hour walk in placeWithinBehavior. A
// working day is at most 24 hours of buckets and NextOpen already skips whole
// non-working days, so this only ever runs out on a pathological profile.
const placementAttempts = 48

func (s *schedulerService) behaviorFor(ctx context.Context, account *models.Email) behavior.Resolved {
	if s.behaviorSvc == nil || account == nil {
		return behavior.Resolved{}
	}
	return s.behaviorSvc.Resolve(ctx, account)
}

func (s *schedulerService) behaviorForAll(ctx context.Context, accounts []models.Email) map[uuid.UUID]behavior.Resolved {
	if s.behaviorSvc == nil {
		return map[uuid.UUID]behavior.Resolved{}
	}
	return s.behaviorSvc.ResolveMany(ctx, accounts)
}

// behaviorWindow snaps a time into the mailbox's rolled workday: past the
// start of the day, out of the lunch break, and onto the next working weekday
// when the day is over. It applies no volume gate, so it is what warmup uses —
// warmup has its own ramp and should follow the persona's hours without
// competing for the cold-send budget.
//
// Returns the input unchanged when behaviour is off, and (zero, false) only
// when the profile has no working days at all.
func behaviorWindow(r behavior.Resolved, desired time.Time) (time.Time, bool) {
	if !r.Enabled {
		return desired, true
	}
	at, _, ok := r.NextOpen(desired)
	if !ok {
		return time.Time{}, false
	}
	return at, true
}

// placeWithinBehavior is behaviorWindow plus the volume gates: it walks forward
// until it finds an instant that is inside the workday AND still has room in
// both the day's rolled cold budget and that clock hour's ceiling.
//
// The hourly ceiling is what stops a day's whole allowance landing in one
// burst; without it a mailbox with a 40/day plan and a 90s floor could empty
// the day before 11am and then sit silent, which is a more obvious pattern
// than sending too much.
func (s *schedulerService) placeWithinBehavior(ctx context.Context, r behavior.Resolved, desired time.Time) (time.Time, bool) {
	if !r.Enabled || s.behaviorSvc == nil {
		return desired, true
	}

	at := desired
	for i := 0; i < placementAttempts; i++ {
		open, _, ok := r.NextOpen(at)
		if !ok {
			return time.Time{}, false
		}

		if s.behaviorSvc.RemainingToday(ctx, r, open) <= 0 {
			// This local day is spent. Restart the search at the first instant
			// of the next one rather than the next hour, so a full day costs
			// one iteration instead of twenty-four.
			at = behavior.PlanDateFor(open, r.Loc).AddDate(0, 0, 1)
			continue
		}

		if s.behaviorSvc.RemainingThisHour(ctx, r, open) > 0 {
			return open, true
		}

		_, hourEnd := behavior.HourWindow(open, r.Loc)
		at = hourEnd
	}
	return time.Time{}, false
}

// behaviorGap returns the spacing to leave after this mailbox's previous send,
// in seconds. With behaviour enabled the gap is drawn fresh from the profile's
// range for every send, so consecutive intervals differ instead of forming an
// arithmetic sequence; the configured min_wait_time is the fallback for every
// mailbox that has not opted in.
func (s *schedulerService) behaviorGap(r behavior.Resolved, at time.Time, fallbackSeconds int) int {
	if !r.Enabled {
		return fallbackSeconds
	}
	plan := r.PlanOn(behavior.PlanDateFor(at, r.Loc))
	gap := behavior.DrawGap(plan, rand.Float64)
	secs := int(gap / time.Second)
	if secs < 1 {
		return fallbackSeconds
	}
	return secs
}

// notBefore keeps a scheduled slot in the future.
//
// Every scheduler adds SYMMETRIC jitter (±20 minutes for campaigns) to a
// candidate that may already sit only seconds from now, so the negative half
// can push the slot into the past. A past scheduled_at fires the moment it is
// enqueued, which skips the spacing the send was placed with, and one more than
// 15 minutes stale is cancelled outright by the overdue sweep.
//
// The floor is now plus a few seconds of spread, so a batch of clamped tasks
// does not all land on the same instant.
func notBefore(candidate time.Time) time.Time {
	now := time.Now()
	if candidate.After(now) {
		return candidate
	}
	return now.Add(time.Duration(5+rand.Intn(55)) * time.Second)
}

// sameLocalDay reports whether two instants fall on the same calendar day in a
// mailbox's own timezone. Budgets are per LOCAL day, so this — not a UTC date
// comparison — is what decides whether a send is spending today's allowance.
func sameLocalDay(a, b time.Time, loc *time.Location) bool {
	if loc == nil {
		loc = time.UTC
	}
	ay, am, ad := a.In(loc).Date()
	by, bm, bd := b.In(loc).Date()
	return ay == by && am == bm && ad == bd
}

// intersectWindows walks a candidate time forward until it satisfies BOTH the
// campaign's sending windows (in the campaign timezone) and the mailbox's
// rolled workday (in the mailbox timezone), which are two different calendars
// whenever a campaign sends from mailboxes in other regions.
//
// Each pass only ever moves the candidate later, so the walk is monotonic and
// terminates; the bound is there for the case where the two calendars never
// overlap at all, in which case the campaign window wins and the behaviour
// engine simply does not delay the send further.
func (s *schedulerService) intersectWindows(
	ctx context.Context,
	r behavior.Resolved,
	t time.Time,
	sw models.ScheduleWindows,
	tz *time.Location,
) time.Time {
	if !r.Enabled || s.behaviorSvc == nil {
		return nextScheduleSlot(t, sw, tz)
	}
	for i := 0; i < 8; i++ {
		inCampaign := nextScheduleSlot(t, sw, tz)
		placed, ok := s.placeWithinBehavior(ctx, r, inCampaign)
		if !ok || !placed.After(inCampaign) {
			return inCampaign
		}
		t = placed
	}
	return t
}

// behaviorDailyCap folds the day's rolled cold budget into an existing
// remaining-capacity number. min() only: the plan can lower a mailbox's
// remaining sends for the day, never raise them above its cold cap.
func (s *schedulerService) behaviorDailyCap(ctx context.Context, r behavior.Resolved, remaining int, at time.Time) int {
	if !r.Enabled || s.behaviorSvc == nil {
		return remaining
	}
	return min(remaining, s.behaviorSvc.RemainingToday(ctx, r, at))
}
