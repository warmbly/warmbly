package scheduler

import (
	"context"
	"math/rand"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

// poolTypesForHealthLookup lists the pools a participant could be in, in
// priority order. Premium first so paid orgs apply premium-pool health.
var poolTypesForHealthLookup = []string{"premium", "free"}

const (
	minWarmupRecipientRecheck = 4 * time.Hour
	maxWarmupRecipientRecheck = 8 * time.Hour
	activeCampaignWarmupCap   = 5
)

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

func warmupPoolTypeForAccount(account *models.Email) string {
	if account != nil && account.WarmupPoolType != "" {
		return account.WarmupPoolType
	}
	return "premium"
}

func recipientRecheckTime() time.Time {
	return time.Now().Add(time.Duration(randomJitter(
		int(minWarmupRecipientRecheck/time.Minute),
		int(maxWarmupRecipientRecheck/time.Minute),
	)) * time.Minute)
}

// warmupRampTarget computes the day's warmup volume before recipient-capacity
// and health-state adjustments. An actively-warming mailbox follows its ramp
// (base + daysWarming*increase, capped at max), reduced to the in-campaign cap
// when it also backs a live campaign so warmup doesn't stack on production
// sending pressure. A mailbox kept warm only because it backs a campaign (the
// monitor lane) runs at the in-campaign cap. Pure (no DB) so the policy is
// unit-testable.
func warmupRampTarget(activelyWarming bool, base, increase, max, daysWarming int, inCampaign bool) int {
	if !activelyWarming {
		return activeCampaignWarmupCap
	}
	target := min(base+daysWarming*increase, max)
	if inCampaign && target > activeCampaignWarmupCap {
		target = activeCampaignWarmupCap
	}
	return target
}

// dailyVolumeFactor returns a stable-per-day multiplier (0.75–1.10, with an
// occasional lighter day near 0.6) for a mailbox's warmup volume. Real senders
// don't send exactly N every day; a flat daily count is a pattern. Deterministic
// on (accountID, localDate) so the scheduler's many intra-day recomputations
// don't thrash the target.
func dailyVolumeFactor(accountID uuid.UUID, day time.Time) float64 {
	y, m, d := day.Date()
	var seed uint64 = 1469598103934665603 // FNV-1a offset basis
	mix := func(b []byte) {
		for _, c := range b {
			seed ^= uint64(c)
			seed *= 1099511628211
		}
	}
	id := accountID
	mix(id[:])
	mix([]byte{byte(y), byte(y >> 8), byte(m), byte(d)})

	u := float64(seed%1000) / 1000.0 // stable [0,1)
	if u < 0.15 {
		return 0.60 // ~15% of days are lighter
	}
	return 0.75 + u*0.35 // 0.75–1.10
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

// accountInActiveCampaign reports whether the mailbox currently backs at
// least one active campaign (matched through the campaign's email tags).
// Failing closed on error keeps warmup behaviour conservative: a transient
// DB error simply means no in-campaign health-check floor this cycle.
func (s *schedulerService) accountInActiveCampaign(ctx context.Context, accountID uuid.UUID) bool {
	if s.campaignRepo == nil {
		return false
	}
	count, err := s.campaignRepo.CountActiveCampaignsForAccount(ctx, accountID)
	if err != nil {
		return false
	}
	return count > 0
}

// CalculateNextWarmupTime calculates the next best time to send a warmup email
// This implements the progressive warmup algorithm with anti-spam patterns
func (s *schedulerService) CalculateNextWarmupTime(ctx context.Context, accountID uuid.UUID) (time.Time, error) {
	// STEP 1: Load email account details
	account, xerr := s.emailRepo.GetByID(ctx, accountID)
	if xerr != nil {
		return time.Time{}, xerr
	}

	// Two reasons a mailbox warms up:
	//   1. The user has warmup actively enabled — follow the normal ramp.
	//   2. The mailbox backs a live campaign — keep a small health-check
	//      volume flowing even when warmup is paused/off so reputation
	//      signals stay fresh while it sends cold outreach.
	activelyWarming := account.IsWarmingActive()
	inCampaign := s.accountInActiveCampaign(ctx, accountID)
	if !activelyWarming && !inCampaign {
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
			return humanizeSeconds(firstSlot.Add(time.Minute * time.Duration(jitter))), nil
		}
	}

	// STEP 2: Calculate target volume for today (before recipient-capacity and
	// health adjustments). Pure policy in warmupRampTarget so it's unit-tested.
	daysWarming := 0
	if activelyWarming {
		daysWarming = int(time.Since(*account.Warmup).Hours() / 24)
	}
	targetVolume := warmupRampTarget(activelyWarming, account.WarmupBase, account.WarmupIncrease, account.WarmupMax, daysWarming, inCampaign)

	// Vary the day's target so a mailbox doesn't send an identical count every
	// day. Deterministic per (account, local day) so it's stable across the
	// day's reschedules. Actively-warming mailboxes keep a floor of WarmupBase.
	if activelyWarming && targetVolume > 0 {
		factor := dailyVolumeFactor(accountID, time.Now().In(loadLocation(account.Timezone)))
		varied := int(float64(targetVolume)*factor + 0.5)
		if varied < account.WarmupBase {
			varied = account.WarmupBase
		}
		if varied < 1 {
			varied = 1
		}
		if varied < targetVolume {
			targetVolume = varied
		}
	}

	// STEP 2.1: Cap per-mailbox volume to actual recipient capacity. The
	// sender should not send multiple warmup messages to the same recipient
	// in a single day just to hit an arbitrary target; that creates obvious
	// pool loops when membership is small. Recipient-only participants count
	// here, so operators can add inbound capacity without making those
	// mailboxes warmup senders.
	if s.warmupRepo != nil {
		eligibleRecipients, err := s.warmupRepo.CountEligibleRecipients(ctx, warmupPoolTypeForAccount(account), accountID)
		if err == nil {
			if eligibleRecipients <= 0 {
				return recipientRecheckTime(), nil
			}
			if targetVolume > eligibleRecipients {
				targetVolume = eligibleRecipients
			}
		}
	}

	// STEP 2.5: Apply health-state adjustments. Throttled/watch participants
	// run at reduced volume and wider spacing until the health sweep clears
	// them back to healthy. We never zero out volume — even degraded mailboxes
	// keep a small heartbeat so the sweep has fresh sample data to evaluate.
	adj := adjustmentFor(s.resolveHealthState(ctx, accountID))
	if adj.volumeMultiplier < 1.0 {
		floor := 1
		if activelyWarming {
			floor = account.WarmupBase
		}
		adjusted := int(float64(targetVolume)*adj.volumeMultiplier + 0.5)
		if adjusted < floor {
			adjusted = floor
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
	emailsSentToday, err := s.taskRepo.CountWarmupEmailsSentToday(ctx, accountID)
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

	// STEP 7: Add ideal interval to last email time. The interval is varied
	// multiplicatively (0.55x–1.45x) so the day reads as bursts and lulls
	// rather than a metronome: perfectly even spacing is its own signature
	// even with additive jitter on top.
	if lastEmailTime != nil && idealIntervalHours > 0 {
		varied := idealIntervalHours * (0.55 + rand.Float64()*0.9)
		idealNext := lastEmailTime.Add(time.Duration(varied * float64(time.Hour)))
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

	// Randomise the sub-minute component so warmup sends never land on :00.
	return humanizeSeconds(candidateTime), nil
}
