package wmail

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/client/msgraph"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type countingGraphMap struct {
	recoveryMessageMap
	gets int
	err  error
}

func (m *countingGraphMap) Get(ctx context.Context, userID, emailID uuid.UUID, id string) (*repository.EmailMessageData, error) {
	m.gets++
	if m.err != nil {
		return nil, m.err
	}
	return m.recoveryMessageMap.Get(ctx, userID, emailID, id)
}

func TestGraphKnownMessageSingleLookup(t *testing.T) {
	for _, tc := range []struct {
		name, id string
		seen     bool
		failEmit bool
		failGet  bool
	}{
		{name: "seen", seen: true}, {name: "unread"},
		{name: "event failure", failEmit: true},
		{name: "lookup failure", failGet: true},
		{name: "invalid mapping", id: "invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var events []captured
			var relayed []models.SyncState
			w := newGraphTestMail(t, newFakeGraph().serve(t), &events, &relayed)
			id := tc.id
			if id == "" {
				id = uuid.NewString()
			}
			maps := &countingGraphMap{recoveryMessageMap: recoveryMessageMap{data: map[string]repository.EmailMessageData{
				"known": {ID: id, MessageID: "known"},
			}}}
			injected := errors.New("injected failure")
			if tc.failGet {
				maps.err = injected
			}
			w.EmailMessageMapRepository = maps
			if tc.failEmit {
				w.onEvent = func(models.JobEventType, any) error { return injected }
			}
			added, err := w.onGraphMessageSeen(t.Context(), msgraph.FolderInbox, "known", tc.seen)
			if maps.gets != 1 {
				t.Fatalf("got %d broker lookups, want 1", maps.gets)
			}
			if tc.failEmit || tc.failGet || tc.id != "" {
				if err == nil || added || len(events) != 0 {
					t.Fatalf("failure advanced message: added=%v err=%v events=%v", added, err, events)
				}
				if tc.failGet && !errors.Is(err, msgraph.ErrStop) {
					t.Fatalf("lookup failure did not stop the pass: %v", err)
				}
				if tc.failEmit && !errors.Is(err, injected) {
					t.Fatalf("publish failure not propagated: %v", err)
				}
				return
			}
			if err != nil || !added || len(events) != 2 {
				t.Fatalf("known message: added=%v err=%v events=%v", added, err, events)
			}
			want := models.JobEventTypeFlagsRemove
			if tc.seen {
				want = models.JobEventTypeFlagsAdd
			}
			flags := events[0].body.(*models.JobEventFlags)
			if events[0].eventType != want || flags.ID.String() != id || flags.UserID != w.UserID || flags.EmailID != w.ID || len(flags.Flags) != 1 || flags.Flags[0] != "\\Seen" {
				t.Fatalf("incorrect flag event: %+v", events[0])
			}
			folder := events[1].body.(*models.JobEventFolderUpdate)
			if events[1].eventType != models.JobEventTypeFolderUpdate || folder.ID.String() != id || folder.Folder != models.FolderInbox {
				t.Fatalf("incorrect folder event: %+v", events[1])
			}
		})
	}
}

func BenchmarkGraphKnownMessage(b *testing.B) {
	maps := &countingGraphMap{recoveryMessageMap: recoveryMessageMap{data: map[string]repository.EmailMessageData{
		"known": {ID: uuid.NewString(), MessageID: "known"},
	}}}
	w := &WMail{
		ID: uuid.New(), UserID: uuid.New(),
		EmailMessageMapRepository: maps,
		GraphData:                 &GraphData{Client: &msgraph.Client{}},
	}
	w.onEvent = func(models.JobEventType, any) error { return nil }
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := w.onGraphMessageSeen(b.Context(), msgraph.FolderInbox, "known", true); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(maps.gets)/float64(b.N), "lookups/op")
}

// A walk that re-reads a folder relays a known message's state only when it changed.
func TestGraphKnownMessageRelaysOnlyChanges(t *testing.T) {
	var events []captured
	var relayed []models.SyncState
	w := newGraphTestMail(t, newFakeGraph().serve(t), &events, &relayed)
	w.EmailMessageMapRepository = &countingGraphMap{recoveryMessageMap: recoveryMessageMap{data: map[string]repository.EmailMessageData{
		"known": {ID: uuid.NewString(), MessageID: "known"},
	}}}
	injected := errors.New("injected failure")
	fail := false
	emit := w.onEvent
	w.onEvent = func(typ models.JobEventType, body any) error {
		if fail {
			return injected
		}
		return emit(typ, body)
	}
	seen := func(folder string, read bool) {
		t.Helper()
		if added, err := w.onGraphMessageSeen(t.Context(), folder, "known", read); err != nil || !added {
			t.Fatalf("seen %s read=%v: added=%v err=%v", folder, read, added, err)
		}
	}
	expect := func(step string, n int) {
		t.Helper()
		if len(events) != n {
			t.Fatalf("%s: %d events relayed, want %d", step, len(events), n)
		}
	}

	seen(msgraph.FolderSent, true)
	seen(msgraph.FolderSent, true)
	seen(msgraph.FolderSent, true)
	expect("unchanged walks", 2)
	seen(msgraph.FolderSent, false)
	expect("read state changed", 4)
	seen(msgraph.FolderArchive, false)
	expect("folder changed", 6)

	fail = true
	if _, err := w.onGraphMessageSeen(t.Context(), msgraph.FolderInbox, "known", false); !errors.Is(err, injected) {
		t.Fatalf("publish failure not propagated: %v", err)
	}
	fail = false
	seen(msgraph.FolderInbox, false)
	expect("relay retried after a failed publish", 8)

	if err := w.onGraphMessageRemove(t.Context(), "known"); err != nil {
		t.Fatal(err)
	}
	removed := len(events)
	seen(msgraph.FolderInbox, false)
	expect("state relayed again after a removal", removed+2)
}

// A reconcile relays the read state it fetched, and that is what a later walk compares against.
func TestGraphReconcileReplacesTheRelayedState(t *testing.T) {
	var events []captured
	var relayed []models.SyncState
	w := newGraphTestMail(t, newFakeGraph().serve(t), &events, &relayed)
	id := uuid.NewString()
	w.EmailMessageMapRepository = &countingGraphMap{recoveryMessageMap: recoveryMessageMap{data: map[string]repository.EmailMessageData{
		"known": {ID: id, MessageID: "known"},
	}}}
	if _, err := w.onGraphMessageSeen(t.Context(), msgraph.FolderInbox, "known", true); err != nil {
		t.Fatal(err)
	}
	// The fake mailbox reports the message unread, so the reconcile relays unread.
	if err := w.graphReconcileMessage(t.Context(), id, "known", msgraph.FolderInbox); err != nil {
		t.Fatal(err)
	}
	if last := events[len(events)-1]; last.eventType != models.JobEventTypeFlagsRemove {
		t.Fatalf("reconcile relayed %s, want the fetched unread state", last.eventType)
	}
	before := len(events)
	if _, err := w.onGraphMessageSeen(t.Context(), msgraph.FolderInbox, "known", true); err != nil {
		t.Fatal(err)
	}
	if len(events) != before+2 || events[before].eventType != models.JobEventTypeFlagsAdd {
		t.Fatalf("read state after a reconcile relayed unread was not relayed: %v", events[before:])
	}
	if _, err := w.onGraphMessageSeen(t.Context(), msgraph.FolderInbox, "known", true); err != nil {
		t.Fatal(err)
	}
	if len(events) != before+2 {
		t.Fatalf("unchanged state relayed again: %v", events[before:])
	}
}

// A relay that fails after its flag event leaves nothing cached, so a later
// return to the earlier state is still relayed.
func TestGraphKnownMessagePartialRelayIsNotRemembered(t *testing.T) {
	var events []captured
	var relayed []models.SyncState
	w := newGraphTestMail(t, newFakeGraph().serve(t), &events, &relayed)
	w.EmailMessageMapRepository = &countingGraphMap{recoveryMessageMap: recoveryMessageMap{data: map[string]repository.EmailMessageData{
		"known": {ID: uuid.NewString(), MessageID: "known"},
	}}}
	failFolder := false
	emit := w.onEvent
	w.onEvent = func(typ models.JobEventType, body any) error {
		if failFolder && typ == models.JobEventTypeFolderUpdate {
			return errors.New("injected failure")
		}
		return emit(typ, body)
	}
	if _, err := w.onGraphMessageSeen(t.Context(), msgraph.FolderInbox, "known", true); err != nil {
		t.Fatal(err)
	}
	failFolder = true
	if _, err := w.onGraphMessageSeen(t.Context(), msgraph.FolderInbox, "known", false); err == nil {
		t.Fatal("folder publish failure not propagated")
	}
	failFolder = false
	before := len(events)
	if _, err := w.onGraphMessageSeen(t.Context(), msgraph.FolderInbox, "known", true); err != nil {
		t.Fatal(err)
	}
	if len(events) != before+2 || events[before].eventType != models.JobEventTypeFlagsAdd {
		t.Fatalf("read state after a partial unread relay was not relayed: %v", events[before:])
	}
}
