package wmail

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/client/goog"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
	"golang.org/x/oauth2"
)

// The checkpoint row is keyed (user_id, email_id) with a foreign key to users,
// so an event carrying zero UUIDs is rejected by Postgres and the mailbox never
// gets a checkpoint at all.
func TestNewHistoryIDCarriesRowKey(t *testing.T) {
	userID := uuid.New()
	emailID := uuid.New()

	var got *models.JobEventHistoryIDUpdate
	w := &WMail{
		UserID: userID,
		ID:     emailID,
		onEvent: func(jobType models.JobEventType, body any) error {
			if jobType != models.JobEventTypeHistoryIDUpdate {
				t.Errorf("published %v, want %v", jobType, models.JobEventTypeHistoryIDUpdate)
			}
			got = body.(*models.JobEventHistoryIDUpdate)
			return nil
		},
	}

	if err := w.NewHistoryID(65207); err != nil {
		t.Fatalf("NewHistoryID: %v", err)
	}

	if got == nil {
		t.Fatal("no event was published")
	}
	if got.UserID != userID {
		t.Errorf("UserID = %v, want %v", got.UserID, userID)
	}
	if got.EmailID != emailID {
		t.Errorf("EmailID = %v, want %v", got.EmailID, emailID)
	}
	if got.HistoryID != 65207 {
		t.Errorf("HistoryID = %d, want 65207", got.HistoryID)
	}
	if got.UserID == uuid.Nil || got.EmailID == uuid.Nil {
		t.Error("event carries a nil UUID, which violates the users foreign key")
	}
}

// fakeGmail is the Gmail API as one backfill sees it: a messages.list that can
// be made to refuse, and a messages.get for whatever it did list.
type fakeGmail struct {
	ids       []string
	failList  int // list calls left to refuse (-1 for always)
	listCalls int
}

func (g *fakeGmail) serve(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, isGet := strings.CutPrefix(r.URL.Path, "/gmail/v1/users/me/messages/")
		if isGet {
			_, _ = fmt.Fprintf(w, `{"id":%q,"threadId":"t-%s","payload":{"headers":[{"name":"Message-Id","value":"<%s@gmail.test>"},{"name":"Subject","value":"history"}]}}`, id, id, id)
			return
		}
		if g.failList != 0 {
			if g.failList > 0 {
				g.failList--
			}
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":400,"message":"injected"}}`))
			return
		}
		g.listCalls++
		msgs := make([]string, 0, len(g.ids))
		for _, id := range g.ids {
			msgs = append(msgs, fmt.Sprintf(`{"id":%q,"threadId":"t-%s"}`, id, id))
		}
		_, _ = fmt.Fprintf(w, `{"messages":[%s]}`, strings.Join(msgs, ","))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newGoogleTestMail(t *testing.T, srv *httptest.Server, events *[]captured) *WMail {
	t.Helper()
	w := &WMail{
		ID:                        uuid.New(),
		UserID:                    uuid.New(),
		Email:                     "box@gmail.test",
		EmailType:                 models.InboxProviderGoogle,
		Storage:                   fakeStore{},
		EmailMessageMapRepository: fakeMessageMap{},
		gov:                       newGovernor(uuid.New(), nil, nil, models.SyncPolicy{}),
	}
	w.onEvent = func(kind models.JobEventType, body any) error {
		*events = append(*events, captured{eventType: kind, body: body})
		return nil
	}
	w.tracker = newSyncTracker(nil, func(models.SyncState) error { return nil })

	client := &goog.Client{
		Email:          w.Email,
		OnMessageAdded: w.onGoogleMessageAdded,
		OnTokenRefresh: func(context.Context, *oauth2.Token) error { return nil },
	}
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{Transport: rewriteToTestServer(srv.URL)})
	token := &oauth2.Token{AccessToken: "live", Expiry: time.Now().Add(time.Hour)}
	if merr := client.Init(ctx, token, oauth2.Config{}); merr != nil {
		t.Fatalf("client init: %v", merr.Message)
	}
	w.GoogleData = &GoogleData{Client: client}
	return w
}

// The Graph defect's shape, checked on the Gmail import: a refused listing
// ends the pass with the page token where it was, and never reports the
// history as imported.
func TestGoogleBackfillRetriesAfterATransientFailure(t *testing.T) {
	g := &fakeGmail{ids: []string{"g1", "g2"}, failList: 1}
	var events []captured
	w := newGoogleTestMail(t, g.serve(t), &events)

	if merr := w.googleBackfill(t.Context(), &tickStats{}); merr == nil {
		t.Fatal("a refused listing was swallowed; the pass must end so the import is retried")
	}
	if st := w.tracker.state.BackfillStatus; st == models.SyncBackfillComplete {
		t.Fatalf("backfill status = %s after a failed listing", st)
	}
	if tok := w.tracker.state.BackfillCursor.PageToken; tok != "" {
		t.Errorf("page token = %q, want it held where it was", tok)
	}

	if merr := w.googleBackfill(t.Context(), &tickStats{}); merr != nil {
		t.Fatalf("second pass: %v", merr.Message)
	}
	if st := w.tracker.state.BackfillStatus; st != models.SyncBackfillComplete {
		t.Fatalf("backfill status = %s, want %s", st, models.SyncBackfillComplete)
	}
	if got := len(importedIDs(events)); got != 2 {
		t.Errorf("imported %d messages, want 2: %v", got, importedIDs(events))
	}
	if g.listCalls != 1 {
		t.Errorf("listed %d times, want the one call that succeeded", g.listCalls)
	}
}

type googleBackfillBudget struct {
	fixedBudget
	policy models.SyncPolicy
	lanes  []SyncLane
}

func (b *googleBackfillBudget) Policy() models.SyncPolicy { return normalizePolicy(b.policy) }

func (b *googleBackfillBudget) Admit(ctx context.Context, lane SyncLane) Admission {
	b.lanes = append(b.lanes, lane)
	return b.fixedBudget.Admit(ctx, lane)
}

func TestGoogleBackfillExpiredPageResumesAfterReloadWithinWindowAndBudgets(t *testing.T) {
	var tokens, gets []string
	since := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	started := since.Add(30 * 24 * time.Hour)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/messages") {
			token := r.URL.Query().Get("pageToken")
			tokens = append(tokens, token)
			if q := r.URL.Query().Get("q"); q != fmt.Sprintf("after:%d -in:trash -in:spam -in:chats", since.Unix()) {
				t.Errorf("import window changed: q=%q", q)
			}
			if r.URL.Query().Get("includeSpamTrash") != "false" || r.URL.Query().Get("maxResults") != "100" {
				t.Errorf("import listing changed: %s", r.URL)
			}
			switch token {
			case "expired":
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":{"code":400,"message":"Invalid page token"}}`))
			case "":
				_, _ = w.Write([]byte(`{"messages":[{"id":"known"},{"id":"new-1"},{"id":"new-2"}],"nextPageToken":"page-2"}`))
			case "page-2":
				_, _ = w.Write([]byte(`{"messages":[{"id":"new-3"},{"id":"over-cap"}]}`))
			default:
				t.Errorf("unexpected token %q", token)
			}
			return
		}
		id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		gets = append(gets, id)
		_, _ = fmt.Fprintf(w, `{"id":%q,"threadId":"t-%s","payload":{"headers":[{"name":"Message-Id","value":"<%s@gmail.test>"}]}}`, id, id, id)
	}))
	t.Cleanup(srv.Close)
	var events []captured
	w := newGoogleTestMail(t, srv, &events)
	budget := &googleBackfillBudget{fixedBudget: fixedBudget{allow: 1}, policy: models.SyncPolicy{BackfillDays: 2, BackfillMessages: 4}}
	w.gov = budget
	mapping := &recoveryMessageMap{data: map[string]repository.EmailMessageData{"known": {ID: uuid.NewString(), MessageID: "known"}}}
	w.EmailMessageMapRepository = mapping
	w.tracker.state = models.SyncState{
		BackfillStatus: models.SyncBackfillRunning, BackfillSynced: 1,
		BackfillSince: &since, BackfillStartedAt: &started,
		BackfillCursor: models.SyncCursor{PageToken: "expired", Folders: map[string]models.SyncFolderCursor{}},
		Deferred:       5, LastSyncedAt: &started,
	}
	reload := func(state models.SyncState) {
		t.Helper()
		encoded, err := json.Marshal(state)
		if err != nil {
			t.Fatal(err)
		}
		var seed models.SyncState
		if err := json.Unmarshal(encoded, &seed); err != nil {
			t.Fatal(err)
		}
		id, userID := w.ID, w.UserID
		w = newGoogleTestMail(t, srv, &events)
		w.ID, w.UserID = id, userID
		w.gov, w.EmailMessageMapRepository = budget, mapping
		w.tracker = newSyncTracker(&seed, func(models.SyncState) error { return nil })
	}
	reload(w.tracker.state)
	want := w.tracker.state
	want.BackfillCursor.PageToken = ""
	if err := w.googleBackfill(t.Context(), &tickStats{}); err != nil {
		t.Fatalf("expired continuation must reset for retry: %v", err)
	}
	if !reflect.DeepEqual(w.tracker.state, want) || !w.tracker.dirty || budget.admitted != 0 || len(events) != 0 || !slices.Equal(tokens, []string{"expired"}) {
		t.Fatalf("reset changed more than the continuation: state=%+v tokens=%v budget=%+v", w.tracker.state, tokens, budget)
	}
	var relayed models.SyncState
	w.tracker.emit = func(state models.SyncState) error { relayed = state; return nil }
	w.tracker.flush(time.Now())
	reload(relayed)
	if err := w.googleBackfill(t.Context(), &tickStats{}); err != nil {
		t.Fatal(err)
	}
	if w.tracker.state.BackfillSynced != 2 || w.tracker.state.BackfillCursor.PageToken != "" || w.tracker.state.BackfillStatus != models.SyncBackfillRunning {
		t.Fatalf("pacing must pin the restarted page: %+v", w.tracker.state)
	}
	reload(w.tracker.state)
	if err := w.googleBackfill(t.Context(), &tickStats{}); err != nil {
		t.Fatal(err)
	}
	if len(newEmails(events)) != 1 || budget.admitted != 1 {
		t.Fatal("reload bypassed the admission budget or duplicated an arrival")
	}
	budget.allow = 3
	if err := w.googleBackfill(t.Context(), &tickStats{}); err != nil {
		t.Fatal(err)
	}
	if w.tracker.state.BackfillStatus != models.SyncBackfillComplete || w.tracker.state.BackfillSynced != 4 || !w.tracker.state.BackfillSince.Equal(since) || !w.tracker.state.BackfillStartedAt.Equal(started) {
		t.Fatalf("import changed the fixed window or count: %+v", w.tracker.state)
	}
	if !slices.Equal(gets, []string{"new-1", "new-2", "new-3"}) || len(newEmails(events)) != 3 || len(mapping.data) != 4 || budget.admitted != 3 || budget.observed != 0 {
		t.Fatalf("known mail or capped mail was imported: gets=%v arrivals=%d budget=%+v", gets, len(newEmails(events)), budget)
	}
	for _, lane := range budget.lanes {
		if lane != LaneBackfill {
			t.Fatalf("import charged %s instead of backfill", lane)
		}
	}
	reload(w.tracker.state)
	if err := w.googleBackfill(t.Context(), &tickStats{}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(tokens, []string{"expired", "", "", "", "page-2"}) || len(newEmails(events)) != 3 {
		t.Fatalf("completed reload repeated the import: tokens=%v arrivals=%d", tokens, len(newEmails(events)))
	}
}

func TestGoogleBackfillRetainsContinuationOnOtherProviderErrors(t *testing.T) {
	for _, tc := range []struct {
		name, message string
		code          int
	}{
		{"invalid query", "Invalid query", 400},
		{"quota", "Invalid page token", 429},
		{"auth", "Invalid page token", 401},
		{"forbidden", "Invalid page token", 403},
		{"unavailable", "Invalid page token", 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Query().Get("pageToken") != "keep" {
					t.Error("provider error reset the continuation")
				}
				w.WriteHeader(tc.code)
				_, _ = fmt.Fprintf(w, `{"error":{"code":%d,"message":%q}}`, tc.code, tc.message)
			}))
			t.Cleanup(srv.Close)
			var events []captured
			w := newGoogleTestMail(t, srv, &events)
			w.tracker.startBackfill(time.Now(), 30)
			w.tracker.state.BackfillCursor.PageToken = "keep"
			w.tracker.state.BackfillSynced = 7
			want := w.tracker.state
			for range 2 {
				stats := &tickStats{}
				if err := w.googleBackfill(t.Context(), stats); err == nil || !stats.aborted {
					t.Fatalf("provider error was swallowed: err=%v aborted=%t", err, stats.aborted)
				}
				if !reflect.DeepEqual(w.tracker.state, want) || len(events) != 0 {
					t.Fatalf("provider failure changed import progress: %+v", w.tracker.state)
				}
			}
			if calls != 2 {
				t.Fatalf("requests=%d, want one per attempt", calls)
			}
		})
	}
}

func TestGoogleBackfillCompletedOrCappedImportDoesNotRestartExpiredPage(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status models.SyncBackfillStatus
		synced int
	}{
		{"completed below cap", models.SyncBackfillComplete, 1},
		{"running at cap", models.SyncBackfillRunning, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
				t.Error("completed or capped import must not re-list an expired page")
			}))
			t.Cleanup(srv.Close)
			var events []captured
			w := newGoogleTestMail(t, srv, &events)
			w.gov = &googleBackfillBudget{fixedBudget: fixedBudget{allow: 10}, policy: models.SyncPolicy{BackfillMessages: 4}}
			w.tracker.startBackfill(time.Now(), 30)
			w.tracker.state.BackfillStatus = tc.status
			w.tracker.state.BackfillSynced = tc.synced
			w.tracker.state.BackfillCursor.PageToken = "expired"
			if err := w.googleBackfill(t.Context(), &tickStats{}); err != nil {
				t.Fatal(err)
			}
			if w.tracker.state.BackfillStatus != models.SyncBackfillComplete || w.tracker.state.BackfillSynced != tc.synced || w.tracker.state.BackfillCursor.PageToken != "expired" || len(events) != 0 {
				t.Fatalf("completed import changed: %+v", w.tracker.state)
			}
		})
	}
}
