package clientreport

import (
	"errors"
	"net/mail"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/app/tz"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// ErrInvalidSchedule is the code for a schedule that cannot run.
const ErrInvalidSchedule = "invalid_report_schedule"

func scheduleError(msg string) *errx.Error {
	return errx.NewWithIdentifier(errx.BadRequest, ErrInvalidSchedule, msg)
}

// normalizeSchedule validates a schedule request into the schedule it stores, without its next run.
func normalizeSchedule(req *models.SetClientReportScheduleRequest) (*models.ClientReportSchedule, *errx.Error) {
	recipients, xerr := normalizeRecipients(req.Recipients)
	if xerr != nil {
		return nil, xerr
	}
	if !req.Cadence.Valid() {
		return nil, scheduleError("cadence must be weekly or monthly.")
	}
	if req.Weekday < 0 || req.Weekday > 6 {
		return nil, scheduleError("weekday must be between 0 (Sunday) and 6 (Saturday).")
	}
	monthDay := req.MonthDay
	if monthDay == 0 {
		monthDay = 1
	}
	if monthDay < 1 || monthDay > 28 {
		return nil, scheduleError("month_day must be between 1 and 28, so every month has it.")
	}
	if req.Hour < 0 || req.Hour > 23 {
		return nil, scheduleError("hour must be between 0 and 23.")
	}
	zone := strings.TrimSpace(req.Timezone)
	if zone == "" {
		zone = "UTC"
	}
	if !tz.Valid(zone) {
		return nil, scheduleError("timezone must be an IANA zone such as Europe/London.")
	}
	return &models.ClientReportSchedule{Recipients: recipients, Cadence: req.Cadence, Weekday: req.Weekday,
		MonthDay: monthDay, Hour: req.Hour, Timezone: zone, Paused: req.Paused}, nil
}

// normalizeRecipients keeps bare, lowercased, deduplicated addresses.
func normalizeRecipients(in []string) ([]string, *errx.Error) {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, raw := range in {
		v := strings.TrimSpace(raw)
		if v == "" {
			continue
		}
		addr, err := mail.ParseAddress(v)
		if err != nil || addr.Address != v || len(v) > 254 {
			return nil, scheduleError("each recipient must be a plain email address.")
		}
		v = strings.ToLower(v)
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return nil, scheduleError("add at least one recipient.")
	}
	if len(out) > models.MaxClientReportRecipients {
		return nil, scheduleError("a schedule sends to at most 10 recipients.")
	}
	return out, nil
}

var errNoNextRun = errors.New("clientreport: schedule has no next run")

// NextRun is the first send time strictly after after, at the schedule's hour on its local day.
func NextRun(s models.ClientReportSchedule, after time.Time) (time.Time, error) {
	loc, err := time.LoadLocation(s.Timezone)
	if err != nil {
		return time.Time{}, err
	}
	local := after.In(loc)
	switch s.Cadence {
	case models.ClientReportWeekly:
		for i := 0; i <= 7; i++ {
			// Noon reads the date's weekday clear of any DST shift.
			day := time.Date(local.Year(), local.Month(), local.Day()+i, 12, 0, 0, 0, loc)
			if int(day.Weekday()) != s.Weekday {
				continue
			}
			if at := time.Date(day.Year(), day.Month(), day.Day(), s.Hour, 0, 0, 0, loc); at.After(after) {
				return at.UTC(), nil
			}
		}
	case models.ClientReportMonthly:
		for i := 0; i <= 1; i++ {
			if at := time.Date(local.Year(), local.Month()+time.Month(i), s.MonthDay, s.Hour, 0, 0, 0, loc); at.After(after) {
				return at.UTC(), nil
			}
		}
	}
	return time.Time{}, errNoNextRun
}
