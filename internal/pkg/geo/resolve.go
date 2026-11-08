// Package geo works out where a contact is from whatever their record already
// carries: a timezone, country, state, city or location column from an import
// or an integration, the phone number, and the email domain.
package geo

import (
	"sort"
	"strings"
)

// Source says what a resolved timezone was read from, most confident first.
type Source string

const (
	SourceNone        Source = ""
	SourceTimezone    Source = "timezone"
	SourceCity        Source = "city"
	SourceRegion      Source = "region"
	SourceCountry     Source = "country"
	SourcePhone       Source = "phone"
	SourceEmailDomain Source = "email_domain"
)

// Sources lists every non-empty source, as the contacts CHECK allows them.
var Sources = []Source{SourceTimezone, SourceCity, SourceRegion, SourceCountry, SourcePhone, SourceEmailDomain}

// Input is what a contact record offers.
type Input struct {
	Fields map[string]string
	Phone  string
	Email  string
}

// Location is the resolver's answer. Zero means nothing on the record says.
type Location struct {
	Timezone string
	Country  string
	Source   Source
}

// Known reports whether a timezone was found.
func (l Location) Known() bool { return l.Timezone != "" }

// fieldKinds maps a folded custom-field key to what it holds. Keys naming the
// company are kept apart: they are read only when the person's own say nothing.
var fieldKinds = map[string]fieldKind{
	"timezone": kindTimezone, "tz": kindTimezone, "timezonename": kindTimezone, "localtimezone": kindTimezone,
	"leadtimezone": kindTimezone, "contacttimezone": kindTimezone, "persontimezone": kindTimezone,
	"country": kindCountry, "countrycode": kindCountry, "countryname": kindCountry, "countryregion": kindCountry,
	"personcountry": kindCountry, "contactcountry": kindCountry, "leadcountry": kindCountry, "nation": kindCountry,
	"state": kindRegion, "province": kindRegion, "stateprovince": kindRegion, "stateorprovince": kindRegion,
	"stateregion": kindRegion, "region": kindRegion, "personstate": kindRegion, "contactstate": kindRegion,
	"statecode": kindRegion, "provincecode": kindRegion, "county": kindRegion,
	"city": kindCity, "town": kindCity, "personcity": kindCity, "contactcity": kindCity, "leadcity": kindCity,
	"location": kindLocation, "personlocation": kindLocation, "contactlocation": kindLocation,
	"address": kindLocation, "fulladdress": kindLocation, "geo": kindLocation, "locality": kindLocation,

	"companycountry": kindCountry | kindCompany, "organizationcountry": kindCountry | kindCompany,
	"companystate": kindRegion | kindCompany, "organizationstate": kindRegion | kindCompany,
	"companycity": kindCity | kindCompany, "organizationcity": kindCity | kindCompany,
	"companylocation": kindLocation | kindCompany, "companyaddress": kindLocation | kindCompany,
	"organizationlocation": kindLocation | kindCompany, "hqlocation": kindLocation | kindCompany,
	"companytimezone": kindTimezone | kindCompany,
}

type fieldKind uint8

const (
	kindTimezone fieldKind = 1 << iota
	kindCountry
	kindRegion
	kindCity
	kindLocation
	kindCompany
)

// IsLocationField reports whether a custom-field key is one the resolver reads,
// which is how an import knows a column is worth keeping.
func IsLocationField(key string) bool {
	_, ok := fieldKinds[fold(key)]
	return ok
}

// CanonicalFieldKey is the custom-field key an import stores a location
// column under ("Time Zone" -> "timezone", "State/Region" -> "state").
func CanonicalFieldKey(header string) (string, bool) {
	k, ok := fieldKinds[fold(header)]
	if !ok {
		return "", false
	}
	prefix := ""
	if k&kindCompany != 0 {
		prefix = "company_"
	}
	if fold(header) == "region" {
		// "Region" may be a state or a territory; keep the header's own word.
		return "region", true
	}
	switch k &^ kindCompany {
	case kindTimezone:
		return prefix + "timezone", true
	case kindCountry:
		return prefix + "country", true
	case kindRegion:
		return prefix + "state", true
	case kindCity:
		return prefix + "city", true
	case kindLocation:
		return prefix + "location", true
	}
	return "", false
}

// plainFieldName are the keys an import writes and a person types, read first.
var plainFieldName = map[string]bool{
	"timezone": true, "country": true, "state": true, "region": true, "city": true, "location": true,
	"companytimezone": true, "companycountry": true, "companystate": true, "companycity": true, "companylocation": true,
}

// place is the location fields of one party (the person or their company).
type place struct {
	timezone, country, region, city, location string
}

func (p place) empty() bool { return p == place{} }

// splitPlaces reads the fields in a fixed order (the plain name, "timezone",
// before an alias, "tz"), and keeps a timezone or country only when it parses,
// so the answer never depends on map order and a junk alias never hides a good one.
func splitPlaces(fields map[string]string) (person, company place) {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		pi, pj := !plainFieldName[fold(keys[i])], !plainFieldName[fold(keys[j])]
		if pi != pj {
			return !pi
		}
		return keys[i] < keys[j]
	})
	for _, k := range keys {
		v := strings.TrimSpace(fields[k])
		if v == "" {
			continue
		}
		kind, ok := fieldKinds[fold(k)]
		if !ok {
			continue
		}
		dst := &person
		if kind&kindCompany != 0 {
			dst = &company
		}
		switch kind &^ kindCompany {
		case kindTimezone:
			if _, ok := ParseTimezone(v); ok {
				dst.timezone = firstOf(dst.timezone, v)
			}
		case kindCountry:
			if _, ok := ParseCountry(v); ok {
				dst.country = firstOf(dst.country, v)
			}
		case kindRegion:
			dst.region = firstOf(dst.region, v)
		case kindCity:
			dst.city = firstOf(dst.city, v)
		case kindLocation:
			dst.location = firstOf(dst.location, v)
		}
	}
	return person, company
}

func firstOf(have, v string) string {
	if have != "" {
		return have
	}
	return v
}

// Resolve works out a contact's timezone, most confident source first: an
// explicit timezone, then state, city and country (from their own columns or
// one free-text location), the person's before the company's, then the phone
// number's country, then a country-code email domain. Never fails.
func Resolve(in Input) Location {
	phoneCountry := PhoneCountry(in.Phone)
	emailCountry := EmailDomainCountry(in.Email)
	// +1 and +7 each span two countries; a domain in the other one settles it.
	if sharedCallingCode[phoneCountry] == emailCountry && emailCountry != "" {
		phoneCountry = emailCountry
	}
	// The phone and domain also settle a token that reads two ways ("DE" is
	// Delaware or Germany).
	hint := firstOf(phoneCountry, emailCountry)
	person, company := splitPlaces(in.Fields)
	if l := resolvePlace(person, hint); l.Known() {
		return l
	}
	if !company.empty() {
		if l := resolvePlace(company, hint); l.Known() {
			return l
		}
	}
	if c, ok := CountryByCode(phoneCountry); ok {
		return Location{Timezone: c.Zone, Country: c.Code, Source: SourcePhone}
	}
	if c, ok := CountryByCode(emailCountry); ok {
		return Location{Timezone: c.Zone, Country: c.Code, Source: SourceEmailDomain}
	}
	return Location{}
}

func resolvePlace(p place, hint string) Location {
	country := ""
	if p.country != "" {
		country, _ = ParseCountry(p.country)
	}
	// A free-text location fills what the dedicated columns left empty.
	if p.location != "" {
		lc, lr, lcity := parseLocation(p.location, country, hint)
		if country == "" {
			country = lc
		}
		p.region = firstOf(p.region, lr)
		p.city = firstOf(p.city, lcity)
	}
	if p.timezone != "" {
		if z, ok := ParseTimezone(p.timezone); ok {
			return Location{Timezone: z, Country: firstOf(country, ZoneCountry(z)), Source: SourceTimezone}
		}
	}
	if country == "" && p.region != "" && p.city != "" {
		// "Perth, WA" is Western Australia, not Washington.
		if z, ok := lookupCity(p.city, ""); ok {
			if cc := ZoneCountry(z); cc != "US" {
				if _, _, ok := lookupRegion(p.region, cc); ok {
					country = cc
				}
			}
		}
	}
	if country == "" && p.region != "" {
		// A state with no country is read in the hinted country first, when it has states.
		if _, ok := subdivisions[hint]; ok {
			if _, _, ok := lookupRegion(p.region, hint); ok {
				country = hint
			}
		}
	}
	if p.region != "" {
		if code, z, ok := lookupRegion(p.region, country); ok {
			return Location{Timezone: z, Country: code, Source: SourceRegion}
		}
	}
	if p.city != "" {
		if z, ok := lookupCity(p.city, country); ok {
			return Location{Timezone: z, Country: firstOf(country, ZoneCountry(z)), Source: SourceCity}
		}
	}
	if country != "" {
		if c, ok := CountryByCode(country); ok {
			return Location{Timezone: c.Zone, Country: c.Code, Source: SourceCountry}
		}
	}
	return Location{}
}

// lookupRegion reads a state or province by code or name, also in the ISO
// 3166-2 form "US-CA". Without a country it tries the US, then Canada, then
// Australia, so a bare "CA" is California.
func lookupRegion(s, country string) (string, string, bool) {
	s = strings.TrimSpace(s)
	if cc, sub, ok := strings.Cut(s, "-"); ok && len(cc) == 2 {
		if code, ok := ParseCountry(cc); ok {
			country, s = code, sub
		}
	}
	order := subdivisionCountryOrder
	if country != "" {
		if _, ok := subdivisions[country]; !ok {
			return "", "", false
		}
		order = []string{country}
	}
	f := fold(s)
	for _, cc := range order {
		for _, sd := range subdivisions[cc] {
			if strings.EqualFold(sd.code, s) || fold(sd.name) == f {
				return cc, sd.zone, true
			}
		}
	}
	return "", "", false
}

// lookupCity matches a city against zone cities and the extra city table. A
// known country rules out a namesake abroad: London, Ontario is not London.
func lookupCity(s, country string) (string, bool) {
	z, ok := load().cities[fold(cleanPlace(s))]
	if !ok {
		return "", false
	}
	if country != "" && ZoneCountry(z) != country {
		return "", false
	}
	return z, true
}

// cleanPlace drops the words LinkedIn and CRMs wrap a metro in.
func cleanPlace(s string) string {
	s = strings.TrimSpace(s)
	low := strings.ToLower(s)
	for _, p := range []string{"greater ", "metro "} {
		if strings.HasPrefix(low, p) {
			s, low = s[len(p):], low[len(p):]
		}
	}
	for _, suf := range []string{" metropolitan area", " metro area", " area", " metro", " region"} {
		if strings.HasSuffix(low, suf) {
			s = s[:len(s)-len(suf)]
			break
		}
	}
	return strings.TrimSpace(s)
}

// parseLocation reads "City, State, Country" in any of its partial forms,
// right to left. A last token that is both a state and a country ("CA",
// "Georgia") is settled by lastIsState.
func parseLocation(s, knownCountry, hint string) (country, region, city string) {
	var parts []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			parts = append(parts, p)
		}
	}
	if len(parts) == 0 {
		return "", "", ""
	}
	country = knownCountry
	if len(parts) > 1 {
		last := parts[len(parts)-1]
		if code, ok := ParseCountry(last); ok {
			switch {
			case knownCountry != "" && code == knownCountry:
				// It repeats the country column: "Kelowna, BC, Canada".
				parts = parts[:len(parts)-1]
			case knownCountry == "" && !lastIsState(last, parts[:len(parts)-1], code, hint):
				country = code
				parts = parts[:len(parts)-1]
			}
		}
	} else if country == "" {
		// One token: a country ("Netherlands", "GB"), a state ("CA") or a city.
		if code, ok := ParseCountry(parts[0]); ok && !lastIsState(parts[0], nil, code, hint) {
			return code, "", ""
		}
	}
	if len(parts) > 1 {
		if _, _, ok := lookupRegion(parts[len(parts)-1], country); ok {
			region = parts[len(parts)-1]
			parts = parts[:len(parts)-1]
		}
	} else if _, _, ok := lookupRegion(parts[0], country); ok && len(parts[0]) > 2 {
		return country, parts[0], ""
	}
	if len(parts) > 0 {
		city = parts[0]
	}
	return country, region, city
}

// lastIsState decides a last token that names both a country and a state:
// "Toronto, CA" is Canada and "Austin, CA" California; "Tbilisi, Georgia" is
// the country and "Atlanta, Georgia" the state. A known city decides, then the
// phone or domain country, and otherwise it is the state.
func lastIsState(token string, before []string, country, hint string) bool {
	if _, _, ok := lookupRegion(token, ""); !ok {
		return false
	}
	if len(before) > 0 {
		if z, ok := load().cities[fold(cleanPlace(before[0]))]; ok {
			return ZoneCountry(z) != country
		}
	}
	return hint != country
}
