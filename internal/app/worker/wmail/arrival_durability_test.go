package wmail

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	goimap "github.com/emersion/go-imap/v2"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/client/msgraph"
	"github.com/warmbly/warmbly/internal/client/smtpimap/imap"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type durableTestMap struct {
	recoveryMessageMap
	pending         map[string]*repository.PendingArrival
	failAfterCommit bool
	unsupported     bool
	admitErr        error
}

type completedArrivalMap struct {
	recoveryMessageMap
	admissions int
	lost       bool
	reads      int
	readErrAt  int
}

func (m *completedArrivalMap) Get(ctx context.Context, userID, emailID uuid.UUID, messageID string) (*repository.EmailMessageData, error) {
	m.reads++
	if m.readErrAt == m.reads {
		return nil, errors.New("canonical map lookup unavailable")
	}
	return m.recoveryMessageMap.Get(ctx, userID, emailID, messageID)
}

func (m *completedArrivalMap) AdmitArrival(_ context.Context, data repository.EmailMessageData, _ *repository.PendingArrival) error {
	m.admissions++
	if _, exists := m.data[data.MessageID]; exists {
		return repository.ErrArrivalAdmissionUnconfirmed
	}
	m.data[data.MessageID] = data
	if m.lost {
		return errors.New("successful admission response lost after consumer drain")
	}
	return nil
}

func TestCompletedArrivalLostResponseReofferUsesKnownMapAcrossProviders(t *testing.T) {
	for _, provider := range []string{"google", "graph", "imap"} {
		t.Run(provider, func(t *testing.T) {
			var events []captured
			var w *WMail
			var offer func() (bool, error)
			key := "provider-id"
			switch provider {
			case "google":
				w = newGoogleTestMail(t, (&fakeGmail{}).serve(t), &events)
				offer = func() (bool, error) { return w.onGoogleMessageAdded(t.Context(), key, "thread") }
			case "graph":
				var relayed []models.SyncState
				w = newGraphTestMail(t, newFakeGraph().serve(t), &events, &relayed)
				offer = func() (bool, error) { return w.onGraphMessageSeen(t.Context(), msgraph.FolderInbox, key, false) }
			case "imap":
				key = "<arrival@test.local>"
				var captured *[]captured
				w, captured = newIMAPTestMail(&fakeImapConn{}, &fixedBudget{allow: 10}, &models.Mailbox{Name: "INBOX", UIDValidity: 1})
				offer = func() (bool, error) {
					ok, err := w.imapApply(t.Context(), []*imap.Fetched{{Email: &models.EmailMessageData{MessageID: key, UID: 1}}}, false, &tickStats{}, nil)
					events = *captured
					if err != nil {
						return ok, err
					}
					return ok, nil
				}
			}
			m := &completedArrivalMap{recoveryMessageMap: recoveryMessageMap{data: map[string]repository.EmailMessageData{}}, lost: true}
			w.EmailMessageMapRepository = m
			storage := &retainedArrivalStore{body: map[string][]byte{}}
			w.Storage = storage
			if ok, err := offer(); ok || (provider != "imap" && err == nil) || m.admissions != 1 || len(m.data) != 1 {
				t.Fatalf("lost response did not retain canonical admission: ok=%t err=%v calls=%d maps=%d", ok, err, m.admissions, len(m.data))
			}
			canonical := m.data[key].ID
			w.googleTick, w.graphTick = nil, nil
			if ok, err := offer(); !ok || err != nil {
				t.Fatalf("completed admission was retried instead of known-map path: ok=%t err=%v", ok, err)
			}
			if m.admissions != 1 || m.data[key].ID != canonical || len(storage.body) != 1 || storage.deletes != 0 || len(newEmails(events)) != 0 {
				t.Fatal("known reoffer rewrote admission identity/body or published another arrival")
			}
		})
	}
}

func TestIMAPBatchDuplicateCompletedArrivalUsesCanonicalMap(t *testing.T) {
	for _, backfill := range []bool{false, true} {
		t.Run(fmt.Sprintf("backfill=%t", backfill), func(t *testing.T) {
			budget := &fixedBudget{allow: 10}
			w, events := newIMAPTestMail(&fakeImapConn{}, budget, &models.Mailbox{Name: "INBOX", UIDValidity: 1})
			m := &completedArrivalMap{recoveryMessageMap: recoveryMessageMap{data: map[string]repository.EmailMessageData{}}}
			w.EmailMessageMapRepository = m
			storage := &retainedArrivalStore{body: map[string][]byte{}}
			w.Storage = storage
			key := "<duplicate@test.local>"
			stats := &tickStats{}
			ok, err := w.imapApply(t.Context(), []*imap.Fetched{
				{Email: &models.EmailMessageData{MessageID: key, UID: 1, ModSeq: 3, Flags: []string{"\\Seen"}}},
				{Email: &models.EmailMessageData{MessageID: key, UID: 2}},
			}, backfill, stats, nil)
			if !ok || err != nil {
				t.Fatalf("same-batch duplicate retried completed admission: ok=%t err=%v calls=%d", ok, err, m.admissions)
			}
			if m.admissions != 1 || len(m.data) != 1 || len(storage.body) != 1 || storage.deletes != 0 || budget.admitted != 1 {
				t.Fatalf("duplicate re-admitted, re-budgeted or lost body: calls=%d maps=%d bodies=%d budget=%d", m.admissions, len(m.data), len(storage.body), budget.admitted)
			}
			updates := 0
			for _, e := range *events {
				if e.eventType == models.JobEventTypeEmailUpdate {
					updates++
					u := e.body.(*models.JobEventEmailUpdate)
					if u.ID.String() != m.data[key].ID || u.UID != 1 || u.ModSeq != 3 || len(u.Flags) != 1 || u.Flags[0] != "\\Seen" {
						t.Fatal("duplicate metadata lost canonical identity, UID or flags")
					}
				}
			}
			if len(newEmails(*events)) != 0 || (!backfill && (updates != 1 || stats.seen != 1)) || (backfill && (updates != 0 || stats.seen != 0)) {
				t.Fatal("duplicate published another arrival, skipped reconciliation or inflated flood observation")
			}
		})
	}
}

func TestIMAPBatchDuplicateCanonicalLookupFailureHoldsPass(t *testing.T) {
	for _, backfill := range []bool{false, true} {
		t.Run(fmt.Sprintf("backfill=%t", backfill), func(t *testing.T) {
			budget := &fixedBudget{allow: 10}
			w, events := newIMAPTestMail(&fakeImapConn{}, budget, &models.Mailbox{Name: "INBOX", UIDValidity: 1})
			m := &completedArrivalMap{recoveryMessageMap: recoveryMessageMap{data: map[string]repository.EmailMessageData{}}, readErrAt: 3}
			w.EmailMessageMapRepository = m
			storage := &retainedArrivalStore{body: map[string][]byte{}}
			w.Storage = storage
			stats := &tickStats{}
			key := "<duplicate@test.local>"
			ok, err := w.imapApply(t.Context(), []*imap.Fetched{
				{Email: &models.EmailMessageData{MessageID: key, UID: 1}},
				{Email: &models.EmailMessageData{MessageID: key, UID: 2}},
			}, backfill, stats, nil)
			if ok || err != nil || !stats.aborted || m.admissions != 1 || len(storage.body) != 1 || storage.deletes != 0 || budget.admitted != 1 || len(newEmails(*events)) != 0 {
				t.Fatal("failed canonical lookup re-admitted duplicate, lost body or completed the pass")
			}
		})
	}
}

func (m *durableTestMap) AdmitArrival(ctx context.Context, data repository.EmailMessageData, p *repository.PendingArrival) error {
	if m.admitErr != nil {
		return m.admitErr
	}
	if m.unsupported {
		return repository.ErrArrivalOutboxUnsupported
	}
	if _, exists := m.data[data.MessageID]; !exists {
		m.data[data.MessageID] = data
		m.pending[data.MessageID] = p
	}
	if m.failAfterCommit {
		return errors.New("admission response lost")
	}
	return nil
}

type retainedArrivalStore struct {
	fakeStore
	body    map[string][]byte
	deletes int
}

func (s *retainedArrivalStore) Put(_ context.Context, key string, body io.Reader, _ string) error {
	data, err := io.ReadAll(body)
	if err == nil {
		s.body[key] = data
	}
	return err
}

func (s *retainedArrivalStore) Delete(context.Context, string) error {
	s.deletes++
	return nil
}

func TestArrivalOwnershipLossRetiresOnlyExactGenerationAcrossProviders(t *testing.T) {
	for _, tc := range []struct {
		name     string
		provider models.InboxProvider
		store    func(*WMail, context.Context, *models.EmailMessageData) error
	}{
		{"google", models.InboxProviderGoogle, (*WMail).googleStore},
		{"graph", models.InboxProviderOutlook, (*WMail).graphStore},
		{"imap", models.InboxProviderSMTPIMAP, (*WMail).imapStore},
	} {
		for _, loss := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/ownershipLost=%t", tc.name, loss), func(t *testing.T) {
				failure := errors.New("arrival admission not confirmed: status 503")
				if loss {
					failure = fmt.Errorf("wrapped: %w", repository.ErrArrivalMailboxOwnershipLost)
				}
				m := &durableTestMap{recoveryMessageMap: recoveryMessageMap{data: map[string]repository.EmailMessageData{}}, pending: map[string]*repository.PendingArrival{}, admitErr: failure}
				var events []captured
				w := newTestWMail(t, tc.provider, &events)
				w.EmailMessageMapRepository = m
				storage := &retainedArrivalStore{body: map[string][]byte{}}
				w.Storage = storage
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				w.Ctx, w.Cancel = ctx, cancel
				terminations := 0
				w.TerminateFunc = func() { terminations++ }
				msg := &models.EmailMessageData{MessageID: "<held@test.local>", GmailID: "held-provider-id", BodyPlain: "retained body"}
				if err := tc.store(w, ctx, msg); !errors.Is(err, failure) {
					t.Fatalf("failed arrival was acknowledged: %v", err)
				}
				if len(storage.body) != 1 || storage.deletes != 0 || len(m.data) != 0 || len(m.pending) != 0 || len(events) != 0 {
					t.Fatal("failed admission discarded body, created mapping or published success")
				}
				if (terminations == 1) != loss || (ctx.Err() != nil) != loss {
					t.Fatal("ordinary failure retired mailbox or authoritative absence did not")
				}
				done, allowed := w.BeginExecution()
				if allowed == loss {
					t.Fatal("retired mailbox admitted new execution or retryable mailbox stopped")
				}
				if allowed {
					done()
				}
			})
		}
	}
}

func TestArrivalOwnershipLossHoldsIMAPCursorAndSyncTruth(t *testing.T) {
	for _, loss := range []bool{false, true} {
		t.Run(fmt.Sprintf("ownershipLost=%t", loss), func(t *testing.T) {
			failure := errors.New("arrival admission not confirmed: status 503")
			if loss {
				failure = repository.ErrArrivalMailboxOwnershipLost
			}
			box := &models.Mailbox{Name: "INBOX", UIDValidity: 1, UIDNext: 1, HighestModSeq: 1}
			conn := &fakeImapConn{folders: []models.Mailbox{{Name: "INBOX", UIDValidity: 1, UIDNext: 2, HighestModSeq: 2}}, changed: []goimap.UID{1}}
			w, events := newIMAPTestMail(conn, &fixedBudget{allow: 10}, box)
			w.EmailMessageMapRepository = &durableTestMap{recoveryMessageMap: recoveryMessageMap{data: map[string]repository.EmailMessageData{}}, pending: map[string]*repository.PendingArrival{}, admitErr: failure}
			storage := &retainedArrivalStore{body: map[string][]byte{}}
			w.Storage = storage
			last := time.Now().Add(-time.Hour)
			w.tracker.state.LastSyncedAt = &last
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			w.Ctx, w.Cancel = ctx, cancel
			terminated := false
			w.TerminateFunc = func() { terminated = true }
			if err := w.Sync(ctx); err != nil {
				t.Fatal(err)
			}
			if box.UIDNext != 1 || box.HighestModSeq != 1 || !w.tracker.state.LastSyncedAt.Equal(last) || w.tracker.tickComplete || len(newEmails(*events)) != 0 {
				t.Fatal("failed admission advanced cursor, sync success or arrival acknowledgment")
			}
			if len(storage.body) != 1 || storage.deletes != 0 || terminated != loss {
				t.Fatal("incorrect body retention or mailbox retirement")
			}
		})
	}
}

func TestArrivalOwnershipLossHoldsOAuthProviderCheckpoints(t *testing.T) {
	for _, provider := range []string{"google", "graph"} {
		for _, loss := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/ownershipLost=%t", provider, loss), func(t *testing.T) {
				var events []captured
				var relayed []models.SyncState
				var w *WMail
				if provider == "google" {
					srv := httptest.NewServer(http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) {
						out.Header().Set("Content-Type", "application/json")
						if strings.HasSuffix(r.URL.Path, "/history") {
							_, _ = out.Write([]byte(`{"history":[{"id":"101","messagesAdded":[{"message":{"id":"new","threadId":"thread"}}]}],"historyId":"200"}`))
							return
						}
						_, _ = out.Write([]byte(`{"id":"new","threadId":"thread","labelIds":["INBOX"],"payload":{"headers":[{"name":"Message-ID","value":"<new@test.local>"}]}}`))
					}))
					defer srv.Close()
					w = newGoogleTestMail(t, srv, &events)
					w.GoogleData.LastHistoryID = 100
				} else {
					g := newFakeGraph()
					g.live = []string{"new"}
					w = newGraphTestMail(t, g.serve(t), &events, &relayed)
				}
				previous := seedSyncTruth(w, &relayed)
				failure := errors.New("arrival admission not confirmed: status 503")
				if loss {
					failure = repository.ErrArrivalMailboxOwnershipLost
				}
				w.EmailMessageMapRepository = &durableTestMap{recoveryMessageMap: recoveryMessageMap{data: map[string]repository.EmailMessageData{}}, pending: map[string]*repository.PendingArrival{}, admitErr: failure}
				storage := &retainedArrivalStore{body: map[string][]byte{}}
				w.Storage = storage
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				w.Ctx, w.Cancel = ctx, cancel
				terminated := false
				w.TerminateFunc = func() { terminated = true }
				if provider == "google" {
					if err := w.SyncGoogle(ctx); err != nil {
						t.Fatal(err)
					}
					if w.GoogleData.LastHistoryID != 100 {
						t.Fatal("failed admission advanced Gmail history")
					}
				} else {
					before := w.GraphData.Client.DeltaLinks[msgraph.FolderInbox]
					if err := w.SyncGraph(ctx); err != nil {
						t.Fatal(err)
					}
					if w.GraphData.Client.DeltaLinks[msgraph.FolderInbox] != before {
						t.Fatal("failed admission advanced Graph delta")
					}
				}
				assertNoSyncSuccess(t, w, previous)
				if len(storage.body) != 1 || storage.deletes != 0 || terminated != loss || w.tracker.tickComplete {
					t.Fatal("incorrect retention, retirement or sync success")
				}
				for _, event := range events {
					if event.eventType == models.JobEventTypeNewEmail || event.eventType == models.JobEventTypeHistoryIDUpdate || event.eventType == models.JobEventTypeGraphDeltaUpdate {
						t.Fatal("failed admission published arrival or advanced checkpoint")
					}
				}
			})
		}
	}
}

func TestArrivalDurabilityRestartPreservesIDAcrossProviders(t *testing.T) {
	for _, tc := range []struct {
		name     string
		provider models.InboxProvider
		store    func(*WMail, context.Context, *models.EmailMessageData) error
		key      string
	}{
		{"google", models.InboxProviderGoogle, (*WMail).googleStore, "provider-id"},
		{"graph", models.InboxProviderOutlook, (*WMail).graphStore, "provider-id"},
		{"imap", models.InboxProviderSMTPIMAP, (*WMail).imapStore, "<arrival@test.local>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			m := &durableTestMap{recoveryMessageMap: recoveryMessageMap{data: map[string]repository.EmailMessageData{}}, pending: map[string]*repository.PendingArrival{}, failAfterCommit: true}
			var events []captured
			w := newTestWMail(t, tc.provider, &events)
			w.EmailMessageMapRepository = m
			msg := &models.EmailMessageData{MessageID: "<arrival@test.local>", GmailID: "provider-id", Subject: "arrival", BodyPlain: "complete body", InReplyTo: []string{"<send@test.local>"}}
			if err := tc.store(w, ctx, msg); err == nil {
				t.Fatal("expected ambiguous response")
			}
			canonical, err := m.Get(ctx, w.UserID, w.ID, tc.key)
			if err != nil || canonical == nil {
				t.Fatalf("durable mapping missing: %v", err)
			}
			original := canonical.ID
			if len(events) != 0 || len(m.pending) != 1 {
				t.Fatal("durable arrival must not depend on worker publication")
			}
			fresh := newTestWMail(t, tc.provider, &events)
			fresh.UserID = w.UserID
			fresh.ID = w.ID
			fresh.EmailMessageMapRepository = m
			m.failAfterCommit = false
			if !fresh.retryUnmap(ctx) {
				t.Fatal("fresh worker failed")
			}
			// Re-offering after a lost response cannot replace the queued canonical ID.
			if err := tc.store(fresh, ctx, msg); err != nil {
				t.Fatal(err)
			}
			p := m.pending[tc.key]
			if len(m.pending) != 1 || p.Arrival.Message.ID.String() != original || m.data[tc.key].ID != original || len(events) != 0 {
				t.Fatal("restart reminted the arrival or published outside durable delivery")
			}
			if p.Arrival.UserID != w.UserID || p.Arrival.Message.EmailID != w.ID || p.Arrival.Message.ID == uuid.Nil {
				t.Fatal("ownership not retained")
			}
		})
	}
}

func TestArrivalOldBackendKeepsLegacyPublication(t *testing.T) {
	var events []captured
	w := newTestWMail(t, models.InboxProviderGoogle, &events)
	m := &durableTestMap{recoveryMessageMap: recoveryMessageMap{data: map[string]repository.EmailMessageData{}}, unsupported: true}
	w.EmailMessageMapRepository = m
	if err := w.googleStore(context.Background(), &models.EmailMessageData{MessageID: "legacy", GmailID: "legacy"}); err != nil {
		t.Fatal(err)
	}
	if len(newEmails(events)) != 1 || len(m.data) != 1 {
		t.Fatal("old backend lost existing sync functionality")
	}
}
