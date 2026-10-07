package wmail

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/client/goog"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"golang.org/x/oauth2"
)

type refusingSendRT struct {
	status int
	calls  int
}

func (r *refusingSendRT) RoundTrip(req *http.Request) (*http.Response, error) {
	r.calls++
	if r.status == 0 {
		return nil, errors.New("connection closed after submission")
	}
	return &http.Response{StatusCode: r.status, Header: http.Header{"Content-Type": {"application/json"}, "Retry-After": {"120"}}, Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"error":{"code":%d,"message":"private body"}}`, r.status))), Request: req}, nil
}

func TestSendNativeFailuresAreNotRetriedAndSurviveTransport(t *testing.T) {
	for _, status := range []int{0, 429, 503} {
		rt := &refusingSendRT{status: status}
		ctx := context.WithValue(t.Context(), oauth2.HTTPClient, &http.Client{Transport: rt})
		c := &goog.Client{Email: "sender@example.test"}
		if err := c.InitWithSource(ctx, oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "fixture-token"})); err != nil {
			t.Fatal(err)
		}
		m := &WMail{EmailType: models.InboxProviderGoogle, GoogleData: &GoogleData{Client: c}}
		result := m.Send(ctx, &SendRequest{To: []string{"receiver@example.test"}, Subject: "Diagnostic", BodyPlain: "Diagnostic transport fixture", MessageID: "<fixture@example.test>"})
		if result.Success || result.Error == nil || result.Error.Failure == nil || rt.calls != 1 {
			t.Fatalf("result = %+v; calls = %d", result, rt.calls)
		}
		want := errx.SendAmbiguous
		if status == 429 {
			want = errx.SendThrottle
		}
		f := MailErrorToSendError(result.Error).Failure
		if f.Disposition != want || f.Stage != "submit" || f.Provider != "google" {
			t.Fatalf("failure = %+v", f)
		}
		wantEvent := models.JobEventTypeEmailServerError
		if status == 429 {
			wantEvent = models.JobEventTypeEmailRateLimited
		}
		if got := DetermineErrorEventType(result.Error); got != wantEvent {
			t.Fatalf("event=%s want=%s", got, wantEvent)
		}
		if status != 0 && result.Error.RetryAfter != 120*time.Second {
			t.Fatalf("RetryAfter = %s", result.Error.RetryAfter)
		}
	}
}

func TestLegacyThrottleKeepsAvailableRetryAfterWithoutInventingProtocol(t *testing.T) {
	for _, code := range []errx.MailErrorCode{errx.MailErrorCodeSendingTooFast, errx.MailErrorCodeQuotaExceeded} {
		source := errx.MError(errx.MailErrorWarning, code, "fixture", errx.MailErrorResolveMethodRetry)
		source.RetryAfter = time.Hour
		got := MailErrorToSendError(source)
		if got.Failure == nil || got.Failure.RetryAt == nil || got.Failure.RetryAt.Sub(got.Failure.ObservedAt) != time.Hour || got.Failure.Disposition != errx.SendThrottle || got.Failure.Scope != "mailbox" || got.Failure.Protocol != "" || got.Failure.Provider != "" || got.Failure.Status != 0 || source.Failure != nil {
			t.Fatal("legacy guidance lost or native evidence invented")
		}
	}
}
