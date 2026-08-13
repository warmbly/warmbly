package scheduler

import (
	"context"
	"math/rand"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// CalculateNextCampaignTime calculates the next best time to send a campaign email
// Returns: nextTime, contactSequencePair, emailAccountID, error
func (s *schedulerService) CalculateNextCampaignTime(ctx context.Context, campaignID uuid.UUID) (time.Time, *repository.ContactSequencePair, uuid.UUID, error) {
	// STEP 1: Load campaign details
	campaign, err := s.campaignRepo.GetByID(ctx, campaignID)
	if err != nil {
		return time.Time{}, nil, uuid.Nil, err
	}

	if campaign.Status != "active" {
		return time.Time{}, nil, uuid.Nil, ErrCampaignNotActive
	}

	// STEP 1.5: Advance the per-campaign daily ramp level (idempotent, once per
	// UTC day; no-op when ramp is disabled). Re-load so campaign.RampLevel
	// reflects today's level before any capacity math. Failing open here keeps
	// scheduling running (the worst case is today's ramp not advancing).
	if campaign.RampEnabled {
		if aerr := s.campaignRepo.AdvanceRampLevel(ctx, campaignID); aerr == nil {
			if reloaded, rerr := s.campaignRepo.GetByID(ctx, campaignID); rerr == nil && reloaded != nil {
				campaign = reloaded
			}
		}
	}

	// STEP 2: Resolve the campaign's sending mailboxes. Explicit strategy uses
	// the campaign_senders pool (carrying per-sender rotation metadata); tags
	// strategy keeps the existing tag-based resolution. An empty explicit pool
	// falls back to tags so a misconfigured campaign still sends.
	type senderMeta struct {
		weight           int
		rotationPosition int
		lastSentAt       *time.Time
		hasMeta          bool
	}
	accounts := []models.Email{}
	senderMetaByID := map[uuid.UUID]senderMeta{}
	seen := map[uuid.UUID]bool{}
	// UNION of the explicit campaign_senders pool and the tag-resolved mailboxes
	// (one dropdown picks both — they're no longer mutually exclusive). When the
	// campaign selects NEITHER tags nor explicit accounts, it sends from ALL of
	// the owner's active mailboxes ("all").
	senders, serr := s.emailRepo.GetByCampaignSenders(ctx, campaign.UserID, campaignID)
	if serr != nil {
		return time.Time{}, nil, uuid.Nil, serr
	}
	for _, snd := range senders {
		accounts = append(accounts, snd.Account)
		seen[snd.Account.ID] = true
		senderMetaByID[snd.Account.ID] = senderMeta{
			weight:           snd.Weight,
			rotationPosition: snd.RotationPosition,
			lastSentAt:       snd.LastSentAt,
			hasMeta:          true,
		}
	}
	if len(campaign.EmailTags) > 0 {
		tagAccounts, terr := s.emailRepo.GetByTags(ctx, campaign.UserID, campaign.EmailTags)
		if terr != nil {
			return time.Time{}, nil, uuid.Nil, terr
		}
		for _, ta := range tagAccounts {
			if !seen[ta.ID] {
				accounts = append(accounts, ta)
				seen[ta.ID] = true
			}
		}
	}
	if len(senders) == 0 && len(campaign.EmailTags) == 0 {
		allAccts, aerr := s.emailRepo.GetAllActiveByUser(ctx, campaign.UserID)
		if aerr != nil {
			return time.Time{}, nil, uuid.Nil, aerr
		}
		accounts = allAccts
	}

	if len(accounts) == 0 {
		return time.Time{}, nil, uuid.Nil, ErrNoEmailAccounts
	}

	// STEP 3: Get campaign progress - find next contact/sequence to send.
	// Honor the new-lead-per-day cap and the prioritize-new-leads ordering.
	orderField := ""
	if campaign.ContactOrderField != nil {
		orderField = *campaign.ContactOrderField
	}
	excludeNewLeads := false
	if campaign.MaxNewLeadsPerDay > 0 {
		newLeadsToday, nlerr := s.campaignRepo.CountNewLeadsStartedToday(ctx, campaignID)
		if nlerr != nil {
			return time.Time{}, nil, uuid.Nil, nlerr
		}
		if newLeadsToday >= campaign.MaxNewLeadsPerDay {
			excludeNewLeads = true
		}
	}
	nextPair, recheckAt, err := s.campaignProgressRepo.FindNextRoutedPair(
		ctx,
		campaignID,
		campaign.ContactOrderBy,
		campaign.ContactOrderDir,
		orderField,
		campaign.PrioritizeNewLeads,
		excludeNewLeads,
	)
	if err != nil {
		return time.Time{}, nil, uuid.Nil, err
	}

	if nextPair == nil {
		// When the new-lead cap is active and only new-lead pairs remain,
		// FindNextRoutedPair returns nil with exclude on but WOULD return a pair
		// without it. In that case defer to the next day so follow-ups keep
		// progressing and new leads resume tomorrow — do NOT complete.
		if excludeNewLeads {
			if again, _, aerr := s.campaignProgressRepo.FindNextRoutedPair(
				ctx, campaignID, campaign.ContactOrderBy, campaign.ContactOrderDir, orderField,
				campaign.PrioritizeNewLeads, false,
			); aerr == nil && again != nil {
				s.logCampaignDecision(ctx, campaignID, "new_lead_cap_reached",
					"Daily new-lead cap reached; deferring remaining new leads to tomorrow",
					map[string]interface{}{"max_new_leads_per_day": campaign.MaxNewLeadsPerDay})
				deferTime := s.deferToNextDay(campaign)
				// Return a DEFERRAL, never a sendable pair: the caller only checks
				// err for deferrals, so a nil-error here would send a new lead and
				// blow past the cap. nil pair + sentinel = reschedule, don't send.
				return deferTime, nil, accounts[0].ID, ErrCampaignDeferred
			}
		}
		// Some contacts are waiting on a condition window (e.g. "if didn't open
		// within 3 days"). Defer and re-check exactly when the soonest window
		// elapses, instead of marking the campaign complete.
		if recheckAt != nil {
			s.logCampaignDecision(ctx, campaignID, "awaiting_condition_window",
				"Waiting on a branch condition window; re-checking when it elapses",
				map[string]interface{}{"recheck_at": recheckAt.UTC().Format(time.RFC3339)})
			return *recheckAt, nil, accounts[0].ID, ErrCampaignDeferred
		}
		return time.Time{}, nil, uuid.Nil, ErrCampaignCompleted
	}

	// Branch routing is resolved inside FindNextRoutedPair: the chosen step is the
	// route out of the contact's last-sent step — conditional branches first
	// (first match wins, evaluated against opened/clicked/replied), then the
	// explicit "else" catch-all, then linear position+1 only when a step defines
	// no branches. A step is sent only if the flow reaches it; STOP/end and
	// already-sent loops drop the contact in the finder. Conditions are evaluated
	// at schedule time (a known, accepted race vs. last-moment engagement).

	// STEP 3.5: Resolve the recipient ESP/provider for ESP matching. Cheap:
	// prefer the cached contact.esp_provider, else derive from the domain
	// string. NEVER dial MX on the hot path. Empty => unknown => wildcard.
	recipientProvider := ""
	if campaign.ESPMatchMode != "off" && s.contactRepo != nil {
		if contact, cerr := s.contactRepo.GetByID(ctx, nextPair.ContactID); cerr == nil && contact != nil {
			if contact.ESPProvider != "" {
				recipientProvider = contact.ESPProvider
			} else {
				recipientProvider = providerForEmailDomain(contact.Email)
				// Opportunistically cache the derived provider (best-effort).
				if recipientProvider != "" {
					_ = s.contactRepo.SetContactESP(ctx, contact.ID, recipientProvider)
				}
			}
		}
	}

	// STEP 4: Calculate base time from sequence wait_after
	baseTime := time.Now()

	// Check if this contact has already received emails in this campaign
	lastSentTime, err := s.campaignProgressRepo.GetContactLastSequenceTime(ctx, nextPair.ContactID, campaignID)
	if err != nil {
		return time.Time{}, nil, uuid.Nil, err
	}

	if lastSentTime != nil {
		// Get sequence details to know wait_after
		sequence, err := s.campaignRepo.GetSequenceByID(ctx, nextPair.SequenceID)
		if err != nil {
			return time.Time{}, nil, uuid.Nil, err
		}

		// Add wait_after days to last sent time
		waitDuration := time.Hour * 24 * time.Duration(sequence.WaitAfter)
		baseTime = lastSentTime.Add(waitDuration)
	}

	// STEP 5: Apply campaign schedule constraints
	// Fall back to UTC if campaign has no timezone set (account timezone checked later)
	campaignTZName := campaign.Timezone
	campaignTZ := loadLocation(campaignTZName)
	// Authoritative per-day sending windows (or derived from the legacy
	// days/start/end fields). Drives every day-of-week + time-window gate below.
	windows := effectiveWindows(campaign)
	candidateTime := baseTime

	// Check campaign date range
	if campaign.StartDate != nil && candidateTime.Before(*campaign.StartDate) {
		candidateTime = *campaign.StartDate
	}

	if campaign.EndDate != nil && candidateTime.After(*campaign.EndDate) {
		return time.Time{}, nil, uuid.Nil, ErrCampaignEnded
	}

	// STEP 6+7: Snap to the next allowed per-day sending window (handles both
	// the day-of-week gate and the time-of-day window, including multiple
	// intervals per day).
	candidateTime = nextScheduleSlot(candidateTime, windows, campaignTZ)

	// effectiveCap is the per-mailbox cold cap for THIS campaign, after the ramp
	// clamp. It is min(per-mailbox cold cap, campaign daily limit) further min()'d
	// with the day's ramp ceiling. Applied via min() only — it can never RAISE a
	// mailbox above its cold cap (the mailbox-first safety invariant).
	effectiveCap := func(acct models.Email) int {
		lim := min(acct.CampaignLimit, campaign.DailyLimit)
		if campaign.RampEnabled {
			lim = min(lim, campaignRampCeiling(true, campaign.RampStart, campaign.RampIncrement, campaign.RampCeiling, campaign.RampLevel))
		}
		return lim
	}

	// providerMatches reports whether a mailbox's provider satisfies the
	// recipient ESP under the current match mode. An unknown recipient provider
	// (non-Google/Outlook domain) is always a wildcard so matching never blocks
	// first contact. An smtp_imap mailbox has no known ESP: under PREFER it acts
	// as a wildcard so matching never starves, but under STRICT "same provider"
	// means exactly that — an smtp_imap mailbox is NOT treated as a Gmail/Outlook
	// match (it only carries unknown/other-domain recipients, handled above).
	providerMatches := func(acctProvider string) bool {
		if campaign.ESPMatchMode == "off" || recipientProvider == "" {
			return true
		}
		if acctProvider == "smtp_imap" {
			return campaign.ESPMatchMode != "strict"
		}
		return acctProvider == recipientProvider
	}

	// STEP 8: Build weighted account candidates, gating each mailbox on when it
	// may next send in its OWN timezone: its rolled workday when it has a
	// sending-behaviour profile, and the historical 8am-8pm business-hours band
	// when it does not.
	//
	// Profiles are resolved for the whole pool in one query, so adding
	// mailboxes to a campaign does not add a query per mailbox to every pass.
	behaviors := s.behaviorForAll(ctx, accounts)

	// Rotation fallback for mailboxes with no campaign_senders row (tag-resolved
	// pools and the "all active mailboxes" default). One query for the whole
	// pool; a failure just leaves the map empty and rotation degrades to its
	// tie-breaker rather than blocking the send.
	lastSends := map[uuid.UUID]time.Time{}
	if needsRotationFallback(campaign.RotationMode) {
		ids := make([]uuid.UUID, 0, len(accounts))
		for _, a := range accounts {
			if _, ok := senderMetaByID[a.ID]; !ok {
				ids = append(ids, a.ID)
			}
		}
		if sends, lerr := s.taskRepo.GetLastSendTimes(ctx, ids, "campaign"); lerr == nil {
			lastSends = sends
		}
	}

	var candidates []AccountCandidate
	for _, acct := range accounts {
		sentToday, err := s.taskRepo.CountCampaignEmailsSentToday(ctx, acct.ID)
		if err != nil {
			return time.Time{}, nil, uuid.Nil, err
		}

		acctLimit := effectiveCap(acct)
		remaining := acctLimit - sentToday

		// Skip accounts that have reached their daily limit
		if remaining <= 0 {
			continue
		}

		bhv := behaviors[acct.ID]

		// Health-gate cold sends on the SAME warmup health state used for pool
		// selection, so a mailbox in deliverability trouble doesn't keep blasting
		// cold volume (the concentration risk the safety policy warns about):
		//   - quarantined/blocked (still within blocked_until) → don't send at all
		//   - watch/throttled → dampen today's budget by the same multiplier warmup
		//     uses for that band (watch 0.7x, throttled 0.5x), via adjustmentFor so
		//     the warmup and cold schedulers can't drift; the wider min-gap still applies
		// This gate runs FIRST, before any rotation/ESP logic, so a degraded
		// mailbox is always dropped regardless of weighting.
		if state, blockedUntil, herr := s.warmupRepo.GetHealthState(ctx, acct.ID); herr == nil {
			switch state {
			case models.WarmupHealthQuarantined, models.WarmupHealthBlocked:
				if blockedUntil == nil || blockedUntil.After(time.Now()) {
					continue
				}
			case models.WarmupHealthWatch, models.WarmupHealthThrottled:
				remaining = int(float64(remaining) * adjustmentFor(state).volumeMultiplier)
				if remaining <= 0 {
					continue
				}
			}
		}

		// Where the mailbox may next send, in its OWN timezone. With a
		// behaviour profile this is its rolled workday (start, lunch, end,
		// working weekdays) with the hourly ceiling already applied; without
		// one it falls back to the historical 8am-8pm business-hours gate.
		var behaviorOpenAt *time.Time
		if bhv.Enabled {
			openAt, ok := s.placeWithinBehavior(ctx, bhv, candidateTime)
			if !ok {
				continue // profile has no working days at all
			}
			behaviorOpenAt = &openAt

			// Budget the send against the day it will actually land on. When
			// today is spent, placeWithinBehavior has already walked to a later
			// day, and that day starts with the mailbox's full cold cap.
			if !sameLocalDay(openAt, time.Now(), bhv.Loc) {
				remaining = acctLimit
			}
			// The day's rolled cold budget, folded in by min(): a persona can
			// only lower a mailbox's remaining sends, never lift it above the
			// cold cap.
			remaining = s.behaviorDailyCap(ctx, bhv, remaining, openAt)
			if remaining <= 0 {
				continue
			}
		} else if acct.Timezone != "" && acct.Timezone != campaign.Timezone {
			acctTZ := loadLocation(acct.Timezone)
			acctLocal := candidateTime.In(acctTZ)
			acctHour := acctLocal.Hour()
			if acctHour < 8 || acctHour >= 20 {
				continue // outside account's business hours
			}
		}

		warmupAgeDays := 0
		if acct.Warmup != nil {
			warmupAgeDays = int(time.Since(*acct.Warmup).Hours() / 24)
		}

		cand := AccountCandidate{
			Account:        acct,
			RemainingToday: remaining,
			WarmupAgeDays:  warmupAgeDays,
			Weight:         computeWeight(remaining, warmupAgeDays),
			ProviderMatch:  providerMatches(acct.Provider),
			Behavior:       bhv,
			BehaviorOpenAt: behaviorOpenAt,
		}
		if meta, ok := senderMetaByID[acct.ID]; ok {
			cand.HasSenderMetadata = true
			cand.SenderWeight = meta.weight
			cand.RotationPosition = meta.rotationPosition
			cand.SenderLastSentAt = meta.lastSentAt
		} else {
			// No explicit sender row, so no stored cursor. Derive both signals
			// from what the mailbox has actually done: today's send count is
			// the round-robin position (least-used goes next), and its real
			// last send drives least-recently-used. Without this, a tag-based
			// campaign on either mode would keep picking the same mailbox.
			cand.RotationPosition = sentToday
			if at, ok := lastSends[acct.ID]; ok {
				t := at
				cand.SenderLastSentAt = &t
			}
		}
		candidates = append(candidates, cand)
	}

	// STEP 8.25: Apply ESP matching to the under-budget candidate set.
	//   strict → only matching mailboxes are eligible; if none, DEFER (never
	//            send cross-provider).
	//   prefer → restrict to matching mailboxes when at least one has capacity,
	//            otherwise fall back to the full eligible set (never starves).
	if campaign.ESPMatchMode != "off" && recipientProvider != "" {
		matching := make([]AccountCandidate, 0, len(candidates))
		for _, c := range candidates {
			if c.ProviderMatch {
				matching = append(matching, c)
			}
		}
		switch campaign.ESPMatchMode {
		case "strict":
			if len(matching) == 0 {
				// No matching mailbox under budget today: defer to the next slot
				// rather than complete or send cross-provider.
				s.logCampaignDecision(ctx, campaignID, "provider_match_deferred",
					"No same-provider mailbox available; deferring to next slot",
					map[string]interface{}{"recipient_provider": recipientProvider})
				// Deferral, not a send: nil pair + sentinel so the caller reschedules
				// instead of sending this contact from a cross-provider mailbox.
				return s.deferToNextDay(campaign), nil, accounts[0].ID, ErrCampaignDeferred
			}
			candidates = matching
		case "prefer":
			if len(matching) > 0 {
				candidates = matching
			}
		}
	}

	// STEP 8.5: Select best account per the campaign's rotation mode.
	selected := selectAccountByRotationMode(campaign.RotationMode, candidates)
	if selected == nil {
		// ALL accounts at capacity today — push to next day and recompute with
		// tomorrow's full (ramp-clamped) capacity. The ramp clamp AND the ESP
		// filter MUST be re-applied here, or tomorrow's recompute over-budgets a
		// mailbox past its ramp ceiling / picks a cross-provider sender.
		candidateTime = candidateTime.Add(24 * time.Hour)
		candidateTime = nextScheduleSlot(candidateTime, windows, campaignTZ)

		var tomorrow []AccountCandidate
		for i := range candidates {
			acct := candidates[i].Account
			// ESP-strict: keep only matching mailboxes for tomorrow too.
			if campaign.ESPMatchMode == "strict" && recipientProvider != "" && !candidates[i].ProviderMatch {
				continue
			}
			acctLimit := effectiveCap(acct) // same ramp clamp as STEP 8
			c := candidates[i]
			c.RemainingToday = acctLimit
			c.Weight = computeWeight(acctLimit, candidates[i].WarmupAgeDays)
			tomorrow = append(tomorrow, c)
		}
		// ESP-prefer: restrict tomorrow to matching mailboxes when any exist.
		if campaign.ESPMatchMode == "prefer" && recipientProvider != "" {
			var matchingTomorrow []AccountCandidate
			for _, c := range tomorrow {
				if c.ProviderMatch {
					matchingTomorrow = append(matchingTomorrow, c)
				}
			}
			if len(matchingTomorrow) > 0 {
				tomorrow = matchingTomorrow
			}
		}

		selected = selectAccountByRotationMode(campaign.RotationMode, tomorrow)
		if selected == nil {
			// ESP-strict with no matching mailbox at all: defer rather than
			// complete or send cross-provider.
			if campaign.ESPMatchMode == "strict" && recipientProvider != "" {
				s.logCampaignDecision(ctx, campaignID, "provider_match_deferred",
					"No same-provider mailbox available tomorrow; deferring",
					map[string]interface{}{"recipient_provider": recipientProvider})
				// Deferral, not a send (see above).
				return s.deferToNextDay(campaign), nil, accounts[0].ID, ErrCampaignDeferred
			}
			return time.Time{}, nil, uuid.Nil, ErrNoEmailAccounts
		}
	}

	account := &selected.Account

	// STEP 8.75: Send-to-send spacing for THIS send. With a behaviour profile
	// the gap is drawn fresh from the mailbox's range, so the intervals between
	// its sends are irregular; otherwise it is the mailbox's fixed min gap.
	gapSeconds := s.behaviorGap(selected.Behavior, candidateTime, account.MinWaitTime)

	// Move the candidate onto the mailbox's own workday before the spacing
	// maths below runs, so distribution is computed against the window the send
	// will actually land in.
	if selected.BehaviorOpenAt != nil && selected.BehaviorOpenAt.After(candidateTime) {
		candidateTime = *selected.BehaviorOpenAt
	}

	// STEP 9: Even distribution across the candidate day's sending window. With
	// a behaviour profile that is the mailbox's own rolled workday (lunch
	// excluded); otherwise it is the span of the campaign's intervals for that
	// weekday.
	remainingEmails := selected.RemainingToday
	if remainingEmails > 0 {
		remainingMinutes, ok := remainingSendMinutes(selected, candidateTime, windows, campaignTZ)
		if ok && remainingMinutes > 0 {
			// Vary the pace multiplicatively (bursts and lulls) — evenly
			// metronomed sends are a pattern even with additive jitter.
			varied := float64(remainingMinutes/remainingEmails) * (0.55 + rand.Float64()*0.9)
			idealInterval := time.Duration(varied * float64(time.Minute))
			minInterval := time.Second * time.Duration(gapSeconds)
			if idealInterval < minInterval {
				idealInterval = minInterval
			}
			distributedTime := time.Now().Add(idealInterval)
			if distributedTime.After(candidateTime) {
				candidateTime = distributedTime
			}
		}
	}

	// STEP 10: Respect minimum wait time from account's last email
	lastEmailTime, err := s.taskRepo.GetLastEmailTime(ctx, account.ID)
	if err != nil {
		return time.Time{}, nil, uuid.Nil, err
	}

	if lastEmailTime != nil {
		minWait := time.Second * time.Duration(gapSeconds)
		earliestNext := lastEmailTime.Add(minWait)

		if candidateTime.Before(earliestNext) {
			candidateTime = earliestNext
			// Re-snap into a sending window after adjusting for min wait.
			candidateTime = nextScheduleSlot(candidateTime, windows, campaignTZ)
		}
	}

	// STEP 11: Add jitter. Deliberately NOT rounded to a 5-minute grid — a
	// fleet that only ever sends at :x0/:x5 marks is a detectable pattern.
	jitter := randomJitter(-20, 20)
	candidateTime = notBefore(candidateTime.Add(time.Minute * time.Duration(jitter)))

	// STEP 12: Check conflicts with other scheduled tasks
	dateToCheck := candidateTime
	scheduledTasks, err := s.taskRepo.GetScheduledTasksForAccount(ctx, account.ID, dateToCheck)
	if err != nil {
		return time.Time{}, nil, uuid.Nil, err
	}

	candidateTime = resolveConflicts(candidateTime, scheduledTasks, gapSeconds)

	// STEP 13: Apply human-like distribution (favor morning/afternoon peaks).
	// Skipped for behaviour-profiled mailboxes: the profile already describes
	// this mailbox's workday and its own lunch break, and layering the generic
	// curve on top would drag sends away from the hours the customer chose.
	if !selected.Behavior.Enabled {
		candidateTime = applyDistributionCurve(candidateTime, campaignTZ)
	}

	// STEP 14: Ensure still within a sending window after all adjustments
	// (jitter/conflict/distribution can push into a gap between intervals, or
	// out of the mailbox's workday). Satisfies both calendars.
	candidateTime = s.intersectWindows(ctx, selected.Behavior, notBefore(candidateTime), windows, campaignTZ)

	// STEP 15: Randomise the sub-minute component so sends never land on :00.
	return finalSlot(candidateTime), nextPair, account.ID, nil
}

// deferToNextDay pushes a candidate time to the next valid campaign day within
// the campaign's send window. Used by the ESP-strict and new-lead-cap deferral
// paths so a campaign reschedules instead of completing or busy-looping.
func (s *schedulerService) deferToNextDay(campaign *models.Campaign) time.Time {
	tz := loadLocation(campaign.Timezone)
	t := nextScheduleSlot(time.Now().Add(24*time.Hour), effectiveWindows(campaign), tz)
	// Add a small jitter so deferred tasks don't all wake at the same instant.
	return t.Add(time.Minute * time.Duration(randomJitter(0, 30)))
}

// logCampaignDecision records a send-path decision (ESP defer, new-lead cap) to
// the campaign activity log. Best-effort and nil-safe — a logging miss never
// blocks scheduling.
func (s *schedulerService) logCampaignDecision(ctx context.Context, campaignID uuid.UUID, eventType, message string, metadata map[string]interface{}) {
	if s.campaignLogRepo == nil {
		return
	}
	_ = s.campaignLogRepo.CreateLog(ctx, &repository.CampaignLogEntry{
		CampaignID: campaignID,
		EventType:  eventType,
		Message:    message,
		Metadata:   metadata,
	})
}
