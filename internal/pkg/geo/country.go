package geo

import (
	_ "embed"
	"sort"
	"strings"
	"sync"
	// Every zone the resolver hands out must load on any image, whatever tzdata it ships.
	_ "time/tzdata"

	"golang.org/x/text/language"
)

// The two tzdb tables (public domain): ISO 3166 names, and each zone with its country.
//
//go:embed data/iso3166.tab
var iso3166Tab string

//go:embed data/zone.tab
var zoneTab string

//go:embed data/links.tab
var linksTab string

// Country is one ISO 3166-1 country as the tz database names it.
type Country struct {
	Code string `json:"code"`
	Name string `json:"name"`
	// Zone is the country's most populous zone, used when nothing narrows it.
	Zone string `json:"zone"`
	// MultiZone marks a country that spans more than one UTC offset.
	MultiZone bool `json:"multi_zone"`
}

type tables struct {
	countries   map[string]*Country
	byName      map[string]string // folded name or alias -> code
	zoneCountry map[string]string // zone -> code
	zoneFold    map[string]string // lower(zone) -> zone
	links       map[string]string // lower(legacy name) -> zone, "US/Eastern" -> "America/New_York"
	cities      map[string]string // folded city -> zone
	zones       []string
	ordered     []Country
}

var (
	loadOnce sync.Once
	data     *tables
)

// populousZone overrides zone.tab's first zone where the table's geographic
// order does not put the most populous zone first.
var populousZone = map[string]string{
	"AU": "Australia/Sydney", "BR": "America/Sao_Paulo", "CA": "America/Toronto",
	"RU": "Europe/Moscow", "UA": "Europe/Kyiv", "UZ": "Asia/Tashkent",
	"CL": "America/Santiago", "EC": "America/Guayaquil", "PT": "Europe/Lisbon",
	"ES": "Europe/Madrid", "KI": "Pacific/Tarawa", "FM": "Pacific/Pohnpei",
}

// singleOffset lists multi-row countries whose zones all share one offset, so
// the country alone pins the clock.
var singleOffset = map[string]bool{
	"DE": true, "CN": true, "UZ": true, "AR": true, "MY": true, "CY": true,
	"KZ": true,
}

// countryAliases are the names exporters write that the tz table spells otherwise.
var countryAliases = map[string]string{
	"usa": "US", "unitedstatesofamerica": "US", "america": "US", "unitedstatesamerica": "US",
	"uk": "GB", "unitedkingdom": "GB", "greatbritain": "GB", "britain": "GB", "england": "GB",
	"scotland": "GB", "wales": "GB", "northernireland": "GB", "gbr": "GB",
	"southkorea": "KR", "korea": "KR", "republicofkorea": "KR", "kor": "KR", "northkorea": "KP",
	"russianfederation": "RU", "rus": "RU", "czechia": "CZ", "cze": "CZ", "holland": "NL",
	"thenetherlands": "NL", "nld": "NL", "deutschland": "DE", "deu": "DE", "germany": "DE",
	"espana": "ES", "esp": "ES", "france": "FR", "fra": "FR", "italia": "IT", "ita": "IT",
	"schweiz": "CH", "suisse": "CH", "che": "CH", "osterreich": "AT", "aut": "AT",
	"uae": "AE", "are": "AE", "emirates": "AE", "ksa": "SA", "sau": "SA",
	"turkiye": "TR", "tur": "TR", "ivorycoast": "CI", "cotedivoire": "CI", "capeverde": "CV", "caboverde": "CV",
	"vietnam": "VN", "vnm": "VN", "laos": "LA", "iran": "IR", "syria": "SY", "taiwan": "TW", "twn": "TW",
	"hongkong": "HK", "hkg": "HK", "macau": "MO", "macao": "MO", "burma": "MM", "myanmar": "MM",
	"swaziland": "SZ", "eswatini": "SZ", "vatican": "VA", "holysee": "VA", "palestine": "PS",
	"macedonia": "MK", "northmacedonia": "MK", "moldova": "MD", "bolivia": "BO", "venezuela": "VE",
	"tanzania": "TZ", "brunei": "BN", "micronesia": "FM", "drc": "CD", "drcongo": "CD",
	"democraticrepublicofthecongo": "CD", "republicofthecongo": "CG", "congo": "CG",
	"can": "CA", "aus": "AU", "nzl": "NZ", "ind": "IN", "chn": "CN", "jpn": "JP", "bra": "BR",
	"mex": "MX", "arg": "AR", "zaf": "ZA", "sgp": "SG", "irl": "IE", "swe": "SE", "nor": "NO",
	"dnk": "DK", "fin": "FI", "pol": "PL", "prt": "PT", "bel": "BE", "isr": "IL", "egy": "EG",
	"nga": "NG", "ken": "KE", "phl": "PH", "idn": "ID", "tha": "TH", "mys": "MY", "pak": "PK",
	"bgd": "BD", "col": "CO", "chl": "CL", "per": "PE", "ukr": "UA", "rou": "RO", "hun": "HU",
	"grc": "GR", "bgr": "BG", "hrv": "HR", "srb": "RS", "svk": "SK", "svn": "SI", "ltu": "LT",
	"lva": "LV", "est": "EE", "isl": "IS", "lux": "LU", "mlt": "MT", "cyp": "CY",
	"phillipines": "PH", "philippines": "PH", "newzealand": "NZ", "southafrica": "ZA",
	"saudiarabia": "SA", "unitedarabemirates": "AE", "czechrepublic": "CZ",
}

func load() *tables {
	loadOnce.Do(func() {
		t := &tables{
			countries:   map[string]*Country{},
			byName:      map[string]string{},
			zoneCountry: map[string]string{},
			zoneFold:    map[string]string{},
			links:       map[string]string{},
			cities:      map[string]string{},
		}
		for _, line := range strings.Split(iso3166Tab, "\n") {
			if line == "" || line[0] == '#' {
				continue
			}
			code, name, ok := strings.Cut(line, "\t")
			if !ok || len(code) != 2 {
				continue
			}
			t.countries[code] = &Country{Code: code, Name: strings.TrimSpace(name)}
			t.byName[fold(name)] = code
			// "Korea (South)" and "Britain (UK)": the bare name reads as the country too.
			if bare, _, ok := strings.Cut(name, " ("); ok {
				if _, taken := t.byName[fold(bare)]; !taken {
					t.byName[fold(bare)] = code
				}
			}
		}
		rows := map[string]int{}
		for _, line := range strings.Split(zoneTab, "\n") {
			if line == "" || line[0] == '#' {
				continue
			}
			cols := strings.Split(line, "\t")
			if len(cols) < 3 {
				continue
			}
			code, zone := cols[0], cols[2]
			c := t.countries[code]
			if c == nil {
				continue
			}
			rows[code]++
			if c.Zone == "" {
				c.Zone = zone
			}
			t.zoneCountry[zone] = code
			t.zoneFold[strings.ToLower(zone)] = zone
			t.zones = append(t.zones, zone)
			city := zone[strings.LastIndex(zone, "/")+1:]
			t.cities[fold(strings.ReplaceAll(city, "_", " "))] = zone
		}
		for code, c := range t.countries {
			if z, ok := populousZone[code]; ok {
				c.Zone = z
			}
			c.MultiZone = rows[code] > 1 && !singleOffset[code]
		}
		for alias, code := range countryAliases {
			t.byName[fold(alias)] = code
		}
		for city, zone := range extraCities {
			t.cities[fold(city)] = zone
		}
		t.zoneFold["utc"] = "UTC"
		for _, line := range strings.Split(linksTab, "\n") {
			if line == "" || line[0] == '#' {
				continue
			}
			if link, target, ok := strings.Cut(line, "\t"); ok {
				t.links[strings.ToLower(link)] = target
			}
		}
		sort.Strings(t.zones)
		for _, c := range t.countries {
			if c.Zone != "" {
				t.ordered = append(t.ordered, *c)
			}
		}
		sort.Slice(t.ordered, func(i, j int) bool { return t.ordered[i].Name < t.ordered[j].Name })
		data = t
	})
	return data
}

// Countries lists every country that has a zone, sorted by name.
func Countries() []Country {
	return append([]Country(nil), load().ordered...)
}

// CountryByCode returns the country for an ISO 3166-1 alpha-2 code.
func CountryByCode(code string) (Country, bool) {
	c, ok := load().countries[strings.ToUpper(strings.TrimSpace(code))]
	if !ok || c.Zone == "" {
		return Country{}, false
	}
	return *c, true
}

// ParseCountry reads an alpha-2 or alpha-3 code, an English name, or a common
// alias ("USA", "UK", "Deutschland") and returns the alpha-2 code.
func ParseCountry(s string) (string, bool) {
	t := load()
	s = strings.TrimSpace(s)
	if len(s) == 2 {
		if c, ok := t.countries[strings.ToUpper(s)]; ok && c.Zone != "" {
			return c.Code, true
		}
		// "UK" is not the ISO code but is what everyone writes.
		if strings.EqualFold(s, "uk") {
			return "GB", true
		}
		return "", false
	}
	if code, ok := t.byName[fold(s)]; ok {
		return code, true
	}
	// Every ISO 3166-1 alpha-3 code ("AFG"), from the CLDR region table.
	if len(s) == 3 && isLetters(s) {
		if r, err := language.ParseRegion(s); err == nil {
			if c, ok := t.countries[r.String()]; ok && c.Zone != "" {
				return c.Code, true
			}
		}
	}
	return "", false
}

func isLetters(s string) bool {
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return false
		}
	}
	return true
}

// Zones lists every zone the resolver can produce from location data, sorted.
func Zones() []string {
	return append([]string(nil), load().zones...)
}

// ZoneCountry is the country a zone belongs to, "" for UTC and offset zones.
func ZoneCountry(zone string) string {
	return load().zoneCountry[zone]
}

// fold lowercases, strips accents that ASCII exporters drop, and keeps only
// letters and digits, so "Côte d'Ivoire" and "cote divoire" compare equal.
func fold(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if a, ok := accentFold[r]; ok {
			b.WriteString(a)
			continue
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

var accentFold = map[rune]string{
	'á': "a", 'à': "a", 'â': "a", 'ä': "a", 'ã': "a", 'å': "a", 'ç': "c", 'é': "e", 'è': "e",
	'ê': "e", 'ë': "e", 'í': "i", 'ì': "i", 'î': "i", 'ï': "i", 'ñ': "n", 'ó': "o", 'ò': "o",
	'ô': "o", 'ö': "o", 'õ': "o", 'ø': "o", 'ú': "u", 'ù': "u", 'û': "u", 'ü': "u", 'ý': "y",
	'ß': "ss", 'ł': "l", 'ś': "s", 'ź': "z", 'ż': "z", 'ć': "c", 'ń': "n", 'ę': "e", 'ą': "a",
	'ő': "o", 'ű': "u", 'š': "s", 'č': "c", 'ž': "z", 'ř': "r", 'ğ': "g", 'ı': "i", 'ş': "s",
}
