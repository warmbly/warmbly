package scheduler

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/behavior"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// A lead's sending mailbox is fixed for its whole sequence: rotation picks the
// mailbox for the first email and every follow-up leaves from the same address
// (issue #401). Three emails from three addresses is not a conversation, and
// the reply lands in a mailbox the contact was never written to from.
//
// That only works if the two passes agree on what "this mailbox can send" means:
// routing skips a lead whose mailbox is busy, and placement then has to be
// willing to send from it. Both read gateFor and windowFor, over facts this
// file collects once per pass, so a lead can never be told to wait for a
// mailbox placement would have refused, nor moved off one it would have used.

// mailboxGate is why a mailbox cannot take a cold send right now.
type mailboxGate struct {
	// reason is "" when the mailbox can send. Otherwise it names the gate, in
	// the vocabulary the placer's empty-pool activity lines already count.
	reason string
	// paced is true when the gate lifts on its own and soon: today's budget
	// resets at midnight, the mailbox's hours reopen in the morning. A lead
	// bound to a paced mailbox waits for it.
	//
	// A gate that is NOT paced (disconnected, failing domain authentication,
	// resting, held by warmup health) is a mailbox that is not going to serve
	// this campaign in the near future, so its leads are moved to another one
	// rather than left silent for a week.
	paced bool
	// reopensAt is when a paced mailbox is expected back; zero when unknown,
	// which the caller reads as "tomorrow".
	reopensAt time.Time
}

func (g mailboxGate) open() bool { return g.reason == "" }

// Gate reasons. They double as the metadata the empty-pool activity-log lines
// count, so keep them stable.
const (
	gateAuth      = "domain_auth"
	gateResting   = "resting"
	gateHealth    = "health"
	gateBudget    = "budget"
	gateHours     = "hours"
	gateNoWorkday = "no_working_day"
)

// campaignPass holds everything about the mailbox pool that one scheduling pass
// resolves once: the batch lookups, and the per-mailbox reads memoized so the
// availability pre-pass and the placement that follows it cost one query each
// rather than two.
type campaignPass struct {
	campaign        *models.Campaign
	coldRamp        map[uuid.UUID]repository.ColdRampState
	lifecycles      map[uuid.UUID]models.SendLifecycleState
	lifecyclesKnown bool
	behaviors       map[uuid.UUID]behavior.Resolved
	enforceAuth     bool
	authGrace       time.Duration
	risk            models.OrgRiskState

	sentToday map[uuid.UUID]int
	health    map[uuid.UUID]healthRead
}

// healthRead is one mailbox's warmup health, as the gate reads it.
type healthRead struct {
	state        models.WarmupHealthState
	blockedUntil *time.Time
	known        bool
}

// newCampaignPass resolves the pool-wide state for one scheduling pass.
func (s *schedulerService) newCampaignPass(ctx context.Context, campaign *models.Campaign, accounts []models.Email) *campaignPass {
	enforceAuth, authGrace := s.domainAuthGate(ctx)
	lifecycles, known := s.sendLifecycles(ctx, accounts)
	return &campaignPass{
		campaign:        campaign,
		coldRamp:        s.coldRampStates(ctx, accounts),
		lifecycles:      lifecycles,
		lifecyclesKnown: known,
		behaviors:       s.behaviorForAll(ctx, accounts),
		enforceAuth:     enforceAuth,
		authGrace:       authGrace,
		risk:            s.orgRiskState(ctx, campaign.OrganizationID),
		sentToday:       map[uuid.UUID]int{},
		health:          map[uuid.UUID]healthRead{},
	}
}

// effectiveCap is the per-mailbox cold cap for THIS campaign, after the ramp,
// graduation and org-risk clamps. Applied via min() only — it can never RAISE a
// mailbox above its own cold cap (the mailbox-first safety invariant).
func (p *campaignPass) effectiveCap(acct models.Email) int {
	c := p.campaign
	lim := min(acct.CampaignLimit, c.DailyLimit)
	if c.RampEnabled {
		lim = min(lim, campaignRampCeiling(true, c.RampStart, c.RampIncrement, c.RampCeiling, c.RampLevel))
	}
	// Graduation ceiling: a mailbox at its warmup ceiling must not reach the
	// full cold cap the day it joins a campaign.
	lim = min(lim, coldCeilingFor(p.coldRamp[acct.ID], lim))
	if m := p.risk.CapMultiplier(); m < 1 {
		risked := int(float64(lim)*m + 0.5)
		// A restricted organization still sends, just far less. Zeroing it here
		// would stop the campaign without ever saying why; suspension is the
		// band that stops sending, and it does so at the send gate.
		if risked < 1 {
			risked = 1
		}
		lim = min(lim, risked)
	}
	return lim
}

// sentTodayFor is the mailbox's campaign sends so far today, read once per pass.
func (s *schedulerService) sentTodayFor(ctx context.Context, p *campaignPass, id uuid.UUID) (int, error) {
	if n, ok := p.sentToday[id]; ok {
		return n, nil
	}
	n, err := s.taskRepo.CountCampaignEmailsSentToday(ctx, id)
	if err != nil {
		return 0, err
	}
	p.sentToday[id] = n
	return n, nil
}

// healthFor is the mailbox's warmup health, read once per pass. An unreadable
// state is "unknown" and gates nothing, exactly as the inline read it replaces.
func (s *schedulerService) healthFor(ctx context.Context, p *campaignPass, id uuid.UUID) healthRead {
	if h, ok := p.health[id]; ok {
		return h
	}
	h := healthRead{}
	if s.warmupRepo != nil {
		if state, until, err := s.warmupRepo.GetHealthState(ctx, id); err == nil {
			h = healthRead{state: state, blockedUntil: until, known: true}
		}
	}
	p.health[id] = h
	return h
}

// gateFor is the ONE answer to "can this mailbox take a cold send for this
// campaign right now", over the gates that depend on the mailbox alone and not
// on which lead is being placed. The time-of-day gates (a mailbox's own hours,
// a behaviour profile's workday) are deliberately not here: they move a send
// forward rather than refusing it, and the placer applies them against the slot
// it is actually building.
//
// remaining is today's budget left, already read; it is returned adjusted for a
// health band that dampens rather than blocks.
func (s *schedulerService) gateFor(ctx context.Context, p *campaignPass, acct models.Email, remaining int) (mailboxGate, int) {
	// Sending-domain authentication first, because unauthenticated mail is
	// rejected outright by Gmail/Yahoo/Outlook: no amount of budget, window or
	// rotation makes it deliverable. Only a SUSTAINED failure gates — "unknown"
	// and a failure inside the grace window both pass, so a resolver hiccup
	// never stops a campaign.
	if p.enforceAuth && acct.DomainAuthBlocked(time.Now(), p.authGrace) {
		return mailboxGate{reason: gateAuth}, 0
	}
	// Not in cold rotation. A resting mailbox keeps its warmup traffic and its
	// reputation; it just is not offered cold sends, and its leads move on.
	if p.lifecyclesKnown && !p.lifecycles[acct.ID].State.SendsCold() {
		return mailboxGate{reason: gateResting}, 0
	}
	// The SAME warmup health state pool selection uses, so a mailbox in
	// deliverability trouble does not keep blasting cold volume:
	//   - quarantined/blocked (still within blocked_until) → no sends at all
	//   - watch/throttled → dampen today's budget by the band's multiplier
	if h := s.healthFor(ctx, p, acct.ID); h.known {
		switch h.state {
		case models.WarmupHealthQuarantined, models.WarmupHealthBlocked:
			if h.blockedUntil == nil || h.blockedUntil.After(time.Now()) {
				return mailboxGate{reason: gateHealth}, 0
			}
		case models.WarmupHealthWatch, models.WarmupHealthThrottled:
			remaining = int(float64(remaining) * adjustmentFor(h.state).volumeMultiplier)
		}
	}
	if remaining <= 0 {
		return mailboxGate{reason: gateBudget, paced: true}, 0
	}
	return mailboxGate{}, remaining
}

// windowFor places a mailbox on its OWN calendar for a send aimed at `at`, and
// is the second half of the gate: the part that depends on the clock rather
// than on the mailbox's standing. With a behaviour profile that is its rolled
// workday (start, lunch, end, working weekdays, hourly ceiling and the day's
// rolled volume); without one it is the historical 8am-8pm business-hours band.
//
// wait asks it to move the send to the mailbox's next opening instead of
// refusing it. Only the lead's own mailbox gets that: for everyone else the
// pass simply picks another mailbox, but a bound lead has no other address.
//
// It returns the opening instant to place the send at (nil when the mailbox is
// free now), the timezone that instant's DAY is counted in, the day's remaining
// budget re-based onto the day the send will land on, and the gate.
func (s *schedulerService) windowFor(ctx context.Context, p *campaignPass, acct models.Email, at time.Time, acctLimit, remaining int, wait bool) (*time.Time, *time.Location, int, mailboxGate) {
	bhv := p.behaviors[acct.ID]
	if bhv.Enabled {
		openAt, ok := s.placeWithinBehavior(ctx, bhv, at)
		if !ok {
			// A profile with no working days at all: nothing to wait for.
			return nil, nil, 0, mailboxGate{reason: gateNoWorkday}
		}
		// Budget the send against the day it will actually land on. When today
		// is spent, placeWithinBehavior has already walked to a later day, and
		// that day starts with the mailbox's full cold cap.
		if !sameLocalDay(openAt, time.Now(), bhv.Loc) {
			remaining = acctLimit
		}
		// The day's rolled cold budget, folded in by min(): a persona can only
		// lower a mailbox's remaining sends, never lift it above the cold cap.
		remaining = s.behaviorDailyCap(ctx, bhv, remaining, openAt)
		if remaining <= 0 {
			return nil, nil, 0, mailboxGate{reason: gateBudget, paced: true}
		}
		return &openAt, bhv.Loc, remaining, mailboxGate{}
	}
	if acct.Timezone == "" || acct.Timezone == p.campaign.Timezone {
		return nil, nil, remaining, mailboxGate{}
	}
	loc := loadLocation(acct.Timezone)
	if h := at.In(loc).Hour(); h >= 8 && h < 20 {
		return nil, nil, remaining, mailboxGate{}
	}
	open := businessHoursReopen(at, loc)
	if !wait {
		return nil, nil, 0, mailboxGate{reason: gateHours, paced: true, reopensAt: open}
	}
	if !sameLocalDay(open, time.Now(), loc) {
		remaining = acctLimit
	}
	return &open, loc, remaining, mailboxGate{}
}

// pacedSenders is the availability pre-pass routing consults: the pool
// mailboxes that cannot take a send right now but will be able to on their own.
// A lead already bound to one waits for it instead of parking every lead behind
// it; a lead bound to a mailbox that is NOT here is offered to the placer,
// which moves it.
//
// A mailbox outside its own sending hours is paced too, so a campaign spanning
// timezones keeps serving the leads whose mailbox is awake.
func (s *schedulerService) pacedSenders(ctx context.Context, p *campaignPass, accounts []models.Email) repository.PacedSenders {
	paced := repository.PacedSenders{}
	now := time.Now()
	for _, acct := range accounts {
		sentToday, err := s.sentTodayFor(ctx, p, acct.ID)
		if err != nil {
			// Unknown budget is not a reason to strand a lead: leave the
			// mailbox unpaced and let the placer decide with a real read.
			continue
		}
		acctLimit := p.effectiveCap(acct)
		gate, remaining := s.gateFor(ctx, p, acct, acctLimit-sentToday)
		if gate.open() {
			// Same clock gates the placement pass applies, asked about now.
			_, _, _, gate = s.windowFor(ctx, p, acct, now, acctLimit, remaining, false)
		}
		// Held, not paced, means its leads are moved rather than left waiting,
		// so only a paced mailbox is recorded here.
		if gate.paced {
			back := gate.reopensAt
			if back.IsZero() {
				back = s.deferToNextDay(p.campaign, false)
			}
			paced[acct.ID] = back
		}
	}
	return paced
}

// boundSender finds the lead's bound mailbox in the campaign's pool. Not found
// means the mailbox is no longer one this campaign can send from at all — it
// was disconnected, or taken off the campaign's sending accounts — and the lead
// is moved to another one.
func boundSender(accounts []models.Email, id *uuid.UUID) *models.Email {
	if id == nil {
		return nil
	}
	for i := range accounts {
		if accounts[i].ID == *id {
			return &accounts[i]
		}
	}
	return nil
}

// pickBound returns the candidate for the lead's own mailbox, or nil when it
// did not survive this pass's gates. Rotation is not consulted: the address the
// contact has already heard from is not a choice to re-make on every step.
func pickBound(candidates []AccountCandidate, acct *models.Email) *AccountCandidate {
	if acct == nil {
		return nil
	}
	for i := range candidates {
		if candidates[i].Account.ID == acct.ID && candidates[i].Weight > 0 {
			return &candidates[i]
		}
	}
	return nil
}
