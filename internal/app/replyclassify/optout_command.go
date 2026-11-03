package replyclassify

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The command words stand alone only when the reply is nothing else, which is
// why "stop" is not in unsubscribeKeywords. Besides English, the one word a
// German, Dutch, Nordic or Spanish reader types for the same thing.
const (
	commandWords  = `(?:stop|stopp|remove|remove me|unsubscribe|unsubscribe me|opt out|optout|abmelden|austragen|afmelden|baja)`
	commandPolite = `(?:please|pls|plz|now|thanks|thank you|thx|ty|bitte|danke|graag|alstublieft|aub|por favor|gracias|svp|merci|tack)`
)

// optOutCommand matches one line, read through commandText, that is only an
// opt-out command and the courtesy around it: "STOP!!", "Remove.", "please stop".
var optOutCommand = regexp.MustCompile(`^(?:` + commandPolite + ` )*` + commandWords + `(?: (?:` + commandWords + `|` + commandPolite + `))*$`)

// commandGreetings and commandSignOffs open a salutation or sign-off line,
// read through commandText; only name-like words may follow them.
var (
	commandGreetings = wordSet("hi", "hello", "hey", "dear", "hallo", "hola", "bonjour", "hej", "good morning", "good afternoon", "guten tag", "moin")
	commandSignOffs  = wordSet("thanks", "thank you", "thx", "ty", "many thanks", "cheers", "regards", "best", "best regards", "kind regards",
		"warm regards", "sincerely", "br", "kr", "mfg", "lg", "vg", "viele gruesse", "beste gruesse", "freundliche gruesse",
		"mit freundlichen gruessen", "gruss", "gruesse", "saludos", "cordialement", "bien a vous", "met vriendelijke groet",
		"groeten", "mvh", "hilsen", "med vanlig halsning")
)

// commandFooter is a whole mobile-client footer line, read through commandText.
var commandFooter = regexp.MustCompile(`^(?:sent from my (?:\S+ ?){1,3}|sent from (?:outlook|yahoo mail|mail for windows|gmail)(?: for (?:ios|android|windows|mac))?|get outlook for (?:ios|android)|sent (?:via|with) \S+(?: \S+)?|von meinem (?:\S+ ?){1,3}gesendet|gesendet von meinem (?:\S+ ?){1,3}|enviado desde mi (?:\S+ ?){1,3}|envoye de mon (?:\S+ ?){1,3}|verzonden vanaf mijn (?:\S+ ?){1,3}|skickat fran min (?:\S+ ?){1,3}|inviato dal mio (?:\S+ ?){1,3})$`)

// commandDisclaimer opens a legal disclaimer, whose text below is boilerplate.
var commandDisclaimer = regexp.MustCompile(`^(?:confidentiality notice(?: |$)|(?:confidentiality|disclaimer)$|(?:this|the information (?:contained )?in this) (?:e mail|email|message)(?: and any (?:attachments?|files)(?: transmitted with it)?)? (?:is|are|may be|contains?) (?:strictly )?(?:confidential|privileged|intended))`)

// proseWords mark a sentence rather than a signature line, even capitalized.
var proseWords = wordSet("i", "me", "my", "we", "us", "our", "you", "your", "it", "is", "are", "am", "was", "be", "but", "not",
	"don", "do", "would", "could", "can", "will", "if", "so", "just", "actually", "love", "like", "want", "let", "lets",
	"interested", "please", "call", "send", "tell", "talk", "great", "sounds", "ich", "wir", "sie", "ist", "aber", "nicht")

// signatureConnectors are the lowercase words a title or company name carries.
var signatureConnectors = wordSet("of", "and", "at", "for", "the", "de", "van", "von", "der", "la", "le", "&")

// signatureMaxLines bounds the signature a command may carry.
const signatureMaxLines = 8

func wordSet(words ...string) map[string]bool {
	out := make(map[string]bool, len(words))
	for _, w := range words {
		out[w] = true
	}
	return out
}

// commandText folds a line for the command patterns: case, accents and
// punctuation are dropped, words single-spaced.
func commandText(line string) string {
	folded := []rune(foldOptOut(line))
	for i, r := range folded {
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) {
			folded[i] = ' '
		}
	}
	return strings.Join(strings.Fields(string(folded)), " ")
}

// isOptOutCommand reports a reply, quoted history already cut, that says
// nothing but an opt-out command: its first line after any greeting is the
// command, and every line below is a sign-off, a footer or a signature.
func isOptOutCommand(body string) bool {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	for i, raw := range lines {
		switch {
		case optOutCommand.MatchString(commandText(raw)):
			return signatureOnly(lines[i+1:])
		case strings.TrimSpace(raw) == "" || ledByNames(raw, commandGreetings):
			continue
		default:
			return false
		}
	}
	return false
}

// signatureOnly reports whether the lines under the command are only a
// sign-off, footers and a name-like signature. A "-- " delimiter or a
// disclaimer ends the reading; anything else unrecognised is prose.
func signatureOnly(lines []string) bool {
	n := 0
	for _, raw := range lines {
		ln := strings.TrimSpace(raw)
		if ln == "" {
			continue
		}
		if strings.HasPrefix(ln, "--") || strings.HasPrefix(ln, "__") {
			return true
		}
		text := commandText(ln)
		if commandDisclaimer.MatchString(text) {
			return true
		}
		if commandFooter.MatchString(text) || ledByNames(ln, commandSignOffs) {
			continue
		}
		if n++; n > signatureMaxLines || !signatureLine(ln) {
			return false
		}
	}
	return true
}

// ledByNames reports a line that opens with one of the phrases and carries at
// most three name-like words after it: "Thanks, John", "Hi Jane,".
func ledByNames(raw string, phrases map[string]bool) bool {
	tokens := strings.Fields(raw)
	for k := min(len(tokens), 4); k > 0; k-- {
		if !phrases[commandText(strings.Join(tokens[:k], " "))] {
			continue
		}
		rest := tokens[k:]
		if len(rest) > 3 {
			return false
		}
		for _, t := range rest {
			if !nameLike(t) {
				return false
			}
		}
		return true
	}
	return false
}

// signatureLine is a short line made only of name-like tokens: a name, a
// title, a company, a phone number, an address or a link.
func signatureLine(raw string) bool {
	tokens := strings.Fields(raw)
	if len(tokens) == 0 || len(tokens) > 6 {
		return false
	}
	for _, t := range tokens {
		if !nameLike(t) {
			return false
		}
	}
	return true
}

// nameLike is one signature token: a capitalized word or initial, a short
// abbreviation, a connector, a separator, a phone number, an address or a link.
func nameLike(token string) bool {
	t := strings.TrimRight(token, ",;:")
	if t == "" || strings.Trim(t, "|-–—·/•") == "" {
		return true
	}
	if strings.ContainsAny(t, "'’`?!") {
		return false
	}
	lower := strings.ToLower(t)
	if strings.Contains(t, "@") || strings.HasPrefix(lower, "http") || strings.HasPrefix(lower, "www.") {
		return true
	}
	if phoneLike(t) {
		return true
	}
	if signatureConnectors[lower] {
		return true
	}
	word, dotted := strings.CutSuffix(t, ".")
	if word == "" || proseWords[strings.ToLower(word)] {
		return false
	}
	if dotted && utf8.RuneCountInString(word) > 3 {
		return false
	}
	first, _ := utf8.DecodeRuneInString(word)
	if !unicode.IsUpper(first) {
		return false
	}
	for _, r := range word {
		if !unicode.IsLetter(r) && !unicode.Is(unicode.Mn, r) && r != '-' && r != '.' && r != '&' {
			return false
		}
	}
	return true
}

// phoneLike is a token of digits and the punctuation phone numbers carry.
func phoneLike(t string) bool {
	digit := false
	for _, r := range t {
		switch {
		case unicode.IsDigit(r):
			digit = true
		case strings.ContainsRune("+()-./x", r):
		default:
			return false
		}
	}
	return digit
}
