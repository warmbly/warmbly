package validate

import (
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/warmbly/warmbly/internal/bitmask"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func CampaignName(name string) *errx.Error {
	l := len(name)
	if l < 3 || l > 50 {
		return errx.ErrCampaignName
	}
	return nil
}

func CampaignDescription(description string) *errx.Error {
	if len(description) > 300 {
		return errx.ErrCampaignDescription
	}
	return nil
}

func CampaignDailyLimit(val int) *errx.Error {
	if val < config.CampaignDailyLimitMin || val > config.LimitMax {
		return errx.ErrCampaignDailyLimit
	}
	return nil
}

// CampaignStartDate accepts today and future dates. The dashboard's date
// picker sends midnight in the user's timezone, so "today" always sits a few
// hours in the past at submit time; a 24h grace keeps "today = start now"
// working from any timezone while still rejecting genuinely past dates.
func CampaignStartDate(date time.Time) *errx.Error {
	if date.Before(time.Now().Add(-24 * time.Hour)) {
		return errx.ErrCampaignStartDate
	}
	return nil
}

func CampaignEndDate(date time.Time) *errx.Error {
	if !date.After(time.Now()) {
		return errx.ErrCampaignEndDate
	}
	return nil
}

func CampaignDays(days uint8) *errx.Error {
	if err := bitmask.ValidateDaysMask(days); err != nil {
		return errx.ErrBitmask
	}
	return nil
}

func CampaignTime(input string) *errx.Error {
	_, err := time.Parse("15:04", input)
	if err != nil {
		return errx.ErrTime
	}
	return nil
}

// CampaignScheduleWindows validates a per-day sending schedule: each interval
// must sit within the day (0..1440 minutes) with start < end, and no day may
// carry an unreasonable number of windows. Overlaps are allowed (the scheduler
// resolves them); ordering is not required.
func CampaignScheduleWindows(w *models.ScheduleWindows) *errx.Error {
	if w == nil {
		return nil
	}
	for _, day := range w {
		if len(day) > 8 {
			return errx.New(errx.BadRequest, "a day may have at most 8 sending windows")
		}
		for _, iv := range day {
			if iv.Start < 0 || iv.End > 1440 || iv.Start >= iv.End {
				return errx.New(errx.BadRequest, "invalid sending window: 0 <= start < end <= 1440")
			}
		}
	}
	return nil
}

// ── Net-new send-control validators ──────────────────────────────────────

func CampaignSenderStrategy(s string) *errx.Error {
	if s != "tags" && s != "explicit" {
		return errx.ErrInvalid
	}
	return nil
}

func CampaignRotationMode(s string) *errx.Error {
	switch s {
	case "weighted", "round_robin", "least_recently_used":
		return nil
	}
	return errx.ErrInvalid
}

// CampaignFollowUpSender validates which mailbox a lead's follow-ups use.
func CampaignFollowUpSender(s string) *errx.Error {
	if !models.FollowUpSender(s).Valid() {
		return errx.New(errx.BadRequest, "follow_up_sender must be one of same, same_domain, rotate")
	}
	return nil
}

func CampaignSenderWeight(w int) *errx.Error {
	if w < 1 || w > 100 {
		return errx.New(errx.BadRequest, "sender weight must be between 1 and 100")
	}
	return nil
}

// CampaignRamp validates the ramp config. The ceiling<=daily_limit cross-check
// is intentionally NOT enforced here: the scheduler applies the ramp via
// min(daily_limit, ramp_ceiling, per-mailbox cap), so a ceiling above the
// daily limit can only be clamped down, never over-send.
func CampaignRamp(start, increment, ceiling int) *errx.Error {
	if start < 1 || start > config.LimitMax {
		return errx.New(errx.BadRequest, fmt.Sprintf("ramp start must be between 1 and %d", config.LimitMax))
	}
	if increment < 0 || increment > 100 {
		return errx.New(errx.BadRequest, "ramp increment must be between 0 and 100")
	}
	if ceiling < 1 || ceiling > config.LimitMax {
		return errx.New(errx.BadRequest, fmt.Sprintf("ramp ceiling must be between 1 and %d", config.LimitMax))
	}
	if start > ceiling {
		return errx.New(errx.BadRequest, "ramp start cannot exceed ramp ceiling")
	}
	return nil
}

func CampaignESPMatchMode(s string) *errx.Error {
	switch s {
	case "off", "prefer", "strict":
		return nil
	}
	return errx.ErrInvalid
}

func CampaignMaxNewLeads(v int) *errx.Error {
	if v < 0 || v > 1000 {
		return errx.New(errx.BadRequest, "max new leads per day must be between 0 and 1000")
	}
	return nil
}

// CampaignEntryDelay validates the delay held before a contact's first email,
// in minutes. 0 means the first email is due the moment the contact enters the
// campaign; the ceiling mirrors the column's CHECK constraint.
func CampaignEntryDelay(v int) *errx.Error {
	if v < 0 || v > CampaignEntryDelayMaxMinutes {
		return errx.New(errx.BadRequest, "delay before the first email must be between 0 minutes and 90 days")
	}
	return nil
}

// CampaignEntryDelayMaxMinutes is the ceiling for the entry delay (90 days),
// kept in step with the entry_delay_minutes CHECK constraint.
const CampaignEntryDelayMaxMinutes = 90 * 24 * 60

// CampaignTrackingDomain validates a campaign-scoped tracking-domain override.
// Empty means "fall back to the mailbox/default domain". Otherwise it has to be
// a bare hostname, by the same rule the mailbox field uses.
func CampaignTrackingDomain(host string) *errx.Error {
	if host == "" {
		return nil
	}
	if !TrackingHostname(host) {
		return errx.New(errx.BadRequest, "invalid tracking domain")
	}
	return nil
}

// CampaignUTMValue validates one of the campaign's UTM overrides. Empty means
// "use the default". Values are query-string parameters, so they must be
// short, single-line and printable; encoding is the send path's job.
func CampaignUTMValue(v string) *errx.Error {
	if utf8.RuneCountInString(v) > 128 {
		return errx.New(errx.BadRequest, "utm values must be 128 characters or fewer")
	}
	for _, r := range v {
		if r < 0x20 || r == 0x7f {
			return errx.New(errx.BadRequest, "utm values cannot contain control characters")
		}
	}
	return nil
}

func CampaignTimezoneMode(s string) *errx.Error {
	if !models.TimezoneMode(s).Valid() {
		return errx.New(errx.BadRequest, "timezone_mode must be one of campaign, recipient")
	}
	return nil
}
