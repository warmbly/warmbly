package scheduler

import (
	"math"
	"math/rand"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/behavior"
	"github.com/warmbly/warmbly/internal/bitmask"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/mailhost"
	"github.com/warmbly/warmbly/internal/repository"
)

// findNextValidDay finds the next day the Monday-first days mask allows.
func findNextValidDay(from time.Time, daysBitmask uint8, tz *time.Location) time.Time {
	if daysBitmask == 0 {
		// If no days specified, allow all days
		return from
	}

	candidate := from.In(tz)

	// Try up to 7 days
	for i := 0; i < 7; i++ {
		if bitmask.HasWeekday(daysBitmask, candidate.Weekday()) {
			return candidate
		}
		candidate = candidate.Add(24 * time.Hour)
	}

	// If no valid day found in 7 days, just return the input
	return from
}

// ensureTimeWindow ensures time is within the allowed window (start_time to end_time)
func ensureTimeWindow(t time.Time, startTime, endTime string, tz *time.Location) time.Time {
	start := parseTimeOfDay(startTime) // Minutes since midnight
	end := parseTimeOfDay(endTime)

	if start == 0 && end == 0 {
		// No time window specified, allow any time
		return t
	}

	tLocal := t.In(tz)
	minutesOfDay := tLocal.Hour()*60 + tLocal.Minute()

	if minutesOfDay < start {
		// Too early, move to start time today
		return time.Date(tLocal.Year(), tLocal.Month(), tLocal.Day(),
			start/60, start%60, 0, 0, tz)
	}

	if minutesOfDay > end {
		// Too late, move to tomorrow's start time
		next := tLocal.Add(24 * time.Hour)
		return time.Date(next.Year(), next.Month(), next.Day(),
			start/60, start%60, 0, 0, tz)
	}

	return t
}

// effectiveWindows returns the campaign's authoritative per-day sending
// schedule. When ScheduleWindows is set it is used as-is; otherwise it is
// DERIVED from the legacy days bitmask + start/end time (one interval per active
// day) so campaigns created before the multi-window feature still schedule
// correctly. The stored days bitmask is Monday-indexed (bit 0 = Monday, as the
// bitmask package and dashboard write it), so it is mapped to time.Weekday
// (Sun=0) via (bit+1)%7 — which also corrects the historical off-by-one in the
// legacy day check.
func effectiveWindows(c *models.Campaign) models.ScheduleWindows {
	if !c.ScheduleWindows.IsEmpty() {
		return c.ScheduleWindows
	}
	start := parseTimeOfDay(c.StartTime)
	end := parseTimeOfDay(c.EndTime)
	if end <= start {
		// No usable legacy window → unconstrained (any time allowed).
		return models.ScheduleWindows{}
	}
	var sw models.ScheduleWindows
	for bit := 0; bit < 7; bit++ {
		if c.Days == 0 || c.Days&(1<<uint(bit)) != 0 {
			wd := (bit + 1) % 7 // Monday-indexed bit → time.Weekday
			sw[wd] = []models.TimeInterval{{Start: start, End: end}}
		}
	}
	return sw
}

// nextScheduleSlot returns the earliest time >= from that falls inside one of
// the campaign's per-day sending windows, searching up to 8 days ahead. An
// empty schedule means "unconstrained" and returns from unchanged. When from is
// already inside a window its exact instant is preserved (so jitter/min-wait
// adjustments survive); otherwise it advances to the next interval's start.
func nextScheduleSlot(from time.Time, sw models.ScheduleWindows, tz *time.Location) time.Time {
	if sw.IsEmpty() {
		return from
	}
	cur := from.In(tz)
	for i := 0; i < 8; i++ {
		y, m, d := cur.Date()
		nowMin := cur.Hour()*60 + cur.Minute()

		ivs := append([]models.TimeInterval(nil), sw[int(cur.Weekday())]...)
		sort.Slice(ivs, func(a, b int) bool { return ivs[a].Start < ivs[b].Start })

		for _, iv := range ivs {
			if nowMin < iv.Start {
				return time.Date(y, m, d, iv.Start/60, iv.Start%60, 0, 0, tz)
			}
			if nowMin < iv.End {
				if i == 0 {
					return from // already inside a window — keep the exact instant
				}
				return cur
			}
		}
		// No interval left today — jump to the start of the next day.
		cur = time.Date(y, m, d, 0, 0, 0, 0, tz).Add(24 * time.Hour)
	}
	return from
}

// ensureBusinessHours ensures time is within business hours (8am-8pm)
func ensureBusinessHours(t time.Time, timezone string) time.Time {
	loc := loadLocation(timezone)
	return ensureTimeWindow(t, "08:00", "20:00", loc)
}

// businessHoursReopen is when the 8am-8pm band next opens for an instant
// that sits outside it: 8am the same local day before the band, 8am the next
// local day after it.
func businessHoursReopen(t time.Time, loc *time.Location) time.Time {
	local := t.In(loc)
	open := time.Date(local.Year(), local.Month(), local.Day(), 8, 0, 0, 0, loc)
	if local.Hour() >= 20 {
		open = open.AddDate(0, 0, 1)
	}
	return open
}

// calculateHoursRemainingUntil calculates hours remaining until a specific end time
func calculateHoursRemainingUntil(timezone, endTime string) float64 {
	loc := loadLocation(timezone)
	now := time.Now().In(loc)
	endMinutes := parseTimeOfDay(endTime)
	if endMinutes == 0 {
		endMinutes = 20 * 60 // fallback to 8pm
	}
	endOfDay := time.Date(now.Year(), now.Month(), now.Day(), endMinutes/60, endMinutes%60, 0, 0, loc)
	if now.After(endOfDay) {
		return 0
	}
	return max(0, endOfDay.Sub(now).Hours())
}

// calculateFirstSlotTomorrowAt calculates first slot tomorrow at a specific start time
func calculateFirstSlotTomorrowAt(timezone, startTime string) time.Time {
	loc := loadLocation(timezone)
	now := time.Now().In(loc)
	startMinutes := parseTimeOfDay(startTime)
	if startMinutes == 0 {
		startMinutes = 8 * 60 // fallback to 8am
	}
	tomorrow := now.Add(24 * time.Hour)
	firstSlot := time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(),
		startMinutes/60, startMinutes%60, 0, 0, loc)
	jitter := randomJitter(0, 60)
	return finalSlot(firstSlot.Add(time.Minute * time.Duration(jitter)))
}

// humanizeSeconds moves a slot sitting on second :00 (every time.Date build and
// minute-granular jitter) 1-59s later, so the fleet never sends at second zero.
// A slot already off :00 keeps its exact time and the pacing that produced it.
func humanizeSeconds(t time.Time) time.Time {
	if t.Second() != 0 {
		return t
	}
	return t.Add(time.Duration(1+rand.Intn(59)) * time.Second)
}

// poolSendInterval is a campaign chain's gap to its next send: the window left
// over the pool's budget left, varied 0.55-1.45x by draw so sends come in bursts
// and lulls. The floor is the pool's spacing (one mailbox gap over the pool
// size), never the chosen mailbox's, whose own gap STEP 10 enforces.
func poolSendInterval(remainingMinutes, remainingEmails, gapSeconds, poolSize int, draw float64) time.Duration {
	perSend := float64(remainingMinutes) / float64(max(1, remainingEmails))
	interval := time.Duration(perSend * (0.55 + draw*0.9) * float64(time.Minute))
	floor := time.Second * time.Duration(gapSeconds) / time.Duration(max(1, poolSize))
	return max(interval, floor)
}

// finalSlot is the last thing a scheduler does to a candidate: take it off
// second :00, then clamp a slot the jitter left in the past to the near future.
func finalSlot(t time.Time) time.Time {
	return notBefore(humanizeSeconds(t))
}

// avoidRoundTimes adds randomness to avoid exact round times (10:00, 11:00)
func avoidRoundTimes(t time.Time) time.Time {
	if t.Minute() == 0 {
		// Move to random minute between 3-12
		offset := randomJitter(3, 12)
		return t.Add(time.Minute * time.Duration(offset))
	}
	return t
}

// applyDistributionCurve applies human-like distribution patterns
// Favors morning (9-11am) and afternoon (2-4pm) peaks
func applyDistributionCurve(t time.Time, tz *time.Location) time.Time {
	hour := t.In(tz).Hour()

	// Avoid lunch hour (12-1pm) - 30% chance to push to 1:15pm
	if hour == 12 {
		if rand.Float64() < 0.3 {
			minutes := 75 + randomJitter(0, 30)
			return t.Add(time.Minute * time.Duration(minutes))
		}
	}

	// Slightly avoid very early (before 9am) and very late (after 6pm)
	// Add small random delays to push toward peak hours
	if hour < 9 {
		// Small chance to push to 9am
		if rand.Float64() < 0.2 {
			target := time.Date(t.Year(), t.Month(), t.Day(), 9, 0, 0, 0, tz)
			if target.After(t) {
				offset := randomJitter(0, 30)
				return target.Add(time.Minute * time.Duration(offset))
			}
		}
	}

	return t
}

// resolveConflicts resolves scheduling conflicts with existing tasks
// Ensures minimum spacing between emails from the same account
func resolveConflicts(desired time.Time, scheduled []repository.Task, minWait int) time.Time {
	if len(scheduled) == 0 {
		return desired
	}

	// Sort tasks by scheduled time
	sort.Slice(scheduled, func(i, j int) bool {
		if scheduled[i].ScheduledAt == nil || scheduled[j].ScheduledAt == nil {
			return false
		}
		return scheduled[i].ScheduledAt.Before(*scheduled[j].ScheduledAt)
	})

	candidate := desired
	maxAttempts := 100

	for attempt := 0; attempt < maxAttempts; attempt++ {
		hasConflict := false

		for _, task := range scheduled {
			if task.ScheduledAt == nil {
				continue
			}

			diff := math.Abs(candidate.Sub(*task.ScheduledAt).Seconds())

			if diff < float64(minWait) {
				// Conflict! Move candidate after this task
				hasConflict = true
				candidate = task.ScheduledAt.Add(time.Second * time.Duration(minWait))

				// Add small random jitter to avoid creating a new conflict
				jitterMinutes := randomJitter(1, 5)
				candidate = candidate.Add(time.Minute * time.Duration(jitterMinutes))
				break
			}
		}

		if !hasConflict {
			return candidate
		}
	}

	// If still conflicts after 100 attempts, push to next hour
	return candidate.Add(time.Hour)
}

// AccountCandidate holds an email account with its computed scheduling weight
type AccountCandidate struct {
	Account        models.Email
	RemainingToday int
	WarmupAgeDays  int
	Weight         float64

	// Per-sender rotation metadata (explicit sender strategy only). SenderWeight
	// multiplies the base Weight in weighted mode; RotationPosition drives
	// round_robin; SenderLastSentAt drives least_recently_used. Defaults
	// (weight 1, nil last-sent, position 0) make tag-strategy candidates behave
	// exactly as before.
	SenderWeight      int
	RotationPosition  int
	SenderLastSentAt  *time.Time
	HasSenderMetadata bool

	// ProviderMatch reflects whether this mailbox's provider matches the
	// recipient ESP under ESP matching. Always true when ESP matching is off or
	// the recipient provider is unknown.
	ProviderMatch bool

	// Behavior is the mailbox's resolved sending-behaviour profile for this
	// pass. Zero value (Enabled false) means the mailbox has not opted in.
	Behavior behavior.Resolved
	// OpenAt is the earliest instant the mailbox's own calendar allows: its
	// rolled workday and hourly ceiling under a behaviour profile, otherwise
	// the reopening of its 8am-8pm band, and it is only set for a mailbox the
	// pass decided to wait for rather than drop. OpenLoc is the timezone that
	// instant's DAY is counted in. Both nil when the mailbox can send now.
	OpenAt  *time.Time
	OpenLoc *time.Location
}

// remainingSendMinutes returns how much sending time is left in the day the
// candidate is about to send on, which is what the even-distribution step
// paces the day's remaining emails across.
//
// With a behaviour profile the answer comes from the mailbox's own rolled
// workday in its own timezone, with any part of the lunch break still ahead
// subtracted — pacing across a window that includes a break the mailbox will
// not send in would bunch the remainder into the afternoon. Without one it is
// the campaign's window span for that weekday, exactly as before.
func remainingSendMinutes(c *AccountCandidate, at time.Time, sw models.ScheduleWindows, campaignTZ *time.Location) (int, bool) {
	if c.Behavior.Enabled {
		loc := c.Behavior.Loc
		plan := c.Behavior.PlanOn(behavior.PlanDateFor(at, loc))
		if !plan.IsWorkingDay {
			return 0, false
		}
		cur := max(behavior.MinuteOfDay(at, loc), plan.WorkStartMinute)
		if cur >= plan.WorkEndMinute {
			return 0, false
		}
		remaining := plan.WorkEndMinute - cur
		if plan.HasLunch() {
			breakStart, breakEnd := *plan.LunchStartMinute, *plan.LunchEndMinute
			if breakEnd > cur {
				remaining -= breakEnd - max(breakStart, cur)
			}
		}
		return max(remaining, 0), true
	}

	wd := int(at.In(campaignTZ).Weekday())
	dayStart, dayEnd, ok := sw.DaySpan(wd)
	if !ok {
		return 0, false
	}
	nowLocal := time.Now().In(campaignTZ)
	currentMinutes := nowLocal.Hour()*60 + nowLocal.Minute()
	return dayEnd - max(currentMinutes, dayStart), true
}

// poolRemainingOn is how many cold sends the campaign's eligible mailboxes still
// have between them on the day `at` falls on — the denominator the even-
// distribution step paces the day across.
//
// Only mailboxes actually landing on that day are counted. A mailbox whose
// today is spent (or whose hours have closed) has already been walked to a
// later day, and counting tomorrow's allowance as if it were available now
// would pace the campaign faster than the mailboxes that can send today can
// keep up with. A mailbox that opened BEFORE `at` is available whatever day it
// opened on, which is what keeps the count right when the whole pass has been
// pushed to tomorrow because every mailbox was at capacity today.
func poolRemainingOn(pool []AccountCandidate, at time.Time) int {
	total := 0
	for i := range pool {
		c := &pool[i]
		if c.RemainingToday <= 0 {
			continue
		}
		if c.OpenAt != nil && c.OpenLoc != nil &&
			c.OpenAt.After(at) && !sameLocalDay(*c.OpenAt, at, c.OpenLoc) {
			continue
		}
		total += c.RemainingToday
	}
	return total
}

// campaignRampCeiling returns the day's effective ramp ceiling. When ramp is
// disabled it returns the campaign ceiling unchanged (the caller still min()s
// against the per-mailbox cap). When enabled it returns the already-advanced
// level clamped into [start, ceiling]. The scheduler applies this ONLY via
// min() against the per-mailbox cold cap, so it can only LOWER volume.
func campaignRampCeiling(enabled bool, start, increment, ceiling, level int) int {
	_ = increment // the increment is applied by AdvanceRampLevel; level is already advanced
	if !enabled {
		return ceiling
	}
	v := level
	if v < start {
		v = start
	}
	if v > ceiling {
		v = ceiling
	}
	return v
}

// senderESP is a mailbox's family for ESP matching: its email_provider, or
// for an smtp_imap mailbox on Google or Microsoft (an app-password import)
// that host's family.
func senderESP(acct models.Email) string {
	if acct.Provider == "smtp_imap" {
		if esp := mailhost.ESPFamily(mailhost.Host(acct.MailHost)); esp == "gmail" || esp == "outlook" {
			return esp
		}
	}
	return acct.Provider
}

// recipientESP is the provider family ESP matching compares against a
// mailbox's email_provider: "gmail", "outlook", or "" for anything else,
// which matches every mailbox. A contact the provider sweep has not reached
// yet is read from its domain alone.
func recipientESP(c *models.Contact) string {
	esp := c.ESPProvider
	if c.MailHost == "" && esp == "" {
		esp = mailhost.ESPFamily(mailhost.KnownDomain(c.Email))
	}
	if esp == "gmail" || esp == "outlook" {
		return esp
	}
	return ""
}

// computeWeight calculates a scheduling weight for an account based on remaining capacity and warmup age.
// Accounts with more remaining capacity and older warmup age get higher weight.
func computeWeight(remaining int, warmupAgeDays int) float64 {
	if remaining <= 0 {
		return 0
	}
	warmupFactor := 1.0 + math.Log2(float64(warmupAgeDays+1))
	return float64(remaining) * warmupFactor
}

// selectAccountWeighted picks an account using weighted random selection.
// Returns nil if all candidates have zero weight. The per-sender weight (1..100,
// default 1 for tag-strategy candidates) multiplies the base scheduling weight,
// so an operator can bias rotation toward specific mailboxes without ever
// raising any mailbox above its already-clamped per-mailbox cap.
func selectAccountWeighted(candidates []AccountCandidate) *AccountCandidate {
	var totalWeight float64
	var viable []AccountCandidate
	for _, c := range candidates {
		w := effectiveWeight(c)
		if w > 0 {
			totalWeight += w
			c.Weight = w
			viable = append(viable, c)
		}
	}

	if len(viable) == 0 {
		return nil
	}

	r := rand.Float64() * totalWeight
	var cumulative float64
	for i := range viable {
		cumulative += viable[i].Weight
		if r <= cumulative {
			return &viable[i]
		}
	}

	// Fallback to last viable candidate
	return &viable[len(viable)-1]
}

// effectiveWeight folds the per-sender weight into the base scheduling weight.
// Tag-strategy candidates carry SenderWeight 0 (no metadata) and so use the
// base weight unchanged.
func effectiveWeight(c AccountCandidate) float64 {
	if c.HasSenderMetadata && c.SenderWeight > 0 {
		return c.Weight * float64(c.SenderWeight)
	}
	return c.Weight
}

// selectAccountLeastRecentlyUsed picks the viable candidate (Weight>0) whose
// sender last_sent_at is oldest; a nil last_sent_at (never used) sorts first.
// Ties broken by account id for determinism.
func selectAccountLeastRecentlyUsed(candidates []AccountCandidate) *AccountCandidate {
	var best *AccountCandidate
	for i := range candidates {
		if candidates[i].Weight <= 0 {
			continue
		}
		if best == nil || lruLess(&candidates[i], best) {
			best = &candidates[i]
		}
	}
	return best
}

func lruLess(a, b *AccountCandidate) bool {
	switch {
	case a.SenderLastSentAt == nil && b.SenderLastSentAt == nil:
		return a.Account.ID.String() < b.Account.ID.String()
	case a.SenderLastSentAt == nil:
		return true
	case b.SenderLastSentAt == nil:
		return false
	case a.SenderLastSentAt.Equal(*b.SenderLastSentAt):
		return a.Account.ID.String() < b.Account.ID.String()
	default:
		return a.SenderLastSentAt.Before(*b.SenderLastSentAt)
	}
}

// selectAccountRoundRobin picks the viable candidate (Weight>0) with the lowest
// rotation_position; ties broken by account id for determinism.
func selectAccountRoundRobin(candidates []AccountCandidate) *AccountCandidate {
	var best *AccountCandidate
	for i := range candidates {
		if candidates[i].Weight <= 0 {
			continue
		}
		if best == nil ||
			candidates[i].RotationPosition < best.RotationPosition ||
			(candidates[i].RotationPosition == best.RotationPosition &&
				candidates[i].Account.ID.String() < best.Account.ID.String()) {
			best = &candidates[i]
		}
	}
	return best
}

// needsRotationFallback reports whether a rotation mode depends on per-sender
// bookkeeping that only explicitly-picked senders carry. Weighted selection is
// driven by remaining capacity, so it already spreads without a cursor.
func needsRotationFallback(rotationMode string) bool {
	return rotationMode == "round_robin" || rotationMode == "least_recently_used"
}

// selectAccountByRotationMode dispatches to the chosen rotation strategy. For
// explicit-strategy campaigns rotationMode is one of weighted / round_robin /
// least_recently_used; anything else falls back to weighted.
func selectAccountByRotationMode(rotationMode string, candidates []AccountCandidate) *AccountCandidate {
	switch rotationMode {
	case "round_robin":
		return selectAccountRoundRobin(candidates)
	case "least_recently_used":
		return selectAccountLeastRecentlyUsed(candidates)
	default:
		return selectAccountWeighted(candidates)
	}
}

// withoutSender drops one mailbox from the eligible set for per-step sender
// rotation. Excluding the previous step's mailbox must never make a sendable
// step unsendable: a single-mailbox pool, or an ESP-strict match that only
// leaves that one mailbox, keeps the original set so the sequence still sends.
func withoutSender(candidates []AccountCandidate, id uuid.UUID) []AccountCandidate {
	filtered := make([]AccountCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Account.ID != id {
			filtered = append(filtered, candidate)
		}
	}
	if len(filtered) == 0 {
		return candidates
	}
	return filtered
}

// resolvePreviousSender chooses the mailbox per-step rotation must exclude for
// the next step: the last sender a delivered step actually used when the lead
// has one, otherwise the current binding (which may name a mailbox whose
// dispatch was rolled back). Rotation off means no exclusion at all.
func resolvePreviousSender(rotate bool, assigned, lastDelivered *uuid.UUID) *uuid.UUID {
	if !rotate {
		return nil
	}
	if lastDelivered != nil {
		return lastDelivered
	}
	return assigned
}
