package analytics

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// MaxWarmupPlacementDays bounds one placement report.
const MaxWarmupPlacementDays = 366

// WireWarmupPlacement attaches the placement history.
func (s *analyticsService) WireWarmupPlacement(r repository.WarmupPlacementRepository) {
	s.placementRepo = r
}

// WarmupPlacementAware is the optional capability the caller uses to attach it.
type WarmupPlacementAware interface {
	WireWarmupPlacement(r repository.WarmupPlacementRepository)
}

// placementRates is every mailbox's rolling rate, nil when unavailable.
func (s *analyticsService) placementRates(ctx context.Context, orgID uuid.UUID, emailID *uuid.UUID) map[uuid.UUID]models.WarmupPlacementRate {
	if s.placementRepo == nil {
		return nil
	}
	rates, err := s.placementRepo.Rates(ctx, orgID, emailID, placementWindowStart(time.Now().UTC()))
	if err != nil {
		return nil
	}
	return rates
}

// placementWindowStart is the first UTC day of the trailing window ending on now's day.
func placementWindowStart(now time.Time) time.Time {
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	return day.AddDate(0, 0, -(models.WarmupPlacementWindowDays - 1))
}

// applyWarmupPlacement caps the health score at the measured inbox rate, so a
// mailbox landing in spam reads as degraded before the pool acts on it.
func applyWarmupPlacement(health *models.AccountHealth, r *models.WarmupPlacementRate) {
	if r == nil || r.InboxRate == nil {
		return
	}
	if capped := int(math.Floor(*r.InboxRate)); capped < health.Score {
		health.Score = capped
	}
	if *r.InboxRate >= models.WarmupPlacementGoodRate {
		return
	}
	if health.Status == "healthy" {
		health.Status = "warning"
	}
	health.Issues = append(health.Issues, fmt.Sprintf("Warmup inbox rate is %.0f%% over the last %d days", *r.InboxRate, r.WindowDays))
}

func (s *analyticsService) GetWarmupPlacement(ctx context.Context, orgID uuid.UUID, emailID *uuid.UUID, from, to time.Time) (*models.WarmupPlacementReport, *errx.Error) {
	from = time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	to = time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, time.UTC)
	if to.Before(from) {
		return nil, errx.New(errx.BadRequest, "from must be on or before to")
	}
	if int(to.Sub(from).Hours()/24)+1 > MaxWarmupPlacementDays {
		return nil, errx.New(errx.BadRequest, fmt.Sprintf("a placement report covers at most %d days", MaxWarmupPlacementDays))
	}

	// Every mailbox the report may name, which also proves emailID is the org's.
	names := make(map[uuid.UUID]string)
	if emailID != nil {
		email, xerr := s.emailRepo.Get(ctx, orgID.String(), emailID.String())
		if xerr != nil {
			return nil, xerr
		}
		names[email.ID] = email.Email
	} else {
		boxes, xerr := s.emailRepo.Search(ctx, orgID.String(), "", nil, nil, 1000, nil)
		if xerr != nil {
			return nil, xerr
		}
		for _, b := range boxes.Data {
			names[b.ID] = b.Email
		}
	}

	report := &models.WarmupPlacementReport{
		EmailAccountID: emailID,
		DateRange:      models.DateRange{From: from, To: to},
		Rate:           models.NewWarmupPlacementRate(0, 0, 0),
		Daily:          make([]models.WarmupPlacementDay, 0),
		Providers:      make([]models.WarmupPlacementProvider, 0),
	}
	if s.placementRepo == nil {
		return report, nil
	}

	// The rolling rate on the first day reads the window before it.
	lookback := from.AddDate(0, 0, -(models.WarmupPlacementWindowDays - 1))
	dayRows, err := s.placementRepo.Daily(ctx, orgID, emailID, lookback, to)
	if err != nil {
		return nil, errx.InternalError()
	}
	hostRows, err := s.placementRepo.Hosts(ctx, orgID, emailID, from, to)
	if err != nil {
		return nil, errx.InternalError()
	}
	sentRows, err := s.placementRepo.Sent(ctx, orgID, emailID, from, to)
	if err != nil {
		return nil, errx.InternalError()
	}
	cutoff := time.Now().Add(-models.WarmupUnconfirmedAfterHours * time.Hour)
	unconfirmedRows, err := s.placementRepo.Unconfirmed(ctx, orgID, emailID, from, to, cutoff)
	if err != nil {
		return nil, errx.InternalError()
	}
	rates, err := s.placementRepo.Rates(ctx, orgID, emailID, placementWindowStart(time.Now().UTC()))
	if err != nil {
		return nil, errx.InternalError()
	}

	b := newPlacementBuilder(from, to)
	for _, r := range dayRows {
		b.addDelivery(r)
	}
	for _, r := range sentRows {
		b.addCount(r, func(c *models.WarmupPlacementCounts, n int) { c.Sent += n })
	}
	for _, r := range unconfirmedRows {
		b.addCount(r, func(c *models.WarmupPlacementCounts, n int) { c.Unconfirmed += n })
	}

	report.Daily = b.days()
	for _, d := range report.Daily {
		report.Summary.Add(d.WarmupPlacementCounts)
	}
	report.Summary.Finish()
	report.Providers = placementProviders(hostRows)

	var total [3]int
	for _, r := range rates {
		total[0] += r.Inbox
		total[1] += r.Tabs
		total[2] += r.Spam
	}
	report.Rate = models.NewWarmupPlacementRate(total[0], total[1], total[2])

	if emailID == nil {
		report.Mailboxes = b.mailboxes(names, rates)
	}
	return report, nil
}

// placementBuilder folds the rollup rows into days, groups and senders.
type placementBuilder struct {
	dates   []string
	index   map[string]int
	day     []models.WarmupPlacementCounts
	groups  []map[string]*models.WarmupPlacementGroupCounts
	senders map[uuid.UUID]*senderPlacement
	// window holds every day's deliveries from the lookback on, for the rolling rate.
	window map[string][3]int
	first  time.Time
}

type senderPlacement struct {
	total models.WarmupPlacementCounts
	days  []models.WarmupPlacementCounts
}

func newPlacementBuilder(from, to time.Time) *placementBuilder {
	b := &placementBuilder{
		index:   make(map[string]int),
		senders: make(map[uuid.UUID]*senderPlacement),
		window:  make(map[string][3]int),
		first:   from,
	}
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		b.index[key] = len(b.dates)
		b.dates = append(b.dates, key)
	}
	b.day = make([]models.WarmupPlacementCounts, len(b.dates))
	b.groups = make([]map[string]*models.WarmupPlacementGroupCounts, len(b.dates))
	return b
}

func (b *placementBuilder) sender(id uuid.UUID) *senderPlacement {
	sp, ok := b.senders[id]
	if !ok {
		sp = &senderPlacement{days: make([]models.WarmupPlacementCounts, len(b.dates))}
		b.senders[id] = sp
	}
	return sp
}

func (b *placementBuilder) addDelivery(r repository.WarmupPlacementDayRow) {
	w := b.window[r.Date]
	w[0] += r.Inbox
	w[1] += r.Tabs
	w[2] += r.Spam
	b.window[r.Date] = w

	i, ok := b.index[r.Date]
	if !ok {
		return
	}
	add := func(c *models.WarmupPlacementCounts) {
		c.Inbox += r.Inbox
		c.Tabs += r.Tabs
		c.Spam += r.Spam
		c.Rescued += r.Rescued
	}
	add(&b.day[i])
	sp := b.sender(r.SenderID)
	add(&sp.total)
	add(&sp.days[i])

	if b.groups[i] == nil {
		b.groups[i] = make(map[string]*models.WarmupPlacementGroupCounts)
	}
	g, ok := b.groups[i][r.Group]
	if !ok {
		g = &models.WarmupPlacementGroupCounts{Group: r.Group}
		b.groups[i][r.Group] = g
	}
	g.Inbox += r.Inbox
	g.Tabs += r.Tabs
	g.Spam += r.Spam
	g.Rescued += r.Rescued
}

func (b *placementBuilder) addCount(r repository.WarmupSenderDayCount, apply func(*models.WarmupPlacementCounts, int)) {
	i, ok := b.index[r.Date]
	if !ok {
		return
	}
	apply(&b.day[i], r.Count)
	sp := b.sender(r.SenderID)
	apply(&sp.total, r.Count)
	apply(&sp.days[i], r.Count)
}

func (b *placementBuilder) days() []models.WarmupPlacementDay {
	out := make([]models.WarmupPlacementDay, len(b.dates))
	for i, date := range b.dates {
		c := b.day[i]
		c.Finish()
		groups := make([]models.WarmupPlacementGroupCounts, 0, len(b.groups[i]))
		for _, key := range models.WarmupRecipientGroups {
			if g, ok := b.groups[i][key]; ok {
				groups = append(groups, *g)
			}
		}
		out[i] = models.WarmupPlacementDay{
			Date:                  date,
			WarmupPlacementCounts: c,
			RollingInboxRate:      b.rolling(i),
			Groups:                groups,
		}
	}
	return out
}

// rolling is the trailing-window inbox rate ending on day i.
func (b *placementBuilder) rolling(i int) *float64 {
	end := b.first.AddDate(0, 0, i)
	var sum [3]int
	for k := 0; k < models.WarmupPlacementWindowDays; k++ {
		w := b.window[end.AddDate(0, 0, -k).Format("2006-01-02")]
		sum[0] += w[0]
		sum[1] += w[1]
		sum[2] += w[2]
	}
	return models.NewWarmupPlacementRate(sum[0], sum[1], sum[2]).InboxRate
}

func (b *placementBuilder) mailboxes(names map[uuid.UUID]string, rates map[uuid.UUID]models.WarmupPlacementRate) []models.WarmupPlacementMailbox {
	out := make([]models.WarmupPlacementMailbox, 0, len(b.senders))
	for id, sp := range b.senders {
		email, ok := names[id]
		if !ok {
			continue
		}
		total := sp.total
		total.Finish()
		daily := make([]*float64, len(sp.days))
		for i, d := range sp.days {
			d.Finish()
			daily[i] = d.InboxRate
		}
		rate, ok := rates[id]
		if !ok {
			rate = models.NewWarmupPlacementRate(0, 0, 0)
		}
		out = append(out, models.WarmupPlacementMailbox{
			EmailAccountID:        id,
			Email:                 email,
			WarmupPlacementCounts: total,
			Rate:                  rate,
			DailyInboxRate:        daily,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, c := out[i], out[j]
		if (a.Rate.InboxRate == nil) != (c.Rate.InboxRate == nil) {
			return a.Rate.InboxRate != nil
		}
		if a.Rate.InboxRate != nil && *a.Rate.InboxRate != *c.Rate.InboxRate {
			return *a.Rate.InboxRate < *c.Rate.InboxRate
		}
		if a.Delivered != c.Delivered {
			return a.Delivered > c.Delivered
		}
		return a.Email < c.Email
	})
	return out
}

// placementProviders groups the window's host rows by recipient group, in
// display order, busiest host first.
func placementProviders(rows []repository.WarmupPlacementHostRow) []models.WarmupPlacementProvider {
	byGroup := make(map[string]*models.WarmupPlacementProvider)
	for _, r := range rows {
		p, ok := byGroup[r.Group]
		if !ok {
			p = &models.WarmupPlacementProvider{Group: r.Group, Hosts: make([]models.WarmupPlacementHost, 0)}
			byGroup[r.Group] = p
		}
		c := models.WarmupPlacementCounts{Inbox: r.Inbox, Tabs: r.Tabs, Spam: r.Spam, Rescued: r.Rescued}
		p.Add(c)
		c.Finish()
		p.Hosts = append(p.Hosts, models.WarmupPlacementHost{Host: r.Host, WarmupPlacementCounts: c})
	}
	out := make([]models.WarmupPlacementProvider, 0, len(byGroup))
	for _, key := range models.WarmupRecipientGroups {
		p, ok := byGroup[key]
		if !ok {
			continue
		}
		p.Finish()
		sort.SliceStable(p.Hosts, func(i, j int) bool { return p.Hosts[i].Delivered > p.Hosts[j].Delivered })
		out = append(out, *p)
	}
	return out
}
