package wmail

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/client/msgraph"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func TestMailTransportErrorScopesAndWorkerObservation(t *testing.T) {
	observed := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	err := errx.WithSendFailure(errx.ErrMailServerUnreachable, errx.SendFailure{ObservedAt: observed})
	w := &WMail{}
	var events []models.EmailErrorEvent
	w.onEvent = func(_ models.JobEventType, body any) error {
		events = append(events, body.(models.EmailErrorEvent))
		return nil
	}
	w.CaptureError(err)
	w.captureSyncError(err)
	if len(events) != 2 || events[0].Operation != models.MailTransportUnknown || events[1].Operation != models.MailTransportSync {
		t.Fatalf("general error was inferred sync-only: %+v", events)
	}
	for _, event := range events {
		if event.Timestamp != observed.Unix() || event.TaskID != "" {
			t.Fatalf("worker clock or context changed: %+v", event)
		}
	}
}

func TestTransientProviderFailuresAreNotReportedAsApplicationErrors(t *testing.T) {
	for _, mailErr := range []*errx.MailError{
		errx.ErrMailServerUnreachable,
		errx.MError(errx.MailErrorWarning, errx.MailErrorCodeConnectionLost, "connection closed", errx.MailErrorResolveMethodRetry),
		errx.ErrMailSendingTooFast,
		errx.ErrMailQuotaExceeded,
	} {
		if reportable(mailErr) {
			t.Errorf("%s was reportable; the worker retries this expected provider condition", mailErr.Code)
		}
	}
}

// A Graph success body that is not JSON retries on the next pass and is logged
// by the client; it is not an application error.
func TestUnreadableGraphResponseIsNotReported(t *testing.T) {
	if reportable(fmt.Errorf("%w: %w", msgraph.ErrUnreadableResponse, errors.New("invalid character"))) {
		t.Error("an unreadable Graph response was reportable")
	}
}

func TestOnlyWarmblyRateLimitDeactivatesMailbox(t *testing.T) {
	tests := []struct {
		name string
		err  *errx.MailError
		want models.JobEventType
	}{
		{name: "anti abuse limit", err: errx.ErrMailRateLimitExceeded, want: models.JobEventTypeEmailRateLimited},
		{name: "provider throttle", err: errx.ErrMailSendingTooFast, want: models.JobEventTypeEmailFailed},
		{name: "provider quota", err: errx.ErrMailQuotaExceeded, want: models.JobEventTypeEmailFailed},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := DetermineErrorEventType(tc.err); got != tc.want {
				t.Fatalf("event = %s, want %s", got, tc.want)
			}
		})
	}
}
