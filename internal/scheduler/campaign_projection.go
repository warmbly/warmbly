package scheduler

import (
	"context"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/behavior"
	"github.com/warmbly/warmbly/internal/app/warmupramp"
	"github.com/warmbly/warmbly/internal/bitmask"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

const (
	// projectionHorizonDays bounds the walk; an audience that needs longer
	// reports no finish date rather than a meaningless one.
	projectionHorizonDays = 2 * 366
	// projectionExactDays are simulated mailbox by mailbox. Graduation and
	// health holds settle well inside them, so later days repeat the last week.
	projectionExactDays = 56
	// otherCampaignLookbackDays is the history other campaigns' demand is read from.
	otherCampaignLookbackDays = 7
	// defaultWarmupStartMinute and defaultWarmupEndMinute stand in for a
	// mailbox with no warmup window of its own.
	defaultWarmupStartMinute = 8 * 60
	defaultWarmupEndMinute   = 18 * 60
)

// CampaignProjectionInput is a campaign that may not exist yet, its pool and
// its audience. Campaign carries the schedule, limit and timezone the wizard
// chose; Recipients and StepWaits describe who gets how many emails.
type CampaignProjectionInput struct {
	Campaign      *models.Campaign
	Accounts      []models.Email
	Recipients    int
	StepWaits     []int
	OrgDailyLimit int
}

// span is a half-open stretch of wall time.
type span struct{ from, to time.Time }

func spanSeconds(ss []span) int {
	total := 0
	for _, s := range ss {
		if s.to.After(s.from) {
			total += int(s.to.Sub(s.from).Seconds())
		}
	}
	return total
}

// intersectSpans keeps the time covered by both lists.
func intersectSpans(a, b []span) []span {
	var out []span
	for _, x := range a {
		for _, y := range b {
			from, to := x.from, x.to
			if y.from.After(from) {
				from = y.from
			}
			if y.to.Before(to) {
				to = y.to
			}
			if to.After(from) {
				out = append(out, span{from, to})
			}
		}
	}
	return out
}

// clipSpans drops everything before at.
func clipSpans(ss []span, at time.Time) []span {
	var out []span
	for _, s := range ss {
		if !s.to.After(at) {
			continue
		}
		if s.from.Before(at) {
			s.from = at
		}
		out = append(out, s)
	}
	return out
}

// localBand is [startMin, endMin) on each local day around day, in loc. Three
// days cover any offset between loc and the campaign's clock.
func localBand(day time.Time, loc *time.Location, startMin, endMin int) []span {
	if endMin <= startMin {
		return nil
	}
	local := day.In(loc)
	out := make([]span, 0, 3)
	for d := -1; d <= 1; d++ {
		mid := time.Date(local.Year(), local.Month(), local.Day()+d, 0, 0, 0, 0, loc)
		out = append(out, span{mid.Add(time.Duration(startMin) * time.Minute), mid.Add(time.Duration(endMin) * time.Minute)})
	}
	return out
}

// campaignSpans is the campaign's sending time on the local day starting at
// midnight: its windows for that weekday, or the whole day when it has none.
func campaignSpans(windows models.ScheduleWindows, midnight time.Time) []span {
	if windows.IsEmpty() {
		return []span{{midnight, midnight.AddDate(0, 0, 1)}}
	}
	var out []span
	for _, iv := range windows[int(midnight.Weekday())] {
		if iv.End > iv.Start {
			out = append(out, span{midnight.Add(time.Duration(iv.Start) * time.Minute), midnight.Add(time.Duration(iv.End) * time.Minute)})
		}
	}
	return out
}

// projectedSender is one mailbox's standing for the projection.
type projectedSender struct {
	acct     models.Email
	state    string
	held     bool
	heldTill *time.Time
	cap      int // mailbox cap after the campaign limit and workspace risk
	capLoss  int // sends the campaign limit took from the mailbox cap
	riskLoss int
	cold     repository.ColdRampState
	hasCold  bool
	health   models.WarmupHealthState
	other    float64
	loc      *time.Location
	bhv      behavior.Resolved
	sentAll  int
}

// senderDay is one mailbox's day with the clamps that shaped it.
type senderDay struct {
	sends, warmup                                          int
	graduation, other, health, spacing, behaviorLoss, held int
}

// warmupOnDay is the warmup mail a mailbox sends on its local day while it
// backs a live campaign, and the spans it is spread over.
func warmupOnDay(p *projectedSender, localNoon time.Time) (int, []span) {
	a := p.acct
	if !a.IsWarmingActive() || p.state == models.EstimateSenderHealthHold {
		return 0, nil
	}
	if a.WarmupDays > 0 && !bitmask.HasWeekday(uint8(a.WarmupDays), localNoon.Weekday()) {
		return 0, nil
	}
	days := 0
	if a.Warmup != nil {
		days = max(0, int(localNoon.Sub(*a.Warmup).Hours()/24))
	}
	n := warmupramp.Target(true, a.WarmupBase, a.WarmupIncrease, a.WarmupMax, days, true)
	n = warmupramp.Apply(n, warmupramp.HealthVolumeMultiplier(p.health))
	startMin, endMin := parseTimeOfDay(a.WarmupStartTime), parseTimeOfDay(a.WarmupEndTime)
	if p.bhv.Enabled {
		plan := p.bhv.PlanOn(behavior.PlanDateFor(localNoon, p.bhv.Loc))
		startMin, endMin = plan.WorkStartMinute, plan.WorkEndMinute
	}
	if startMin == 0 && endMin <= startMin {
		startMin, endMin = defaultWarmupStartMinute, defaultWarmupEndMinute
	}
	if endMin <= startMin {
		endMin = defaultWarmupEndMinute
	}
	loc := p.loc
	if p.bhv.Enabled && p.bhv.Loc != nil {
		loc = p.bhv.Loc
	}
	local := localNoon.In(loc)
	mid := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	return n, []span{{mid.Add(time.Duration(startMin) * time.Minute), mid.Add(time.Duration(endMin) * time.Minute)}}
}

// projectSenderDay walks one mailbox through one campaign day. spans is the
// campaign's sending time that day, already clipped to the start; rampStart is
// when the mailbox's cold ramp starts climbing if it has not yet; today marks
// the day that already has sends on the ledger.
func projectSenderDay(p *projectedSender, spans []span, midnight, rampStart time.Time, today bool) senderDay {
	var d senderDay
	noon := midnight.Add(12 * time.Hour)
	// Warmup keeps running on the side, on every warmup day, on the same
	// spacing clock as campaign sends.
	w, wSpans := warmupOnDay(p, noon.In(p.loc))
	d.warmup = w
	if len(spans) == 0 {
		return d
	}
	c := p.cap
	if p.held {
		if p.heldTill == nil || !p.heldTill.Before(spans[len(spans)-1].to) {
			d.held = c
			return d
		}
		spans = clipSpans(spans, *p.heldTill)
	}

	// Graduation: a warmed mailbox climbs from its starting volume.
	if p.hasCold && p.cold.WarmupStartedAt != nil {
		start := rampStart
		if p.cold.ColdRampStartedAt != nil {
			start = *p.cold.ColdRampStartedAt
		}
		warmupDays := max(0, int(noon.Sub(*p.cold.WarmupStartedAt).Hours()/24))
		if g := warmupramp.ColdCeiling(warmupDays, start, p.cold.Placements, noon, c, p.cold.ConfirmedReplies); g < c {
			d.graduation = c - g
			c = g
		}
	}

	// The mailbox's own clock: a behaviour profile's workday, or the 8am-8pm
	// band in its own timezone when that differs from the campaign's.
	gap := p.acct.MinWaitTime
	if p.bhv.Enabled {
		plan := p.bhv.PlanOn(behavior.PlanDateFor(noon, p.bhv.Loc))
		if !plan.IsWorkingDay {
			d.behaviorLoss = c
			return d
		}
		work := localBand(noon, p.bhv.Loc, plan.WorkStartMinute, plan.WorkEndMinute)
		if plan.HasLunch() {
			var cut []span
			for _, wk := range work {
				mid := time.Date(wk.from.Year(), wk.from.Month(), wk.from.Day(), 0, 0, 0, 0, p.bhv.Loc)
				ls := mid.Add(time.Duration(*plan.LunchStartMinute) * time.Minute)
				le := mid.Add(time.Duration(*plan.LunchEndMinute) * time.Minute)
				cut = append(cut, span{wk.from, ls}, span{le, wk.to})
			}
			work = cut
		}
		spans = intersectSpans(spans, work)
		limit := plan.DailyLimit
		if plan.HourlyLimit > 0 {
			limit = min(limit, plan.HourlyLimit*((spanSeconds(spans)+3599)/3600))
		}
		if limit < c {
			d.behaviorLoss = c - limit
			c = limit
		}
		if g := (plan.GapMinSeconds + plan.GapMaxSeconds) / 2; g > 0 {
			gap = g
		}
	} else if p.acct.Timezone != "" && p.loc.String() != midnight.Location().String() {
		spans = intersectSpans(spans, localBand(noon, p.loc, 8*60, 20*60))
	}
	warmupInWindow := 0
	if w > 0 {
		if total := spanSeconds(wSpans); total > 0 {
			warmupInWindow = int(math.Round(float64(w) * float64(spanSeconds(intersectSpans(wSpans, spans))) / float64(total)))
		}
	}

	// Other campaigns share the mailbox cap and its clock. Today's ledger
	// already holds what everyone sent; later days use the recent average.
	other := int(math.Round(p.other))
	if today {
		other = p.sentAll
	}
	room := max(0, c-other)
	d.other = c - room
	if m := adjustmentFor(p.health).volumeMultiplier; m < 1 {
		next := int(float64(room) * m)
		d.health = room - next
		room = next
	}

	secs := spanSeconds(spans)
	slots := room
	if secs <= 0 {
		slots = 0
	} else if gap > 0 {
		slots = secs/gap + 1 - warmupInWindow
		if !today {
			slots -= other
		}
	}
	slots = max(0, slots)
	if slots < room {
		d.spacing = room - slots
		room = slots
	}
	d.sends = room
	return d
}

// ProjectCampaign simulates a campaign day by day: every mailbox's cap under
// the same clamps the send path applies (campaign limit, workspace risk,
// warmup graduation, health bands and holds, other campaigns, sending
// behaviour, spacing against the warmup mail running alongside), and the
// audience moving through its steps as that capacity allows, follow-ups first.
func (s *schedulerService) ProjectCampaign(ctx context.Context, in CampaignProjectionInput) (*models.CampaignEstimateResult, error) {
	campaign := in.Campaign
	if campaign == nil {
		campaign = &models.Campaign{}
	}
	now := time.Now()
	out := &models.CampaignEstimateResult{
		Recipients: in.Recipients,
		Mailboxes:  len(in.Accounts),
		Steps:      1 + len(in.StepWaits),
		Timeline:   []models.CampaignEstimateDay{},
		Senders:    []models.CampaignEstimateSender{},
	}
	out.TotalSends = out.Recipients * out.Steps

	pass := s.newCampaignPass(ctx, campaign, in.Accounts)
	_ = s.prefillPool(ctx, pass, in.Accounts)
	ids := make([]uuid.UUID, 0, len(in.Accounts))
	for _, a := range in.Accounts {
		ids = append(ids, a.ID)
	}
	var otherAvg map[uuid.UUID]float64
	if len(ids) > 0 {
		if got, err := s.taskRepo.CampaignSendsPerDayByAccounts(ctx, ids, otherCampaignLookbackDays); err == nil {
			otherAvg = got
		}
	}

	riskMul := pass.risk.CapMultiplier()
	senders := make([]*projectedSender, 0, len(in.Accounts))
	otherPerDay := 0.0
	for _, acct := range in.Accounts {
		p := &projectedSender{acct: acct, loc: loadLocation(acct.ClockTimezone()), bhv: pass.behaviors[acct.ID], other: otherAvg[acct.ID]}
		otherPerDay += p.other
		p.cap = acct.CampaignLimit
		if campaign.DailyLimit > 0 && campaign.DailyLimit < p.cap {
			p.capLoss = p.cap - campaign.DailyLimit
			p.cap = campaign.DailyLimit
		}
		if riskMul < 1 {
			r := max(1, int(float64(p.cap)*riskMul+0.5))
			if r < p.cap {
				p.riskLoss = p.cap - r
				p.cap = r
			}
		}
		p.cold = pass.coldRamp[acct.ID].WithKnownWarmup(acct.Warmup)
		p.hasCold = p.cold.WarmupStartedAt != nil
		h := s.healthFor(ctx, pass, acct.ID)
		if h.known {
			p.health = h.state
		}
		p.sentAll, _ = s.sentTodayFor(ctx, pass, acct.ID)

		gate, _ := s.gateFor(ctx, pass, acct, 1)
		switch gate.reason {
		case gateAuth:
			p.held, p.state = true, models.EstimateSenderDomainAuth
		case gateResting:
			p.held, p.state = true, models.EstimateSenderResting
		case gateHealth:
			p.held, p.state = true, models.EstimateSenderHealthHold
			p.heldTill = h.blockedUntil
		case gateNoWorker:
			p.state = models.EstimateSenderNoWorker
		}
		if p.state == "" {
			switch p.health {
			case models.WarmupHealthThrottled, models.WarmupHealthWatch:
				p.state = models.EstimateSenderThrottled
			default:
				p.state = models.EstimateSenderReady
			}
		}
		senders = append(senders, p)
	}
	out.OtherCampaignsPerDay = int(math.Round(otherPerDay))

	// The calendar, in the campaign's clock.
	loc := loadLocation(campaign.ClockTimezone())
	windows := effectiveWindows(campaign)
	start := now
	if campaign.StartDate != nil && campaign.StartDate.After(now) {
		start = *campaign.StartDate
	}
	startLocal := start.In(loc)
	startDay := time.Date(startLocal.Year(), startLocal.Month(), startLocal.Day(), 0, 0, 0, 0, loc)
	nowLocal := now.In(loc)
	today := time.Date(nowLocal.Year(), nowLocal.Month(), nowLocal.Day(), 0, 0, 0, 0, loc)
	startsToday := startDay.Equal(today)

	orgRoom := -1
	orgRoomToday := -1
	if in.OrgDailyLimit >= 0 {
		orgRoom = max(0, in.OrgDailyLimit-int(math.Round(otherPerDay)))
		orgRoomToday = orgRoom
		if campaign.OrganizationID != nil {
			if sent, err := s.campaignProgressRepo.CountEmailsSentTodayByOrganization(ctx, *campaign.OrganizationID); err == nil {
				orgRoomToday = max(0, in.OrgDailyLimit-sent)
			}
		}
	}

	exact := make([][]senderDay, projectionExactDays)
	poolExact := make([]int, projectionExactDays)
	warmupExact := make([]int, projectionExactDays)
	sendingExact := make([]bool, projectionExactDays)
	firstFull := -1
	var lossOrg int
	for i := 0; i < projectionExactDays; i++ {
		mid := time.Date(startDay.Year(), startDay.Month(), startDay.Day()+i, 0, 0, 0, 0, loc)
		spans := campaignSpans(windows, mid)
		isToday := i == 0 && startsToday
		if i == 0 {
			spans = clipSpans(spans, start)
		}
		sendingExact[i] = len(spans) > 0
		row := make([]senderDay, len(senders))
		total := 0
		for j, p := range senders {
			row[j] = projectSenderDay(p, spans, mid, startDay, isToday)
			total += row[j].sends
			warmupExact[i] += row[j].warmup
		}
		if pass.risk.BlocksSending() {
			total = 0
		}
		room := orgRoom
		if isToday {
			room = orgRoomToday
		}
		if room >= 0 && total > room {
			if firstFull < 0 && !isToday && sendingExact[i] {
				lossOrg = total - room
			}
			total = room
		}
		exact[i] = row
		poolExact[i] = total
		if firstFull < 0 && !isToday && sendingExact[i] {
			firstFull = i
		}
	}

	// Steady state is the last simulated week; later days repeat it.
	capacityOn := func(i int) int {
		if i < projectionExactDays {
			return poolExact[i]
		}
		return poolExact[projectionExactDays-7+(i-projectionExactDays)%7]
	}
	for i := projectionExactDays - 7; i < projectionExactDays; i++ {
		out.SteadyCapacity = max(out.SteadyCapacity, poolExact[i])
	}
	if fullCapacityEvidenceKnown(senders) && out.SteadyCapacity > 0 && firstFull >= 0 && poolExact[firstFull] < out.SteadyCapacity {
		for i := firstFull; i < projectionExactDays; i++ {
			if sendingExact[i] && poolExact[i] >= out.SteadyCapacity {
				at := time.Date(startDay.Year(), startDay.Month(), startDay.Day()+i, 0, 0, 0, 0, loc)
				out.FullCapacityAt = &at
				break
			}
		}
	}

	// Mailbox by mailbox, and the clamp that costs the pool the most.
	losses := map[string]int{}
	if firstFull >= 0 {
		losses[models.EstimateBottleneckOrgDailyLimit] = lossOrg
		if pass.risk.BlocksSending() {
			losses[models.EstimateBottleneckWorkspaceRisk] = math.MaxInt32
		}
	}
	for j, p := range senders {
		steady := 0
		for i := projectionExactDays - 7; i < projectionExactDays; i++ {
			steady = max(steady, exact[i][j].sends)
		}
		first := 0
		if firstFull >= 0 {
			d := exact[firstFull][j]
			first = d.sends
			losses[models.EstimateBottleneckCampaignLimit] += p.capLoss
			losses[models.EstimateBottleneckWorkspaceRisk] += p.riskLoss
			losses[models.EstimateBottleneckGraduation] += d.graduation
			losses[models.EstimateBottleneckOtherCampaigns] += d.other
			losses[models.EstimateBottleneckHealth] += d.health
			losses[models.EstimateBottleneckSpacing] += d.spacing
			losses[models.EstimateBottleneckSendingBehavior] += d.behaviorLoss
			losses[models.EstimateBottleneckHeld] += d.held
			if d.graduation > 0 {
				out.Ramping++
				if p.state == models.EstimateSenderReady {
					p.state = models.EstimateSenderRamping
				}
			}
		}
		if p.held {
			out.Held++
		}
		w := 0
		for i := 0; i < 7 && i < projectionExactDays; i++ {
			w = max(w, exact[i][j].warmup)
		}
		if w > 0 {
			out.Warmup.Mailboxes++
			out.Warmup.PerDay += w
		}
		if len(out.Senders) >= models.CampaignEstimateSendersMax {
			continue
		}
		sender := models.CampaignEstimateSender{
			ID: p.acct.ID, Email: p.acct.Email, Provider: p.acct.Provider, State: p.state,
			FirstDayCap: first, SteadyCap: steady, WarmupPerDay: w,
		}
		if firstFull >= 0 && first < steady {
			for i := firstFull; i < projectionExactDays; i++ {
				if exact[i][j].sends >= steady {
					at := time.Date(startDay.Year(), startDay.Month(), startDay.Day()+i, 0, 0, 0, 0, loc)
					sender.FullCapAt = &at
					break
				}
			}
		}
		out.Senders = append(out.Senders, sender)
	}
	biggest := 0
	for _, kind := range []string{
		models.EstimateBottleneckWorkspaceRisk, models.EstimateBottleneckHeld, models.EstimateBottleneckCampaignLimit,
		models.EstimateBottleneckGraduation, models.EstimateBottleneckOtherCampaigns, models.EstimateBottleneckHealth,
		models.EstimateBottleneckSendingBehavior, models.EstimateBottleneckSpacing, models.EstimateBottleneckOrgDailyLimit,
	} {
		if losses[kind] > biggest {
			biggest, out.Bottleneck = losses[kind], kind
		}
	}

	if out.Recipients <= 0 {
		return out, nil
	}
	sim := simulateLeads(capacityOn, out.Recipients, in.StepWaits, projectionHorizonDays)
	for i, d := range sim.days {
		if i >= models.CampaignEstimateTimelineMax {
			break
		}
		at := time.Date(startDay.Year(), startDay.Month(), startDay.Day()+i, 0, 0, 0, 0, loc)
		warm := 0
		sending := true
		if i < projectionExactDays {
			warm = warmupExact[i]
			sending = sendingExact[i]
		} else {
			k := projectionExactDays - 7 + (i-projectionExactDays)%7
			warm, sending = warmupExact[k], sendingExact[k]
		}
		out.Timeline = append(out.Timeline, models.CampaignEstimateDay{
			Date: at.Format("2006-01-02"), SendingDay: sending, Capacity: capacityOn(i),
			Sends: d.first + d.followUps, FirstEmails: d.first, FollowUps: d.followUps, Warmup: warm,
		})
	}
	if sim.firstTouchDay >= 0 {
		at := time.Date(startDay.Year(), startDay.Month(), startDay.Day()+sim.firstTouchDay, 0, 0, 0, 0, loc)
		out.FirstTouchFinishAt = &at
	}
	if sim.finishDay >= 0 {
		at := time.Date(startDay.Year(), startDay.Month(), startDay.Day()+sim.finishDay, 0, 0, 0, 0, loc)
		out.EstimatedFinishAt = &at
		n := sim.sendingDays
		out.SendingDays = &n
	}
	return out, nil
}

func fullCapacityEvidenceKnown(senders []*projectedSender) bool {
	for _, p := range senders {
		if p.hasCold && p.cold.WarmupStartedAt != nil && coldCeilingFor(p.cold, p.cap) < p.cap {
			return false
		}
	}
	return true
}

type simDay struct{ first, followUps int }

type leadSim struct {
	days          []simDay
	firstTouchDay int
	finishDay     int
	sendingDays   int
}

// simulateLeads moves an audience through its steps against a daily capacity:
// each day due follow-ups go first, oldest step first, then new contacts; a
// contact's next step comes due waits[k] days after the step before it. A
// wait of zero can go out the same day. finishDay is -1 when the horizon ends
// first.
func simulateLeads(capacity func(day int) int, recipients int, in []int, horizon int) leadSim {
	steps := 1 + len(in)
	waits := make([]int, len(in))
	maxWait := 0
	for i, w := range in {
		waits[i] = max(0, w)
		maxWait = max(maxWait, waits[i])
	}
	due := make([][]int, steps)
	for k := 1; k < steps; k++ {
		due[k] = make([]int, horizon+maxWait+1)
	}
	backlog := make([]int, steps)
	fresh := recipients
	outstanding := recipients * steps
	res := leadSim{firstTouchDay: -1, finishDay: -1}
	for i := 0; i < horizon && outstanding > 0; i++ {
		c := max(0, capacity(i))
		var day simDay
		for k := 1; k < steps; k++ {
			backlog[k] += due[k][i]
			due[k][i] = 0
		}
		for {
			progressed := false
			for k := steps - 1; k >= 1 && c > 0; k-- {
				n := min(c, backlog[k])
				if n == 0 {
					continue
				}
				backlog[k] -= n
				c -= n
				day.followUps += n
				outstanding -= n
				progressed = true
				if k+1 < steps {
					if waits[k] == 0 {
						backlog[k+1] += n
					} else {
						due[k+1][i+waits[k]] += n
					}
				}
			}
			if n := min(c, fresh); n > 0 {
				fresh -= n
				c -= n
				day.first += n
				outstanding -= n
				progressed = true
				if steps > 1 {
					if waits[0] == 0 {
						backlog[1] += n
					} else {
						due[1][i+waits[0]] += n
					}
				}
			}
			if !progressed || c == 0 {
				break
			}
		}
		res.days = append(res.days, day)
		if day.first+day.followUps > 0 {
			res.sendingDays++
		}
		if fresh == 0 && res.firstTouchDay < 0 {
			res.firstTouchDay = i
		}
		if outstanding == 0 {
			res.finishDay = i
		}
	}
	return res
}
