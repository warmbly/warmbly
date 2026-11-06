// Package warmlint is a small content-safety check shared by the live warmup
// send path and the offline AI generator. Warmup mail must look unremarkable;
// this rejects content that would raise the sending mailbox's own spam score.
package warmlint

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/warmbly/warmbly/internal/pkg/mailhtml"
)

var (
	stackedPunct = regexp.MustCompile(`[!?]{2,}`)
	wordToken    = regexp.MustCompile(`[a-z0-9%]+`)
	linkPattern  = regexp.MustCompile(`https?://[^\s"'<>)\]]*`)
	hrefPattern  = regexp.MustCompile(`(?i)href\s*=\s*["']?\s*(https?://[^\s"'<>]*)`)
	imgTag       = regexp.MustCompile(`(?i)<img\b[^>]*>`)
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
	if n := countTriggerTerms(withoutURLs(combined)); n >= 3 {
		return fmt.Errorf("content has %d spam-trigger terms", n)
	}
	return nil
}

// Field names the half of the template a finding sits in, so the editor can
// say "in the subject" rather than leaving the writer to search for the word.
const (
	FieldSubject = "subject"
	FieldBody    = "body"
)

// Span locates one exact fragment that triggered an issue. Without it a writer
// reads "3 spam-trigger term(s) found" and has to guess which words those are.
type Span struct {
	// Field is FieldSubject or FieldBody.
	Field string `json:"field"`
	// Text is the fragment as it is written in the copy.
	Text string `json:"text"`
	// Line is the 1-based line the fragment sits on within that field.
	Line int `json:"line,omitempty"`
	// Excerpt is that whole line, so the fragment can be shown in context.
	Excerpt string `json:"excerpt,omitempty"`
}

// Issue is a single advisory content problem found by Score.
type Issue struct {
	Severity string `json:"severity"` // "warn" | "high"
	Code     string `json:"code"`
	Message  string `json:"message"`
	// Field is FieldSubject or FieldBody when the issue lives in exactly one of
	// them, and empty when it spans both or describes the send as a whole.
	Field string `json:"field,omitempty"`
	// Spans are the exact fragments that triggered the issue, in reading order.
	// Empty for an issue with nothing to point at (an empty subject, an
	// attachment count).
	Spans []Span `json:"spans,omitempty"`
	// Suggestion is the concrete fix, in one line.
	Suggestion string `json:"suggestion,omitempty"`
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
//
// Every issue carries where it is (subject or body) and the exact fragments
// that caused it, because a score with no location is advice nobody can act on.
func Score(subject, bodyHTML, bodyPlain string) ScoreResult {
	res := ScoreResult{Score: 100, Issues: []Issue{}}
	// The field is read from the whole span list and the list is trimmed after,
	// so a cap that drops one half's spans can never relabel the issue.
	deduct := func(n int, issue Issue) {
		res.Score -= n
		issue.Spans = quotableSpans(issue.Spans)
		if issue.Field == "" {
			issue.Field = commonField(issue.Spans)
		}
		issue.Spans = capSpans(issue.Spans)
		res.Issues = append(res.Issues, issue)
	}

	subj := strings.TrimSpace(subject)
	body := bodyPlain
	if strings.TrimSpace(body) == "" {
		body = stripTags(bodyHTML)
	}

	if subj == "" {
		deduct(20, Issue{
			Severity: "high", Code: "empty_subject", Field: FieldSubject,
			Message:    "Subject is empty.",
			Suggestion: "Write a short, specific subject: lowercase, under six words, about them.",
		})
	} else if isAllCaps(subj) {
		deduct(15, Issue{
			Severity: "high", Code: "all_caps_subject", Field: FieldSubject,
			Message:    "Subject is all caps, a strong spam signal.",
			Spans:      []Span{{Field: FieldSubject, Text: subj, Line: 1, Excerpt: subj}},
			Suggestion: "Write the subject in ordinary sentence case.",
		})
	}
	if punct := punctuationSpans(subj, body); len(punct) > 0 {
		deduct(10, Issue{
			Severity: "warn", Code: "stacked_punctuation",
			Message:    "Stacked punctuation (e.g. !!! or ?!) reads as promotional.",
			Spans:      punct,
			Suggestion: "Use a single full stop or question mark.",
		})
	}
	terms, termSpans := triggerSpans(subj, body)
	if n := len(terms); n > 0 {
		d := n * 8
		if d > 40 {
			d = 40
		}
		severity := "warn"
		if n >= 3 {
			severity = "high"
		}
		deduct(d, Issue{
			Severity: severity, Code: "spam_trigger_terms",
			Message:    fmt.Sprintf("%d spam-trigger term(s): %s.", n, joinTerms(terms)),
			Spans:      termSpans,
			Suggestion: "Rewrite those words in plain language, or cut the sentence they sit in.",
		})
	}
	linked := linkSpans(subj, body, bodyHTML)
	if links := len(linked); links > 3 {
		d := (links - 3) * 5
		if d > 20 {
			d = 20
		}
		deduct(d, Issue{
			Severity: "warn", Code: "too_many_links",
			Message:    fmt.Sprintf("%d links. Keep the link count low in cold email.", links),
			Spans:      linked,
			Suggestion: "Keep one link at most on a first touch, and cut the rest.",
		})
	}
	if strings.TrimSpace(body) == "" {
		deduct(25, Issue{
			Severity: "high", Code: "empty_body", Field: FieldBody,
			Message:    "Body has no text content (image-only or empty body hurts deliverability).",
			Suggestion: "Write the message as text. Filters cannot read an image.",
		})
	} else if len(body) > 15000 {
		deduct(10, Issue{
			Severity: "warn", Code: "oversized_body", Field: FieldBody,
			Message:    "Body is very large; trim it for deliverability.",
			Suggestion: "Cut it to a few short paragraphs and one ask.",
		})
	}

	// Images: cold mail from a real person is usually plain. A wall of images,
	// or images carrying most of the message, reads as a marketing blast.
	if images := len(imgTag.FindAllString(bodyHTML, -1)); images > 0 {
		switch {
		case len(strings.TrimSpace(body)) < 200 && images >= 1:
			deduct(20, Issue{
				Severity: "high", Code: "image_heavy", Field: FieldBody,
				Message:    "Almost all of this email is images. Filters cannot read it and treat that as evasion.",
				Suggestion: "Put the message in text and keep images to a signature at most.",
			})
		case images > 3:
			d := (images - 3) * 5
			if d > 15 {
				d = 15
			}
			deduct(d, Issue{
				Severity: "warn", Code: "many_images", Field: FieldBody,
				Message:    fmt.Sprintf("%d images. Cold email from a person rarely has many.", images),
				Suggestion: "Drop all but the one image the message actually needs.",
			})
		}
	}

	if res.Score < 0 {
		res.Score = 0
	}
	return res
}

// LeadIssue renders the one issue worth naming in a one-line summary: the first
// high-severity one, else the first of any. It says where the problem is,
// because "3 spam-trigger terms" on its own sends the reader looking in the
// wrong box. Empty when there is nothing to report.
func LeadIssue(res ScoreResult) string {
	lead := Issue{}
	for _, issue := range res.Issues {
		if issue.Severity == "high" {
			lead = issue
			break
		}
		if lead.Code == "" {
			lead = issue
		}
	}
	switch {
	case lead.Field == FieldSubject:
		return "Subject: " + lead.Message
	case lead.Field == FieldBody:
		return "Body: " + lead.Message
	case len(lead.Spans) > 0:
		// No single field but fragments to point at means it is in both.
		return "Subject and body: " + lead.Message
	default:
		return lead.Message
	}
}

// ScoreWithAttachments is Score plus the attachment heuristic, which needs
// context the template alone does not carry.
func ScoreWithAttachments(subject, bodyHTML, bodyPlain string, attachments int) ScoreResult {
	res := Score(subject, bodyHTML, bodyPlain)
	if attachments > 0 {
		// A first-contact cold email with an attachment is both a spam signal
		// and a security prompt for the recipient.
		res.Score -= 15
		if res.Score < 0 {
			res.Score = 0
		}
		res.Issues = append(res.Issues, Issue{
			Severity:   "warn",
			Code:       "has_attachments",
			Message:    fmt.Sprintf("%d attachment(s) on a cold email. Link to the file instead.", attachments),
			Suggestion: "Remove the file and offer to send it once they reply.",
		})
	}
	return res
}

// stripTags reduces an HTML body to the words a reader sees, for scoring.
//
// It renders rather than strips tags: a regex left a <style> block's CSS
// behind as body text, so a class named .free-trial-banner cost a designed
// email eight points for a spam-trigger term nobody would ever read.
// withoutURLs removes link destinations before the words are scored. The text
// renderer keeps a link's target so the plain-text part stays usable, which
// put URL slugs in front of the trigger-term list: a CTA pointing at
// /free-trial cost eight points for a word no reader ever sees.
func withoutURLs(s string) string {
	return linkPattern.ReplaceAllString(s, " ")
}

func stripTags(s string) string {
	return strings.TrimSpace(mailhtml.ToPlainText(s))
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

// trimURL drops the sentence punctuation a URL picks up in prose, so the same
// link matches whether it was written inline or as an anchor's destination.
func trimURL(u string) string {
	return strings.TrimRight(u, ".,;:!?)]}\"'")
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
