package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Queued sends leave from organization mailboxes, so the workspace that owns
// the mailbox lists, counts and cancels them, whichever member connected it,
// and no other workspace can.
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
		all, err := repo.ListScheduledInOrg(ctx, tc.org, 50)
		if err != nil || len(all) != tc.want {
			t.Errorf("list for %s = %d rows (err %v), want %d", tc.org, len(all), err, tc.want)
		}
		inThread, err := repo.ListScheduledInOrgByThread(ctx, tc.org, thread, 50)
		if err != nil || len(inThread) != tc.want {
			t.Errorf("thread list for %s = %d rows (err %v), want %d", tc.org, len(inThread), err, tc.want)
		}
		n, err := repo.CountScheduledInOrg(ctx, tc.org)
		if err != nil || n != int64(tc.want) {
			t.Errorf("count for %s = %d (err %v), want %d", tc.org, n, err, tc.want)
		}
	}

	if _, ok, err := repo.CancelScheduledInOrg(ctx, taskID, foreignOrg); err != nil || ok {
		t.Fatalf("foreign cancel ok=%v err=%v, want refused", ok, err)
	}
	if _, ok, err := repo.CancelScheduledInOrg(ctx, taskID, org); err != nil || !ok {
		t.Fatalf("workspace cancel ok=%v err=%v, want cancelled", ok, err)
	}
}

// A thread id is a mailbox's own provider handle once the mailbox holds a
// message in it, whether synced back or only recorded from its own send.
func TestLiveThreadHeldByMailbox(t *testing.T) {
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
	for _, id := range []uuid.UUID{holder, stranger} {
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
	for _, tc := range []struct {
		mailbox uuid.UUID
		thread  string
		want    bool
	}{
		{holder, "synced-thread", true},
		{holder, "sent-thread", true},
		{stranger, "synced-thread", false},
		{stranger, "sent-thread", false},
		{holder, "", false},
	} {
		held, err := repo.ThreadHeldByMailbox(ctx, tc.mailbox, tc.thread)
		if err != nil || held != tc.want {
			t.Errorf("held(%s, %q) = %v (err %v), want %v", tc.mailbox, tc.thread, held, err, tc.want)
		}
	}
}
