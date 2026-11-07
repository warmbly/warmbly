package bounceclass

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/warmbly/warmbly/internal/pkg/typesafe"
)

type fakeAsker struct {
	calls     int
	state     any
	questions map[string]typesafe.Question
	resp      *typesafe.Response
	err       error
}

func (f *fakeAsker) Ask(_ context.Context, state any, q map[string]typesafe.Question) (*typesafe.Response, error) {
	f.calls++
	f.state = state
	f.questions = q
	return f.resp, f.err
}

func answer(cause string, conf float64) *typesafe.Response {
	r := &typesafe.Response{Model: typesafe.Model, Answers: map[string]typesafe.Answer{
		questionCause: {Type: typesafe.QuestionChoice, Choice: cause, Confidence: conf},
	}}
	return r
}

func TestClassifyEmptyReasonAsksNothing(t *testing.T) {
	f := &fakeAsker{resp: answer(CauseReputationBlock, 0.99)}
	for _, reason := range []string{"", "   ", "\n\t"} {
		v, err := Classify(context.Background(), f, reason)
		if err != nil || v != nil {
			t.Fatalf("reason %q: got %+v, %v; want nil, nil", reason, v, err)
		}
	}
	if f.calls != 0 {
		t.Fatalf("empty reason reached the model %d times", f.calls)
	}
}

func TestClassifyAsksOneChoiceQuestion(t *testing.T) {
	f := &fakeAsker{resp: answer(CauseReputationBlock, 0.93)}
	v, err := Classify(context.Background(), f, "550 5.7.1 Service unavailable; client host blocked using Spamhaus")
	if err != nil {
		t.Fatal(err)
	}
	if f.calls != 1 {
		t.Fatalf("calls = %d, want 1", f.calls)
	}
	if len(f.questions) != 1 {
		t.Fatalf("questions = %d, want 1", len(f.questions))
	}
	q, ok := f.questions[questionCause]
	if !ok || q.Type != typesafe.QuestionChoice {
		t.Fatalf("question %q missing or not a choice: %+v", questionCause, f.questions)
	}
	opts, ok := q.Criteria.(map[string]string)
	if !ok || len(opts) != 5 {
		t.Fatalf("criteria = %#v, want the five causes", q.Criteria)
	}
	for _, c := range []string{CauseRecipientInvalid, CauseMailboxFull, CauseReputationBlock, CauseTransient, CauseOther} {
		if opts[c] == "" {
			t.Errorf("cause %q has no description", c)
		}
	}
	state, ok := f.state.(map[string]string)
	if !ok || state["reason"] == "" {
		t.Fatalf("state = %#v, want {reason}", f.state)
	}
	if v.Cause != CauseReputationBlock || v.Confidence != 0.93 || v.Model != typesafe.Model {
		t.Fatalf("verdict = %+v", v)
	}
}

func TestClassifyCapsReason(t *testing.T) {
	f := &fakeAsker{resp: answer(CauseOther, 0.5)}
	long := strings.Repeat("é", ReasonLimit+200)
	if _, err := Classify(context.Background(), f, long); err != nil {
		t.Fatal(err)
	}
	sent := f.state.(map[string]string)["reason"]
	if n := len([]rune(sent)); n != ReasonLimit {
		t.Fatalf("sent %d runes, want %d", n, ReasonLimit)
	}
}

func TestClassifyErrors(t *testing.T) {
	ctx := context.Background()
	if _, err := Classify(ctx, nil, "550 blocked"); err == nil {
		t.Error("nil asker accepted")
	}
	if _, err := Classify(ctx, &fakeAsker{err: errors.New("boom")}, "550 blocked"); err == nil {
		t.Error("transport error swallowed")
	}
	if _, err := Classify(ctx, &fakeAsker{resp: &typesafe.Response{}}, "550 blocked"); err == nil {
		t.Error("missing answer accepted")
	}
	if _, err := Classify(ctx, &fakeAsker{resp: answer("made_up", 0.9)}, "550 blocked"); err == nil {
		t.Error("unknown cause accepted")
	}
}

func TestAddressIsFine(t *testing.T) {
	cases := []struct {
		v    *Verdict
		want bool
	}{
		{nil, false},
		{&Verdict{Cause: CauseReputationBlock, Confidence: ConfFloor}, true},
		{&Verdict{Cause: CauseTransient, Confidence: 0.95}, true},
		{&Verdict{Cause: CauseMailboxFull, Confidence: 0.81}, true},
		{&Verdict{Cause: CauseReputationBlock, Confidence: 0.79}, false},
		{&Verdict{Cause: CauseRecipientInvalid, Confidence: 0.99}, false},
		{&Verdict{Cause: CauseOther, Confidence: 0.99}, false},
	}
	for _, c := range cases {
		if got := c.v.AddressIsFine(); got != c.want {
			t.Errorf("%+v: AddressIsFine = %v, want %v", c.v, got, c.want)
		}
	}
}

// Only a confident recipient_invalid verdict may suppress an address.
func TestNamesAddress(t *testing.T) {
	cases := []struct {
		v    *Verdict
		want bool
	}{
		{nil, false},
		{&Verdict{Cause: CauseRecipientInvalid, Confidence: ConfFloor}, true},
		{&Verdict{Cause: CauseRecipientInvalid, Confidence: 0.79}, false},
		{&Verdict{Cause: CauseOther, Confidence: 0.99}, false},
		{&Verdict{Cause: CauseReputationBlock, Confidence: 0.99}, false},
	}
	for _, c := range cases {
		if got := c.v.NamesAddress(); got != c.want {
			t.Errorf("%+v: NamesAddress = %v, want %v", c.v, got, c.want)
		}
	}
}
