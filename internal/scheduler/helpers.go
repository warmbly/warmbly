package scheduler

import (
	"math"
	"math/rand"
	"sort"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// findNextValidDay finds the next valid day based on campaign days bitmask
// Bit 0 = Sunday, Bit 1 = Monday, ..., Bit 6 = Saturday
func findNextValidDay(from time.Time, daysBitmask uint8, tz *time.Location) time.Time {
	if daysBitmask == 0 {
		// If no days specified, allow all days
		return from
	}

	candidate := from.In(tz)

	// Try up to 7 days
	for i := 0; i < 7; i++ {
		dayOfWeek := int(candidate.Weekday())
		if (daysBitmask & (1 << dayOfWeek)) != 0 {
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
	return firstSlot.Add(time.Minute * time.Duration(jitter))
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

// providerForEmailDomain maps a recipient email address to a coarse ESP bucket
// for provider matching. Pure string work — never dials MX. Unknown/other
// domains return "" so matching never blocks the first contact.
func providerForEmailDomain(email string) string {
	at := strings.LastIndex(email, "@")
	if at < 0 || at == len(email)-1 {
		return ""
	}
	domain := strings.ToLower(strings.TrimSpace(email[at+1:]))
	switch domain {
	case "gmail.com", "googlemail.com":
		return "gmail"
	case "outlook.com", "hotmail.com", "live.com", "msn.com", "office365.com", "microsoft.com":
		return "outlook"
	}
	// Subdomain / suffix heuristics for hosted Google/Microsoft mail.
	if strings.HasSuffix(domain, ".onmicrosoft.com") {
		return "outlook"
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
