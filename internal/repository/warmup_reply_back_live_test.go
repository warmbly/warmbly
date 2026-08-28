package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Issue #151: a reply-back re-points the RECIPIENT's pending warmup task at the
// sender and pulls it forward. Only one warmup task may be pending per mailbox
// (CreateWarmupTaskWithLock), so it has to move the existing one rather than
// add work.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LiveReplyBack -v

type replyBackFixture struct {
	pool      *pgxpool.Pool
	user      uuid.UUID
	org       uuid.UUID
	recipient uuid.UUID
	sender    uuid.UUID
	repo      TaskRepository
}

func newReplyBackFixture(t *testing.T) *replyBackFixture {
	t.Helper()
	handle, pool := liveContactDB(t)
	ctx := context.Background()
	f := &replyBackFixture{
		pool: pool, user: uuid.New(), org: uuid.New(),
		recipient: uuid.New(), sender: uuid.New(),
		repo: NewTaskRepository(pool),
	}
	_ = handle
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture %q: %v", sql[:min(60, len(sql))], err)
		}
	}
	exec(`INSERT INTO users (id, email, first_name, last_name) VALUES ($1, $2, 'Reply', 'Back')`,
		f.user, "rb-"+f.user.String()[:8]+"@test.local")
	exec(`INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Reply Back', $2, $3)`,
		f.org, "rb-"+f.org.String()[:8], f.user)
	for _, id := range []uuid.UUID{f.recipient, f.sender} {
		exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain,
		          signature_html, provider, status, campaign_limit, min_wait_time, timezone)
		      VALUES ($1, $2, $3, $4, 'RB', '', '', 'smtp_imap', 'active', 50, 600, 'UTC')`,
			id, f.user, f.org, "rb-"+id.String()[:8]+"@test.local")
	}
	t.Cleanup(func() {
		c := context.Background()
		for _, step := range []struct {
			sql string
			arg any
		}{
			{`DELETE FROM warmup_tasks WHERE task_id IN (SELECT id FROM tasks WHERE email_account_id IN (SELECT id FROM email_accounts WHERE organization_id = $1))`, f.org},
			{`DELETE FROM tasks WHERE email_account_id IN (SELECT id FROM email_accounts WHERE organization_id = $1)`, f.org},
			{`DELETE FROM email_accounts WHERE organization_id = $1`, f.org},
			{`DELETE FROM organizations WHERE id = $1`, f.org},
			{`DELETE FROM users WHERE id = $1`, f.user},
		} {
			if _, err := pool.Exec(c, step.sql, step.arg); err != nil {
				t.Errorf("cleanup %q: %v", step.sql, err)
			}
		}
	})
	return f
}

// pendingWarmupTask schedules the recipient's ordinary next warmup send.
func (f *replyBackFixture) pendingWarmupTask(t *testing.T, at time.Time) uuid.UUID {
	t.Helper()
	id := uuid.New()
	created, err := f.repo.CreateWarmupTaskWithLock(context.Background(),
		&Task{ID: id, TaskType: "warmup", EmailAccountID: f.recipient, Status: "pending", ScheduledAt: &at},
		&WarmupTask{TaskID: id})
	if err != nil || !created {
		t.Fatalf("create pending task: created=%v err=%v", created, err)
	}
	return id
}

func (f *replyBackFixture) taskState(t *testing.T, id uuid.UUID) (time.Time, *uuid.UUID) {
	t.Helper()
	var at time.Time
	var target *uuid.UUID
	if err := f.pool.QueryRow(context.Background(), `
		SELECT t.scheduled_at, w.target_account_id
		  FROM tasks t JOIN warmup_tasks w ON w.task_id = t.id
		 WHERE t.id = $1`, id).Scan(&at, &target); err != nil {
		t.Fatalf("read task: %v", err)
	}
	return at, target
}

func TestLiveReplyBackPullsTheTaskForwardAndPointsIt(t *testing.T) {
	f := newReplyBackFixture(t)
	later := time.Now().Add(6 * time.Hour)
	id := f.pendingWarmupTask(t, later)

	replyAt := time.Now().Add(40 * time.Minute)
	moved, err := f.repo.DirectPendingWarmupTask(context.Background(), f.recipient, f.sender, replyAt)
	if err != nil || !moved {
		t.Fatalf("DirectPendingWarmupTask: moved=%v err=%v", moved, err)
	}

	at, target := f.taskState(t, id)
	if at.After(later.Add(-time.Minute)) {
		t.Errorf("scheduled_at = %s, want it pulled forward from %s", at, later)
	}
	if target == nil || *target != f.sender {
		t.Errorf("target = %v, want the sender %v", target, f.sender)
	}
}

// A reply-back must never DELAY work the mailbox had already planned sooner.
func TestLiveReplyBackNeverPushesATaskLater(t *testing.T) {
	f := newReplyBackFixture(t)
	soon := time.Now().Add(5 * time.Minute)
	id := f.pendingWarmupTask(t, soon)

	moved, err := f.repo.DirectPendingWarmupTask(context.Background(), f.recipient, f.sender, time.Now().Add(3*time.Hour))
	if err != nil {
		t.Fatalf("DirectPendingWarmupTask: %v", err)
	}
	if moved {
		t.Error("a later reply-back moved a task that was already due sooner")
	}
	at, target := f.taskState(t, id)
	if at.After(soon.Add(time.Minute)) {
		t.Errorf("scheduled_at = %s, want the original %s", at, soon)
	}
	if target != nil {
		t.Errorf("target = %v, want it left unset when the task did not move", target)
	}
}

// With nothing pending there is nothing to re-point; a reply-back must not
// invent work, or it would spend a mailbox's budget outside its own ramp.
func TestLiveReplyBackWithNoPendingTaskIsANoop(t *testing.T) {
	f := newReplyBackFixture(t)
	moved, err := f.repo.DirectPendingWarmupTask(context.Background(), f.recipient, f.sender, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("DirectPendingWarmupTask: %v", err)
	}
	if moved {
		t.Error("reported moving a task that does not exist")
	}
	var n int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM tasks WHERE email_account_id = $1`, f.recipient).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("%d tasks exist, want none created", n)
	}
}
