package inboxtag

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/replyclassify"
	"github.com/warmbly/warmbly/internal/repository"
)

func TestStoredReplyResultMatchesTheInbox(t *testing.T) {
	org := uuid.New()
	repo := &fakeRepo{saved: []*repository.InboxTagResult{
		{MessageID: "pricing", Kind: KindHumanReply, KindConfidence: 1, Intent: IntentWantsPricing, IntentConfidence: 0.91},
		{MessageID: "info", Kind: KindHumanReply, KindConfidence: 1, Intent: IntentWantsInfo, IntentConfidence: 0.88},
		{MessageID: "declined", Kind: KindHumanReply, KindConfidence: 1, Intent: IntentNotInterested, IntentConfidence: 0.98},
		{MessageID: "unsure-intent", Kind: KindHumanReply, KindConfidence: 1, Intent: IntentWantsInfo, IntentConfidence: 0.48, NeedsReview: true},
		{MessageID: "unsure-kind", Kind: KindNotification, KindConfidence: 0.44, NeedsReview: true},
		{MessageID: "notice", Kind: KindNotification, KindConfidence: 0.93},
		{MessageID: "bounce", Kind: KindBounceHard, KindConfidence: 0.97},
		{MessageID: "pitch", Kind: KindColdInbound, KindConfidence: 0.9},
	}}

	for _, tc := range []struct {
		id    string
		want  string
		found bool
	}{
		{"pricing", replyclassify.ClassPositive, true},
		{"info", replyclassify.ClassPositive, true},
		{"declined", replyclassify.ClassNegative, true},
		{"unsure-intent", replyclassify.ClassUnknown, true},
		{"unsure-kind", replyclassify.ClassUnknown, true},
		{"notice", replyclassify.ClassAutoReply, true},
		{"bounce", replyclassify.ClassAutoReply, true},
		{"pitch", replyclassify.ClassUnknown, true},
		{"never-tagged", "", false},
	} {
		got, found := StoredReplyResult(context.Background(), repo, org, tc.id)
		if found != tc.found || got.Class != tc.want {
			t.Errorf("%s: got %q found=%v, want %q found=%v", tc.id, got.Class, found, tc.want, tc.found)
		}
	}
}

func TestReplyClassifierReadsStoredVerdictBehindAClosedGate(t *testing.T) {
	org := uuid.New()
	repo := &fakeRepo{saved: []*repository.InboxTagResult{
		{MessageID: "m1", Kind: KindHumanReply, KindConfidence: 1, Intent: IntentWantsPricing, IntentConfidence: 0.9},
	}}
	classify := ReplyClassifier(nil, repo)
	closed := func() bool { return false }

	got, ok := classify(context.Background(), replyclassify.Input{OrganizationID: org, MessageID: "m1"}, closed)
	if !ok || got.Class != replyclassify.ClassPositive {
		t.Fatalf("got %q ok=%v, want positive", got.Class, ok)
	}
	if _, ok := classify(context.Background(), replyclassify.Input{OrganizationID: org, MessageID: "m2", BodyText: "What does it cost?"}, closed); ok {
		t.Fatal("answered a message with no stored verdict behind a closed gate")
	}
}
