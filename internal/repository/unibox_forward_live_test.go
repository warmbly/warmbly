package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

// A forward sent from a Unibox conversation is filed into it, from either
// mailbox and whichever of the Sent sync and the send result lands first, while
// reply threading never treats it as part of the conversation.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LiveUniboxForward -v
func TestLiveUniboxForwardIsFiledIntoItsConversation(t *testing.T) {
	handle, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 275)
	f := newRoutedPairsFixture(t, pool, 0)
	ctx := context.Background()
	unibox := NewUniboxRepository(handle)
	tasks := NewTaskRepository(pool)

	other := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO email_accounts (id, user_id, organization_id, email, name,
	          signature_plain, signature_html, provider, status, campaign_limit, min_wait_time, timezone)
	      VALUES ($1, $2, $3, $4, 'Other', '', '', 'smtp_imap', 'active', 50, 0, 'UTC')`,
		other, f.owner, f.org, "fwd-other-"+other.String()[:8]+"@test.local"); err != nil {
		t.Fatalf("second mailbox: %v", err)
	}
	var taskIDs []uuid.UUID
	t.Cleanup(func() {
		c := context.Background()
		for _, step := range []struct {
			sql string
			arg any
		}{
			{`DELETE FROM unibox_emails WHERE email_id = ANY($1)`, []uuid.UUID{f.mailbox, other}},
			{`DELETE FROM email_tasks WHERE task_id = ANY($1)`, taskIDs},
			{`DELETE FROM tasks WHERE id = ANY($1)`, taskIDs},
			{`DELETE FROM email_accounts WHERE id = $1`, other},
		} {
			if _, err := pool.Exec(c, step.sql, step.arg); err != nil {
				t.Errorf("cleanup %q: %v", step.sql, err)
			}
		}
	})

	const conversation = "conv-forward-1"
	store := func(mailbox uuid.UUID, threadID, messageID, folder string, at time.Time) *models.EmailMessageStoreData {
		t.Helper()
		m := &models.EmailMessageStoreData{
			ID: uuid.New(), EmailID: mailbox, ThreadID: threadID, MessageID: messageID, Folder: folder,
			Subject: "Packaging", InternalDate: at, SentDate: at, CreatedAt: at, UpdatedAt: at,
		}
		if err := unibox.CreateEntry(ctx, f.owner, m); err != nil {
			t.Fatalf("store %s: %v", messageID, err)
		}
		return m
	}
	forward := func(mailbox uuid.UUID, status, messageID string) uuid.UUID {
		t.Helper()
		id := uuid.New()
		taskIDs = append(taskIDs, id)
		at := time.Now().Add(time.Hour)
		thread := conversation
		if err := tasks.CreateEmailTaskFull(ctx,
			&Task{ID: id, TaskType: "email", EmailAccountID: mailbox, Status: status, MessageID: messageID, ScheduledAt: &at},
			&EmailTask{TaskID: id, To: []string{"team@example.com"}, Subject: "Fwd: Packaging", ForwardedPlain: "forwarded", ForwardThreadID: &thread},
		); err != nil {
			t.Fatalf("forward task: %v", err)
		}
		return id
	}

	base := time.Now().Add(-time.Hour)
	store(f.mailbox, conversation, "<prospect@example.com>", models.FolderInbox, base)

	// The send result came first: the Sent copy is filed as it is stored, from
	// another mailbox and with the Message-ID written without brackets.
	forward(other, "completed", "<fwd-1@test.local>")
	sent := store(other, "other-own-thread", "fwd-1@test.local", models.FolderSent, base.Add(time.Minute))
	if sent.ThreadID != conversation {
		t.Fatalf("sent copy stored under %q, want %q", sent.ThreadID, conversation)
	}

	// The Sent copy came first: the send result files it.
	late := forward(f.mailbox, "completed", "")
	early := store(f.mailbox, "own-thread-2", "<fwd-2@test.local>", models.FolderSent, base.Add(2*time.Minute))
	if early.ThreadID != "own-thread-2" {
		t.Fatalf("copy with no known send filed into %q", early.ThreadID)
	}
	if err := tasks.UpdateTaskMessageID(ctx, late, "<fwd-2@test.local>"); err != nil {
		t.Fatal(err)
	}
	filed, err := unibox.FileSentForward(ctx, late)
	if err != nil {
		t.Fatal(err)
	}
	if len(filed) != 1 || filed[0].Message.ID != early.ID || filed[0].Message.ThreadID != conversation || filed[0].UserID != f.owner {
		t.Fatalf("filed %+v", filed)
	}
	if again, err := unibox.FileSentForward(ctx, late); err != nil || len(again) != 0 {
		t.Fatalf("second filing moved %d rows (err %v)", len(again), err)
	}

	thread, err := unibox.GetByThread(ctx, f.org, uuid.Nil, conversation, 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(thread.Data) != 3 {
		t.Fatalf("conversation holds %d messages, want the reply and both forwards", len(thread.Data))
	}

	// A reply still answers the prospect, and a mailbox whose only message in the
	// conversation is a forward has no provider thread in it.
	parent, err := unibox.LatestMessageIDInThread(ctx, f.org, conversation)
	if err != nil || parent != "<prospect@example.com>" {
		t.Fatalf("reply parent %q (err %v), want the prospect's message", parent, err)
	}
	if own, err := tasks.ProviderThreadForMailbox(ctx, other, conversation); err != nil || own != "" {
		t.Fatalf("provider thread %q (err %v), want none", own, err)
	}

	// A queued forward is listed with the conversation's scheduled sends.
	queued := forward(other, "pending", "")
	items, err := tasks.ListScheduledInOrgByThread(ctx, f.org, conversation, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].TaskID != queued || items[0].ThreadID == nil || *items[0].ThreadID != conversation {
		t.Fatalf("scheduled in conversation: %+v", items)
	}
}
