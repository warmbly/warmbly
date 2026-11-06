package wmail

import (
	"context"
	"errors"
	"time"

	"github.com/warmbly/warmbly/internal/client/msgraph"
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
	if errors.Is(err, eventbus.ErrBusClosed) || errors.Is(err, context.Canceled) || errors.Is(err, msgraph.ErrUnreadableResponse) {
		return false
	}
	var mailErr *errx.MailError
	if !errors.As(err, &mailErr) {
		return true
	}
	// Throttles the loop rides out and credential refusals only the owner can fix; Warmbly's own deactivations stay reportable.
	switch mailErr.Code {
	case errx.MailErrorCodeSendingTooFast,
		errx.MailErrorCodeQuotaExceeded,
		errx.MailErrorCodeServerUnreachable,
		errx.MailErrorCodeConnectionLost,
		errx.MailErrorCodeTLSCertificateChanged,
		errx.MailErrorCodeTLSCertificateUnconfirmed:
		return false
	case errx.MailErrorCodeGoogleAuth, errx.MailErrorCodeInvalidCredentials:
		// Relayed below, which deactivates the mailbox and tells its owner.
		return false
	}
	return true
}

func (w *WMail) CaptureError(err error) {
	w.captureOperationError(err, models.MailTransportUnknown)
}

func (w *WMail) captureSyncError(err error) {
	w.captureOperationError(err, models.MailTransportSync)
}

func (w *WMail) captureOperationError(err error, operation models.MailTransportOperation) {
	w.lifecycleMu.Lock()
	stopped := w.stopped
	w.lifecycleMu.Unlock()
	if stopped {
		return
	}
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
		TransportGeneration: w.TransportGeneration,
		Operation:           operation,
		EmailAccountID:      w.ID.String(),
		UserID:              w.UserID.String(),
		ErrorCode:           string(mailErr.Code),
		ErrorType:           string(mailErr.Type),
		ResolveMethod:       string(mailErr.ResolveMethod),
		Message:             mailErr.Message,
		UserVisible:         mailErr.IsUserVisible(),
		UserTitle:           userInfo.Title,
		UserMessage:         userInfo.Message,
		ActionRequired:      userInfo.ActionRequired,
		Timestamp:           ErrorObservedTimestamp(mailErr),
	}

	w.lifecycleMu.Lock()
	if w.stopped {
		w.lifecycleMu.Unlock()
		return
	}
	_ = w.onEvent(eventType, errorEvent)
	w.lifecycleMu.Unlock()

	// Critical errors should stop the sync loop and remove the account from the
	// worker's local state until the user re-authenticates.
	if eventType == models.JobEventTypeEmailAuthError ||
		eventType == models.JobEventTypeEmailDisabled {
		w.Terminate()
	}
}

func ErrorObservedTimestamp(mailErr *errx.MailError) int64 {
	if mailErr.Failure != nil && !mailErr.Failure.ObservedAt.IsZero() {
		return mailErr.Failure.ObservedAt.Unix()
	}
	return time.Now().Unix()
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
	// A refused certificate pin is a connection error the owner resolves in
	// the mailbox's settings; it never deactivates the mailbox.
	case errx.MailErrorCodeServerUnreachable,
		errx.MailErrorCodeConnectionLost,
		errx.MailErrorCodeNotFound,
		errx.MailErrorCodeImapUnknown,
		errx.MailErrorCodeTLSCertificateChanged,
		errx.MailErrorCodeTLSCertificateUnconfirmed:
		return models.JobEventTypeEmailServerError
	}
	return ""
}
