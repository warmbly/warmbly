package geo

import (
	"testing"
	"time"
)

func TestResolveReadsWhatImportsCarry(t *testing.T) {
	cases := []struct {
		name   string
		in     Input
		zone   string
		cc     string
		source Source
	}{
		{"iana timezone wins", Input{Fields: map[string]string{"timezone": "Europe/Berlin", "country": "United States"}}, "Europe/Berlin", "US", SourceTimezone},
		{"timezone in lower case", Input{Fields: map[string]string{"Time Zone": "america/chicago"}}, "America/Chicago", "US", SourceTimezone},
		{"abbreviation", Input{Fields: map[string]string{"tz": "PST"}}, "America/Los_Angeles", "US", SourceTimezone},
		{"windows name", Input{Fields: map[string]string{"timezone": "(GMT-05:00) Eastern Time (US & Canada)"}}, "America/New_York", "US", SourceTimezone},
		{"whole hour offset", Input{Fields: map[string]string{"timezone": "UTC+2"}}, "Etc/GMT-2", "", SourceTimezone},
		{"half hour offset", Input{Fields: map[string]string{"timezone": "GMT+05:30"}}, "Asia/Kolkata", "IN", SourceTimezone},
		{"state code with country", Input{Fields: map[string]string{"country": "United States", "state": "TX", "city": "Austin"}}, "America/Chicago", "US", SourceRegion},
		{"state name alone", Input{Fields: map[string]string{"state": "California"}}, "America/Los_Angeles", "US", SourceRegion},
		{"bare state code is a US state", Input{Fields: map[string]string{"state": "CA"}}, "America/Los_Angeles", "US", SourceRegion},
		{"province with country", Input{Fields: map[string]string{"Country/Region": "Canada", "State/Region": "British Columbia"}}, "America/Vancouver", "CA", SourceRegion},
		{"iso 3166-2 region", Input{Fields: map[string]string{"region": "US-WA"}}, "America/Los_Angeles", "US", SourceRegion},
		{"city alone", Input{Fields: map[string]string{"city": "Munich"}}, "Europe/Berlin", "DE", SourceCity},
		{"zone city alone", Input{Fields: map[string]string{"city": "São Paulo"}}, "America/Sao_Paulo", "BR", SourceCity},
		{"namesake abroad is refused", Input{Fields: map[string]string{"city": "London", "country": "Canada"}}, "America/Toronto", "CA", SourceCountry},
		{"single zone country", Input{Fields: map[string]string{"country": "Deutschland"}}, "Europe/Berlin", "DE", SourceCountry},
		{"alpha-3 country", Input{Fields: map[string]string{"country": "GBR"}}, "Europe/London", "GB", SourceCountry},
		{"multi zone country takes populous zone", Input{Fields: map[string]string{"country": "Australia"}}, "Australia/Sydney", "AU", SourceCountry},
		{"linkedin location", Input{Fields: map[string]string{"location": "San Francisco Bay Area"}}, "America/Los_Angeles", "US", SourceCity},
		{"location city state", Input{Fields: map[string]string{"location": "Denver, CO"}}, "America/Denver", "US", SourceRegion},
		{"location city state country", Input{Fields: map[string]string{"location": "Seattle, Washington, United States"}}, "America/Los_Angeles", "US", SourceRegion},
		{"location city and country code", Input{Fields: map[string]string{"location": "Toronto, CA"}}, "America/Toronto", "CA", SourceCity},
		{"location australian state", Input{Fields: map[string]string{"location": "Perth, WA"}}, "Australia/Perth", "AU", SourceRegion},
		{"location country only", Input{Fields: map[string]string{"location": "Netherlands"}}, "Europe/Amsterdam", "NL", SourceCountry},
		{"person before company", Input{Fields: map[string]string{"company_country": "France", "city": "Tokyo"}}, "Asia/Tokyo", "JP", SourceCity},
		{"company when person says nothing", Input{Fields: map[string]string{"company_country": "France"}}, "Europe/Paris", "FR", SourceCountry},
		{"phone", Input{Phone: "+44 20 7946 0958"}, "Europe/London", "GB", SourcePhone},
		{"phone with 00", Input{Phone: "0049 30 123456"}, "Europe/Berlin", "DE", SourcePhone},
		{"national phone says nothing", Input{Phone: "(415) 555-0100", Email: "a@acme.com"}, "", "", SourceNone},
		{"email ccTLD", Input{Email: "anna@firma.de"}, "Europe/Berlin", "DE", SourceEmailDomain},
		{"co.uk", Input{Email: "sam@shop.co.uk"}, "Europe/London", "GB", SourceEmailDomain},
		{"vanity ccTLD says nothing", Input{Email: "dev@startup.io"}, "", "", SourceNone},
		{"unreadable values fall through", Input{Fields: map[string]string{"timezone": "whenever", "country": "Atlantis"}, Email: "x@y.fr"}, "Europe/Paris", "FR", SourceEmailDomain},
		{"unrelated fields ignored", Input{Fields: map[string]string{"industry": "Retail"}}, "", "", SourceNone},
		{"state named like a country", Input{Fields: map[string]string{"location": "Atlanta, Georgia"}}, "America/New_York", "US", SourceRegion},
		{"country named like a state", Input{Fields: map[string]string{"location": "Tbilisi, Georgia"}}, "Asia/Tbilisi", "GE", SourceCity},
		{"country code a domain confirms", Input{Fields: map[string]string{"location": "Wuppertal, DE"}, Email: "a@firma.de"}, "Europe/Berlin", "DE", SourceCountry},
		{"country code a phone confirms", Input{Fields: map[string]string{"location": "Bucaramanga, CO"}, Phone: "+57 300 1234567"}, "America/Bogota", "CO", SourceCountry},
		{"unconfirmed code stays a state", Input{Fields: map[string]string{"location": "Springfield, IL"}}, "America/Chicago", "US", SourceRegion},
		{"known city settles a code", Input{Fields: map[string]string{"location": "Leipzig, DE"}}, "Europe/Berlin", "DE", SourceCity},
		{"bare state read in the hinted country", Input{Fields: map[string]string{"state": "WA"}, Phone: "+61 8 9123 4567"}, "Australia/Perth", "AU", SourceRegion},
		{"legacy zone name", Input{Fields: map[string]string{"timezone": "US/Eastern"}}, "America/New_York", "US", SourceTimezone},
		{"legacy zone name with a country", Input{Fields: map[string]string{"timezone": "Asia/Calcutta"}}, "Asia/Kolkata", "IN", SourceTimezone},
		{"a direction is not a timezone", Input{Fields: map[string]string{"timezone": "West"}}, "", "", SourceNone},
		{"any alpha-3 code", Input{Fields: map[string]string{"country": "AFG"}}, "Asia/Kabul", "AF", SourceCountry},
		{"location repeats the country column", Input{Fields: map[string]string{"country": "Canada", "location": "Kelowna, BC, Canada"}}, "America/Vancouver", "CA", SourceRegion},
		{"bare country code location", Input{Fields: map[string]string{"location": "GB"}}, "Europe/London", "GB", SourceCountry},
		{"canadian area code", Input{Phone: "+1 416 555 0100"}, "America/Toronto", "CA", SourcePhone},
		{"us area code", Input{Phone: "+1 (415) 555-0100 ext 22"}, "America/New_York", "US", SourcePhone},
		{"kazakh number", Input{Phone: "+7 717 212 3456"}, "Asia/Almaty", "KZ", SourcePhone},
		{"shared code settled by the domain", Input{Phone: "+1 212 555 0100", Email: "a@firma.ca"}, "America/Toronto", "CA", SourcePhone},
		{"less common calling code", Input{Phone: "+225 27 20 21 22 23"}, "Africa/Abidjan", "CI", SourcePhone},
		{"malformed phone yields to the domain", Input{Phone: "+491garbage", Email: "a@societe.fr"}, "Europe/Paris", "FR", SourceEmailDomain},
		{"business abbreviation beats a legacy link", Input{Fields: map[string]string{"timezone": "EST"}}, "America/New_York", "US", SourceTimezone},
		{"a UTC spelling", Input{Fields: map[string]string{"timezone": "Etc/UTC"}}, "UTC", "", SourceTimezone},
		{"plain field name wins over an alias", Input{Fields: map[string]string{"tz": "Asia/Tokyo", "timezone": "Europe/Berlin"}}, "Europe/Berlin", "DE", SourceTimezone},
		{"a junk alias does not hide a good one", Input{Fields: map[string]string{"timezone": "whenever", "tz": "Asia/Tokyo"}}, "Asia/Tokyo", "JP", SourceTimezone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Resolve(tc.in)
			if got.Timezone != tc.zone || got.Country != tc.cc || got.Source != tc.source {
				t.Fatalf("Resolve = %+v, want {%s %s %s}", got, tc.zone, tc.cc, tc.source)
			}
		})
	}
}

// Every zone the resolver can hand out is stored and later read back by
// time.LoadLocation and by Postgres AT TIME ZONE; one that does not load
// would stall the lead it was written for.
func TestEveryResolvableZoneLoads(t *testing.T) {
	zones := append(Zones(), "UTC")
	for _, z := range zoneNames {
		zones = append(zones, z)
	}
	for _, z := range fractionalOffsets {
		zones = append(zones, z)
	}
	for _, list := range subdivisions {
		for _, sd := range list {
			zones = append(zones, sd.zone)
		}
	}
	for _, z := range extraCities {
		zones = append(zones, z)
		if ZoneCountry(z) == "" {
			t.Errorf("city zone %s has no country", z)
		}
	}
	for _, c := range Countries() {
		zones = append(zones, c.Zone)
	}
	for _, z := range zones {
		if _, err := time.LoadLocation(z); err != nil {
			t.Errorf("zone %q does not load: %v", z, err)
		}
	}
}

func TestOffsetZonesKeepTheirOffset(t *testing.T) {
	at := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	for in, want := range map[string]int{"UTC+2": 2 * 3600, "GMT-5": -5 * 3600, "+09:00": 9 * 3600, "UTC-03:30": -(3*3600 + 1800)} {
		z, ok := ParseTimezone(in)
		if !ok {
			t.Fatalf("ParseTimezone(%q) failed", in)
		}
		loc, _ := time.LoadLocation(z)
		if _, off := at.In(loc).Zone(); off != want {
			t.Errorf("%q -> %s has offset %d, want %d", in, z, off, want)
		}
	}
}

func TestEveryCountryHasARegion(t *testing.T) {
	for _, c := range Countries() {
		if RegionOf(c.Code) == "" && c.Code != "AQ" {
			t.Errorf("%s (%s, %s) has no world region", c.Code, c.Name, c.Zone)
		}
	}
	for code, want := range map[string]WorldRegion{"US": RegionNorthAmerica, "MX": RegionLatinAmerica, "GB": RegionEMEA, "AE": RegionEMEA, "ZA": RegionEMEA, "IN": RegionAPAC, "AU": RegionAPAC, "JP": RegionAPAC} {
		if got := RegionOf(code); got != want {
			t.Errorf("RegionOf(%s) = %s, want %s", code, got, want)
		}
	}
}

func TestCanonicalFieldKey(t *testing.T) {
	for header, want := range map[string]string{"Time Zone": "timezone", "Country/Region": "country", "State/Province": "state", "City": "city", "Location": "location", "Company Country": "company_country", "Person Location": "location"} {
		if got, ok := CanonicalFieldKey(header); !ok || got != want {
			t.Errorf("CanonicalFieldKey(%q) = %q, %v; want %q", header, got, ok, want)
		}
	}
	if _, ok := CanonicalFieldKey("Industry"); ok {
		t.Error("Industry read as a location column")
	}
}
