package inboxtag

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/repository"
)

func storedRow(kind, intent string, answers map[string]Answer) *repository.InboxTagResult {
	raw, _ := json.Marshal(answers)
	return &repository.InboxTagResult{
		Kind: kind, KindConfidence: 0.95, Intent: intent, IntentConfidence: 0.9, Answers: raw,
	}
}

func TestDraftDecision(t *testing.T) {
	cases := []struct {
		name string
		row  *repository.InboxTagResult
		want bool
		why  string
	}{
		{"untrusted verdict drafts", &repository.InboxTagResult{Kind: KindHumanReply, KindConfidence: 0.4}, true, ""},
		{"needs review drafts", &repository.InboxTagResult{Kind: KindBounceHard, KindConfidence: 0.99, NeedsReview: true}, true, ""},
		{"bounce", storedRow(KindBounceHard, "", nil), false, "not a human reply: bounce_hard"},
		{"closed intent", storedRow(KindHumanReply, IntentNotInterested, nil), false, "the reply closed the conversation: not_interested"},
		{"opt out", storedRow(KindHumanReply, IntentOptOut, nil), false, "the reply closed the conversation: opt_out"},
		{"wrong person", storedRow(KindHumanReply, IntentWrongPerson, nil), false, "the reply closed the conversation: wrong_person"},
		{"not now", storedRow(KindHumanReply, IntentNotNow, nil), false, "the reply closed the conversation: not_now"},
		{"open intent", storedRow(KindHumanReply, IntentWantsInfo, nil), true, ""},
		{"legal threat", storedRow(KindHumanReply, IntentWantsInfo, map[string]Answer{SigLegalThreat: {Noul: 0.7}}), false, "legal threat: a person reads this first"},
		{"needs a person", storedRow(KindHumanReply, IntentWantsInfo, map[string]Answer{SigNeedsHumanJudgement: {Noul: 0.9}}), false, "needs human judgement"},
		{"one-line ack", storedRow(KindHumanReply, IntentQuestionAnswered, map[string]Answer{ScoreReplyEffort: {Score: 0}}), false, "a one-line acknowledgement is not worth a draft"},
		{"real write-up", storedRow(KindHumanReply, IntentQuestionAnswered, map[string]Answer{ScoreReplyEffort: {Score: 2}}), true, ""},
	}
	for _, c := range cases {
		got, why := DraftDecision(c.row)
		if got != c.want || why != c.why {
			t.Errorf("%s: draft=%v (%s), want %v (%s)", c.name, got, why, c.want, c.why)
		}
	}
}

func TestShouldDraftWithoutEvidenceDrafts(t *testing.T) {
	ok, why := NewDraftGate(nil).ShouldDraft(context.Background(), uuid.New(), "m")
	if !ok || why != "" {
		t.Fatalf("draft=%v why=%q", ok, why)
	}
}
