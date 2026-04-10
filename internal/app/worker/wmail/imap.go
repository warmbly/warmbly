package wmail

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"
)

// StartImapWorker runs a periodic IMAP sync loop until the context is cancelled.
// On each tick it pulls folder/message changes from the IMAP server and emits
// inbox events. Errors are logged and do not terminate the loop unless they
// indicate auth or connectivity failure.
func (w *WMail) StartImapWorker(ctx context.Context) {
	if w.SmtpImapData == nil || w.SmtpImapData.ImapClient == nil {
		return
	}

	// Run an initial sync immediately so the inbox is fresh on startup.
	if err := w.Sync(ctx); err != nil {
		log.Warn().Err(err).Str("email_id", w.ID.String()).Msg("initial IMAP sync failed")
	}

	ticker := time.NewTicker(ImapCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.Sync(ctx); err != nil {
				w.CaptureError(err)
				log.Warn().Err(err).Str("email_id", w.ID.String()).Msg("IMAP sync error")
				// If sync repeatedly fails the rate limiter / error handler will
				// terminate this account; we keep trying for transient errors.
			}
		}
	}
}
