package wmail

import (
	"context"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
)

func newSignalTestMail() *WMail {
	return &WMail{wake: make(chan struct{}, 1), signal: make(chan struct{}, 1)}
}

// A mailbox without a live change signal gets a narrow check on the 5-minute timer.
func TestNoSignalMailboxNarrowPollsOnTheTimer(t *testing.T) {
	w := newSignalTestMail()
	last := time.Now()
	at, _ := w.narrowPoll(last, 1, true)
	if !at.Equal(last.Add(config.SyncNarrowPollInterval)) {
		t.Fatalf("no signal: narrow poll at %v, want five minutes after the last check", at)
	}
	if at, _ := w.narrowPoll(last, 1, false); !at.IsZero() {
		t.Fatal("a narrow poll was scheduled inside a backoff")
	}
	started := time.Now()
	step := w.waitForPass(t.Context(), syncPlan{due: time.Now().Add(time.Hour), narrowAt: time.Now().Add(50 * time.Millisecond), wakeable: true})
	if step != stepNarrow || time.Since(started) > config.SyncWakeSettle+time.Second {
		t.Fatalf("a due narrow poll gave step %v after %v", step, time.Since(started))
	}
	if step := w.waitForPass(t.Context(), syncPlan{due: time.Now().Add(50 * time.Millisecond), narrowAt: time.Now().Add(time.Hour), wakeable: true}); step != stepFull {
		t.Fatalf("a narrow poll after the full pass gave step %v", step)
	}
	spread := map[time.Duration]bool{}
	for range 20 {
		d := jitter(config.SyncNarrowPollInterval)
		if d < config.SyncNarrowPollInterval*9/10 || d > config.SyncNarrowPollInterval*11/10 {
			t.Fatalf("narrow poll gap %v outside 10%%", d)
		}
		spread[d] = true
	}
	if len(spread) < 2 {
		t.Fatal("the narrow poll gap carries no jitter")
	}
}

// A mailbox with IDLE has no narrow timer; a live push channel gets only the 15-minute safety check.
func TestSignalledMailboxDoesNotNarrowPoll(t *testing.T) {
	w := newSignalTestMail()
	w.setImapIdle(true)
	if at, _ := w.narrowPoll(time.Now(), 1, true); !at.IsZero() {
		t.Fatal("an IDLE mailbox was given a narrow poll")
	}

	push := newSignalTestMail()
	expires := time.Now().Add(time.Hour)
	push.ApplyPush(&models.AddWorkerEmailPushData{GraphNotificationURL: "https://x", GraphClientState: "s",
		State: &models.MailboxPushState{Provider: models.MailboxPushGraph, SubscriptionID: "sub", ExpiresAt: expires}})
	last := time.Now()
	at, until := push.narrowPoll(last, 1, true)
	if !at.Equal(last.Add(config.SyncPushSafetyInterval)) || !until.Equal(expires) {
		t.Fatalf("live push channel: narrow poll %v, lapse %v, want the 15-minute safety check", at, until)
	}
	<-push.signal
	// The channel lapsing ends the wait so the timer takes over.
	if step := push.waitForPass(t.Context(), syncPlan{due: time.Now().Add(time.Hour), signalUntil: time.Now().Add(20 * time.Millisecond), wakeable: true}); step != stepReplan {
		t.Fatalf("a lapsing channel gave step %v", step)
	}
	push.push.state.ExpiresAt = time.Now().Add(-time.Second)
	if at, _ := push.narrowPoll(time.Now(), 1, true); at.IsZero() {
		t.Fatal("a lapsed push channel did not fall back to the narrow poll")
	}
}

// Losing IDLE wakes the waiting loop at once and switches the narrow poll on; getting it back switches it off.
func TestLosingIdleSwitchesTheNarrowPollOn(t *testing.T) {
	w := newSignalTestMail()
	w.setImapIdle(true)
	<-w.signal
	done := make(chan syncStep, 1)
	go func() {
		done <- w.waitForPass(context.Background(), syncPlan{due: time.Now().Add(time.Hour), wakeable: true})
	}()
	time.Sleep(20 * time.Millisecond)
	w.setImapIdle(false)
	select {
	case step := <-done:
		if step != stepReplan {
			t.Fatalf("losing IDLE gave step %v", step)
		}
	case <-time.After(time.Second):
		t.Fatal("losing IDLE did not end the wait")
	}
	if at, _ := w.narrowPoll(time.Now(), 1, true); at.IsZero() {
		t.Fatal("no narrow poll after IDLE dropped")
	}
	w.setImapIdle(true)
	if at, _ := w.narrowPoll(time.Now(), 1, true); !at.IsZero() {
		t.Fatal("the narrow poll kept running after IDLE came back")
	}
}
