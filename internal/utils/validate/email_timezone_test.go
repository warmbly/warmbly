package validate

import "testing"

func TestEmailTimezone(t *testing.T) {
	valid := []string{
		"",    // not configured: the campaign's own window is the only one
		"UTC", // still a legitimate deliberate choice
		"Europe/London",
		"America/Denver",
		"Asia/Tokyo",
	}
	for _, tz := range valid {
		if err := EmailTimezone(tz); err != nil {
			t.Errorf("EmailTimezone(%q) rejected a valid zone: %v", tz, err)
		}
	}

	// The scheduler coerces an unloadable zone to UTC, so an invalid value
	// would otherwise be accepted and then silently ignored.
	invalid := []string{
		"Europe/Nowhere",
		"GMT+1",
		"not a timezone",
		"America/Denver; DROP TABLE email_accounts",
	}
	for _, tz := range invalid {
		if err := EmailTimezone(tz); err == nil {
			t.Errorf("EmailTimezone(%q) accepted an unloadable zone", tz)
		}
	}
}
