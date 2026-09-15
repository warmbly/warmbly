package wmail

import (
	"context"
	"fmt"
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

// fakeImapConn is the sync pass's view of a server. Only the methods a pass
// calls are implemented; the embedded nil interface makes anything else panic
// rather than silently pass.
type fakeImapConn struct {
	ImapConn
	folders   []models.Mailbox
	changed   []goimap.UID
	fetches   int
	released  int
	overflow  int
	conflicts int
	// noCondStore drives the UIDNEXT path instead of the mod-sequence one.
	noCondStore bool
	flags       map[uint32]imap.FlagState
	flagScans   int
	// all is the folder's complete UID set, which the drafts reconciliation
	// diffs against the UIDs the platform holds.
	all []goimap.UID
}

func (c *fakeImapConn) Folders() ([]models.Mailbox, *errx.MailError) { return c.folders, nil }

func (c *fakeImapConn) FolderOverflow() int  { return c.overflow }
func (c *fakeImapConn) FolderConflicts() int { return c.conflicts }

// condStore defaults to true: most of these tests exercise the mod-sequence
// path, and the UIDNEXT path has its own tests.
func (c *fakeImapConn) HasCondStore() bool { return !c.noCondStore }

func (c *fakeImapConn) SearchNewSince(uidNext uint32) ([]goimap.UID, *errx.MailError) {
	var out []goimap.UID
	for _, uid := range c.changed {
		if uint32(uid) >= uidNext {
			out = append(out, uid)
		}
	}
	return out, nil
}

func (c *fakeImapConn) FetchFlags(context.Context, uint32) (map[uint32]imap.FlagState, *errx.MailError) {
	c.flagScans++
	// A fresh map per call, like the real client: the scan keeps the result
	// as its baseline, so handing back the same map would compare it to
	// itself and never see a change.
	out := make(map[uint32]imap.FlagState, len(c.flags))
	for uid, st := range c.flags {
		out[uid] = st
	}
	return out, nil
}

func (c *fakeImapConn) ReleaseMailbox() { c.released++ }

func (c *fakeImapConn) SelectForSync(string) (uint32, *errx.MailError) {
	return uint32(len(c.changed)), nil
}

func (c *fakeImapConn) SearchChangedSince(uint64) ([]goimap.UID, *errx.MailError) {
	return append([]goimap.UID(nil), c.changed...), nil
}

func (c *fakeImapConn) SearchAll() ([]goimap.UID, *errx.MailError) {
	return append([]goimap.UID(nil), c.all...), nil
}

func (c *fakeImapConn) FetchEnvelopes(_ context.Context, uids []goimap.UID) ([]*imap.Fetched, *errx.MailError) {
	c.fetches++
	out := make([]*imap.Fetched, 0, len(uids))
	for _, uid := range uids {
		out = append(out, &imap.Fetched{Email: &models.EmailMessageData{
			UID:       uint32(uid),
			MessageID: fmt.Sprintf("<%d@fake.test>", uid),
			Subject:   "hello",
		}})
	}
	return out, nil
}

func (c *fakeImapConn) FetchBody(*imap.Fetched) {}

// fixedBudget is a syncBudget that admits a fixed number of messages and then
// denies on the daily window, which is how a real governor answers once the
// mailbox's day is spent. Redis is unreachable from a unit test, so the fake
// stands in for the counters, not for the decision the pass makes from them.
type fixedBudget struct {
	allow    int
	admitted int
	// observed totals what ObserveLive was told, so a test can check that a
	// held backlog is not re-counted toward the flood threshold every pass.
	observed int
}

func (b *fixedBudget) Policy() models.SyncPolicy               { return normalizePolicy(models.SyncPolicy{}) }
func (b *fixedBudget) SetPolicy(models.SyncPolicy)             {}
func (b *fixedBudget) RecordThrottledDay(context.Context) bool { return false }

func (b *fixedBudget) Admit(context.Context, SyncLane) Admission {
	if b.admitted >= b.allow {
		return Admission{Reason: models.SyncThrottleDaily, Until: time.Now().Add(time.Hour)}
	}
	b.admitted++
	return Admission{OK: true}
}

func (b *fixedBudget) ObserveLive(_ context.Context, n int) bool {
	b.observed += n
	return false
}

// newIMAPTestMail builds the smallest WMail that can run an IMAP pass.
func newIMAPTestMail(conn ImapConn, budget syncBudget, saved *models.Mailbox) (*WMail, *[]captured) {
	var events []captured
	w := &WMail{
		UserID:                    uuid.New(),
		ID:                        uuid.New(),
		Storage:                   fakeStore{},
		EmailMessageMapRepository: fakeMessageMap{},
		gov:                       budget,
		SmtpImapData: &SmtpImapData{
			ImapClient: conn,
			Mailboxes:  []*models.Mailbox{saved},
		},
	}
	w.onEvent = func(kind models.JobEventType, body any) error {
		events = append(events, captured{eventType: kind, body: body})
		return nil
	}
	// The backfill is a separate lane with its own early return; keep it out
	// of the way so these tests only exercise the live batch loop.
	w.tracker = newSyncTracker(
		&models.SyncState{BackfillStatus: models.SyncBackfillComplete},
		func(models.SyncState) error { return nil },
	)
	return w, &events
}

func uidRange(n int) []goimap.UID {
	out := make([]goimap.UID, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, goimap.UID(i))
	}
	return out
}

func relayedModSeq(t *testing.T, events []captured) uint64 {
	t.Helper()
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].eventType != models.JobEventTypeMailboxUpdate {
			continue
		}
		return events[i].body.(*models.JobEventMailboxUpdate).Data.HighestModSeq
	}
	t.Fatal("no MAILBOX_UPDATE was relayed")
	return 0
}

func hasEvent(events []captured, kind models.JobEventType) bool {
	for _, e := range events {
		if e.eventType == kind {
			return true
		}
	}
	return false
}

// A mailbox unfrozen on a long backlog must not walk the whole thing: once
// the live lane is denied, the folder stops fetching and holds its
// mod-sequence, so the backlog is not re-offered to the flood detector batch
// after batch until the mailbox deactivates itself.
func TestImapSyncStopsFetchingOnceTheLiveLaneIsDenied(t *testing.T) {
	conn := &fakeImapConn{
		folders: []models.Mailbox{{Name: "INBOX", UIDValidity: 7, HighestModSeq: 90_000}},
		changed: uidRange(3 * config.ImapFetchBatchSize),
	}
	budget := &fixedBudget{allow: 0}
	w, events := newIMAPTestMail(conn, budget, &models.Mailbox{Name: "INBOX", UIDValidity: 7, HighestModSeq: 100})

	if err := w.Sync(t.Context()); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if conn.fetches != 1 {
		t.Errorf("fetched %d batches after the lane was denied, want 1", conn.fetches)
	}
	if got := w.SmtpImapData.Mailboxes[0].HighestModSeq; got != 100 {
		t.Errorf("mod-sequence advanced to %d; a deferred backlog must hold it at 100", got)
	}
	if got := relayedModSeq(t, *events); got != 100 {
		t.Errorf("relayed mod-sequence = %d, want the held 100", got)
	}
	if hasEvent(*events, models.JobEventTypeEmailRateLimited) {
		t.Error("mailbox was deactivated by its own deferred backlog")
	}

	// The next pass is re-offered the same mail. It must not read as a fresh
	// flood: only messages the pass has never classified are observed.
	seenAfterFirst := budget.observed
	if seenAfterFirst != config.ImapFetchBatchSize {
		t.Fatalf("observed %d new messages, want one batch", seenAfterFirst)
	}
	if err := w.Sync(t.Context()); err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	if budget.observed != seenAfterFirst {
		t.Errorf("observed %d after the second pass, want %d: the held backlog was counted twice",
			budget.observed, seenAfterFirst)
	}
	if conn.fetches != 2 {
		t.Errorf("fetched %d batches over two passes, want 2", conn.fetches)
	}
}

// The denial stops the loop at the batch it happened in, not before it:
// everything admitted up to that point is stored, and only the rest waits.
func TestImapSyncKeepsWhatFitBeforeTheDenial(t *testing.T) {
	conn := &fakeImapConn{
		folders: []models.Mailbox{{Name: "INBOX", UIDValidity: 7, HighestModSeq: 90_000}},
		changed: uidRange(3 * config.ImapFetchBatchSize),
	}
	budget := &fixedBudget{allow: config.ImapFetchBatchSize + 50}
	w, events := newIMAPTestMail(conn, budget, &models.Mailbox{Name: "INBOX", UIDValidity: 7, HighestModSeq: 100})

	if err := w.Sync(t.Context()); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if conn.fetches != 2 {
		t.Errorf("fetched %d batches, want 2 (the full first and the one that ran out)", conn.fetches)
	}
	if budget.admitted != config.ImapFetchBatchSize+50 {
		t.Errorf("admitted %d, want the whole budget spent", budget.admitted)
	}
	if got := w.SmtpImapData.Mailboxes[0].HighestModSeq; got != 100 {
		t.Errorf("mod-sequence advanced to %d with mail still on the server", got)
	}
	if hasEvent(*events, models.JobEventTypeEmailRateLimited) {
		t.Error("mailbox was deactivated by a plain budget denial")
	}
}

// The control case: with budget to spare the pass still walks every batch and
// the folder's mod-sequence moves to what the server reported.
func TestImapSyncWalksEveryBatchWithinBudget(t *testing.T) {
	conn := &fakeImapConn{
		folders: []models.Mailbox{{Name: "INBOX", UIDValidity: 7, HighestModSeq: 90_000}},
		changed: uidRange(3 * config.ImapFetchBatchSize),
	}
	budget := &fixedBudget{allow: 10 * config.ImapFetchBatchSize}
	w, _ := newIMAPTestMail(conn, budget, &models.Mailbox{Name: "INBOX", UIDValidity: 7, HighestModSeq: 100})

	if err := w.Sync(t.Context()); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if conn.fetches != 3 {
		t.Errorf("fetched %d batches, want 3", conn.fetches)
	}
	if got := w.SmtpImapData.Mailboxes[0].HighestModSeq; got != 90_000 {
		t.Errorf("mod-sequence = %d, want 90000 once every change was stored", got)
	}
	if conn.released != 1 {
		t.Errorf("released the mailbox %d times, want once before LIST-STATUS", conn.released)
	}
}

// backfillImapConn serves an initial import: per-folder UID lists, with a
// folder's search made to fail on demand so a pass can be driven through a
// server having a moment.
type backfillImapConn struct {
	ImapConn
	folders  []models.Mailbox
	uids     map[string][]goimap.UID
	fail     map[string]int // folder -> failures left (-1 for always)
	selected string
}

func (c *backfillImapConn) Folders() ([]models.Mailbox, *errx.MailError) { return c.folders, nil }
func (c *backfillImapConn) FolderOverflow() int                          { return 0 }
func (c *backfillImapConn) FolderConflicts() int                         { return 0 }
func (c *backfillImapConn) HasCondStore() bool                           { return true }
func (c *backfillImapConn) ReleaseMailbox()                              {}

func (c *backfillImapConn) SelectForSync(name string) (uint32, *errx.MailError) {
	c.selected = name
	return uint32(len(c.uids[name])), nil
}

func (c *backfillImapConn) SearchChangedSince(uint64) ([]goimap.UID, *errx.MailError) {
	return nil, nil
}

func (c *backfillImapConn) SearchSince(time.Time) ([]goimap.UID, *errx.MailError) {
	if n := c.fail[c.selected]; n != 0 {
		if n > 0 {
			c.fail[c.selected] = n - 1
		}
		return nil, errx.ErrMailServerUnreachable
	}
	return append([]goimap.UID(nil), c.uids[c.selected]...), nil
}

func (c *backfillImapConn) FetchEnvelopes(_ context.Context, uids []goimap.UID) ([]*imap.Fetched, *errx.MailError) {
	out := make([]*imap.Fetched, 0, len(uids))
	for _, uid := range uids {
		out = append(out, &imap.Fetched{Email: &models.EmailMessageData{
			UID:       uint32(uid),
			MessageID: fmt.Sprintf("<%s-%d@fake.test>", c.selected, uid),
			Subject:   "history",
		}})
	}
	return out, nil
}

func (c *backfillImapConn) FetchBody(*imap.Fetched) {}

// The Graph defect's shape, checked on the IMAP import: a folder whose search
// fails holds its cursor and is walked again on the next pass. Nothing about a
// server error may mark a folder, or the whole import, complete.
func TestImapBackfillRetriesAFolderAfterATransientFailure(t *testing.T) {
	conn := &backfillImapConn{
		folders: []models.Mailbox{
			{Name: "INBOX", UIDValidity: 7, HighestModSeq: 100},
			{Name: "Archive", UIDValidity: 8, HighestModSeq: 100},
		},
		uids: map[string][]goimap.UID{"INBOX": {5, 6}, "Archive": {9}},
		fail: map[string]int{"Archive": 1},
	}
	var events []captured
	w := &WMail{
		UserID:                    uuid.New(),
		ID:                        uuid.New(),
		Storage:                   fakeStore{},
		EmailMessageMapRepository: fakeMessageMap{},
		gov:                       newGovernor(uuid.New(), nil, nil, models.SyncPolicy{}),
		SmtpImapData: &SmtpImapData{
			ImapClient: conn,
			Mailboxes: []*models.Mailbox{
				{Name: "INBOX", UIDValidity: 7, HighestModSeq: 100},
				{Name: "Archive", UIDValidity: 8, HighestModSeq: 100},
			},
		},
	}
	w.onEvent = func(kind models.JobEventType, body any) error {
		events = append(events, captured{eventType: kind, body: body})
		return nil
	}
	w.tracker = newSyncTracker(nil, func(models.SyncState) error { return nil })

	if err := w.Sync(t.Context()); err == nil {
		t.Fatal("a failed folder search was swallowed; the pass must end so the folder is retried")
	}
	if w.tracker.folder("Archive").Done {
		t.Fatal("the archive backfill was marked complete by a transient failure")
	}
	if st := w.tracker.state.BackfillStatus; st == models.SyncBackfillComplete {
		t.Fatalf("backfill status = %s after a failed pass", st)
	}

	if err := w.Sync(t.Context()); err != nil {
		t.Fatalf("second pass: %v", err.Message)
	}
	if !w.tracker.folder("Archive").Done {
		t.Error("archive is still not done after a successful search")
	}
	if err := w.Sync(t.Context()); err != nil {
		t.Fatalf("third pass: %v", err.Message)
	}
	if st := w.tracker.state.BackfillStatus; st != models.SyncBackfillComplete {
		t.Fatalf("backfill status = %s, want %s", st, models.SyncBackfillComplete)
	}
	if !hasEvent(events, models.JobEventTypeNewEmail) {
		t.Error("no history was imported at all")
	}
}

// On a server without CONDSTORE the pass follows UIDNEXT instead of the
// mod-sequence. Refusing those servers is what left Outlook.com, Microsoft
// 365 over IMAP and Yahoo mailboxes unable to sync at all.
func TestImapSyncFollowsUIDNextWithoutCondStore(t *testing.T) {
	conn := &fakeImapConn{
		noCondStore: true,
		folders:     []models.Mailbox{{Name: "INBOX", UIDValidity: 7, UIDNext: 104}},
		changed:     []goimap.UID{101, 102, 103},
	}
	w, events := newIMAPTestMail(conn, &fixedBudget{allow: 10},
		&models.Mailbox{Name: "INBOX", UIDValidity: 7, UIDNext: 101})

	if err := w.Sync(t.Context()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if conn.fetches != 1 {
		t.Errorf("fetched %d batches, want 1: the new mail above the cursor", conn.fetches)
	}
	if got := w.SmtpImapData.Mailboxes[0].UIDNext; got != 104 {
		t.Errorf("UIDNEXT cursor = %d, want 104 once everything was stored", got)
	}
	if !hasEvent(*events, models.JobEventTypeNewEmail) {
		t.Error("no mail was stored on a server without CONDSTORE")
	}
}

// The cursor is held when the budget defers part of the batch, exactly as the
// mod-sequence is: the held mail is re-offered next pass rather than skipped.
func TestImapSyncHoldsUIDNextWhenDeferred(t *testing.T) {
	conn := &fakeImapConn{
		noCondStore: true,
		folders:     []models.Mailbox{{Name: "INBOX", UIDValidity: 7, UIDNext: 500}},
		changed:     uidRange(3 * config.ImapFetchBatchSize),
	}
	w, _ := newIMAPTestMail(conn, &fixedBudget{allow: 0},
		&models.Mailbox{Name: "INBOX", UIDValidity: 7, UIDNext: 1})

	if err := w.Sync(t.Context()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if got := w.SmtpImapData.Mailboxes[0].UIDNext; got != 1 {
		t.Errorf("UIDNEXT advanced to %d with mail still waiting on the server", got)
	}
}

// A quiet folder must cost nothing: with the cursor level there is no search
// and no fetch, which is what keeps a per-minute pass cheap on a big account.
func TestImapSyncSkipsAQuietFolderWithoutCondStore(t *testing.T) {
	conn := &fakeImapConn{
		noCondStore: true,
		folders:     []models.Mailbox{{Name: "INBOX", UIDValidity: 7, UIDNext: 101}},
	}
	w, _ := newIMAPTestMail(conn, &fixedBudget{allow: 10},
		&models.Mailbox{Name: "INBOX", UIDValidity: 7, UIDNext: 101})

	if err := w.Sync(t.Context()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if conn.fetches != 0 {
		t.Errorf("fetched %d batches from a folder with nothing new", conn.fetches)
	}
}

// Read state is mirrored by the periodic scan on a server that cannot say what
// changed. The first scan only baselines: relaying it would send an update for
// every message in the window for nothing.
func TestImapFlagScanBaselinesThenRelaysChanges(t *testing.T) {
	conn := &fakeImapConn{
		noCondStore: true,
		folders:     []models.Mailbox{{Name: "INBOX", UIDValidity: 7, UIDNext: 101}},
		flags: map[uint32]imap.FlagState{
			1: {MessageID: "<known@test>", Flags: []string{}},
		},
	}
	w, events := newIMAPTestMail(conn, &fixedBudget{allow: 10},
		&models.Mailbox{Name: "INBOX", UIDValidity: 7, UIDNext: 101})
	// The platform already has this message; only a known message can have
	// its flags mirrored.
	w.EmailMessageMapRepository = knownMessageMap{id: uuid.New().String()}

	if err := w.Sync(t.Context()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if conn.flagScans != 1 {
		t.Fatalf("ran %d flag scans, want 1", conn.flagScans)
	}
	if hasEvent(*events, models.JobEventTypeEmailUpdate) {
		t.Fatal("the first scan relayed updates; it has nothing to compare against yet")
	}

	// The message is marked read in the customer's own mail client.
	conn.flags[1] = imap.FlagState{MessageID: "<known@test>", Flags: []string{"\\Seen"}}
	w.flagScan["INBOX"].at = time.Now().Add(-2 * config.ImapFlagScanInterval)
	if err := w.Sync(t.Context()); err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	if !hasEvent(*events, models.JobEventTypeEmailUpdate) {
		t.Error("a message marked read elsewhere was never mirrored")
	}
}

// The scan is periodic, not per pass: it is a FETCH per folder and read state
// is not worth one every minute on every folder.
func TestImapFlagScanIsPeriodic(t *testing.T) {
	conn := &fakeImapConn{
		noCondStore: true,
		folders:     []models.Mailbox{{Name: "INBOX", UIDValidity: 7, UIDNext: 101}},
		flags:       map[uint32]imap.FlagState{},
	}
	w, _ := newIMAPTestMail(conn, &fixedBudget{allow: 10},
		&models.Mailbox{Name: "INBOX", UIDValidity: 7, UIDNext: 101})

	for i := 0; i < 3; i++ {
		if err := w.Sync(t.Context()); err != nil {
			t.Fatalf("Sync: %v", err)
		}
	}
	if conn.flagScans != 1 {
		t.Errorf("ran %d flag scans over three passes, want 1", conn.flagScans)
	}
}

// A CONDSTORE server must not pay for the scan: its mod-sequence already
// reports flag changes.
func TestImapFlagScanIsSkippedWithCondStore(t *testing.T) {
	conn := &fakeImapConn{
		folders: []models.Mailbox{{Name: "INBOX", UIDValidity: 7, HighestModSeq: 100}},
		flags:   map[uint32]imap.FlagState{},
	}
	w, _ := newIMAPTestMail(conn, &fixedBudget{allow: 10},
		&models.Mailbox{Name: "INBOX", UIDValidity: 7, HighestModSeq: 100})

	if err := w.Sync(t.Context()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if conn.flagScans != 0 {
		t.Errorf("ran %d flag scans on a CONDSTORE server", conn.flagScans)
	}
}

// What the listing could not follow is state, not an event: an error row is
// never withdrawn, so raising one left a red "needs attention" after the user
// had fixed the problem. The counts ride the sync state and clear themselves.
func TestFoldersSkippedIsRelayedAsStateAndClears(t *testing.T) {
	conn := &fakeImapConn{
		folders:   []models.Mailbox{{Name: "INBOX", UIDValidity: 7, HighestModSeq: 100}},
		overflow:  3,
		conflicts: 2,
	}
	w, events := newIMAPTestMail(conn, &fixedBudget{allow: 10},
		&models.Mailbox{Name: "INBOX", UIDValidity: 7, HighestModSeq: 100})

	if err := w.Sync(t.Context()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if got := w.tracker.state.FoldersSkippedCap; got != 3 {
		t.Errorf("FoldersSkippedCap = %d, want 3", got)
	}
	if got := w.tracker.state.FoldersSkippedConflict; got != 2 {
		t.Errorf("FoldersSkippedConflict = %d, want 2", got)
	}
	// Never as an error: those are never withdrawn.
	for _, e := range *events {
		if e.eventType == models.JobEventTypeEmailServerError {
			t.Error("a folder problem was raised as an error row, which nothing ever resolves")
		}
	}

	// The user moves folders until the mailbox fits again.
	conn.overflow, conn.conflicts = 0, 0
	if err := w.Sync(t.Context()); err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	if w.tracker.state.FoldersSkippedCap != 0 || w.tracker.state.FoldersSkippedConflict != 0 {
		t.Errorf("counts stayed at %d/%d after the condition cleared",
			w.tracker.state.FoldersSkippedCap, w.tracker.state.FoldersSkippedConflict)
	}
}

// knownMessageMap answers every lookup with the same stored message, which is
// what lets a flag-scan test exercise the relay rather than the "not ours"
// early return.
type knownMessageMap struct{ id string }

func (knownMessageMap) Add(context.Context, repository.EmailMessageData) error { return nil }
func (m knownMessageMap) Get(_ context.Context, _, _ uuid.UUID, messageID string) (*repository.EmailMessageData, error) {
	return &repository.EmailMessageData{ID: m.id, MessageID: messageID}, nil
}
func (knownMessageMap) Del(context.Context, uuid.UUID, uuid.UUID, string, uuid.UUID) error {
	return nil
}

// fakeSyncContext stands in for the worker's HTTP sync-context proxy and
// answers with the platform's stored rows for one folder.
type fakeSyncContext struct {
	stored map[string][]repository.StoredFolderMessage
	calls  int
	// uidValidity is what the last lookup asked for, so a test can check the
	// reconciliation is scoped to the folder's current generation.
	uidValidity uint32
}

func (fakeSyncContext) IsOwnConversation(context.Context, uuid.UUID, uuid.UUID, []string, string) (bool, error) {
	return false, nil
}

func (f *fakeSyncContext) ListFolderMessages(_ context.Context, _, _ uuid.UUID, folderPath string, uidValidity uint32) ([]repository.StoredFolderMessage, error) {
	f.calls++
	f.uidValidity = uidValidity
	return f.stored[folderPath], nil
}

func removeIDs(events []captured) []uuid.UUID {
	var out []uuid.UUID
	for _, e := range events {
		if e.eventType != models.JobEventTypeRemoveEmail {
			continue
		}
		out = append(out, e.body.(*models.JobEventRemoveEmail).ID)
	}
	return out
}

func draftsFolder(modseq uint64) models.Mailbox {
	return models.Mailbox{Name: "[Gmail]/Drafts", Attrs: []string{"\\Drafts"}, UIDValidity: 9, HighestModSeq: modseq}
}

func draftsBox(modseq uint64) *models.Mailbox {
	b := draftsFolder(modseq)
	return &b
}

// Gmail's autosave gives every draft a new UID and a new Message-ID and
// expunges the previous copy. The pass must remove the row for the copy the
// server no longer reports, and leave the current one alone.
func TestImapSyncRemovesExpungedDraftRows(t *testing.T) {
	conn := &fakeImapConn{
		folders: []models.Mailbox{draftsFolder(500)},
		changed: []goimap.UID{5},
		all:     []goimap.UID{5},
	}
	w, events := newIMAPTestMail(conn, &fixedBudget{allow: 10}, draftsBox(400))
	w.EmailMessageMapRepository = knownMessageMap{id: uuid.New().String()}
	expunged, current := uuid.New(), uuid.New()
	ctx := &fakeSyncContext{stored: map[string][]repository.StoredFolderMessage{
		"[Gmail]/Drafts": {
			{UID: 4, MessageID: "<4@fake.test>", ID: expunged},
			{UID: 5, MessageID: "<5@fake.test>", ID: current},
		},
	}}
	w.SyncContext = ctx

	if err := w.Sync(t.Context()); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	removed := removeIDs(*events)
	if len(removed) != 1 || removed[0] != expunged {
		t.Fatalf("removed %v, want exactly the expunged draft %s", removed, expunged)
	}
	// The lookup must be scoped to the folder's current UIDVALIDITY, so rows
	// whose UIDs a generation change voided are never read as expunged.
	if ctx.uidValidity != 9 {
		t.Errorf("looked up folder rows for UIDVALIDITY %d, want 9", ctx.uidValidity)
	}
}

// Rows are also reconciled on a pass where the drafts folder did not change,
// which is what cleans up the rows an earlier worker or an earlier build left
// behind.
func TestImapSyncRemovesAccumulatedDraftRowsOnAQuietPass(t *testing.T) {
	conn := &fakeImapConn{
		folders: []models.Mailbox{draftsFolder(500)},
		all:     []goimap.UID{5},
	}
	w, events := newIMAPTestMail(conn, &fixedBudget{allow: 10}, draftsBox(500))
	expunged := uuid.New()
	w.SyncContext = &fakeSyncContext{stored: map[string][]repository.StoredFolderMessage{
		"[Gmail]/Drafts": {
			{UID: 4, MessageID: "<4@fake.test>", ID: expunged},
			{UID: 5, MessageID: "<5@fake.test>", ID: uuid.New()},
		},
	}}

	if err := w.Sync(t.Context()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if conn.fetches != 0 {
		t.Errorf("fetched %d batches from an unchanged folder, want 0", conn.fetches)
	}

	removed := removeIDs(*events)
	if len(removed) != 1 || removed[0] != expunged {
		t.Fatalf("removed %v, want the accumulated draft row %s", removed, expunged)
	}
}

// A draft re-appended under a new UID keeps its Message-ID, so the pass
// fetched it: the row was re-filed, not expunged, and must survive.
func TestImapSyncKeepsADraftReappendedUnderANewUID(t *testing.T) {
	conn := &fakeImapConn{
		folders: []models.Mailbox{draftsFolder(500)},
		changed: []goimap.UID{5},
		all:     []goimap.UID{5},
	}
	w, events := newIMAPTestMail(conn, &fixedBudget{allow: 10}, draftsBox(400))
	w.EmailMessageMapRepository = knownMessageMap{id: uuid.New().String()}
	w.SyncContext = &fakeSyncContext{stored: map[string][]repository.StoredFolderMessage{
		"[Gmail]/Drafts": {
			// Filed under the old UID 4, but the same Message-ID arrived
			// again as UID 5 in this pass.
			{UID: 4, MessageID: "<5@fake.test>", ID: uuid.New()},
		},
	}}

	if err := w.Sync(t.Context()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if removed := removeIDs(*events); len(removed) != 0 {
		t.Fatalf("removed %v for a draft that was re-appended, not expunged", removed)
	}
}

// The same absence in a folder that is not drafts is not a removal: a message
// can leave INBOX for a place this sync does not follow (Gmail's All Mail is
// dropped as a virtual label view), and deleting the row would lose mail the
// user still expects to see.
func TestImapSyncDoesNotReconcileExpungesOutsideDrafts(t *testing.T) {
	conn := &fakeImapConn{
		folders: []models.Mailbox{{Name: "INBOX", UIDValidity: 7, HighestModSeq: 500}},
		changed: []goimap.UID{5},
		all:     []goimap.UID{5},
	}
	w, events := newIMAPTestMail(conn, &fixedBudget{allow: 10},
		&models.Mailbox{Name: "INBOX", UIDValidity: 7, HighestModSeq: 400})
	w.EmailMessageMapRepository = knownMessageMap{id: uuid.New().String()}
	ctx := &fakeSyncContext{stored: map[string][]repository.StoredFolderMessage{
		"INBOX": {{UID: 4, MessageID: "<4@fake.test>", ID: uuid.New()}},
	}}
	w.SyncContext = ctx

	if err := w.Sync(t.Context()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if removed := removeIDs(*events); len(removed) != 0 {
		t.Fatalf("removed %v from INBOX, where an absent UID is not proof the row is gone", removed)
	}
	if ctx.calls != 0 {
		t.Errorf("asked the backend about INBOX %d times; only drafts are reconciled", ctx.calls)
	}
}
