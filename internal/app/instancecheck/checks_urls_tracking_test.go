package instancecheck

import (
	"strings"
	"testing"
)

// The exact misconfiguration this exists for: a tracking host on the brand's
// own registered domain, where a URL blocklisting earned by one workspace's
// complaints reaches the site and the platform mail too.
func TestTrackingDomainSharesBrand(t *testing.T) {
	t.Setenv("APP_URL", "https://app.warmbly.com")
	t.Setenv("API_PUBLIC_URL", "https://api.warmbly.com")

	t.Setenv("TRACKING_DOMAIN", "track.warmbly.com")
	if f := checkTrackingDomainSharesBrand(t.Context(), Deps{}, Input{}); f == nil {
		t.Error("a tracking host on the brand's registered domain was not reported")
	}

	// A separate registered domain is the whole point, and must stay silent.
	t.Setenv("TRACKING_DOMAIN", "t.emberbly.com")
	if f := checkTrackingDomainSharesBrand(t.Context(), Deps{}, Input{}); f != nil {
		t.Errorf("a separate tracking domain was reported: %s", f.Message)
	}

	// Unset is a different finding's job, and localhost is development.
	for _, v := range []string{"", "localhost:3000"} {
		t.Setenv("TRACKING_DOMAIN", v)
		if f := checkTrackingDomainSharesBrand(t.Context(), Deps{}, Input{}); f != nil {
			t.Errorf("TRACKING_DOMAIN=%q was reported: %s", v, f.Message)
		}
	}
}

// Domains under a multi-label public suffix are only related when their eTLD+1 matches.
func TestTrackingDomainSharesBrandUnderMultiLabelSuffix(t *testing.T) {
	t.Setenv("APP_URL", "https://campaigns.build-bros.co.uk/")
	t.Setenv("API_PUBLIC_URL", "")

	t.Setenv("TRACKING_DOMAIN", "track.buildbrosmarketing.co.uk")
	if f := checkTrackingDomainSharesBrand(t.Context(), Deps{}, Input{}); f != nil {
		t.Errorf("separate registered domains under co.uk were reported: %s", f.Message)
	}

	t.Setenv("TRACKING_DOMAIN", "track.build-bros.co.uk")
	f := checkTrackingDomainSharesBrand(t.Context(), Deps{}, Input{})
	if f == nil {
		t.Fatal("a tracking host on the brand's co.uk domain was not reported")
	}
	if !strings.Contains(f.Message, "registered domain build-bros.co.uk ") {
		t.Errorf("the finding named the wrong registered domain: %s", f.Message)
	}
}

func TestRegistrableDomain(t *testing.T) {
	for host, want := range map[string]string{
		"track.warmbly.com":              "warmbly.com",
		"Track.Warmbly.COM.":             "warmbly.com",
		"campaigns.build-bros.co.uk":     "build-bros.co.uk",
		"track.buildbrosmarketing.co.uk": "buildbrosmarketing.co.uk",
		"a.b.example.com.au":             "example.com.au",
		"mail.example.org.uk":            "example.org.uk",
		"t.example.co.nz":                "example.co.nz",
		"co.uk":                          "co.uk",
		"localhost":                      "localhost",
		"10.0.0.1":                       "10.0.0.1",
		"::1":                            "::1",
	} {
		if got := registrableDomain(host); got != want {
			t.Errorf("registrableDomain(%q) = %q, want %q", host, got, want)
		}
	}
}

// The sender-domain check compares registered domains, so a .co.uk sender on another domain is a mismatch.
func TestMailFromDomainMismatchUnderMultiLabelSuffix(t *testing.T) {
	t.Setenv("APP_URL", "https://campaigns.build-bros.co.uk")
	t.Setenv("FRONTEND_BASE_URL", "")

	t.Setenv("EMAIL_ADDRESS", "Build Bros <hello@buildbrosmarketing.co.uk>")
	if f := checkMailFromDomainMismatch(t.Context(), Deps{}, Input{}); f == nil {
		t.Error("a sender on a different co.uk domain was not reported")
	}

	t.Setenv("EMAIL_ADDRESS", "hello@mail.build-bros.co.uk")
	if f := checkMailFromDomainMismatch(t.Context(), Deps{}, Input{}); f != nil {
		t.Errorf("a sender on the dashboard's own domain was reported: %s", f.Message)
	}
}
