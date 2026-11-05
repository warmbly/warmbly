package replyclassify

import (
	"context"
	"strings"
	"testing"
)

// typedStub answers like the tagger: a stored verdict costs nothing, a new
// question only when mayAsk allows it.
type typedStub struct {
	stored *Result
	fresh  *Result
	asked  bool
}

func (s *typedStub) classify(_ context.Context, _ Input, mayAsk func() bool) (Result, bool) {
	if s.stored != nil {
		return *s.stored, true
	}
	if !mayAsk() || s.fresh == nil {
		return Result{}, false
	}
	s.asked = true
	return *s.fresh, true
}

func wireTyped(t *testing.T, s *typedStub) {
	t.Helper()
	SetTypedClassifier(s.classify)
	t.Cleanup(func() { SetTypedClassifier(nil) })
}

func TestTypedVerdictOutranksKeywordSentiment(t *testing.T) {
	positive := &Result{Class: ClassPositive, Confidence: 0.9, Source: SourceModel}
	unsure := &Result{Class: ClassUnknown, Source: SourceModel}

	for _, tc := range []struct {
		name   string
		stub   typedStub
		in     Input
		want   string
		source string
	}{
		{
			name:   "pricing question the keywords read as negative",
			stub:   typedStub{stored: positive},
			in:     Input{Subject: "Re: Quick question", BodyText: "How much is it? Not looking to spend a lot, send details."},
			want:   ClassPositive,
			source: SourceModel,
		},
		{
			name:   "a campaign subject cannot decide the reply",
			stub:   typedStub{fresh: positive},
			in:     Input{Subject: "Re: Not the right time?", BodyText: "What's your pricing? Booked a slot on your calendar."},
			want:   ClassPositive,
			source: SourceModel,
		},
		{
			name:   "an unsure stored verdict is not replaced by a keyword guess",
			stub:   typedStub{stored: unsure},
			in:     Input{Subject: "Re: Quick question", BodyText: "Price? No need for a long deck, just the numbers."},
			want:   ClassUnknown,
			source: SourceModel,
		},
		{
			name:   "an opt-out stands whatever the model says",
			stub:   typedStub{stored: positive},
			in:     Input{Subject: "Re: Quick question", BodyText: "Please remove me from your list."},
			want:   ClassUnsubscribe,
			source: SourceLexicon,
		},
		{
			name:   "keywords decide when the model has no answer",
			stub:   typedStub{},
			in:     Input{Subject: "Re: Quick question", BodyText: "Not interested, thanks."},
			want:   ClassNegative,
			source: SourceLexicon,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := tc.stub
			wireTyped(t, &stub)
			got := ClassifyContext(context.Background(), tc.in)
			if got.Class != tc.want || got.Source != tc.source {
				t.Fatalf("got %s from %q, want %s from %q", got.Class, got.Source, tc.want, tc.source)
			}
		})
	}
}

func TestGateHoldsBackOnlyANewQuestion(t *testing.T) {
	in := Input{Subject: "Re: Quick question", BodyText: "How much is it? Not looking to spend a lot, send details."}
	closed := func() bool { return false }

	stored := &typedStub{stored: &Result{Class: ClassPositive, Confidence: 0.9, Source: SourceModel}}
	wireTyped(t, stored)
	if got := ClassifyGated(context.Background(), in, closed); got.Class != ClassPositive {
		t.Fatalf("stored verdict behind a closed gate: got %s, want %s", got.Class, ClassPositive)
	}

	fresh := &typedStub{fresh: &Result{Class: ClassPositive, Confidence: 0.9, Source: SourceModel}}
	wireTyped(t, fresh)
	if got := ClassifyKnown(context.Background(), in); fresh.asked || got.Source != SourceLexicon {
		t.Fatalf("ClassifyKnown asked a new question (asked=%v, source %q)", fresh.asked, got.Source)
	}
}

func TestOfflineNeverAsksTheTypedLayer(t *testing.T) {
	SetTypedClassifier(func(context.Context, Input, func() bool) (Result, bool) {
		t.Fatal("typed layer asked on the offline path")
		return Result{}, false
	})
	t.Cleanup(func() { SetTypedClassifier(nil) })

	got := ClassifyOffline(Input{Subject: "Re: Quick question", BodyText: "Not interested, thanks."})
	if got.Class != ClassNegative {
		t.Fatalf("got %s, want %s", got.Class, ClassNegative)
	}
}

func TestGateIsConsultedOnce(t *testing.T) {
	// No stored verdict and no confident answer, so both model layers want the gate.
	wireTyped(t, &typedStub{})
	SetModelClassifier(func(context.Context, string, string) (string, error) { return "neutral", nil })
	t.Cleanup(func() { SetModelClassifier(nil) })

	calls := 0
	gate := func() bool { calls++; return true }
	ClassifyGated(context.Background(), Input{Subject: "Re: Quick question", BodyText: "Can you walk me through it next week?"}, gate)
	if calls != 1 {
		t.Fatalf("gate consulted %d times, want 1", calls)
	}
}

func TestModelReadsTheReplyNotTheQuotedHistory(t *testing.T) {
	var seen string
	SetModelClassifier(func(_ context.Context, _, user string) (string, error) { seen = user; return "positive", nil })
	t.Cleanup(func() { SetModelClassifier(nil) })
	read := func(body string) string {
		seen = ""
		ClassifyContext(context.Background(), Input{Subject: "Re: Quick question", BodyText: body})
		return seen
	}
	quote := "\n\nOn Mon, Nov 3, 2025 at 9:14 AM Sender <sender@warmbly.test> wrote:\n> Not the right time? No need to reply."

	t.Run("quoted history is dropped", func(t *testing.T) {
		got := read("Can you walk me through it next week?" + quote)
		if !strings.Contains(got, "walk me through it") || strings.Contains(got, "No need to reply") {
			t.Fatalf("model input %q", got)
		}
	})
	t.Run("a reply below the quote is read", func(t *testing.T) {
		got := read(strings.TrimSpace(quote) + "\n\nWhat is the price for ten seats?")
		if !strings.Contains(got, "What is the price") {
			t.Fatalf("model input %q lost the reply", got)
		}
	})
	t.Run("a long reply is capped", func(t *testing.T) {
		got := read("Can you walk me through it? " + strings.Repeat("x", 2*modelBodyRunes))
		if n := len([]rune(got)); n > modelBodyRunes+len("Subject: Re: Quick question\n\n") {
			t.Fatalf("the model read %d runes, cap is %d", n, modelBodyRunes)
		}
	})
}
