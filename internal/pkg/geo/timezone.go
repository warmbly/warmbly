package geo

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// zoneNames maps the abbreviations and Windows, Rails and Outlook display
// names exporters write to the zone they mean, keyed by fold().
var zoneNames = map[string]string{
	"est": "America/New_York", "edt": "America/New_York", "et": "America/New_York", "eastern": "America/New_York",
	"easterntime": "America/New_York", "easternstandardtime": "America/New_York", "easterntimeuscanada": "America/New_York",
	"useastern": "America/New_York", "easterndaylighttime": "America/New_York",
	"cst": "America/Chicago", "cdt": "America/Chicago", "ct": "America/Chicago", "central": "America/Chicago",
	"centraltime": "America/Chicago", "centralstandardtime": "America/Chicago", "centraltimeuscanada": "America/Chicago",
	"uscentral": "America/Chicago", "centraldaylighttime": "America/Chicago",
	"mst": "America/Denver", "mdt": "America/Denver", "mt": "America/Denver", "mountain": "America/Denver",
	"mountaintime": "America/Denver", "mountainstandardtime": "America/Denver", "mountaintimeuscanada": "America/Denver",
	"usmountain": "America/Denver", "usmountainstandardtime": "America/Phoenix", "arizona": "America/Phoenix",
	"pst": "America/Los_Angeles", "pdt": "America/Los_Angeles", "pt": "America/Los_Angeles", "pacific": "America/Los_Angeles",
	"pacifictime": "America/Los_Angeles", "pacificstandardtime": "America/Los_Angeles", "pacifictimeuscanada": "America/Los_Angeles",
	"uspacific": "America/Los_Angeles", "pacificdaylighttime": "America/Los_Angeles",
	"akst": "America/Anchorage", "akdt": "America/Anchorage", "alaska": "America/Anchorage", "alaskanstandardtime": "America/Anchorage",
	"hst": "Pacific/Honolulu", "hawaii": "Pacific/Honolulu", "hawaiianstandardtime": "Pacific/Honolulu",
	"ast": "America/Halifax", "atlantictime": "America/Halifax", "atlanticstandardtime": "America/Halifax", "atlantictimecanada": "America/Halifax",
	"nst": "America/St_Johns", "newfoundland": "America/St_Johns", "newfoundlandstandardtime": "America/St_Johns",
	"gmt": "Europe/London", "bst": "Europe/London", "gmtstandardtime": "Europe/London", "london": "Europe/London",
	"wet": "Europe/Lisbon",
	"cet": "Europe/Paris", "cest": "Europe/Paris", "met": "Europe/Paris", "centraleuropeantime": "Europe/Paris",
	"centraleuropestandardtime": "Europe/Budapest", "centraleuropeanstandardtime": "Europe/Warsaw",
	"romancestandardtime": "Europe/Paris", "westeuropestandardtime": "Europe/Berlin", "wedt": "Europe/Berlin",
	"eet": "Europe/Athens", "eest": "Europe/Athens", "easterneuropeantime": "Europe/Athens",
	"gtbstandardtime": "Europe/Bucharest", "flestandardtime": "Europe/Kyiv",
	"msk": "Europe/Moscow", "moscow": "Europe/Moscow", "russianstandardtime": "Europe/Moscow",
	"turkeystandardtime": "Europe/Istanbul", "israelstandardtime": "Asia/Jerusalem", "idt": "Asia/Jerusalem",
	"gst": "Asia/Dubai", "arabianstandardtime": "Asia/Dubai", "arabstandardtime": "Asia/Riyadh",
	"ist": "Asia/Kolkata", "indiastandardtime": "Asia/Kolkata", "pkt": "Asia/Karachi", "pakistanstandardtime": "Asia/Karachi",
	"ict": "Asia/Bangkok", "seasiastandardtime": "Asia/Bangkok", "wib": "Asia/Jakarta",
	"sgt": "Asia/Singapore", "singaporestandardtime": "Asia/Singapore", "hkt": "Asia/Hong_Kong",
	"chinastandardtime": "Asia/Shanghai", "taipeistandardtime": "Asia/Taipei", "pht": "Asia/Manila",
	"awst": "Australia/Perth", "wausstandardtime": "Australia/Perth",
	"jst": "Asia/Tokyo", "tokyostandardtime": "Asia/Tokyo", "kst": "Asia/Seoul", "koreastandardtime": "Asia/Seoul",
	"acst": "Australia/Adelaide", "acdt": "Australia/Adelaide", "cenaustraliastandardtime": "Australia/Adelaide",
	"aest": "Australia/Sydney", "aedt": "Australia/Sydney", "auseasternstandardtime": "Australia/Sydney",
	"eaustraliastandardtime": "Australia/Brisbane", "nzst": "Pacific/Auckland", "nzdt": "Pacific/Auckland",
	"newzealandstandardtime": "Pacific/Auckland", "sast": "Africa/Johannesburg", "southafricastandardtime": "Africa/Johannesburg",
	"wat": "Africa/Lagos", "eat": "Africa/Nairobi", "egyptstandardtime": "Africa/Cairo",
	"brt": "America/Sao_Paulo", "esouthamericastandardtime": "America/Sao_Paulo", "brasilia": "America/Sao_Paulo",
	"art": "America/Argentina/Buenos_Aires", "argentinastandardtime": "America/Argentina/Buenos_Aires",
	"centralstandardtimemexico": "America/Mexico_City", "sapacificstandardtime": "America/Bogota", "cot": "America/Bogota",
	"utc": "UTC", "z": "UTC", "zulu": "UTC", "coordinateduniversaltime": "UTC", "universal": "UTC", "etcutc": "UTC",
	"etcgmt": "UTC", "etcuniversal": "UTC", "etczulu": "UTC", "gmt0": "UTC", "etcgmt0": "UTC", "uct": "UTC",
	"etcuct": "UTC", "greenwich": "UTC", "etcgreenwich": "UTC",
	"est5edt": "America/New_York", "cst6cdt": "America/Chicago", "mst7mdt": "America/Denver", "pst8pdt": "America/Los_Angeles",
}

// fractionalOffsets names a real zone for each offset that is not whole hours;
// there is no Etc/ zone for them.
var fractionalOffsets = map[int]string{
	-570: "Pacific/Marquesas", -210: "America/St_Johns", 210: "Asia/Tehran", 270: "Asia/Kabul",
	330: "Asia/Kolkata", 345: "Asia/Kathmandu", 390: "Asia/Yangon", 525: "Australia/Eucla",
	570: "Australia/Darwin", 630: "Australia/Lord_Howe", 765: "Pacific/Chatham",
}

var (
	// "UTC+5", "GMT-05:00", "+0530", "UTC +2".
	offsetRe = regexp.MustCompile(`^(?i:utc|gmt)?\s*([+-])\s*(\d{1,2})(?::?(\d{2}))?$`)
	// "(GMT-05:00) Eastern Time (US & Canada)", the Windows and Rails picker format.
	labelledOffsetRe = regexp.MustCompile(`^\(\s*(?i:utc|gmt)\s*([+-]\d{1,2}(?::?\d{2})?)?\s*\)\s*(.*)$`)
)

// ParseTimezone reads a zone in any of the shapes exporters write: an IANA
// name in any case, an abbreviation (EST, CET, IST), a Windows or Rails
// display name, or a UTC offset. An abbreviation that means two things is
// read as the business zone most exporters mean by it (IST is India).
func ParseTimezone(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 80 {
		return "", false
	}
	if m := labelledOffsetRe.FindStringSubmatch(s); m != nil {
		if z, ok := namedZone(m[2]); ok {
			return z, true
		}
		if m[1] == "" {
			return "UTC", true
		}
		return offsetZone(m[1])
	}
	t := load()
	if z, ok := t.zoneFold[strings.ToLower(s)]; ok {
		return z, true
	}
	// An abbreviation before a legacy link: EST is New York, not the Panama link.
	if z, ok := namedZone(s); ok {
		return z, true
	}
	// A legacy name reads as the zone it now links to, so it carries a country.
	if z, ok := t.links[strings.ToLower(s)]; ok {
		if canonical, ok := t.zoneFold[strings.ToLower(z)]; ok {
			return canonical, true
		}
	}
	// A fixed-offset zone as written ("Etc/GMT-2"). Nothing else is stored, so
	// every stored zone is one routing and the segment picker know.
	for _, z := range OffsetZones() {
		if strings.EqualFold(z, s) {
			return z, true
		}
	}
	return offsetZone(s)
}

func namedZone(s string) (string, bool) {
	z, ok := zoneNames[fold(s)]
	return z, ok
}

// offsetZone turns a fixed offset into a zone. Whole hours become Etc/GMT
// zones, whose sign is inverted by POSIX convention.
func offsetZone(s string) (string, bool) {
	m := offsetRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return "", false
	}
	hours, _ := strconv.Atoi(m[2])
	minutes := 0
	if m[3] != "" {
		minutes, _ = strconv.Atoi(m[3])
	}
	if hours > 14 || minutes >= 60 {
		return "", false
	}
	total := hours*60 + minutes
	if m[1] == "-" {
		total = -total
	}
	if total == 0 {
		return "UTC", true
	}
	if minutes != 0 {
		z, ok := fractionalOffsets[total]
		return z, ok
	}
	if total < -12*60 {
		return "", false
	}
	if total > 0 {
		return fmt.Sprintf("Etc/GMT-%d", hours), true
	}
	return fmt.Sprintf("Etc/GMT+%d", hours), true
}

// OffsetZones are the zones a bare offset ("UTC+2") resolves to, UTC first.
func OffsetZones() []string {
	out := []string{"UTC"}
	for h := -12; h <= 14; h++ {
		switch {
		case h > 0:
			out = append(out, fmt.Sprintf("Etc/GMT-%d", h))
		case h < 0:
			out = append(out, fmt.Sprintf("Etc/GMT+%d", -h))
		}
	}
	return out
}

// ResolvableZones is every zone Resolve produces apart from a backward link
// written verbatim (US/Eastern), sorted, with the fixed offsets last.
func ResolvableZones() []string {
	return append(Zones(), OffsetZones()...)
}
