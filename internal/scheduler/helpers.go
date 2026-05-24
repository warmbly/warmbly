package scheduler

import (
	"math"
	"math/rand"
	"sort"
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
// Returns nil if all candidates have zero weight.
func selectAccountWeighted(candidates []AccountCandidate) *AccountCandidate {
	var totalWeight float64
	var viable []AccountCandidate
	for _, c := range candidates {
		if c.Weight > 0 {
			totalWeight += c.Weight
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
