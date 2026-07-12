package wmail

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/models"
)

// StartSyncWorker runs a periodic mail sync loop until the context is cancelled.
// It dispatches to the right provider (Google history-based or IMAP poll-based)
// and keeps the inbox up to date by emitting JobEventTypeNewEmail and other
// events whenever changes are detected on the upstream mail server.
func (w *WMail) StartSyncWorker(ctx context.Context) {
	interval := ImapCheckInterval
	if w.EmailType == models.InboxProviderGoogle {
		// Google polling can be slightly slower because the API is more efficient
		// and rate-limited.
		interval = 1 * time.Minute
	}

	// Run an initial sync immediately so the inbox is fresh on startup.
	w.syncOnce(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.syncOnce(ctx)
		}
	}
}

// syncOnce runs one sync pass, containing panics: the worker is multi-tenant,
// so one mailbox's bad server response must not take down every other
// account's sync and send loops.
func (w *WMail) syncOnce(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			err := fmt.Errorf("mail sync panic: %v", r)
			w.CaptureError(err)
			log.Error().Err(err).Str("email_id", w.ID.String()).Msg("mail sync panicked")
		}
	}()
	if err := w.SyncMail(ctx); err != nil {
		w.CaptureError(err)
		log.Warn().Err(err).Str("email_id", w.ID.String()).Msg("mail sync error")
	}
}
