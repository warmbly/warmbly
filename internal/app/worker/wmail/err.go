package wmail

import "github.com/getsentry/sentry-go"

func (w *WMail) CaptureError(err error) {
	sentry.WithScope(func(scope *sentry.Scope) {
		scope.SetTag("user_id", w.UserID.String())
		scope.SetTag("email_id", w.ID.String())
		sentry.CaptureException(err)
	})
}
