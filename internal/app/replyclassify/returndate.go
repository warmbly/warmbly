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
// langs are the workspace's tagging languages, which add their cues and months.
func ParseReturnDate(subject, body string, now time.Time, langs ...string) (time.Time, bool) {
	r, ok := FindReturnDate(subject, body, now, langs...)
	return r.Back, ok
}

// ReturnDate is a parsed return date and the words it was read from.
type ReturnDate struct {
	// Back is the first day the recipient is back, midnight UTC.
	Back time.Time
	// Phrase is the cue and the date as written, lower-cased with accents
	// flattened ("bis einschliesslich 18.9."), so a reader can find it in the
	// message without redoing the parser's arithmetic.
	Phrase string
}

// FindReturnDate is ParseReturnDate with the phrase the date came from.
func FindReturnDate(subject, body string, now time.Time, langs ...string) (ReturnDate, bool) {
	text := normalizeForDates(subject + "\n" + StripQuoted(body, langs...))
	if text == "" {
		return ReturnDate{}, false
	}
	rs := rulesFor(langs)
	at := func(m []int) (ReturnDate, bool) {
		// Only the span right after the cue is considered: an auto-reply is
		// mostly prose, and the first date anywhere in it is usually not the
		// one that matters.
		cue := text[m[0]:m[1]]
		start := m[1]
		if strings.HasSuffix(cue, "kw") {
			// Leave the week marker in the span for the week pattern.
			start -= len("kw")
		}
		tail := text[start:min(m[1]+returnCueWindow, len(text))]
		d, kind, end, ok := firstDate(tail, now, rs.months, endCues[cue])
		if !ok {
			return ReturnDate{}, false
		}
		phrase := strings.TrimSpace(strings.ReplaceAll(text[m[0]:start+end], sentenceMark, ""))
		switch {
		case kind == weekDate && endCues[cue]:
			// "bis KW 41" and "bis KW 40/41" are away through the last week:
			// back the Monday after it.
			d = d.AddDate(0, 0, 7)
		case kind == weekDate:
			// A week already under way means back now, not on its Monday.
			if today := now.UTC().Truncate(24 * time.Hour); d.Before(today) {
				d = today
			}
		case rs.inclusive[cue]:
			// The cue named the last day AWAY, not the day back.
			d = d.AddDate(0, 0, 1)
		}
		return ReturnDate{Back: d, Phrase: phrase}, true
	}
	cues := rs.cue.FindAllStringIndex(text, -1)
	for i, m := range cues {
		r, ok := at(m)
		if !ok {
			continue
		}
		// "ab Freitag, 4.9., bis 18.9." names the first day away, and the
		// "bis" right behind it the end of the absence. Not when a sentence
		// or a return word comes between: "ab dem 14.9. wieder erreichbar,
		// bis zum 30.9. nur eingeschränkt" is back on the 14th.
		if strings.HasPrefix(text[m[0]:m[1]], "ab ") && i+1 < len(cues) {
			if next := cues[i+1]; next[0] < m[1]+returnCueWindow && endCues[text[next[0]:next[1]]] &&
				!rangeBreak.MatchString(rangeNoise.ReplaceAllString(text[m[1]:next[0]], "")) {
				if end, ok := at(next); ok && end.Back.After(r.Back) {
					return end, true
				}
			}
		}
		return r, true
	}
	return ReturnDate{}, false
}

// Cues in the inclusive list name the last day of the absence rather than the
// first day back ("through Friday", "bis einschliesslich Freitag"), so the
// return date is the day after the one they wrote. "until" is left out on
// purpose, because "out of the office until 12 September" is normally read as
// back ON the 12th.

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

// endCues name the end of an absence: the last week away when a calendar
// week follows them, and the end of an "ab ... bis ..." range.
var endCues = map[string]bool{"bis": true, "bis zum": true, "bis einschliesslich": true}

// rangeBreak is a sentence end or a return word between an "ab" date and a
// "bis" date, which makes them two statements instead of one range.
var rangeBreak = regexp.MustCompile(sentenceMark + `|[a-z][.!?;:](\s|$)|[!?;]|\b(wieder|zurueck|erreichbar|buero|da)\b`)

// sentenceMark stands in for a sentence that opens on "Bis" after a date's
// dot ("ab dem 4.9. Bis zum 11.9. vertritt mich"), which lower-casing hides.
const sentenceMark = "\x00"

var sentenceStartBis = regexp.MustCompile(`([.!?;:])(\s+)Bis\b`)

// rangeNoise is taken out before rangeBreak reads the text: an absence said
// as a negated return word ("ab dem 7.9. nicht erreichbar, bis 18.9.") and a
// month abbreviation's dot ("ab dem 4. Sept. bis zum 18. Sept.") are not
// breaks.
var rangeNoise = regexp.MustCompile(`\bnicht (mehr )?(im )?(buero|erreichbar|da)\b|\b(jan|feb|mrz|apr|jun|jul|aug|sep|sept|okt|nov|dez)\.`)

// dateKind tells a day from a calendar week, returned as its Monday.
type dateKind int

const (
	dayDate dateKind = iota
	weekDate
)

// dateFormats are the unambiguous written forms, tried in order against the
// text right after a cue.
var (
	isoDate     = regexp.MustCompile(`\b(\d{4})-(\d{2})-(\d{2})\b`)
	dottedDate  = regexp.MustCompile(`\b(\d{1,2})\.\s?(\d{1,2})\.\s?(\d{4}|\d{2})?`)
	dayThenName = regexp.MustCompile(`\b(\d{1,2})(?:st|nd|rd|th|\.)?\s+(?:of\s+|de\s+|di\s+)?([a-z]{3,12})\.?(?:\s+(\d{4}))?`)
	// The day group ends on a word boundary, or "October 2026" would read the
	// "20" of the year as a day of the month and invent a return date.
	nameThenDay = regexp.MustCompile(`\b([a-z]{3,12})\.?\s+(\d{1,2})\b(?:st|nd|rd|th|\.)?(?:,?\s+(\d{4}))?`)
	// A German calendar week, "KW 42" or "KW42", or a range, "KW 41/42".
	calendarWeek = regexp.MustCompile(`\bkw\s?(\d{1,2})(?:\s?[-/–]\s?(\d{1,2}))?\b`)
)

// firstDate returns the EARLIEST date the span yields, in text order rather
// than in the order the formats happen to be tried. A cue window holds prose as
// well as the date ("until 10 September; ref 2026-10-01"), and scanning ISO
// first would answer with the reference number's date and park the lead three
// weeks too long. kind tells a day from a calendar week, and end is where the
// date's text stops in span; lastWeek reads a range of weeks by its last week
// rather than its first.
func firstDate(span string, now time.Time, monthByName map[string]int, lastWeek bool) (time.Time, dateKind, int, bool) {
	type hit struct {
		at   int
		end  int
		d    time.Time
		kind dateKind
	}
	var hits []hit
	add := func(re *regexp.Regexp, parse func(m []string) (time.Time, bool)) {
		// The two calls walk the same matches in the same order, so the index
		// list lines up with the submatch list.
		at := re.FindAllStringIndex(span, -1)
		for i, m := range re.FindAllStringSubmatch(span, -1) {
			if d, ok := parse(m); ok {
				kind := dayDate
				if re == calendarWeek {
					kind = weekDate
				}
				hits = append(hits, hit{at[i][0], at[i][1], d, kind})
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
	add(calendarWeek, func(m []string) (time.Time, bool) {
		if lastWeek && m[2] != "" {
			return resolveWeek(atoi(m[2]), now)
		}
		return resolveWeek(atoi(m[1]), now)
	})

	best := -1
	for i := range hits {
		if best < 0 || hits[i].at < hits[best].at {
			best = i
		}
	}
	if best < 0 {
		return time.Time{}, dayDate, 0, false
	}
	return hits[best].d, hits[best].kind, hits[best].end, true
}

// resolveWeek is the Monday of ISO week w: this year's while that week is not
// over, else next year's. The Monday has to land inside the sanity window,
// unless the week is the one under way.
func resolveWeek(w int, now time.Time) (time.Time, bool) {
	if w < 1 || w > 53 {
		return time.Time{}, false
	}
	today := now.UTC().Truncate(24 * time.Hour)
	year, _ := today.ISOWeek()
	for _, y := range []int{year, year + 1} {
		// 4 January always falls in week 1.
		jan4 := time.Date(y, time.January, 4, 0, 0, 0, 0, time.UTC)
		monday := jan4.AddDate(0, 0, 7*(w-1)-(int(jan4.Weekday())+6)%7)
		// Week 53 of a year that has 52 is the next year's week 1.
		if _, got := monday.ISOWeek(); got != w {
			continue
		}
		if monday.AddDate(0, 0, 6).Before(today) || monday.After(today.AddDate(0, 0, ReturnWindowDays)) {
			continue
		}
		return monday, true
	}
	return time.Time{}, false
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
	"ł", "l", "ą", "a", "ę", "e", "ć", "c", "ś", "s", "ź", "z", "ż", "z", "ń", "n",
	"ø", "o", "æ", "ae",
	// Czech, Slovak, Romanian, Hungarian and Turkish.
	"č", "c", "ř", "r", "š", "s", "ž", "z", "ý", "y", "ů", "u", "ě", "e", "ň", "n",
	"ť", "t", "ď", "d", "ľ", "l", "ĺ", "l", "ŕ", "r",
	"ă", "a", "ș", "s", "ş", "s", "ț", "t", "ţ", "t",
	"ő", "o", "ű", "u", "ı", "i", "ğ", "g",
	// The dot a lower-cased Turkish "İ" keeps, and Greek and Cyrillic marks
	// an all-caps subject drops.
	"\u0307", "",
	"ά", "α", "έ", "ε", "ή", "η", "ί", "ι", "ό", "ο", "ύ", "υ", "ώ", "ω", "ϊ", "ι", "ϋ", "υ", "ё", "е",
	" ", " ",
)

// foldAccents lower-cases and folds the accents, quotes and spaces that would
// otherwise need a second spelling of every vocabulary entry, then collapses
// whitespace. Shared by the date cues and the out-of-office subject markers.
func foldAccents(s string) string {
	s = accentFolder.Replace(strings.ToLower(s))
	return strings.Join(strings.Fields(s), " ")
}

// normalizeForDates is foldAccents with "Kalenderwoche" shortened to the
// "KW" the week cues and pattern read, and "KW42" spaced so a cue ending in
// "kw" still ends on a word boundary.
func normalizeForDates(s string) string {
	s = sentenceStartBis.ReplaceAllString(s, "$1"+sentenceMark+"$2Bis")
	return weekDigits.ReplaceAllString(strings.ReplaceAll(foldAccents(s), "kalenderwoche", "kw"), "kw $1")
}

var weekDigits = regexp.MustCompile(`\bkw(\d)`)
