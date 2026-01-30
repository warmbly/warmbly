package tz

import (
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"
)

var tzs = []string{
	"America/New_York",
	"America/Chicago",
	"America/Denver",
	"America/Los_Angeles",
	"America/Toronto",
	"America/Sao_Paulo",
	"America/Mexico_City",
	"America/Bogota",
	"America/Lima",
	"America/Caracas",
	"America/Argentina/Buenos_Aires",
	"America/Anchorage",
	"America/Phoenix",
	"America/Winnipeg",
	"America/Montevideo",

	"Europe/London",
	"Europe/Paris",
	"Europe/Berlin",
	"Europe/Madrid",
	"Europe/Rome",
	"Europe/Moscow",
	"Europe/Istanbul",
	"Europe/Budapest",
	"Europe/Amsterdam",
	"Europe/Stockholm",
	"Europe/Vienna",
	"Europe/Zurich",

	"Africa/Johannesburg",
	"Africa/Cairo",
	"Africa/Lagos",
	"Africa/Nairobi",

	"Asia/Dubai",
	"Asia/Kolkata",
	"Asia/Bangkok",
	"Asia/Shanghai",
	"Asia/Hong_Kong",
	"Asia/Tokyo",
	"Asia/Seoul",
	"Asia/Tehran",
	"Asia/Jerusalem",
	"Asia/Karachi",
	"Asia/Dhaka",
	"Asia/Singapore",
	"Asia/Taipei",

	"Australia/Sydney",
	"Australia/Melbourne",
	"Australia/Brisbane",
	"Pacific/Auckland",
	"Pacific/Honolulu",
	"Pacific/Fiji",
}

type TimezoneOption struct {
	Name        string `json:"name"`         // IANA name, ex. "Europe/Budapest"
	DisplayName string `json:"display_name"` // ex. "(UTC+02:00) Budapest"
}

type Client struct {
	sync.RWMutex
	lastFetched time.Time
	Timezones   []TimezoneOption
}

func Valid(name string) bool {
	return slices.Contains(tzs, name)
}

func NewTZ() *Client {
	fetchTimes()
	tz := &Client{}
	tz.fetch()
	return tz
}

func (t *Client) Get() []TimezoneOption {
	t.RLock()

	if time.Since(t.lastFetched) > 24*time.Hour {
		t.RUnlock()
		t.Lock()
		t.fetch()
		t.Unlock()
		t.RLock()
	}
	defer t.RUnlock()
	return t.Timezones
}

func (t *Client) fetch() {
	now := time.Now()

	sort.Slice(tzs, func(i, j int) bool {
		locI, _ := time.LoadLocation(tzs[i])
		locJ, _ := time.LoadLocation(tzs[j])
		_, offsetI := now.In(locI).Zone()
		_, offsetJ := now.In(locJ).Zone()
		return offsetI < offsetJ
	})

	options := make([]TimezoneOption, 0, len(tzs))

	for _, tz := range tzs {
		loc, err := time.LoadLocation(tz)
		if err != nil {
			continue
		}
		_, offsetSeconds := now.In(loc).Zone()

		sign := "+"
		if offsetSeconds < 0 {
			sign = "-"
			offsetSeconds = -offsetSeconds
		}
		hours := offsetSeconds / 3600
		minutes := (offsetSeconds % 3600) / 60

		display := fmt.Sprintf("(UTC%s%02d:%02d) %s", sign, hours, minutes, tz)
		options = append(options, TimezoneOption{
			Name:        tz,
			DisplayName: display,
		})
	}
	t.Timezones = options
	t.lastFetched = now
}
