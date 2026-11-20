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
// The full pass runs on a timer. A wake (push notification or IDLE event)
// runs only a narrow check of what such an event can announce, at most once
// per SyncWakeMinGap, and leaves the full pass's schedule where it was.
//
// The interval adapts: a mailbox held by fair use, or one the provider just
// throttled, waits longer, and every wait carries jitter so a worker with
// hundreds of mailboxes does not fire them all in the same second.
func (w *WMail) StartSyncWorker(ctx context.Context) {
	// A failed startup connection enters the same backoff as a failed sync.
	err := w.syncStartupError
	if err == nil {
		err = w.syncOnce(ctx, false)
	} else {
		if isTransportError(err) {
			w.transportFailures++
		}
		w.captureSyncError(err)
	}

	if w.EmailType == models.InboxProviderSMTPIMAP {
		go w.runImapWatcher(ctx)
	}
	last := time.Now()
	due := last.Add(w.nextSyncDelay(w.pollInterval(), err))
	pollGap := jitter(config.SyncNarrowPollInterval)
	for {
		w.maintainPush(ctx)
		narrowAt, signalUntil := w.narrowPoll(last, pollGap, err == nil)
		step := w.waitForPass(ctx, syncPlan{due: due, notBefore: last.Add(config.SyncWakeMinGap), narrowAt: narrowAt, signalUntil: signalUntil, wakeable: err == nil})
		if step == stepStop {
			return
		}
		if step == stepReplan {
			continue
		}
		narrow := step == stepNarrow
		last = time.Now()
		pollGap = jitter(config.SyncNarrowPollInterval)
		err = w.syncOnce(ctx, narrow)
		// A clean check keeps the full pass's slot; a failure backs off and retries as a full pass.
		if !narrow || err != nil {
			due = time.Now().Add(w.nextSyncDelay(w.pollInterval(), err))
		}
	}
}

// narrowPoll is when a mailbox with no live change signal gets its timed
// narrow check (zero while a signal holds or after a failed check), and when a
// live push channel lapses so the timer can take over then.
func (w *WMail) narrowPoll(last time.Time, gap time.Duration, clean bool) (narrowAt, signalUntil time.Time) {
	live, until := w.liveSignal()
	if !live && clean {
		narrowAt = last.Add(gap)
	}
	return narrowAt, until
}

// syncStep is what the loop does after a wait.
type syncStep int

const (
	stepStop syncStep = iota
	stepFull
	stepNarrow
	// stepReplan: the mailbox's change signal came or went, so the timers are worked out again.
	stepReplan
)

// syncPlan is what one wait is for: the full pass, the earliest a narrow check
// may start, the narrow poll when the mailbox has no live change signal, and
// when its live signal lapses.
type syncPlan struct {
	due, notBefore, narrowAt, signalUntil time.Time
	wakeable                              bool
}

// waitForPass sleeps until the full pass is due, a narrow poll falls due, the
// change signal changes, or, when the last check was clean, a wake arrives and
// its check may run. A wake never cuts short a backoff the provider asked
// for, and a narrow check that would start after the full pass is folded into it.
func (w *WMail) waitForPass(ctx context.Context, p syncPlan) syncStep {
	timer := time.NewTimer(time.Until(p.due))
	defer timer.Stop()
	var poll, lapse <-chan time.Time
	if !p.narrowAt.IsZero() && p.narrowAt.Before(p.due) {
		t := time.NewTimer(time.Until(p.narrowAt))
		defer t.Stop()
		poll = t.C
	}
	if !p.signalUntil.IsZero() && p.signalUntil.Before(p.due) {
		t := time.NewTimer(time.Until(p.signalUntil))
		defer t.Stop()
		lapse = t.C
	}
	wake := w.wake
	if !p.wakeable {
		wake = nil
	}
	select {
	case <-ctx.Done():
		return stepStop
	case <-timer.C:
		w.drainWake()
		return stepFull
	case <-lapse:
		return stepReplan
	case <-w.signal:
		return stepReplan
	case <-poll:
	case <-wake:
	}
	// Let the rest of a burst land, and keep to the gap between checks.
	hold := max(config.SyncWakeSettle, time.Until(p.notBefore))
	if until := time.Until(p.due); hold >= until {
		if !sleepCtx(ctx, until) {
			return stepStop
		}
		w.drainWake()
		return stepFull
	}
	if !sleepCtx(ctx, hold) {
		return stepStop
	}
	// The check about to run covers any wake that queued while it waited.
	w.drainWake()
	return stepNarrow
}

// jitter spreads d by ±10% so a fleet does not check in lockstep.
func jitter(d time.Duration) time.Duration {
	spread := d / 10
	return d + time.Duration(rand.Int64N(int64(2*spread)+1)) - spread
}

func (w *WMail) drainWake() {
	select {
	case <-w.wake:
	default:
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
	d = jitter(d)
	if d < minimum {
		d = minimum
	}
	return d
}

// syncOnce runs one sync pass (narrow: a wake's check), containing panics: the
// worker is multi-tenant, so one mailbox's bad server response must not take
// down every other account's sync and send loops.
func (w *WMail) syncOnce(ctx context.Context, narrow bool) (result *errx.MailError) {
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
	if err := w.syncMail(ctx, narrow); err != nil {
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
