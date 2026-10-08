package form

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/typesafe"
)

// Submission triage asks TypeSafe two questions about the answers a visitor
// typed and files the result on the submission. The model answers, this code
// decides: every threshold is here, and only junk changes what Submit does.
const (
	// TriageJunkAt is the noul level at which a submission is junk. Junk is
	// kept and flagged, but creates no contact and joins no campaign, so the
	// bar is the strong one.
	TriageJunkAt = 0.80

	// TriageConfFloor is the confidence below which the submitter choice is
	// not trusted; the submission is filed as other with that confidence.
	TriageConfFloor = 0.70

	// triageMinRunes is the least answer text worth a call: an email and a
	// checkbox say nothing about who is asking.
	triageMinRunes = 12
	// triageValueRunes caps one answer; triageStateRunes caps the whole state.
	triageValueRunes = 500
	triageStateRunes = 4000

	// triageTimeout bounds the call so a slow judge never delays the
	// visitor's success page.
	triageTimeout = 5 * time.Second
)

const triageJunkQuestion = "The submission is spam, gibberish, a test entry, or an unsolicited pitch aimed at the form owner."

var triageQuestions = map[string]typesafe.Question{
	"junk": typesafe.Noul(triageJunkQuestion),
	"submitter": typesafe.Choice("Who filled in this form, going by their answers?", map[string]string{
		models.FormTriageBuyer:     "A prospective customer, or someone asking about the product or service",
		models.FormTriageVendor:    "Someone selling or pitching a product or service",
		models.FormTriageJobSeeker: "Someone asking about a job or an internship",
		models.FormTriageOther:     "None of the above",
	}),
}

// triageAnswer is one labelled answer as sent to the model. Nothing about the
// visitor's connection (IP, user agent, page URL) is part of the state.
type triageAnswer struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type triageState struct {
	Answers []triageAnswer `json:"answers"`
}

// triage returns the verdict for one submission. ok is false when nothing was
// decided: too little text, a failed call, or an answer the model did not
// give. The caller stores the submission either way.
func triage(ctx context.Context, asker typesafe.Asker, fields []models.FormField, data map[string]any) (string, float64, bool) {
	if asker == nil {
		return "", 0, false
	}
	state, runes := buildTriageState(fields, data)
	if runes < triageMinRunes {
		return "", 0, false
	}
	resp, err := asker.Ask(typesafe.WithUsage(ctx, "form_triage", ""), state, triageQuestions)
	if err != nil || resp == nil {
		return "", 0, false
	}
	if junk, ok := resp.Answers["junk"]; ok && junk.Noul >= TriageJunkAt {
		return models.FormTriageJunk, junk.Noul, true
	}
	who, ok := resp.Answers["submitter"]
	if !ok {
		return "", 0, false
	}
	if who.Confidence < TriageConfFloor {
		return models.FormTriageOther, who.Confidence, true
	}
	switch who.Choice {
	case models.FormTriageBuyer, models.FormTriageVendor, models.FormTriageJobSeeker, models.FormTriageOther:
		return who.Choice, who.Confidence, true
	}
	// An option the question never offered must not reach the CHECK constraint.
	return models.FormTriageOther, who.Confidence, true
}

// buildTriageState turns the stored data into labelled answers in field
// order, capped per value and in total, and reports how many runes of answer
// text it carries. Hidden fields are the owner's constants, not the visitor's
// words, so they are left out.
func buildTriageState(fields []models.FormField, data map[string]any) (triageState, int) {
	state := triageState{Answers: []triageAnswer{}}
	total := 0
	for _, f := range fields {
		if !f.Type.IsInput() || f.Type == models.FormFieldHidden {
			continue
		}
		v := strings.TrimSpace(answerString(data[f.ID]))
		if v == "" {
			continue
		}
		v = capRunes(v, triageValueRunes)
		if room := triageStateRunes - total; room <= 0 {
			break
		} else if utf8.RuneCountInString(v) > room {
			v = capRunes(v, room)
		}
		label := f.Label
		if label == "" {
			label = f.ID
		}
		state.Answers = append(state.Answers, triageAnswer{Label: label, Value: v})
		total += utf8.RuneCountInString(v)
	}
	return state, total
}

// answerString flattens a stored answer: a string, or a checkbox group's slice.
func answerString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []string:
		return strings.Join(t, ", ")
	case []any:
		parts := make([]string, 0, len(t))
		for _, p := range t {
			if s, ok := p.(string); ok {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ", ")
	}
	return ""
}

func capRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n])
}
