package wmail

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func TestWakeRunsTheNextPassEarlyOnlyAfterACleanPass(t *testing.T) {
	w := &WMail{wake: make(chan struct{}, 1)}
	w.Wake(false)
	started := time.Now()
	if !w.waitForPass(t.Context(), time.Hour, true) {
		t.Fatal("wait ended without a pass")
	}
	if waited := time.Since(started); waited > config.SyncWakeMinGap+time.Second {
		t.Fatalf("a wake waited %v, not the merge gap", waited)
	}

	w.Wake(false)
	started = time.Now()
	if !w.waitForPass(t.Context(), 50*time.Millisecond, false) {
		t.Fatal("wait ended without a pass")
	}
	if time.Since(started) < 50*time.Millisecond {
		t.Fatal("a wake cut short the backoff after a failed pass")
	}
	if len(w.wake) != 0 {
		t.Fatal("a wake the timed pass covers was left for another pass")
	}
}

func TestWakesCoalesce(t *testing.T) {
	w := &WMail{wake: make(chan struct{}, 1)}
	for range 100 {
		w.Wake(false)
	}
	if len(w.wake) != 1 {
		t.Fatalf("%d wakes pending, want one pass for the burst", len(w.wake))
	}
}

func TestPollIntervalFollowsThePushChannel(t *testing.T) {
	w := &WMail{wake: make(chan struct{}, 1)}
	if got := w.pollInterval(); got != ImapCheckInterval {
		t.Fatalf("no push: %v", got)
	}
	w.ApplyPush(&models.AddWorkerEmailPushData{GraphNotificationURL: "https://x", GraphClientState: "s",
		State: &models.MailboxPushState{Provider: models.MailboxPushGraph, SubscriptionID: "sub", ExpiresAt: time.Now().Add(time.Hour)}})
	if got := w.pollInterval(); got != config.SyncPushFallbackInterval() {
		t.Fatalf("live subscription: %v", got)
	}
	w.push.state.ExpiresAt = time.Now().Add(-time.Minute)
	if got := w.pollInterval(); got != ImapCheckInterval {
		t.Fatalf("an expired subscription must fall back to the normal poll, got %v", got)
	}
	w.push.imapIdle.Store(true)
	if got := w.pollInterval(); got != config.SyncImapWatchedInterval {
		t.Fatalf("IMAP in IDLE: %v", got)
	}
}

func TestWakeRenewIsRemembered(t *testing.T) {
	w := &WMail{wake: make(chan struct{}, 1)}
	w.Wake(true)
	if !w.push.renew.Load() {
		t.Fatal("a renew wake was dropped")
	}
}

func TestAnUnsentChannelReportOutranksTheControlPlanesRow(t *testing.T) {
	var refuse bool
	var sent []*models.MailboxPushState
	w := &WMail{wake: make(chan struct{}, 1), onEvent: func(_ models.JobEventType, body any) error {
		if refuse {
			return errors.New("bus down")
		}
		sent = append(sent, body.(*models.JobEventPushState).State)
		return nil
	}}
	// The channel was closed, and the report saying so was refused.
	refuse = true
	w.push.unsent.Store(true)
	w.reportPush(nil)
	if !w.push.unsent.Load() {
		t.Fatal("a refused report was marked taken")
	}
	w.ApplyPush(&models.AddWorkerEmailPushData{GraphNotificationURL: "https://x", GraphClientState: "s",
		State: &models.MailboxPushState{Provider: models.MailboxPushGraph, SubscriptionID: "closed", ExpiresAt: time.Now().Add(time.Hour)}})
	if w.push.state != nil {
		t.Fatal("the control plane's stale row replaced a channel this worker closed")
	}
	refuse = false
	w.reportPush(nil)
	if w.push.unsent.Load() || len(sent) != 1 || sent[0] != nil {
		t.Fatalf("the retried report was not taken: unsent=%v sent=%v", w.push.unsent.Load(), sent)
	}
}

func TestAClosedChannelIsNotAdoptedFromAStoredRowNobodyCleared(t *testing.T) {
	live := &models.MailboxPushState{Provider: models.MailboxPushGraph, SubscriptionID: "sub-1", ExpiresAt: time.Now().Add(time.Hour)}
	want := &models.AddWorkerEmailPushData{GraphNotificationURL: "https://x", GraphClientState: "s", State: live}
	// The bus took the closing report, but a consumer that does not know PUSH_STATE dropped it.
	w := &WMail{wake: make(chan struct{}, 1)}
	w.push.closed = live
	w.ApplyPush(want)
	if w.push.state != nil {
		t.Fatal("a closed Graph subscription was adopted from the stored row")
	}
	other := *live
	other.SubscriptionID = "sub-2"
	w.ApplyPush(&models.AddWorkerEmailPushData{GraphNotificationURL: "https://x", GraphClientState: "s", State: &other})
	if w.push.state == nil || w.push.state.SubscriptionID != "sub-2" {
		t.Fatal("a different subscription was refused")
	}

	gmailClosed := &models.MailboxPushState{Provider: models.MailboxPushGmail, ExpiresAt: time.Now().Add(time.Hour)}
	if !closedChannel(gmailClosed, &models.MailboxPushState{Provider: models.MailboxPushGmail, ExpiresAt: gmailClosed.ExpiresAt.Add(-time.Minute)}) {
		t.Fatal("an older Gmail watch than the closed one was treated as live")
	}
	if closedChannel(gmailClosed, &models.MailboxPushState{Provider: models.MailboxPushGmail, ExpiresAt: gmailClosed.ExpiresAt.Add(time.Hour)}) {
		t.Fatal("a newer Gmail watch was treated as the closed one")
	}
}

func TestAPendingReportIsResentDuringTheRetryBackoff(t *testing.T) {
	var sent []*models.MailboxPushState
	reports := 0
	w := &WMail{wake: make(chan struct{}, 1), onEvent: func(_ models.JobEventType, body any) error {
		reports++
		sent = append(sent, body.(*models.JobEventPushState).State)
		return nil
	}}
	// A channel was closed, its report refused, and the provider is in backoff.
	w.ID = uuid.New()
	pushRefusals.Store(w.ID, pushRefusal{retryAt: time.Now().Add(time.Hour), reported: time.Now()})
	t.Cleanup(func() { pushRefusals.Delete(w.ID) })
	w.push.unsent.Store(true)
	w.maintainPush(t.Context())
	if reports != 1 || sent[0] != nil || w.push.unsent.Load() {
		t.Fatalf("the pending report was not resent in backoff: reports=%d sent=%v unsent=%v", reports, sent, w.push.unsent.Load())
	}
	w.maintainPush(t.Context())
	if reports != 1 {
		t.Fatalf("a report already taken was sent again: %d", reports)
	}
}

func TestPushRetryWaitsLongerOnARefusalAndHonoursRetryAfter(t *testing.T) {
	now := time.Now()
	fail := func(f errx.SendFailure) error {
		return errx.ProviderCall("op", "", errx.WithSendFailure(errx.ErrMailSendRejected("x"), f))
	}
	if got := pushRetryAt(errors.New("dial tcp: timeout"), now); !got.Equal(now.Add(pushRetryAfter)) {
		t.Fatalf("transport failure retries at %v", got.Sub(now))
	}
	if got := pushRetryAt(fail(errx.SendFailure{Status: 400, Disposition: errx.SendPermanent}), now); !got.Equal(now.Add(pushRefusedRetryAfter)) {
		t.Fatalf("a refused topic retries at %v", got.Sub(now))
	}
	if got := pushRetryAt(fail(errx.SendFailure{Status: 503, Disposition: errx.SendRetry}), now); !got.Equal(now.Add(pushRetryAfter)) {
		t.Fatalf("an outage retries at %v", got.Sub(now))
	}
	later := now.Add(2 * time.Hour)
	if got := pushRetryAt(fail(errx.SendFailure{Status: 429, Disposition: errx.SendThrottle, RetryAt: &later}), now); !got.Equal(later) {
		t.Fatalf("Retry-After cut short: %v", got.Sub(now))
	}
	far := now.Add(30 * 24 * time.Hour)
	if got := pushRetryAt(fail(errx.SendFailure{Status: 429, Disposition: errx.SendThrottle, RetryAt: &far}), now); !got.Equal(now.Add(pushRetryAfterMax)) {
		t.Fatalf("Retry-After not bounded: %v", got.Sub(now))
	}
}

func TestARebuiltMailboxKeepsItsPushBackoff(t *testing.T) {
	id := uuid.New()
	t.Cleanup(func() { pushRefusals.Delete(id) })
	want := &models.AddWorkerEmailPushData{GmailTopic: "projects/p/topics/t"}
	pushRefusals.Store(id, pushRefusal{target: pushTarget(want), retryAt: time.Now().Add(time.Hour), reported: time.Now()})
	// A fresh WMail with no Google client: it must not reach the provider while the backoff holds.
	w := &WMail{ID: id, EmailType: models.InboxProviderGoogle, GoogleData: &GoogleData{}, wake: make(chan struct{}, 1)}
	w.ApplyPush(want)
	w.maintainPush(t.Context())

	other := &models.AddWorkerEmailPushData{GmailTopic: "projects/p/topics/other"}
	if pushTarget(other) == pushTarget(want) {
		t.Fatal("a changed topic is not a new target")
	}
}
