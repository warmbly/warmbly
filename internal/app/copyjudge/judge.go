// Package copyjudge asks TypeSafe how a campaign email reads to its recipient
// and turns the answers into numbers the Advisor and the editor threshold on.
// Every question and threshold lives in this file; the model answers, code
// decides.
package copyjudge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/warmbly/warmbly/internal/pkg/mailhtml"
	"github.com/warmbly/warmbly/internal/pkg/typesafe"
)

// Thresholds. Tuning these is tuning the product.
const (
	// BulkAt is the normalized reads_as position from which copy is treated
	// as bulk mail. The rubric has three levels, so 0.75 means the model put
	// it past the midpoint of "somewhere between" and "bulk".
	BulkAt = 0.75

	// SpamClaimAt is the noul level at which a filter-baiting claim counts as
	// present. It is read on its own, without the confidence floor: a noul's
	// value is its probability, and there is no second number to floor on.
	SpamClaimAt = 0.70

	// ConfFloor is the confidence below which a score or choice is not acted
	// on. A low-confidence answer is not merely uncertain, it is not
	// reproducible, and a finding that comes and goes between evaluations
	// teaches people to ignore the Advisor.
	ConfFloor = 0.70
)

// BodyLimit caps the plain-text body sent as state, in runes. Accuracy falls as
// state grows with content the questions do not need, and a cold email past
// this is already something the length detector flags.
const BodyLimit = 4000

// Ask outcomes. The choice's identifiers are a contract: stored verdicts and
// the editor read them by name.
const (
	AskOneClear = "one_clear_ask"
	AskSeveral  = "several_asks"
	AskNone     = "no_ask"
)

// Question identifiers, one per answer in the response.
const (
	qReadsAs         = "reads_as"
	qPersonalization = "personalization"
	qAsk             = "ask"
	qSpamClaim       = "spam_claim"
)

// Scores are ordered rubrics read only through Normalized, so the first level
// is 0 and the last is 1 whatever their count.
var (
	readsAsLevels = []string{
		"A personal note from one person to another",
		"Somewhere between",
		"A bulk marketing email",
	}
	personalizationLevels = []string{
		"Written for this specific reader",
		"Could be sent to anyone",
	}
)

var askOptions = map[string]string{
	AskOneClear: "Asks the reader for exactly one thing",
	AskSeveral:  "Asks for more than one thing",
	AskNone:     "Asks for nothing",
}

// Questions is the entire question set, built once for one call.
func Questions() map[string]typesafe.Question {
	return map[string]typesafe.Question{
		qReadsAs:         typesafe.Score("How does this email read to the person receiving it?", readsAsLevels),
		qPersonalization: typesafe.Score("Is this email written for one specific reader, or could it be sent to anyone?", personalizationLevels),
		qAsk:             typesafe.Choice("What does this email ask the reader to do?", askOptions),
		qSpamClaim: typesafe.Noul("The message makes a claim a spam filter would object to: " +
			"guaranteed results, free money, prizes, or pressure to act now."),
	}
}

// Verdict is one judgment of one piece of copy. ReadsAs and Personalization
// are 0..1 positions on their rubric, 0 being the personal end.
type Verdict struct {
	ReadsAs         float64 `json:"reads_as"`
	Personalization float64 `json:"personalization"`
	Ask             string  `json:"ask"`
	SpamClaim       float64 `json:"spam_claim"`
	// Confidence is the lowest confidence across the two scores and the
	// choice, so one shaky answer makes the whole verdict shaky.
	Confidence  float64 `json:"confidence"`
	Model       string  `json:"model"`
	InputTokens int     `json:"input_tokens"`
}

// ReadsAsBulk is the Advisor's bulk-mail test: confidently past the bulk
// threshold, or carrying a claim a filter would object to whatever the tone.
func (v Verdict) ReadsAsBulk() bool {
	return (v.ReadsAs >= BulkAt && v.Confidence >= ConfFloor) || v.SpamClaim >= SpamClaimAt
}

// LacksClearAsk reports copy that asks for nothing or for several things, when
// the model was sure enough to say so.
func (v Verdict) LacksClearAsk() bool {
	return (v.Ask == AskNone || v.Ask == AskSeveral) && v.Confidence >= ConfFloor
}

// Summary is the one-sentence reading a person sees in the editor when no
// language model is configured to write findings.
func (v Verdict) Summary() string {
	lead := "Reads as"
	if v.Confidence < ConfFloor {
		lead = "Probably reads as"
	}
	var tone string
	switch {
	case v.ReadsAs < 1.0/3:
		tone = "a personal note"
	case v.ReadsAs < 2.0/3:
		tone = "somewhere between a personal note and bulk mail"
	default:
		tone = "bulk mail"
	}
	var ask string
	switch v.Ask {
	case AskOneClear:
		ask = " with one clear ask"
	case AskSeveral:
		ask = " that asks for several things"
	case AskNone:
		ask = " that asks for nothing"
	}
	s := fmt.Sprintf("%s %s%s.", lead, tone, ask)
	if v.SpamClaim >= SpamClaimAt {
		s += " It makes a claim a spam filter would object to."
	}
	return s
}

// ErrNoContent is returned when there is nothing to judge, before any call.
var ErrNoContent = errors.New("copyjudge: nothing to judge")

// state is what the model sees. Plain text only: markup is layout, not
// something the reader reads.
type state struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// Body picks the text a step's reader receives: the plain body when there is
// one, otherwise the HTML body. One rule, shared by every caller, so the hash
// the Advisor stores and the hash the editor computes agree.
func Body(plain, html string) string {
	if strings.TrimSpace(plain) != "" {
		return plain
	}
	return html
}

// Text renders a body down to the plain text sent as state: HTML is
// flattened, whitespace is trimmed, and the result is capped at BodyLimit.
func Text(body string) string {
	if mailhtml.LooksLikeHTML(body) {
		body = mailhtml.ToPlainText(body)
	}
	body = strings.TrimSpace(body)
	if utf8.RuneCountInString(body) > BodyLimit {
		body = string([]rune(body)[:BodyLimit])
	}
	return body
}

// cacheVersion is bumped whenever a question, rubric or threshold changes, so
// unchanged copy is not served a verdict from the previous policy.
const cacheVersion = "copyjudge-v1"

// ContentHash identifies one piece of copy for caching. Trimmed, not
// normalized further: a rewrite is a new judgment, a stray trailing newline is
// not.
func ContentHash(subject, body string) string {
	sum := sha256.Sum256([]byte(cacheVersion + "\x00" + strings.TrimSpace(subject) + "\x00" + strings.TrimSpace(body)))
	return hex.EncodeToString(sum[:])
}

// Judge asks every question in one call and reads the answers into a Verdict.
// It never decides anything itself; the thresholds above are read by callers
// through the Verdict's methods.
func Judge(ctx context.Context, asker typesafe.Asker, subject, body string) (*Verdict, error) {
	if asker == nil {
		return nil, errors.New("copyjudge: no asker configured")
	}
	st := state{Subject: strings.TrimSpace(subject), Body: Text(body)}
	if st.Subject == "" && st.Body == "" {
		return nil, ErrNoContent
	}

	resp, err := asker.Ask(typesafe.WithUsage(ctx, "copy_judgment", ""), st, Questions())
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("copyjudge: empty response")
	}

	readsAs, err := answer(resp, qReadsAs, typesafe.QuestionScore, len(readsAsLevels))
	if err != nil {
		return nil, err
	}
	personal, err := answer(resp, qPersonalization, typesafe.QuestionScore, len(personalizationLevels))
	if err != nil {
		return nil, err
	}
	ask, err := answer(resp, qAsk, typesafe.QuestionChoice, 0)
	if err != nil {
		return nil, err
	}
	spam, err := answer(resp, qSpamClaim, typesafe.QuestionNoul, 0)
	if err != nil {
		return nil, err
	}
	if _, known := askOptions[ask.Choice]; !known {
		return nil, fmt.Errorf("copyjudge: unknown ask %q", ask.Choice)
	}

	return &Verdict{
		ReadsAs:         readsAs.Normalized(len(readsAsLevels)),
		Personalization: personal.Normalized(len(personalizationLevels)),
		Ask:             ask.Choice,
		SpamClaim:       spam.Noul,
		Confidence:      min(readsAs.Confidence, personal.Confidence, ask.Confidence),
		Model:           resp.Model,
		InputTokens:     resp.Usage.InputTokens,
	}, nil
}

// answer reads one answer and refuses a shape the question did not ask for:
// a wrong type, a score off its rubric, or a probability outside 0..1 would
// otherwise reach the verdict as a plausible-looking zero.
func answer(resp *typesafe.Response, id, wantType string, levels int) (typesafe.Answer, error) {
	a, ok := resp.Answers[id]
	if !ok {
		return typesafe.Answer{}, fmt.Errorf("copyjudge: no answer for %q", id)
	}
	if a.Type != "" && a.Type != wantType {
		return typesafe.Answer{}, fmt.Errorf("copyjudge: %q answered as %s, want %s", id, a.Type, wantType)
	}
	unit := func(v float64) bool { return v >= 0 && v <= 1 }
	switch wantType {
	case typesafe.QuestionNoul:
		if !unit(a.Noul) {
			return typesafe.Answer{}, fmt.Errorf("copyjudge: %q noul %v out of range", id, a.Noul)
		}
	case typesafe.QuestionScore:
		if a.Score < 0 || a.Score > float64(levels-1) || !unit(a.Confidence) {
			return typesafe.Answer{}, fmt.Errorf("copyjudge: %q score %v off its rubric", id, a.Score)
		}
	case typesafe.QuestionChoice:
		if a.Choice == "" || !unit(a.Confidence) {
			return typesafe.Answer{}, fmt.Errorf("copyjudge: %q has no usable choice", id)
		}
	}
	return a, nil
}
