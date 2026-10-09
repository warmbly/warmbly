package wmail

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/client/msgraph"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"golang.org/x/oauth2"
)

// fakeGraph is Microsoft Graph as one mailbox's sync pass sees it: a delta
// stream per tracked folder, a listing per backfill folder, and per-folder
// failure injection so a pass can be driven through a provider incident.
type fakeGraph struct {
	// messages is what each folder's listing returns, by message id.
	messages map[string][]string
	// live is offered once through the inbox delta stream.
	live []string
	// listFail is the status a folder's listing answers with, and how many
	// calls answer that way before it recovers.
	listFail map[string]*graphFailure
	// listed counts successful listings per folder.
	listed map[string]int
}

type graphFailure struct {
	status int
	times  int
}

func newFakeGraph() *fakeGraph {
	return &fakeGraph{
		messages: map[string][]string{},
		listFail: map[string]*graphFailure{},
		listed:   map[string]int{},
	}
}

func (g *fakeGraph) serve(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := strings.TrimPrefix(r.URL.Path, "/v1.0/me/")

		// Hydration of one live message: GET /me/messages/{id}
		if id, ok := strings.CutPrefix(path, "messages/"); ok {
			_ = json.NewEncoder(w).Encode(graphMessageJSON(id))
			return
		}

		rest, ok := strings.CutPrefix(path, "mailFolders/")
		if !ok {
			t.Errorf("unexpected Graph path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotImplemented)
			return
		}
		folder, tail, _ := strings.Cut(rest, "/")

		if tail == "messages/delta" {
			value := []any{}
			if folder == msgraph.FolderInbox {
				for _, id := range g.live {
					value = append(value, map[string]any{"id": id, "isRead": false})
				}
				g.live = nil
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"value":             value,
				"@odata.deltaLink":  "https://graph.microsoft.com/v1.0/me/mailFolders/" + folder + "/messages/delta?$deltatoken=t",
				"@odata.deltaToken": "t",
			})
			return
		}

		if f := g.listFail[folder]; f != nil && f.times != 0 {
			if f.times > 0 {
				f.times--
			}
			w.WriteHeader(f.status)
			_, _ = w.Write([]byte(`{"error":{"code":"Failed","message":"injected"}}`))
			return
		}

		g.listed[folder]++
		value := make([]any, 0, len(g.messages[folder]))
		for _, id := range g.messages[folder] {
			value = append(value, graphMessageJSON(id))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"value": value})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func graphMessageJSON(id string) map[string]any {
	return map[string]any{
		"id":                id,
		"internetMessageId": fmt.Sprintf("<%s@outlook.test>", id),
		"conversationId":    "conv-" + id,
		"subject":           "subject " + id,
		"receivedDateTime":  time.Now().UTC().Format(time.RFC3339),
		"from":              map[string]any{"emailAddress": map[string]any{"address": "someone@example.test"}},
		"body":              map[string]any{"contentType": "text", "content": "body " + id},
	}
}

// rewriteToTestServer sends the real provider URLs a client builds at the test
// server instead, keeping the path and query intact.
func rewriteToTestServer(base string) http.RoundTripper {
	target, err := url.Parse(base)
	if err != nil {
		panic(err)
	}
	return testRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		clone := req.Clone(req.Context())
		clone.URL.Scheme = target.Scheme
		clone.URL.Host = target.Host
		clone.Host = target.Host
		return http.DefaultTransport.RoundTrip(clone)
	})
}

type testRoundTripFunc func(*http.Request) (*http.Response, error)

func (f testRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// newGraphTestMail wires a real msgraph client, pointed at the fake tenant,
// into the smallest WMail that can run a Graph sync pass.
func newGraphTestMail(t *testing.T, srv *httptest.Server, events *[]captured, relayed *[]models.SyncState) *WMail {
	t.Helper()
	w := &WMail{
		ID:                        uuid.New(),
		UserID:                    uuid.New(),
		Email:                     "box@outlook.test",
		EmailType:                 models.InboxProviderOutlook,
		Storage:                   fakeStore{},
		EmailMessageMapRepository: fakeMessageMap{},
		gov:                       newGovernor(uuid.New(), nil, nil, models.SyncPolicy{}),
	}
	w.onEvent = func(kind models.JobEventType, body any) error {
		*events = append(*events, captured{eventType: kind, body: body})
		return nil
	}
	w.tracker = newSyncTracker(nil, func(st models.SyncState) error {
		*relayed = append(*relayed, st)
		return nil
	})

	// Seeded cursors model a mailbox already following live deltas.
	deltaLinks := map[string]string{}
	for _, folder := range msgraph.TrackedFolders {
		deltaLinks[folder] = "https://graph.microsoft.com/v1.0/me/mailFolders/" + folder + "/messages/delta?$deltatoken=seed"
	}
	client := &msgraph.Client{
		Email:           w.Email,
		DeltaLinks:      deltaLinks,
		OnMessageSeen:   w.onGraphMessageSeen,
		OnMessageRemove: w.onGraphMessageRemove,
		OnDelta:         w.onGraphDelta,
		OnTokenRefresh:  func(context.Context, *oauth2.Token) error { return nil },
	}
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{Transport: rewriteToTestServer(srv.URL)})
	token := &oauth2.Token{AccessToken: "live", Expiry: time.Now().Add(time.Hour)}
	if merr := client.Init(ctx, token, oauth2.Config{}); merr != nil {
		t.Fatalf("client init: %v", merr.Message)
	}
	w.GraphData = &GraphData{Client: client}
	return w
}

// importedIDs is every message the pass stored, in order.
func importedIDs(events []captured) []string {
	var out []string
	for _, e := range events {
		if e.eventType != models.JobEventTypeNewEmail {
			continue
		}
		if ev, ok := e.body.(*models.JobEventNewEmail); ok {
			out = append(out, ev.Message.GmailID)
		}
	}
	return out
}

func hasID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// The incident: one Graph blip on a folder's first backfill page used to mark
// that folder complete forever, so the customer who connected a mailbox during
// an outage silently got no archive history, on this worker or any later one.
func TestGraphBackfillRetriesAFolderAfterATransientFailure(t *testing.T) {
	g := newFakeGraph()
	g.messages[msgraph.FolderInbox] = []string{"inbox-1"}
	g.messages[msgraph.FolderSent] = []string{"sent-1"}
	g.messages[msgraph.FolderArchive] = []string{"archive-1"}
	g.live = []string{"live-1"}
	// Graph is having a moment, but only for the archive listing.
	g.listFail[msgraph.FolderArchive] = &graphFailure{status: http.StatusServiceUnavailable, times: 1}

	var events []captured
	var relayed []models.SyncState
	w := newGraphTestMail(t, g.serve(t), &events, &relayed)

	merr := w.SyncGraph(t.Context())
	if merr == nil {
		t.Fatal("a 503 on the archive listing was swallowed; the pass must end so the folder is retried")
	}
	if merr.Code != errx.MailErrorCodeServerUnreachable {
		t.Fatalf("code = %s, want %s", merr.Code, errx.MailErrorCodeServerUnreachable)
	}
	if cur := w.tracker.folder(msgraph.FolderArchive); cur.Done {
		t.Fatal("the archive backfill was marked complete by a transient failure")
	}
	if st := w.tracker.state.BackfillStatus; st == models.SyncBackfillComplete {
		t.Fatalf("backfill status = %s after a failed pass", st)
	}
	// The persisted half of the same defect: nothing that reaches the control
	// plane may write the folder off, or a replaced worker never retries it.
	for _, st := range relayed {
		if st.BackfillCursor.Folders[msgraph.FolderArchive].Done {
			t.Fatal("a relayed SYNC_STATE marked the archive folder done after a transient failure")
		}
	}
	// The folders that did answer still landed, and live mail was not lost.
	for _, want := range []string{"live-1", "inbox-1", "sent-1"} {
		if !hasID(importedIDs(events), want) {
			t.Errorf("%s was not imported: %v", want, importedIDs(events))
		}
	}

	// Next pass: Graph is back.
	if merr := w.SyncGraph(t.Context()); merr != nil {
		t.Fatalf("second pass: %v", merr.Message)
	}
	if !hasID(importedIDs(events), "archive-1") {
		t.Fatalf("the archive history was never imported: %v", importedIDs(events))
	}
	if !w.tracker.folder(msgraph.FolderArchive).Done {
		t.Error("archive is still not done after a successful listing")
	}

	// One more pass settles the whole backfill and relays it.
	if merr := w.SyncGraph(t.Context()); merr != nil {
		t.Fatalf("third pass: %v", merr.Message)
	}
	if st := w.tracker.state.BackfillStatus; st != models.SyncBackfillComplete {
		t.Fatalf("backfill status = %s, want %s", st, models.SyncBackfillComplete)
	}
	if len(relayed) == 0 {
		t.Fatal("no SYNC_STATE was relayed, so nothing would be persisted")
	}
	last := relayed[len(relayed)-1]
	if !last.BackfillCursor.Folders[msgraph.FolderArchive].Done {
		t.Error("the relayed state does not carry the archive folder as done")
	}
}

// The behavior the skip was written for, now keyed on Graph actually saying so:
// a tenant without an archive folder is not a mailbox that syncs forever.
func TestGraphBackfillSkipsAFolderTheTenantDoesNotHave(t *testing.T) {
	g := newFakeGraph()
	g.messages[msgraph.FolderInbox] = []string{"inbox-1"}
	g.messages[msgraph.FolderSent] = []string{"sent-1"}
	// No archive on this plan: Graph answers 404 for as long as it is asked.
	g.listFail[msgraph.FolderArchive] = &graphFailure{status: http.StatusNotFound, times: -1}

	var events []captured
	var relayed []models.SyncState
	w := newGraphTestMail(t, g.serve(t), &events, &relayed)

	if merr := w.SyncGraph(t.Context()); merr != nil {
		t.Fatalf("a missing folder must not fail the pass: %v", merr.Message)
	}
	if !w.tracker.folder(msgraph.FolderArchive).Done {
		t.Fatal("the absent archive folder was not skipped, so the backfill can never finish")
	}
	for _, want := range []string{"inbox-1", "sent-1"} {
		if !hasID(importedIDs(events), want) {
			t.Errorf("%s was not imported: %v", want, importedIDs(events))
		}
	}

	if merr := w.SyncGraph(t.Context()); merr != nil {
		t.Fatalf("second pass: %v", merr.Message)
	}
	if st := w.tracker.state.BackfillStatus; st != models.SyncBackfillComplete {
		t.Fatalf("backfill status = %s, want %s", st, models.SyncBackfillComplete)
	}
	if g.listed[msgraph.FolderArchive] != 0 {
		t.Error("the fake tenant served an archive listing it was supposed to refuse")
	}
}

// Every folder is retried on its own terms: a blip on the inbox listing holds
// the inbox, and does not quietly hand the sent folder the same verdict.
func TestGraphBackfillHoldsOnlyTheFolderThatFailed(t *testing.T) {
	g := newFakeGraph()
	g.messages[msgraph.FolderInbox] = []string{"inbox-1"}
	g.messages[msgraph.FolderSent] = []string{"sent-1"}
	g.messages[msgraph.FolderArchive] = []string{"archive-1"}
	g.listFail[msgraph.FolderInbox] = &graphFailure{status: http.StatusInternalServerError, times: 1}

	var events []captured
	var relayed []models.SyncState
	w := newGraphTestMail(t, g.serve(t), &events, &relayed)

	if merr := w.SyncGraph(t.Context()); merr == nil {
		t.Fatal("a 500 on the inbox listing was swallowed")
	}
	if w.tracker.folder(msgraph.FolderInbox).Done {
		t.Fatal("the inbox backfill was marked complete by a transient failure")
	}
	// The pass ended at the inbox, so the folders behind it are untouched, not
	// written off.
	if w.tracker.folder(msgraph.FolderSent).Done || w.tracker.folder(msgraph.FolderArchive).Done {
		t.Fatal("a later folder was marked done by a failure in an earlier one")
	}

	if merr := w.SyncGraph(t.Context()); merr != nil {
		t.Fatalf("second pass: %v", merr.Message)
	}
	for _, want := range []string{"inbox-1", "sent-1", "archive-1"} {
		if !hasID(importedIDs(events), want) {
			t.Errorf("%s was not imported: %v", want, importedIDs(events))
		}
	}
}
