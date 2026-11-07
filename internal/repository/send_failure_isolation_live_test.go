package repository

import (
	"context"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func TestLiveSendFailureKeepsRecipientAndMailboxRecoverySeparate(t *testing.T) {
	for _, tc := range []struct {
		name, disposition, scope, protocol, stage string
		hold, cooldown                            bool
	}{
		{name: "recipient rejection", disposition: errx.SendPermanent, scope: "recipient"},
		{name: "preparation failure", disposition: errx.SendRetry, scope: "mailbox", protocol: "internal", stage: "prepare"},
		{name: "temporary transport failure", disposition: errx.SendRetry, scope: "mailbox", cooldown: true},
		{name: "provider throttle", disposition: errx.SendThrottle, scope: "mailbox", cooldown: true},
		{name: "authentication refusal", disposition: errx.SendAuth, scope: "mailbox", hold: true},
		{name: "mailbox restriction", disposition: errx.SendPermanent, scope: "mailbox", hold: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newCampaignIsolationFixture(t)
			result := models.SendEmailResult{TaskID: f.request.TaskID, Error: &models.EmailSendError{
				Failure: &errx.SendFailure{Disposition: tc.disposition, Scope: tc.scope, Protocol: tc.protocol, Stage: tc.stage, ObservedAt: time.Now().UTC()},
			}}
			if err := f.r.FinishOutbound(t.Context(), result.TaskID, f.sender, f.worker, result); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if err := f.r.ApplySendResult(t.Context(), result, func(context.Context) error { return nil }); err != nil {
					t.Fatal(err)
				}
			}
			admission, err := f.r.GetSendAdmission(t.Context(), f.org, f.sender, models.InboxProviderSMTPIMAP, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			cooling := admission.RetryAt != nil && admission.RetryAt.After(time.Now())
			if admission.RecoveryHold != tc.hold || cooling != tc.cooldown || admission.Allowed != (!tc.hold && !tc.cooldown) {
				t.Fatalf("incorrect failure scope: %+v", admission)
			}
			if tc.cooldown {
				if admission.RetryAt.After(time.Now().Add(6 * time.Minute)) {
					t.Fatalf("temporary failure produced an unbounded pause: %v", admission.RetryAt)
				}
				admission, err = f.r.GetSendAdmission(t.Context(), f.org, f.sender, models.InboxProviderSMTPIMAP, admission.RetryAt.Add(time.Second))
				if err != nil || !admission.Allowed || admission.RecoveryHold {
					t.Fatalf("temporary failure needs manual recovery: %+v %v", admission, err)
				}
			}
			if !tc.hold && !tc.cooldown {
				next, _ := f.campaignAdmissionFixture.request(t, f.sender)
				nonce, err := f.r.ReserveOutbound(t.Context(), next)
				if err != nil {
					t.Fatalf("one failed email blocked another contact: %v", err)
				}
				state, err := f.r.BeginOutbound(t.Context(), next.TaskID, f.sender, f.worker, nonce)
				if err != nil || state.State != "execute" {
					t.Fatalf("unrelated contact cannot send: %+v %v", state, err)
				}
			}
		})
	}
}

func TestLiveServerPacingHoldWaitsForItsRetryTime(t *testing.T) {
	f := newCampaignIsolationFixture(t)
	retryAt := time.Now().UTC().Add(9 * time.Minute)
	result := models.SendEmailResult{TaskID: f.request.TaskID, Error: &models.EmailSendError{
		Code: string(errx.MailErrorCodeServerUnreachable), Message: errx.MailServerPacedMessage,
		Failure: &errx.SendFailure{Provider: "smtp", Protocol: "smtp", Stage: "pacing", Cause: errx.SendCauseServerPacing,
			Disposition: errx.SendRetry, Scope: "mailbox", ObservedAt: time.Now().UTC(), RetryAt: &retryAt},
	}}
	if err := f.r.FinishOutbound(t.Context(), result.TaskID, f.sender, f.worker, result); err != nil {
		t.Fatal(err)
	}
	if err := f.r.ApplySendResult(t.Context(), result, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	early, err := f.r.GetSendAdmission(t.Context(), f.org, f.sender, models.InboxProviderSMTPIMAP, retryAt.Add(-time.Minute))
	if err != nil || early.Allowed || early.RecoveryHold || early.RetryAt == nil || early.RetryAt.Before(retryAt.Add(-time.Second)) {
		t.Fatalf("held send admitted before the server's retry time: %+v %v", early, err)
	}
	late, err := f.r.GetSendAdmission(t.Context(), f.org, f.sender, models.InboxProviderSMTPIMAP, retryAt.Add(time.Second))
	if err != nil || !late.Allowed || late.RecoveryHold {
		t.Fatalf("held send not admitted after the retry time: %+v %v", late, err)
	}
}
