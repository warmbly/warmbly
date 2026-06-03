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
	linkPattern  = regexp.MustCompile(`https?://`)
	htmlTag      = regexp.MustCompile(`(?i)<[a-z!/][^>]*>`)
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

// Issue is a single advisory content problem found by Score.
type Issue struct {
	Severity string `json:"severity"` // "warn" | "high"
	Code     string `json:"code"`
	Message  string `json:"message"`
}

// ScoreResult is an advisory content assessment for a campaign template.
type ScoreResult struct {
	Score  int     `json:"score"` // 0-100, higher = safer
	Issues []Issue `json:"issues"`
}

// Score gives an ADVISORY 0-100 content-safety score (higher = safer) for a
// campaign template, plus the issues found. Unlike Check — a hard gate for
// warmup mail — Score never blocks: it surfaces guidance before the user sends
// the mail that actually reaches prospects and drives complaints. It reuses the
// same trigger-term and ALL-CAPS heuristics as the warmup lint.
func Score(subject, bodyHTML, bodyPlain string) ScoreResult {
	res := ScoreResult{Score: 100, Issues: []Issue{}}
	deduct := func(n int, severity, code, msg string) {
		res.Score -= n
		res.Issues = append(res.Issues, Issue{Severity: severity, Code: code, Message: msg})
	}

	subj := strings.TrimSpace(subject)
	body := bodyPlain
	if strings.TrimSpace(body) == "" {
		body = stripTags(bodyHTML)
	}
	combined := subj + "\n" + body

	if subj == "" {
		deduct(20, "high", "empty_subject", "Subject is empty.")
	} else if isAllCaps(subj) {
		deduct(15, "high", "all_caps_subject", "Subject is all caps — a strong spam signal.")
	}
	if stackedPunct.MatchString(combined) {
		deduct(10, "warn", "stacked_punctuation", "Stacked punctuation (e.g. !!! or ?!) reads as promotional.")
	}
	if n := countTriggerTerms(combined); n > 0 {
		d := n * 8
		if d > 40 {
			d = 40
		}
		severity := "warn"
		if n >= 3 {
			severity = "high"
		}
		deduct(d, severity, "spam_trigger_terms", fmt.Sprintf("%d spam-trigger term(s) found in subject/body.", n))
	}
	if links := len(linkPattern.FindAllString(combined, -1)); links > 3 {
		d := (links - 3) * 5
		if d > 20 {
			d = 20
		}
		deduct(d, "warn", "too_many_links", fmt.Sprintf("%d links — keep cold-email link count low.", links))
	}
	if strings.TrimSpace(body) == "" {
		deduct(25, "high", "empty_body", "Body has no text content (image-only or empty body hurts deliverability).")
	} else if len(body) > 15000 {
		deduct(10, "warn", "oversized_body", "Body is very large; trim it for deliverability.")
	}

	if res.Score < 0 {
		res.Score = 0
	}
	return res
}

func stripTags(s string) string {
	return strings.TrimSpace(htmlTag.ReplaceAllString(s, " "))
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
