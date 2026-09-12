package scheduler

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/config"
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

	// STEP 2: Resolve the campaign's sending mailboxes.
	accounts, senderMetaByID, err := s.campaignSenders(ctx, campaign)
	if err != nil {
		return time.Time{}, nil, uuid.Nil, err
	}

	if len(accounts) == 0 {
		return time.Time{}, nil, uuid.Nil, ErrNoEmailAccounts
	}

	// STEP 2.5: What the pool can do this pass, resolved once. `paced` is the
	// mailboxes that are simply busy right now (today's budget spent, hours
	// closed); routing holds back the leads already bound to one so the rest of
	// the campaign keeps sending, instead of parking every lead behind the
	// first one whose mailbox is full.
	pass := s.newCampaignPass(ctx, campaign, accounts)
	paced := s.pacedSenders(ctx, pass, accounts)

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
	// The due leads this pass may try, in routing order. More than one, because
	// placement can refuse a lead for a reason that is that lead's alone while
	// the lead behind them is sendable this second (issue #437).
	candidates, recheckAt, senderWait, err := s.campaignProgressRepo.FindRoutedPairs(
		ctx,
		campaignID,
		campaign.ContactOrderBy,
		campaign.ContactOrderDir,
		orderField,
		campaign.PrioritizeNewLeads,
		excludeNewLeads,
		paced,
		config.CampaignPlacementCandidates,
	)
	if err != nil {
		return time.Time{}, nil, uuid.Nil, err
	}

	if len(candidates) == 0 {
		// When the new-lead cap is active and only new-lead pairs remain,
		// FindRoutedPairs returns nothing with exclude on but WOULD return a
		// pair without it. In that case defer to the next day so follow-ups keep
		// progressing and new leads resume tomorrow — do NOT complete.
		if excludeNewLeads {
			again, againDue, againSenderWait, aerr := s.campaignProgressRepo.FindRoutedPairs(
				ctx, campaignID, campaign.ContactOrderBy, campaign.ContactOrderDir, orderField,
				campaign.PrioritizeNewLeads, false, paced, 1,
			)
			switch {
			case aerr != nil:
				// Fall through to the ordinary wait/complete decision below.
			case len(again) > 0:
				s.logCampaignDecision(ctx, campaignID, "new_lead_cap_reached",
					"Daily new-lead cap reached; deferring remaining new leads to tomorrow",
					map[string]interface{}{"max_new_leads_per_day": campaign.MaxNewLeadsPerDay})
				deferTime := s.deferToNextDay(campaign, false)
				// A follow-up that comes due before tomorrow must not wait for
				// the new-lead cap to reset.
				if recheckAt != nil && recheckAt.Before(deferTime) {
					deferTime = *recheckAt
				}
				// Return a DEFERRAL, never a sendable pair: the caller only checks
				// err for deferrals, so a nil-error here would send a new lead and
				// blow past the cap. nil pair + sentinel = reschedule, don't send.
				return deferTime, nil, accounts[0].ID, ErrCampaignDeferred
			case againDue != nil && (recheckAt == nil || againDue.Before(*recheckAt)):
				// The excluded pass skips new leads BEFORE routing them, so a new
				// lead still inside the campaign's entry delay contributes no
				// re-check time to `recheckAt`. Take the unexcluded pass's, or a
				// campaign whose only remaining leads are delayed ones completes
				// while they are still waiting to be sent.
				recheckAt, senderWait = againDue, againSenderWait
			}
		}
		// Nothing is due yet: every remaining contact is inside a step's wait
		// or a condition window (e.g. "if didn't open within 3 days"). Defer
		// and re-check exactly when the soonest one elapses, instead of
		// marking the campaign complete.
		if recheckAt != nil {
			// Once per day, not once per pass: a deferred chain re-finds this
			// every ~15 minutes, and a campaign holding its first emails for two
			// days would otherwise write two hundred identical activity lines.
			message := "No step is due yet; re-checking when the next wait elapses"
			metadata := map[string]interface{}{"recheck_at": recheckAt.UTC().Format(time.RFC3339)}
			if senderWait {
				// The steps ARE due; their mailboxes are not free. Say that,
				// rather than blaming a wait nobody configured.
				message = "Every lead that is due is waiting for its own mailbox: each contact keeps the address they first heard from, and those mailboxes have nothing left for now"
				metadata["waiting_on_sender"] = true
			}
			if !senderWait && campaign.EntryDelayMinutes > 0 {
				message += fmt.Sprintf(" (this campaign holds the first email for %s after a contact enters it)",
					humanizeMinutes(campaign.EntryDelayMinutes))
				metadata["entry_delay_minutes"] = campaign.EntryDelayMinutes
			}
			s.logCampaignDecisionOnce(ctx, campaignID, "awaiting_next_step", message, metadata)
			return *recheckAt, nil, accounts[0].ID, ErrCampaignDeferred
		}
		return time.Time{}, nil, uuid.Nil, ErrCampaignCompleted
	}

	// Branch routing is resolved inside FindRoutedPairs: the chosen step is the
	// route out of the contact's last-sent step — conditional branches first
	// (first match wins, evaluated against opened/clicked/replied), then the
	// explicit "else" catch-all, then linear position+1 only when a step defines
	// no branches. A step is sent only if the flow reaches it; STOP/end and
	// already-sent loops drop the contact in the finder. Conditions are evaluated
	// at schedule time (a known, accepted race vs. last-moment engagement).
	//
	// Place the due leads in order and send the first one the pool can take. A
	// refusal that is about THIS lead (ErrLeadDeferred: ESP-strict has no
	// mailbox for their provider, their own mailbox is busy, their preferred
	// hours are hours away) moves to the lead behind them; anything else is the
	// pool's answer for every lead and ends the pass immediately. Only when
	// every candidate is refused does the campaign defer, at the soonest of
	// their slots.
	var leadSlot time.Time
	leadAccount := accounts[0].ID
	for i := range candidates {
		at, sendable, accountID, perr := s.placeCampaignSend(ctx, campaign, accounts, senderMetaByID, &candidates[i], pass, false)
		if !errors.Is(perr, ErrLeadDeferred) {
			return at, sendable, accountID, perr
		}
		if leadSlot.IsZero() || (!at.IsZero() && at.Before(leadSlot)) {
			leadSlot, leadAccount = at, accountID
		}
	}
	// Every due lead was refused for its own reason. Re-check on the deferral
	// horizon rather than completing: the leads are still there, and what
	// refuses them (a mailbox under budget again, a recipient's morning) comes
	// back from outside this chain.
	return leadSlot, nil, leadAccount, ErrCampaignDeferred
}

// humanizeMinutes renders a delay as the largest whole unit it divides into
// ("2 days", "4 hours", "90 minutes") for the campaign activity feed.
func humanizeMinutes(minutes int) string {
	unit := func(n int, name string) string {
		if n == 1 {
			return "1 " + name
		}
		return fmt.Sprintf("%d %ss", n, name)
	}
	switch {
	case minutes%(24*60) == 0:
		return unit(minutes/(24*60), "day")
	case minutes%60 == 0:
		return unit(minutes/60, "hour")
	default:
		return unit(minutes, "minute")
	}
}

// senderMeta is a mailbox's campaign_senders rotation metadata.
type senderMeta struct {
	weight           int
	rotationPosition int
	lastSentAt       *time.Time
	hasMeta          bool
}

// campaignSenders resolves the campaign's sending mailboxes through the shared
// resolver (explicit pool united with tags, "all" when neither is selected),
// keeping the explicit pool's rotation metadata for the rotation modes.
func (s *schedulerService) campaignSenders(ctx context.Context, campaign *models.Campaign) ([]models.Email, map[uuid.UUID]senderMeta, error) {
	pool, err := repository.ResolveCampaignSenderPool(ctx, s.emailRepo, campaign)
	if err != nil {
		return nil, nil, err
	}
	senderMetaByID := make(map[uuid.UUID]senderMeta, len(pool.Explicit))
	for _, snd := range pool.Explicit {
		senderMetaByID[snd.Account.ID] = senderMeta{
			weight:           snd.Weight,
			rotationPosition: snd.RotationPosition,
			lastSentAt:       snd.LastSentAt,
			hasMeta:          true,
		}
	}
	return pool.Accounts, senderMetaByID, nil
}

// placeCampaignSend runs every hard constraint and pacing rule on one
// (contact, step) pair against the campaign's mailbox pool and returns the
// slot, exactly as the send path uses it. preview makes it read-only (no
// decision logs, no cached writes) so the contact drawer can ask "when would
// this step go" through the same rules the scheduler applies.
func (s *schedulerService) placeCampaignSend(ctx context.Context, campaign *models.Campaign, accounts []models.Email, senderMetaByID map[uuid.UUID]senderMeta, nextPair *repository.ContactSequencePair, pass *campaignPass, preview bool) (time.Time, *repository.ContactSequencePair, uuid.UUID, error) {
	campaignID := campaign.ID
	if pass == nil {
		pass = s.newCampaignPass(ctx, campaign, accounts)
	}
	logDecision := func(eventType, message string, metadata map[string]interface{}) {
		if !preview {
			s.logCampaignDecision(ctx, campaignID, eventType, message, metadata)
		}
	}
	logDecisionOnce := func(eventType, message string, metadata map[string]interface{}) {
		if !preview {
			s.logCampaignDecisionOnce(ctx, campaignID, eventType, message, metadata)
		}
	}

	// STEP 3.5: Resolve the recipient ESP/provider for ESP matching. Cheap:
	// prefer the cached contact.esp_provider, else derive from the domain
	// string. NEVER dial MX on the hot path. Empty => unknown => wildcard.
	// STEP 3.4: Recipient-timezone policy. Disabled, absent or unreadable all
	// leave the send on the sending mailbox's clock.
	sendPref := s.sendTimePreference(ctx, campaign.OrganizationID)

	// Loaded once: ESP matching and send-time optimization share the row.
	var recipientContact *models.Contact
	if (campaign.ESPMatchMode != "off" || sendPref.enabled) && s.contactRepo != nil {
		if contact, cerr := s.contactRepo.GetByID(ctx, nextPair.ContactID); cerr == nil {
			recipientContact = contact
		}
	}

	recipientProvider := ""
	if campaign.ESPMatchMode != "off" && recipientContact != nil {
		if recipientContact.ESPProvider != "" {
			recipientProvider = recipientContact.ESPProvider
		} else {
			recipientProvider = providerForEmailDomain(recipientContact.Email)
			// Opportunistically cache the derived provider (best-effort).
			if recipientProvider != "" && !preview {
				_ = s.contactRepo.SetContactESP(ctx, recipientContact.ID, recipientProvider)
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

	// Routing's own floor for this pair: the campaign's entry delay for a first
	// step, a preceding wait node's minutes for a follow-up. Honouring it here
	// keeps the placer from handing back a slot the router would refuse.
	if nextPair.NotBefore != nil && nextPair.NotBefore.After(baseTime) {
		baseTime = *nextPair.NotBefore
	}

	// An OVERDUE step's earliest possible send is now, never the moment it
	// became due. Every schedule gate below is asked about `baseTime`, and for
	// a follow-up whose wait elapsed hours ago that instant is in the past —
	// where nextScheduleSlot deliberately keeps an instant that was already
	// inside a sending window, and the end-date comparison finds a candidate
	// that predates the end date. So a step that came due at 2pm passed both
	// gates at 11pm: hardFloor sat in the past, STEP 14.5 read it as due, and
	// the task sent it hours after the window closed, or days after the
	// campaign ended. The window has to be asked about the send that is
	// actually about to happen, which is this one, now.
	if baseTime.Before(time.Now()) {
		baseTime = time.Now()
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

	// The organization's own posture. A restricted organization keeps sending at
	// a fraction of the volume (folded into pass.effectiveCap); a suspended one
	// does not send at all, and is stopped here because campaign sends never
	// pass through the manual send gate.
	if pass.risk.BlocksSending() {
		logDecision("org_suspended",
			"Sending is paused for this workspace while it is under review", nil)
		return s.deferToNextDay(campaign, preview), nil, accounts[0].ID, ErrCampaignDeferred
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

	// STEP 8: Build weighted account candidates. Each mailbox goes through the
	// same two-part gate routing's availability pre-pass used: what it can do at
	// all (authentication, cold rotation, warmup health, today's budget), then
	// when its own calendar next lets it send.
	//
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

	// Why mailboxes were left out. A reason that clears on its own (the budget
	// resets at midnight, the mailbox's hours reopen, a health hold expires)
	// makes an empty pool a deferral; only one that never clears makes it a
	// pause. authGated is counted apart so an empty pool can be reported as the
	// DNS problem it is rather than as a scheduling one, and reopensAt is the
	// earliest reopening among hours-closed mailboxes.
	//
	// gates keeps the same answer per mailbox, so the lead's own mailbox can be
	// told apart from the rest: busy today (wait for it) or not sending for this
	// campaign at all (move the lead off it).
	authGated := 0
	lifecycleGated := 0
	budgetSpent := 0
	hoursClosed := 0
	healthHeld := 0
	var reopensAt time.Time
	gates := map[uuid.UUID]mailboxGate{}

	// The mailbox this lead's conversation belongs to, when it is still one this
	// campaign can send from. Its gates are the same as everyone else's; what
	// differs is that a closed hour moves the send rather than dropping the
	// mailbox, because there is no second address to fall back to.
	bound := boundSender(accounts, nextPair.AssignedSender)

	// A lead can have steps but no recorded binding: it was removed from the
	// campaign and added back, which keeps its progress and starts a new lead
	// row. The address it last actually heard from is still the one to keep, so
	// it is PREFERRED — used when that mailbox is free, and given up when it is
	// not. It cannot be waited for the way a recorded binding is: routing does
	// not know about it, so a lead waiting on one would hold up every lead
	// behind it. The next send records it, and from then on it is a rule.
	prefer := bound
	if bound == nil && nextPair.AssignedSender == nil && !nextPair.IsNewLead {
		if last, lerr := s.campaignProgressRepo.LastSenderForLead(ctx, campaignID, nextPair.ContactID); lerr == nil {
			prefer = boundSender(accounts, last)
		}
	}

	var candidates []AccountCandidate
	for _, acct := range accounts {
		sentToday, err := s.sentTodayFor(ctx, pass, acct.ID)
		if err != nil {
			return time.Time{}, nil, uuid.Nil, err
		}

		acctLimit := pass.effectiveCap(acct)
		// Authentication, cold rotation, warmup health and today's budget, in
		// the order the gate applies them. Same answer routing's availability
		// pre-pass got, from the same reads.
		gate, remaining := s.gateFor(ctx, pass, acct, acctLimit-sentToday)
		if !gate.open() {
			gates[acct.ID] = gate
			switch gate.reason {
			case gateAuth:
				authGated++
			case gateResting:
				lifecycleGated++
			case gateHealth:
				healthHeld++
			case gateBudget:
				budgetSpent++
			}
			continue
		}

		// Where the mailbox may next send, on its OWN calendar. The lead's own
		// mailbox is allowed to WAIT for its next opening rather than drop out
		// of the pass: there is no second address this conversation could come
		// from, so a closed hour moves the send instead of refusing it.
		openAt, openLoc, remaining, wgate := s.windowFor(
			ctx, pass, acct, candidateTime, acctLimit, remaining, bound != nil && bound.ID == acct.ID)
		if !wgate.open() {
			gates[acct.ID] = wgate
			switch wgate.reason {
			case gateBudget:
				budgetSpent++
			case gateHours:
				hoursClosed++
				if reopensAt.IsZero() || wgate.reopensAt.Before(reopensAt) {
					reopensAt = wgate.reopensAt
				}
			}
			continue
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
			Behavior:       pass.behaviors[acct.ID],
			OpenAt:         openAt,
			OpenLoc:        openLoc,
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

	// Every mailbox in the pool was gated on authentication: say so, instead of
	// falling through to a message about sending windows and daily caps.
	//
	// Only this case is logged. A partially gated pool still sends, from the
	// mailboxes that passed, so there is no campaign-level event to report, and
	// logging one per scheduling pass would bury the activity feed under a row
	// every few minutes for a condition the mailbox badge, the Advisor card and
	// the notification already cover.
	if len(candidates) == 0 && authGated == len(accounts) {
		logDecision("domain_auth_gated",
			"No mailbox can send: every sending domain fails SPF/DMARC authentication",
			map[string]interface{}{"gated_mailboxes": authGated, "pool_size": len(accounts)})
		return time.Time{}, nil, uuid.Nil, ErrDomainAuthFailing
	}

	// An empty pool is a pause only when nothing in it will change on its own.
	// A pool with every mailbox at its daily cap used to fall through to the
	// pause below, and the campaign had to be restarted by hand the next
	// morning (issue #306); it is a deferral, like every other gate that lifts
	// by itself. The conditions a deferred chain re-finds every few minutes
	// until midnight are logged once a day, or the feed drowns in them.
	if len(candidates) == 0 {
		switch {
		case budgetSpent > 0 || hoursClosed > 0:
			// Resume when the first of them can send again: tomorrow for a
			// spent budget, the reopening of the mailbox's own 8am-8pm band
			// otherwise. A closed band is routine and short, so it earns no
			// line in the activity log; a pool whose every usable mailbox is
			// capped does.
			var resume time.Time
			if budgetSpent > 0 {
				resume = s.deferToNextDay(campaign, preview)
			}
			if hoursClosed > 0 {
				if open := nextScheduleSlot(reopensAt, windows, campaignTZ); resume.IsZero() || open.Before(resume) {
					resume = open
				}
			}
			if hoursClosed == 0 {
				logDecisionOnce("daily_cap_reached",
					"Every available mailbox has used its daily budget; sending resumes tomorrow",
					map[string]interface{}{"capped_mailboxes": budgetSpent, "pool_size": len(accounts)})
			}
			return resume, nil, accounts[0].ID, ErrCampaignDeferred
		case lifecycleGated == len(accounts):
			// Every mailbox is out of cold rotation. Say so rather than letting
			// the campaign look stalled for no visible reason; they return on
			// their own.
			logDecisionOnce("mailboxes_resting",
				"No mailbox is in cold rotation: all are resting or held in reserve",
				map[string]interface{}{"resting_mailboxes": lifecycleGated, "pool_size": len(accounts)})
			return s.deferToNextDay(campaign, preview), nil, accounts[0].ID, ErrCampaignDeferred
		case healthHeld > 0 || lifecycleGated > 0:
			var why []string
			if healthHeld > 0 {
				why = append(why, fmt.Sprintf("%d held by warmup health", healthHeld))
			}
			if lifecycleGated > 0 {
				why = append(why, fmt.Sprintf("%d resting or in reserve", lifecycleGated))
			}
			if authGated > 0 {
				why = append(why, fmt.Sprintf("%d failing domain authentication", authGated))
			}
			logDecisionOnce("mailboxes_unavailable",
				"No mailbox can send right now: "+strings.Join(why, ", "),
				map[string]interface{}{"health_held": healthHeld, "resting_mailboxes": lifecycleGated,
					"auth_gated": authGated, "pool_size": len(accounts)})
			return s.deferToNextDay(campaign, preview), nil, accounts[0].ID, ErrCampaignDeferred
		}
		// What is left was gated by a sending-behaviour profile with no working
		// days, which no amount of waiting fixes.
		return time.Time{}, nil, uuid.Nil, ErrNoEligibleMailbox
	}

	// STEP 8.2: The lead's own mailbox, if this pass left it able to send. Every
	// step of one conversation leaves from the address the contact first heard
	// from, so once this is set there is nothing left to choose: no rotation, no
	// ESP matching, no weighting. Those decide which mailbox STARTS a lead.
	boundCand := pickBound(candidates, prefer)

	// STEP 8.25: Apply ESP matching to the under-budget candidate set.
	//   strict → only matching mailboxes are eligible; if none, DEFER (never
	//            send cross-provider).
	//   prefer → restrict to matching mailboxes when at least one has capacity,
	//            otherwise fall back to the full eligible set (never starves).
	//
	// Skipped for a lead that already has its mailbox: matching picks a sender
	// for a first email, and re-applying it to a follow-up could only refuse
	// the one address this contact is allowed to hear from.
	if boundCand == nil && campaign.ESPMatchMode != "off" && recipientProvider != "" {
		matching := make([]AccountCandidate, 0, len(candidates))
		for _, c := range candidates {
			if c.ProviderMatch {
				matching = append(matching, c)
			}
		}
		switch campaign.ESPMatchMode {
		case "strict":
			if len(matching) == 0 {
				// No matching mailbox under budget today: leave THIS lead for
				// later rather than complete or send cross-provider. It is the
				// recipient's own domain that has no mailbox, so the lead behind
				// them may still be sendable; logged once a day, because the
				// pass now re-finds this for every refused lead.
				logDecisionOnce("provider_match_deferred",
					"No same-provider mailbox available; those leads wait for one",
					map[string]interface{}{"recipient_provider": recipientProvider})
				// Deferral, not a send: nil pair + sentinel so the caller reschedules
				// instead of sending this contact from a cross-provider mailbox.
				return s.deferToNextDay(campaign, preview), nil, accounts[0].ID, ErrLeadDeferred
			}
			candidates = matching
		case "prefer":
			if len(matching) > 0 {
				candidates = matching
			}
		}
	}

	// STEP 8.5: Select the mailbox. A lead already bound to one keeps it; only a
	// lead starting its sequence goes through the campaign's rotation mode. pool
	// is the set selection ran over, kept for the pacing maths below. Every
	// candidate has budget left today, so it has weight and the selector always
	// picks one; the guard only keeps a nil from being dereferenced.
	pool := candidates
	selected := boundCand
	if selected == nil {
		// The lead is bound to a mailbox that did not survive this pass. Waiting
		// is right only while the mailbox is coming back: a spent budget resets
		// at midnight, closed hours reopen. A mailbox that is disconnected,
		// failing authentication, resting or held by warmup health is not going
		// to write to this contact again soon, and a week of silence mid-sequence
		// is worse than a change of address, so the lead moves.
		if bound != nil {
			gate := gates[bound.ID]
			if gate.paced {
				logDecisionOnce("sender_busy",
					"Some leads are waiting for their own mailbox: every contact keeps the address they first heard from, and that mailbox has nothing left for now",
					map[string]interface{}{"mailbox": bound.Email, "reason": gate.reason})
				resume := gate.reopensAt
				if resume.IsZero() {
					resume = s.deferToNextDay(campaign, preview)
				}
				return resume, nil, bound.ID, ErrSenderBusy
			}
		}
		if preview {
			// Rotation is a draw, and a read that draws answers a different
			// mailbox — and so a different min-gap — every time it is called.
			// A preview picks the same one every time instead; which mailbox
			// rotation will really hand this lead is not knowable in advance
			// anyway, and the drawer is reporting a time, not a sender.
			selected = stableCandidate(candidates)
		} else {
			selected = selectAccountByRotationMode(campaign.RotationMode, candidates)
		}
	}
	if selected == nil {
		return time.Time{}, nil, uuid.Nil, ErrNoEligibleMailbox
	}

	account := &selected.Account

	// STEP 8.75: Send-to-send spacing for THIS send. With a behaviour profile
	// the gap is drawn fresh from the mailbox's range, so the intervals between
	// its sends are irregular; otherwise it is the mailbox's fixed min gap. A
	// preview takes the shortest gap the profile allows instead of a draw, so
	// it answers the same question the same way twice.
	var gapSeconds int
	if preview {
		gapSeconds = s.behaviorGapFloor(selected.Behavior, candidateTime, account.MinWaitTime)
	} else {
		gapSeconds = s.behaviorGap(selected.Behavior, candidateTime, account.MinWaitTime)
	}

	// leadFloor records that the hard floor below belongs to THIS lead and not
	// to the pool, so a refusal moves the pass to the next lead instead of
	// parking the campaign (issue #437).
	leadFloor := false

	// Move the candidate onto the mailbox's own workday before the spacing
	// maths below runs, so distribution is computed against the window the send
	// will actually land in.
	if selected.OpenAt != nil && selected.OpenAt.After(candidateTime) {
		candidateTime = *selected.OpenAt
		// Waiting for a mailbox to reopen is this lead's own wait only when the
		// lead is BOUND to it, for the reason spelled out at the min-gap below:
		// an unbound lead is one rotation places, and rotation gets another go.
		leadFloor = bound != nil && bound.ID == account.ID
	}

	// hardFloor is the earliest moment this send is ALLOWED: wait_after,
	// start date, sending windows, day capacity and workday placement so far,
	// with the mailbox min-gap folded in below. Pacing added after this point
	// (distribution, jitter, curve) shapes the slot but never gates a send.
	hardFloor := candidateTime

	// A preview reports that FLOOR and stops there. Steps 9 to 13 below are
	// spacing: drawn fresh from a random source and measured from time.Now(),
	// so running them for a read made two reads of one unchanged campaign
	// answer minutes apart, and the drawer showed a time that walked forward on
	// every refresh while the step sat there marked Due (issue #437). Every
	// real constraint — the mailbox min-gap, the recipient's hours, the sending
	// windows — still runs.

	// STEP 9: Even distribution across the candidate day's sending window. With
	// a behaviour profile that is the mailbox's own rolled workday (lunch
	// excluded); otherwise it is the span of the campaign's intervals for that
	// weekday.
	// The day's remaining sends belong to the POOL, not to the mailbox this tick
	// happens to have picked. A campaign is one self-perpetuating task, so this
	// interval is the campaign's whole send rate: pacing it by the selected
	// mailbox alone made a campaign with three mailboxes send at one mailbox's
	// rate and leave the other two idle, however high their caps were. Every
	// per-mailbox guard still binds afterwards — each mailbox's own daily cap
	// and hourly ceiling gated it into this set, and the min-gap plus conflict
	// resolution below space its own sends.
	if remainingEmails := poolRemainingOn(pool, candidateTime); !preview && remainingEmails > 0 {
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
		if hardFloor.Before(earliestNext) {
			hardFloor = nextScheduleSlot(earliestNext, windows, campaignTZ)
			// A bound lead has one address and must wait out ITS mailbox's gap,
			// which is this lead's wait and nobody else's: the lead behind it,
			// on another mailbox, can still go now.
			//
			// An unbound lead keeps the pool-wide answer. Selection does not
			// look at the min-gap, so rotation can hand an unbound lead a
			// mailbox that has just sent — but round_robin and
			// least_recently_used both pick the least-used mailbox, which is
			// the one that has NOT just sent, and weighted re-draws on the next
			// tick. Skipping the lead would spend the whole candidate budget
			// re-deriving one shared gap on a single-mailbox campaign, which is
			// most of them.
			if bound != nil && bound.ID == account.ID {
				leadFloor = true
			}
		}
	}

	// STEP 11: Add jitter, scaled to the placement it is perturbing. A flat
	// +/-20 minutes is wider than the interval STEP 9 just computed whenever the
	// pool is pacing tighter than 40 minutes apart, so the negative half landed
	// the slot in the past, notBefore clamped it to a few seconds from now, and
	// the day's spread collapsed onto the mailbox min-gap. Half the distance to
	// the slot, capped at the original 20 minutes, keeps the irregularity
	// without erasing the pacing. Deliberately NOT rounded to a 5-minute grid —
	// a fleet that only ever sends at :x0/:x5 marks is a detectable pattern.
	if !preview {
		if spread := min(int(time.Until(candidateTime).Minutes())/2, 20); spread > 0 {
			candidateTime = candidateTime.Add(time.Minute * time.Duration(randomJitter(-spread, spread)))
		}
	}
	candidateTime = notBefore(candidateTime)

	// STEP 12: Check conflicts with other scheduled tasks
	if !preview {
		scheduledTasks, terr := s.taskRepo.GetScheduledTasksForAccount(ctx, account.ID, candidateTime)
		if terr != nil {
			return time.Time{}, nil, uuid.Nil, terr
		}
		candidateTime = resolveConflicts(candidateTime, scheduledTasks, gapSeconds)
	}

	// STEP 13: Apply human-like distribution (favor morning/afternoon peaks).
	// Skipped for behaviour-profiled mailboxes: the profile already describes
	// this mailbox's workday and its own lunch break, and layering the generic
	// curve on top would drag sends away from the hours the customer chose.
	if !preview && !selected.Behavior.Enabled {
		candidateTime = applyDistributionCurve(candidateTime, campaignTZ)
	}

	// STEP 13.5: Aim the send at the RECIPIENT's business hours. hardFloor
	// moves with it, because the task handler sends whatever pair this returns
	// and a slot that did not gate would be a hint the send ignores.
	if sendPref.enabled {
		if snapped, ok := s.recipientSlot(ctx, selected.Behavior, candidateTime, windows, campaignTZ,
			recipientContact, sendPref, campaign.EndDate); ok && snapped.After(candidateTime) {
			candidateTime = snapped
			hardFloor = snapped
			// One recipient's morning is nobody else's: the lead behind this one
			// may be awake right now.
			leadFloor = true
		}
	}

	// STEP 14: Ensure still within a sending window after all adjustments
	// (jitter/conflict/distribution can push into a gap between intervals, or
	// out of the mailbox's workday). Satisfies both calendars.
	candidateTime = s.intersectWindows(ctx, selected.Behavior, notBefore(candidateTime), windows, campaignTZ)

	// STEP 14.5: A task can fire ahead of the step's hard constraints — an
	// early successor tick, a duplicate chain, a moved slot. The task handler
	// sends whatever pair this function returns, so returning a not-yet-due
	// pair here is what sends a "wait 3 days" follow-up seconds after step
	// one. Report it deferred instead: the caller reschedules at the computed
	// slot without sending. A task that fired at its own slot always passes.
	if time.Until(hardFloor) > config.CampaignNotDueGraceSeconds*time.Second {
		if leadFloor {
			return scheduledSlot(candidateTime, preview), nil, account.ID, ErrLeadDeferred
		}
		return scheduledSlot(candidateTime, preview), nil, account.ID, ErrCampaignDeferred
	}

	// STEP 15: Randomise the sub-minute component so sends never land on :00.
	return scheduledSlot(candidateTime, preview), nextPair, account.ID, nil
}

// stableCandidate is the preview's stand-in for rotation: the strongest
// candidate, ties broken by mailbox id so the same pool always answers with the
// same mailbox.
func stableCandidate(candidates []AccountCandidate) *AccountCandidate {
	var best *AccountCandidate
	for i := range candidates {
		c := &candidates[i]
		if best == nil || c.Weight > best.Weight ||
			(c.Weight == best.Weight && c.Account.ID.String() < best.Account.ID.String()) {
			best = c
		}
	}
	return best
}

// scheduledSlot is finalSlot for a real scheduled_at and a bare future clamp
// for a preview: the sub-minute randomisation exists so the fleet does not send
// at second :00, and a read-only answer that moved by up to a minute between
// two refreshes was reporting that jitter as if it were news.
func scheduledSlot(t time.Time, preview bool) time.Time {
	if preview {
		return notBefore(t)
	}
	return finalSlot(t)
}

// deferToNextDay pushes a candidate time to the next valid campaign day within
// the campaign's send window. Used by the ESP-strict, new-lead-cap and
// daily-cap deferral paths so a campaign reschedules instead of completing,
// pausing or busy-looping.
//
// A preview gets the FLOOR of the same answer instead: the first open minute of
// the campaign's next sending day. Two things made the scheduler's own value
// wrong to show in a drawer — it is measured from the instant it is asked
// ("24 hours from now", so 3pm today means 3pm tomorrow) and it then adds up to
// half an hour of jitter so a fleet of deferred chains does not all wake
// together. Neither means anything to a reader, and between them they moved the
// drawer's "not before" on every refresh for the commonest waiting reason
// there is: every mailbox having spent its daily budget (issue #437). The
// floor is the honest answer anyway, because what the step is waiting for is
// the day rolling over, not the wake-up the chain happens to have picked.
func (s *schedulerService) deferToNextDay(campaign *models.Campaign, preview bool) time.Time {
	tz := loadLocation(campaign.Timezone)
	windows := effectiveWindows(campaign)
	if preview {
		local := time.Now().In(tz)
		midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, tz).AddDate(0, 0, 1)
		return nextScheduleSlot(midnight, windows, tz)
	}
	t := nextScheduleSlot(time.Now().Add(24*time.Hour), windows, tz)
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

// logCampaignDecisionOnce is logCampaignDecision for a condition the deferred
// chain re-finds on every wake-up until the day rolls over: one line per UTC
// day, the day the budgets reset on. Best-effort and nil-safe.
func (s *schedulerService) logCampaignDecisionOnce(ctx context.Context, campaignID uuid.UUID, eventType, message string, metadata map[string]interface{}) {
	if s.campaignLogRepo == nil {
		return
	}
	if metadata == nil {
		metadata = map[string]interface{}{}
	}
	dayStart := time.Now().UTC().Truncate(24 * time.Hour)
	day := dayStart.Format("2006-01-02")
	metadata["day"] = day
	_, _ = s.campaignLogRepo.CreateLogOnce(ctx, &repository.CampaignLogEntry{
		CampaignID: campaignID,
		EventType:  eventType,
		Message:    message,
		Metadata:   metadata,
	}, "day", day, dayStart)
}
