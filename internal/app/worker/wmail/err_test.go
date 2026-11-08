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
		errx.ErrMailResourceNotFound,
	} {
		if reportable(mailErr) {
			t.Errorf("%s was reportable; the worker retries this expected provider condition", mailErr.Code)
		}
	}
}

// A non-JSON Graph success body retries and is not an application error.
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

// A refused password or grant reaches its owner through the relay and stops the mailbox; it is not an application error.
func TestCredentialRefusalsRelayOnceWithoutReport(t *testing.T) {
	for _, mailErr := range []*errx.MailError{
		errx.ErrMailGoogleAuth,
		errx.ErrMailInvalidCredentials,
		errx.ErrMailWrongServer("[ALERT] The server details you are using seems incorrect."),
	} {
		if reportable(mailErr) {
			t.Errorf("%s was reportable; the owner is told through the relay", mailErr.Code)
		}
		var kinds []models.JobEventType
		w := &WMail{onEvent: func(kind models.JobEventType, _ any) error {
			kinds = append(kinds, kind)
			return nil
		}}
		w.captureSyncError(mailErr)
		w.captureSyncError(mailErr)
		if len(kinds) != 1 || kinds[0] != models.JobEventTypeEmailAuthError {
			t.Errorf("%s relayed %v, want one %s and the sync stopped", mailErr.Code, kinds, models.JobEventTypeEmailAuthError)
		}
	}
	if !reportable(errx.ErrMailAuthenticationFailed) {
		t.Error("an OAuth failure our own client could cause must stay reportable")
	}
}
