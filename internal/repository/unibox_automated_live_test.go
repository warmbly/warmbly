package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/models"
)

// Mail nobody wrote leaves the inbox for the Automated view, and the rail,
// the badge and the list have to agree about it:
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/<db>?sslmode=disable \
//	  go test ./internal/repository/ -run LiveUniboxAutomated -v

func (f *uniboxFolderFixture) judge(t *testing.T, pool *pgxpool.Pool, messageID uuid.UUID, threadID string, automated bool) {
	t.Helper()
	kind := "human_reply"
	if automated {
		kind = "notification"
	}
	err := NewInboxTagRepository(pool).Save(context.Background(), &InboxTagResult{
		OrganizationID: f.org, EmailAccountID: f.mailbox,
		MessageID: "<" + messageID.String() + "@test.local>", ThreadID: threadID,
		Kind: kind, KindConfidence: 0.95, KindSource: "model", Priority: "whenever",
		Automated: automated,
	})
	if err != nil {
		t.Fatalf("Save verdict: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM inbox_tag_results WHERE organization_id = $1`, f.org)
	})
}

func TestLiveUniboxAutomatedLeavesTheInbox(t *testing.T) {
	handle := liveUniboxFolderDB(t)
	f := newUniboxFolderFixture(t, handle.Pool)
	repo := NewUniboxRepository(handle)
	ctx := context.Background()
	ours := f.mailboxAddress(t, repo)
	now := time.Now().UTC()

	alert := f.scopedMessage(t, repo, "thread-alert", "Google <no-reply@accounts.google.com>", models.FolderInbox, now)
	f.judge(t, handle.Pool, alert, "thread-alert", true)

	f.scopedMessage(t, repo, "thread-person", "them@example.com", models.FolderInbox, now)

	// A real conversation that ends in an autoresponder is still a conversation.
	f.scopedMessage(t, repo, "thread-conv", "Alex <"+ours+">", models.FolderSent, now.Add(-2*time.Hour))
	reply := f.scopedMessage(t, repo, "thread-conv", "them@example.com", models.FolderInbox, now.Add(-time.Hour))
	f.judge(t, handle.Pool, reply, "thread-conv", false)
	ooo := f.scopedMessage(t, repo, "thread-conv", "them@example.com", models.FolderInbox, now)
	f.judge(t, handle.Pool, ooo, "thread-conv", true)

	search := func(p models.MailSearchParams) *models.MailSearchResult {
		t.Helper()
		p.PageSize = 50
		res, err := repo.Search(ctx, f.org, &p)
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		return res
	}
	no, yes := false, true
	inbox := models.FolderInbox

	got := search(models.MailSearchParams{Folder: &inbox, Automated: &no})
	if listsThread(got, "thread-alert") || !listsThread(got, "thread-person") || !listsThread(got, "thread-conv") {
		t.Fatalf("inbox = %v, want the person and the conversation, not the alert", threadIDs(got))
	}
	got = search(models.MailSearchParams{Automated: &yes})
	if ids := threadIDs(got); len(ids) != 1 || ids[0] != "thread-alert" {
		t.Fatalf("automated = %v, want only the alert", ids)
	}
	if got = search(models.MailSearchParams{}); len(got.Data) != 3 {
		t.Fatalf("unfiltered = %v, want all three", threadIDs(got))
	}

	ov, err := repo.Overview(ctx, f.org)
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}
	if ov.Automated != 1 || ov.AutomatedUnread != 1 || ov.Unread != 2 {
		t.Fatalf("overview automated=%d automated_unread=%d unread=%d, want 1, 1, 2", ov.Automated, ov.AutomatedUnread, ov.Unread)
	}
	for _, fo := range ov.Folders {
		if fo.Folder == models.FolderInbox && fo.Unread != 2 {
			t.Fatalf("inbox folder unread = %d, want 2", fo.Unread)
		}
	}
	if ov.Mailboxes[0].Unread != 2 {
		t.Fatalf("mailbox unread = %d, want 2", ov.Mailboxes[0].Unread)
	}
	if n, err := repo.GetUnseenCount(ctx, f.org, nil); err != nil || n != 2 {
		t.Fatalf("unread badge = %d (%v), want 2", n, err)
	}

	// A person writing into it brings it back before anything judges them.
	f.scopedMessage(t, repo, "thread-alert", "them@example.com", models.FolderInbox, now.Add(time.Minute))
	got = search(models.MailSearchParams{Folder: &inbox, Automated: &no})
	if !listsThread(got, "thread-alert") {
		t.Fatalf("inbox = %v, want the alert thread back once a person wrote in it", threadIDs(got))
	}
	if n, _ := repo.GetUnseenCount(ctx, f.org, nil); n != 3 {
		t.Fatalf("unread badge = %d, want 3", n)
	}
}

// A message row re-created by a sync keeps the verdict its message already had.
func TestLiveUniboxAutomatedSurvivesARecreatedRow(t *testing.T) {
	handle := liveUniboxFolderDB(t)
	f := newUniboxFolderFixture(t, handle.Pool)
	repo := NewUniboxRepository(handle)
	ctx := context.Background()

	id := uuid.New()
	f.judge(t, handle.Pool, id, "thread-late", true)
	now := time.Now().UTC()
	if err := repo.CreateEntry(ctx, f.user, &models.EmailMessageStoreData{
		ID: id, EmailID: f.mailbox, Folder: models.FolderInbox,
		ThreadID: "thread-late", MessageID: "<" + id.String() + "@test.local>",
		FromAddr: []string{"no-reply@example.com"}, ToAddr: []string{"me@test.local"},
		Subject: "Receipt", InternalDate: now, SentDate: now, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}

	yes := true
	res, err := repo.Search(ctx, f.org, &models.MailSearchParams{Automated: &yes, PageSize: 50})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !listsThread(res, "thread-late") {
		t.Fatalf("automated = %v, want the row created after its verdict", threadIDs(res))
	}
}
