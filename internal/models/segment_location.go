package models

import (
	"strings"

	"github.com/warmbly/warmbly/internal/pkg/geo"
)

// The location fields read what the contact location sweep derived from the
// record (contacts.location_*), so every value here is one geo can produce.

// SegmentWorldRegionNone is the world_region value for a contact with no
// country: nothing places it, or only a bare UTC offset does.
const SegmentWorldRegionNone = "none"

func worldRegionOptions() []string {
	out := make([]string, 0, len(geo.WorldRegions)+1)
	for _, r := range geo.WorldRegions {
		out = append(out, string(r))
	}
	return append(out, SegmentWorldRegionNone)
}

func worldRegionLabels() map[string]string {
	out := map[string]string{SegmentWorldRegionNone: "No known region"}
	for _, r := range geo.WorldRegions {
		out[string(r)] = r.Label()
	}
	return out
}

func countryOptions() []string {
	cs := geo.Countries()
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Code
	}
	return out
}

func countryLabels() map[string]string {
	out := map[string]string{}
	for _, c := range geo.Countries() {
		out[c.Code] = c.Name
	}
	return out
}

func timezoneOptions() []string {
	return geo.ResolvableZones()
}

// timezoneLabels reads "Los Angeles · United States" so the picker's search
// finds a zone by its city or its country.
func timezoneLabels() map[string]string {
	out := map[string]string{"UTC": "UTC"}
	for _, z := range geo.Zones() {
		city := strings.ReplaceAll(z[strings.LastIndex(z, "/")+1:], "_", " ")
		if c, ok := geo.CountryByCode(geo.ZoneCountry(z)); ok {
			out[z] = city + " · " + c.Name
		} else {
			out[z] = city
		}
	}
	for _, z := range geo.OffsetZones()[1:] {
		// Etc/GMT-2 is UTC+2: the POSIX sign is inverted.
		n := strings.TrimPrefix(z, "Etc/GMT")
		sign := "+"
		if n[0] == '+' {
			sign = "-"
		}
		out[z] = "UTC" + sign + n[1:] + " (fixed offset)"
	}
	return out
}

// SegmentWorldRegionCountries lists a territory's countries for the SQL
// compiler, which filters world_region as a country list.
func SegmentWorldRegionCountries(region string) []string {
	return geo.CountriesIn(geo.WorldRegion(region))
}
