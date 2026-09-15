package goog

import (
	"errors"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/pkg/mailauth"
	"google.golang.org/api/googleapi"
)

// IsThreadRefusal reports a send Gmail rejected because of the threadId it was
// given rather than because of the message. Gmail only files a message in a
// thread when the subject and the reference headers line up with it, and the
// thread has to still exist in this mailbox. The send provably did not happen,
// so the caller can safely try again without the thread handle and deliver the
// email as its own conversation instead of failing the step.
//
// Only ask this about a send that carried a threadId.
func IsThreadRefusal(err error) bool {
	if err == nil {
		return false
	}
	var gerr *googleapi.Error
	if !errors.As(err, &gerr) {
		return false
	}
	// Beyond the message it is sending, the only entity the request names is
	// the thread, so a 404 is about the thread whatever it says. Gmail's
	// wording for a thread that is gone is the generic "Requested entity was
	// not found", which is why this cannot be a match on the word.
	if gerr.Code == 404 {
		return true
	}
	// A 400 usually is about the message (a rejected address, a malformed
	// body), and re-sending those without the handle would only fail again, so
	// only one that names the thread counts.
	return gerr.Code == 400 && strings.Contains(strings.ToLower(gerr.Message), "thread")
}

// googleThrottleReasons are the reasons Google returns for "you are going too
// fast", all of which arrive as a 403 alongside the ones that mean the grant is
// genuinely refused. Treating the whole status as refusal told the owner of a
// mailbox that had merely hit a per-minute quota to re-authorize, and reported
// every retry as an incident.
var googleThrottleReasons = map[string]struct{}{
	"rateLimitExceeded":     {},
	"userRateLimitExceeded": {},
	"quotaExceeded":         {},
	"dailyLimitExceeded":    {},
}

func isGoogleThrottle(gerr *googleapi.Error) bool {
	if gerr.Code == 429 {
		return true
	}
	if gerr.Code != 403 {
		return false
	}
	for _, item := range gerr.Errors {
		if _, ok := googleThrottleReasons[item.Reason]; ok {
			return true
		}
	}
	// Some Gmail 403s carry no structured reason, only the sentence. "Quota
	// exceeded for quota metric ... limit ... per minute per user" is the one
	// that flooded error tracking.
	msg := strings.ToLower(gerr.Message)
	return strings.Contains(msg, "quota exceeded") || strings.Contains(msg, "rate limit exceeded")
}

func HandleError(err error) *errx.MailError {
	if err == nil {
		return nil
	}
	// errors.As, not a type assertion: the API client wraps its error on some
	// paths, and an unwrapped assertion sends a real 401 down the transport
	// branch below.
	var gerr *googleapi.Error
	if errors.As(err, &gerr) {
		// Throttles come back as 403 and 429, so the status alone does not say
		// whether the caller should back off or the owner should re-authorize.
		if isGoogleThrottle(gerr) {
			return errx.ErrMailSendingTooFast
		}
		switch gerr.Code {
		case 401:
			return errx.ErrMailGoogleAuth
		case 402:
			return errx.ErrMailGooglePayment
		case 403:
			return errx.ErrMailGoogleForbidden(gerr.Message)
		default:
			respErr := errx.ErrMailGoogleUnknown(gerr.Code, gerr.Message)
			log.Debug().Err(err).Msg("Google Api Error")
			return respErr
		}
	}

	// The token source runs inside the API call, so a grant the user revoked
	// (or Google expired) never reaches Gmail to become a 401: it fails the
	// call itself and would otherwise read as an unreachable server, promising
	// a retry that can never succeed.
	if f := mailauth.ClassifyTokenError(err); f.Refused() {
		log.Debug().
			Str("oauth_error", f.ErrorCode).
			Str("oauth_description", f.Description).
			Int("status", f.Status).
			Bool("revoked", f.Revoked()).
			Msg("Gmail token refresh refused")
		if f.Revoked() {
			return errx.ErrMailGoogleAuth
		}
	}

	// Non-API failures (DNS, TLS, timeouts) are transient transport errors.
	// Returning nil here would silently swallow them and leave callers holding
	// a typed-nil *MailError in an error interface.
	log.Debug().Err(err).Msg("Google transport error")
	return errx.ErrMailServerUnreachable
}
