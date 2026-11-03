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
// read through commandText; only a name may follow them on the line.
var (
	commandGreetings = wordSet("hi", "hello", "hey", "dear", "hallo", "hola", "bonjour", "hej", "good morning", "good afternoon", "guten tag", "moin")
	commandSignOffs  = wordSet("thanks", "thank you", "thx", "ty", "many thanks", "cheers", "regards", "best", "best regards", "kind regards",
		"warm regards", "sincerely", "br", "kr", "mfg", "lg", "vg", "viele gruesse", "beste gruesse", "freundliche gruesse",
		"mit freundlichen gruessen", "gruss", "gruesse", "saludos", "cordialement", "bien a vous", "met vriendelijke groet",
		"groeten", "mvh", "hilsen", "med vanlig halsning")
)

// commandFooters are whole mobile-client footer lines, read through commandText.
var commandFooters = wordSet(
	"sent from my iphone", "sent from my ipad", "sent from my android", "sent from my android phone", "sent from my mobile",
	"sent from my mobile device", "sent from my phone", "sent from my samsung galaxy smartphone", "sent from my blackberry",
	"sent from mail for windows", "sent from mail for windows 10", "sent from outlook for ios", "sent from outlook for android",
	"get outlook for ios", "get outlook for android", "sent from yahoo mail for iphone", "sent from yahoo mail on android",
	"sent from gmail mobile", "von meinem iphone gesendet", "von meinem ipad gesendet", "von meinem smartphone gesendet",
	"enviado desde mi iphone", "enviado desde mi ipad", "envoye de mon iphone", "envoye de mon ipad",
	"verzonden vanaf mijn iphone", "verstuurd vanaf mijn iphone", "skickat fran min iphone", "sendt fra min iphone",
)

// proseWords are words no name carries, even capitalized.
var proseWords = wordSet("i", "me", "my", "we", "us", "our", "you", "your", "it", "is", "are", "am", "was", "be", "but", "not",
	"do", "would", "could", "can", "will", "if", "so", "just", "actually", "love", "like", "want", "let", "lets",
	"interested", "please", "call", "send", "tell", "talk", "great", "sounds", "meeting", "demo", "price", "pricing",
	"yes", "no", "sure", "ok", "okay", "maybe", "later", "today", "tomorrow", "monday", "tuesday", "wednesday", "thursday",
	"friday", "saturday", "sunday", "noon", "next", "week", "month", "year", "soon", "now", "asap", "time", "here", "there",
	"the", "a", "an", "at", "of", "and", "for", "to", "in", "on", "with",
	"ich", "wir", "sie", "ist", "aber", "nicht")

// commandPronouns is a pronoun note after a name: "(she/her)".
var commandPronouns = regexp.MustCompile(`(?i)^\((?:she|he|they|ze|xe)/(?:her|him|them|hers|his|theirs|zir|xem)(?:/[a-z]+)?\)$`)

// commandMaxLines bounds the lines a command may carry above a delimiter.
const commandMaxLines = 4

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
// command, and the lines below pass signatureOnly.
func isOptOutCommand(body string) bool {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	for i, raw := range lines {
		switch {
		case optOutCommand.MatchString(commandText(raw)):
			return signatureOnly(lines[i+1:])
		case strings.TrimSpace(raw) == "" || ledByName(raw, commandGreetings):
			continue
		default:
			return false
		}
	}
	return false
}

// signatureOnly is a closed whitelist of what may follow the command: blank
// lines, whole mobile footers, sign-off lines, name lines, and a "--"
// delimiter, below which nothing is read. Any other line means it is not an
// opt-out.
func signatureOnly(lines []string) bool {
	n := 0
	for _, raw := range lines {
		ln := strings.TrimSpace(raw)
		switch {
		case ln == "" || commandFooters[commandText(ln)]:
			continue
		case ln == "--":
			return true
		}
		if n++; n > commandMaxLines {
			return false
		}
		if !ledByName(ln, commandSignOffs) && !nameLine(strings.Fields(ln)) {
			return false
		}
	}
	return true
}

// ledByName reports a line that is one of the phrases, optionally followed by
// a name: "Thanks,", "Best regards, Jane", "Hi Jane,".
func ledByName(raw string, phrases map[string]bool) bool {
	tokens := strings.Fields(raw)
	for k := min(len(tokens), 4); k > 0; k-- {
		if phrases[commandText(strings.Join(tokens[:k], " "))] {
			return k == len(tokens) || nameLine(tokens[k:])
		}
	}
	return false
}

// nameLine is one to three capitalized name words or initials, optionally
// followed by a pronoun note; a trailing comma or period is allowed.
func nameLine(tokens []string) bool {
	if len(tokens) > 0 && commandPronouns.MatchString(strings.TrimRight(tokens[len(tokens)-1], ",.")) {
		tokens = tokens[:len(tokens)-1]
	}
	if len(tokens) == 0 || len(tokens) > 3 {
		return false
	}
	for i, t := range tokens {
		if i == len(tokens)-1 {
			t = strings.TrimRight(t, ",.")
		}
		if !nameWord(t) {
			return false
		}
	}
	return true
}

// nameWord is a capitalized letters-only word, or an initial such as "J.".
func nameWord(t string) bool {
	if word, ok := strings.CutSuffix(t, "."); ok && utf8.RuneCountInString(word) == 1 {
		t = word
	}
	first, _ := utf8.DecodeRuneInString(t)
	if t == "" || !unicode.IsUpper(first) || proseWords[strings.ToLower(t)] {
		return false
	}
	for _, r := range t {
		if !unicode.IsLetter(r) && !unicode.Is(unicode.Mn, r) {
			return false
		}
	}
	return true
}
