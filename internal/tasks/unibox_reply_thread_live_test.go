package tasks

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/tasks/proto"
)

// A unibox reply can leave from a mailbox other than the one holding the
// conversation. The provider thread handle belongs to the mailbox that holds
// it, so only that mailbox may send with it; any other one still threads for
// the recipient through In-Reply-To.
func TestLiveUniboxReplyUsesTheThreadHandleOnlyFromTheMailboxHoldingIt(t *testing.T) {
	handle := liveCampaignDB(t)
	f := newCampaignSendFixture(t, handle.Pool)
	other := f.otherMailbox(t, handle.Pool)
	// Gmail is the one provider with a thread handle to send.
	if _, err := handle.Pool.Exec(context.Background(),
		`UPDATE email_accounts SET provider = 'gmail' WHERE id = ANY($1)`, []uuid.UUID{f.mailbox, other}); err != nil {
		t.Fatalf("make the mailboxes gmail: %v", err)
	}
	const thread, parent = "gmail-thread-670", "<parent-670@test.local>"
	holdThread(t, handle.Pool, f.user, f.mailbox, thread, parent)

	for _, tc := range []struct {
		name       string
		mailbox    uuid.UUID
		wantThread string
	}{
		{"same mailbox keeps the handle", f.mailbox, thread},
		{"another mailbox drops it", other, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sender := &recordingSender{}
			svc := liveCampaignService(t, handle, sender)
			taskID := queueUniboxReply(t, handle.Pool, tc.mailbox, thread, parent)

			if xerr := svc.HandleUserEmailTask(&proto.ProcessTask{TaskId: taskID.String()}); xerr != nil {
				t.Fatalf("handle reply: %v", xerr)
			}
			msg := sender.message(t, 0)
			if msg.ThreadID != tc.wantThread {
				t.Errorf("thread handle = %q, want %q", msg.ThreadID, tc.wantThread)
			}
			if msg.InReplyTo != parent {
				t.Errorf("in_reply_to = %q, want %q so the recipient still threads it", msg.InReplyTo, parent)
			}
		})
	}
}

// holdThread stores one synced message in the thread for the mailbox.
func holdThread(t *testing.T, pool *pgxpool.Pool, user, mailbox uuid.UUID, thread, messageID string) {
	t.Helper()
	id := uuid.New()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO unibox_emails (id, user_id, email_id, thread_id, message_id, subject, folder, seen)
		VALUES ($1, $2, $3, $4, $5, 'Pricing', 'inbox', true)`, id, user, mailbox, thread, messageID); err != nil {
		t.Fatalf("hold thread: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM unibox_emails WHERE id = $1`, id); err != nil {
			t.Errorf("cleanup unibox email: %v", err)
		}
	})
}

// queueUniboxReply writes the task POST /unibox/reply creates, due now.
func queueUniboxReply(t *testing.T, pool *pgxpool.Pool, mailbox uuid.UUID, thread, inReplyTo string) uuid.UUID {
	t.Helper()
	now := time.Now()
	id := uuid.New()
	if err := repository.NewTaskRepository(pool).CreateEmailTaskFull(context.Background(),
		&repository.Task{ID: id, TaskType: "email", EmailAccountID: mailbox, Status: "pending", ScheduledAt: &now},
		&repository.EmailTask{
			TaskID: id, To: []string{"them@test.local"}, InReplyTo: []string{inReplyTo},
			Subject: "Re: Pricing", Body: "Sure", BodyPlain: "Sure", ThreadID: &thread, SendMode: "instant",
		}); err != nil {
		t.Fatalf("queue reply: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM tasks WHERE id = $1`, id); err != nil {
			t.Errorf("cleanup reply task: %v", err)
		}
	})
	return id
}
