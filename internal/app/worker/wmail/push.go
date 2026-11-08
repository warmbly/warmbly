package wmail

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/client/msgraph"
	"github.com/warmbly/warmbly/internal/client/smtpimap/imap"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"golang.org/x/time/rate"
)

const (
	// graphRenewBefore renews a Graph subscription with two days of its week left.
	graphRenewBefore = 48 * time.Hour
	// gmailRenewBefore renews a Gmail watch daily, as Google recommends.
	gmailRenewBefore = 6 * 24 * time.Hour
	// pushRetryAfter spaces attempts to open a channel the provider refused.
	pushRetryAfter = 30 * time.Minute
	// imapWatchBackoffMax bounds the wait before re-opening a dropped IDLE session.
	imapWatchBackoffMax = 30 * time.Minute
)

// imapWatchDials paces IDLE logins across the whole worker so a restart does
// not sign every mailbox in at once.
var imapWatchDials = rate.NewLimiter(rate.Limit(20), 20)

// pushChannel is the provider channel a mailbox holds open, and what the
// control plane asked for.
type pushChannel struct {
	mu      sync.Mutex
	want    *models.AddWorkerEmailPushData
	state   *models.MailboxPushState
	retryAt time.Time
	renew   atomic.Bool
	// failing keeps a refused channel to one warning until it opens again.
	failing atomic.Bool
	// unsent is set while the control plane has not taken the current state.
	unsent atomic.Bool
	// closed is the last channel this worker closed, so a stored copy a consumer never cleared is not adopted.
	closed *models.MailboxPushState
	// imapIdle is set while the IMAP watcher holds INBOX in IDLE.
	imapIdle atomic.Bool
	imapCred *models.Service
}

// ApplyPush takes the push settings from a republished ADD_EMAIL.
func (w *WMail) ApplyPush(data *models.AddWorkerEmailPushData) {
	w.push.mu.Lock()
	defer w.push.mu.Unlock()
	w.push.want = data
	// A report the control plane has not taken yet is newer than the row it hands back.
	if w.push.state == nil && !w.push.unsent.Load() && data != nil && data.State != nil && !closedChannel(w.push.closed, data.State) {
		s := *data.State
		w.push.state = &s
	}
}

// Wake runs the next sync pass now; renew also refreshes the push channel.
func (w *WMail) Wake(renew bool) {
	if renew {
		w.push.renew.Store(true)
	}
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// pollInterval is the wait between passes: long when the provider announces
// the mailbox's changes, short when only polling finds them.
func (w *WMail) pollInterval() time.Duration {
	if w.push.imapIdle.Load() {
		return config.SyncImapWatchedInterval
	}
	w.push.mu.Lock()
	defer w.push.mu.Unlock()
	if s, want := w.push.state, w.push.want; s != nil && want != nil && s.ExpiresAt.After(time.Now()) &&
		(s.Provider == models.MailboxPushGraph && want.GraphNotificationURL != "" || s.Provider == models.MailboxPushGmail && want.GmailTopic != "") {
		return config.SyncPushFallbackInterval()
	}
	return ImapCheckInterval
}

func credentialPrint(v string) string {
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:8])
}

// maintainPush opens, renews or closes the mailbox's provider channel. It runs
// between sync passes on the sync goroutine, so it never races a pass.
func (w *WMail) maintainPush(ctx context.Context) {
	done, ok := w.BeginExecution()
	if !ok {
		return
	}
	defer done()
	ctx, cancel := w.ExecutionContext(ctx)
	defer cancel()
	now := time.Now()
	w.push.mu.Lock()
	want, state, retryAt := w.push.want, w.push.state, w.push.retryAt
	w.push.mu.Unlock()
	renew := w.push.renew.Swap(false)
	if !renew && now.Before(retryAt) {
		if w.push.unsent.Load() {
			w.reportPush(state)
		}
		return
	}
	var next *models.MailboxPushState
	var err error
	switch w.EmailType {
	case models.InboxProviderOutlook:
		if w.GraphData == nil {
			return
		}
		next, err = w.maintainGraphPush(ctx, want, state, renew, now)
	case models.InboxProviderGoogle:
		if w.GoogleData == nil {
			return
		}
		next, err = w.maintainGmailPush(ctx, want, state, renew, now)
	default:
		return
	}
	if err != nil {
		w.push.mu.Lock()
		w.push.retryAt = now.Add(pushRetryAfter)
		w.push.mu.Unlock()
		event := log.Debug()
		if !w.push.failing.Swap(true) {
			event = log.Warn()
			// Workers ship no logs, so the first failure per mailbox is how an operator sees why push is off.
			errs.CaptureException(err, errs.Tag("component", "mailbox_push"), errs.Tag("provider", string(w.EmailType)), errs.Tag("email_id", w.ID.String()))
		}
		event.Err(err).Str("email_id", w.ID.String()).Msg("mailbox push channel not opened; polling continues")
	} else {
		w.push.failing.Store(false)
	}
	// A failed open still reports a channel it closed on the way, and a lost report is sent again next pass.
	if samePush(state, next) && !w.push.unsent.Load() {
		return
	}
	w.push.mu.Lock()
	w.push.unsent.Store(true)
	if state != nil && (next == nil || next.Provider != state.Provider || next.SubscriptionID != state.SubscriptionID) {
		w.push.closed = state
	}
	w.push.state = next
	w.push.mu.Unlock()
	w.reportPush(next)
}

// closedChannel reports a stored row describing a channel this worker already closed: the same Graph
// subscription, or a Gmail watch no newer than the closed one (a renewal only moves the expiry later).
func closedChannel(closed, row *models.MailboxPushState) bool {
	if closed == nil || row == nil || closed.Provider != row.Provider {
		return false
	}
	if closed.SubscriptionID != "" || row.SubscriptionID != "" {
		return closed.SubscriptionID == row.SubscriptionID
	}
	return !row.ExpiresAt.After(closed.ExpiresAt)
}

// reportPush sends the channel state to the control plane; unsent stays set until it is taken.
func (w *WMail) reportPush(state *models.MailboxPushState) {
	if err := w.onEvent(models.JobEventTypePushState, &models.JobEventPushState{UserID: w.UserID, EmailID: w.ID, State: state}); err != nil {
		log.Debug().Err(err).Str("email_id", w.ID.String()).Msg("could not record mailbox push channel")
		return
	}
	w.push.unsent.Store(false)
}

func samePush(a, b *models.MailboxPushState) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func (w *WMail) maintainGraphPush(ctx context.Context, want *models.AddWorkerEmailPushData, state *models.MailboxPushState, renew bool, now time.Time) (*models.MailboxPushState, error) {
	client := w.GraphData.Client
	if state != nil && state.Provider != models.MailboxPushGraph {
		state = nil
	}
	if want == nil || want.GraphNotificationURL == "" || want.GraphClientState == "" {
		if state != nil && state.SubscriptionID != "" {
			if err := client.DeleteSubscription(ctx, state.SubscriptionID); err != nil {
				return state, err
			}
		}
		return nil, nil
	}
	cred := credentialPrint(want.GraphNotificationURL + "\x00" + want.GraphClientState)
	if state != nil && state.SubscriptionID != "" && state.Credential == cred {
		if !renew && state.ExpiresAt.Sub(now) > graphRenewBefore {
			return state, nil
		}
		sub, err := client.RenewSubscription(ctx, state.SubscriptionID, now)
		if err == nil {
			return &models.MailboxPushState{Provider: models.MailboxPushGraph, SubscriptionID: sub.ID, ExpiresAt: sub.ExpiresAt, Credential: cred}, nil
		}
		if !msgraph.SubscriptionGone(err) {
			return state, err
		}
		state = nil
	} else if state != nil && state.SubscriptionID != "" && client.DeleteSubscription(ctx, state.SubscriptionID) == nil {
		state = nil
	}
	sub, err := client.CreateSubscription(ctx, want.GraphNotificationURL, want.GraphClientState, now)
	if msgraph.SubscriptionConflict(err) {
		// An earlier subscription for this endpoint that we lost track of: replace it.
		subs, lerr := client.ListSubscriptions(ctx)
		if lerr != nil {
			return state, lerr
		}
		for _, s := range subs {
			if s.NotificationURL == want.GraphNotificationURL {
				_ = client.DeleteSubscription(ctx, s.ID)
			}
		}
		sub, err = client.CreateSubscription(ctx, want.GraphNotificationURL, want.GraphClientState, now)
	}
	if err != nil {
		return state, err
	}
	return &models.MailboxPushState{Provider: models.MailboxPushGraph, SubscriptionID: sub.ID, ExpiresAt: sub.ExpiresAt, Credential: cred}, nil
}

func (w *WMail) maintainGmailPush(ctx context.Context, want *models.AddWorkerEmailPushData, state *models.MailboxPushState, renew bool, now time.Time) (*models.MailboxPushState, error) {
	client := w.GoogleData.Client
	if state != nil && state.Provider != models.MailboxPushGmail {
		state = nil
	}
	if want == nil || want.GmailTopic == "" {
		if state != nil {
			if err := client.StopWatch(ctx); err != nil {
				return state, err
			}
		}
		return nil, nil
	}
	cred := credentialPrint(want.GmailTopic)
	if state != nil && state.Credential == cred && !renew && state.ExpiresAt.Sub(now) > gmailRenewBefore {
		return state, nil
	}
	expires, err := client.Watch(ctx, want.GmailTopic)
	if err != nil {
		return state, err
	}
	return &models.MailboxPushState{Provider: models.MailboxPushGmail, ExpiresAt: expires.UTC(), Credential: cred}, nil
}

// runImapWatcher holds the mailbox's INBOX in IDLE on its own connection and
// wakes the sync loop on every change, reconnecting with backoff when dropped.
func (w *WMail) runImapWatcher(ctx context.Context) {
	cred := w.push.imapCred
	if cred == nil || !config.ImapIdleEnabled() {
		return
	}
	// Spread a worker's IDLE logins over the first minute after it loads mailboxes.
	if !sleepCtx(ctx, time.Duration(rand.Int64N(int64(time.Minute)))) {
		return
	}
	backoff := 30 * time.Second
	for ctx.Err() == nil {
		if imapWatchDials.Wait(ctx) != nil {
			return
		}
		conn := &imap.Client{
			Context:     ctx,
			Email:       w.Email,
			AuthType:    models.AuthPlain,
			Credentials: cred,
			OnChange:    func() { w.Wake(false) },
		}
		started := time.Now()
		err := conn.WatchInbox(ctx, func() {
			if !w.push.imapIdle.Swap(true) {
				// Anything that arrived while the watch was down is picked up now.
				w.Wake(false)
			}
		})
		w.push.imapIdle.Store(false)
		_ = conn.Close()
		if ctx.Err() != nil {
			return
		}
		switch {
		case err == imap.ErrIdleUnsupported:
			log.Debug().Str("email_id", w.ID.String()).Msg("mail server has no IDLE; polling only")
			return
		case err != nil && !isTransportError(err) && err.Code != errx.MailErrorCodeSendingTooFast:
			// Credentials or permissions: the sync session reports those; retry rarely.
			backoff = imapWatchBackoffMax
		case time.Since(started) > 5*time.Minute:
			backoff = 30 * time.Second
		default:
			backoff = min(backoff*2, imapWatchBackoffMax)
		}
		if !sleepCtx(ctx, backoff) {
			return
		}
	}
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
