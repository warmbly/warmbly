package replyclassify

import (
	"regexp"
	"slices"
	"strings"
)

// classifyLexicon is Layer 2: a deterministic, offline keyword scan over the
// subject + body. It returns (result, true) only on a CLEAR signal; ambiguous
// text returns (zero, false) so the optional model layer (or "unknown") decides.
//
// Order matters and encodes priority:
//  1. Compliance words (unsubscribe / stop / remove me / take me off) ALWAYS win.
//     Treating these as anything other than an unsubscribe request is a
//     compliance risk, so they short-circuit before sentiment.
//  2. A declined interest ("not interested") => negative. It is read before
//     the interest phrases, which would take its "interested" for interest.
//  3. Clear interest phrases => positive.
//  4. Clear rejection phrases => negative.
func classifyLexicon(in Input) (Result, bool) {
	text := quoteFolder.Replace(strings.ToLower(strings.TrimSpace(in.Subject + "\n" + StripQuoted(in.BodyText, slices.Concat(in.Languages, optOutLanguages)...))))
	if text == "" {
		return Result{}, false
	}

	// 1. Compliance / opt-out (highest priority).
	if matchesOptOut(text) {
		return Result{Class: ClassUnsubscribe, Confidence: 0.9, Source: SourceLexicon}, true
	}

	// Accents only, and read only by a chosen language's phrases; English matches the text as written.
	rules, folded := rulesFor(in.Languages), accentFolder.Replace(text)

	// 2. Declined interest => negative, before "interested" reads as interest.
	for _, kw := range rules.declined {
		if kw.in(text, folded) {
			return Result{Class: ClassNegative, Confidence: 0.8, Source: SourceLexicon}, true
		}
	}

	// 3. Clear interest => positive.
	for _, kw := range positiveKeywords {
		if strings.Contains(text, kw) {
			return Result{Class: ClassPositive, Confidence: 0.8, Source: SourceLexicon}, true
		}
	}

	// 4. Clear rejection => negative.
	for _, kw := range rules.negative {
		if kw.in(text, folded) {
			return Result{Class: ClassNegative, Confidence: 0.8, Source: SourceLexicon}, true
		}
	}

	return Result{}, false
}

// unsubscribeKeywords are explicit opt-out requests. Compliance-first: any of
// these short-circuits to "unsubscribe" before sentiment is considered. Each
// is matched on word boundaries, so "stop" alone never fires on "stop by".
var unsubscribeKeywords = []string{
	"unsubscribe",
	"opt out",
	"opt-out",
	"remove me",
	"remove my email",
	"remove my address",
	"take me off",
	"stop emailing",
	"stop sending",
	"stop contacting",
	"stop these emails",
	"no more emails",
	"do not contact",
	"don't contact",
	"do not email",
	"don't email",
	"do not send",
	"don't send",
	"please stop",
	"delete my details",
	"delete my data",
	"delete my information",
	// German, written as accentFolder leaves it ("löschen" reads "loeschen").
	// "abmelden" and "austragen" also cancel a meeting or a course, so they
	// count only next to "bitte" or the writer themselves.
	"bitte austragen",
	"austragen bitte",
	"mich austragen",
	"uns austragen",
	"mich auszutragen",
	"uns auszutragen",
	"um austragung",
	"tragen sie mich aus",
	"tragen sie uns aus",
	"tragen sie mich bitte aus",
	"tragen sie uns bitte aus",
	"trag mich aus",
	"trag mich bitte aus",
	"tragt mich aus",
	"tragt uns aus",
	"bitte abmelden",
	"abmelden bitte",
	"mich abmelden",
	"uns abmelden",
	"um abmeldung",
	"bitte melden sie mich ab",
	"melden sie mich bitte ab",
	"bitte melden sie uns ab",
	"melden sie uns bitte ab",
	"bitte abbestellen",
	"mich aus dem verteiler",
	"mich aus ihrem verteiler",
	"mich aus eurem verteiler",
	"mich aus deinem verteiler",
	"uns aus dem verteiler",
	"uns aus ihrem verteiler",
	"aus dem verteiler nehmen",
	"aus ihrem verteiler nehmen",
	"aus dem verteiler loeschen",
	"aus ihrem verteiler loeschen",
	"entfernen sie mich",
	"entfernen sie uns",
	"entfernen sie meine daten",
	"entfernen sie meine adresse",
	"entfernen sie meine e-mail",
	"entfern mich",
	"streichen sie mich aus",
	"streichen sie mich von",
	"streichen sie uns aus",
	"streichen sie uns von",
	"von der liste streichen",
	"von ihrer liste streichen",
	"nehmen sie mich aus",
	"nehmen sie mich von",
	"nehmen sie mich raus",
	"nehmen sie uns aus",
	"nimm mich raus",
	"keine weiteren e-mails",
	"keine weiteren emails",
	"keine weiteren mails",
	"keine weitere e-mail",
	"keine weitere email",
	"keine weitere mail",
	"keine e-mails mehr",
	"keine emails mehr",
	"keine mails mehr",
	"keine nachrichten mehr",
	"bitte keine werbung mehr",
	"mir keine werbung mehr",
	"uns keine werbung mehr",
	"keine werbemails",
	"stoppen sie die zusendung",
	"stoppen sie die zusendungen",
	"mich nicht mehr kontaktieren",
	"uns nicht mehr kontaktieren",
	"kontaktieren sie mich nicht mehr",
	"kontaktieren sie uns nicht mehr",
	"bitte nicht mehr kontaktieren",
	"bitte nicht mehr anschreiben",
	"nicht mehr angeschrieben",
	"schreiben sie mich nicht mehr an",
	"schreiben sie mir nicht mehr",
	"schreiben sie mir bitte nicht mehr",
	"schreib mir nicht mehr",
	"schreib mir bitte nicht mehr",
	"von weiteren kontaktaufnahmen",
	"keine weiteren kontaktaufnahmen",
	"von einer weiteren kontaktaufnahme",
	"keine weitere kontaktaufnahme",
	"von jeglicher kontaktaufnahme",
	"unterlassen sie jegliche",
	"unterlassen sie kuenftig",
	"unterlassen sie weitere",
	"loeschen sie meine daten",
	"loeschen sie bitte meine daten",
	"loeschen sie meine adresse",
	"loeschen sie meine e-mail",
	"loeschen sie meine kontaktdaten",
	"loeschen sie bitte meine adresse",
	"loeschen sie bitte meine e-mail",
	"loeschen sie mich",
	"meine daten loeschen",
	"meine daten zu loeschen",
}

// gdprObjection is a GDPR Art. 21 objection. The verb also means "I disagree"
// ("ich widerspreche hiermit Ihrer Preisberechnung"), so it counts only when
// the same sentence goes on to what an objection objects to, behind an
// article: "der Verarbeitung", "dem Erhalt weiterer E-Mails", "jeglicher
// Kontaktaufnahme". The words between vary too much for a phrase list
// ("ich widerspreche hiermit ausdrücklich gem. Art. 21 DSGVO der weiteren
// Datenverarbeitung"), so a dot counts only in those abbreviations, and a
// comma not at all ("da widerspreche ich, bei der Nutzung hatten wir nie
// Probleme"). Only the
// first person counts: a signature's privacy notice says "Sie können der
// Verarbeitung jederzeit widersprechen".
var gdprObjection = regexp.MustCompile(`(^|[^a-z0-9])(widerspreche|wir widersprechen|widersprechen wir)([^a-z0-9]|$)` +
	`(?:[^.!?:;,]|\b(?:gem|art|abs|nr|lit|i\.v\.m|z\.b|bzw|ggf)\.){0,90}?` +
	`\b(der|dem|den|einer|jeder|jede|jeglicher|jeglichen) (?:[a-z-]+ ){0,2}?[a-z-]*(verarbeitung|nutzung|verwendung|speicherung|weitergabe|zusendung|kontaktaufnahme|werbung|mails|nachrichten|newsletter)`)

// quoteFolder folds the typographic apostrophes mail clients substitute while
// typing, so "don’t email me" matches the straight-quote phrase. The opt-out
// scan folds with foldOptOut, which does the same and flattens accents.
var quoteFolder = strings.NewReplacer("\u2019", "'", "\u2018", "'", "\u02bc", "'", "\u00b4", "'", "`", "'")

func matchesOptOut(lowerText string) bool {
	// A plain-text reply wraps a phrase across lines; the phrases hold one
	// space between words.
	lowerText = foldOptOut(lowerText)
	for _, re := range optOutPatterns {
		if re.MatchString(lowerText) {
			return true
		}
	}
	return gdprObjection.MatchString(lowerText)
}

// MentionsOptOut reports opt-out wording anywhere in a message, quoted history
// included. It is not a decision: it finds the message an earlier reading of
// the whole body acted on.
func MentionsOptOut(subject, body string) bool {
	return matchesOptOut(strings.ToLower(subject + "\n" + body))
}

// IsOptOut reports whether a reply, read without its quoted history, asks to
// stop being emailed. It is the single check behind automatic suppression:
// the quoted original carries the sender's own opt-out line, so matching the
// whole body would opt out everyone who replies.
func IsOptOut(subject, body string) bool {
	text := strings.ToLower(strings.TrimSpace(subject + "\n" + StripQuoted(body, optOutLanguages...)))
	if text == "" {
		return false
	}
	return matchesOptOut(text)
}

// quoteMarker begins the quoted history a mail client appends to a reply.
//
// The attributions are matched anywhere, not only as whole lines: a client
// wraps a long "On ... wrote:" across two lines, and bodies stored before line
// breaks were kept sit on a single line, where an anchored marker never fires
// and the quoted footer ("unsubscribe") reads as the reply. An attribution
// carries a date, a time or an address between its words, which keeps prose
// such as "not on the team, our CTO wrote:" from cutting the reply short.
// Each language's markers live in languages.go.
type quoteMarker struct {
	re *regexp.Regexp
	// lineStart cuts from the start of the matched line: the attribution
	// opens with a name or a weekday the pattern does not reach back to.
	lineStart bool
}

// attribution matches "<lead> ... <tail>" with a year, a time or an address
// in between, on one line or wrapped across two.
func attribution(lead, tail string) *regexp.Regexp {
	return regexp.MustCompile(`(?is)(^|\s)` + lead + `\s[^\n]{0,250}?(\d{4}|\d{1,2}[:.]\d{2}|@)[^\n]{0,250}?\n?[^\n]{0,250}?\s` + tail)
}

// headerBlock matches Outlook's quoted header block, "<From>: ... <Sent>: ",
// which two labels within a few lines of each other identify.
func headerBlock(from, sent string) *regexp.Regexp {
	return regexp.MustCompile(`(?is)(^|\s)(` + from + `):\s.{1,300}?\s(` + sent + `):\s`)
}

// StripQuoted drops the quoted history from a reply body: everything from the
// first reply marker on, plus any line that is itself a ">" quote. langs adds
// those languages' markers to the base set.
func StripQuoted(body string, langs ...string) string {
	if body == "" {
		return ""
	}
	cut := len(body)
	for _, m := range rulesFor(langs).quote {
		loc := m.re.FindStringIndex(body)
		if loc == nil {
			continue
		}
		at := loc[0]
		// Only a body that keeps its line breaks has a line to go back to;
		// on a single stored line that would take the reply with it.
		if m.lineStart {
			if nl := strings.LastIndexByte(body[:at], '\n'); nl >= 0 {
				at = nl + 1
			}
		}
		if at < cut {
			cut = at
		}
	}
	body = body[:cut]
	lines := strings.Split(body, "\n")
	kept := lines[:0]
	for _, ln := range lines {
		if strings.HasPrefix(strings.TrimSpace(ln), ">") {
			continue
		}
		kept = append(kept, ln)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

// positiveKeywords are clear buying / interest signals. Kept conservative so the
// deterministic layer only fires on unambiguous intent; nuance is left to the
// model layer.
var positiveKeywords = []string{
	"interested",
	"sounds good",
	"sounds great",
	"let's chat",
	"lets chat",
	"let's talk",
	"lets talk",
	"happy to chat",
	"happy to talk",
	"set up a call",
	"book a call",
	"schedule a call",
	"schedule a demo",
	"book a demo",
	"send me more",
	"tell me more",
	"would love to",
	"count me in",
	"sign me up",
	"how much does it cost",
	"what's the pricing",
	"whats the pricing",
	"send pricing",
}

// declinedInterestKeywords turn "interested" around. "not interested" is the
// canonical cold-outreach brush-off, and every one of these carries
// "interested", so they are read before positiveKeywords.
var declinedInterestKeywords = []string{
	"not interested",
	"n't interested",
	"not really interested",
	"not very interested",
	"not that interested",
	"not too interested",
	"not particularly interested",
	"not currently interested",
	"no longer interested",
	"not be interested",
	"n't be interested",
	"uninterested",
	"disinterested",
}

// negativeKeywords are clear rejection signals.
var negativeKeywords = []string{
	"no thanks",
	"no thank you",
	"not a fit",
	"not the right",
	"not relevant",
	"no need",
	"we already have",
	"we have a solution",
	"not looking",
	"wrong person",
	"wrong contact",
	"please don't",
	"leave me alone",
	"go away",
}
