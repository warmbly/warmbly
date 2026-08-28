package scheduler

import (
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/models"
)

func TestTimezoneForEmailDomain(t *testing.T) {
	tests := []struct {
		email string
		want  string
	}{
		{"a@example.de", "Europe/Berlin"},
		{"a@sub.example.co.uk", "Europe/London"},
		{"a@example.jp", "Asia/Tokyo"},
		{"a@example.com", ""},             // gTLD carries no location signal
		{"a@example.io", ""},              // ditto
		{"not-an-email", ""},              // no @
		{"a@localhost", ""},               // single label
		{"A@Example.DE", "Europe/Berlin"}, // case-insensitive
	}
	for _, tt := range tests {
		if got := timezoneForEmailDomain(tt.email); got != tt.want {
			t.Errorf("timezoneForEmailDomain(%q) = %q, want %q", tt.email, got, tt.want)
		}
	}
}

func TestNormalizeHoursRejectsGarbageAndNeverEmpties(t *testing.T) {
	// An empty result would mean "no hour is acceptable", which would stall
	// every campaign on the org, so it must fall back to business hours.
	for _, in := range [][]int{nil, {}, {-1, 24, 99}} {
		got := normalizeHours(in)
		if len(got) != len(defaultPreferredHours) {
			t.Errorf("normalizeHours(%v) = %v, want the business-hours default", in, got)
		}
	}
	if got := normalizeHours([]int{16, 9, 9, 30, 11}); len(got) != 3 || got[0] != 9 || got[1] != 11 || got[2] != 16 {
		t.Errorf("normalizeHours() = %v, want sorted+deduped [9 11 16]", got)
	}
}

func TestSendTimePreferenceFromDisabled(t *testing.T) {
	if p := sendTimePreferenceFrom(nil); p.enabled {
		t.Error("nil settings must not enable optimization")
	}
	s := models.DefaultAdvancedOutreachSettings()
	s.SendTimeOptimization.Enabled = false
	if p := sendTimePreferenceFrom(&s); p.enabled {
		t.Error("disabled settings must not enable optimization")
	}
	s.SendTimeOptimization.Enabled = true
	p := sendTimePreferenceFrom(&s)
	if !p.enabled || !p.useContactTZ || !p.avoidWeekends {
		t.Errorf("defaults not carried through: %+v", p)
	}
}

func TestRecipientLocationPrefersContactFieldThenDomain(t *testing.T) {
	pref := sendTimePreference{enabled: true, useContactTZ: true, defaultTZ: "UTC"}

	explicit := &models.Contact{Email: "a@example.de", CustomFields: map[string]string{"timezone": "Asia/Tokyo"}}
	if got := recipientLocation(explicit, pref); got.String() != "Asia/Tokyo" {
		t.Errorf("explicit contact timezone ignored: got %s", got)
	}

	byDomain := &models.Contact{Email: "a@example.de"}
	if got := recipientLocation(byDomain, pref); got.String() != "Europe/Berlin" {
		t.Errorf("domain inference failed: got %s", got)
	}

	// A junk timezone field must fall through, not fail the send.
	junk := &models.Contact{Email: "a@example.de", CustomFields: map[string]string{"timezone": "Mars/Olympus"}}
	if got := recipientLocation(junk, pref); got.String() != "Europe/Berlin" {
		t.Errorf("junk timezone should fall through to the domain: got %s", got)
	}

	// gTLD with no contact field lands on the org default.
	pref.defaultTZ = "America/Denver"
	if got := recipientLocation(&models.Contact{Email: "a@example.com"}, pref); got.String() != "America/Denver" {
		t.Errorf("expected org default, got %s", got)
	}

	// use_contact_timezone off means the org default always wins.
	pref.useContactTZ = false
	if got := recipientLocation(explicit, pref); got.String() != "America/Denver" {
		t.Errorf("use_contact_timezone=false must ignore the contact: got %s", got)
	}
}

func TestSnapToPreferredHourIsForwardOnly(t *testing.T) {
	loc := time.UTC
	hours := []int{9, 10, 14}

	// Already inside a preferred hour: unchanged, so optimization never
	// perturbs a slot the rest of the scheduler already agreed on.
	at := time.Date(2026, 3, 4, 9, 30, 0, 0, loc)
	if got := snapToPreferredHour(at, loc, hours, false); !got.Equal(at) {
		t.Errorf("in-window slot moved: got %s want %s", got, at)
	}

	// Before the first hour: forward to it, same minute.
	early := time.Date(2026, 3, 4, 6, 17, 0, 0, loc)
	got := snapToPreferredHour(early, loc, hours, false)
	if got.Hour() != 9 || got.Minute() != 17 || got.Day() != 4 {
		t.Errorf("early slot = %s, want 09:17 same day", got)
	}

	// Between preferred hours: forward to the next one, not backward.
	between := time.Date(2026, 3, 4, 11, 5, 0, 0, loc)
	got = snapToPreferredHour(between, loc, hours, false)
	if got.Hour() != 14 || got.Day() != 4 {
		t.Errorf("mid-gap slot = %s, want 14:xx same day", got)
	}
	if got.Before(between) {
		t.Errorf("snap moved backwards: %s < %s", got, between)
	}

	// After the last hour: first preferred hour tomorrow.
	late := time.Date(2026, 3, 4, 22, 0, 0, 0, loc)
	got = snapToPreferredHour(late, loc, hours, false)
	if got.Hour() != 9 || got.Day() != 5 {
		t.Errorf("late slot = %s, want 09:00 next day", got)
	}
}

func TestSnapToPreferredHourSkipsWeekend(t *testing.T) {
	loc := time.UTC
	// 2026-03-07 is a Saturday.
	sat := time.Date(2026, 3, 7, 22, 0, 0, 0, loc)
	got := snapToPreferredHour(sat, loc, []int{9}, true)
	if got.Weekday() != time.Monday || got.Day() != 9 {
		t.Errorf("weekend slot = %s (%s), want Monday the 9th", got, got.Weekday())
	}
	// With weekend sending allowed it stays on Sunday.
	got = snapToPreferredHour(sat, loc, []int{9}, false)
	if got.Weekday() != time.Sunday {
		t.Errorf("non-avoiding slot = %s (%s), want Sunday", got, got.Weekday())
	}
}

func TestSnapToPreferredHourNoHoursIsNoop(t *testing.T) {
	at := time.Date(2026, 3, 4, 6, 0, 0, 0, time.UTC)
	if got := snapToPreferredHour(at, time.UTC, nil, false); !got.Equal(at) {
		t.Errorf("empty hour list must not move the slot: got %s", got)
	}
}

func TestSnapToPreferredHourCrossesDSTForward(t *testing.T) {
	// US DST starts 2026-03-08. A slot the evening before must still resolve
	// to a real 09:00 local the next morning, not an hour that does not exist.
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("tzdata unavailable")
	}
	before := time.Date(2026, 3, 7, 23, 0, 0, 0, loc)
	got := snapToPreferredHour(before, loc, []int{9}, false)
	if got.Hour() != 9 || got.Day() != 8 {
		t.Errorf("DST slot = %s, want 09:00 on the 8th", got)
	}
	if got.Before(before) {
		t.Errorf("DST snap moved backwards: %s < %s", got, before)
	}
}
