package wmail

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/client/msgraph"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type graphRecoveryProvider struct {
	snapshot         map[string][]any
	page2            map[string][]any
	messages         map[string]map[string]any
	translations     map[string]string
	messageErrors    map[string]int
	resolvedID       string
	resolveError     int
	resolveMalformed bool
	expired          string
	seen             bool
	requests         []string
}

func (g *graphRecoveryProvider) serve(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !strings.Contains(r.Header.Get("Prefer"), `IdType="ImmutableId"`) {
			t.Error("sync requested move-unstable ids")
		}
		path := strings.TrimPrefix(r.URL.Path, "/v1.0/me/")
		g.requests = append(g.requests, path+"?"+r.URL.RawQuery)
		switch {
		case path == "messages":
			if r.URL.Query().Get("$filter") != "internetMessageId eq '<stored@fake.test>'" {
				t.Error("RFC lookup lost its exact stored identity")
			}
			if g.resolveError != 0 {
				w.WriteHeader(g.resolveError)
				_, _ = w.Write([]byte(`{"error":{"code":"ErrorServerBusy"}}`))
				return
			}
			value := []any{}
			if g.resolveMalformed {
				value = append(value, map[string]any{})
			}
			if g.resolvedID != "" {
				value = append(value, map[string]any{"id": g.resolvedID})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"value": value})
		case path == "translateExchangeIds":
			var body struct {
				IDs    []string `json:"inputIds"`
				Target string   `json:"targetIdType"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			value := []any{}
			for _, id := range body.IDs {
				target := "stable-" + strings.TrimPrefix(id, "regular-")
				if body.Target == "restId" {
					target = "regular-" + strings.TrimPrefix(id, "stable-")
				}
				if translated, ok := g.translations[id]; ok {
					target = translated
				}
				value = append(value, map[string]any{"sourceId": id, "targetId": target})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"value": value})
		case strings.HasPrefix(path, "messages/"):
			id := strings.TrimPrefix(path, "messages/")
			if status := g.messageErrors[id]; status != 0 {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":{"code":"ErrorServerBusy"}}`))
				return
			}
			msg := g.messages[id]
			if msg == nil {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":{"code":"ErrorItemNotFound"}}`))
				return
			}
			copy := map[string]any{}
			for key, value := range msg {
				copy[key] = value
			}
			copy["isRead"] = g.seen
			_ = json.NewEncoder(w).Encode(copy)
		case strings.HasPrefix(path, "mailFolders/"):
			folder, tail, _ := strings.Cut(strings.TrimPrefix(path, "mailFolders/"), "/")
			if tail == "" {
				_ = json.NewEncoder(w).Encode(map[string]any{"id": "folder-" + folder})
				return
			}
			if tail != "messages/delta" {
				t.Errorf("unexpected path %s", path)
				w.WriteHeader(500)
				return
			}
			if folder == g.expired && r.URL.Query().Get("$deltatoken") == "seed" {
				w.WriteHeader(http.StatusGone)
				_, _ = w.Write([]byte(`{"error":{"code":"syncStateNotFound"}}`))
				return
			}
			value := g.snapshot[folder]
			out := map[string]any{"value": value, "@odata.deltaLink": "https://graph.microsoft.com" + r.URL.Path + "?$deltatoken=fresh"}
			if g.page2[folder] != nil {
				if r.URL.Query().Get("$skiptoken") == "page2" {
					out["value"] = g.page2[folder]
				} else {
					delete(out, "@odata.deltaLink")
					out["@odata.nextLink"] = "https://graph.microsoft.com" + r.URL.Path + "?$skiptoken=page2"
				}
			}
			_ = json.NewEncoder(w).Encode(out)
		default:
			t.Errorf("unexpected Graph path %s", path)
			w.WriteHeader(500)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestGraphIdentityUpgradeRecoversPartialConversions(t *testing.T) {
	for _, tc := range []struct {
		name         string
		message      map[string]any
		status       int
		resolvedID   string
		resolveError int
		wantErr      bool
		wantRemoved  bool
	}{
		{"live message", graphMessageJSON("stable-recovered"), 0, "", 0, false, false},
		{"moved message", nil, 0, "stable-recovered", 0, false, false},
		{"confirmed missing", nil, 0, "", 0, false, true},
		{"provider unavailable", nil, http.StatusServiceUnavailable, "", 0, true, false},
		{"RFC lookup unavailable", nil, 0, "", http.StatusServiceUnavailable, true, false},
		{"empty message identity", map[string]any{}, 0, "", 0, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := &graphRecoveryProvider{translations: map[string]string{"regular-recover": ""}, messages: map[string]map[string]any{"regular-recover": tc.message}, messageErrors: map[string]int{"regular-recover": tc.status}, resolvedID: tc.resolvedID, resolveError: tc.resolveError}
			var events []captured
			var states []models.SyncState
			w := newGraphTestMail(t, g.serve(t), &events, &states)
			w.GraphData.Client.SetImmutableIDMode(true)
			id := uuid.New()
			w.EmailMessageMapRepository = &recoveryMessageMap{data: map[string]repository.EmailMessageData{"regular-recover": {UserID: w.UserID.String(), EmailID: w.ID.String(), MessageID: "regular-recover", ID: id.String(), ThreadID: "owned"}}}
			w.SyncContext = &graphRecoveryContext{recoveryRows: recoveryRows{rows: []repository.ProviderFolderMessage{{ID: id, ProviderID: "regular-recover", MessageID: "<stored@fake.test>", ProviderFolder: models.FolderInbox}}}}
			done, err := w.graphUpgradeIDs(t.Context())
			if (err != nil) != tc.wantErr || done == tc.wantErr {
				t.Fatalf("done=%v err=%v", done, err)
			}
			cur := w.tracker.folder(graphIdentityCursor)
			if tc.wantErr {
				if cur.Next != "" || cur.Done || len(events) != 0 {
					t.Fatalf("failed lookup advanced or deleted mail: cursor=%+v events=%v", cur, events)
				}
				return
			}
			if cur.Next != id.String() || !cur.Done || len(events) != 1 {
				t.Fatalf("upgrade failed to checkpoint: cursor=%+v events=%v", cur, events)
			}
			if tc.wantRemoved {
				if events[0].eventType != models.JobEventTypeRemoveEmail || events[0].body.(*models.JobEventRemoveEmail).ID != id {
					t.Fatal("confirmed missing message not removed with original identity")
				}
			} else {
				update := events[0].body.(*models.JobEventFolderUpdate)
				if update.ID != id || update.ProviderID != "stable-recovered" || !update.Relayed {
					t.Fatalf("recovered identity changed the conversation: %+v", update)
				}
			}
		})
	}
}

func TestGraphIdentityUpgradeResumesAfterTransientFailure(t *testing.T) {
	first := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	second := uuid.MustParse("00000000-0000-4000-8000-000000000002")
	g := &graphRecoveryProvider{translations: map[string]string{"regular-recover": ""}, messages: map[string]map[string]any{}, messageErrors: map[string]int{"regular-recover": http.StatusServiceUnavailable}}
	srv := g.serve(t)
	var events []captured
	var states []models.SyncState
	w := newGraphTestMail(t, srv, &events, &states)
	w.GraphData.Client.SetImmutableIDMode(true)
	maps := &recoveryMessageMap{data: map[string]repository.EmailMessageData{}}
	for key, id := range map[string]uuid.UUID{"regular-first": first, "regular-recover": second} {
		maps.data[key] = repository.EmailMessageData{UserID: w.UserID.String(), EmailID: w.ID.String(), MessageID: key, ID: id.String(), ThreadID: "owned"}
	}
	w.EmailMessageMapRepository = maps
	w.SyncContext = &graphRecoveryContext{recoveryRows: recoveryRows{rows: []repository.ProviderFolderMessage{
		{ID: first, ProviderID: "regular-first", ProviderFolder: models.FolderInbox},
		{ID: second, ProviderID: "regular-recover", MessageID: "<stored@fake.test>", ProviderFolder: models.FolderInbox},
	}}}
	if done, err := w.graphUpgradeIDs(t.Context()); err == nil || done {
		t.Fatalf("transient conversion marked upgrade done: done=%v err=%v", done, err)
	}
	if cur := w.tracker.folder(graphIdentityCursor); cur.Next != first.String() || cur.Done || len(events) != 1 {
		t.Fatalf("lost successful prefix or advanced past failure: cursor=%+v events=%v", cur, events)
	}
	g.messageErrors = nil
	g.resolvedID = "stable-recovered"
	reloaded := newGraphTestMail(t, srv, &events, &states)
	reloaded.ID, reloaded.UserID = w.ID, w.UserID
	reloaded.GraphData.Client.SetImmutableIDMode(true)
	reloaded.EmailMessageMapRepository, reloaded.SyncContext = maps, w.SyncContext
	reloaded.tracker = newSyncTracker(graphReloadState(t, w.tracker.state), func(models.SyncState) error { return nil })
	if done, err := reloaded.graphUpgradeIDs(t.Context()); err != nil || !done {
		t.Fatalf("retry failed to recover: done=%v err=%v", done, err)
	}
	if len(events) != 2 || maps.data["stable-first"].ID != first.String() || maps.data["stable-recovered"].ID != second.String() {
		t.Fatalf("retry changed identity or replayed prefix: events=%v maps=%v", events, maps.data)
	}
}

func TestGraphIdentityUpgradeWithoutRFCIdentityNeverDeletesOnLegacy404(t *testing.T) {
	g := &graphRecoveryProvider{translations: map[string]string{"regular-missing": ""}, messages: map[string]map[string]any{}}
	var events []captured
	var states []models.SyncState
	w := newGraphTestMail(t, g.serve(t), &events, &states)
	w.GraphData.Client.SetImmutableIDMode(true)
	w.SyncContext = &graphRecoveryContext{recoveryRows: recoveryRows{rows: []repository.ProviderFolderMessage{{ID: uuid.New(), ProviderID: "regular-missing"}}}}
	if done, err := w.graphUpgradeIDs(t.Context()); !errors.Is(err, repository.ErrSyncContextUnsupported) || done {
		t.Fatalf("missing stable identity did not retain legacy mode: done=%v err=%v", done, err)
	}
	if cur := w.tracker.folder(graphIdentityCursor); cur.Next != "" || cur.Done || len(events) != 0 {
		t.Fatalf("legacy 404 deleted or checkpointed mail: cursor=%+v events=%v", cur, events)
	}
}

func TestGraphRFCIdentityLookupRejectsMalformedResults(t *testing.T) {
	g := &graphRecoveryProvider{resolveMalformed: true}
	var events []captured
	var states []models.SyncState
	w := newGraphTestMail(t, g.serve(t), &events, &states)
	w.GraphData.Client.SetImmutableIDMode(true)
	if _, err := w.GraphData.Client.ResolveMessageID(t.Context(), "<stored@fake.test>"); err == nil {
		t.Fatal("malformed lookup reported definitive absence")
	}
}

type graphRecoveryContext struct{ recoveryRows }

func (*graphRecoveryContext) IsOwnConversation(_ context.Context, _, _ uuid.UUID, _ []string, thread string) (bool, error) {
	return thread == "conv-stable-reply", nil
}

type graphRecoveryBudget struct {
	backfill int
	lanes    []SyncLane
	observed int
}

func (*graphRecoveryBudget) Policy() models.SyncPolicy {
	return normalizePolicy(models.SyncPolicy{BackfillDays: 7, BackfillMessages: 2})
}
func (*graphRecoveryBudget) SetPolicy(models.SyncPolicy)             {}
func (*graphRecoveryBudget) RecordThrottledDay(context.Context) bool { return false }
func (b *graphRecoveryBudget) ObserveLive(_ context.Context, n int) bool {
	b.observed += n
	return false
}
func (b *graphRecoveryBudget) Admit(_ context.Context, lane SyncLane) Admission {
	b.lanes = append(b.lanes, lane)
	if lane == LaneBackfill {
		if b.backfill == 0 {
			return Admission{Until: time.Now().Add(time.Second)}
		}
		b.backfill--
	}
	return Admission{OK: true}
}

func graphReloadState(t *testing.T, state models.SyncState) *models.SyncState {
	t.Helper()
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	var out models.SyncState
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return &out
}

func TestGraphRecoveryBudgetedReplyAndNativeContinuationSurviveReload(t *testing.T) {
	for _, expired := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing completed Archive", true: "expired Inbox"}[expired], func(t *testing.T) {
			folder := msgraph.FolderArchive
			if expired {
				folder = msgraph.FolderInbox
			}
			g := &graphRecoveryProvider{snapshot: map[string][]any{folder: {map[string]any{"id": "stable-one"}}}, page2: map[string][]any{folder: {map[string]any{"id": "stable-two"}, map[string]any{"id": "stable-capped"}, map[string]any{"id": "stable-reply"}}}, messages: map[string]map[string]any{}}
			for _, id := range []string{"stable-one", "stable-two", "stable-capped", "stable-reply"} {
				g.messages[id] = graphMessageJSON(id)
			}
			g.messages["stable-reply"]["receivedDateTime"] = time.Now().Add(-90 * 24 * time.Hour).UTC().Format(time.RFC3339)
			if expired {
				g.expired = folder
			}
			srv := g.serve(t)
			var events []captured
			var states []models.SyncState
			w := newGraphTestMail(t, srv, &events, &states)
			w.GraphData.Client.SetImmutableIDMode(true)
			if !expired {
				delete(w.GraphData.Client.DeltaLinks, folder)
			}
			maps := &recoveryMessageMap{data: map[string]repository.EmailMessageData{}}
			w.EmailMessageMapRepository = maps
			w.SyncContext = &graphRecoveryContext{}
			w.tracker.state.BackfillStatus = models.SyncBackfillComplete
			budget := &graphRecoveryBudget{backfill: 1}
			w.gov = budget
			if err := w.SyncGraph(t.Context()); err != nil {
				t.Fatal(err)
			}
			ids := importedIDs(events)
			if !hasID(ids, "stable-one") || !hasID(ids, "stable-reply") || hasID(ids, "stable-two") || budget.observed != 0 {
				t.Fatalf("first replay imports=%v live-count=%d", ids, budget.observed)
			}
			if !strings.Contains(w.GraphData.Client.DeltaLinks[folder], "$skiptoken=page2") || w.tracker.state.LastSyncedAt != nil {
				t.Fatalf("deferred replay advanced freshness or lost native continuation: %v", w.tracker.state)
			}
			before, err := w.graphRecovery(folder)
			if err != nil || before.Count != 1 {
				t.Fatalf("saved recovery=%+v err=%v", before, err)
			}
			reloaded := newGraphTestMail(t, srv, &events, &states)
			reloaded.ID, reloaded.UserID = w.ID, w.UserID
			reloaded.EmailMessageMapRepository, reloaded.SyncContext = maps, w.SyncContext
			reloaded.GraphData.Client.SetImmutableIDMode(true)
			reloaded.GraphData.Client.DeltaLinks = cloneStringMap(w.GraphData.Client.DeltaLinks)
			reloaded.tracker = newSyncTracker(graphReloadState(t, w.tracker.state), func(st models.SyncState) error { states = append(states, st); return nil })
			reloaded.gov = &graphRecoveryBudget{backfill: 1}
			if err := reloaded.SyncGraph(t.Context()); err != nil {
				t.Fatal(err)
			}
			after, err := reloaded.graphRecovery(folder)
			if err != nil || !before.Since.Equal(after.Since) || after.Count != 2 || reloaded.graphRecovering(folder) {
				t.Fatalf("restarted recovery changed fixed window/count: before=%+v after=%+v err=%v", before, after, err)
			}
			ids = importedIDs(events)
			if len(ids) != 3 || !hasID(ids, "stable-two") || !strings.Contains(reloaded.GraphData.Client.DeltaLinks[folder], "$deltatoken=fresh") {
				t.Fatalf("eventual deduplicated replay imports=%v links=%v", ids, reloaded.GraphData.Client.DeltaLinks)
			}
			g.snapshot[folder], g.page2[folder] = nil, nil
			if err := reloaded.SyncGraph(t.Context()); err != nil || len(importedIDs(events)) != 3 {
				t.Fatalf("repeat created duplicate arrivals: %v %v", err, importedIDs(events))
			}
		})
	}
}

func TestGraphArchiveUpgradePreservesLegacyIdentityMovesAndFlags(t *testing.T) {
	id := uuid.New()
	g := &graphRecoveryProvider{snapshot: map[string][]any{
		msgraph.FolderInbox:   {map[string]any{"id": "stable-known", "@removed": map[string]any{"reason": "deleted"}}},
		msgraph.FolderArchive: {map[string]any{"id": "stable-known"}, map[string]any{"id": "stable-new"}},
	}, messages: map[string]map[string]any{"stable-known": graphMessageJSON("stable-known"), "stable-new": graphMessageJSON("stable-new")}}
	g.messages["stable-known"]["parentFolderId"] = "folder-archive"
	var events []captured
	var states []models.SyncState
	w := newGraphTestMail(t, g.serve(t), &events, &states)
	w.GraphData.Client.SetImmutableIDMode(true)
	delete(w.GraphData.Client.DeltaLinks, msgraph.FolderArchive)
	maps := &recoveryMessageMap{data: map[string]repository.EmailMessageData{"regular-known": {UserID: w.UserID.String(), EmailID: w.ID.String(), MessageID: "regular-known", ID: id.String(), ThreadID: "owned"}}}
	w.EmailMessageMapRepository = maps
	rows := &graphRecoveryContext{recoveryRows: recoveryRows{rows: []repository.ProviderFolderMessage{{ID: id, ProviderID: "regular-known", ProviderFolder: models.FolderInbox}}}}
	w.SyncContext = rows
	w.tracker.state.BackfillStatus = models.SyncBackfillComplete
	baseEmit := w.onEvent
	w.onEvent = func(kind models.JobEventType, body any) error {
		if update, ok := body.(*models.JobEventFolderUpdate); ok && update.ID == id {
			if update.Relayed {
				rows.rows[0].ProviderID = update.ProviderID
			} else {
				rows.rows[0].ProviderFolder = update.Folder
			}
		}
		return baseEmit(kind, body)
	}
	if err := w.SyncGraph(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(importedIDs(events)) != 1 || maps.data["stable-known"].ID != id.String() || maps.data["regular-known"].ID != id.String() || rows.rows[0].ProviderFolder != models.FolderArchive {
		t.Fatalf("move changed identity or lost Archive: ids=%v maps=%v rows=%v", importedIDs(events), maps.data, rows.rows)
	}
	for _, ev := range events {
		if ev.eventType == models.JobEventTypeRemoveEmail {
			t.Fatal("move-out deleted the platform conversation")
		}
	}
	g.seen = true
	before := len(events)
	reloaded := newGraphTestMail(t, g.serve(t), &events, &states)
	reloaded.ID, reloaded.UserID = w.ID, w.UserID
	reloaded.GraphData.Client.SetImmutableIDMode(true)
	reloaded.GraphData.Client.DeltaLinks = cloneStringMap(w.GraphData.Client.DeltaLinks)
	reloaded.EmailMessageMapRepository, reloaded.SyncContext = maps, rows
	reloaded.tracker = newSyncTracker(graphReloadState(t, w.tracker.state), func(models.SyncState) error { return nil })
	if err := reloaded.SyncGraph(t.Context()); err != nil {
		t.Fatal(err)
	}
	flags := false
	for _, ev := range events[before:] {
		if ev.eventType == models.JobEventTypeFlagsAdd {
			update := ev.body.(*models.JobEventFlags)
			flags = flags || update.ID == id
		}
	}
	if !flags || len(importedIDs(events)) != 1 {
		t.Fatalf("Archive read-state update lost or reimported: flags=%v arrivals=%v", flags, importedIDs(events))
	}
}

func TestGraphRecoveryPendingBaselineAndStoredRemovalScanResumeAfterReload(t *testing.T) {
	g := &graphRecoveryProvider{snapshot: map[string][]any{}, expired: msgraph.FolderInbox, messages: map[string]map[string]any{}}
	srv := g.serve(t)
	var events []captured
	var states []models.SyncState
	w := newGraphTestMail(t, srv, &events, &states)
	w.GraphData.Client.SetImmutableIDMode(true)
	w.tracker.state.BackfillStatus = models.SyncBackfillComplete
	w.tracker.setFolder(graphIdentityCursor, models.SyncFolderCursor{Done: true})
	rows := &graphRecoveryContext{}
	maps := &recoveryMessageMap{data: map[string]repository.EmailMessageData{}}
	for n := 1; n <= graphBackfillPage+1; n++ {
		id := uuid.UUID{}
		id[15] = byte(n)
		providerID := "stable-" + id.String()
		rows.rows = append(rows.rows, repository.ProviderFolderMessage{ID: id, ProviderID: providerID, ProviderFolder: models.FolderInbox})
		maps.data[providerID] = repository.EmailMessageData{ID: id.String(), MessageID: providerID}
	}
	w.EmailMessageMapRepository, w.SyncContext = maps, rows
	if err := w.SyncGraph(t.Context()); err != nil {
		t.Fatal(err)
	}
	removals := func() int {
		n := 0
		for _, ev := range events {
			if ev.eventType == models.JobEventTypeRemoveEmail {
				n++
			}
		}
		return n
	}
	if removals() != graphBackfillPage || !strings.Contains(w.GraphData.Client.DeltaLinks[msgraph.FolderInbox], "seed") {
		t.Fatalf("unreconciled baseline installed: events=%d links=%v", len(events), w.GraphData.Client.DeltaLinks)
	}
	state, err := w.graphRecovery(msgraph.FolderInbox)
	if err != nil || !strings.Contains(state.PendingLink, "fresh") {
		t.Fatalf("captured baseline missing: %+v %v", state, err)
	}
	countInbox := func() int {
		n := 0
		for _, req := range g.requests {
			if strings.HasPrefix(req, "mailFolders/inbox/messages/delta") {
				n++
			}
		}
		return n
	}
	before := countInbox()
	reloaded := newGraphTestMail(t, srv, &events, &states)
	reloaded.ID, reloaded.UserID = w.ID, w.UserID
	reloaded.GraphData.Client.SetImmutableIDMode(true)
	reloaded.GraphData.Client.DeltaLinks = cloneStringMap(w.GraphData.Client.DeltaLinks)
	reloaded.EmailMessageMapRepository, reloaded.SyncContext = maps, rows
	reloaded.tracker = newSyncTracker(graphReloadState(t, w.tracker.state), func(models.SyncState) error { return nil })
	if err := reloaded.SyncGraph(t.Context()); err != nil {
		t.Fatal(err)
	}
	if removals() != graphBackfillPage+1 || countInbox() != before || !strings.Contains(reloaded.GraphData.Client.DeltaLinks[msgraph.FolderInbox], "fresh") || reloaded.tracker.state.LastSyncedAt != nil {
		t.Fatalf("stored scan did not resume/freshness claimed before live catch-up: events=%d inbox=%d/%d state=%v", len(events), countInbox(), before, reloaded.tracker.state)
	}
	if err := reloaded.SyncGraph(t.Context()); err != nil || reloaded.tracker.state.LastSyncedAt == nil {
		t.Fatalf("fresh baseline did not catch up: err=%v", err)
	}
}

func TestGraphRecoveryCompletionSurvivesLaterLiveCursorAdvancement(t *testing.T) {
	var events []captured
	var states []models.SyncState
	w := newGraphTestMail(t, newFakeGraph().serve(t), &events, &states)
	if err := w.graphRecoveryStart(t.Context(), msgraph.FolderInbox); err != nil {
		t.Fatal(err)
	}
	if err := w.graphRecoveryCheckpoint(t.Context(), msgraph.FolderInbox, "baseline"); err != nil {
		t.Fatal(err)
	}
	w.GraphData.Client.DeltaLinks[msgraph.FolderInbox] = "baseline"
	if w.graphRecovering(msgraph.FolderInbox) {
		t.Fatal("installed baseline still recovering")
	}
	w.GraphData.Client.DeltaLinks[msgraph.FolderInbox] = "later-live-checkpoint"
	if w.graphRecovering(msgraph.FolderInbox) {
		t.Fatal("later live cursor reopened completed recovery")
	}
	legacy := graphReloadState(t, w.tracker.state)
	if legacy.BackfillCursor.Folders["graph:delta-recovery:inbox"].Next == "" || !legacy.BackfillCursor.Folders["graph:delta-recovery:inbox"].Done {
		t.Fatal("existing cursor protocol lost opaque recovery metadata")
	}
}

func TestGraphImmutableAliasRetainsHiddenLegacyMappingAndRejectsCollision(t *testing.T) {
	g := &graphRecoveryProvider{snapshot: map[string][]any{}, messages: map[string]map[string]any{}}
	var events []captured
	var states []models.SyncState
	w := newGraphTestMail(t, g.serve(t), &events, &states)
	w.GraphData.Client.SetImmutableIDMode(true)
	id := uuid.NewString()
	maps := &recoveryMessageMap{data: map[string]repository.EmailMessageData{"regular-hidden": {ID: id, MessageID: "regular-hidden"}}}
	w.EmailMessageMapRepository = maps
	known, err := w.graphMessageMap(t.Context(), "stable-hidden")
	if err != nil || known == nil || known.ID != id || maps.data["regular-hidden"].ID != id || len(importedIDs(events)) != 0 {
		t.Fatalf("hidden legacy map became a new arrival: known=%v err=%v", known, err)
	}
	other := uuid.NewString()
	maps.data["stable-conflict"] = repository.EmailMessageData{ID: other}
	if err := w.graphAddAlias(t.Context(), repository.EmailMessageData{MessageID: "stable-conflict", ID: id}); err == nil || maps.data["stable-conflict"].ID != other {
		t.Fatal("alias overwrote an unrelated provider mapping")
	}
}

func TestGraphRecoveryArrivalPublicationFailureRetriesAfterReload(t *testing.T) {
	g := newFakeGraph()
	g.live = []string{"reply"}
	var events []captured
	var states []models.SyncState
	srv := g.serve(t)
	w := newGraphTestMail(t, srv, &events, &states)
	delete(w.GraphData.Client.DeltaLinks, msgraph.FolderInbox)
	w.tracker.state.BackfillStatus = models.SyncBackfillComplete
	maps := &recoveryMessageMap{data: map[string]repository.EmailMessageData{}}
	w.EmailMessageMapRepository = maps
	base := w.onEvent
	w.onEvent = func(kind models.JobEventType, body any) error {
		if kind == models.JobEventTypeNewEmail {
			return errors.New("publication unavailable")
		}
		return base(kind, body)
	}
	if err := w.SyncGraph(t.Context()); err != nil {
		t.Fatal(err)
	}
	if w.GraphData.Client.DeltaLinks[msgraph.FolderInbox] != "" || len(maps.data) != 0 {
		t.Fatalf("failed publication advanced cursor or retained suppressing map: %v %v", w.GraphData.Client.DeltaLinks, maps.data)
	}
	g.live = []string{"reply"}
	reloaded := newGraphTestMail(t, srv, &events, &states)
	reloaded.EmailMessageMapRepository = maps
	reloaded.GraphData.Client.DeltaLinks = cloneStringMap(w.GraphData.Client.DeltaLinks)
	reloaded.tracker = newSyncTracker(graphReloadState(t, w.tracker.state), func(models.SyncState) error { return nil })
	if err := reloaded.SyncGraph(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(importedIDs(events)) != 1 || reloaded.GraphData.Client.DeltaLinks[msgraph.FolderInbox] == "" {
		t.Fatalf("arrival not replayed: ids=%v links=%v", importedIDs(events), reloaded.GraphData.Client.DeltaLinks)
	}
}

func TestGraphOldBackendRetainsLegacySyncWithoutImmutableClaim(t *testing.T) {
	g := newFakeGraph()
	g.live = []string{"live"}
	var events []captured
	var states []models.SyncState
	w := newGraphTestMail(t, g.serve(t), &events, &states)
	w.GraphData.Client.SetImmutableIDMode(true)
	w.SyncContext = &recoveryRows{err: repository.ErrSyncContextUnsupported}
	w.tracker.state.BackfillStatus = models.SyncBackfillComplete
	if err := w.SyncGraph(t.Context()); err != nil {
		t.Fatal(err)
	}
	if w.GraphData.Client.ImmutableIDMode() || len(importedIDs(events)) != 1 {
		t.Fatalf("old backend stalled sync or claimed immutable reconciliation: immutable=%v imports=%v", w.GraphData.Client.ImmutableIDMode(), importedIDs(events))
	}
}
