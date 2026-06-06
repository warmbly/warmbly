package validate

import (
	"net"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/bitmask"
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
	if val < 3 || val > 100 {
		return errx.ErrCampaignDailyLimit
	}
	return nil
}

func CampaignStartDate(date time.Time) *errx.Error {
	if !date.After(time.Now()) {
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
	if start < 1 || start > 100 {
		return errx.New(errx.BadRequest, "ramp start must be between 1 and 100")
	}
	if increment < 0 || increment > 100 {
		return errx.New(errx.BadRequest, "ramp increment must be between 0 and 100")
	}
	if ceiling < 1 || ceiling > 100 {
		return errx.New(errx.BadRequest, "ramp ceiling must be between 1 and 100")
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

// CampaignTrackingDomain validates a campaign-scoped tracking-domain override.
// Empty means "fall back to the mailbox/default domain". Otherwise it must be a
// bare hostname: no scheme, no path, no raw IP literal, and no internal/metadata
// host — mirroring the mailbox tracking-domain rules and the webhook-SSRF posture.
func CampaignTrackingDomain(host string) *errx.Error {
	if host == "" {
		return nil
	}
	if len(host) > 253 || strings.Contains(host, "://") || strings.ContainsAny(host, " \t\r\n/\\?#@:") {
		return errx.New(errx.BadRequest, "invalid tracking domain")
	}
	if net.ParseIP(host) != nil {
		return errx.New(errx.BadRequest, "invalid tracking domain")
	}
	lower := strings.ToLower(host)
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") || lower == "metadata.google.internal" {
		return errx.New(errx.BadRequest, "invalid tracking domain")
	}
	if !strings.Contains(host, ".") {
		return errx.New(errx.BadRequest, "invalid tracking domain")
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 {
			return errx.New(errx.BadRequest, "invalid tracking domain")
		}
		for _, c := range label {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-') {
				return errx.New(errx.BadRequest, "invalid tracking domain")
			}
		}
	}
	return nil
}
