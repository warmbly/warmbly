package goog

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/errx"
	"google.golang.org/api/googleapi"
)

func TestNativeSendFailureGmail(t *testing.T) {
	for _, tc := range []struct {
		status              int
		reason, disposition string
	}{
		{401, "authError", errx.SendAuth}, {403, "insufficientPermissions", errx.SendAuth},
		{403, "userRateLimitExceeded", errx.SendThrottle}, {403, "dailyLimitExceeded", errx.SendThrottle},
		{429, "rateLimitExceeded", errx.SendThrottle}, {503, "backendError", errx.SendRetry},
		{400, "invalidArgument", errx.SendPermanent},
	} {
		t.Run(fmt.Sprintf("%d/%s", tc.status, tc.reason), func(t *testing.T) {
			native := &googleapi.Error{Code: tc.status, Message: "private body", Header: http.Header{"Retry-After": {"90"}}, Errors: []googleapi.ErrorItem{{Reason: tc.reason}}}
			got := HandleError(fmt.Errorf("wrapped: %w", native))
			f := got.Failure
			if f == nil || f.Provider != "google" || f.Status != tc.status || f.Cause != tc.reason || f.Scope != "mailbox" || f.Disposition != tc.disposition || got.RetryAfter != 90*time.Second {
				t.Fatalf("error = %+v, failure = %+v", got, f)
			}
			if got.Message == native.Message {
				t.Fatal("raw provider body retained")
			}
		})
	}
}
