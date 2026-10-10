package wmail

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/client/goog"
	"github.com/warmbly/warmbly/internal/client/msgraph"
	"github.com/warmbly/warmbly/internal/client/smtpimap/imap"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func seedSyncTruth(w *WMail, relayed *[]models.SyncState) time.Time {
	previous := time.Now().Add(-time.Hour)
	w.tracker = newSyncTracker(&models.SyncState{BackfillStatus: models.SyncBackfillComplete, LastSyncedAt: &previous, Deferred: 5}, func(st models.SyncState) error {
		*relayed = append(*relayed, st)
		return nil
	})
	return previous
}

func TestSyncTruthRepeatedTransportFailuresRelayFreshObservations(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	var events []captured
	w := newGoogleTestMail(t, srv, &events)
	for i := 0; i < 2; i++ {
		if err := w.syncOnce(t.Context()); err == nil {
			t.Fatal("provider failure hidden")
		}
	}
	errors := 0
	for _, event := range events {
		if event.eventType == models.JobEventTypeEmailServerError {
			errors++
		}
	}
	if errors != 2 || w.transportFailures != 2 {
		t.Fatalf("observations=%d failures=%d", errors, w.transportFailures)
	}
}

func assertNoSyncSuccess(t *testing.T, w *WMail, previous time.Time) {
	t.Helper()
	if w.tracker.state.LastSyncedAt == nil || !w.tracker.state.LastSyncedAt.Equal(previous) || w.tracker.state.Deferred != 5 {
		t.Fatalf("incomplete pass replaced successful evidence/backlog: %+v", w.tracker.state)
	}
}

func assertSyncCatchUp(t *testing.T, w *WMail, previous time.Time) {
	t.Helper()
	if w.tracker.state.LastSyncedAt == nil || !w.tracker.state.LastSyncedAt.After(previous) || w.tracker.state.Deferred != 0 {
		t.Fatalf("successful catch-up did not replace old evidence: %+v", w.tracker.state)
	}
}

func TestSyncTruthGraphServiceUnavailableHonorsRetryWindowAndRecovers(t *testing.T) {
	var recovered atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(out http.ResponseWriter, req *http.Request) {
		out.Header().Set("Content-Type", "application/json")
		if !recovered.Load() {
			out.Header().Set("Retry-After", "720")
			out.WriteHeader(http.StatusServiceUnavailable)
			_, _ = out.Write([]byte(`{"error":{"code":"ServiceUnavailable"}}`))
			return
		}
		_, _ = fmt.Fprintf(out, `{"value":[],"@odata.deltaLink":"https://graph.microsoft.com%s?$deltatoken=recovered"}`, req.URL.Path)
	}))
	defer srv.Close()
	var events []captured
	var relayed []models.SyncState
	w := newGraphTestMail(t, srv, &events, &relayed)
	previous := seedSyncTruth(w, &relayed)
	before := w.GraphData.Client.DeltaLinks[msgraph.FolderInbox]

	for range 2 {
		err := w.syncOnce(t.Context())
		if err == nil || err.Code != errx.MailErrorCodeServerUnreachable || err.RetryAfter != 12*time.Minute {
			t.Fatalf("missing retryable provider evidence: %v", err)
		}
		if delay := w.nextSyncDelay(time.Minute, err); delay < err.RetryAfter {
			t.Fatalf("service retry window undercut: %v < %v", delay, err.RetryAfter)
		}
		assertNoSyncSuccess(t, w, previous)
		if w.GraphData.Client.DeltaLinks[msgraph.FolderInbox] != before {
			t.Fatal("failed service request advanced delta cursor")
		}
	}
	if w.transportFailures != 2 {
		t.Fatalf("failed passes reset transport evidence: %d", w.transportFailures)
	}
	recovered.Store(true)
	if err := w.syncOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertSyncCatchUp(t, w, previous)
	if w.transportFailures != 0 || w.GraphData.Client.DeltaLinks[msgraph.FolderInbox] == before {
		t.Fatal("provider-confirmed complete pass failed to update recovery state")
	}
}

func TestSyncTruthImapFolderFailurePreservesSavedStateUntilRecovery(t *testing.T) {
	box := &models.Mailbox{Name: "Sent", UIDValidity: 7, UIDNext: 10, HighestModSeq: 30}
	conn := &fakeImapConn{folders: []models.Mailbox{*box}, folderErr: errx.ErrMailServerUnreachable}
	w, events := newIMAPTestMail(conn, &fixedBudget{allow: 100}, box)
	w.EmailType = models.InboxProviderSMTPIMAP
	var relayed []models.SyncState
	previous := seedSyncTruth(w, &relayed)
	w.tracker.state.BackfillCursor.Folders = map[string]models.SyncFolderCursor{"Sent": {Done: true, UID: 7}}
	for range 2 {
		if err := w.syncOnce(t.Context()); err == nil || err.Code != errx.MailErrorCodeServerUnreachable {
			t.Fatalf("folder failure hidden: %v", err)
		}
		assertNoSyncSuccess(t, w, previous)
		if hasEvent(*events, models.JobEventTypeMailboxDelete) || len(w.SmtpImapData.Mailboxes) != 1 || w.SmtpImapData.Mailboxes[0] != box {
			t.Fatal("failed folder listing retired saved mailbox state")
		}
		if cursor, ok := w.tracker.state.BackfillCursor.Folders["Sent"]; !ok || cursor.UID != 7 || !cursor.Done {
			t.Fatal("failed folder listing lost the durable recovery cursor")
		}
	}
	conn.folderErr = nil
	if err := w.syncOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertSyncCatchUp(t, w, previous)
	if hasEvent(*events, models.JobEventTypeMailboxDelete) || w.transportFailures != 0 {
		t.Fatal("complete provider recovery did not preserve folder state")
	}
}

func TestSyncTruthGoogleFailureHeartbeatAndRecovery(t *testing.T) {
	failing := true
	srv := httptest.NewServer(http.HandlerFunc(func(out http.ResponseWriter, _ *http.Request) {
		out.Header().Set("Content-Type", "application/json")
		if failing {
			out.WriteHeader(http.StatusServiceUnavailable)
			_, _ = out.Write([]byte(`{"error":{"code":503,"message":"retry later"}}`))
			return
		}
		_, _ = out.Write([]byte(`{"historyId":"200"}`))
	}))
	defer srv.Close()
	var events []captured
	w := newGoogleTestMail(t, srv, &events)
	w.GoogleData.LastHistoryID = 100
	var relayed []models.SyncState
	previous := seedSyncTruth(w, &relayed)
	for range 2 {
		w.tracker.lastSent = time.Now().Add(-2 * syncStateHeartbeat)
		if err := w.SyncGoogle(t.Context()); err == nil {
			t.Fatal("expected provider error")
		}
		assertNoSyncSuccess(t, w, previous)
	}
	if len(relayed) != 2 {
		t.Fatalf("failed passes must relay safe heartbeat state: %d", len(relayed))
	}
	for _, st := range relayed {
		if !st.LastSyncedAt.Equal(previous) || st.Deferred != 5 {
			t.Fatalf("false success escaped via heartbeat: %+v", st)
		}
	}
	failing = false
	if err := w.SyncGoogle(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertSyncCatchUp(t, w, previous)
}

type syncTruthMap struct {
	repository.EmailMessageMapRepository
	failGet bool
	failDel bool
}

func (r *syncTruthMap) Get(context.Context, uuid.UUID, uuid.UUID, string) (*repository.EmailMessageData, error) {
	if r.failGet {
		return nil, errors.New("map unavailable")
	}
	return nil, nil
}

func (r *syncTruthMap) Del(context.Context, uuid.UUID, uuid.UUID, string, uuid.UUID) error {
	if r.failDel {
		return errors.New("unmap unavailable")
	}
	return nil
}

func TestSyncTruthGraphAbortedAdmissionAndCheckpoint(t *testing.T) {
	for _, failure := range []string{"map", "checkpoint"} {
		t.Run(failure, func(t *testing.T) {
			g := newFakeGraph()
			var events []captured
			var relayed []models.SyncState
			w := newGraphTestMail(t, g.serve(t), &events, &relayed)
			previous := seedSyncTruth(w, &relayed)
			before := w.GraphData.Client.DeltaLinks[msgraph.FolderInbox]
			r := &syncTruthMap{failGet: failure == "map"}
			w.EmailMessageMapRepository = r
			failing := true
			w.onEvent = func(kind models.JobEventType, _ any) error {
				if failing && failure == "checkpoint" && kind == models.JobEventTypeGraphDeltaUpdate {
					return errors.New("checkpoint unavailable")
				}
				return nil
			}
			for range 2 {
				if failure == "map" {
					g.live = []string{"new-arrival"}
				}
				w.tracker.lastSent = time.Now().Add(-2 * syncStateHeartbeat)
				_ = w.SyncGraph(t.Context())
				assertNoSyncSuccess(t, w, previous)
				if w.GraphData.Client.DeltaLinks[msgraph.FolderInbox] != before {
					t.Fatal("failed/aborted page advanced local checkpoint")
				}
			}
			failing, r.failGet = false, false
			if err := w.SyncGraph(t.Context()); err != nil {
				t.Fatal(err)
			}
			assertSyncCatchUp(t, w, previous)
		})
	}
}

func TestSyncTruthRetryUnmapAndCancellation(t *testing.T) {
	for _, provider := range []string{"google", "graph", "imap"} {
		t.Run(provider, func(t *testing.T) {
			w, _ := newIMAPTestMail(&fakeImapConn{}, &fixedBudget{}, &models.Mailbox{})
			var relayed []models.SyncState
			previous := seedSyncTruth(w, &relayed)
			w.unmapPending = map[string]uuid.UUID{"failed-arrival": uuid.New()}
			w.EmailMessageMapRepository = &syncTruthMap{failDel: true}
			switch provider {
			case "google":
				_ = w.SyncGoogle(t.Context())
			case "graph":
				_ = w.SyncGraph(t.Context())
			case "imap":
				_ = w.Sync(t.Context())
			}
			assertNoSyncSuccess(t, w, previous)
			if len(relayed) != 1 {
				t.Fatal("retry-unmap blockage did not relay safe state")
			}
		})
	}
	w, _ := newIMAPTestMail(&fakeImapConn{}, &fixedBudget{}, &models.Mailbox{})
	var relayed []models.SyncState
	previous := seedSyncTruth(w, &relayed)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_ = w.Sync(ctx)
	assertNoSyncSuccess(t, w, previous)
}

func TestSyncTruthImapFlagFailureRetainsBothUpdatesAndSuccessEvidence(t *testing.T) {
	box := &models.Mailbox{Name: "INBOX", UIDValidity: 7, UIDNext: 101}
	conn := &fakeImapConn{noCondStore: true, folders: []models.Mailbox{*box}, flags: map[uint32]imap.FlagState{
		1: {MessageID: "one", Flags: nil}, 2: {MessageID: "two", Flags: nil},
	}}
	w, events := newIMAPTestMail(conn, &fixedBudget{allow: 10}, box)
	w.EmailMessageMapRepository = knownMessageMap{id: uuid.NewString()}
	if err := w.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	var relayed []models.SyncState
	previous := seedSyncTruth(w, &relayed)
	conn.flags[1], conn.flags[2] = imap.FlagState{MessageID: "one", Flags: []string{models.FlagSeen}}, imap.FlagState{MessageID: "two", Flags: []string{models.FlagSeen}}
	w.flagScan[box.Name].at = time.Now().Add(-2 * config.ImapFlagScanInterval)
	failing := true
	w.onEvent = func(kind models.JobEventType, body any) error {
		if kind == models.JobEventTypeEmailUpdate && failing {
			return errors.New("publisher unavailable")
		}
		*events = append(*events, captured{eventType: kind, body: body})
		return nil
	}
	if err := w.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertNoSyncSuccess(t, w, previous)
	for uid := range conn.flags {
		if w.flagScan[box.Name].flags[uid] != flagFingerprint(nil) {
			t.Fatal("failed/unvisited flags were absorbed into baseline")
		}
	}
	failing = false
	w.flagScan[box.Name].at = time.Now().Add(-2 * config.ImapFlagScanInterval)
	if err := w.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	updates := 0
	for _, e := range *events {
		if e.eventType == models.JobEventTypeEmailUpdate {
			updates++
		}
	}
	if updates != 2 {
		t.Fatalf("unchanged provider flags did not retry both updates: %d", updates)
	}
	assertSyncCatchUp(t, w, previous)
}

func TestSyncTruthPanicDoesNotResetTransportFailure(t *testing.T) {
	w, _ := newIMAPTestMail(&fakeImapConn{}, &fixedBudget{}, &models.Mailbox{})
	w.EmailType = models.InboxProviderGoogle
	w.GoogleData = nil // Fault inside the provider pass, after beginTick.
	w.transportFailures = 2
	var relayed []models.SyncState
	previous := seedSyncTruth(w, &relayed)
	if err := w.syncOnce(t.Context()); err == nil {
		t.Fatal("recovered panic returned successful outcome")
	}
	assertNoSyncSuccess(t, w, previous)
	if w.transportFailures != 2 {
		t.Fatal("panic reset existing transport warning/backoff")
	}
}

func TestSyncTruthBoundedAndPinnedProviderPasses(t *testing.T) {
	for _, provider := range []string{"google", "graph"} {
		for _, outcome := range []string{"bounded", "pinned", "stop"} {
			t.Run(provider+"/"+outcome, func(t *testing.T) {
				caughtUp := false
				srv := httptest.NewServer(http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) {
					out.Header().Set("Content-Type", "application/json")
					if provider == "google" {
						if caughtUp {
							_, _ = out.Write([]byte(`{"historyId":"200"}`))
							return
						}
						if outcome == "bounded" {
							_, _ = out.Write([]byte(`{"historyId":"200","nextPageToken":"next"}`))
							return
						}
						_, _ = out.Write([]byte(`{"historyId":"200","history":[{"id":"150","messagesAdded":[{"message":{"id":"new-arrival","threadId":"thread"}}]}]}`))
						return
					}
					folder := msgraph.FolderInbox
					if caughtUp {
						_, _ = fmt.Fprintf(out, `{"value":[],"@odata.deltaLink":"https://graph.microsoft.com/v1.0/me/mailFolders/%s/messages/delta?final=1"}`, folder)
						return
					}
					if outcome == "bounded" {
						_, _ = fmt.Fprintf(out, `{"value":[],"@odata.nextLink":"https://graph.microsoft.com%s?next=1"}`, r.URL.Path)
						return
					}
					_, _ = fmt.Fprintf(out, `{"value":[{"id":"new-arrival","isRead":false}],"@odata.deltaLink":"https://graph.microsoft.com/v1.0/me/mailFolders/%s/messages/delta?final=1"}`, folder)
				}))
				defer srv.Close()
				var events []captured
				var relayed []models.SyncState
				var w *WMail
				var sync func(context.Context) *errx.MailError
				if provider == "google" {
					w = newGoogleTestMail(t, srv, &events)
					w.GoogleData.LastHistoryID = 100
					w.GoogleData.Client.OnMessageAdded = func(context.Context, string, string) (bool, error) {
						if outcome == "stop" {
							return false, goog.ErrStop
						}
						return false, nil
					}
					sync = w.SyncGoogle
				} else {
					w = newGraphTestMail(t, srv, &events, &relayed)
					w.GraphData.Client.OnMessageSeen = func(context.Context, string, string, bool) (bool, error) {
						if outcome == "stop" {
							return false, msgraph.ErrStop
						}
						return false, nil
					}
					sync = w.SyncGraph
				}
				previous := seedSyncTruth(w, &relayed)
				w.transportFailures = 2
				if err := sync(t.Context()); err != nil {
					t.Fatal(err)
				}
				assertNoSyncSuccess(t, w, previous)
				caughtUp = true
				if err := sync(t.Context()); err != nil {
					t.Fatal(err)
				}
				assertSyncCatchUp(t, w, previous)
			})
		}
	}
}

func TestSyncTruthGoogleCheckpointFailureAndDirtyRelayRetry(t *testing.T) {
	failingProvider := false
	srv := httptest.NewServer(http.HandlerFunc(func(out http.ResponseWriter, _ *http.Request) {
		out.Header().Set("Content-Type", "application/json")
		if failingProvider {
			out.WriteHeader(503)
			_, _ = out.Write([]byte(`{"error":{"code":503,"message":"injected"}}`))
			return
		}
		_, _ = out.Write([]byte(`{"historyId":"200"}`))
	}))
	defer srv.Close()
	var events []captured
	w := newGoogleTestMail(t, srv, &events)
	w.GoogleData.LastHistoryID = 100
	var relayed []models.SyncState
	previous := seedSyncTruth(w, &relayed)
	w.transportFailures = 2
	w.onEvent = func(kind models.JobEventType, _ any) error {
		if kind == models.JobEventTypeHistoryIDUpdate {
			return errors.New("checkpoint unavailable")
		}
		return nil
	}
	if err := w.syncOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertNoSyncSuccess(t, w, previous)
	if w.GoogleData.LastHistoryID != 100 || w.transportFailures != 2 {
		t.Fatal("failed checkpoint was installed or claimed recovery")
	}
	failingProvider = true
	expired := time.Now().Add(-time.Minute)
	w.tracker.state.ThrottledUntil = &expired
	w.tracker.lastSent = time.Now().Add(-2 * syncStateHeartbeat)
	w.tracker.emit = func(st models.SyncState) error { return errors.New("state relay unavailable") }
	_ = w.SyncGoogle(t.Context())
	assertNoSyncSuccess(t, w, previous)
	if !w.tracker.dirty || w.tracker.state.ThrottledUntil != nil {
		t.Fatal("failed relay forgot dirty throttle progress")
	}
	w.tracker.emit = func(st models.SyncState) error { relayed = append(relayed, st); return nil }
	_ = w.SyncGoogle(t.Context())
	assertNoSyncSuccess(t, w, previous)
	if w.tracker.dirty || relayed[len(relayed)-1].ThrottledUntil != nil {
		t.Fatal("safe dirty-state relay did not retry")
	}
}
