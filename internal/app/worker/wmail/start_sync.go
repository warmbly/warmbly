package wmail

import (
	"context"
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
	if err := w.SyncMail(ctx); err != nil {
		log.Warn().Err(err).Str("email_id", w.ID.String()).Msg("initial mail sync failed")
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.SyncMail(ctx); err != nil {
				w.CaptureError(err)
				log.Warn().Err(err).Str("email_id", w.ID.String()).Msg("mail sync error")
			}
		}
	}
}
