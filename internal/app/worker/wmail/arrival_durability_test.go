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
