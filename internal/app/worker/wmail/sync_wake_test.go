package wmail

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/client/msgraph"
	"github.com/warmbly/warmbly/internal/client/smtpimap/imap"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// batchMap is a message map that answers batch lookups and counts every request.
type batchMap struct {
	recoveryMessageMap
	gets    []string
	batches [][]string
}

func newBatchMap(known ...string) *batchMap {
	m := &batchMap{recoveryMessageMap: recoveryMessageMap{data: map[string]repository.EmailMessageData{}}}
	for _, id := range known {
		m.data[id] = repository.EmailMessageData{ID: uuid.NewString(), MessageID: id}
	}
	return m
}

func (m *batchMap) Get(ctx context.Context, userID, emailID uuid.UUID, id string) (*repository.EmailMessageData, error) {
	m.gets = append(m.gets, id)
	return m.recoveryMessageMap.Get(ctx, userID, emailID, id)
}

func (m *batchMap) GetMany(_ context.Context, _, _ uuid.UUID, ids []string) (map[string]*repository.EmailMessageData, error) {
	m.batches = append(m.batches, slices.Clone(ids))
	out := map[string]*repository.EmailMessageData{}
	for _, id := range ids {
		if data, ok := m.data[id]; ok {
			out[id] = &data
		}
	}
	return out, nil
}

func TestGraphWakeChecksInboxAndJunkOnly(t *testing.T) {
	g := newFakeGraph()
	g.live = []string{"live-1"}
	g.messages[msgraph.FolderInbox] = []string{"old-1"}
	var events []captured
	var relayed []models.SyncState
	w := newGraphTestMail(t, g.serve(t), &events, &relayed)

	if merr := w.syncGraph(t.Context(), true); merr != nil {
		t.Fatalf("narrow check: %v", merr.Message)
	}
	want := map[string]int{msgraph.FolderInbox: 1, msgraph.FolderJunk: 1}
	if fmt.Sprint(g.deltas) != fmt.Sprint(want) {
		t.Fatalf("a wake walked %v, want only inbox and junk", g.deltas)
	}
	if len(g.listed) != 0 {
		t.Fatalf("a wake ran the backfill: %v", g.listed)
	}
	if got := importedIDs(events); len(got) != 1 || got[0] != "live-1" {
		t.Fatalf("a wake imported %v, want the new message", got)
	}
	if w.tracker.state.LastSyncedAt != nil {
		t.Fatal("a narrow check stamped the mailbox as fully synced")
	}

	// The timed pass covers everything, and the mail the check stored is not offered again.
	if merr := w.SyncGraph(t.Context()); merr != nil {
		t.Fatalf("full pass: %v", merr.Message)
	}
	for _, folder := range msgraph.TrackedFolders {
		if g.deltas[folder] == 0 {
			t.Errorf("the full pass skipped the %s delta", folder)
		}
	}
	if g.listed[msgraph.FolderInbox] == 0 {
		t.Error("the full pass skipped the backfill")
	}
	if got := importedIDs(events); len(got) != 2 || !hasID(got, "old-1") {
		t.Fatalf("after the full pass imported %v, want live-1 once and old-1", got)
	}
}

// A wake folder still on its first walk or a recovery is left to the full pass.
func TestGraphWakeLeavesAFirstWalkToTheFullPass(t *testing.T) {
	g := newFakeGraph()
	var events []captured
	var relayed []models.SyncState
	w := newGraphTestMail(t, g.serve(t), &events, &relayed)
	delete(w.GraphData.Client.DeltaLinks, msgraph.FolderJunk)

	if merr := w.syncGraph(t.Context(), true); merr != nil {
		t.Fatalf("narrow check: %v", merr.Message)
	}
	if fmt.Sprint(g.deltas) != fmt.Sprint(map[string]int{msgraph.FolderInbox: 1}) {
		t.Fatalf("a wake walked %v, want only the inbox already on a delta link", g.deltas)
	}
}

func TestGraphDeltaPageResolvesTheMapInOneLookup(t *testing.T) {
	g := newFakeGraph()
	g.live = []string{"known-1", "new-1", "new-2", "known-2", "new-3"}
	var events []captured
	var relayed []models.SyncState
	w := newGraphTestMail(t, g.serve(t), &events, &relayed)
	maps := newBatchMap("known-1", "known-2")
	w.EmailMessageMapRepository = maps

	if merr := w.syncGraph(t.Context(), true); merr != nil {
		t.Fatalf("narrow check: %v", merr.Message)
	}
	if len(maps.batches) != 1 || len(maps.batches[0]) != 5 {
		t.Fatalf("batch lookups %v, want one for the page", maps.batches)
	}
	if len(maps.gets) != 0 {
		t.Fatalf("single lookups %v, want none", maps.gets)
	}
	if got := importedIDs(events); fmt.Sprint(got) != "[new-1 new-2 new-3]" {
		t.Fatalf("imported %v, want only the unknown messages", got)
	}
}

// translateGraph answers translateExchangeIds from a fixed table and counts the calls.
func translateGraph(t *testing.T, table map[string]string, calls *[][]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/translateExchangeIds") {
			t.Errorf("unexpected Graph path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotImplemented)
			return
		}
		var in struct {
			InputIDs []string `json:"inputIds"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		*calls = append(*calls, in.InputIDs)
		var value []map[string]string
		for _, id := range in.InputIDs {
			if target, ok := table[id]; ok {
				value = append(value, map[string]string{"sourceId": id, "targetId": target})
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"value": value})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// The regular-id fallback for immutable ids is one translation and one batch for the page.
func TestGraphPrefetchBatchesTheRegularIDFallback(t *testing.T) {
	var calls [][]string
	srv := translateGraph(t, map[string]string{"b": "rb", "c": "rc"}, &calls)
	var events []captured
	var relayed []models.SyncState
	w := newGraphTestMail(t, srv, &events, &relayed)
	w.GraphData.Client.SetImmutableIDMode(true)
	maps := newBatchMap("a", "rb")
	w.EmailMessageMapRepository = maps

	w.graphPrefetch(t.Context(), []string{"a", "b", "c", "d"})
	if fmt.Sprint(calls) != "[[b c d]]" {
		t.Fatalf("translations %v, want one for the unknown ids", calls)
	}
	if fmt.Sprint(maps.batches) != "[[a b c d] [rb rc]]" {
		t.Fatalf("batch lookups %v", maps.batches)
	}

	for _, tc := range []struct {
		id    string
		known bool
	}{{"a", true}, {"c", false}} {
		got, err := w.graphMessageMap(t.Context(), tc.id)
		if err != nil || (got != nil) != tc.known {
			t.Fatalf("%s: got %+v err %v", tc.id, got, err)
		}
	}
	if len(maps.gets) != 0 || len(calls) != 1 {
		t.Fatalf("answered ids asked again: gets %v, translations %v", maps.gets, calls)
	}

	// b is known under its regular id: it is aliased and answered under the immutable one.
	got, err := w.graphMessageMap(t.Context(), "b")
	if err != nil || got == nil || got.MessageID != "b" || got.ID != maps.data["rb"].ID {
		t.Fatalf("b: got %+v err %v", got, err)
	}
	if _, ok := maps.data["b"]; !ok {
		t.Fatal("the immutable alias was not recorded")
	}

	// d had no translation, so it takes the single lookup path and reports what that path reports.
	if _, err := w.graphMessageMap(t.Context(), "d"); err == nil {
		t.Fatal("an untranslatable id was answered from the page")
	}
	if len(calls) != 2 || fmt.Sprint(calls[1]) != "[d]" {
		t.Fatalf("translations %v, want d asked on its own", calls)
	}
}

// gmailWakeServer is Gmail with one history page, counting every other call.
type gmailWakeServer struct {
	history  int
	listings int
}

func (g *gmailWakeServer) serve(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/history"):
			g.history++
			_, _ = w.Write([]byte(`{"historyId":"200","history":[
				{"id":"101","messagesAdded":[{"message":{"id":"h1","threadId":"t1"}}]},
				{"id":"102","labelsAdded":[{"message":{"id":"known-1","labelIds":["INBOX","UNREAD"]},"labelIds":["UNREAD"]}]},
				{"id":"103","labelsRemoved":[{"message":{"id":"stranger","labelIds":["IMPORTANT"]},"labelIds":["STARRED"]}]},
				{"id":"104","messagesDeleted":[{"message":{"id":"gone"}}]}]}`))
		case strings.Contains(r.URL.Path, "/messages/"):
			id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
			_, _ = fmt.Fprintf(w, `{"id":%q,"threadId":"t-%s","labelIds":["INBOX"],"payload":{"headers":[{"name":"Message-Id","value":"<%s@gmail.test>"},{"name":"Subject","value":"history"}]}}`, id, id, id)
		default:
			g.listings++
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestGoogleWakeReadsHistoryOnlyInOneLookupPerPage(t *testing.T) {
	g := &gmailWakeServer{}
	var events []captured
	w := newGoogleTestMail(t, g.serve(t), &events)
	w.GoogleData.LastHistoryID = 100
	w.GoogleData.Client.OnMessageRemove = w.onGoogleMessageRemove
	w.GoogleData.Client.OnLabelAdd = w.onGoogleMessageLabelsAdded
	w.GoogleData.Client.OnLabelRemove = w.onGoogleMessageLabelsRemoved
	maps := newBatchMap("known-1")
	w.EmailMessageMapRepository = maps

	if merr := w.syncGoogle(t.Context(), true); merr != nil {
		t.Fatalf("narrow check: %v", merr.Message)
	}
	if g.history != 1 || g.listings != 0 {
		t.Fatalf("a wake made %d history and %d other calls, want history alone", g.history, g.listings)
	}
	if w.GoogleData.LastHistoryID != 200 {
		t.Fatalf("checkpoint %d, want 200", w.GoogleData.LastHistoryID)
	}
	if fmt.Sprint(maps.batches) != "[[h1 known-1 stranger gone]]" || len(maps.gets) != 0 {
		t.Fatalf("lookups: batches %v gets %v, want one batch for the page", maps.batches, maps.gets)
	}
	if got := importedIDs(events); len(got) != 1 || got[0] != "h1" {
		t.Fatalf("imported %v", got)
	}
	if !hasEvent(events, models.JobEventTypeFlagsRemove) {
		t.Fatal("the known message's read state was not relayed")
	}

	// The full pass goes on to the backfill and the folder reconciliation.
	if merr := w.SyncGoogle(t.Context()); merr != nil {
		t.Fatalf("full pass: %v", merr.Message)
	}
	if g.listings == 0 {
		t.Fatal("the full pass did not run the backfill")
	}
}

// A lapsed checkpoint is the full pass's to recover; a wake leaves it alone.
func TestGoogleWakeLeavesRecoveryToTheFullPass(t *testing.T) {
	g := &gmailWakeServer{}
	var events []captured
	w := newGoogleTestMail(t, g.serve(t), &events)
	w.tracker.state.BackfillCursor.GoogleRecovery = &models.GoogleHistoryRecovery{HistoryID: 5, Since: time.Now()}
	if merr := w.syncGoogle(t.Context(), true); merr != nil || g.history+g.listings != 0 {
		t.Fatalf("a wake during recovery made calls: %v %+v", merr, g)
	}
	w.tracker.state.BackfillCursor.GoogleRecovery = nil
	if merr := w.syncGoogle(t.Context(), true); merr != nil || g.history+g.listings != 0 {
		t.Fatalf("a wake without a baseline made calls: %v %+v", merr, g)
	}
}

// wakeImapConn records which folders a pass selects and whether it listed.
type wakeImapConn struct {
	*fakeImapConn
	lists    int
	selected []string
}

func (c *wakeImapConn) ListFolders() (imap.FolderListing, *errx.MailError) {
	c.lists++
	return c.fakeImapConn.ListFolders()
}

func (c *wakeImapConn) SelectForSyncState(name string) (imap.Selected, *errx.MailError) {
	c.selected = append(c.selected, name)
	return c.fakeImapConn.SelectForSyncState(name)
}

func TestImapWakeChecksInboxAndJunkOnly(t *testing.T) {
	listed := []models.Mailbox{
		{Name: "INBOX", UIDValidity: 7, HighestModSeq: 200},
		{Name: "Junk", Attrs: []string{"\\Junk"}, UIDValidity: 8, HighestModSeq: 50},
		{Name: "Sent", Attrs: []string{"\\Sent"}, UIDValidity: 9, HighestModSeq: 30},
		{Name: "Clients", UIDValidity: 10, HighestModSeq: 20},
	}
	conn := &wakeImapConn{fakeImapConn: &fakeImapConn{folders: listed, changed: uidRange(2)}}
	w, _ := newIMAPTestMail(conn, &fixedBudget{allow: 100}, nil)
	w.SmtpImapData.Mailboxes = nil
	saved := map[string]uint64{"INBOX": 100, "Junk": 40, "Sent": 20, "Clients": 10}
	for _, box := range listed {
		b := box
		b.HighestModSeq = saved[box.Name]
		w.SmtpImapData.Mailboxes = append(w.SmtpImapData.Mailboxes, &b)
	}
	cursor := func(name string) uint64 {
		for _, b := range w.SmtpImapData.Mailboxes {
			if b.Name == name {
				return b.HighestModSeq
			}
		}
		return 0
	}

	if merr := w.syncMailForTest(t.Context(), true); merr != nil {
		t.Fatalf("narrow check: %v", merr.Message)
	}
	if conn.lists != 0 || fmt.Sprint(conn.selected) != "[INBOX Junk]" || conn.flagScans != 0 {
		t.Fatalf("a wake listed %d times, selected %v, scanned flags %d times", conn.lists, conn.selected, conn.flagScans)
	}
	if cursor("INBOX") != 200 || cursor("Junk") != 50 || cursor("Sent") != 20 || cursor("Clients") != 10 {
		t.Fatalf("cursors after a wake: %v %v %v %v", cursor("INBOX"), cursor("Junk"), cursor("Sent"), cursor("Clients"))
	}

	// The full pass walks only what the check left behind.
	searches := conn.modseqSearches
	if merr := w.syncMailForTest(t.Context(), false); merr != nil {
		t.Fatalf("full pass: %v", merr.Message)
	}
	if conn.lists != 1 || conn.modseqSearches-searches != 2 {
		t.Fatalf("the full pass listed %d times and searched %d folders, want 1 and the 2 left behind", conn.lists, conn.modseqSearches-searches)
	}
	if cursor("Sent") != 30 || cursor("Clients") != 20 {
		t.Fatalf("the full pass did not advance the other folders: %v %v", cursor("Sent"), cursor("Clients"))
	}
}

// A folder the server re-generated is re-baselined by the full pass, never walked by a wake.
func TestImapWakeLeavesANewGenerationToTheFullPass(t *testing.T) {
	conn := &wakeImapConn{fakeImapConn: &fakeImapConn{
		folders: []models.Mailbox{{Name: "INBOX", UIDValidity: 9, HighestModSeq: 200}},
		changed: uidRange(2),
	}}
	w, events := newIMAPTestMail(conn, &fixedBudget{allow: 100}, &models.Mailbox{Name: "INBOX", UIDValidity: 7, HighestModSeq: 100})
	if merr := w.syncMailForTest(t.Context(), true); merr != nil {
		t.Fatalf("narrow check: %v", merr.Message)
	}
	if conn.modseqSearches != 0 || len(*events) != 0 || w.SmtpImapData.Mailboxes[0].HighestModSeq != 100 {
		t.Fatalf("a wake walked a re-generated folder: searches %d events %v", conn.modseqSearches, *events)
	}
}

// syncMailForTest runs the provider pass the way syncMail dispatches it, without the execution lifecycle.
func (w *WMail) syncMailForTest(ctx context.Context, narrow bool) *errx.MailError {
	if narrow {
		return w.imapWakeCheck(ctx)
	}
	return w.Sync(ctx)
}
