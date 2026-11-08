package scheduler

import (
	"slices"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/models"
)

// A weekday 9:00-17:00 campaign on a New York clock, asked at 15:00 UTC on a
// Wednesday: 10:00 in New York, 16:00 in Berlin, 00:00 in Tokyo.
func recipientCampaign(mode models.TimezoneMode) (*models.Campaign, time.Time) {
	c := &models.Campaign{Timezone: "America/New_York", TimezoneMode: mode, StartTime: "09:00", EndTime: "17:00", Days: 0b0011111}
	return c, time.Date(2026, 11, 11, 15, 0, 0, 0, time.UTC)
}

func TestOpenClocksNamesTheZonesWhoseDayIsOpen(t *testing.T) {
	c, now := recipientCampaign(models.TimezoneModeRecipient)
	open := openClocks(c, now)
	if open == nil {
		t.Fatal("a recipient-local campaign has no open clocks")
	}
	for _, z := range []string{"America/New_York", "America/Chicago", "Europe/Berlin", "Europe/London"} {
		if !slices.Contains(open.Zones, z) {
			t.Errorf("%s is inside 9-17 at %s but not open", z, now)
		}
	}
	for _, z := range []string{"Asia/Tokyo", "Australia/Sydney", "Pacific/Honolulu"} {
		if slices.Contains(open.Zones, z) {
			t.Errorf("%s is outside 9-17 at %s but open", z, now)
		}
	}
	if !open.Unknown {
		t.Error("the campaign's own clock is open, so a lead with no zone should be too")
	}
}

func TestOpenClocksLeavesCampaignClockCampaignsAlone(t *testing.T) {
	c, now := recipientCampaign(models.TimezoneModeCampaign)
	if open := openClocks(c, now); open != nil {
		t.Fatalf("a campaign-clock campaign reordered routing: %+v", open)
	}
}

func TestScheduleClockFollowsTheLead(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	tokyo := &models.Contact{Email: "k@example.com", CustomFields: map[string]string{"country": "Japan"}}
	unknown := &models.Contact{Email: "x@example.com"}

	rc, _ := recipientCampaign(models.TimezoneModeRecipient)
	if got := scheduleClock(rc, ny, tokyo); got.String() != "Asia/Tokyo" {
		t.Errorf("recipient mode placed a Japanese lead on %s", got)
	}
	if got := scheduleClock(rc, ny, unknown); got != ny {
		t.Errorf("a lead with no location must fall back to the campaign clock, got %s", got)
	}
	if got := scheduleClock(rc, ny, nil); got != ny {
		t.Errorf("an unloaded contact must fall back to the campaign clock, got %s", got)
	}
	cc, _ := recipientCampaign(models.TimezoneModeCampaign)
	if got := scheduleClock(cc, ny, tokyo); got != ny {
		t.Errorf("campaign mode followed the lead to %s", got)
	}
}

// The slot a Tokyo lead gets is Tokyo's 9am, which is the evening before in
// New York: the campaign window is the lead's, not the sender's.
func TestRecipientClockPlacesTheSlotInTheLeadsMorning(t *testing.T) {
	c, now := recipientCampaign(models.TimezoneModeRecipient)
	ny, _ := time.LoadLocation("America/New_York")
	tokyo := &models.Contact{CustomFields: map[string]string{"timezone": "JST"}}
	loc := scheduleClock(c, ny, tokyo)
	slot := nextScheduleSlot(now, effectiveWindows(c), loc).In(loc)
	if slot.Hour() != 9 || slot.Weekday() != time.Thursday {
		t.Fatalf("Tokyo lead slotted at %s, want Thursday 09:00 Tokyo", slot)
	}
}
