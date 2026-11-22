package tasks

import (
	"errors"
	"fmt"
	"testing"

	"github.com/warmbly/warmbly/internal/repository"
)

func TestCampaignDispatchFailureCode(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{
		{repository.ErrCampaignDailyLimit, "CAMPAIGN_DAILY_LIMIT_REACHED"},
		{repository.ErrMailboxSendingPlan, "MAILBOX_SENDING_PLAN_DENIED"},
		{repository.ErrSendAdmissionDenied, "SEND_ADMISSION_DENIED"},
		{ErrWorkerOffline, "WORKER_UNAVAILABLE"},
		{ErrWorkerUnconfirmed, "WORKER_UNAVAILABLE"},
		{ErrSendDispatchUnknown, "SEND_DISPATCH_UNKNOWN"},
		{errors.New("publish failed"), "SEND_FAILED"},
	} {
		t.Run(tc.code+"/"+tc.err.Error(), func(t *testing.T) {
			if got := campaignDispatchFailureCode(fmt.Errorf("dispatch: %w", tc.err)); got != tc.code {
				t.Fatalf("got %s, want %s", got, tc.code)
			}
		})
	}
}

// End-to-end checks for issue #306 over the real campaign tick: the chain's
// own wake-ups (a deferral, a pause) must not spend the mailbox's daily
// budget, and a campaign whose mailboxes are all at their cap waits for
// tomorrow instead of pausing. Same harness and env var as
// campaign_send_live_test.go.
