package scheduler

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// CalculateNextWarmupTime calculates the next best time to send a warmup email
// This implements the progressive warmup algorithm with anti-spam patterns
func (s *schedulerService) CalculateNextWarmupTime(ctx context.Context, accountID uuid.UUID) (time.Time, error) {
	// STEP 1: Load email account details
	account, xerr := s.emailRepo.GetByID(ctx, accountID)
	if xerr != nil {
		return time.Time{}, xerr
	}

	if account.Warmup == nil {
		return time.Time{}, ErrWarmupNotEnabled
	}

	// STEP 1.5: Ensure today is a valid warmup day
	if account.WarmupDays > 0 {
		loc := loadLocation(account.Timezone)
		now := time.Now().In(loc)
		candidateDay := findNextValidDay(now, uint8(account.WarmupDays), loc)
		if candidateDay.Day() != now.Day() || candidateDay.Month() != now.Month() || candidateDay.Year() != now.Year() {
			// Today is not a valid warmup day, schedule for next valid day's start time
			startMinutes := parseTimeOfDay(account.WarmupStartTime)
			if startMinutes == 0 {
				startMinutes = 8 * 60
			}
			nextDay := candidateDay.In(loc)
			firstSlot := time.Date(nextDay.Year(), nextDay.Month(), nextDay.Day(),
				startMinutes/60, startMinutes%60, 0, 0, loc)
			jitter := randomJitter(0, 60)
			return firstSlot.Add(time.Minute * time.Duration(jitter)), nil
		}
	}

	// STEP 2: Calculate target volume for today based on progression
	daysSinceStart := time.Since(*account.Warmup).Hours() / 24
	targetVolume := min(
		account.WarmupBase+int(daysSinceStart)*account.WarmupIncrease,
		account.WarmupMax,
	)

	// STEP 3: Count emails already sent today
	emailsSentToday, err := s.taskRepo.CountEmailsSentToday(ctx, accountID)
	if err != nil {
		return time.Time{}, err
	}

	// STEP 4: Check if we've hit today's limit
	if emailsSentToday >= targetVolume {
		// Move to tomorrow's first slot (8am-9am local time)
		return calculateFirstSlotTomorrowAt(account.Timezone, account.WarmupStartTime), nil
	}

	// STEP 5: Calculate ideal spacing
	// Distribute remaining emails across remaining business hours
	remainingSlots := targetVolume - emailsSentToday
	hoursRemaining := calculateHoursRemainingUntil(account.Timezone, account.WarmupEndTime)

	// If business hours are over, move to tomorrow
	if hoursRemaining <= 0 {
		return calculateFirstSlotTomorrowAt(account.Timezone, account.WarmupStartTime), nil
	}

	idealIntervalHours := hoursRemaining / float64(remainingSlots)

	// STEP 6: Get last email time and apply min_wait_time
	lastEmailTime, err := s.taskRepo.GetLastEmailTime(ctx, accountID)
	if err != nil {
		return time.Time{}, err
	}

	now := time.Now()
	earliestNext := now

	if lastEmailTime != nil {
		minWait := time.Second * time.Duration(account.MinWaitTime)
		earliestNext = lastEmailTime.Add(minWait)
	}

	// If earliestNext is in the past, use now
	if earliestNext.Before(now) {
		earliestNext = now
	}

	// STEP 7: Add ideal interval to last email time
	if lastEmailTime != nil && idealIntervalHours > 0 {
		idealNext := lastEmailTime.Add(time.Duration(idealIntervalHours * float64(time.Hour)))
		if idealNext.After(earliestNext) {
			earliestNext = idealNext
		}
	}

	// STEP 8: Add human-like jitter (±15 minutes)
	jitter := randomJitter(-15, 15)
	candidateTime := earliestNext.Add(time.Minute * time.Duration(jitter))

	// STEP 9: Avoid exact round times (10:00, 11:00)
	candidateTime = avoidRoundTimes(candidateTime)

	// STEP 10: Ensure within configured warmup time window
	candidateTime = ensureTimeWindow(candidateTime, account.WarmupStartTime, account.WarmupEndTime, loadLocation(account.Timezone))

	// STEP 11: Check conflicts with other tasks on this account
	scheduledTasks, err := s.taskRepo.GetScheduledTasksToday(ctx, accountID)
	if err != nil {
		return time.Time{}, err
	}

	candidateTime = resolveConflicts(candidateTime, scheduledTasks, account.MinWaitTime)

	// STEP 12: Apply human-like distribution curve
	loc := loadLocation(account.Timezone)
	candidateTime = applyDistributionCurve(candidateTime, loc)

	// STEP 13: Final day-of-week guard — conflict resolution or jitter may have pushed
	// the candidate into a day that isn't a valid warmup day.
	if account.WarmupDays > 0 {
		validDay := findNextValidDay(candidateTime, uint8(account.WarmupDays), loc)
		candidateLocal := candidateTime.In(loc)
		validLocal := validDay.In(loc)
		if candidateLocal.Day() != validLocal.Day() || candidateLocal.Month() != validLocal.Month() || candidateLocal.Year() != validLocal.Year() {
			startMinutes := parseTimeOfDay(account.WarmupStartTime)
			if startMinutes == 0 {
				startMinutes = 8 * 60
			}
			candidateTime = time.Date(validLocal.Year(), validLocal.Month(), validLocal.Day(),
				startMinutes/60, startMinutes%60, 0, 0, loc)
			jitter := randomJitter(0, 60)
			candidateTime = candidateTime.Add(time.Minute * time.Duration(jitter))
		}
	}

	return candidateTime, nil
}
