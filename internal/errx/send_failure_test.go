package errx

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestRetryAfterAt(t *testing.T) {
	now := time.Date(2026, 10, 7, 11, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		value string
		want  time.Duration
		valid bool
	}{
		{"90", 90 * time.Second, true}, {" 0 ", 0, true},
		{now.Add(time.Hour).Format(http.TimeFormat), time.Hour, true},
		{now.Add(-time.Hour).Format(http.TimeFormat), 0, true},
		{"-1", 0, false}, {"9223372036854775807", 0, false}, {"soon", 0, false}, {"", 0, false},
	} {
		t.Run(tc.value, func(t *testing.T) {
			got := RetryAfterAt(tc.value, now)
			if (got != nil) != tc.valid || got != nil && got.Sub(now) != tc.want {
				t.Fatalf("RetryAfterAt(%q) = %v", tc.value, got)
			}
		})
	}
}

func TestSendFailureSafeEvidenceDoesNotMutateSharedError(t *testing.T) {
	now := time.Now().UTC()
	retry := now.Add(time.Minute)
	got := WithSendFailure(ErrMailSendingTooFast, SendFailure{Disposition: SendThrottle, ObservedAt: now, RetryAt: &retry})
	if got.RetryAfter != time.Minute || got.Failure == nil || ErrMailSendingTooFast.Failure != nil {
		t.Fatal("send metadata must not mutate singleton errors")
	}
	for _, code := range []string{"userRateLimitExceeded", "ErrorAccessDenied", "5.7.26"} {
		if SafeProviderCode(code) != code {
			t.Fatalf("safe identifier %q removed", code)
		}
	}
	for _, code := range []string{"user@example.test", "Bearer sensitive", "<html>secret</html>", strings.Repeat("x", 97)} {
		if SafeProviderCode(code) != "" {
			t.Fatalf("unsafe code %q retained", code)
		}
	}
}
