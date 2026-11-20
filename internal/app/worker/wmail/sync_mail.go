package wmail

import (
	"context"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// SyncMail runs one full pass.
func (w *WMail) SyncMail(ctx context.Context) *errx.MailError {
	return w.syncMail(ctx, false)
}

// syncMail runs a full pass, or with narrow the check a wake asks for: only
// what a push or IDLE event can announce, from the cursors the full pass keeps.
func (w *WMail) syncMail(ctx context.Context, narrow bool) *errx.MailError {
	done, ok := w.BeginExecution()
	if !ok {
		return errx.ErrMailServerUnreachable
	}
	defer done()
	w.syncMu.Lock()
	defer w.syncMu.Unlock()
	ctx, cancel := w.ExecutionContext(ctx)
	defer cancel()
	if ctx.Err() != nil {
		return errx.ErrMailServerUnreachable
	}
	w.retryDiagnostics(ctx)
	defer w.retryDiagnostics(ctx)
	switch w.EmailType {
	case models.InboxProviderGoogle:
		return w.syncGoogle(ctx, narrow)
	case models.InboxProviderOutlook:
		return w.syncGraph(ctx, narrow)
	case models.InboxProviderSMTPIMAP:
		if narrow {
			return w.imapWakeCheck(ctx)
		}
		return w.Sync(ctx)
	default:
		return nil
	}
}
