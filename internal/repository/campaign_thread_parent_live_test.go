package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ThreadParentForLead decides which email a campaign follow-up is sent as a
// reply to, and which subject it carries. It is hand-written SQL plus a
// hand-written walk back through the contact's sends, and getting either wrong
// is invisible until a recipient sees a second cold email instead of a nudge
// (issue #472), so it is asserted directly.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LiveThreadParent -v

type threadParentFixture struct {
	pool                                 *pgxpool.Pool
	owner, org, mailbox, other, campaign uuid.UUID
	contact                              uuid.UUID
	exec                                 func(sql string, args ...any)
}

func newThreadParentFixture(t *testing.T, pool *pgxpool.Pool) *threadParentFixture {
	t.Helper()
	ctx := context.Background()
	f := &threadParentFixture{
		pool: pool, owner: uuid.New(), org: uuid.New(), mailbox: uuid.New(),
		other: uuid.New(), campaign: uuid.New(), contact: uuid.New(),
	}
	f.exec = func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture %q: %v", sql[:min(70, len(sql))], err)
		}
	}
	f.exec(`INSERT INTO users (id, first_name, last_name, email, password_hash)
	        VALUES ($1, 'Thread', 'Parent', $2, 'x')`, f.owner, "thread-"+f.owner.String()[:8]+"@test.local")
	f.exec(`INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Thread Parent', $2, $3)`,
		f.org, "thread-"+f.org.String()[:8], f.owner)
	for _, mb := range []uuid.UUID{f.mailbox, f.other} {
		f.exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name,
		            signature_plain, signature_html, provider, status, campaign_limit, min_wait_time, timezone)
		        VALUES ($1, $2, $3, $4, 'Thread', '', '', 'smtp_imap', 'active', 50, 0, 'UTC')`,
			mb, f.owner, f.org, "thread-mb-"+mb.String()[:8]+"@test.local")
	}
	f.exec(`INSERT INTO campaigns (id, user_id, organization_id, name, description, status,
	            daily_limit, timezone, days, start_time, end_time, rotation_mode, updated_at, created_at)
	        VALUES ($1, $2, $3, 'Thread Parent', '', 'active', 50, 'UTC', 127, '00:00', '23:59',
	                'least_recently_used', NOW(), NOW())`, f.campaign, f.owner, f.org)
	f.exec(`INSERT INTO contacts (id, user_id, organization_id, email, first_name, last_name,
	            company, phone, custom_fields, verification_status, created_at)
	        VALUES ($1, $2, $3, $4, 'Thread', 'Lead', '', '', '{}', 'valid', NOW())`,
		f.contact, f.owner, f.org, "thread-lead-"+f.contact.String()[:8]+"@test.local")

	t.Cleanup(func() {
		c := context.Background()
		for _, step := range []struct {
			sql string
			arg any
		}{
			{`DELETE FROM campaign_tasks WHERE campaign_id = $1`, f.campaign},
			{`DELETE FROM tasks WHERE email_account_id = $1`, f.mailbox},
			{`DELETE FROM tasks WHERE email_account_id = $1`, f.other},
			{`DELETE FROM sequences WHERE campaign_id = $1`, f.campaign},
			{`DELETE FROM campaigns WHERE id = $1`, f.campaign},
			{`DELETE FROM contacts WHERE organization_id = $1`, f.org},
			{`DELETE FROM email_accounts WHERE organization_id = $1`, f.org},
			{`DELETE FROM organizations WHERE id = $1`, f.org},
			{`DELETE FROM users WHERE id = $1`, f.owner},
		} {
			if _, err := pool.Exec(c, step.sql, step.arg); err != nil {
				t.Errorf("cleanup %q: %v", step.sql, err)
			}
		}
	})
	return f
}

// step inserts an email step at the given position.
func (f *threadParentFixture) step(position int, subject string, threadReply bool) uuid.UUID {
	id := uuid.New()
	f.exec(`INSERT INTO sequences (id, campaign_id, organization_id, name, subject,
	            body_plain, body_html, wait_after, position, kind, thread_reply)
	        VALUES ($1, $2, $3, 'Step', $4, 'Hello', '<p>Hello</p>', 0, $5, 'email', $6)`,
		id, f.campaign, f.org, subject, position, threadReply)
	return id
}

// send records a completed campaign send of `step` to the fixture's contact,
// `ageSeconds` in the past so ordering is unambiguous.
func (f *threadParentFixture) send(stepID, mailbox uuid.UUID, messageID, threadID string, ageSeconds int) uuid.UUID {
	taskID := uuid.New()
	f.exec(`INSERT INTO tasks (id, task_type, email_account_id, status, message_id, thread_id, created_at, updated_at)
	        VALUES ($1, 'campaign', $2, 'completed', $3, $4, NOW() - make_interval(secs => $5), NOW())`,
		taskID, mailbox, messageID, threadID, float64(ageSeconds))
	f.exec(`INSERT INTO campaign_tasks (task_id, campaign_id, contact_id, sequence_id)
	        VALUES ($1, $2, $3, $4)`, taskID, f.campaign, f.contact, stepID)
	return taskID
}

func (f *threadParentFixture) parent(t *testing.T) *ThreadParent {
	t.Helper()
	p, err := NewCampaignProgressRepository(f.pool).ThreadParentForLead(context.Background(), f.campaign, f.contact)
	if err != nil {
		t.Fatalf("thread parent: %v", err)
	}
	return p
}

// A contact who has received nothing has no thread: their first email opens one.
func TestLiveThreadParentIsNilBeforeTheFirstSend(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newThreadParentFixture(t, pool)
	f.step(0, "Quick question", true)

	if p := f.parent(t); p != nil {
		t.Fatalf("got parent %+v, want none before anything was sent", p)
	}
}

// The parent is the LAST send, and the subject is the conversation's — which
// lives on the step that opened it, not on the blank-subject follow-up in
// between.
func TestLiveThreadParentCarriesTheConversationSubject(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newThreadParentFixture(t, pool)
	first := f.step(0, "Quick question", true)
	second := f.step(1, "", true)
	f.send(first, f.mailbox, "<one@test.local>", "thr-1", 120)
	f.send(second, f.mailbox, "<two@test.local>", "thr-1", 60)

	p := f.parent(t)
	if p == nil {
		t.Fatal("got no parent, want the second send")
	}
	if p.MessageID != "<two@test.local>" {
		t.Errorf("parent message id = %q, want the most recent send", p.MessageID)
	}
	if p.ThreadID != "thr-1" {
		t.Errorf("parent thread id = %q, want thr-1", p.ThreadID)
	}
	if p.SenderID != f.mailbox {
		t.Errorf("parent sender = %s, want the sending mailbox %s", p.SenderID, f.mailbox)
	}
	if p.Subject != "Quick question" {
		t.Errorf("conversation subject = %q, want the subject the thread was opened with", p.Subject)
	}
}

// A step that deliberately starts a new thread becomes the conversation the
// steps after it join, so its subject is the one they inherit.
func TestLiveThreadParentStopsAtTheStepThatStartedTheThread(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newThreadParentFixture(t, pool)
	first := f.step(0, "Quick question", true)
	fresh := f.step(1, "New angle", false)
	after := f.step(2, "", true)
	f.send(first, f.mailbox, "<one@test.local>", "thr-1", 180)
	f.send(fresh, f.mailbox, "<two@test.local>", "thr-2", 120)
	f.send(after, f.mailbox, "<three@test.local>", "thr-2", 60)

	p := f.parent(t)
	if p == nil {
		t.Fatal("got no parent, want the third send")
	}
	if p.Subject != "New angle" {
		t.Errorf("conversation subject = %q, want the subject of the step that started this thread", p.Subject)
	}
	if p.ThreadID != "thr-2" {
		t.Errorf("parent thread id = %q, want thr-2", p.ThreadID)
	}
}

// A send the worker never answered leaves no Message-ID, so it cannot be
// referenced: the follow-up threads on the last send that has one.
func TestLiveThreadParentSkipsSendsWithNoMessageID(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newThreadParentFixture(t, pool)
	first := f.step(0, "Quick question", true)
	second := f.step(1, "", true)
	f.send(first, f.mailbox, "<one@test.local>", "thr-1", 120)
	f.send(second, f.mailbox, "", "", 60)

	p := f.parent(t)
	if p == nil {
		t.Fatal("got no parent, want the first send")
	}
	if p.MessageID != "<one@test.local>" {
		t.Errorf("parent message id = %q, want the last send that reported one", p.MessageID)
	}
}

// The sending mailbox travels with the parent: a provider thread handle only
// means something inside the mailbox that owns it.
func TestLiveThreadParentReportsTheSendingMailbox(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newThreadParentFixture(t, pool)
	first := f.step(0, "Quick question", true)
	f.send(first, f.other, "<one@test.local>", "thr-1", 60)

	p := f.parent(t)
	if p == nil {
		t.Fatal("got no parent, want the first send")
	}
	if p.SenderID != f.other {
		t.Errorf("parent sender = %s, want %s", p.SenderID, f.other)
	}
}

// A task that never got past 'active' (or was walked back) is not a send the
// recipient has, so it is not something to reply to.
func TestLiveThreadParentIgnoresUnfinishedTasks(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newThreadParentFixture(t, pool)
	first := f.step(0, "Quick question", true)
	taskID := f.send(first, f.mailbox, "<one@test.local>", "thr-1", 60)
	f.exec(`UPDATE tasks SET status = 'failed' WHERE id = $1`, taskID)

	if p := f.parent(t); p != nil {
		t.Fatalf("got parent %+v, want none: the send failed", p)
	}
}

// A step deleted after it sent leaves campaign_tasks.sequence_id NULL, so
// nothing records whether it opened the conversation or joined one. The walk
// must stop there rather than handing back an older step's subject: the
// caller pairs that subject with the deleted send's provider thread, and the
// two need not belong together.
func TestLiveThreadParentStopsAtADeletedStep(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newThreadParentFixture(t, pool)
	first := f.step(0, "Quick question", false)
	gone := f.step(1, "New angle", false)
	f.send(first, f.mailbox, "<one@test.local>", "thr-1", 120)
	f.send(gone, f.mailbox, "<two@test.local>", "thr-2", 60)
	f.exec(`DELETE FROM sequences WHERE id = $1`, gone)

	p := f.parent(t)
	if p == nil {
		t.Fatal("got no parent, want the deleted step's send")
	}
	if p.MessageID != "<two@test.local>" {
		t.Errorf("parent message id = %q, want the most recent send", p.MessageID)
	}
	if p.Subject != "" {
		t.Errorf("conversation subject = %q, want none: the step that sent it is gone", p.Subject)
	}
}
