package scheduler

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

// poolTypesForHealthLookup lists the pools a participant could be in, in
// priority order. Premium first so paid orgs apply premium-pool health.
var poolTypesForHealthLookup = []string{"premium", "free"}

// healthAdjustment captures how the throttled/watch health state should
// dampen warmup throughput. The scheduler reads this off the participant's
// current state instead of carrying it on the task payload, so a state
// change takes effect on the next reschedule without any plumbing.
type healthAdjustment struct {
	volumeMultiplier  float64 // applied to target_volume
	minWaitMultiplier float64 // applied to MinWaitTime between sends
}

func adjustmentFor(state models.WarmupHealthState) healthAdjustment {
	switch state {
	case models.WarmupHealthThrottled:
		// Probation / spam-placement throttle band: cut volume in half and
		// double minimum spacing so the mailbox gets a much lighter day.
		return healthAdjustment{volumeMultiplier: 0.5, minWaitMultiplier: 2.0}
	case models.WarmupHealthWatch:
		// Watch band: lighter dampening — keep volume close to normal but
		// stretch spacing modestly so we are not bursting.
		return healthAdjustment{volumeMultiplier: 0.7, minWaitMultiplier: 1.5}
	default:
		return healthAdjustment{volumeMultiplier: 1.0, minWaitMultiplier: 1.0}
	}
}

// resolveHealthState looks up the participant's current health state across
// the warmup pools they may belong to. Returns Healthy if the account is not
// in any pool or the lookup fails — failing open keeps warmup running rather
// than silently halting on a transient DB error.
func (s *schedulerService) resolveHealthState(ctx context.Context, accountID uuid.UUID) models.WarmupHealthState {
	if s.warmupRepo == nil {
		return models.WarmupHealthHealthy
	}
	for _, poolType := range poolTypesForHealthLookup {
		health, err := s.warmupRepo.GetParticipantHealth(ctx, accountID, poolType)
		if err != nil || health == nil {
			continue
		}
		return health.HealthState
	}
	return models.WarmupHealthHealthy
}

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

	// STEP 2.5: Apply health-state adjustments. Throttled/watch participants
	// run at reduced volume and wider spacing until the health sweep clears
	// them back to healthy. We never zero out volume — even degraded mailboxes
	// keep a small heartbeat so the sweep has fresh sample data to evaluate.
	adj := adjustmentFor(s.resolveHealthState(ctx, accountID))
	if adj.volumeMultiplier < 1.0 {
		adjusted := int(float64(targetVolume)*adj.volumeMultiplier + 0.5)
		if adjusted < account.WarmupBase {
			adjusted = account.WarmupBase
		}
		if adjusted < 1 {
			adjusted = 1
		}
		targetVolume = adjusted
	}
	minWaitSeconds := account.MinWaitTime
	if adj.minWaitMultiplier > 1.0 {
		minWaitSeconds = int(float64(account.MinWaitTime)*adj.minWaitMultiplier + 0.5)
	}

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
		minWait := time.Second * time.Duration(minWaitSeconds)
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

	candidateTime = resolveConflicts(candidateTime, scheduledTasks, minWaitSeconds)

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
