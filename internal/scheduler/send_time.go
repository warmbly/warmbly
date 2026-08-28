package scheduler

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/app/behavior"
	"github.com/warmbly/warmbly/internal/models"
)

// sendTimePreference flattens the org's recipient-timezone policy so the slot
// math below stays pure.
type sendTimePreference struct {
	enabled        bool
	useContactTZ   bool
	defaultTZ      string
	preferredHours []int
	avoidWeekends  bool
}

// defaultPreferredHours mirrors models.DefaultAdvancedOutreachSettings so an
// org that enabled optimization but cleared the hours still gets business hours.
var defaultPreferredHours = []int{9, 10, 11, 14, 15, 16}

// sendTimePreferenceFrom flattens the org settings. Returns enabled=false when
// the feature is off, which is the caller's signal to leave the slot alone.
func sendTimePreferenceFrom(s *models.AdvancedOutreachSettings) sendTimePreference {
	if s == nil || !s.SendTimeOptimization.Enabled {
		return sendTimePreference{}
	}
	o := s.SendTimeOptimization
	return sendTimePreference{
		enabled:        true,
		useContactTZ:   o.UseContactTimezone,
		defaultTZ:      o.DefaultContactTimezone,
		preferredHours: normalizeHours(o.PreferredHours),
		avoidWeekends:  o.WeekendWeightMultiplier < 1,
	}
}

// normalizeHours sorts, dedupes and clamps. An empty result would mean "no
// hour is ever acceptable" and stall every campaign, so it falls back.
func normalizeHours(hours []int) []int {
	seen := map[int]bool{}
	out := make([]int, 0, len(hours))
	for _, h := range hours {
		if h < 0 || h > 23 || seen[h] {
			continue
		}
		seen[h] = true
		out = append(out, h)
	}
	if len(out) == 0 {
		return defaultPreferredHours
	}
	sort.Ints(out)
	return out
}

// ccTLDTimezone maps a country-code TLD to that country's business timezone.
// Multi-zone countries resolve to their most populous zone: this only picks
// which local hour a send aims for, so an approximation beats UTC.
var ccTLDTimezone = map[string]string{
	"uk": "Europe/London", "ie": "Europe/Dublin", "fr": "Europe/Paris",
	"de": "Europe/Berlin", "at": "Europe/Vienna", "ch": "Europe/Zurich",
	"nl": "Europe/Amsterdam", "be": "Europe/Brussels", "es": "Europe/Madrid",
	"pt": "Europe/Lisbon", "it": "Europe/Rome", "dk": "Europe/Copenhagen",
	"se": "Europe/Stockholm", "no": "Europe/Oslo", "fi": "Europe/Helsinki",
	"pl": "Europe/Warsaw", "cz": "Europe/Prague", "sk": "Europe/Bratislava",
	"hu": "Europe/Budapest", "ro": "Europe/Bucharest", "bg": "Europe/Sofia",
	"gr": "Europe/Athens", "tr": "Europe/Istanbul", "ua": "Europe/Kyiv",
	"il": "Asia/Jerusalem", "ae": "Asia/Dubai", "sa": "Asia/Riyadh",
	"in": "Asia/Kolkata", "pk": "Asia/Karachi", "sg": "Asia/Singapore",
	"my": "Asia/Kuala_Lumpur", "th": "Asia/Bangkok", "vn": "Asia/Ho_Chi_Minh",
	"id": "Asia/Jakarta", "ph": "Asia/Manila", "hk": "Asia/Hong_Kong",
	"jp": "Asia/Tokyo", "kr": "Asia/Seoul", "cn": "Asia/Shanghai",
	"tw": "Asia/Taipei", "nz": "Pacific/Auckland", "za": "Africa/Johannesburg",
	"ng": "Africa/Lagos", "ke": "Africa/Nairobi", "eg": "Africa/Cairo",
	"mx": "America/Mexico_City", "br": "America/Sao_Paulo",
	"ar": "America/Argentina/Buenos_Aires", "cl": "America/Santiago",
	"co": "America/Bogota", "pe": "America/Lima",
	"au": "Australia/Sydney", "ca": "America/Toronto", "us": "America/New_York",
	"ru": "Europe/Moscow",
}

// recipientLocation resolves the timezone to aim at, most confident source
// first. Never fails: an unresolvable name degrades to the next source.
func recipientLocation(contact *models.Contact, pref sendTimePreference) *time.Location {
	if pref.useContactTZ && contact != nil {
		if tz, ok := contact.CustomFields["timezone"]; ok {
			if loc, err := time.LoadLocation(strings.TrimSpace(tz)); err == nil {
				return loc
			}
		}
		if tz := timezoneForEmailDomain(contactEmail(contact)); tz != "" {
			if loc, err := time.LoadLocation(tz); err == nil {
				return loc
			}
		}
	}
	return loadLocation(pref.defaultTZ)
}

func contactEmail(contact *models.Contact) string {
	if contact == nil {
		return ""
	}
	return contact.Email
}

// timezoneForEmailDomain infers a timezone from an address's country-code TLD.
// Returns "" for gTLDs (.com/.io/...), which carry no location signal.
func timezoneForEmailDomain(email string) string {
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return ""
	}
	domain := strings.ToLower(strings.TrimSpace(email[at+1:]))
	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return ""
	}
	return ccTLDTimezone[labels[len(labels)-1]]
}

// snapToPreferredHour moves t forward to the next preferred local hour in loc,
// keeping the minute so sends do not pile onto the hour mark. Forward-only.
func snapToPreferredHour(t time.Time, loc *time.Location, hours []int, avoidWeekends bool) time.Time {
	if len(hours) == 0 {
		return t
	}
	local := t.In(loc)

	// Bounded at 8 days so an avoid-weekends policy can never spin.
	for day := 0; day < 8; day++ {
		candidateDay := local.AddDate(0, 0, day)
		if avoidWeekends && isWeekend(candidateDay) {
			continue
		}
		for _, h := range hours {
			if day == 0 && h < local.Hour() {
				continue
			}
			minute, second := local.Minute(), local.Second()
			if day > 0 || h > local.Hour() {
				second = 0
			}
			candidate := time.Date(candidateDay.Year(), candidateDay.Month(), candidateDay.Day(),
				h, minute, second, 0, loc)
			if !candidate.Before(local) {
				return candidate
			}
		}
	}
	return t
}

func isWeekend(t time.Time) bool {
	return t.Weekday() == time.Saturday || t.Weekday() == time.Sunday
}

// maxRecipientSlotProbes bounds the search for a slot both calendars accept.
const maxRecipientSlotProbes = 8

// recipientSlot finds the earliest moment that is BOTH inside one of the
// recipient's preferred local hours and already legal for the sender (campaign
// window plus mailbox workday).
//
// ok=false means the two calendars do not meet inside the horizon, and the
// caller must leave the slot alone. Snapping anyway would raise a hard floor
// the sender can never satisfy: every tick would re-derive a future recipient
// hour, defer, wake in the sender's window, and defer again, so the send would
// never go out at all.
func (s *schedulerService) recipientSlot(
	ctx context.Context,
	r behavior.Resolved,
	from time.Time,
	windows models.ScheduleWindows,
	campaignTZ *time.Location,
	contact *models.Contact,
	pref sendTimePreference,
	endDate *time.Time,
) (time.Time, bool) {
	loc := recipientLocation(contact, pref)
	at := from
	for i := 0; i < maxRecipientSlotProbes; i++ {
		snapped := snapToPreferredHour(at, loc, pref.preferredHours, pref.avoidWeekends)
		if !snapped.After(from) {
			// Already inside a preferred hour; nothing to move.
			return from, true
		}
		if endDate != nil && snapped.After(*endDate) {
			// Waiting for the recipient's clock must never outlive the campaign.
			return time.Time{}, false
		}
		legal := s.intersectWindows(ctx, r, snapped, windows, campaignTZ)
		if !legal.After(snapped) {
			return snapped, true
		}
		// The sender cannot send at that hour. Resume the search from the next
		// moment it CAN, which strictly advances, so this terminates.
		at = legal
	}
	return time.Time{}, false
}
