package wmail

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/nodeevidence"
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
	// A failed startup connection enters the same backoff as a failed sync.
	err := w.syncStartupError
	if err == nil {
		err = w.syncOnce(ctx)
	} else {
		if isTransportError(err) {
			w.transportFailures++
		}
		w.captureSyncError(err)
	}

	if w.EmailType == models.InboxProviderSMTPIMAP {
		go w.runImapWatcher(ctx)
	}
	for {
		w.maintainPush(ctx)
		if !w.waitForPass(ctx, w.nextSyncDelay(w.pollInterval(), err), err == nil) {
			return
		}
		err = w.syncOnce(ctx)
	}
}

// waitForPass sleeps until the next pass is due or, when the last pass was
// clean, a wake arrives. A wake never cuts short a backoff the provider asked for.
func (w *WMail) waitForPass(ctx context.Context, delay time.Duration, wakeable bool) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	wake := w.wake
	if !wakeable {
		wake = nil
	}
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		// The pass about to run covers any wake that queued during the wait.
		select {
		case <-w.wake:
		default:
		}
		return true
	case <-wake:
		// Let the rest of a burst land so one pass covers it.
		return sleepCtx(ctx, config.SyncWakeMinGap)
	}
}

// nextSyncDelay picks the wait before the next pass.
func (w *WMail) nextSyncDelay(base time.Duration, last *errx.MailError) time.Duration {
	d := base
	minimum := base / 2
	switch {
	case last != nil && last.RetryAfter > 0 && (isTransportError(last) || last.Code == errx.MailErrorCodeSendingTooFast):
		// Service-unavailable responses can supply the same retry window as 429.
		d = max(last.RetryAfter, base)
		minimum = d
	case last != nil && (last.Code == errx.MailErrorCodeSendingTooFast ||
		last.Code == errx.MailErrorCodeTLSCertificateChanged || last.Code == errx.MailErrorCodeTLSCertificateUnconfirmed):
		d = syncBackoffMax
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
			w.captureSyncError(err)
			log.Error().Err(err).Str("email_id", w.ID.String()).Msg("mail sync panicked")
			result = &errx.MailError{Code: errx.MailErrorCodeImapUnknown, Type: errx.MailErrorWarning, Message: "mail sync pass interrupted"}
		}
	}()
	if err := w.SyncMail(ctx); err != nil {
		w.lifecycleMu.Lock()
		stopped := w.stopped
		w.lifecycleMu.Unlock()
		if stopped || ctx.Err() != nil {
			return err
		}
		switch err.Code {
		case errx.MailErrorCodeServerUnreachable, errx.MailErrorCodeConnectionLost:
			nodeevidence.Emit(nodeevidence.ProviderUnreachable, w.ID, 0, 1)
		case errx.MailErrorCodeGoogleAuth, errx.MailErrorCodeAuthenticationFailed, errx.MailErrorCodeAuthorizationFailed:
			nodeevidence.Emit(nodeevidence.ProviderAuth, w.ID, 0, 1)
		case errx.MailErrorCodeSendingTooFast:
			nodeevidence.Emit(nodeevidence.ProviderThrottled, w.ID, 0, 1)
		}
		// Keep observations fresh; the consumer deduplicates user notifications.
		if isTransportError(err) {
			w.transportFailures++
			if w.transportFailures > 1 {
				w.captureSyncError(err)
				log.Debug().Err(err).Str("email_id", w.ID.String()).Int("consecutive", w.transportFailures).Msg("mail server still unreachable")
				return err
			}
		}
		w.captureSyncError(err)
		log.Warn().Err(err).Str("email_id", w.ID.String()).Msg("mail sync error")
		return err
	}
	if w.tracker != nil && w.tracker.tickComplete {
		if w.transportFailures > 0 {
			nodeevidence.Emit(nodeevidence.ProviderRecovered, w.ID, 0, 1)
		}
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
