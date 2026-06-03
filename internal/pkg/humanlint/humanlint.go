// Package humanlint makes generated warmup copy read like a real person wrote
// it, using the actual signals AI-text detectors and linguists key on rather
// than just telling the model to "sound human".
//
// It does two things, both with NO ML model:
//
//   - Humanize(text, seed): conservative, meaning-preserving transforms that
//     remove the strongest AI tells — em-dashes, stock openers ("I hope this
//     email finds you well"), AI-accent vocabulary (delve/leverage/robust...),
//     the "not only X but also Y" template, curly quotes, exclamation spam — and
//     applies contractions PROBABILISTICALLY (~0.7, seeded) so the result isn't
//     itself a uniform fingerprint.
//   - Score(text): an advisory AI-likeness score (0 clean .. 100 robotic) from
//     tell density + burstiness (sentence-length variation, the most-cited
//     non-ML human/AI separator). LooksRobotic gates content offline.
//
// Hard safety constraints for warmup mail (see the research): every transform
// preserves meaning and NEVER adds a spam signal — no fake typos/leetspeak, no
// urgency/marketing, no links, exclamations capped. Humanized output must still
// pass the existing warmlint spam gate; the score is advisory ("remove obvious
// tells + read naturally"), never a "guaranteed undetectable" claim, and never
// overrides warmlint.
package humanlint

import (
	"math/rand"
	"regexp"
	"strings"
)

// contractionProbability is how often an eligible formal phrase is contracted.
// NOT 1.0 on purpose: contracting 100% is its own uniform fingerprint; humans
// leave some expanded.
const contractionProbability = 0.72

// aiTellWords are single-word "AI accent" terms. Their presence raises the
// robotic score; a subset with a safe plain equivalent is also swapped by
// Humanize (see wordSwaps). The rest are penalized/regenerated, not mangled.
var aiTellWords = map[string]struct{}{
	"delve": {}, "leverage": {}, "utilize": {}, "utilise": {}, "harness": {},
	"streamline": {}, "underscore": {}, "foster": {}, "fostering": {},
	"navigating": {}, "showcase": {}, "showcasing": {}, "emphasize": {},
	"enhance": {}, "unlock": {}, "unveil": {}, "embark": {}, "elevate": {},
	"amplify": {}, "galvanize": {}, "illuminate": {}, "resonate": {},
	"boasts": {}, "garner": {}, "facilitate": {}, "robust": {}, "seamless": {},
	"pivotal": {}, "multifaceted": {}, "comprehensive": {}, "intricate": {},
	"intricacies": {}, "meticulous": {}, "nuanced": {}, "bespoke": {},
	"cutting-edge": {}, "transformative": {}, "groundbreaking": {},
	"compelling": {}, "holistic": {}, "proactive": {}, "iterative": {},
	"palpable": {}, "bolstered": {}, "tapestry": {}, "realm": {},
	"testament": {}, "synergy": {}, "paradigm": {}, "ecosystem": {},
	"beacon": {}, "bastion": {}, "camaraderie": {}, "interplay": {},
	"furthermore": {}, "moreover": {}, "additionally": {}, "consequently": {},
	"notably": {}, "amidst": {},
}

// wordSwaps maps an AI-accent word to a safe, plainer, meaning-preserving
// equivalent for casual email. Only high-confidence 1:1 swaps live here; words
// without a clean swap stay in aiTellWords (scored, not transformed).
var wordSwaps = map[string]string{
	"leverage":      "use",
	"utilize":       "use",
	"utilise":       "use",
	"harness":       "use",
	"streamline":    "simplify",
	"underscore":    "stress",
	"showcase":      "show",
	"showcasing":    "showing",
	"foster":        "build",
	"facilitate":    "help",
	"garner":        "get",
	"enhance":       "improve",
	"robust":        "solid",
	"seamless":      "smooth",
	"pivotal":       "key",
	"comprehensive": "full",
	"delve":         "dig",
	"furthermore":   "also",
	"moreover":      "also",
	"additionally":  "also",
	"consequently":  "so",
}

// aiTellPhrases are multi-word stock LLM/corporate constructions. Scored; the
// fillerStrips subset is also removed/rewritten by Humanize.
var aiTellPhrases = []string{
	"i hope this email finds you well", "i hope this message finds you well",
	"i hope this note finds you well", "i wanted to reach out",
	"i just wanted to reach out", "i am reaching out to", "i just wanted to",
	"in today's fast-paced world", "in today's digital age",
	"in today's digital landscape", "in the realm of", "when it comes to",
	"it is worth noting that", "it's worth noting", "it is important to note",
	"it's important to note", "it bears mentioning", "that being said",
	"a wide range of", "a wide array of", "a testament to",
	"due to the fact that", "embark on a journey", "at the end of the day",
	"rest assured", "please be advised", "at your earliest convenience",
	"as previously mentioned", "circle back", "touch base", "in order to",
	"in conclusion", "to sum up", "in summary", "in essence",
	"serves as a", "stands as a", "plays a pivotal role", "not only", "but also",
}

// clicheOpeners are formulaic email first lines. If the text starts with one,
// Humanize drops that leading clause.
var clicheOpeners = []string{
	"i hope this email finds you well", "i hope this message finds you well",
	"i hope this note finds you well", "i hope you're doing well",
	"i hope you are doing well", "i hope all is well", "i wanted to reach out",
	"i just wanted to reach out", "i am reaching out", "i'm reaching out",
	"i just wanted to touch base", "i wanted to touch base",
	"dear sir or madam", "to whom it may concern", "i hope this finds you well",
}

// fillerStrips are phrases Humanize rewrites to plainer equivalents (empty = drop).
var fillerStrips = []struct{ from, to string }{
	{"due to the fact that", "because"},
	{"in order to", "to"},
	{"it is worth noting that ", ""},
	{"it's worth noting that ", ""},
	{"it is worth mentioning that ", ""},
	{"it's worth mentioning that ", ""},
	{"that being said, ", ""},
	{"at the end of the day, ", ""},
	{"to be honest, ", ""},
	{"as previously mentioned, ", ""},
}

// contractions are ordered so longer/negation forms apply before shorter ones.
var contractions = []struct{ from, to string }{
	{"do not", "don't"}, {"does not", "doesn't"}, {"did not", "didn't"},
	{"is not", "isn't"}, {"are not", "aren't"}, {"was not", "wasn't"},
	{"were not", "weren't"}, {"has not", "hasn't"}, {"have not", "haven't"},
	{"had not", "hadn't"}, {"will not", "won't"}, {"would not", "wouldn't"},
	{"should not", "shouldn't"}, {"could not", "couldn't"}, {"cannot", "can't"},
	{"can not", "can't"}, {"I will", "I'll"}, {"I am", "I'm"}, {"I have", "I've"},
	{"I would", "I'd"}, {"we are", "we're"}, {"we will", "we'll"},
	{"we have", "we've"}, {"you are", "you're"}, {"you will", "you'll"},
	{"you have", "you've"}, {"they are", "they're"}, {"they will", "they'll"},
	{"it is", "it's"}, {"that is", "that's"}, {"there is", "there's"},
	{"here is", "here's"}, {"let us", "let's"}, {"what is", "what's"},
	{"who is", "who's"},
}

var (
	emDash         = regexp.MustCompile(`\s*[\x{2014}\x{2013}]\s*`)
	spacedDash     = regexp.MustCompile(`\s+-{1,2}\s+`)
	notOnlyButAlso = regexp.MustCompile(`(?i)\bnot only\b(.+?)\bbut also\b`)
	multiSpace     = regexp.MustCompile(`[ \t]{2,}`)
	sentenceSplit  = regexp.MustCompile(`[.!?]+`)
	wordRe         = regexp.MustCompile(`[A-Za-z0-9'%-]+`)
	tricolon       = regexp.MustCompile(`(?i)\b([a-z]+), ([a-z]+),? and ([a-z]+)\b`)
)

// Humanize applies conservative, meaning-preserving transforms that strip the
// strongest AI tells and contract probabilistically. seed makes it
// deterministic per call (so the same content humanizes identically) while
// still varying across different content.
func Humanize(text string, seed int64) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	rng := rand.New(rand.NewSource(seed))

	out := normalizePunctuation(text)
	out = stripLeadingOpener(out)
	out = applyFillerStrips(out)
	out = applyWordSwaps(out)
	out = flattenNotOnlyButAlso(out)
	out = applyContractions(out, rng)
	out = capExclamations(out)
	out = multiSpace.ReplaceAllString(out, " ")
	return strings.TrimSpace(out)
}

// normalizePunctuation straightens curly quotes/ellipsis and turns em/spaced
// dashes into commas (a clause join) — the single most cited punctuation tell.
func normalizePunctuation(s string) string {
	r := strings.NewReplacer(
		"‘", "'", "’", "'", "“", "\"", "”", "\"",
		"…", "...",
	)
	s = r.Replace(s)
	s = emDash.ReplaceAllString(s, ", ")
	s = spacedDash.ReplaceAllString(s, ", ")
	return s
}

func stripLeadingOpener(s string) string {
	trimmed := strings.TrimLeft(s, " \t")
	lower := strings.ToLower(trimmed)
	for _, op := range clicheOpeners {
		if strings.HasPrefix(lower, op) {
			rest := strings.TrimLeft(trimmed[len(op):], " ,.!:;-")
			if rest == "" {
				return trimmed // opener was the whole thing — keep something
			}
			// Capitalize the new first letter.
			return upperFirst(rest)
		}
	}
	return s
}

func applyFillerStrips(s string) string {
	for _, f := range fillerStrips {
		s = replaceFold(s, f.from, f.to)
	}
	return s
}

func applyWordSwaps(s string) string {
	return wordRe.ReplaceAllStringFunc(s, func(w string) string {
		repl, ok := wordSwaps[strings.ToLower(w)]
		if !ok {
			return w
		}
		if isUpperFirst(w) {
			return upperFirst(repl)
		}
		return repl
	})
}

func flattenNotOnlyButAlso(s string) string {
	return notOnlyButAlso.ReplaceAllString(s, "$1 and ")
}

func applyContractions(s string, rng *rand.Rand) string {
	for _, c := range contractions {
		re := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(c.from) + `\b`)
		s = re.ReplaceAllStringFunc(s, func(m string) string {
			if rng.Float64() > contractionProbability {
				return m // leave a fraction expanded
			}
			if isUpperFirst(m) {
				return upperFirst(c.to)
			}
			return c.to
		})
	}
	return s
}

// capExclamations keeps at most one '!' and never stacked punctuation.
func capExclamations(s string) string {
	s = strings.ReplaceAll(s, "!!", "!")
	s = strings.ReplaceAll(s, "?!", "?")
	if strings.Count(s, "!") <= 1 {
		return s
	}
	seen := false
	var b strings.Builder
	for _, r := range s {
		if r == '!' {
			if seen {
				b.WriteRune('.')
				continue
			}
			seen = true
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Issue is one robotic signal found by Score.
type Issue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Score returns an advisory AI-likeness score (0 = reads human, 100 = robotic)
// plus the signals found. It is advisory only — never a pass/fail "undetectable"
// gate, and it must never override the warmlint spam gate.
func Score(text string) (int, []Issue) {
	lower := strings.ToLower(text)
	score := 0
	var issues []Issue
	add := func(n int, code, msg string) {
		score += n
		issues = append(issues, Issue{Code: code, Message: msg})
	}

	for _, op := range clicheOpeners {
		if strings.HasPrefix(strings.TrimSpace(lower), op) {
			add(30, "stock_opener", "opens with a stock line: "+op)
			break
		}
	}

	tellWords := 0
	for _, w := range wordRe.FindAllString(lower, -1) {
		if _, ok := aiTellWords[w]; ok {
			tellWords++
		}
	}
	if tellWords > 0 {
		add(min(40, tellWords*12), "ai_vocabulary", "uses AI-accent vocabulary")
	}

	phraseHits := 0
	for _, p := range aiTellPhrases {
		if strings.Contains(lower, p) {
			phraseHits++
		}
	}
	if phraseHits > 0 {
		add(min(35, phraseHits*12), "stock_phrases", "uses stock LLM phrasing")
	}

	if strings.ContainsAny(text, "—–") {
		add(15, "em_dash", "contains an em-dash")
	}
	if notOnlyButAlso.MatchString(text) {
		add(20, "not_only_but_also", "uses the 'not only X but also Y' template")
	}
	if tricolon.MatchString(text) {
		add(8, "tricolon", "uses a rule-of-three list")
	}

	// Formal (uncontracted) forms present where a human would contract.
	formal := 0
	for _, c := range contractions {
		if regexpFoldContains(lower, c.from) {
			formal++
		}
	}
	if formal >= 2 {
		add(min(20, formal*5), "no_contractions", "formal/uncontracted phrasing")
	}

	// Burstiness: low sentence-length variation reads robotic. Only meaningful
	// for multi-sentence text; single-line messages skip it (CV undefined).
	if cv, ok := sentenceLengthCV(text); ok && cv < 0.5 {
		add(18, "low_burstiness", "uniform sentence length (low burstiness)")
	}

	if score > 100 {
		score = 100
	}
	return score, issues
}

// LooksRobotic reports whether text still reads machine-generated after
// humanization (used as an offline content-quality gate, not a hard spam gate).
func LooksRobotic(text string) bool {
	s, _ := Score(text)
	return s >= 45
}

// sentenceLengthCV returns the coefficient of variation of per-sentence word
// counts and whether it's defined (needs >=2 sentences with words).
func sentenceLengthCV(text string) (float64, bool) {
	parts := sentenceSplit.Split(text, -1)
	var lens []float64
	for _, p := range parts {
		n := len(wordRe.FindAllString(p, -1))
		if n > 0 {
			lens = append(lens, float64(n))
		}
	}
	if len(lens) < 2 {
		return 0, false
	}
	var sum float64
	for _, l := range lens {
		sum += l
	}
	mean := sum / float64(len(lens))
	if mean == 0 {
		return 0, false
	}
	var variance float64
	for _, l := range lens {
		d := l - mean
		variance += d * d
	}
	variance /= float64(len(lens))
	return sqrt(variance) / mean, true
}

// ---- small helpers (kept dependency-free) ----

func replaceFold(s, from, to string) string {
	if from == "" {
		return s
	}
	re := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(from))
	return re.ReplaceAllStringFunc(s, func(m string) string {
		if to != "" && isUpperFirst(m) {
			return upperFirst(to)
		}
		return to
	})
}

func regexpFoldContains(lowerText, phrase string) bool {
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(strings.ToLower(phrase)) + `\b`)
	return re.MatchString(lowerText)
}

func isUpperFirst(s string) bool {
	if s == "" {
		return false
	}
	r := rune(s[0])
	return r >= 'A' && r <= 'Z'
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	r := rune(s[0])
	if r >= 'a' && r <= 'z' {
		return string(r-32) + s[1:]
	}
	return s
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func sqrt(x float64) float64 {
	if x <= 0 {
		return 0
	}
	// Newton's method — avoids importing math for one call.
	z := x
	for i := 0; i < 24; i++ {
		z -= (z*z - x) / (2 * z)
	}
	return z
}
