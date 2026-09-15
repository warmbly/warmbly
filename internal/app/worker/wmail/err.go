package wmail

import (
	"context"
	"errors"
	"time"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/eventbus"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
)

// reportable is false for the conditions the sync loop is built to absorb.
// Reporting them filed an issue per retry for a provider throttle that clears
// itself, and one per mailbox for a bus that closed because we are shutting
// down. The relay below still runs either way, so nothing stops acting on them.
func reportable(err error) bool {
	if errors.Is(err, eventbus.ErrBusClosed) || errors.Is(err, context.Canceled) {
		return false
	}
	var mailErr *errx.MailError
	if !errors.As(err, &mailErr) {
		return true
	}
	// Only the throttles the loop rides out. The codes that deactivate a
	// mailbox (rate limit, sync fair use, flood) stay reportable, because an
	// operator does want to see those.
	switch mailErr.Code {
	case errx.MailErrorCodeSendingTooFast, errx.MailErrorCodeQuotaExceeded:
		return false
	}
	return true
}

func (w *WMail) CaptureError(err error) {
	if reportable(err) {
		errs.CaptureException(err,
			errs.Tag("user_id", w.UserID.String()),
			errs.Tag("email_id", w.ID.String()),
		)
	}

	// If the error is a critical mail error (auth, disabled, rate limit), publish
	// an event so the consumer can mark the account inactive and stop syncing.
	var mailErr *errx.MailError
	if !errors.As(err, &mailErr) {
		return
	}

	eventType := mailErrorToJobEventType(mailErr)
	if eventType == "" {
		return
	}

	userInfo := mailErr.GetUserErrorInfo()
	errorEvent := models.EmailErrorEvent{
		EmailAccountID: w.ID.String(),
		UserID:         w.UserID.String(),
		ErrorCode:      string(mailErr.Code),
		ErrorType:      string(mailErr.Type),
		ResolveMethod:  string(mailErr.ResolveMethod),
		Message:        mailErr.Message,
		UserVisible:    mailErr.IsUserVisible(),
		UserTitle:      userInfo.Title,
		UserMessage:    userInfo.Message,
		ActionRequired: userInfo.ActionRequired,
		Timestamp:      time.Now().Unix(),
	}

	_ = w.onEvent(eventType, errorEvent)

	// Critical errors should stop the sync loop and remove the account from the
	// worker's local state until the user re-authenticates.
	if eventType == models.JobEventTypeEmailAuthError ||
		eventType == models.JobEventTypeEmailDisabled {
		if w.Cancel != nil {
			w.Cancel()
		}
		if w.TerminateFunc != nil {
			w.TerminateFunc()
		}
	}
}

// mailErrorToJobEventType maps a mail error code to the matching job event type
// so the consumer knows how to handle it.
func mailErrorToJobEventType(mailErr *errx.MailError) models.JobEventType {
	switch mailErr.Code {
	case errx.MailErrorCodeGoogleAuth,
		errx.MailErrorCodeAuthenticationFailed,
		errx.MailErrorCodeAuthorizationFailed,
		errx.MailErrorCodeInvalidCredentials:
		return models.JobEventTypeEmailAuthError
	case errx.MailErrorCodeRateLimitExceeded:
		return models.JobEventTypeEmailRateLimited
	// A provider 429 during sync (MailErrorCodeSendingTooFast) is not
	// relayed: the loop backs off and retries. Relaying it as a rate-limit
	// event would deactivate the mailbox for a transient provider throttle.
	case errx.MailErrorCodeServerUnreachable,
		errx.MailErrorCodeConnectionLost,
		errx.MailErrorCodeNotFound,
		errx.MailErrorCodeImapUnknown:
		return models.JobEventTypeEmailServerError
	}
	return ""
}
