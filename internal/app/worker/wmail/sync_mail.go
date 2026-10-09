package wmail

import (
	"context"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func (w *WMail) SyncMail(ctx context.Context) *errx.MailError {
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
		return w.SyncGoogle(ctx)
	case models.InboxProviderOutlook:
		return w.SyncGraph(ctx)
	case models.InboxProviderSMTPIMAP:
		return w.Sync(ctx)
	default:
		return nil
	}
}
