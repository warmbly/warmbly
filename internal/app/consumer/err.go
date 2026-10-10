package jobs

import (
	"errors"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/observability/errs"
)

func CaptureError(userID, emailID uuid.UUID, err error) {
	if errors.Is(err, ErrSyncArrivalPending) {
		return
	}
	errs.CaptureException(err,
		errs.Tag("user_id", userID.String()),
		errs.Tag("email_id", emailID.String()),
	)
}
