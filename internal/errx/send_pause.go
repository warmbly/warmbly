package errx

import (
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/config"
)

// SendPause is what a refused send does to its mailbox's sending, read from the worker's failure.
type SendPause string

const (
	SendPauseNone SendPause = ""
	// SendPauseRetry is a transient mailbox-level failure that is not a throttle.
	SendPauseRetry SendPause = "retry"
	// SendPauseRateLimited is the provider asking this mailbox to slow down.
	SendPauseRateLimited SendPause = "rate_limited"
	// SendPauseDailyLimit is the provider's sending quota for this mailbox spent.
	SendPauseDailyLimit SendPause = "daily_limit"
	// SendPauseSenderBlocked is the provider restricting this account from sending.
	SendPauseSenderBlocked SendPause = "sender_blocked"
	// SendPauseDomainAuth is a receiving server refusing the sending domain's SPF, DKIM or DMARC.
	SendPauseDomainAuth     SendPause = "domain_auth"
	SendPauseAuthentication SendPause = "authentication"
	SendPausePermanent      SendPause = "permanent"
)

// Causes the SMTP client reads out of a reply's text, carried in SendFailure.Cause.
const (
	SendCauseSendingLimit  = "sending_limit"
	SendCauseSenderBlocked = "sender_blocked"
	SendCauseRateLimited   = "rate_limited"
)

// Timed pauses lift on their own at a computed time.
func (k SendPause) Timed() bool {
	switch k {
	case SendPauseRetry, SendPauseRateLimited, SendPauseDailyLimit, SendPauseSenderBlocked:
		return true
	}
	return false
}

// Held pauses keep the mailbox from sending until resolved or re-probed.
func (k SendPause) Held() bool {
	switch k {
	case SendPauseAuthentication, SendPauseDomainAuth, SendPausePermanent:
		return true
	}
	return false
}

// SparesLead is a refusal of the sender, so the lead's step is retried without spending an attempt; an SMTP throttle after the recipients were offered still counts.
func (k SendPause) SparesLead(f *SendFailure) bool {
	switch k {
	case SendPauseDailyLimit, SendPauseSenderBlocked:
		return true
	case SendPauseRateLimited:
		return f == nil || f.Protocol != "smtp"
	}
	return false
}

// Provider error codes, lowercased, that name a pause on their own.
var sendPauseCauses = map[string]SendPause{
	SendCauseSendingLimit:           SendPauseDailyLimit,
	SendCauseSenderBlocked:          SendPauseSenderBlocked,
	SendCauseRateLimited:            SendPauseRateLimited,
	"dailylimitexceeded":            SendPauseDailyLimit,
	"ratelimitexceeded":             SendPauseRateLimited,
	"userratelimitexceeded":         SendPauseRateLimited,
	"quotaexceeded":                 SendPauseRateLimited,
	"errorexceededmessagelimit":     SendPauseDailyLimit,
	"errorsubmissionquotaexceeded":  SendPauseDailyLimit,
	"submissionquotaexceeded":       SendPauseDailyLimit,
	"mailboxsendquotaexceeded":      SendPauseDailyLimit,
	"errormessagesubmissionblocked": SendPauseSenderBlocked,
	"applicationthrottled":          SendPauseRateLimited,
	"mailboxconcurrency":            SendPauseRateLimited,
	"toomanyrequests":               SendPauseRateLimited,
}

// Enhanced status codes that blame the sending account wherever in the conversation they arrive.
var sendPauseStatuses = map[string]SendPause{
	"5.4.5":   SendPauseDailyLimit,    // Google: daily user sending limit exceeded
	"5.4.6":   SendPauseSenderBlocked, // Zoho: unusual sending activity
	"5.7.708": SendPauseSenderBlocked, // Microsoft: traffic not accepted from this tenant
	"5.7.705": SendPauseSenderBlocked, // Microsoft: tenant exceeded its threshold
	"5.7.501": SendPauseSenderBlocked, // Microsoft: spam abuse detected
	"5.7.502": SendPauseSenderBlocked,
	"5.7.503": SendPauseSenderBlocked,
	"5.7.750": SendPauseSenderBlocked, // Microsoft: unregistered sending domain
	"5.1.8":   SendPauseSenderBlocked, // Microsoft: bad outbound sender
	"4.7.28":  SendPauseRateLimited,   // Microsoft: rate limited
}

// SenderPauseStatus reports an enhanced status that blames the sending account.
func SenderPauseStatus(enhanced string) bool {
	_, ok := sendPauseStatuses[enhanced]
	return ok
}

// ClassifySendPause reads one worker failure as a pause on its mailbox, from fields every release has sent.
func ClassifySendPause(code string, f *SendFailure) SendPause {
	if MailErrorCode(code) == MailErrorCodeDomainAuthRejected {
		return SendPauseDomainAuth
	}
	if f == nil {
		switch MailErrorCode(code) {
		case MailErrorCodeSendingTooFast, MailErrorCodeQuotaExceeded:
			return SendPauseRateLimited
		}
		return SendPauseNone
	}
	if f.Disposition == SendAmbiguous || f.Protocol == "internal" && f.Stage == "prepare" {
		return SendPauseNone
	}
	kind, named := sendPauseCauses[strings.ToLower(f.Cause)]
	if !named {
		kind, named = sendPauseStatuses[f.EnhancedStatus]
	}
	if !named {
		kind = dispositionPause(f)
	}
	// A rate limit whose stated reset is hours out is a spent quota.
	if kind == SendPauseRateLimited && f.RetryAt != nil && !f.ObservedAt.IsZero() && f.RetryAt.Sub(f.ObservedAt) >= time.Hour {
		return SendPauseDailyLimit
	}
	return kind
}

func dispositionPause(f *SendFailure) SendPause {
	switch f.Disposition {
	case SendAuth:
		return SendPauseAuthentication
	case SendThrottle:
		return SendPauseRateLimited
	case SendRetry:
		// 421 closes the session, so it is the server's answer whatever command it followed.
		throttled := f.Status == 421 || f.Status == 454 || f.Cause == SendCauseServerPacing
		if f.Scope == "recipient" && !throttled {
			return SendPauseNone
		}
		if throttled || f.Status == 429 || strings.HasPrefix(f.EnhancedStatus, "4.7.") {
			return SendPauseRateLimited
		}
		return SendPauseRetry
	case SendPermanent:
		if f.Scope == "recipient" {
			return SendPauseNone
		}
		return SendPausePermanent
	}
	return SendPauseNone
}

// SendPauseState is a mailbox's current timed pause, as stored.
type SendPauseState struct {
	Kind    SendPause
	Until   *time.Time
	Strikes int
}

// NextSendPause is the timed pause a refusal of kind leaves, given the one already stored; one burst of refusals is one strike.
func NextSendPause(kind SendPause, prev SendPauseState, retryAt *time.Time, observed time.Time) SendPauseState {
	strikes := 1
	if prev.Until != nil && prev.Strikes > 0 && observed.Sub(*prev.Until) < config.SendPauseStrikeResetAfter {
		strikes = prev.Strikes
		if !observed.Before(*prev.Until) {
			strikes++
		}
	}
	var wait time.Duration
	switch kind {
	case SendPauseRateLimited:
		wait = doubled(config.SendRateLimitPause, strikes, config.SendRateLimitPauseMax)
	case SendPauseDailyLimit:
		wait = config.SendDailyLimitPause
	case SendPauseSenderBlocked:
		wait = doubled(config.SendBlockedPause, strikes, config.SendBlockedPauseMax)
	default:
		wait = config.SendRateLimitPause
	}
	until := observed.Add(wait)
	if retryAt != nil && retryAt.After(observed) {
		stated := *retryAt
		if ceiling := observed.Add(config.SendPauseMax); stated.After(ceiling) {
			stated = ceiling
		}
		switch kind {
		case SendPauseDailyLimit, SendPauseSenderBlocked:
			// Only a reset hours out is the provider's quota clock; a short one is pacing.
			if stated.Sub(observed) >= time.Hour {
				until = stated
			}
		case SendPauseRateLimited:
			// The provider's wait on the first throttle, never less than the escalation after.
			if strikes == 1 || stated.After(until) {
				until = stated
			}
		default:
			until = stated
		}
	}
	if prev.Until != nil && prev.Until.After(until) {
		if prev.Kind != SendPauseNone {
			kind = prev.Kind
		}
		return SendPauseState{Kind: kind, Until: prev.Until, Strikes: strikes}
	}
	return SendPauseState{Kind: kind, Until: &until, Strikes: strikes}
}

// NextHoldProbe is when a held mailbox is released for one more try after probes earlier refusals.
func NextHoldProbe(probes int, observed time.Time) time.Time {
	return observed.Add(doubled(config.SendHoldProbe, probes, config.SendHoldProbeMax))
}

func doubled(base time.Duration, strikes int, ceiling time.Duration) time.Duration {
	wait := base << min(max(strikes-1, 0), 10)
	return min(wait, ceiling)
}
