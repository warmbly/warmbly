package errx

import (
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/config"
)

func TestClassifySendPauseReadsAPIProviderCodes(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	soon, later := now.Add(30*time.Second), now.Add(9*time.Hour)
	for _, tc := range []struct {
		name string
		f    SendFailure
		want SendPause
	}{
		{"gmail per-minute quota", SendFailure{Provider: "google", Status: 429, Cause: "rateLimitExceeded", Disposition: SendThrottle, ObservedAt: now, RetryAt: &soon}, SendPauseRateLimited},
		{"gmail mail sending limit", SendFailure{Provider: "google", Status: 429, Cause: "userRateLimitExceeded", Disposition: SendThrottle, ObservedAt: now, RetryAt: &later}, SendPauseDailyLimit},
		{"gmail daily limit", SendFailure{Provider: "google", Status: 403, Cause: "dailyLimitExceeded", Disposition: SendThrottle, ObservedAt: now}, SendPauseDailyLimit},
		{"gmail revoked grant", SendFailure{Provider: "google", Status: 401, Disposition: SendAuth, ObservedAt: now}, SendPauseAuthentication},
		{"graph throttle", SendFailure{Provider: "microsoft", Status: 429, Cause: "ApplicationThrottled", Disposition: SendThrottle, ObservedAt: now}, SendPauseRateLimited},
		{"graph message limit", SendFailure{Provider: "microsoft", Status: 400, Cause: "ErrorExceededMessageLimit", Disposition: SendThrottle, ObservedAt: now}, SendPauseDailyLimit},
		{"graph submission blocked", SendFailure{Provider: "microsoft", Status: 403, Cause: "ErrorMessageSubmissionBlocked", Disposition: SendPermanent, ObservedAt: now}, SendPauseSenderBlocked},
		{"graph send as denied", SendFailure{Provider: "microsoft", Status: 403, Cause: "ErrorSendAsDenied", Disposition: SendAuth, ObservedAt: now}, SendPauseAuthentication},
		{"graph unavailable", SendFailure{Provider: "microsoft", Status: 503, Disposition: SendRetry, ObservedAt: now}, SendPauseRetry},
		{"ambiguous", SendFailure{Provider: "smtp", Disposition: SendAmbiguous}, SendPauseNone},
		{"worker had not loaded the mailbox", SendFailure{Protocol: "internal", Stage: "prepare", Disposition: SendRetry}, SendPauseNone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifySendPause("", &tc.f); got != tc.want {
				t.Fatalf("pause = %q, want %q", got, tc.want)
			}
		})
	}
	if got := ClassifySendPause(string(MailErrorCodeSendingTooFast), nil); got != SendPauseRateLimited {
		t.Fatalf("legacy throttle = %q", got)
	}
}

func TestNextSendPauseEscalatesOncePerBurst(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	first := NextSendPause(SendPauseRateLimited, SendPauseState{}, nil, now)
	if first.Strikes != 1 || !first.Until.Equal(now.Add(config.SendRateLimitPause)) {
		t.Fatalf("first = %+v", first)
	}
	// A refusal from a send already in flight is the same burst.
	same := NextSendPause(SendPauseRateLimited, first, nil, now.Add(time.Minute))
	if same.Strikes != 1 {
		t.Fatalf("in-flight refusal counted as a new strike: %+v", same)
	}
	second := NextSendPause(SendPauseRateLimited, first, nil, first.Until.Add(time.Minute))
	if second.Strikes != 2 || second.Until.Sub(*first.Until) < 2*config.SendRateLimitPause {
		t.Fatalf("second = %+v", second)
	}
	capped := SendPauseState{Kind: SendPauseRateLimited, Until: &now, Strikes: 9}
	if got := NextSendPause(SendPauseRateLimited, capped, nil, now); got.Until.Sub(now) != config.SendRateLimitPauseMax {
		t.Fatalf("uncapped escalation: %v", got.Until.Sub(now))
	}
	stale := now.Add(-2 * config.SendPauseStrikeResetAfter)
	if got := NextSendPause(SendPauseRateLimited, SendPauseState{Kind: SendPauseRateLimited, Until: &stale, Strikes: 5}, nil, now); got.Strikes != 1 {
		t.Fatalf("old throttles were not forgotten: %+v", got)
	}
}

func TestNextSendPauseHonoursTheProviderReset(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	if got := NextSendPause(SendPauseDailyLimit, SendPauseState{}, nil, now); !got.Until.Equal(now.Add(config.SendDailyLimitPause)) {
		t.Fatalf("daily limit without a reset = %v", got.Until)
	}
	reset := now.Add(3 * time.Hour)
	if got := NextSendPause(SendPauseDailyLimit, SendPauseState{}, &reset, now); !got.Until.Equal(reset) {
		t.Fatalf("provider reset ignored: %v", got.Until)
	}
	far := now.Add(30 * 24 * time.Hour)
	if got := NextSendPause(SendPauseRateLimited, SendPauseState{}, &far, now); !got.Until.Equal(now.Add(config.SendPauseMax)) {
		t.Fatalf("provider reset not capped: %v", got.Until)
	}
	// A shorter pause never cuts a longer one short, and keeps its reason.
	long := now.Add(20 * time.Hour)
	got := NextSendPause(SendPauseRetry, SendPauseState{Kind: SendPauseDailyLimit, Until: &long, Strikes: 1}, nil, now)
	if got.Kind != SendPauseDailyLimit || !got.Until.Equal(long) {
		t.Fatalf("got %+v", got)
	}
}

func TestNextHoldProbeBacksOff(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	if got := NextHoldProbe(1, now).Sub(now); got != config.SendHoldProbe {
		t.Fatalf("first probe after %v", got)
	}
	if got := NextHoldProbe(2, now).Sub(now); got != 2*config.SendHoldProbe {
		t.Fatalf("second probe after %v", got)
	}
	if got := NextHoldProbe(20, now).Sub(now); got != config.SendHoldProbeMax {
		t.Fatalf("probe backoff uncapped: %v", got)
	}
}
