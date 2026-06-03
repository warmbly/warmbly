// Package warmlint is a small content-safety check shared by the live warmup
// send path and the offline AI generator. Warmup mail must look unremarkable;
// this rejects content that would raise the sending mailbox's own spam score.
package warmlint

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

var (
	stackedPunct = regexp.MustCompile(`[!?]{2,}`)
	wordToken    = regexp.MustCompile(`[a-z0-9%]+`)
)

// triggerWords are single-token terms that raise SpamAssassin-style content
// scores. Warmup content should read like a normal personal email, so any
// accumulation of these is a red flag (usually an LLM drifting into ad tone).
var triggerWords = map[string]struct{}{
	"free": {}, "guarantee": {}, "guaranteed": {}, "winner": {}, "congratulations": {},
	"cash": {}, "prize": {}, "cheap": {}, "discount": {}, "viagra": {}, "casino": {},
	"loan": {}, "credit": {}, "bitcoin": {}, "crypto": {}, "urgent": {}, "bonus": {},
	"promo": {}, "refinance": {}, "mortgage": {}, "investment": {}, "deal": {},
	"100%": {}, "sale": {}, "income": {}, "earnings": {}, "clearance": {},
}

var triggerPhrases = []string{
	"act now", "click here", "risk free", "risk-free", "limited time", "buy now",
	"earn money", "make money", "dear friend", "order now", "100% free",
	"double your", "extra income", "work from home", "this is not spam",
	"cash bonus", "no cost", "for free", "money back", "satisfaction guaranteed",
}

// Check rejects warmup content that would look spammy:
//   - a fabricated Re:/Fwd: prefix on a NEW (non-reply) message;
//   - an ALL-CAPS subject;
//   - stacked punctuation (!!!, ?!);
//   - three or more distinct spam-trigger terms.
func Check(subject, body string, isReply bool) error {
	subj := strings.TrimSpace(subject)
	lowerSubj := strings.ToLower(subj)

	if !isReply && (strings.HasPrefix(lowerSubj, "re:") ||
		strings.HasPrefix(lowerSubj, "fwd:") ||
		strings.HasPrefix(lowerSubj, "fw:")) {
		return fmt.Errorf("fabricated reply/forward prefix on a new send")
	}
	if isAllCaps(subj) {
		return fmt.Errorf("subject is all caps")
	}

	combined := subject + "\n" + body
	if stackedPunct.MatchString(combined) {
		return fmt.Errorf("stacked punctuation")
	}
	if n := countTriggerTerms(combined); n >= 3 {
		return fmt.Errorf("content has %d spam-trigger terms", n)
	}
	return nil
}

func isAllCaps(s string) bool {
	letters := 0
	for _, r := range s {
		if unicode.IsLower(r) {
			return false
		}
		if unicode.IsLetter(r) {
			letters++
		}
	}
	return letters >= 4
}

func countTriggerTerms(text string) int {
	lower := strings.ToLower(text)
	found := map[string]struct{}{}
	for _, w := range wordToken.FindAllString(lower, -1) {
		if _, ok := triggerWords[w]; ok {
			found[w] = struct{}{}
		}
	}
	for _, p := range triggerPhrases {
		if strings.Contains(lower, p) {
			found[p] = struct{}{}
		}
	}
	return len(found)
}
