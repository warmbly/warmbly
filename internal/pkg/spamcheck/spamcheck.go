// Package spamcheck is the AI half of the campaign content check. warmlint
// scores the rules everyone agrees on (trigger terms, ALL-CAPS, link and image
// counts); this reads the copy with the deployment's configured LLM and says
// which sentence a filter will object to, in the subject or in the body, and
// what to write instead.
//
// Two things make its output usable rather than decorative. Every finding must
// quote the copy verbatim, and a quote that is not actually in the template is
// dropped rather than shown, so the panel can never point at a sentence the
// writer did not write. And the run is deterministic: re-checking unchanged
// copy has to return the same score, or "did my edit help?" cannot be answered.
package spamcheck

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/warmbly/warmbly/internal/pkg/casefold"
	"github.com/warmbly/warmbly/internal/pkg/generation"
	"github.com/warmbly/warmbly/internal/pkg/mailhtml"
	"github.com/warmbly/warmbly/internal/pkg/warmlint"
)

// Bounds on what goes to the provider and what comes back, so one pasted
// newsletter cannot drive a very large completion or a very large response.
const (
	maxBodyRunes    = 8000
	maxSubjectRunes = 400
	maxFindings     = 12
	maxImprovements = 6
	maxTextRunes    = 200
	excerptMax      = 180
	completionMax   = 1600
)

// Input is one template to analyze.
type Input struct {
	Subject   string
	BodyHTML  string
	BodyPlain string
	// Rules is the heuristic pass. It is given to the model as grounding so the
	// two halves of the panel never contradict each other.
	Rules warmlint.ScoreResult
	// Voice is the workspace's product and tone context, which changes what
	// counts as a plausible claim rather than a spam-flavoured one.
	Voice generation.VoiceContext
}

// Finding is one located problem with the copy.
type Finding struct {
	// Severity is "high", "warn" or "info".
	Severity string `json:"severity"`
	// Field is "subject" or "body", empty when the model labelled neither and
	// nothing in the finding could be anchored in the copy.
	Field string `json:"field,omitempty"`
	// Text is the exact fragment quoted from the copy, empty when the finding
	// is about the email as a whole.
	Text string `json:"text,omitempty"`
	// Line is the 1-based line of that field the fragment sits on.
	Line int `json:"line,omitempty"`
	// Excerpt is the whole line, for context around the fragment.
	Excerpt string `json:"excerpt,omitempty"`
	// Issue is why it hurts deliverability.
	Issue string `json:"issue"`
	// Suggestion is what to write instead.
	Suggestion string `json:"suggestion,omitempty"`
	// Category is a coarse grouping ("trigger_word", "tone", "formatting",
	// "links", "structure", "authenticity").
	Category string `json:"category,omitempty"`
}

// Result is the analysis returned to the editor.
type Result struct {
	// Score is the overall 0-100 deliverability score (higher is safer),
	// grounded on the rules pass rather than argued against it.
	Score   int    `json:"score"`
	Verdict string `json:"verdict"`
	// Findings are located problems, most severe first.
	Findings []Finding `json:"findings"`
	// SuggestedSubject is a rewritten subject line, empty when the current one
	// is fine.
	SuggestedSubject string `json:"suggested_subject,omitempty"`
	// Improvements are copy-level suggestions with nothing specific to quote.
	Improvements []string `json:"improvements,omitempty"`
	Model        string   `json:"model"`
	TokensUsed   int      `json:"tokens_used"`
	InputTokens  int      `json:"-"`
	OutputTokens int      `json:"-"`
}

const systemPrompt = `You are a cold-email deliverability analyst. You are given one campaign email template and you report exactly what in it would push the message to spam, where that thing is, and what to write instead.

OUTPUT
Return ONLY a JSON object, no prose around it, no markdown fence:
{
  "score": 0-100,
  "verdict": "one plain sentence on how this email will land",
  "findings": [
    {
      "severity": "high" | "warn" | "info",
      "field": "subject" | "body",
      "text": "the exact words from the template, copied character for character",
      "issue": "why this specific fragment hurts deliverability or replies",
      "suggestion": "what to write instead, concretely",
      "category": "trigger_word" | "tone" | "formatting" | "links" | "structure" | "authenticity"
    }
  ],
  "suggested_subject": "a better subject line, or \"\" when the current one is fine",
  "improvements": ["copy-level advice with no single fragment to quote"]
}

RULES
- "text" MUST be copied verbatim from the subject or body you were given. Never paraphrase it, never fix its spelling, never quote a sentence that is not there. A fragment you cannot copy exactly belongs in "improvements" instead.
- Point at the smallest thing that is wrong: the word, the phrase, or the one sentence. Never quote a whole paragraph.
- "field" must say which half the fragment is in. Getting this wrong sends the writer to the wrong box.
- score is 0-100 where 100 is safest. Take the rule-based findings you are given as already proven and score with them, never against them. A clean, plain, personal email with one ask scores above 85; an email with several trigger phrases, hype and multiple links scores below 40.
- Judge cold-email deliverability and reply odds, not marketing polish. Plain, short and specific is good. Formatting, hype, urgency, unearned claims, link stacking and mail-merge that reads like a mail merge are bad.
- At most 8 findings. Do not invent problems to fill the list: an email with nothing wrong returns an empty findings array and a high score.
- Write every sentence in plain English, no em dashes, no jargon.`

// Analyze runs the model over the template and returns a verified report.
func Analyze(ctx context.Context, p generation.Provider, model string, in Input) (*Result, error) {
	if p == nil {
		return nil, generation.ErrProviderNotConfigured
	}
	subject := truncate(strings.TrimSpace(in.Subject), maxSubjectRunes)
	body := truncate(bodyText(in), maxBodyRunes)

	res, err := p.Complete(ctx, generation.CompletionRequest{
		System:    systemPrompt,
		Prompt:    buildPrompt(subject, body, in),
		Model:     model,
		MaxTokens: completionMax,
		// Deterministic: the whole point of Re-check is comparing this score
		// with the last one, and a sampled score moves on unchanged copy.
		Temperature: generation.Deterministic(),
	})
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, fmt.Errorf("spamcheck: the model returned nothing")
	}

	out := parse(res.Text)
	// A response with no score and no findings is not an analysis, whatever
	// else it carried. Failing here refunds the credit; deriving a score from
	// an empty finding list would render as a confident 100/100 sitting above a
	// verdict that says the opposite.
	if out.Score < 0 && len(out.Findings) == 0 {
		return nil, fmt.Errorf("spamcheck: could not read the model's response")
	}
	out.Model = res.Model
	out.TokensUsed = res.TokensUsed
	out.InputTokens, out.OutputTokens = res.InputTokens, res.OutputTokens
	verify(out, subject, body)
	return out, nil
}

// bodyText is the copy a recipient reads: the plain part when the editor stored
// one, otherwise the HTML rendered down to its words.
func bodyText(in Input) string {
	if b := strings.TrimSpace(in.BodyPlain); b != "" {
		return b
	}
	return strings.TrimSpace(mailhtml.ToPlainText(in.BodyHTML))
}

func buildPrompt(subject, body string, in Input) string {
	var b strings.Builder
	b.WriteString("SUBJECT:\n")
	if subject == "" {
		b.WriteString("(empty)\n")
	} else {
		b.WriteString(subject + "\n")
	}
	b.WriteString("\nBODY:\n")
	if body == "" {
		b.WriteString("(empty)\n")
	} else {
		b.WriteString(body + "\n")
	}

	// The body may be HTML the reader never sees as markup, but its shape is
	// part of how it lands, so the counts travel even though the tags do not.
	if strings.TrimSpace(in.BodyHTML) != "" {
		b.WriteString("\nThis body is HTML. Judge the words above, not the markup.\n")
	}

	b.WriteString("\nRULE-BASED CHECK (already proven, score with it):\n")
	fmt.Fprintf(&b, "score %d/100\n", in.Rules.Score)
	if len(in.Rules.Issues) == 0 {
		b.WriteString("no rule-based issues\n")
	}
	for _, issue := range in.Rules.Issues {
		where := issue.Field
		if where == "" {
			where = "template"
		}
		fmt.Fprintf(&b, "- [%s] %s (%s)", where, issue.Message, issue.Code)
		if len(issue.Spans) > 0 {
			quoted := make([]string, 0, len(issue.Spans))
			for _, s := range issue.Spans {
				if s.Text != "" {
					quoted = append(quoted, quoteFragment(s.Text))
				}
			}
			if len(quoted) > 0 {
				fmt.Fprintf(&b, ": %s", strings.Join(quoted, ", "))
			}
		}
		b.WriteString("\n")
	}

	if ctx := voiceContext(in.Voice); ctx != "" {
		b.WriteString("\nWHAT THIS WORKSPACE SELLS (context, not something to grade):\n")
		b.WriteString(ctx)
	}
	b.WriteString("\nAnalyze this template now. JSON only.")
	return b.String()
}

// voiceContext renders the workspace's own description, so a specific claim is
// read as a real one rather than as hype.
func voiceContext(v generation.VoiceContext) string {
	var b strings.Builder
	for _, part := range []struct{ label, value string }{
		{"Product", v.ProductDescription},
		{"Who they sell to", v.ICPNotes},
		{"Voice", v.VoiceProfile},
	} {
		if s := strings.TrimSpace(part.value); s != "" {
			fmt.Fprintf(&b, "%s: %s\n", part.label, truncate(s, 600))
		}
	}
	return b.String()
}

// quoteFragment wraps a fragment in quotes for the prompt, keeping an embedded
// quote from ending the quoted run.
func quoteFragment(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `'`) + `"`
}

// parse reads the model's JSON, tolerating a ```json fence or a stray sentence
// either side of the object. A response nothing can be read out of comes back
// with no score and no findings, which Analyze refuses so the credit is
// refunded rather than spent on a report with nothing in it.
func parse(text string) *Result {
	text = strings.TrimSpace(text)
	if i := strings.Index(text, "{"); i >= 0 {
		if j := strings.LastIndex(text, "}"); j >= i {
			text = text[i : j+1]
		}
	}
	var raw struct {
		Score            *int     `json:"score"`
		Verdict          string   `json:"verdict"`
		SuggestedSubject string   `json:"suggested_subject"`
		Improvements     []string `json:"improvements"`
		Findings         []struct {
			Severity   string `json:"severity"`
			Field      string `json:"field"`
			Text       string `json:"text"`
			Issue      string `json:"issue"`
			Suggestion string `json:"suggestion"`
			Category   string `json:"category"`
		} `json:"findings"`
	}
	out := &Result{Score: -1, Findings: []Finding{}}
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return out
	}
	if raw.Score != nil {
		out.Score = clamp(*raw.Score)
	}
	out.Verdict = truncate(strings.TrimSpace(raw.Verdict), 300)
	out.SuggestedSubject = truncate(strings.TrimSpace(raw.SuggestedSubject), maxSubjectRunes)
	for _, s := range raw.Improvements {
		if s = truncate(strings.TrimSpace(s), 300); s != "" {
			out.Improvements = append(out.Improvements, s)
		}
		if len(out.Improvements) == maxImprovements {
			break
		}
	}
	for _, f := range raw.Findings {
		issue := truncate(strings.TrimSpace(f.Issue), 300)
		if issue == "" {
			continue
		}
		out.Findings = append(out.Findings, Finding{
			Severity: severity(f.Severity),
			Field:    field(f.Field),
			// Cut, not truncated: verify looks this up in the copy, and an
			// appended ellipsis is never in there, so every long quote would
			// fail verification and lose the fragment it named.
			Text:       cut(strings.TrimSpace(f.Text), maxTextRunes),
			Issue:      issue,
			Suggestion: truncate(strings.TrimSpace(f.Suggestion), 300),
			Category:   category(f.Category),
		})
		if len(out.Findings) == maxFindings {
			break
		}
	}
	return out
}

// verify anchors every finding in the copy the user actually wrote. A quote the
// model got right is given its real line and surrounding line; one it invented
// loses the quote and stays as general advice, because a panel that points at a
// sentence nobody wrote is worse than one that points at nothing.
func verify(res *Result, subject, body string) {
	kept := res.Findings[:0]
	seen := map[string]struct{}{}
	for _, f := range res.Findings {
		if f.Text != "" {
			if m, ok := locate(subject, body, f.Text, f.Field); ok {
				// The copy's own casing, not the model's retyping of it. The
				// fragment is documented as quoted character for character and
				// the editor highlights it, so "free bonus" standing in for
				// "FREE BONUS" both breaks the contract and fails to match.
				f.Text, f.Field, f.Line, f.Excerpt = m.text, m.field, m.line, m.excerpt
			} else {
				f.Text, f.Line, f.Excerpt = "", 0, ""
			}
		}
		key := f.Field + "\x00" + strings.ToLower(f.Text) + "\x00" + strings.ToLower(f.Issue)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		kept = append(kept, f)
	}
	res.Findings = kept

	// Severity order, stable within a band, so the worst thing is read first
	// and equal findings keep the model's own reading order through the email.
	rank := map[string]int{"high": 0, "warn": 1, "info": 2}
	sort.SliceStable(res.Findings, func(i, j int) bool {
		return rank[res.Findings[i].Severity] < rank[res.Findings[j].Severity]
	})

	// A model that answered with findings but no score would render as 0/100.
	if res.Score < 0 {
		res.Score = scoreFromFindings(res.Findings)
	}
}

// match is where a fragment was found and how it is actually written there.
type match struct {
	field   string
	text    string
	line    int
	excerpt string
}

// locate finds the fragment in the copy, preferring the field the model named
// and falling back to the other one (the analysis is still right, the label was
// wrong). Matching is case-insensitive because a model retyping a quote tends to
// normalize its capitalization, and what comes back is the copy's own wording:
// the offsets are mapped out of the fold before anything is sliced, so a rune
// that changes byte length when lowercased cannot shift the quote.
func locate(subject, body, text, preferred string) (match, bool) {
	order := []struct{ name, text string }{
		{warmlint.FieldSubject, subject},
		{warmlint.FieldBody, body},
	}
	if preferred == warmlint.FieldBody {
		order[0], order[1] = order[1], order[0]
	}
	needle := strings.ToLower(strings.TrimSpace(text))
	if needle == "" {
		return match{}, false
	}
	for _, o := range order {
		lower, offsets := casefold.Index(o.text)
		i := strings.Index(lower, needle)
		if i < 0 {
			continue
		}
		n := 1 + strings.Count(lower[:i], "\n")
		found := match{field: o.name, text: strings.TrimSpace(text), line: n, excerpt: lineText(o.text, n)}
		start, end := casefold.Origin(offsets, i), casefold.Origin(offsets, i+len(needle))
		if start >= 0 && end <= len(o.text) && start < end {
			found.text = o.text[start:end]
		}
		return found, true
	}
	return match{}, false
}

// lineText returns one line of a field, trimmed and capped for display.
func lineText(s string, line int) string {
	lines := strings.Split(s, "\n")
	if line < 1 || line > len(lines) {
		return ""
	}
	return truncate(strings.TrimSpace(lines[line-1]), excerptMax)
}

// scoreFromFindings is the fallback when the model returned findings but no
// usable score. Analyze refuses a response with neither, so this is never asked
// to price an empty list into a clean 100.
func scoreFromFindings(findings []Finding) int {
	score := 100
	for _, f := range findings {
		switch f.Severity {
		case "high":
			score -= 20
		case "warn":
			score -= 10
		default:
			score -= 3
		}
	}
	return clamp(score)
}

func severity(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "high", "critical", "severe", "error":
		return "high"
	case "info", "low", "minor", "note":
		return "info"
	default:
		return "warn"
	}
}

// field reads the half the model named. Anything it did not clearly label comes
// back empty rather than guessed: verify fills it in from the quote when there
// is one, and a badge naming the wrong box on a finding nothing anchored is the
// exact error this whole panel exists to avoid.
func field(s string) string {
	switch v := strings.ToLower(strings.TrimSpace(s)); {
	case strings.Contains(v, "subject"):
		return warmlint.FieldSubject
	case strings.Contains(v, "body"):
		return warmlint.FieldBody
	default:
		return ""
	}
}

// categories is the closed set a finding may be grouped under. The field is
// documented as an enum, so anything else is dropped rather than passed
// through: a client validating the response against that set should never be
// handed something outside it.
var categories = map[string]struct{}{
	"trigger_word": {}, "tone": {}, "formatting": {},
	"links": {}, "structure": {}, "authenticity": {},
}

func category(s string) string {
	v := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(s)), " ", "_")
	if _, ok := categories[v]; ok {
		return v
	}
	return ""
}

func clamp(n int) int {
	if n < 0 {
		return 0
	}
	if n > 100 {
		return 100
	}
	return n
}

// truncate bounds prose for display, marking that it was shortened.
func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return strings.TrimSpace(string(r[:max])) + "…"
}

// cut bounds a fragment that still has to be found in the copy, so it adds
// nothing: a prefix of a substring is still a substring.
func cut(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return strings.TrimSpace(string(r[:max]))
}
