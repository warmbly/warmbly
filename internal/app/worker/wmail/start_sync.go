package wmail

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/errx"
)

// syncBackoffMax is the normal ceiling between passes. An explicit provider
// Retry-After may be longer and takes precedence.
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
	minimum := base / 2
	switch {
	case last != nil && last.Code == errx.MailErrorCodeSendingTooFast:
		// The provider returned 429. Respect its window when supplied.
		d = syncBackoffMax
		if last.RetryAfter > 0 {
			d = max(last.RetryAfter, base)
			minimum = d
		}
	case last != nil && isTransportError(last):
		// The server is unreachable. Retry soon after the first failure (a
		// dropped session reconnects on the next pass and costs one dial),
		// then step back toward the ceiling while it stays down, so a server
		// that is out for a day does not write a warning every minute.
		d = min(base<<min(w.transportFailures, 8), syncBackoffMax)
	case w.tracker != nil && w.tracker.state.ThrottledUntil != nil:
		// Held by fair use: no point asking every minute; wake when the
		// window rolls, bounded so a priority reply still lands promptly.
		if until := time.Until(*w.tracker.state.ThrottledUntil); until > d {
			d = min(until, syncBackoffMax)
		}
	}
	// ±10% jitter.
	spread := d / 10
	d += time.Duration(rand.Int64N(int64(2*spread)+1)) - spread
	if d < minimum {
		d = minimum
	}
	return d
}

// syncOnce runs one sync pass, containing panics: the worker is multi-tenant,
// so one mailbox's bad server response must not take down every other
// account's sync and send loops.
func (w *WMail) syncOnce(ctx context.Context) (result *errx.MailError) {
	if w.tracker != nil {
		w.tracker.tickComplete = false
	}
	defer func() {
		if r := recover(); r != nil {
			if w.tracker != nil {
				w.tracker.tickComplete = false
			}
			err := fmt.Errorf("mail sync panic: %v", r)
			w.CaptureError(err)
			log.Error().Err(err).Str("email_id", w.ID.String()).Msg("mail sync panicked")
			result = &errx.MailError{Code: errx.MailErrorCodeImapUnknown, Type: errx.MailErrorWarning, Message: "mail sync pass interrupted"}
		}
	}()
	if err := w.SyncMail(ctx); err != nil {
		// A server that is down answers every pass the same way. Report the
		// first one and then stay quiet until it comes back, so one outage is
		// one warning in the drawer rather than one a minute.
		if isTransportError(err) {
			w.transportFailures++
			if w.transportFailures > 1 {
				log.Debug().Err(err).Str("email_id", w.ID.String()).Int("consecutive", w.transportFailures).Msg("mail server still unreachable")
				return err
			}
		}
		w.CaptureError(err)
		log.Warn().Err(err).Str("email_id", w.ID.String()).Msg("mail sync error")
		return err
	}
	if w.tracker != nil && w.tracker.tickComplete {
		w.transportFailures = 0
	}
	return nil
}

// isTransportError is the mail server being unreachable rather than refusing
// what we asked: a dropped session, a refused dial, a timeout. These retry on
// their own and must not be treated as a mailbox problem.
func isTransportError(err *errx.MailError) bool {
	if err == nil {
		return false
	}
	switch err.Code {
	case errx.MailErrorCodeServerUnreachable, errx.MailErrorCodeConnectionLost:
		return true
	}
	return false
}
