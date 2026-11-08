package models

import (
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/pkg/geo"
)

// TimezoneMode is the clock a campaign's sending schedule is read on.
type TimezoneMode string

const (
	// TimezoneModeCampaign reads the schedule in the campaign's own timezone.
	TimezoneModeCampaign TimezoneMode = "campaign"
	// TimezoneModeRecipient reads it in each lead's timezone, worked out from
	// their record, and in the campaign's when a lead's is not known.
	TimezoneModeRecipient TimezoneMode = "recipient"
)

// Valid reports whether m is one of the stored values.
func (m TimezoneMode) Valid() bool {
	return m == TimezoneModeCampaign || m == TimezoneModeRecipient
}

// SendsInRecipientTimezone reports whether the schedule follows each lead's clock.
func (c *Campaign) SendsInRecipientTimezone() bool {
	return c.TimezoneMode == TimezoneModeRecipient
}

// ContactLocation is where a contact is, read from its record the same way
// the location sweep reads it, so the scheduler never waits for the sweep.
func ContactLocation(c *Contact) geo.Location {
	if c == nil {
		return geo.Location{}
	}
	return geo.Resolve(geo.Input{Fields: c.CustomFields, Phone: c.Phone, Email: c.Email})
}

// RecipientLocation is the zone a lead's schedule is read in under
// TimezoneModeRecipient, and whether it came from the lead at all.
func RecipientLocation(c *Contact) (*time.Location, bool) {
	l := ContactLocation(c)
	if !l.Known() {
		return nil, false
	}
	loc, err := time.LoadLocation(l.Timezone)
	if err != nil {
		return nil, false
	}
	return loc, true
}

// CampaignLeadTimezones is where a campaign's leads are, by the timezone
// their records resolve to, so the schedule can be checked against them
// before launch. GET /campaigns/:id/lead-timezones
type CampaignLeadTimezones struct {
	CampaignID   uuid.UUID    `json:"campaign_id"`
	TimezoneMode TimezoneMode `json:"timezone_mode"`
	// Timezone is the campaign's own clock, the one an unknown lead is sent on.
	Timezone string `json:"timezone"`
	Total    int    `json:"total"`
	// Unknown is the leads nothing on the record places.
	Unknown int `json:"unknown"`
	// Resolving is the leads whose record changed and has not been read again
	// yet; they are counted under their previous answer meanwhile.
	Resolving int                `json:"resolving"`
	Zones     []CampaignLeadZone `json:"zones"`
}

// CampaignLeadZone is one timezone and how many of the campaign's leads are in it.
type CampaignLeadZone struct {
	Timezone string `json:"timezone"`
	// Country is the zone's country, "" for UTC and fixed offsets.
	Country string `json:"country"`
	Leads   int    `json:"leads"`
}
