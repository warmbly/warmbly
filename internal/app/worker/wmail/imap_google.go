package wmail

import (
	"context"
	"errors"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func (w *WMail) ImapGoogleSync(ctx context.Context, lastHistoryID uint64) *errx.MailError {
	newHistoryID, err := w.GoogleData.Client.FetchHistory(ctx, lastHistoryID)
	if newHistoryID != 0 {
		if err := w.onEvent(models.JobEventTypeHistoryIDUpdate, &models.JobEventHistoryIDUpdate{
			UserID:    w.UserID,
			EmailID:   w.ID,
			HistoryID: newHistoryID,
		}); err != nil {
			return nil
		}
	}
	if err != nil {
		var mailErr *errx.MailError
		if errors.As(err, &mailErr) {
			return mailErr
		}

		return nil
	}

	return nil
}
