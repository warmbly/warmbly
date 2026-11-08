package scheduler

import (
	"context"
	"sync"
	"time"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/geo"
	"github.com/warmbly/warmbly/internal/repository"
)

type namedZone struct {
	name string
	loc  *time.Location
}

// recipientZones is every zone a lead's location can resolve to, loaded once.
var recipientZones = sync.OnceValue(func() []namedZone {
	names := geo.ResolvableZones()
	out := make([]namedZone, 0, len(names))
	for _, n := range names {
		if loc, err := time.LoadLocation(n); err == nil {
			out = append(out, namedZone{name: n, loc: loc})
		}
	}
	return out
})

// windowOpenAt reports whether the schedule lets a send go at now on loc's clock.
func windowOpenAt(now time.Time, windows models.ScheduleWindows, loc *time.Location) bool {
	return !nextScheduleSlot(now, windows, loc).After(now)
}

// openClocks names the lead clocks a recipient-local campaign can send on at
// now, so routing tries those leads first. Nil for any other campaign, which
// keeps routing's order exactly as it was.
func openClocks(campaign *models.Campaign, now time.Time) *repository.OpenClocks {
	if !campaign.SendsInRecipientTimezone() {
		return nil
	}
	windows := effectiveWindows(campaign)
	open := &repository.OpenClocks{Zones: []string{}}
	for _, z := range recipientZones() {
		if windowOpenAt(now, windows, z.loc) {
			open.Zones = append(open.Zones, z.name)
		}
	}
	open.Unknown = windowOpenAt(now, windows, loadLocation(campaign.ClockTimezone()))
	return open
}

// scheduleClock is the zone a lead's send is placed on: the lead's own under
// a recipient-local campaign when their record says where they are, the
// campaign's otherwise.
func scheduleClock(campaign *models.Campaign, campaignTZ *time.Location, contact *models.Contact) *time.Location {
	if !campaign.SendsInRecipientTimezone() {
		return campaignTZ
	}
	if loc, ok := models.RecipientLocation(contact); ok {
		return loc
	}
	return campaignTZ
}

// recipientClockRecheck bounds how long a recipient-local campaign waits on its
// leads' hours before routing again.
const recipientClockRecheck = 15 * time.Minute

// planDayWindow is the send plan's calendar for today. A recipient-local
// campaign's day is the union of its leads' windows: open while any lead's
// is, closing with the last of them.
func (s *schedulerService) planDayWindow(ctx context.Context, campaign *models.Campaign, now time.Time) (models.CampaignSendWindow, int, time.Time) {
	base, baseLeft, baseCloses := dayWindow(campaign, now)
	if !campaign.SendsInRecipientTimezone() || campaign.OrganizationID == nil {
		return base, baseLeft, baseCloses
	}
	leads, err := s.campaignRepo.LeadTimezones(ctx, *campaign.OrganizationID, campaign.ID)
	if err != nil || len(leads.Zones) == 0 {
		return base, baseLeft, baseCloses
	}
	zones := make([]string, 0, len(leads.Zones)+1)
	for _, z := range leads.Zones {
		zones = append(zones, z.Timezone)
	}
	if leads.Unknown > 0 {
		zones = append(zones, campaign.ClockTimezone())
	}

	out := models.CampaignSendWindow{StartsAt: base.StartsAt, EndsAt: base.EndsAt}
	var closes time.Time
	sum := 0
	for _, zone := range zones {
		c := *campaign
		c.Timezone = zone
		w, left, cl := dayWindow(&c, now)
		sum += left
		out.SendingDay = out.SendingDay || w.SendingDay
		out.OpenNow = out.OpenNow || w.OpenNow
		if w.OpensAt != nil && (out.OpensAt == nil || w.OpensAt.Before(*out.OpensAt)) {
			out.OpensAt = w.OpensAt
		}
		if w.ClosesAt != nil && cl.After(closes) {
			closes = cl
			out.ClosesAt = w.ClosesAt
		}
	}
	if out.OpenNow {
		out.OpensAt = nil
	}
	if closes.IsZero() {
		closes = baseCloses
	}
	// Today's budget ends at midnight UTC, whatever clock a lead is on.
	if reset := now.UTC().Truncate(24 * time.Hour).Add(24 * time.Hour); closes.After(reset) {
		closes = reset
		out.ClosesAt = &reset
	}
	left := min(sum, max(0, int(closes.Sub(now).Seconds())))
	out.MinutesLeft = left / 60
	return out, left, closes
}
