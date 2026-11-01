package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// What a view shows after a conversation is filed, and who "we spoke last"
// resolves to. Both are decided entirely in SQL, which is why they run against
// a real schema:
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LiveUniboxScope -v
//
// Archive used to leave a conversation in every view but Inbox, so pressing it
// from Awaiting reply removed the row and the next read put it straight back.
// Awaiting reply compared a mailbox address against the whole From header,
// which is "Name <addr>" from every provider, so it matched almost nothing.

// message writes one message into the fixture's mailbox with the from header
// and folder the case needs. threadID groups them into a conversation.
func (f *uniboxFolderFixture) scopedMessage(t *testing.T, repo UniboxRepository, threadID, from, folder string, at time.Time) uuid.UUID {
	t.Helper()
	id := uuid.New()
	err := repo.CreateEntry(context.Background(), f.user, &models.EmailMessageStoreData{
		ID: id, EmailID: f.mailbox, Folder: folder,
		ThreadID: threadID, MessageID: "<" + id.String() + "@test.local>",
		FromAddr: []string{from},
		ToAddr:   []string{"them@example.com"},
		Subject:  "Scope", Snippet: "Scope",
		InternalDate: at, SentDate: at, CreatedAt: at, UpdatedAt: at,
		Seen: false,
	})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	return id
}

func (f *uniboxFolderFixture) mailboxAddress(t *testing.T, repo UniboxRepository) string {
	t.Helper()
	ov, err := repo.Overview(context.Background(), f.org)
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}
	if len(ov.Mailboxes) != 1 {
		t.Fatalf("fixture has %d mailboxes, want 1", len(ov.Mailboxes))
	}
	return ov.Mailboxes[0].Email
}

func TestLiveUniboxScopeDraftsCountMatchesVisibleUnion(t *testing.T) {
	handle := liveUniboxFolderDB(t)
	f := newUniboxFolderFixture(t, handle.Pool)
	other := newUniboxFolderFixture(t, handle.Pool)
	repo := NewUniboxRepository(handle)
	ctx := t.Context()
	now := time.Now().UTC()
	for _, tc := range []struct {
		thread string
		folder string
		status string
	}{
		{"mailbox-only", models.FolderDrafts, ""},
		{"overlap", models.FolderDrafts, "pending"},
		{"agent-only", models.FolderInbox, "pending"},
		{"archived", models.FolderArchive, "pending"},
		{"trashed", models.FolderTrash, "pending"},
		{"snoozed", models.FolderInbox, "pending"},
		{"resolved", models.FolderInbox, "discarded"},
	} {
		f.scopedMessage(t, repo, tc.thread, "them@example.com", tc.folder, now)
		if tc.status != "" {
			if _, err := handle.Pool.Exec(ctx, `INSERT INTO ai_thread_drafts
				(id, organization_id, email_account_id, owner_user_id, thread_id, to_addr, status)
				VALUES ($1,$2,$3,$4,$5,'them@example.com',$6)`, uuid.New(), f.org, f.mailbox, f.user, tc.thread, tc.status); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := repo.UpsertSnoozes(ctx, f.org, f.user, []string{"snoozed"}, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.DeleteSnoozes(context.Background(), f.org, f.user, []string{"snoozed"}) })
	other.scopedMessage(t, repo, "another-workspace", "them@example.com", models.FolderDrafts, now)

	folder, agent := models.FolderDrafts, true
	mailboxRows, err := repo.Search(ctx, f.org, &models.MailSearchParams{Folder: &folder, PageSize: 50})
	if err != nil {
		t.Fatal(err)
	}
	agentRows, err := repo.Search(ctx, f.org, &models.MailSearchParams{AgentDraft: &agent, PageSize: 50})
	if err != nil {
		t.Fatal(err)
	}
	visible := make(map[string]bool)
	for _, result := range []*models.MailSearchResult{mailboxRows, agentRows} {
		for _, thread := range threadIDs(result) {
			visible[thread] = true
		}
	}
	if len(visible) != 3 {
		t.Fatalf("visible draft threads = %v, want mailbox-only, overlap and agent-only", visible)
	}
	overview, err := repo.Overview(ctx, f.org)
	if err != nil || overview.Drafts != int64(len(visible)) {
		t.Fatalf("overview = %+v, error = %v, want %d distinct drafts", overview, err, len(visible))
	}
	for _, count := range overview.Folders {
		if count.Folder == models.FolderDrafts && count.Total != 2 {
			t.Fatalf("legacy mailbox Drafts total changed: %+v", count)
		}
	}
	restricted, err := repo.OverviewForMailboxes(ctx, f.org, []uuid.UUID{other.mailbox})
	if err != nil || restricted.Drafts != 0 {
		t.Fatalf("restricted overview = %+v, error = %v, want no drafts", restricted, err)
	}
}

func threadIDs(res *models.MailSearchResult) []string {
	out := make([]string, 0, len(res.Data))
	for _, row := range res.Data {
		out = append(out, row.ThreadID)
	}
	return out
}

func TestLiveUniboxScopeDraftsCountHonorsMailboxScope(t *testing.T) {
	handle := liveUniboxFolderDB(t)
	f := newUniboxFolderFixture(t, handle.Pool)
	foreign := newUniboxFolderFixture(t, handle.Pool)
	repo := NewUniboxRepository(handle)
	ctx := t.Context()
	now := time.Now().UTC()
	second := *f
	second.mailbox = visibilityMailbox(t, handle.Pool, f, now)
	for _, mailbox := range []*uniboxFolderFixture{f, &second, foreign} {
		mailbox.scopedMessage(t, repo, "shared-thread", "them@example.com", models.FolderInbox, now)
	}
	if _, err := handle.Pool.Exec(ctx, `INSERT INTO ai_thread_drafts
		(id, organization_id, email_account_id, owner_user_id, thread_id, to_addr)
		VALUES ($1,$2,$3,$4,'shared-thread','them@example.com')`, uuid.New(), f.org, second.mailbox, f.user); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		allowed []uuid.UUID
		want    int64
	}{
		{"first mailbox", []uuid.UUID{f.mailbox}, 0},
		{"draft mailbox", []uuid.UUID{second.mailbox}, 1},
		{"both mailboxes", []uuid.UUID{f.mailbox, second.mailbox}, 1},
		{"cross-org grant", []uuid.UUID{f.mailbox, foreign.mailbox}, 0},
		{"foreign mailbox", []uuid.UUID{foreign.mailbox}, 0},
		{"none", models.NoneMatch(), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent := true
			rows, err := repo.Search(ctx, f.org, &models.MailSearchParams{AgentDraft: &agent, EmailAccountIDs: tc.allowed, PageSize: 50})
			if err != nil {
				t.Fatal(err)
			}
			if int64(len(rows.Data)) != tc.want {
				t.Errorf("agent draft rows = %d, want %d", len(rows.Data), tc.want)
			}
			overview, err := repo.OverviewForMailboxes(ctx, f.org, tc.allowed)
			if err != nil {
				t.Fatal(err)
			}
			if overview.Drafts != tc.want || overview.AwaitingAgentDraft != tc.want {
				t.Errorf("draft counts = %d/%d, want %d", overview.Drafts, overview.AwaitingAgentDraft, tc.want)
			}
		})
	}
}

func TestLiveUniboxScopeDraftsCountMatchesUnsnoozedOwner(t *testing.T) {
	handle := liveUniboxFolderDB(t)
	f := newUniboxFolderFixture(t, handle.Pool)
	teammate := newUniboxFolderFixture(t, handle.Pool)
	repo := NewUniboxRepository(handle)
	ctx := t.Context()
	now := time.Now().UTC()
	if _, err := handle.Pool.Exec(ctx, `INSERT INTO organization_members
		(organization_id, user_id, role, accepted_at) VALUES ($1,$2,'member',NOW())`, f.org, teammate.user); err != nil {
		t.Fatal(err)
	}
	second := *f
	second.user = teammate.user
	second.mailbox = visibilityMailbox(t, handle.Pool, &second, now)
	f.scopedMessage(t, repo, "shared-snooze", "them@example.com", models.FolderInbox, now)
	second.scopedMessage(t, repo, "shared-snooze", "them@example.com", models.FolderDrafts, now)
	f.scopedMessage(t, repo, "shared-agent", "them@example.com", models.FolderInbox, now)
	second.scopedMessage(t, repo, "shared-agent", "them@example.com", models.FolderInbox, now)
	if _, err := handle.Pool.Exec(ctx, `INSERT INTO ai_thread_drafts
		(id, organization_id, email_account_id, owner_user_id, thread_id, to_addr)
		VALUES ($1,$2,$3,$4,'shared-agent','them@example.com')`, uuid.New(), f.org, second.mailbox, second.user); err != nil {
		t.Fatal(err)
	}
	threads := []string{"shared-snooze", "shared-agent"}
	if _, err := repo.UpsertSnoozes(ctx, f.org, f.user, threads, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.DeleteSnoozes(context.Background(), f.org, f.user, threads) })
	folder := models.FolderDrafts
	rows, err := repo.Search(ctx, f.org, &models.MailSearchParams{Folder: &folder, PageSize: 50})
	if err != nil || len(rows.Data) != 1 {
		t.Fatalf("mailbox draft rows = %+v, %v; want one", rows, err)
	}
	agent := true
	agentRows, err := repo.Search(ctx, f.org, &models.MailSearchParams{AgentDraft: &agent, PageSize: 50})
	if err != nil || len(agentRows.Data) != 1 {
		t.Fatalf("agent draft rows = %+v, %v; want one", agentRows, err)
	}
	overview, err := repo.Overview(ctx, f.org)
	if err != nil || overview.Drafts != 2 {
		t.Fatalf("overview = %+v, %v; want two drafts", overview, err)
	}
}

func listsThread(res *models.MailSearchResult, want string) bool {
	for _, id := range threadIDs(res) {
		if id == want {
			return true
		}
	}
	return false
}

// A mailbox sends as "Name <addr>". Comparing that against the address whole
// is what made Awaiting reply look empty on a workspace that had been sending
// all week.
func TestLiveUniboxScopeAwaitingReplyReadsTheAddressOutOfTheHeader(t *testing.T) {
	handle := liveUniboxFolderDB(t)
	f := newUniboxFolderFixture(t, handle.Pool)
	repo := NewUniboxRepository(handle)
	ctx := context.Background()
	ours := f.mailboxAddress(t, repo)
	now := time.Now().UTC()

	f.scopedMessage(t, repo, "thread-wrapped", "Alex at Acme <"+ours+">", models.FolderSent, now)
	f.scopedMessage(t, repo, "thread-theirs", "them@example.com", models.FolderInbox, now)

	awaiting := true
	res, err := repo.Search(ctx, f.org, &models.MailSearchParams{AwaitingReply: &awaiting, PageSize: 50})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !listsThread(res, "thread-wrapped") {
		t.Fatalf("awaiting reply = %v, want the thread we spoke last on", threadIDs(res))
	}
	if listsThread(res, "thread-theirs") {
		t.Fatalf("awaiting reply = %v, want no thread the other side spoke last on", threadIDs(res))
	}

	ov, err := repo.Overview(ctx, f.org)
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}
	if ov.AwaitingReply != 1 {
		t.Fatalf("overview awaiting_reply = %d, want 1 (the rail has to agree with the rows)", ov.AwaitingReply)
	}
}

// An address that merely contains ours is not ours.
func TestLiveUniboxScopeAwaitingReplyIsNotASubstringMatch(t *testing.T) {
	handle := liveUniboxFolderDB(t)
	f := newUniboxFolderFixture(t, handle.Pool)
	repo := NewUniboxRepository(handle)
	ctx := context.Background()
	ours := f.mailboxAddress(t, repo)

	f.scopedMessage(t, repo, "thread-lookalike", "Impostor <x"+ours+">", models.FolderInbox, time.Now().UTC())

	awaiting := true
	res, err := repo.Search(ctx, f.org, &models.MailSearchParams{AwaitingReply: &awaiting, PageSize: 50})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if listsThread(res, "thread-lookalike") {
		t.Fatalf("awaiting reply = %v, want an address that only contains ours left out", threadIDs(res))
	}
}

// Filing takes the conversation out of every view a workspace works from, and
// leaves it in All mail and in Archive. Anything less and the row comes back on
// the next read, which is what "Archive does nothing" looked like.
func TestLiveUniboxScopeArchiveLeavesTheWorkingViews(t *testing.T) {
	handle := liveUniboxFolderDB(t)
	f := newUniboxFolderFixture(t, handle.Pool)
	repo := NewUniboxRepository(handle)
	ctx := context.Background()
	ours := f.mailboxAddress(t, repo)
	now := time.Now().UTC()

	f.scopedMessage(t, repo, "thread-filed", "Alex at Acme <"+ours+">", models.FolderSent, now)

	awaiting, unseen, yes := true, true, true
	before, err := repo.Search(ctx, f.org, &models.MailSearchParams{AwaitingReply: &awaiting, PageSize: 50})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !listsThread(before, "thread-filed") {
		t.Fatal("the conversation is not in Awaiting reply to begin with")
	}

	if _, err := repo.MoveThreadsToFolder(ctx, f.org, []string{"thread-filed"}, models.FolderArchive); err != nil {
		t.Fatalf("MoveThreadsToFolder: %v", err)
	}

	for _, tc := range []struct {
		view   string
		params *models.MailSearchParams
	}{
		{"unscoped", &models.MailSearchParams{PageSize: 50}},
		{"awaiting reply", &models.MailSearchParams{AwaitingReply: &awaiting, PageSize: 50}},
		{"unread", &models.MailSearchParams{Unseen: &unseen, PageSize: 50}},
	} {
		res, err := repo.Search(ctx, f.org, tc.params)
		if err != nil {
			t.Fatalf("Search %s: %v", tc.view, err)
		}
		if listsThread(res, "thread-filed") {
			t.Errorf("a filed conversation is still in %s", tc.view)
		}
	}

	allMail, err := repo.Search(ctx, f.org, &models.MailSearchParams{IncludeArchived: &yes, PageSize: 50})
	if err != nil {
		t.Fatalf("Search all mail: %v", err)
	}
	if !listsThread(allMail, "thread-filed") {
		t.Error("a filed conversation has left All mail too; Archive is where it went, not nowhere")
	}

	archive := models.FolderArchive
	filed, err := repo.Search(ctx, f.org, &models.MailSearchParams{Folder: &archive, PageSize: 50})
	if err != nil {
		t.Fatalf("Search archive: %v", err)
	}
	if !listsThread(filed, "thread-filed") {
		t.Error("a filed conversation is not in the Archive folder")
	}

	ov, err := repo.Overview(ctx, f.org)
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}
	if ov.AwaitingReply != 0 || ov.Unread != 0 {
		t.Errorf("overview awaiting=%d unread=%d, want both 0 once the only conversation is filed", ov.AwaitingReply, ov.Unread)
	}
	if ov.Total != 1 {
		t.Errorf("overview total = %d, want 1: All mail still holds it", ov.Total)
	}
	unseenCount, err := repo.GetUnseenCount(ctx, f.org, nil)
	if err != nil {
		t.Fatalf("GetUnseenCount: %v", err)
	}
	if unseenCount != 0 {
		t.Errorf("unread badge = %d, want 0", unseenCount)
	}
}

// Filing by conversation has to take every message in it. Leaving one behind
// puts the whole row back, because a thread is shown by any message of it that
// is still in a working folder.
func TestLiveUniboxScopeArchiveTakesTheWholeConversation(t *testing.T) {
	handle := liveUniboxFolderDB(t)
	f := newUniboxFolderFixture(t, handle.Pool)
	repo := NewUniboxRepository(handle)
	ctx := context.Background()
	now := time.Now().UTC()

	f.scopedMessage(t, repo, "thread-pair", "them@example.com", models.FolderInbox, now.Add(-time.Hour))
	f.scopedMessage(t, repo, "thread-pair", "them@example.com", models.FolderInbox, now)

	if _, err := repo.MoveThreadsToFolder(ctx, f.org, []string{"thread-pair"}, models.FolderArchive); err != nil {
		t.Fatalf("MoveThreadsToFolder: %v", err)
	}

	res, err := repo.Search(ctx, f.org, &models.MailSearchParams{PageSize: 50})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if listsThread(res, "thread-pair") {
		t.Fatal("filing the conversation left part of it behind, so the row is still listed")
	}
}

// Another organization's conversations are not this one's to file.
func TestLiveUniboxScopeMoveThreadsIsOrgScoped(t *testing.T) {
	handle := liveUniboxFolderDB(t)
	mine := newUniboxFolderFixture(t, handle.Pool)
	theirs := newUniboxFolderFixture(t, handle.Pool)
	repo := NewUniboxRepository(handle)
	ctx := context.Background()

	id := theirs.scopedMessage(t, repo, "thread-theirs-only", "them@example.com", models.FolderInbox, time.Now().UTC())
	if _, err := repo.MoveThreadsToFolder(ctx, mine.org, []string{"thread-theirs-only"}, models.FolderArchive); err != nil {
		t.Fatalf("MoveThreadsToFolder: %v", err)
	}

	got, err := repo.GetByID(ctx, theirs.user, id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Folder != models.FolderInbox {
		t.Fatalf("folder = %q, want another org's conversation left at %q", got.Folder, models.FolderInbox)
	}
}

// Marking a conversation read from a list row addresses it by thread, because
// the row knows the conversation and not the ids inside it.
func TestLiveUniboxScopeMarkSeenByThreadsTakesTheWholeConversation(t *testing.T) {
	handle := liveUniboxFolderDB(t)
	f := newUniboxFolderFixture(t, handle.Pool)
	repo := NewUniboxRepository(handle)
	ctx := context.Background()
	now := time.Now().UTC()

	f.scopedMessage(t, repo, "thread-unread", "them@example.com", models.FolderInbox, now.Add(-time.Hour))
	f.scopedMessage(t, repo, "thread-unread", "them@example.com", models.FolderInbox, now)

	changed, err := repo.MarkSeenByThreads(ctx, f.org, []string{"thread-unread"}, true)
	if err != nil {
		t.Fatalf("MarkSeenByThreads: %v", err)
	}
	if len(changed) != 2 {
		t.Fatalf("changed %d messages, want both in the conversation", len(changed))
	}

	count, err := repo.GetUnseenCount(ctx, f.org, nil)
	if err != nil {
		t.Fatalf("GetUnseenCount: %v", err)
	}
	if count != 0 {
		t.Fatalf("unread = %d, want 0", count)
	}

	// Already read costs nothing at the provider: only real changes relay.
	again, err := repo.MarkSeenByThreads(ctx, f.org, []string{"thread-unread"}, true)
	if err != nil {
		t.Fatalf("MarkSeenByThreads again: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("changed %d messages on a no-op mark, want 0", len(again))
	}
}

// The selection bar snoozes a whole screenful in one call, so the repository
// takes the set. Upsert semantics hold per row: re-snoozing a conversation
// already in the set moves its time rather than failing the batch.
func TestLiveUniboxScopeSnoozeTakesASet(t *testing.T) {
	handle := liveUniboxFolderDB(t)
	f := newUniboxFolderFixture(t, handle.Pool)
	repo := NewUniboxRepository(handle)
	ctx := context.Background()
	now := time.Now().UTC()

	f.scopedMessage(t, repo, "thread-a", "them@example.com", models.FolderInbox, now)
	f.scopedMessage(t, repo, "thread-b", "them@example.com", models.FolderInbox, now)
	t.Cleanup(func() {
		_ = repo.DeleteSnoozes(context.Background(), f.org, f.user, []string{"thread-a", "thread-b"})
	})

	rows, err := repo.UpsertSnoozes(ctx, f.org, f.user, []string{"thread-a", "thread-b"}, now.Add(time.Hour))
	if err != nil {
		t.Fatalf("UpsertSnoozes: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("snoozed %d conversations, want 2", len(rows))
	}

	res, err := repo.Search(ctx, f.org, &models.MailSearchParams{PageSize: 50})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if listsThread(res, "thread-a") || listsThread(res, "thread-b") {
		t.Fatalf("a snoozed conversation is still listed: %v", threadIDs(res))
	}

	// A second pass over the same set moves the time instead of erroring.
	if _, err := repo.UpsertSnoozes(ctx, f.org, f.user, []string{"thread-a", "thread-b"}, now.Add(2*time.Hour)); err != nil {
		t.Fatalf("UpsertSnoozes again: %v", err)
	}

	if err := repo.DeleteSnoozes(ctx, f.org, f.user, []string{"thread-a", "thread-b"}); err != nil {
		t.Fatalf("DeleteSnoozes: %v", err)
	}
	back, err := repo.Search(ctx, f.org, &models.MailSearchParams{PageSize: 50})
	if err != nil {
		t.Fatalf("Search after un-snooze: %v", err)
	}
	if !listsThread(back, "thread-a") || !listsThread(back, "thread-b") {
		t.Fatalf("un-snoozing did not bring the conversations back: %v", threadIDs(back))
	}
}
