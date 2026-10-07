package warmupcontent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/warmbly/warmbly/internal/pkg/typesafe"
)

// fakeAsker records the call and answers from a canned response.
type fakeAsker struct {
	resp      *typesafe.Response
	err       error
	state     threadState
	questions map[string]typesafe.Question
}

func (f *fakeAsker) Ask(_ context.Context, state any, questions map[string]typesafe.Question) (*typesafe.Response, error) {
	f.state = state.(threadState)
	f.questions = questions
	return f.resp, f.err
}

func judgeResponse(pitch, template, score, confidence float64) *typesafe.Response {
	resp := &typesafe.Response{
		Model: typesafe.Model,
		Answers: map[string]typesafe.Answer{
			judgeQuestionPitch:       {Type: typesafe.QuestionNoul, Noul: pitch},
			judgeQuestionTemplate:    {Type: typesafe.QuestionNoul, Noul: template},
			judgeQuestionNaturalness: {Type: typesafe.QuestionScore, Score: score, Confidence: confidence},
		},
	}
	resp.Usage.InputTokens = 321
	return resp
}

func TestJudgeThreadAsksOneCallWithTheWholeThread(t *testing.T) {
	asker := &fakeAsker{resp: judgeResponse(0.1, 0.1, 2, 0.9)}
	got, err := judgeThread(context.Background(), asker, " Coffee? ", "Are you around Thursday?", []string{"Yes, after two.", "Perfect."})
	if err != nil {
		t.Fatalf("judgeThread: %v", err)
	}

	if asker.state.Subject != "Coffee?" || asker.state.Opening != "Are you around Thursday?" {
		t.Errorf("state = %+v", asker.state)
	}
	if len(asker.state.Replies) != 2 {
		t.Errorf("replies = %v", asker.state.Replies)
	}
	for _, name := range []string{judgeQuestionPitch, judgeQuestionTemplate, judgeQuestionNaturalness} {
		if _, ok := asker.questions[name]; !ok {
			t.Errorf("question %q not asked", name)
		}
	}
	if len(asker.questions) != 3 {
		t.Errorf("asked %d questions, want 3", len(asker.questions))
	}
	if asker.questions[judgeQuestionNaturalness].Type != typesafe.QuestionScore {
		t.Errorf("naturalness type = %q", asker.questions[judgeQuestionNaturalness].Type)
	}
	for name, q := range asker.questions {
		lower := strings.ToLower(q.Instructions)
		if strings.Contains(lower, "link") || strings.Contains(lower, "sign up") || strings.Contains(lower, "signup") {
			t.Errorf("question %q asks about what warmlint already checks: %q", name, q.Instructions)
		}
	}

	if got.Pitch != 0.1 || got.Template != 0.1 {
		t.Errorf("nouls = %.2f/%.2f", got.Pitch, got.Template)
	}
	if got.Naturalness != 1 {
		t.Errorf("naturalness = %.2f, want 1 (top of a three-level rubric)", got.Naturalness)
	}
	if got.Confidence != 0.9 || got.Model != typesafe.Model || got.InputTokens != 321 {
		t.Errorf("judgment = %+v", got)
	}
	if reject, reason := got.Reject(); reject {
		t.Errorf("rejected a clean thread: %s", reason)
	}
}

func TestJudgeThreadSurfacesErrors(t *testing.T) {
	asker := &fakeAsker{err: errors.New("boom")}
	if _, err := judgeThread(context.Background(), asker, "s", "d", nil); err == nil {
		t.Fatal("expected the asker error")
	}

	missing := &fakeAsker{resp: &typesafe.Response{Answers: map[string]typesafe.Answer{}}}
	if _, err := judgeThread(context.Background(), missing, "s", "d", nil); err == nil {
		t.Fatal("expected an error for a response with no answers")
	}

	if _, err := judgeThread(context.Background(), nil, "s", "d", nil); err == nil {
		t.Fatal("expected an error for a nil asker")
	}
}

func TestThreadJudgmentReject(t *testing.T) {
	tests := []struct {
		name   string
		j      ThreadJudgment
		reject bool
		reason string
	}{
		{name: "clean", j: ThreadJudgment{Pitch: 0.2, Template: 0.3, Naturalness: 1, Confidence: 0.9}},
		{name: "pitch at the yes level", j: ThreadJudgment{Pitch: 0.60, Naturalness: 1, Confidence: 0.9}, reject: true, reason: "pitch"},
		{name: "pitch just under", j: ThreadJudgment{Pitch: 0.59, Naturalness: 1, Confidence: 0.9}},
		{name: "template needs a strong answer", j: ThreadJudgment{Template: 0.79, Naturalness: 1, Confidence: 0.9}},
		{name: "template strong", j: ThreadJudgment{Template: 0.80, Naturalness: 1, Confidence: 0.9}, reject: true, reason: "filler"},
		{name: "unnatural and confident", j: ThreadJudgment{Naturalness: 0, Confidence: 0.70}, reject: true, reason: "unnatural"},
		{name: "unsure is not approval", j: ThreadJudgment{Naturalness: 0, Confidence: 0.69}, reject: true, reason: "unnatural"},
		{name: "ambiguous is not approval", j: ThreadJudgment{Naturalness: 0.5, Confidence: 0.99}, reject: true, reason: "unnatural"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reject, reason := tc.j.Reject()
			if reject != tc.reject {
				t.Fatalf("reject = %v (%q), want %v", reject, reason, tc.reject)
			}
			if tc.reject && !strings.Contains(reason, tc.reason) {
				t.Errorf("reason = %q, want it to mention %q", reason, tc.reason)
			}
			if !tc.reject && reason != "" {
				t.Errorf("accepted with a reason: %q", reason)
			}
		})
	}
}

func TestBoundThreadStateCapsTotalRunes(t *testing.T) {
	long := strings.Repeat("é", 50)
	state := boundThreadState("subj", long, []string{long, long, long}, 120)

	total := len([]rune(state.Subject)) + len([]rune(state.Opening))
	for _, r := range state.Replies {
		total += len([]rune(r))
	}
	if total != 120 {
		t.Errorf("total runes = %d, want exactly the cap", total)
	}
	if state.Subject != "subj" || state.Opening != long {
		t.Errorf("subject and opening are taken first: %+v", state)
	}
	// 4 + 50 leaves 66: one whole reply and 16 runes of the next, the third dropped.
	if len(state.Replies) != 2 || len([]rune(state.Replies[1])) != 16 {
		t.Errorf("replies = %d entries, second has %d runes", len(state.Replies), len([]rune(state.Replies[1])))
	}
}
