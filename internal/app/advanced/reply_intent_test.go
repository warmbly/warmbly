package advanced

import (
	"testing"

	"github.com/warmbly/warmbly/internal/app/replyclassify"
	"github.com/warmbly/warmbly/internal/models"
)

// Issue #471: the keyword classifier reads words, so an auto-responder that
// says nothing about vacations came back "neutral" and opened a high-priority
// follow-up. The header layer sees the markers instead, and the intent it
// produces is what the task gate reads.
func TestAutomatedRepliesNeverReachTheDefaultTaskGate(t *testing.T) {
	settings := models.DefaultAdvancedOutreachSettings().ReplyIntent
	for _, tc := range []struct {
		name  string
		in    replyclassify.Input
		want  models.ReplyIntentType
		human bool
	}{
		{
			name: "exchange out of office",
			in:   replyclassify.Input{Subject: "Automatic reply: Quick question"},
			want: models.ReplyIntentOutOfOffice,
		},
		{
			name: "rfc 3834 auto-generated",
			in:   replyclassify.Input{Headers: map[string][]string{"Auto-Submitted": {"auto-generated"}}, Subject: "Ticket 8812 received"},
			want: models.ReplyIntentAutomated,
		},
		{
			name: "delivery status report",
			in: replyclassify.Input{
				Headers: map[string][]string{"Content-Type": {"multipart/report; report-type=delivery-status; boundary=x"}},
				Subject: "Undeliverable",
			},
			want: models.ReplyIntentAutomated,
		},
		{
			name: "mailer-daemon bounce",
			in:   replyclassify.Input{Headers: map[string][]string{"From": {"MAILER-DAEMON@mx.example.com"}}, Subject: "Returned mail"},
			want: models.ReplyIntentAutomated,
		},
		{
			name:  "a person writing back",
			in:    replyclassify.Input{Subject: "Re: Quick question", BodyText: "Sounds interesting, can you send pricing?"},
			human: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := replyclassify.ClassifyOffline(tc.in)
			if tc.human {
				if replyclassify.IsAutomated(res.Class) {
					t.Fatalf("a human reply was classified %q", res.Class)
				}
				return
			}
			if !replyclassify.IsAutomated(res.Class) {
				t.Fatalf("classified %q, want an automated class", res.Class)
			}
			intent, _ := automatedIntent(res)
			if intent != tc.want {
				t.Fatalf("intent %q, want %q", intent, tc.want)
			}
			if settings.CreatesTaskFor(intent) {
				t.Errorf("%q opened a follow-up task on default settings", intent)
			}
		})
	}
}

// The wording is what a person reads on the Tasks page, so it stays plain
// rather than echoing the classifier's vocabulary.
func TestReplyTaskTitle(t *testing.T) {
	for _, tc := range []struct {
		intent models.ReplyIntentType
		want   string
	}{
		{models.ReplyIntentPositive, "Follow up: positive reply from a@b.com"},
		{models.ReplyIntentNeutral, "Follow up: reply from a@b.com"},
		{models.ReplyIntentOutOfOffice, "Follow up: out-of-office reply from a@b.com"},
		{models.ReplyIntentAutomated, "Follow up: automatic reply from a@b.com"},
	} {
		if got := replyTaskTitle(tc.intent, "a@b.com"); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.intent, got, tc.want)
		}
	}
}
