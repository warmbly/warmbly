package advanced

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/inboxtag"
	"github.com/warmbly/warmbly/internal/app/replyclassify"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func TestRecordedIntentFollowsTheVerdict(t *testing.T) {
	for _, tc := range []struct {
		verdict replyclassify.Result
		want    models.ReplyIntentType
		decided bool
	}{
		{replyclassify.Result{Class: replyclassify.ClassPositive, Source: replyclassify.SourceModel}, models.ReplyIntentPositive, true},
		{replyclassify.Result{Class: replyclassify.ClassNegative, Source: replyclassify.SourceModel}, models.ReplyIntentNegative, true},
		{replyclassify.Result{Class: replyclassify.ClassUnsubscribe, Source: replyclassify.SourceLexicon}, models.ReplyIntentNegative, true},
		{replyclassify.Result{Class: replyclassify.ClassNeutral, Source: replyclassify.SourceModel}, models.ReplyIntentNeutral, true},
		{replyclassify.Result{Class: replyclassify.ClassOutOfOffice, Source: replyclassify.SourceHeader}, models.ReplyIntentOutOfOffice, true},
		{replyclassify.Result{Class: replyclassify.ClassAutoReply, Source: replyclassify.SourceHeader}, models.ReplyIntentAutomated, true},
		// The tagger was unsure: neutral, never the keyword list's guess.
		{replyclassify.Result{Class: replyclassify.ClassUnknown, Source: replyclassify.SourceModel}, models.ReplyIntentNeutral, true},
		{replyclassify.Result{Class: replyclassify.ClassUnknown}, "", false},
		{replyclassify.Result{}, "", false},
	} {
		got, _, decided := verdictIntent(tc.verdict)
		if got != tc.want || decided != tc.decided {
			t.Errorf("%s from %q: got %q decided=%v, want %q decided=%v", tc.verdict.Class, tc.verdict.Source, got, decided, tc.want, tc.decided)
		}
	}
}

func TestPricingQuestionIsNotRecordedFromKeywords(t *testing.T) {
	// A reply TypeSafe read as a pricing request; the default keyword list
	// alone would have called it a question.
	verdict := replyclassify.Result{Class: replyclassify.ClassPositive, Confidence: 0.91, Source: replyclassify.SourceModel}
	intent, conf, decided := verdictIntent(verdict)
	if !decided || intent != models.ReplyIntentPositive || conf != 0.91 {
		t.Fatalf("got %q %.2f decided=%v", intent, conf, decided)
	}
	if kw, _ := classifyReply("What's your pricing? Can you send more info?", models.DefaultAdvancedOutreachSettings().ReplyIntent); kw == intent {
		t.Fatalf("keyword list already agrees (%q); the case no longer guards anything", kw)
	}
}

// A pricing question the English keywords read as "not looking" is counted,
// recorded and sent to webhooks as the tagger read it.
func TestProcessIncomingReplyRecordsTheTaggerVerdictEverywhere(t *testing.T) {
	for _, tc := range []struct {
		name       string
		stored     *repository.InboxTagResult
		noCampaign bool
		body       string
		wantClass  string
		wantIntent string
		wantStored string
	}{
		{
			name:       "pricing request",
			stored:     &repository.InboxTagResult{Kind: inboxtag.KindHumanReply, KindConfidence: 1, Intent: inboxtag.IntentWantsPricing, IntentConfidence: 0.92},
			wantClass:  replyclassify.ClassPositive,
			wantIntent: string(models.ReplyIntentPositive),
			wantStored: inboxtag.IntentWantsPricing,
		},
		{
			name:       "opt-out past the preview",
			stored:     &repository.InboxTagResult{Kind: inboxtag.KindHumanReply, KindConfidence: 1, Intent: inboxtag.IntentWantsPricing, IntentConfidence: 0.92},
			body:       "How much is it? Not looking to spend a lot, send details. " + strings.Repeat("We compared a few vendors last quarter and kept notes. ", 5) + "Actually, please remove me from your list.",
			wantClass:  replyclassify.ClassUnsubscribe,
			wantIntent: string(models.ReplyIntentNegative),
			wantStored: inboxtag.IntentOptOut,
		},
		{
			name:       "contact writing in with no campaign",
			stored:     &repository.InboxTagResult{Kind: inboxtag.KindHumanReply, KindConfidence: 1, Intent: inboxtag.IntentWantsPricing, IntentConfidence: 0.92},
			noCampaign: true,
			wantIntent: string(models.ReplyIntentPositive),
		},
		{
			name:       "unsure verdict is unclassified, not a keyword guess",
			stored:     &repository.InboxTagResult{Kind: inboxtag.KindHumanReply, KindConfidence: 1, Intent: inboxtag.IntentWantsInfo, IntentConfidence: 0.48, NeedsReview: true, ReviewReason: "intent"},
			wantClass:  replyclassify.ClassUnknown,
			wantIntent: string(models.ReplyIntentNeutral),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			orgID, accountID, contactID := uuid.New(), uuid.New(), uuid.New()
			lead := "lead" + "@" + "prospect.test"
			account := &models.Email{ID: accountID, OrganizationID: &orgID, Email: "sender" + "@" + "warmbly.test"}
			service, progress := newIncomingReplyService(account, &models.Contact{ID: contactID, Email: lead}, contactID)
			store := verdictStore{res: tc.stored}
			service.inboxTags = store
			if tc.noCampaign {
				service.taskRepo = incomingReplyTaskRepo{}
			}
			// An opt-out suppresses the sender and clears the contact's flag.
			service.repo = &optOutAdvancedRepo{incomingReplyAdvancedRepo: progress.advanced}
			service.contactRepo = &optOutContactRepo{incomingReplyContactRepo: service.contactRepo.(incomingReplyContactRepo)}
			replyclassify.SetTypedClassifier(inboxtag.ReplyClassifier(nil, store))
			t.Cleanup(func() { replyclassify.SetTypedClassifier(nil) })
			events := &capturedEvents{}
			service.dispatcher = events

			xerr := service.ProcessIncomingReply(context.Background(), accountID, &models.EmailMessageStoreData{
				ID:        uuid.New(),
				EmailID:   accountID,
				MessageID: "<reply-1" + "@" + "prospect.test>",
				Folder:    models.FolderInbox,
				FromAddr:  []string{"Lead <" + lead + ">"},
				ToAddr:    []string{account.Email},
				InReplyTo: []string{"<step-1" + "@" + "warmbly.test>"},
				Subject:   "Re: Hello",
				Snippet:   "How much is it? Not looking to spend a lot, send details.",
				BodyText:  tc.body,
			})
			if xerr != nil {
				t.Fatal(xerr)
			}
			if progress.class != tc.wantClass {
				t.Errorf("reply_class = %q, want %q", progress.class, tc.wantClass)
			}
			if progress.intent != tc.wantStored {
				t.Errorf("reply_intent = %q, want %q", progress.intent, tc.wantStored)
			}
			reply := events.data[models.WebhookEventCampaignReplyReceived]
			if reply == nil {
				t.Fatal("no campaign.reply_received event")
			}
			if reply["intent"] != tc.wantIntent {
				t.Errorf("webhook intent = %v, want %q", reply["intent"], tc.wantIntent)
			}
		})
	}
}
