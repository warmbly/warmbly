package wmail

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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

type knownRecoveryConn struct {
	*fakeImapConn
	message models.EmailMessageData
}

type initialRecoveryConn struct {
	*folderIdentityRecoveryConn
	since  []time.Time
	bodies []string
}

func (c *initialRecoveryConn) SearchSince(since time.Time) ([]goimap.UID, *errx.MailError) {
	c.since = append(c.since, since)
	return c.fakeImapConn.SearchSince(since)
}

func (c *initialRecoveryConn) FetchBody(f *imap.Fetched) *errx.MailError {
	c.bodies = append(c.bodies, f.Email.MessageID)
	return c.fakeImapConn.FetchBody(f)
}

type initialRecoveryBudget struct {
	fixedBudget
	limit int
}

func (b *initialRecoveryBudget) Policy() models.SyncPolicy {
	policy := b.fixedBudget.Policy()
	policy.BackfillMessages = b.limit
	return policy
}

func newRunningRecoveryMail(t *testing.T, budget syncBudget) (*WMail, *[]captured, *initialRecoveryConn, *recoveryMessageMap) {
	t.Helper()
	conn := &initialRecoveryConn{folderIdentityRecoveryConn: &folderIdentityRecoveryConn{fakeImapConn: &fakeImapConn{
		folders: []models.Mailbox{{Name: "INBOX", UIDValidity: 9, UIDNext: 4, HighestModSeq: 5}},
		all:     uidRange(3), changed: uidRange(3),
	}}}
	w, events := newIMAPTestMail(conn, budget, &models.Mailbox{Name: "INBOX", UIDValidity: 8, UIDNext: 500, HighestModSeq: 5})
	since, started, success := time.Now().Add(-30*24*time.Hour), time.Now().Add(-16*24*time.Hour), time.Now().Add(-time.Hour)
	w.tracker = newSyncTracker(&models.SyncState{BackfillStatus: models.SyncBackfillRunning, BackfillSince: &since, BackfillStartedAt: &started, BackfillSynced: 2, LastSyncedAt: &success}, func(models.SyncState) error { return nil })
	w.tracker.setFolder("INBOX", models.SyncFolderCursor{UID: 50, Done: true})
	maps := &recoveryMessageMap{data: map[string]repository.EmailMessageData{"<2@fake.test>": {ID: uuid.NewString(), MessageID: "<2@fake.test>"}}}
	w.EmailMessageMapRepository = maps
	return w, events, conn, maps
}

func TestImapRunningImportGenerationRecoveryPreservesWindowCountersAndReload(t *testing.T) {
	for _, outcome := range []string{"complete", "deferred", "relay failure", "global cap", "cap already reached"} {
		t.Run(outcome, func(t *testing.T) {
			budget := &initialRecoveryBudget{fixedBudget: fixedBudget{allow: 10}, limit: 4}
			if outcome == "deferred" {
				budget.allow = 1
			}
			if outcome == "global cap" {
				budget.limit = 3
			}
			if outcome == "cap already reached" {
				budget.limit = 2
				budget.allow = 0
			}
			w, events, conn, maps := newRunningRecoveryMail(t, budget)
			since, started, success := *w.tracker.state.BackfillSince, *w.tracker.state.BackfillStartedAt, *w.tracker.state.LastSyncedAt
			knownID := maps.data["<2@fake.test>"].ID
			if outcome == "relay failure" {
				emit := w.onEvent
				w.onEvent = func(kind models.JobEventType, body any) error {
					if kind == models.JobEventTypeEmailUpdate {
						return errors.New("known generation relay failed")
					}
					return emit(kind, body)
				}
			}
			if err := w.Sync(t.Context()); err != nil {
				t.Fatal(err)
			}
			allEvents := append([]captured{}, (*events)...)
			if outcome == "deferred" || outcome == "relay failure" {
				cur := w.tracker.folder("INBOX")
				recovery := imapRecoveryCursor(cur)
				wantUID := uint32(2)
				if outcome == "relay failure" {
					wantUID = 3
				}
				if recovery == nil || !recovery.Initial || recovery.Generation != 9 || !recovery.Since.Equal(since) || cur.Done || cur.UID != wantUID || w.tracker.state.BackfillSynced != 3 || w.tracker.state.BackfillStatus != models.SyncBackfillRunning || !w.tracker.state.LastSyncedAt.Equal(success) {
					t.Fatalf("unfinished recovery lost progress/window: %+v / %+v / %+v", cur, recovery, w.tracker.state)
				}
				w, events = imapReload(t, w, conn, &initialRecoveryBudget{fixedBudget: fixedBudget{allow: 10}, limit: 4})
				if err := w.Sync(t.Context()); err != nil {
					t.Fatal(err)
				}
				allEvents = append(allEvents, (*events)...)
			}
			if !w.tracker.state.BackfillSince.Equal(since) || !w.tracker.state.BackfillStartedAt.Equal(started) {
				t.Fatal("generation changed original import window/timer")
			}
			updates := mailboxEvents(allEvents, models.JobEventTypeEmailUpdate)
			if len(updates) != 1 {
				t.Fatalf("known metadata relayed %d times, want once across reload", len(updates))
			}
			update := updates[0].body.(*models.JobEventEmailUpdate)
			if update.ID.String() != knownID || update.UID != 2 || update.Mailbox != 9 || update.ModSeq != 5 || !models.SeenFromFlags(update.Flags) {
				t.Fatalf("known handles/read state stale: %+v", update)
			}
			wantCount := budget.limit
			if w.tracker.state.BackfillStatus != models.SyncBackfillComplete || w.tracker.state.BackfillSynced != wantCount || len(maps.data) != 1+wantCount-2 || len(mailboxEvents(allEvents, models.JobEventTypeNewEmail)) != wantCount-2 || len(conn.bodies) != wantCount-2 {
				t.Fatal("recovery duplicated known mail, bypassed cap or lost cumulative admission count")
			}
			if maps.data["<2@fake.test>"].ID != knownID {
				t.Fatal("known identity changed")
			}
			for _, cutoff := range conn.since {
				if !cutoff.Equal(since) {
					t.Fatal("provider search escaped original import window")
				}
			}
			cur := w.tracker.folder("INBOX")
			if !cur.Done || cur.UID != 1 || imapRecoveryCursor(cur) == nil || !imapRecoveryCursor(cur).Initial {
				t.Fatalf("running-import recovery continuation lost: %+v", cur)
			}
			if strings.HasPrefix(cur.Next, imapRecoveryPrefix) {
				t.Fatal("an older worker could apply a separate cap to the initial import")
			}
			searches := conn.searches
			again, later := imapReload(t, w, conn, &initialRecoveryBudget{fixedBudget: fixedBudget{allow: 10}, limit: 4})
			if err := again.Sync(t.Context()); err != nil {
				t.Fatal(err)
			}
			if conn.searches != searches || len(mailboxEvents(*later, models.JobEventTypeEmailUpdate)) != 0 || len(mailboxEvents(*later, models.JobEventTypeNewEmail)) != 0 {
				t.Fatal("completed initial-generation replay repeated")
			}
		})
	}
}

func TestImapRunningImportGenerationMarkerPrecedesBaseline(t *testing.T) {
	w, events, conn, _ := newRunningRecoveryMail(t, &initialRecoveryBudget{fixedBudget: fixedBudget{allow: 10}, limit: 4})
	w.tracker.emit = func(models.SyncState) error { return errors.New("generation state relay failed") }
	if err := w.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	if conn.fetches != 0 || len(mailboxEvents(*events, models.JobEventTypeMailboxUpdate)) != 0 || w.SmtpImapData.Mailboxes[0].UIDValidity != 8 || w.tracker.state.BackfillSynced != 2 {
		t.Fatal("baseline or admissions outran initial-generation marker")
	}
	w.tracker.emit = func(models.SyncState) error { return nil }
	if err := w.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	if w.tracker.state.BackfillStatus != models.SyncBackfillComplete || w.tracker.state.BackfillSynced != 4 || len(mailboxEvents(*events, models.JobEventTypeEmailUpdate)) != 1 {
		t.Fatal("state relay retry did not recover initial generation")
	}
}

func (c *knownRecoveryConn) FetchEnvelopes(_ context.Context, _ []goimap.UID) ([]*imap.Fetched, *errx.MailError) {
	c.fetches++
	message := c.message
	message.Flags = append([]string{}, c.message.Flags...)
	return []*imap.Fetched{{Email: &message}}, nil
}

func TestImapRecoveryReconcilesKnownMetadataBeforeCheckpoint(t *testing.T) {
	for _, newFolder := range []bool{false, true} {
		for _, relayFailure := range []bool{false, true} {
			t.Run(fmt.Sprintf("new folder=%t relay failure=%t", newFolder, relayFailure), func(t *testing.T) {
				box := models.Mailbox{Name: "Archive", UIDValidity: 9, UIDNext: 43, HighestModSeq: 100}
				conn := &knownRecoveryConn{fakeImapConn: &fakeImapConn{folders: []models.Mailbox{box}, all: []goimap.UID{42}, bodyErr: errx.ErrMailServerUnreachable}, message: models.EmailMessageData{UID: 42, MessageID: "<same@fake.test>", ModSeq: 100, Flags: []string{models.FlagSeen}}}
				w, events := newIMAPTestMail(conn, &fixedBudget{allow: 0}, &models.Mailbox{Name: box.Name, UIDValidity: 8, UIDNext: 200, HighestModSeq: 100})
				if newFolder {
					w.SmtpImapData.Mailboxes = nil
				}
				id := uuid.NewString()
				maps := &recoveryMessageMap{data: map[string]repository.EmailMessageData{conn.message.MessageID: {ID: id, MessageID: conn.message.MessageID}}}
				w.EmailMessageMapRepository = maps
				w.tracker.state.BackfillSynced = 23
				w.tracker.setFolder(box.Name, models.SyncFolderCursor{UID: 200, Done: true})
				oldSuccess := time.Now().Add(-time.Hour)
				w.tracker.state.LastSyncedAt = &oldSuccess
				if relayFailure {
					emit := w.onEvent
					w.onEvent = func(kind models.JobEventType, body any) error {
						if kind == models.JobEventTypeEmailUpdate {
							return errors.New("known metadata relay unavailable")
						}
						return emit(kind, body)
					}
				}
				if err := w.Sync(t.Context()); err != nil {
					t.Fatal(err)
				}
				if relayFailure {
					cur := w.tracker.folder(box.Name)
					if cur.Done || cur.UID != 0 || !w.tracker.state.LastSyncedAt.Equal(oldSuccess) || len(mailboxEvents(*events, models.JobEventTypeEmailUpdate)) != 0 {
						t.Fatal("failed metadata relay completed recovery or refreshed success")
					}
					w, events = imapReload(t, w, conn, &fixedBudget{allow: 0})
					if err := w.Sync(t.Context()); err != nil {
						t.Fatal(err)
					}
				}
				updates := mailboxEvents(*events, models.JobEventTypeEmailUpdate)
				if len(updates) != 1 {
					t.Fatalf("recovery known updates=%d", len(updates))
				}
				update := updates[0].body.(*models.JobEventEmailUpdate)
				if update.ID.String() != id || update.UID != 42 || update.Mailbox != 9 || update.ModSeq != 100 || update.FolderPath != box.Name || !models.SeenFromFlags(update.Flags) {
					t.Fatalf("stale recovery metadata: %+v", update)
				}
				cur := w.tracker.folder(box.Name)
				if !cur.Done || cur.UID != 42 || imapRecoveryCursor(cur).Synced != 0 || w.tracker.state.BackfillSynced != 23 || w.tracker.state.BackfillStatus != models.SyncBackfillComplete {
					t.Fatal("known reconciliation reset or consumed global history")
				}
				if len(mailboxEvents(*events, models.JobEventTypeNewEmail)) != 0 || len(maps.data) != 1 || maps.data[conn.message.MessageID].ID != id {
					t.Fatal("known recovery reimported a mapped message")
				}
				searches := conn.searches
				again, later := imapReload(t, w, conn, &fixedBudget{allow: 0})
				if err := again.Sync(t.Context()); err != nil {
					t.Fatal(err)
				}
				if conn.searches != searches || len(mailboxEvents(*later, models.JobEventTypeEmailUpdate)) != 0 || len(mailboxEvents(*later, models.JobEventTypeNewEmail)) != 0 {
					t.Fatal("unchanged HIGHESTMODSEQ repeated completed recovery")
				}
			})
		}
	}
}

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
			if cur.Done || cur.UID != 2 || recovery == nil || recovery.Generation != 9 || recovery.Synced != 1 {
				t.Fatalf("deferred recovery = %+v / %+v", cur, recovery)
			}
			if len(mailboxEvents(*events, models.JobEventTypeNewEmail)) != 1 {
				t.Fatal("newest recovery arrival missing")
			}
			if len(mailboxEvents(*events, models.JobEventTypeEmailUpdate)) != 1 {
				t.Fatal("known metadata was not reconciled before the saved floor")
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
			if len(mailboxEvents(*resumed, models.JobEventTypeEmailUpdate)) != 0 {
				t.Fatal("checkpointed known metadata was relayed again after reload")
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
