package scheduler

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/behavior"
	"github.com/warmbly/warmbly/internal/app/warmupramp"
	"github.com/warmbly/warmbly/internal/models"
)

// poolTypesForHealthLookup lists the pools a participant could be in, in
// priority order. Premium first so paid orgs apply premium-pool health.
var poolTypesForHealthLookup = []string{"premium", "free"}

const (
	minWarmupRecipientRecheck = 4 * time.Hour
	maxWarmupRecipientRecheck = 8 * time.Hour
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
		// Watch band: cold volume eases off while warmup keeps its normal spacing, since warming is how placement recovers.
		return healthAdjustment{volumeMultiplier: 0.7, minWaitMultiplier: 1.0}
	default:
		return healthAdjustment{volumeMultiplier: 1.0, minWaitMultiplier: 1.0}
	}
}

// warmupPoolTypeForAccount is the pool the mailbox actually warms in: a restricted
// organization is held in free whatever tier it carries.
func (s *schedulerService) warmupPoolTypeForAccount(ctx context.Context, account *models.Email) string {
	if account == nil {
		return "premium"
	}
	if s.orgRiskState(ctx, account.OrganizationID).ForcesFreeWarmupPool() {
		return "free"
	}
	if account.WarmupPoolType != "" {
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

// warmupRampTarget is the scheduler's entry point into the shared ramp policy.
// It stays here so the existing call sites and tests read unchanged.
func warmupRampTarget(activelyWarming bool, base, increase, max, daysWarming int, inCampaign bool) int {
	return warmupramp.Target(activelyWarming, base, increase, max, daysWarming, inCampaign)
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

	// Warmup has its own days and ramp; the sending plan supplies hours, breaks and spacing.
	bhv := s.behaviorFor(ctx, account).ForWarmup(account.WarmupDays)
	warmupStart, warmupEnd := account.WarmupStartTime, account.WarmupEndTime
	if bhv.Enabled {
		plan := bhv.PlanOn(behavior.PlanDateFor(time.Now(), bhv.Loc))
		warmupStart = minutesToClock(max(models.ClockMinutes(account.WarmupStartTime, 8*60), plan.WorkStartMinute))
		warmupEnd = minutesToClock(min(models.ClockMinutes(account.WarmupEndTime, 20*60), plan.WorkEndMinute))
	}
	opening, err := warmupDispatchWindow(account, bhv, time.Now())
	if err != nil {
		return time.Time{}, err
	}
	if !sameLocalDay(opening, time.Now(), loadLocation(account.ClockTimezone())) {
		return s.snapWarmupToBehavior(account, bhv, opening.Add(time.Duration(randomJitter(0, 60))*time.Minute))
	}

	// STEP 2: One shared resolve, so this target and the one the mailbox
	// drawer reports cannot drift apart.
	healthState := s.resolveHealthState(ctx, accountID)
	rampAnchor := time.Now()
	if account.Warmup != nil {
		rampAnchor = *account.Warmup
	}
	plan := warmupramp.Resolve(ctx, s.warmupRepo, warmupramp.Input{
		AccountID:       accountID,
		WarmupStart:     rampAnchor,
		ActivelyWarming: activelyWarming,
		Base:            account.WarmupBase,
		Increase:        account.WarmupIncrease,
		Max:             account.WarmupMax,
		InCampaign:      inCampaign,
		Health:          healthState,
		Now:             time.Now(),
	})
	targetVolume := plan.Target
	if plan.Cut() {
		log.Info().
			Str("email_account_id", accountID.String()).
			Int("placements_48h", plan.Placements).
			Int("sends_48h", plan.Sends).
			Int("target", targetVolume).
			Msg("warmup volume cut on an early placement signal; ramp held")
	}

	// Vary the day's target so a mailbox doesn't send an identical count every
	// day. Deterministic per (account, local day) so it's stable across the
	// day's reschedules. Actively-warming mailboxes keep a floor of WarmupBase.
	if activelyWarming && targetVolume > 0 {
		factor := dailyVolumeFactor(accountID, time.Now().In(loadLocation(account.ClockTimezone())))
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
		poolType := s.warmupPoolTypeForAccount(ctx, account)
		// The set the selector draws from, so the cap never exceeds what a send can reach.
		candidates, err := s.warmupRepo.WarmupPartnerCandidates(ctx, poolType, accountID)
		if err == nil {
			eligibleRecipients := len(candidates)
			if eligibleRecipients <= 0 {
				return s.snapWarmupToBehavior(account, bhv, recipientRecheckTime())
			}
			if targetVolume > eligibleRecipients {
				targetVolume = eligibleRecipients
			}
		}
	}

	// Resolve owns the band's volume half; only its spacing half applies here.
	adj := adjustmentFor(healthState)

	// Spacing: a drawn gap from the profile when one is enabled, otherwise the
	// mailbox's fixed min gap. The health-state multiplier still applies on top,
	// so a throttled mailbox stretches whichever base it is using.
	minWaitSeconds := s.behaviorGap(bhv, time.Now(), account.MinWaitTime)
	if adj.minWaitMultiplier > 1.0 {
		minWaitSeconds = int(float64(minWaitSeconds)*adj.minWaitMultiplier + 0.5)
	}

	// STEP 3: Count emails already sent on the mailbox's local day, the day the dispatch gate counts.
	dayStart := behavior.PlanDateFor(time.Now(), loadLocation(account.ClockTimezone()))
	emailsSentToday, err := s.taskRepo.CountWarmupEmailsSentBetween(ctx, accountID, dayStart, dayStart.AddDate(0, 0, 1))
	if err != nil {
		return time.Time{}, err
	}

	// STEP 4: Check if we've hit today's limit
	if emailsSentToday >= targetVolume {
		// Move to tomorrow's first slot
		return s.snapWarmupToBehavior(account, bhv, calculateFirstSlotTomorrowAt(account.ClockTimezone(), account.WarmupStartTime))
	}

	// STEP 5: Calculate ideal spacing
	// Distribute remaining emails across remaining business hours
	remainingSlots := targetVolume - emailsSentToday
	hoursRemaining := calculateHoursRemainingUntil(account.ClockTimezone(), warmupEnd)

	// If business hours are over, move to tomorrow
	if hoursRemaining <= 0 {
		return s.snapWarmupToBehavior(account, bhv, calculateFirstSlotTomorrowAt(account.ClockTimezone(), account.WarmupStartTime))
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
	candidateTime := notBefore(earliestNext.Add(time.Minute * time.Duration(jitter)))

	// STEP 9: Avoid exact round times (10:00, 11:00)
	candidateTime = avoidRoundTimes(candidateTime)

	// STEP 10: Ensure within configured warmup time window
	candidateTime = ensureTimeWindow(candidateTime, warmupStart, warmupEnd, loadLocation(account.ClockTimezone()))

	// STEP 11: Check conflicts with other sends booked on this account
	booked, err := s.taskRepo.GetBookedSendsForAccount(ctx, accountID, candidateTime)
	if err != nil {
		return time.Time{}, err
	}

	candidateTime = resolveConflicts(candidateTime, booked, minWaitSeconds)

	// STEP 12: Apply human-like distribution curve. Skipped when a behaviour
	// profile is driving the day — it already carries this mailbox's own peak
	// hours and lunch break.
	loc := loadLocation(account.ClockTimezone())
	if !bhv.Enabled {
		candidateTime = applyDistributionCurve(candidateTime, loc)
	}

	return s.snapWarmupToBehavior(account, bhv, candidateTime)
}

// Scheduling and execution use the same day, hour and break constraints.
func (s *schedulerService) snapWarmupToBehavior(account *models.Email, r behavior.Resolved, candidate time.Time) (time.Time, error) {
	return warmupDispatchWindow(account, r, finalSlot(candidate))
}

// minutesToClock renders minutes-since-midnight as the "15:04" string the
// legacy warmup window helpers parse.
func minutesToClock(minute int) string {
	if minute < 0 {
		minute = 0
	}
	if minute >= models.MinutesPerDay {
		minute = models.MinutesPerDay - 1
	}
	return fmt.Sprintf("%02d:%02d", minute/60, minute%60)
}
