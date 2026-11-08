package geo

import (
	"sort"
	"strings"
)

// callingCodes maps an international dialling prefix to its country. +1 is
// the whole North American plan and reads as the US; +7 reads as Russia.
var callingCodes = map[string]string{
	"1": "US", "7": "RU", "20": "EG", "27": "ZA", "30": "GR", "31": "NL", "32": "BE", "33": "FR",
	"34": "ES", "36": "HU", "39": "IT", "40": "RO", "41": "CH", "43": "AT", "44": "GB", "45": "DK",
	"46": "SE", "47": "NO", "48": "PL", "49": "DE", "51": "PE", "52": "MX", "53": "CU", "54": "AR",
	"55": "BR", "56": "CL", "57": "CO", "58": "VE", "60": "MY", "61": "AU", "62": "ID", "63": "PH",
	"64": "NZ", "65": "SG", "66": "TH", "81": "JP", "82": "KR", "84": "VN", "86": "CN", "90": "TR",
	"91": "IN", "92": "PK", "93": "AF", "94": "LK", "95": "MM", "98": "IR", "211": "SS", "212": "MA",
	"213": "DZ", "216": "TN", "218": "LY", "220": "GM", "221": "SN", "233": "GH", "234": "NG",
	"237": "CM", "251": "ET", "254": "KE", "255": "TZ", "256": "UG", "260": "ZM", "263": "ZW",
	"351": "PT", "352": "LU", "353": "IE", "354": "IS", "355": "AL", "356": "MT", "357": "CY",
	"358": "FI", "359": "BG", "370": "LT", "371": "LV", "372": "EE", "373": "MD", "374": "AM",
	"375": "BY", "380": "UA", "381": "RS", "382": "ME", "385": "HR", "386": "SI", "387": "BA",
	"389": "MK", "420": "CZ", "421": "SK", "423": "LI", "502": "GT", "503": "SV", "504": "HN",
	"505": "NI", "506": "CR", "507": "PA", "591": "BO", "593": "EC", "595": "PY", "598": "UY",
	"852": "HK", "853": "MO", "855": "KH", "856": "LA", "880": "BD", "886": "TW", "960": "MV",
	"961": "LB", "962": "JO", "963": "SY", "964": "IQ", "965": "KW", "966": "SA", "967": "YE",
	"968": "OM", "970": "PS", "971": "AE", "972": "IL", "973": "BH", "974": "QA", "975": "BT",
	"976": "MN", "977": "NP", "992": "TJ", "993": "TM", "994": "AZ", "995": "GE", "996": "KG",
	"998": "UZ",
}

// PhoneCountry reads the country from an international number (+44..., 0044...).
// A national number carries no country and returns "".
func PhoneCountry(phone string) string {
	p := strings.TrimSpace(phone)
	switch {
	case strings.HasPrefix(p, "+"):
		p = p[1:]
	case strings.HasPrefix(p, "00"):
		p = p[2:]
	default:
		return ""
	}
	var digits strings.Builder
	for _, r := range p {
		switch {
		case r >= '0' && r <= '9':
			digits.WriteRune(r)
		case r == ' ' || r == '-' || r == '.' || r == '(' || r == ')':
		default:
			return ""
		}
		if digits.Len() == 3 {
			break
		}
	}
	d := digits.String()
	if len(d) < 3 {
		// Too short to be a number at all.
		return ""
	}
	for n := 3; n >= 1; n-- {
		if c, ok := callingCodes[d[:n]]; ok {
			return c
		}
	}
	return ""
}

// vanityTLDs are country codes sold as generic names (.io, .co, .ai), which
// say nothing about where the company is.
var vanityTLDs = map[string]bool{
	"ac": true, "ai": true, "am": true, "as": true, "bz": true, "cc": true, "cd": true, "co": true,
	"cx": true, "dj": true, "fm": true, "gg": true, "gs": true, "im": true, "io": true,
	"je": true, "la": true, "ly": true, "md": true, "me": true, "ms": true, "mu": true,
	"nu": true, "pw": true, "sc": true, "sh": true, "so": true, "st": true, "tk": true,
	"to": true, "tv": true, "vc": true, "ws": true, "vg": true,
}

// EmailDomainCountry reads the country from an address's country-code domain
// (".de", ".co.uk"). Generic and vanity domains return "".
func EmailDomainCountry(email string) string {
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return ""
	}
	labels := strings.Split(strings.ToLower(strings.TrimSpace(email[at+1:])), ".")
	if len(labels) < 2 {
		return ""
	}
	tld := labels[len(labels)-1]
	if tld == "uk" {
		return "GB"
	}
	if len(tld) != 2 || vanityTLDs[tld] {
		return ""
	}
	if c, ok := CountryByCode(tld); ok {
		return c.Code
	}
	return ""
}

// WorldRegion is a sales territory: the grouping teams split outreach by.
type WorldRegion string

const (
	RegionNorthAmerica WorldRegion = "north_america"
	RegionLatinAmerica WorldRegion = "latin_america"
	RegionEMEA         WorldRegion = "emea"
	RegionAPAC         WorldRegion = "apac"
)

// WorldRegions lists the territories in display order.
var WorldRegions = []WorldRegion{RegionNorthAmerica, RegionLatinAmerica, RegionEMEA, RegionAPAC}

// Label is the territory's display name.
func (r WorldRegion) Label() string {
	switch r {
	case RegionNorthAmerica:
		return "North America"
	case RegionLatinAmerica:
		return "Latin America"
	case RegionEMEA:
		return "Europe, Middle East and Africa"
	case RegionAPAC:
		return "Asia-Pacific"
	}
	return string(r)
}

// emeaInAsia are the countries tz files under Asia/ that sales teams count as EMEA.
var emeaInAsia = map[string]bool{
	"AE": true, "AM": true, "AZ": true, "BH": true, "CY": true, "GE": true, "IL": true, "IQ": true,
	"IR": true, "JO": true, "KW": true, "LB": true, "OM": true, "PS": true, "QA": true, "SA": true,
	"SY": true, "YE": true, "KZ": true, "KG": true, "TJ": true, "TM": true, "UZ": true,
}

// apacInIndian are the Indian Ocean territories that sit with Asia, not Africa.
var apacInIndian = map[string]bool{"MV": true, "IO": true, "CX": true, "CC": true}

// RegionOf is the territory a country belongs to, "" when it has none.
func RegionOf(country string) WorldRegion {
	c, ok := CountryByCode(country)
	if !ok {
		return ""
	}
	area, _, _ := strings.Cut(c.Zone, "/")
	switch area {
	case "America":
		switch c.Code {
		case "US", "CA", "BM", "PM", "GL":
			return RegionNorthAmerica
		}
		return RegionLatinAmerica
	case "Europe", "Africa", "Arctic":
		return RegionEMEA
	case "Atlantic":
		// Bermuda is filed here, the Azores, Faroes and Iceland are European.
		if c.Code == "BM" {
			return RegionNorthAmerica
		}
		if c.Code == "FK" || c.Code == "GS" {
			return RegionLatinAmerica
		}
		return RegionEMEA
	case "Indian":
		if apacInIndian[c.Code] {
			return RegionAPAC
		}
		return RegionEMEA
	case "Asia":
		if emeaInAsia[c.Code] {
			return RegionEMEA
		}
		return RegionAPAC
	case "Australia", "Pacific":
		return RegionAPAC
	}
	return ""
}

// CountriesIn lists every country in a territory, sorted by code.
func CountriesIn(r WorldRegion) []string {
	var out []string
	for _, c := range Countries() {
		if RegionOf(c.Code) == r {
			out = append(out, c.Code)
		}
	}
	sort.Strings(out)
	return out
}
