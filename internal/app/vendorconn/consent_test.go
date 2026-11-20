package vendorconn

import (
	"strings"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/pkg/mailvendor"
)

func TestConsentStepWaitsThenEnds(t *testing.T) {
	start := time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)
	st := consentState{RequestID: "ws:1", Started: start, Polled: start, Stage: "queued"}

	if st.duePoll(start.Add(time.Minute)) || !st.duePoll(start.Add(consentPollEvery)) {
		t.Fatal("status polled off its spacing")
	}
	if (consentState{Started: start}).duePoll(start.Add(time.Hour)) {
		t.Fatal("polled a request the vendor never named")
	}

	next, ended, _ := consentStep(st, &mailvendor.AuthorizationStatus{State: mailvendor.AuthorizationPending, Stage: "processing"}, start.Add(10*time.Minute))
	if ended || next.Stage != "processing" || !next.Polled.Equal(start.Add(10*time.Minute)) {
		t.Fatalf("pending step = %+v, %v", next, ended)
	}
	if _, ended, reason := consentStep(st, &mailvendor.AuthorizationStatus{State: mailvendor.AuthorizationFailed, Reason: "Login challenge"}, start.Add(time.Minute)); !ended || reason != "Login challenge" {
		t.Fatalf("failed step = %v %q", ended, reason)
	}

	done, ended, _ := consentStep(st, &mailvendor.AuthorizationStatus{State: mailvendor.AuthorizationCompleted}, start.Add(time.Minute))
	if ended || done.Completed.IsZero() {
		t.Fatalf("a completed request ended before Google could return: %+v", done)
	}
	if _, ended, reason := consentStep(done, nil, done.Completed.Add(authSettle+time.Second)); !ended || !strings.Contains(reason, "never returned") {
		t.Fatalf("a completed request with no callback = %v %q", ended, reason)
	}
	if _, ended, reason := consentStep(consentState{Started: start}, nil, start.Add(consentMaxWait+time.Second)); !ended || !strings.Contains(reason, "six hours") {
		t.Fatalf("an unnamed request past its wait = %v %q", ended, reason)
	}
}
