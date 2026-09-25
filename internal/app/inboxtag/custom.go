package inboxtag

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/warmbly/warmbly/internal/models"
)

// Workspace questions ride in the same call as the built-in set. They add
// labels and may act through the same Plan, but never touch kind, intent or
// relevance: those stay calibrated against the fixtures.

// customPrefix keys a workspace question's answer, so it can never collide
// with a built-in id in the one question map.
const customPrefix = "custom_"

// noneOfThese is added to every choice question, so a reply that fits no
// option has somewhere to go instead of being forced into a wrong label.
const noneOfThese = "none_of_these"

// CustomMatch is one workspace question that fired on a message.
type CustomMatch struct {
	QuestionID string
	Label      string
	Action     models.InboxTagQuestionAction
	// Strength is the noul, or the choice confidence, it fired at.
	Strength float64
	// Acts is whether the answer is strong enough to act on, not only label:
	// Strong for a yes/no, ConfFloor for a choice.
	Acts bool
}

func customKey(id string) string { return customPrefix + id }

func optionKey(i int) string { return "option_" + strconv.Itoa(i+1) }

// QuestionsFor is the built-in set plus the workspace's own.
func QuestionsFor(custom []models.InboxTagQuestion) map[string]Question {
	q := Questions()
	for _, c := range custom {
		switch c.Type {
		case models.InboxTagQuestionYesNo:
			q[customKey(c.ID)] = Question{Type: QuestionNoul, Instructions: c.Question}
		case models.InboxTagQuestionChoice:
			criteria := make(map[string]string, len(c.Choices)+1)
			for i, opt := range c.Choices {
				criteria[optionKey(i)] = opt.Description
			}
			criteria[noneOfThese] = "None of the other options fits this message"
			q[customKey(c.ID)] = Question{Type: QuestionChoice, Instructions: c.Question, Criteria: criteria}
		}
	}
	return q
}

// automatedKind is mail no person wrote. A workspace question is about what
// someone said, so it labels none of these.
func automatedKind(kind string) bool {
	switch kind {
	case KindBounceHard, KindBounceSoft, KindAutoReplyOOO, KindAutoReplyTicket, KindNotification:
		return true
	}
	return false
}

// decideCustom reads the workspace questions' answers onto a decision.
func decideCustom(d *Decision, answers map[string]Answer, custom []models.InboxTagQuestion) {
	if d.Kind == "" || automatedKind(d.Kind) {
		return
	}
	seen := map[string]bool{}
	for _, l := range d.Labels {
		seen[l] = true
	}
	add := func(m CustomMatch) {
		d.Custom = append(d.Custom, m)
		if !seen[m.Label] {
			seen[m.Label] = true
			d.Labels = append(d.Labels, m.Label)
		}
	}
	for _, c := range custom {
		a, ok := answers[customKey(c.ID)]
		if !ok {
			continue
		}
		switch c.Type {
		case models.InboxTagQuestionYesNo:
			if a.Noul >= Yes && c.Label != "" {
				add(CustomMatch{QuestionID: c.ID, Label: c.Label, Action: c.Action, Strength: a.Noul, Acts: a.Noul >= Strong})
			}
		case models.InboxTagQuestionChoice:
			if a.Confidence < ConfFloor || a.Choice == noneOfThese {
				continue
			}
			for i, opt := range c.Choices {
				if a.Choice == optionKey(i) && opt.Label != "" {
					add(CustomMatch{QuestionID: c.ID, Label: opt.Label, Action: opt.Action, Strength: a.Confidence, Acts: true})
					break
				}
			}
		}
	}
}

// planCustom folds the workspace questions' actions into a plan: the longest
// hold wins, a stop is a stop, and a built-in task keeps its own title.
func planCustom(p *Plan, d Decision) {
	for _, m := range d.Custom {
		if !m.Acts {
			continue
		}
		switch m.Action.Type {
		case models.InboxTagActionHold:
			if m.Action.HoldDays > p.HoldDays {
				p.HoldDays = m.Action.HoldDays
				p.HoldReason = fmt.Sprintf("replied %q", m.Label)
			}
		case models.InboxTagActionStop:
			p.Stop = true
		case models.InboxTagActionTask:
			if p.Task == "" {
				p.Task = fmt.Sprintf("Reply tagged %q", m.Label)
			}
		}
	}
}

// ReservedLabel reports a label a workspace question may not use: one the
// built-in taxonomy already writes, in any case, which would make its meaning
// ambiguous.
func ReservedLabel(label string) bool {
	for _, l := range AllLabels() {
		if strings.EqualFold(l, label) {
			return true
		}
	}
	return false
}

// ValidateQuestions refuses a workspace question whose label the built-in
// taxonomy owns. Shape is checked by the settings model.
func ValidateQuestions(qs []models.InboxTagQuestion) error {
	check := func(label string) error {
		if ReservedLabel(label) {
			return fmt.Errorf("label %q is a built-in tagging label; pick another name", label)
		}
		return nil
	}
	for _, q := range qs {
		if err := check(q.Label); q.Label != "" && err != nil {
			return err
		}
		for _, c := range q.Choices {
			if err := check(c.Label); err != nil {
				return err
			}
		}
	}
	return nil
}

// CustomLabels is every label the workspace questions can write.
func CustomLabels(qs []models.InboxTagQuestion) []string {
	var out []string
	for _, q := range qs {
		if q.Label != "" {
			out = append(out, q.Label)
		}
		for _, c := range q.Choices {
			if c.Label != "" {
				out = append(out, c.Label)
			}
		}
	}
	return out
}

// LanguageHint is what the state carries for a workspace's tagging languages:
// their names, or "" when none is chosen.
func LanguageHint(codes []string) string {
	names := make([]string, 0, len(codes))
	for _, c := range codes {
		if n := models.MailLanguageNames[c]; n != "" {
			names = append(names, n)
		}
	}
	return strings.Join(names, ", ")
}
