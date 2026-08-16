package wmail

import (
	"context"
	"errors"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// SyncGoogle walks Gmail's history from this mailbox's checkpoint and advances
// it. Runs serially from StartSyncWorker, so LastHistoryID needs no locking.
func (w *WMail) SyncGoogle(ctx context.Context) *errx.MailError {
	newHistoryID, err := w.GoogleData.Client.FetchHistory(ctx, w.GoogleData.LastHistoryID)
	if newHistoryID != 0 {
		// Advance the in-memory cursor before persisting. The next tick reads
		// this field, so leaving it stale re-walks the window just processed,
		// and leaving it at zero re-bootstraps past everything that arrived.
		w.GoogleData.LastHistoryID = newHistoryID
		if perr := w.NewHistoryID(newHistoryID); perr != nil {
			w.CaptureError(perr)
		}
	}
	if err != nil {
		var errMail *errx.MailError
		if errors.As(err, &errMail) {
			return errMail
		}

		w.CaptureError(err)
	}

	return nil
}

// NewHistoryID persists the mailbox's Gmail history checkpoint. UserID and
// EmailID address the row: email_history_ids is keyed (user_id, email_id) with
// a foreign key to users, so omitting them sends a zero UUID and the write is
// rejected outright rather than landing on the wrong row.
func (w *WMail) NewHistoryID(historyID uint64) error {
	return w.onEvent(models.JobEventTypeHistoryIDUpdate, &models.JobEventHistoryIDUpdate{
		UserID:    w.UserID,
		EmailID:   w.ID,
		HistoryID: historyID,
	})
}
