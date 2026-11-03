package worker

import (
	"context"

	"github.com/warmbly/warmbly/internal/models"
)

// HandleSyncWake runs a mailbox's sync pass early. A mailbox not held here is
// not an error: it moved, and its new worker polls it.
func (w *WorkerService) HandleSyncWake(_ context.Context, e *models.SyncWake) error {
	if e == nil {
		return nil
	}
	if mail := w.mailManager.Get(e.EmailID); mail != nil && mail.UserID == e.UserID {
		mail.Wake(e.Renew)
	}
	return nil
}

// MailboxCount is how many mailboxes this worker holds, zero before it is initialised.
func (w *WorkerService) MailboxCount() int {
	if w.mailManager == nil {
		return 0
	}
	return w.mailManager.Count()
}
