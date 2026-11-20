package wmail

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/nodeevidence"
)

func TestRecoveryProbeRetiredSyncIsNotProviderUnreachable(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	w := &WMail{Ctx: ctx, Cancel: cancel}
	w.Stop()
	observations := 0
	nodeevidence.Install(func(e nodeevidence.Event) {
		if e.Name == nodeevidence.ProviderUnreachable {
			observations++
		}
	})
	defer nodeevidence.Install(nil)
	w.StartSyncWorker(w.Ctx)
	if observations != 0 {
		t.Fatalf("retired generation emitted %d provider-unreachable observations without a provider call", observations)
	}
}

func TestRecoveryProbeCallerCancellationPreservesErrorWithoutProviderEvidence(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reports, observations := 0, 0
	w := &WMail{Ctx: context.Background(), transportFailures: 2, onEvent: func(models.JobEventType, any) error { reports++; return nil }}
	w.tracker = newSyncTracker(nil, func(models.SyncState) error { t.Fatal("cancelled pass published state"); return nil })
	nodeevidence.Install(func(nodeevidence.Event) { observations++ })
	defer nodeevidence.Install(nil)
	if err := w.syncOnce(ctx, false); err != errx.ErrMailServerUnreachable {
		t.Fatal("cancellation return identity changed")
	}
	if reports != 0 || observations != 0 || w.transportFailures != 2 || w.tracker.tickComplete || w.tracker.state.LastSyncedAt != nil {
		t.Fatal("cancellation claimed provider failure or success")
	}
}

func TestRecoveryProbeMissingStateTransportPassThenRecovery(t *testing.T) {
	conn := &fakeImapConn{folderErr: errx.ErrMailServerUnreachable}
	w, events := newIMAPTestMail(conn, &fixedBudget{allow: 100}, &models.Mailbox{Name: "INBOX"})
	w.EmailType = models.InboxProviderSMTPIMAP
	var relayed []models.SyncState
	w.tracker = newSyncTracker(nil, func(st models.SyncState) error { relayed = append(relayed, st); return nil })
	for range 2 {
		if err := w.syncOnce(t.Context(), false); err != errx.ErrMailServerUnreachable {
			t.Fatal("transport error identity changed")
		}
		if w.tracker.state.LastSyncedAt != nil || w.tracker.tickComplete {
			t.Fatal("failed pass invented provider completion")
		}
	}
	if len(relayed) == 0 || relayed[0].LastSyncedAt != nil || len(mailboxEvents(*events, models.JobEventTypeEmailServerError)) != 2 {
		t.Fatal("live failed pass did not flush safe state and error observations")
	}
	conn.folderErr = nil
	if err := w.syncOnce(t.Context(), false); err != nil {
		t.Fatal(err)
	}
	if w.tracker.state.LastSyncedAt == nil || !w.tracker.tickComplete || w.transportFailures != 0 {
		t.Fatal("restored provider did not recover")
	}
}

func TestRecoveryProbeGraphMalformedPassRetainsCompletionAndRetries(t *testing.T) {
	var broken atomic.Bool
	broken.Store(true)
	srv := httptest.NewServer(http.HandlerFunc(func(out http.ResponseWriter, req *http.Request) {
		out.Header().Set("Content-Type", "application/json")
		if broken.Load() {
			_, _ = out.Write([]byte(`{"value":`))
			return
		}
		_, _ = fmt.Fprintf(out, `{"value":[],"@odata.deltaLink":"https://graph.microsoft.com%s?$deltatoken=recovered"}`, req.URL.Path)
	}))
	defer srv.Close()
	var events []captured
	var relayed []models.SyncState
	w := newGraphTestMail(t, srv, &events, &relayed)
	previous := seedSyncTruth(w, &relayed)
	w.transportFailures = 2
	if err := w.syncOnce(t.Context(), false); err != nil {
		t.Fatal("non-MailError behavior changed")
	}
	assertNoSyncSuccess(t, w, previous)
	if len(relayed) == 0 || w.transportFailures != 2 {
		t.Fatal("incomplete pass failed safe heartbeat")
	}
	if len(mailboxEvents(events, models.JobEventTypeEmailServerError)) != 0 {
		t.Fatal("raw Graph decode failure fabricated transport report")
	}
	broken.Store(false)
	if err := w.syncOnce(t.Context(), false); err != nil {
		t.Fatal(err)
	}
	assertSyncCatchUp(t, w, previous)
}

func TestRecoveryProbeWrappedTransportRelayAndRetryFloor(t *testing.T) {
	mailErr := *errx.ErrMailServerUnreachable
	mailErr.RetryAfter = 12 * time.Minute
	err := fmt.Errorf("bounded fake wrapper: %w", &mailErr)
	var events []captured
	w := &WMail{onEvent: func(kind models.JobEventType, body any) error {
		events = append(events, captured{eventType: kind, body: body})
		return nil
	}}
	if reportable(err) {
		t.Fatal("wrapped retryable condition escalated to application exception")
	}
	w.CaptureError(err)
	if len(events) != 1 || events[0].eventType != models.JobEventTypeEmailServerError || events[0].body.(models.EmailErrorEvent).ErrorCode != string(errx.MailErrorCodeServerUnreachable) {
		t.Fatal("wrapped MailError was not relayed")
	}
	if delay := w.nextSyncDelay(time.Minute, &mailErr); delay < mailErr.RetryAfter {
		t.Fatal("provider retry interval shortened")
	}
}
