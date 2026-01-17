package jobs

import (
	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
)

func CaptureError(userID, emailID uuid.UUID, err error) {
	sentry.WithScope(func(scope *sentry.Scope) {
		scope.SetTag("user_id", userID.String())
		scope.SetTag("email_id", emailID.String())
		sentry.CaptureException(err)
	})
}
