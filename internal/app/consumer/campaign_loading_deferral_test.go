package jobs

import (
	"testing"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func TestMailboxLoadingRequiresPreparationEvidence(t *testing.T) {
	for _, tc := range []struct {
		name    string
		code    string
		failure *errx.SendFailure
		loading bool
	}{
		{name: "missing protocol evidence"},
		{name: "preparation", failure: &errx.SendFailure{Protocol: "internal", Stage: "prepare", Disposition: errx.SendRetry}, loading: true},
		{name: "provider retry", failure: &errx.SendFailure{Protocol: "smtp", Stage: "connect", Disposition: errx.SendRetry}},
		{name: "ambiguous", failure: &errx.SendFailure{Protocol: "internal", Stage: "prepare", Disposition: errx.SendAmbiguous}},
		{name: "authentication", code: string(errx.MailErrorCodeInvalidCredentials)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, code := sendFailureReason(models.SendEmailResult{Error: &models.EmailSendError{Code: tc.code, Message: models.MailboxNotLoadedPrefix, Failure: tc.failure}})
			if (code == models.MailboxLoadingCode) != tc.loading {
				t.Fatalf("code=%s, loading=%t", code, tc.loading)
			}
		})
	}
}

func TestServerPacingNeedsAnSMTPRefusalBeforeTheRecipient(t *testing.T) {
	unreachable := string(errx.MailErrorCodeServerUnreachable)
	for _, tc := range []struct {
		name    string
		code    string
		failure *errx.SendFailure
		pacing  bool
	}{
		{name: "held on the worker", code: unreachable, failure: &errx.SendFailure{Protocol: "smtp", Stage: "pacing", Cause: errx.SendCauseServerPacing, Disposition: errx.SendRetry}, pacing: true},
		{name: "refused at sign-in", code: unreachable, failure: &errx.SendFailure{Protocol: "smtp", Stage: "auth", Status: 454, Cause: errx.SendCauseServerPacing, Disposition: errx.SendRetry}, pacing: true},
		{name: "plain connection failure", code: unreachable, failure: &errx.SendFailure{Protocol: "smtp", Stage: "starttls", Disposition: errx.SendRetry}},
		{name: "other protocol", code: unreachable, failure: &errx.SendFailure{Protocol: "graph", Cause: errx.SendCauseServerPacing, Disposition: errx.SendRetry}},
		{name: "not a retry", code: unreachable, failure: &errx.SendFailure{Protocol: "smtp", Cause: errx.SendCauseServerPacing, Disposition: errx.SendPermanent}},
		{name: "other code", code: string(errx.MailErrorCodeInvalidCredentials), failure: &errx.SendFailure{Protocol: "smtp", Cause: errx.SendCauseServerPacing, Disposition: errx.SendRetry}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, code := sendFailureReason(models.SendEmailResult{Error: &models.EmailSendError{Code: tc.code, Message: "refused", Failure: tc.failure}})
			if (code == models.ServerPacingCode) != tc.pacing {
				t.Fatalf("code=%s, pacing=%t", code, tc.pacing)
			}
		})
	}
}
