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

// commandGreeting is a salutation line above the command ("Hi John,").
var commandGreeting = regexp.MustCompile(`^(?:hi|hello|hey|dear|hallo|hola|bonjour|hej|good morning|good afternoon|guten tag|moin)(?: \S+){0,3}$`)

// commandSignOff opens the sign-off that ends a reply, name and all.
var commandSignOff = regexp.MustCompile(`^(?:thanks|thank you|thx|many thanks|cheers|regards|best|best regards|kind regards|warm regards|sincerely|br|kr|mfg|lg|vg|viele gruesse|beste gruesse|freundliche gruesse|mit freundlichen gruessen|gruss|gruesse|saludos|cordialement|bien a vous|met vriendelijke groet|groeten|mvh|hilsen|med vanlig halsning)(?: \S+){0,4}$`)

// commandFooter is a line from which everything below is boilerplate: a mobile
// client's footer or a legal disclaimer.
var commandFooter = regexp.MustCompile(`^(?:sent from|sent via|sent with|get outlook|download outlook|envoye de|envoye depuis|gesendet von|von meinem|enviado desde|enviado do|verzonden vanaf|verstuurd vanaf|skickat fran|inviato da|confidential|this email|this e mail|this message|disclaimer|the information contained|diese e mail|diese nachricht)`)

// proseWords mark a sentence rather than a signature line (a name, a title, a
// company, a phone number).
var proseWords = map[string]bool{
	"i": true, "me": true, "my": true, "we": true, "us": true, "our": true, "you": true, "your": true,
	"it": true, "is": true, "are": true, "am": true, "was": true, "be": true, "but": true, "not": true,
	"don": true, "do": true, "would": true, "could": true, "can": true, "will": true, "if": true,
	"so": true, "just": true, "actually": true, "love": true, "like": true, "want": true,
	"interested": true, "please": true, "call": true, "send": true, "tell": true, "by": true,
	"ich": true, "wir": true, "sie": true, "ist": true, "aber": true, "nicht": true,
}

// signatureMaxLines bounds the signature an unsigned-off command may carry.
const signatureMaxLines = 8

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
// command, and what follows is at most a sign-off, a signature or a footer.
func isOptOutCommand(body string) bool {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	for i, raw := range lines {
		text := commandText(raw)
		switch {
		case optOutCommand.MatchString(text):
			return signatureOnly(lines[i+1:])
		case text == "" || commandGreeting.MatchString(text):
			continue
		default:
			return false
		}
	}
	return false
}

// signatureOnly reports whether the lines under the command are only a
// signature. A lowercase line straight below it continues the sentence
// ("Stop" / "by our booth"), so it is prose.
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
		if commandSignOff.MatchString(text) || commandFooter.MatchString(text) {
			return true
		}
		if first, _ := utf8.DecodeRuneInString(ln); n == 0 && unicode.IsLower(first) {
			return false
		}
		if n++; n > signatureMaxLines || !signatureLine(ln, text) {
			return false
		}
	}
	return true
}

// signatureLine is a short line with no question, exclamation or prose word.
func signatureLine(raw, text string) bool {
	if strings.ContainsAny(raw, "?!") {
		return false
	}
	words := strings.Fields(text)
	if len(words) > 6 {
		return false
	}
	for _, w := range words {
		if proseWords[w] {
			return false
		}
	}
	return true
}
