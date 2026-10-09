package wmail

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	goimap "github.com/emersion/go-imap/v2"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/client/smtpimap/imap"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func imapReload(t *testing.T, w *WMail, conn ImapConn, budget syncBudget) (*WMail, *[]captured) {
	t.Helper()
	// Use the existing JSON wire shape, not shared in-memory state, to model restart.
	raw, err := json.Marshal(w.tracker.state)
	if err != nil {
		t.Fatal(err)
	}
	var state models.SyncState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	saved := *w.SmtpImapData.Mailboxes[0]
	next, events := newIMAPTestMail(conn, budget, &saved)
	next.EmailMessageMapRepository = w.EmailMessageMapRepository
	next.tracker = newSyncTracker(&state, func(models.SyncState) error { return nil })
	return next, events
}

func TestImapCompletedFolderRecoveryResumesAfterDeferralAndReload(t *testing.T) {
	for _, generation := range []bool{false, true} {
		t.Run(map[bool]string{false: "new populated folder", true: "new generation"}[generation], func(t *testing.T) {
			box := models.Mailbox{Name: "Archive", UIDValidity: 9, UIDNext: 4, HighestModSeq: 100}
			conn := &fakeImapConn{folders: []models.Mailbox{box}, all: []goimap.UID{1, 2, 3}}
			saved := &models.Mailbox{Name: "Archive", UIDValidity: 8, UIDNext: 90, HighestModSeq: 100}
			w, events := newIMAPTestMail(conn, &fixedBudget{allow: 1}, saved)
			if !generation {
				w.SmtpImapData.Mailboxes = nil
			}
			maps := &recoveryMessageMap{data: map[string]repository.EmailMessageData{}}
			knownID := uuid.NewString()
			maps.data["<2@fake.test>"] = repository.EmailMessageData{ID: knownID, MessageID: "<2@fake.test>"}
			w.EmailMessageMapRepository = maps
			w.tracker.setFolder("Archive", models.SyncFolderCursor{UID: 80, Done: true})
			oldSuccess := time.Now().Add(-time.Hour)
			w.tracker.state.LastSyncedAt = &oldSuccess
			if err := w.Sync(t.Context()); err != nil {
				t.Fatal(err)
			}
			cur := w.tracker.folder("Archive")
			recovery := imapRecoveryCursor(cur)
			if cur.Done || cur.UID != 3 || recovery == nil || recovery.Generation != 9 || recovery.Synced != 1 {
				t.Fatalf("deferred recovery = %+v / %+v", cur, recovery)
			}
			if len(mailboxEvents(*events, models.JobEventTypeNewEmail)) != 1 {
				t.Fatal("newest recovery arrival missing")
			}
			if !w.tracker.state.LastSyncedAt.Equal(oldSuccess) {
				t.Fatal("deferred recovery refreshed success")
			}
			cutoff := recovery.Since
			next, resumed := imapReload(t, w, conn, &fixedBudget{allow: 10})
			if err := next.Sync(t.Context()); err != nil {
				t.Fatal(err)
			}
			cur = next.tracker.folder("Archive")
			if !cur.Done || cur.UID != 1 || !imapRecoveryCursor(cur).Since.Equal(cutoff) {
				t.Fatalf("restarted recovery = %+v", cur)
			}
			if len(maps.data) != 3 || len(mailboxEvents(*resumed, models.JobEventTypeNewEmail)) != 1 || maps.data["<2@fake.test>"].ID != knownID {
				t.Fatal("recovery lost or duplicated arrivals")
			}
			if next.tracker.state.BackfillStatus != models.SyncBackfillComplete || next.tracker.state.BackfillSynced != 0 {
				t.Fatal("global history reset")
			}
			before := conn.searches
			again, later := imapReload(t, next, conn, &fixedBudget{allow: 10})
			if err := again.Sync(t.Context()); err != nil {
				t.Fatal(err)
			}
			if conn.searches != before || len(mailboxEvents(*later, models.JobEventTypeNewEmail)) != 0 {
				t.Fatal("completed recovery reimported on restart")
			}
		})
	}
}

func TestImapRecoveryDoesNotBaselineBeforeReplayStateRelays(t *testing.T) {
	conn := &fakeImapConn{folders: []models.Mailbox{{Name: "Archive", UIDValidity: 9, UIDNext: 3, HighestModSeq: 100}}, all: []goimap.UID{1, 2}}
	w, events := newIMAPTestMail(conn, &fixedBudget{allow: 10}, &models.Mailbox{Name: "Archive", UIDValidity: 8})
	w.tracker.emit = func(models.SyncState) error { return errors.New("state unavailable") }
	if err := w.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	if w.SmtpImapData.Mailboxes[0].UIDValidity != 8 || len(mailboxEvents(*events, models.JobEventTypeMailboxUpdate)) != 0 || conn.fetches != 0 {
		t.Fatal("baseline outran replay checkpoint")
	}
	w.tracker.emit = func(models.SyncState) error { return nil }
	if err := w.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !w.tracker.folder("Archive").Done || len(mailboxEvents(*events, models.JobEventTypeNewEmail)) != 2 {
		t.Fatal("checkpoint retry did not recover folder")
	}
}

func TestImapBodyFailurePinsLiveAndRecoveryUntilReload(t *testing.T) {
	for _, recovery := range []bool{false, true} {
		t.Run(map[bool]string{false: "live", true: "recovery"}[recovery], func(t *testing.T) {
			box := models.Mailbox{Name: "INBOX", UIDValidity: 7, UIDNext: 2, HighestModSeq: 101}
			conn := &fakeImapConn{folders: []models.Mailbox{box}, changed: []goimap.UID{1}, all: []goimap.UID{1}, bodyErr: errx.ErrMailServerUnreachable}
			w, events := newIMAPTestMail(conn, &fixedBudget{allow: 10}, &models.Mailbox{Name: "INBOX", UIDValidity: 7, UIDNext: 1, HighestModSeq: 100})
			if recovery {
				w.SmtpImapData.Mailboxes[0].UIDValidity = 6
			}
			maps := &recoveryMessageMap{data: map[string]repository.EmailMessageData{}}
			w.EmailMessageMapRepository = maps
			if err := w.Sync(t.Context()); err == nil {
				t.Fatal("body failure swallowed")
			}
			if len(maps.data) != 0 || len(mailboxEvents(*events, models.JobEventTypeNewEmail)) != 0 || w.tracker.state.BackfillSynced != 0 || w.tracker.folder("INBOX").UID != 0 || w.tracker.folder("INBOX").Done {
				t.Fatal("body failure imported partial message or advanced recovery")
			}
			if !recovery && w.SmtpImapData.Mailboxes[0].UIDNext != 1 {
				t.Fatal("body failure advanced live cursor")
			}
			conn.bodyErr = nil
			next, resumed := imapReload(t, w, conn, &fixedBudget{allow: 10})
			if err := next.Sync(t.Context()); err != nil {
				t.Fatal(err)
			}
			arrivals := mailboxEvents(*resumed, models.JobEventTypeNewEmail)
			if len(arrivals) != 1 || len(maps.data) != 1 {
				t.Fatal("hydration was not retried exactly once")
			}
			if arrivals[0].body.(*models.JobEventNewEmail).Message.BodyText != "complete body" {
				t.Fatal("arrival did not contain hydrated body")
			}
			if next.SmtpImapData.Mailboxes[0].UIDNext != 2 {
				t.Fatal("live cursor did not advance after hydration")
			}
		})
	}
}

func TestImapNomodseqUsesUIDNextAndPeriodicFlags(t *testing.T) {
	for _, listedModseq := range []uint64{0, 100} {
		t.Run(map[uint64]string{0: "status NOMODSEQ", 100: "select NOMODSEQ"}[listedModseq], func(t *testing.T) {
			conn := &fakeImapConn{folders: []models.Mailbox{{Name: "INBOX", UIDValidity: 7, UIDNext: 3, HighestModSeq: listedModseq}}, changed: []goimap.UID{2}, view: &imap.Selected{UIDNext: 3}, flags: map[uint32]imap.FlagState{1: {MessageID: "<known@test>"}}}
			w, events := newIMAPTestMail(conn, &fixedBudget{allow: 10}, &models.Mailbox{Name: "INBOX", UIDValidity: 7, UIDNext: 2, HighestModSeq: listedModseq})
			if err := w.Sync(t.Context()); err != nil {
				t.Fatal(err)
			}
			if len(mailboxEvents(*events, models.JobEventTypeNewEmail)) != 1 || conn.flagScans != 1 || conn.uidSearches != 1 || conn.modseqSearches != 0 {
				t.Fatal("NOMODSEQ ignored arrival or fallback flags")
			}
			w.EmailMessageMapRepository = knownMessageMap{id: uuid.NewString()}
			conn.flags[1] = imap.FlagState{MessageID: "<known@test>", Flags: []string{`\Seen`}}
			w.flagScan["INBOX"].at = time.Now().Add(-2 * config.ImapFlagScanInterval)
			if err := w.Sync(t.Context()); err != nil {
				t.Fatal(err)
			}
			if len(mailboxEvents(*events, models.JobEventTypeEmailUpdate)) != 1 {
				t.Fatal("NOMODSEQ dropped periodic read update")
			}
		})
	}
}

func TestImapBodyFailurePinsInitialHistoryUntilReload(t *testing.T) {
	box := models.Mailbox{Name: "INBOX", UIDValidity: 7, UIDNext: 2, HighestModSeq: 100, Messages: 1}
	conn := &fakeImapConn{folders: []models.Mailbox{box}, changed: []goimap.UID{1}, all: []goimap.UID{1}, bodyErr: errx.ErrMailServerUnreachable}
	w, events := newIMAPTestMail(conn, &fixedBudget{allow: 10}, &box)
	w.tracker = newSyncTracker(nil, func(models.SyncState) error { return nil })
	maps := &recoveryMessageMap{data: map[string]repository.EmailMessageData{}}
	w.EmailMessageMapRepository = maps
	if err := w.Sync(t.Context()); err == nil {
		t.Fatal("history body failure swallowed")
	}
	if len(maps.data) != 0 || len(mailboxEvents(*events, models.JobEventTypeNewEmail)) != 0 || w.tracker.state.BackfillSynced != 0 || w.tracker.folder("INBOX").UID != 0 || w.tracker.folder("INBOX").Done {
		t.Fatal("history advanced after body failure")
	}
	conn.bodyErr = nil
	next, resumed := imapReload(t, w, conn, &fixedBudget{allow: 10})
	if err := next.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(maps.data) != 1 || len(mailboxEvents(*resumed, models.JobEventTypeNewEmail)) != 1 || next.tracker.state.BackfillSynced != 1 || !next.tracker.folder("INBOX").Done {
		t.Fatal("history hydration did not resume")
	}
}

func TestImapFlagScanRetriesBothChangesAfterPublicationAbort(t *testing.T) {
	conn := &fakeImapConn{folders: []models.Mailbox{{Name: "INBOX", UIDValidity: 7, UIDNext: 3}}, noCondStore: true, flags: map[uint32]imap.FlagState{1: {MessageID: "<one@test>"}, 2: {MessageID: "<two@test>"}}}
	w, events := newIMAPTestMail(conn, &fixedBudget{allow: 10}, &models.Mailbox{Name: "INBOX", UIDValidity: 7, UIDNext: 3})
	w.EmailMessageMapRepository = knownMessageMap{id: uuid.NewString()}
	if err := w.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	for uid, st := range conn.flags {
		st.Flags = []string{`\Seen`}
		conn.flags[uid] = st
	}
	emit := w.onEvent
	w.onEvent = func(kind models.JobEventType, body any) error {
		if kind == models.JobEventTypeEmailUpdate {
			return errors.New("publish unavailable")
		}
		return emit(kind, body)
	}
	w.flagScan["INBOX"].at = time.Now().Add(-2 * config.ImapFlagScanInterval)
	if err := w.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	w.onEvent = emit
	w.flagScan["INBOX"].at = time.Now().Add(-2 * config.ImapFlagScanInterval)
	if err := w.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(mailboxEvents(*events, models.JobEventTypeEmailUpdate)) != 2 {
		t.Fatal("failed or unvisited flag changes were absorbed into baseline")
	}
}
