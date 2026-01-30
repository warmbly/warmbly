package wmail

import (
	"context"
	"errors"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func (w *WMail) SyncGoogle(ctx context.Context) *errx.MailError {
	newHistoryID, err := w.GoogleData.Client.FetchHistory(ctx, w.GoogleData.LastHistoryID)
	if newHistoryID != 0 {
		if err := w.NewHistoryID(newHistoryID); err != nil {
			w.CaptureError(err)
			return nil
		}

		return nil
	}
	if err != nil {
		var errMail *errx.MailError
		if errors.As(err, &errMail) {
			return errMail
		}

		w.CaptureError(err)

		return nil
	}

	return nil
}

func (w *WMail) NewHistoryID(historyID uint64) error {
	return w.onEvent(models.JobEventTypeHistoryIDUpdate, &models.JobEventHistoryIDUpdate{
		HistoryID: historyID,
	})
}
