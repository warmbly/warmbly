package replyclassify

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Return-date extraction from an out-of-office auto-reply, so a held contact
// resumes when they are actually back. Deliberately conservative: a cue phrase
// has to introduce the date, it has to parse unambiguously, and it has to land
// inside a sane window; anything else falls back to the configured hold. See
// the guide at /guides/campaigns/#out-of-office-and-pausing-one-lead.

// ReturnWindowDays bounds how far ahead a parsed return date may be. Auto-reply
// bodies quote unrelated dates (a signature, a conference, a renewal), so a
// date beyond one quarter is treated as "not the return date" rather than as a
// reason to park the lead.
const ReturnWindowDays = 92

// ParseReturnDate finds the date an out-of-office reply says the recipient is
// back, as midnight UTC on that day. now anchors the year for dates written
// without one ("back on the 8th of September") and rejects dates in the past.
// Returns false when nothing parses with enough confidence.
func ParseReturnDate(subject, body string, now time.Time) (time.Time, bool) {
	text := normalizeForDates(subject + "\n" + StripQuoted(body))
	if text == "" {
		return time.Time{}, false
	}
	for _, m := range returnCue.FindAllStringIndex(text, -1) {
		// Only the span right after the cue is considered: an auto-reply is
		// mostly prose, and the first date anywhere in it is usually not the
		// one that matters.
		tail := text[m[1]:min(m[1]+returnCueWindow, len(text))]
		d, ok := firstDate(tail, now)
		if !ok {
			continue
		}
		if inclusiveCues[text[m[0]:m[1]]] {
			// The cue named the last day AWAY, not the day back.
			d = d.AddDate(0, 0, 1)
		}
		return d, true
	}
	return time.Time{}, false
}

// inclusiveCues name the last day of the absence rather than the first day
// back ("through Friday", "bis einschliesslich Freitag"), so the return date is
// the day after the one they wrote. Everything else in returnCue names the
// return itself; "until" is left out on purpose, because "out of the office
// until 12 September" is normally read as back ON the 12th.
var inclusiveCues = map[string]bool{
	"through":             true,
	"bis einschliesslich": true,
	"tot en met":          true,
}

// NextBusinessDay is the day after d, skipping Saturday and Sunday. A contact
// who is back on the 8th spends that day on the backlog, so the held step
// resumes on their next working day rather than landing in it.
func NextBusinessDay(d time.Time) time.Time {
	next := d.AddDate(0, 0, 1)
	for next.Weekday() == time.Saturday || next.Weekday() == time.Sunday {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

// returnCueWindow is how much text after a cue phrase may hold the date.
const returnCueWindow = 48

// returnCue are the phrases that introduce a return date, in the languages the
// out-of-office vocabulary covers. Matched on a lower-cased, accent-folded
// copy of the text.
var returnCue = regexp.MustCompile(`(?i)\b(` + strings.Join([]string{
	// English
	`back on`, `back in the office on`, `back at my desk on`, `be back on`,
	`return on`, `returning on`, `will return on`, `i return on`, `my return on`,
	`returns on`, `available again on`, `reachable again on`, `back from`,
	`until`, `till`, `through`,
	// German
	`zurueck am`, `zurueck ab`, `wieder am`, `wieder ab`, `ab dem`, `ab montag den`,
	`wieder erreichbar am`, `wieder erreichbar ab`, `wieder im buero am`,
	`bis einschliesslich`, `bis zum`, `bis`,
	// French
	`de retour le`, `jusqu'au`, `jusqu au`, `a partir du`,
	// Spanish / Portuguese
	`de vuelta el`, `regreso el`, `hasta el`, `a partir del`,
	`de volta a`, `ate o dia`, `a partir de`,
	// Dutch
	`terug op`, `weer aanwezig op`, `tot en met`,
	// Italian
	`di ritorno il`, `fino al`, `rientro il`,
}, `|`) + `)\b`)

// dateFormats are the unambiguous written forms, tried in order against the
// text right after a cue.
var (
	isoDate     = regexp.MustCompile(`\b(\d{4})-(\d{2})-(\d{2})\b`)
	dottedDate  = regexp.MustCompile(`\b(\d{1,2})\.\s?(\d{1,2})\.\s?(\d{4}|\d{2})?`)
	dayThenName = regexp.MustCompile(`\b(\d{1,2})(?:st|nd|rd|th|\.)?\s+(?:of\s+|de\s+|di\s+)?([a-z]{3,12})\.?(?:\s+(\d{4}))?`)
	// The day group ends on a word boundary, or "October 2026" would read the
	// "20" of the year as a day of the month and invent a return date.
	nameThenDay = regexp.MustCompile(`\b([a-z]{3,12})\.?\s+(\d{1,2})\b(?:st|nd|rd|th|\.)?(?:,?\s+(\d{4}))?`)
)

// firstDate returns the EARLIEST date the span yields, in text order rather
// than in the order the formats happen to be tried. A cue window holds prose as
// well as the date ("until 10 September; ref 2026-10-01"), and scanning ISO
// first would answer with the reference number's date and park the lead three
// weeks too long.
func firstDate(span string, now time.Time) (time.Time, bool) {
	type hit struct {
		at int
		d  time.Time
	}
	var hits []hit
	add := func(re *regexp.Regexp, parse func(m []string) (time.Time, bool)) {
		// The two calls walk the same matches in the same order, so the index
		// list lines up with the submatch list.
		at := re.FindAllStringIndex(span, -1)
		for i, m := range re.FindAllStringSubmatch(span, -1) {
			if d, ok := parse(m); ok {
				hits = append(hits, hit{at[i][0], d})
			}
		}
	}

	add(isoDate, func(m []string) (time.Time, bool) {
		return resolve(atoi(m[3]), atoi(m[2]), atoi(m[1]), now, true)
	})
	add(dottedDate, func(m []string) (time.Time, bool) {
		return resolve(atoi(m[1]), atoi(m[2]), yearOf(m[3]), now, m[3] != "")
	})
	add(dayThenName, func(m []string) (time.Time, bool) {
		mon, known := monthByName[m[2]]
		if !known {
			return time.Time{}, false
		}
		return resolve(atoi(m[1]), mon, yearOf(m[3]), now, m[3] != "")
	})
	add(nameThenDay, func(m []string) (time.Time, bool) {
		mon, known := monthByName[m[1]]
		if !known {
			return time.Time{}, false
		}
		return resolve(atoi(m[2]), mon, yearOf(m[3]), now, m[3] != "")
	})

	best := -1
	for i := range hits {
		if best < 0 || hits[i].at < hits[best].at {
			best = i
		}
	}
	if best < 0 {
		return time.Time{}, false
	}
	return hits[best].d, true
}

// resolve builds the date and applies the sanity window. When the reply wrote
// no year, the year is the one that puts the date in the future: an auto-reply
// sent in December naming "5 January" means next year.
func resolve(day, month, year int, now time.Time, explicitYear bool) (time.Time, bool) {
	if day < 1 || day > 31 || month < 1 || month > 12 {
		return time.Time{}, false
	}
	today := now.UTC().Truncate(24 * time.Hour)
	build := func(y int) (time.Time, bool) {
		d := time.Date(y, time.Month(month), day, 0, 0, 0, 0, time.UTC)
		// time.Date normalizes 31 February into March; a date that moved was
		// never a real date.
		if d.Day() != day || int(d.Month()) != month {
			return time.Time{}, false
		}
		return d, true
	}
	if explicitYear {
		d, ok := build(year)
		if !ok || d.Before(today) || d.After(today.AddDate(0, 0, ReturnWindowDays)) {
			return time.Time{}, false
		}
		return d, true
	}
	for _, y := range []int{today.Year(), today.Year() + 1} {
		d, ok := build(y)
		if ok && !d.Before(today) && !d.After(today.AddDate(0, 0, ReturnWindowDays)) {
			return d, true
		}
	}
	return time.Time{}, false
}

// yearOf reads a written year, expanding a two-digit one into the 2000s.
func yearOf(s string) int {
	if s == "" {
		return 0
	}
	y := atoi(s)
	if len(s) == 2 {
		y += 2000
	}
	return y
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

// monthByName maps month names and their common abbreviations, in the
// languages the out-of-office vocabulary covers, to a month number.
var monthByName = buildMonthIndex(map[int][]string{
	1:  {"january", "jan", "januar", "janvier", "enero", "ene", "janeiro", "januari", "gennaio"},
	2:  {"february", "feb", "februar", "fevrier", "febrero", "fevereiro", "februari", "febbraio"},
	3:  {"march", "mar", "maerz", "marz", "mars", "marzo", "marco", "maart"},
	4:  {"april", "apr", "avril", "abril", "aprile"},
	5:  {"may", "mai", "mayo", "maio", "mei", "maggio"},
	6:  {"june", "jun", "juni", "juin", "junio", "junho", "giugno"},
	7:  {"july", "jul", "juli", "juillet", "julio", "julho", "luglio"},
	8:  {"august", "aug", "aout", "agosto", "augustus", "ago"},
	9:  {"september", "sep", "sept", "septembre", "septiembre", "setembro", "settembre"},
	10: {"october", "oct", "oktober", "octobre", "octubre", "outubro", "okt", "ottobre"},
	11: {"november", "nov", "novembre", "noviembre", "novembro"},
	12: {"december", "dec", "dezember", "decembre", "diciembre", "dezembro", "dez", "december", "dicembre"},
})

func buildMonthIndex(src map[int][]string) map[string]int {
	out := make(map[string]int, 128)
	for month, names := range src {
		for _, n := range names {
			out[n] = month
		}
	}
	return out
}

// accentFolder flattens the accents, typographic quotes and non-breaking
// spaces the vocabularies would otherwise need two spellings for
// ("März"/"Maerz", "août"/"aout", "jusqu'au"/"jusqu’au").
var accentFolder = strings.NewReplacer(
	"\u2019", "'", "\u2018", "'", "\u02bc", "'", "\u00b4", "'", "`", "'",
	"ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss",
	"á", "a", "à", "a", "â", "a", "ã", "a", "å", "a",
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"ó", "o", "ò", "o", "ô", "o", "õ", "o",
	"ú", "u", "ù", "u", "û", "u",
	"ç", "c", "ñ", "n",
	// Polish and Nordic letters the away-message markers need.
	"ł", "l", "ą", "a", "ę", "e", "ć", "c", "ś", "s", "ź", "z", "ż", "z",
	"ø", "o", "æ", "ae", "å", "a",
	" ", " ",
)

// foldAccents lower-cases and folds the accents, quotes and spaces that would
// otherwise need a second spelling of every vocabulary entry, then collapses
// whitespace. Shared by the date cues and the out-of-office subject markers.
func foldAccents(s string) string {
	s = accentFolder.Replace(strings.ToLower(s))
	return strings.Join(strings.Fields(s), " ")
}

// normalizeForDates is foldAccents under the name the date scanner reads it by.
func normalizeForDates(s string) string { return foldAccents(s) }
