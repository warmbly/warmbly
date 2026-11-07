package worker

import (
	"testing"

	"github.com/warmbly/warmbly/internal/app/worker/wmail"
	"github.com/warmbly/warmbly/internal/errx"
)

// Only a recipient refusal that names the address counts as a hard bounce.
func TestRecipientRefusalCountsHardOnlyWhenTheAddressFailed(t *testing.T) {
	for _, tc := range []struct {
		name, reply string
		hard        bool
	}{
		{"user unknown", "550 5.1.1 User unknown", true},
		{"bare refusal", "The SMTP server refused this recipient (SMTP rcpt to status 550)", true},
		{"mailbox full", "552 5.2.2 Mailbox full", false},
		{"policy block", "550 5.7.1 Client host blocked using zen.spamhaus.org", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &WorkerService{}
			err := errx.ErrMailRecipientRejected("refused")
			err.Message = tc.reply
			s.recordSendOutcome(&wmail.SendResult{Error: err})
			w := s.HealthCounters.snapshotWindow()
			if got := w.bouncesHard == 1; got != tc.hard || w.bouncesHard+w.bouncesSoft != 1 {
				t.Fatalf("hard=%d soft=%d, want hard=%v", w.bouncesHard, w.bouncesSoft, tc.hard)
			}
		})
	}
}
