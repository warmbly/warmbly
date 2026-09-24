package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Queued sends leave from organization mailboxes, so the workspace that owns
// the mailbox lists, counts and cancels them, whichever member connected it,
// and no other workspace can. An API key's mailbox allowlist narrows the list
// and the cancel to its own mailboxes.
func TestLiveScheduledSendsAreScopedToTheMailboxOrganization(t *testing.T) {
	_, pool := liveContactDB(t)
	ctx := context.Background()
	owner, org, foreignOrg, mailbox := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	const thread = "thread-scheduled-scope"

	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	exec(`INSERT INTO users (id, first_name, last_name, email) VALUES ($1, 'Sched', 'Scope', $2)`,
		owner, "sched-"+uuid.NewString()+"@example.test")
	exec(`INSERT INTO organizations (id, name, owner_user_id) VALUES ($1, 'Sched', $2), ($3, 'Foreign', $2)`,
		org, owner, foreignOrg)
	exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain, signature_html, provider)
	      VALUES ($1, $2, $3, $4, 'Sched', '', '', 'smtp_imap')`,
		mailbox, owner, org, "sched-"+uuid.NewString()+"@example.test")
	t.Cleanup(func() {
		for _, step := range []struct {
			query string
			arg   uuid.UUID
		}{
			{`DELETE FROM tasks WHERE email_account_id = $1`, mailbox},
			{`DELETE FROM email_accounts WHERE id = $1`, mailbox},
			{`DELETE FROM organizations WHERE owner_user_id = $1`, owner},
			{`DELETE FROM users WHERE id = $1`, owner},
		} {
			if _, err := pool.Exec(context.Background(), step.query, step.arg); err != nil {
				t.Errorf("cleanup: %v", err)
			}
		}
	})

	repo := NewTaskRepository(pool)
	at := time.Now().Add(time.Hour)
	taskID := uuid.New()
	threadID := thread
	if err := repo.CreateEmailTaskFull(ctx,
		&Task{ID: taskID, TaskType: "email", EmailAccountID: mailbox, Status: "pending", ScheduledAt: &at},
		&EmailTask{TaskID: taskID, To: []string{"them@example.test"}, Subject: "Re: Hi", Body: "Later", BodyPlain: "Later", ThreadID: &threadID, SendMode: "scheduled"},
	); err != nil {
		t.Fatalf("queue send: %v", err)
	}

	for _, tc := range []struct {
		org  uuid.UUID
		want int
	}{{org, 1}, {foreignOrg, 0}} {
		all, err := repo.ListScheduledInOrg(ctx, tc.org, nil, 50)
		if err != nil || len(all) != tc.want {
			t.Errorf("list for %s = %d rows (err %v), want %d", tc.org, len(all), err, tc.want)
		}
		inThread, err := repo.ListScheduledInOrgByThread(ctx, tc.org, thread, nil, 50)
		if err != nil || len(inThread) != tc.want {
			t.Errorf("thread list for %s = %d rows (err %v), want %d", tc.org, len(inThread), err, tc.want)
		}
		n, err := repo.CountScheduledInOrg(ctx, tc.org)
		if err != nil || n != int64(tc.want) {
			t.Errorf("count for %s = %d (err %v), want %d", tc.org, n, err, tc.want)
		}
	}

	elsewhere := []uuid.UUID{uuid.New()}
	if rows, err := repo.ListScheduledInOrg(ctx, org, elsewhere, 50); err != nil || len(rows) != 0 {
		t.Errorf("list outside the allowlist = %d rows (err %v), want none", len(rows), err)
	}
	if rows, err := repo.ListScheduledInOrgByThread(ctx, org, thread, elsewhere, 50); err != nil || len(rows) != 0 {
		t.Errorf("thread list outside the allowlist = %d rows (err %v), want none", len(rows), err)
	}
	if rows, err := repo.ListScheduledInOrg(ctx, org, []uuid.UUID{mailbox}, 50); err != nil || len(rows) != 1 {
		t.Errorf("list inside the allowlist = %d rows (err %v), want 1", len(rows), err)
	}

	if _, ok, err := repo.CancelScheduledInOrg(ctx, taskID, foreignOrg, nil); err != nil || ok {
		t.Fatalf("foreign cancel ok=%v err=%v, want refused", ok, err)
	}
	if _, ok, err := repo.CancelScheduledInOrg(ctx, taskID, org, elsewhere); err != nil || ok {
		t.Fatalf("cancel outside the allowlist ok=%v err=%v, want refused", ok, err)
	}
	if _, ok, err := repo.CancelScheduledInOrg(ctx, taskID, org, nil); err != nil || !ok {
		t.Fatalf("workspace cancel ok=%v err=%v, want cancelled", ok, err)
	}
}

// A conversation id is a mailbox's own provider handle once the mailbox holds a
// message in it, synced back or only recorded from its own send. A mailbox that
// replied into the conversation from outside keeps using the thread that reply
// landed in.
func TestLiveProviderThreadForMailbox(t *testing.T) {
	_, pool := liveContactDB(t)
	ctx := context.Background()
	owner, org, holder, stranger := uuid.New(), uuid.New(), uuid.New(), uuid.New()

	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	exec(`INSERT INTO users (id, first_name, last_name, email) VALUES ($1, 'Held', 'Thread', $2)`,
		owner, "held-"+uuid.NewString()+"@example.test")
	exec(`INSERT INTO organizations (id, name, owner_user_id) VALUES ($1, 'Held', $2)`, org, owner)
	outsider := uuid.New()
	for _, id := range []uuid.UUID{holder, stranger, outsider} {
		exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain, signature_html, provider)
		      VALUES ($1, $2, $3, $4, 'Held', '', '', 'gmail')`, id, owner, org, "held-"+uuid.NewString()+"@example.test")
	}
	t.Cleanup(func() {
		c := context.Background()
		for _, q := range []string{
			`DELETE FROM unibox_emails WHERE email_id IN (SELECT id FROM email_accounts WHERE organization_id = $1)`,
			`DELETE FROM tasks WHERE email_account_id IN (SELECT id FROM email_accounts WHERE organization_id = $1)`,
			`DELETE FROM email_accounts WHERE organization_id = $1`,
			`DELETE FROM organizations WHERE id = $1`,
		} {
			if _, err := pool.Exec(c, q, org); err != nil {
				t.Errorf("cleanup: %v", err)
			}
		}
		if _, err := pool.Exec(c, `DELETE FROM users WHERE id = $1`, owner); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	})
	exec(`INSERT INTO unibox_emails (id, user_id, email_id, thread_id, subject, folder, seen)
	      VALUES ($1, $2, $3, 'synced-thread', 'Hi', 'inbox', true)`, uuid.New(), owner, holder)
	exec(`INSERT INTO tasks (id, task_type, email_account_id, status, message_id, thread_id, completed_at)
	      VALUES ($1, 'email', $2, 'completed', '<sent@example.test>', 'sent-thread', NOW())`, uuid.New(), holder)

	repo := NewTaskRepository(pool)
	// The outsider's earlier reply into synced-thread landed in its own thread.
	earlier := uuid.New()
	conversation := "synced-thread"
	if err := repo.CreateEmailTaskFull(ctx,
		&Task{ID: earlier, TaskType: "email", EmailAccountID: outsider, Status: "completed"},
		&EmailTask{TaskID: earlier, To: []string{"them@example.test"}, Subject: "Re: Hi", Body: "x", BodyPlain: "x", ThreadID: &conversation, SendMode: "instant"},
	); err != nil {
		t.Fatalf("earlier reply: %v", err)
	}
	exec(`UPDATE tasks SET thread_id = 'outsider-thread', completed_at = NOW() WHERE id = $1`, earlier)

	for _, tc := range []struct {
		mailbox uuid.UUID
		thread  string
		want    string
	}{
		{holder, "synced-thread", "synced-thread"},
		{holder, "sent-thread", "sent-thread"},
		{stranger, "synced-thread", ""},
		{stranger, "sent-thread", ""},
		{outsider, "synced-thread", "outsider-thread"},
		{holder, "", ""},
	} {
		handle, err := repo.ProviderThreadForMailbox(ctx, tc.mailbox, tc.thread)
		if err != nil || handle != tc.want {
			t.Errorf("thread(%s, %q) = %q (err %v), want %q", tc.mailbox, tc.thread, handle, err, tc.want)
		}
	}
}
