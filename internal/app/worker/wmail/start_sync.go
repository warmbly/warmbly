package wmail

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/errx"
)

// syncBackoffMax is the longest a mailbox waits between passes: while fair
// use holds it, or after the provider itself asked us to slow down.
const syncBackoffMax = 5 * time.Minute

// StartSyncWorker runs the mail sync loop until the context is cancelled.
// It dispatches to the right provider (Gmail history, Graph delta, IMAP
// CONDSTORE) and keeps the inbox up to date by emitting NEW_EMAIL and the
// other job events whenever the upstream mailbox changes.
//
// The interval adapts: a mailbox held by fair use, or one the provider just
// throttled, waits longer, and every wait carries jitter so a worker with
// hundreds of mailboxes does not fire them all in the same second.
func (w *WMail) StartSyncWorker(ctx context.Context) {
	// Run an initial sync immediately so the inbox is fresh on startup and a
	// just-connected mailbox starts importing right away.
	err := w.syncOnce(ctx)

	for {
		delay := w.nextSyncDelay(ImapCheckInterval, err)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		err = w.syncOnce(ctx)
	}
}

// nextSyncDelay picks the wait before the next pass.
func (w *WMail) nextSyncDelay(base time.Duration, last *errx.MailError) time.Duration {
	d := base
	if last != nil && last.Code == errx.MailErrorCodeSendingTooFast {
		// The provider returned 429: back off well past the base interval.
		d = syncBackoffMax
	} else if w.tracker != nil && w.tracker.state.ThrottledUntil != nil {
		// Held by fair use: no point asking every minute; wake when the
		// window rolls, bounded so a priority reply still lands promptly.
		if until := time.Until(*w.tracker.state.ThrottledUntil); until > d {
			d = min(until, syncBackoffMax)
		}
	}
	// ±10% jitter.
	spread := d / 10
	d += time.Duration(rand.Int64N(int64(2*spread)+1)) - spread
	if d < base/2 {
		d = base / 2
	}
	return d
}

// syncOnce runs one sync pass, containing panics: the worker is multi-tenant,
// so one mailbox's bad server response must not take down every other
// account's sync and send loops.
func (w *WMail) syncOnce(ctx context.Context) (result *errx.MailError) {
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
		return err
	}
	return nil
}
