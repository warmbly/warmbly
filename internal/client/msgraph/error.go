package msgraph

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/warmbly/warmbly/internal/errx"
)

// graphErrorEnvelope is Graph's standard error shape.
type graphErrorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// HandleError maps a non-2xx Graph response to a MailError. It classifies by
// HTTP status so the send/sync retry loop can tell transient (retryable) from
// critical (needs re-auth / stop) failures:
//   - 401 -> authentication failed (token expired/revoked, re-consent)
//   - 403 -> authorization failed (missing scope / mailbox disabled)
//   - 404 -> resource not found (a folder the tenant does not have, a message
//     already gone); its own code so a caller can skip what is absent without
//     also skipping on a 503
//   - 429 -> sending too fast (throttled; Retry-After honored by the caller loop)
//   - 5xx / other -> server unreachable (retry)
func HandleError(resp *http.Response) *errx.MailError {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	var env graphErrorEnvelope
	_ = json.Unmarshal(body, &env)

	now := time.Now().UTC()
	failure := errx.SendFailure{Provider: "microsoft", Protocol: "http", Status: resp.StatusCode,
		Cause: errx.SafeProviderCode(env.Error.Code), Scope: "mailbox", ObservedAt: now,
		Disposition: errx.SendRetry, RetryAt: errx.RetryAfterAt(resp.Header.Get("Retry-After"), now)}
	base := errx.ErrMailServerUnreachable
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		base, failure.Disposition = errx.ErrMailAuthenticationFailed, errx.SendAuth
	case http.StatusForbidden:
		base, failure.Disposition = errx.ErrMailAuthorizationFailed, errx.SendAuth
	case http.StatusNotFound:
		base, failure.Disposition = errx.ErrMailResourceNotFound, errx.SendPermanent
	case http.StatusTooManyRequests:
		base, failure.Disposition = errx.ErrMailSendingTooFast, errx.SendThrottle
	default:
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			failure.Disposition = errx.SendPermanent
		}
	}
	return errx.WithSendFailure(base, failure)
}

func retryAfter(value string, now time.Time) time.Duration {
	if at := errx.RetryAfterAt(value, now); at != nil {
		return at.Sub(now)
	}
	return 0
}
