package email

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/app/worker"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// Live cover for the disable and disconnect paths against real SQL. Skipped
// unless WARMBLY_TEST_DB is set, so `go test ./...` stays hermetic:
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/app/email/ -run Live -v
//
// Stubs cannot see any of what these check: the delete path used to read the
// mailbox through a query scoped by organization while handing it a user id,
// so it never found the row, and the worker assignment lives in a column no
// update returns.

type removalLiveFixture struct {
	pool    *pgxpool.Pool
	svc     *emailService
	pub     *stubEventPublisher
	user    uuid.UUID
	org     uuid.UUID
	mailbox uuid.UUID
	worker  uuid.UUID
}

func newRemovalLiveFixture(t *testing.T) *removalLiveFixture {
	t.Helper()
	dsn := os.Getenv("WARMBLY_TEST_DB")
	if dsn == "" {
		t.Skip("WARMBLY_TEST_DB not set")
	}
	handle, err := db.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { handle.Pool.Close() })

	ctx := context.Background()
	f := &removalLiveFixture{pool: handle.Pool, user: uuid.New(), org: uuid.New(), mailbox: uuid.New(), worker: uuid.New()}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := f.pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	exec(`INSERT INTO users (id, email, first_name, last_name) VALUES ($1, $2, 'Drop', 'Test')`,
		f.user, "drop-"+f.user.String()[:8]+"@test.local")
	exec(`INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Drop Test', $2, $3)`,
		f.org, "drop-"+f.org.String()[:8], f.user)
	// One mailbox's worth of load: an smtp_imap mailbox that is not warming
	// weighs 1.0, which is what the delete has to refund.
	// A worker is a node (the machine) plus a placement row (the mail on it).
	exec(`INSERT INTO fleet_nodes (id, role, name, address, active, last_seen_at)
	      VALUES ($1, 'worker', 'drop-test', '127.0.0.1', true, now())`, f.worker)
	exec(`INSERT INTO workers (id, account_count, load_score)
	      VALUES ($1, 1, 1)`, f.worker)
	exec(`INSERT INTO email_accounts (id, user_id, organization_id, worker_id, email, name,
	          signature_plain, signature_html, provider, status, campaign_limit, min_wait_time)
	      VALUES ($1, $2, $3, $4, $5, 'Drop', '', '', 'smtp_imap', 'active', 50, 600)`,
		f.mailbox, f.user, f.org, f.worker, "drop-"+f.mailbox.String()[:8]+"@test.local")

	t.Cleanup(func() {
		c := context.Background()
		for _, step := range []struct {
			sql string
			arg any
		}{
			{`DELETE FROM email_accounts WHERE id = $1`, f.mailbox},
			{`DELETE FROM fleet_nodes WHERE id = $1`, f.worker},
			{`DELETE FROM organizations WHERE id = $1`, f.org},
			{`DELETE FROM users WHERE id = $1`, f.user},
		} {
			if _, err := f.pool.Exec(c, step.sql, step.arg); err != nil {
				t.Errorf("cleanup %q: %v", step.sql, err)
			}
		}
	})

	f.pub = &stubEventPublisher{}
	f.svc = &emailService{
		emailRepository:  repository.NewEmailRepostory(handle, nil),
		publisher:        f.pub,
		workerAssignment: worker.NewAssignmentService(repository.NewWorkerRepository(f.pool), nil, nil),
	}
	return f
}

func (f *removalLiveFixture) mailboxExists(t *testing.T) bool {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM email_accounts WHERE id = $1`, f.mailbox).Scan(&n); err != nil {
		t.Fatalf("read back: %v", err)
	}
	return n == 1
}

func (f *removalLiveFixture) workerLoad(t *testing.T) (int, float64) {
	t.Helper()
	var count int
	var score float64
	if err := f.pool.QueryRow(context.Background(),
		`SELECT account_count, load_score FROM workers WHERE id = $1`, f.worker).Scan(&count, &score); err != nil {
		t.Fatalf("read worker: %v", err)
	}
	return count, score
}

// Disabling writes the status AND tells the worker holding the mailbox, with
// the assignment read from the column rather than from the row the update
// returns (which does not carry one).
func TestLiveDisablingAMailboxRemovesItFromItsWorker(t *testing.T) {
	f := newRemovalLiveFixture(t)
	inactive := "inactive"

	account, xerr := f.svc.Update(context.Background(), f.org.String(), f.user.String(), f.mailbox.String(), &models.UpdateEmail{Status: &inactive})
	if xerr != nil {
		t.Fatalf("update: %v", xerr)
	}
	if account.Status != "inactive" {
		t.Fatalf("status = %q, want inactive", account.Status)
	}
	if len(f.pub.removed) != 1 {
		t.Fatalf("published %d removals, want 1", len(f.pub.removed))
	}
	if f.pub.removed[0].workerID != f.worker || f.pub.removed[0].emailID != f.mailbox.String() {
		t.Errorf("removal = %+v, want worker %s and mailbox %s", f.pub.removed[0], f.worker, f.mailbox)
	}

	// The mailbox keeps its placement while disabled, so re-enabling puts it
	// back on the same worker and the same sending IP.
	var assigned *uuid.UUID
	if err := f.pool.QueryRow(context.Background(),
		`SELECT worker_id FROM email_accounts WHERE id = $1`, f.mailbox).Scan(&assigned); err != nil {
		t.Fatalf("read assignment: %v", err)
	}
	if assigned == nil || *assigned != f.worker {
		t.Errorf("assignment = %v, want it kept at %s", assigned, f.worker)
	}
}

// Deleting has to reach the worker BEFORE the row goes: afterwards there is no
// worker_id left to read.
func TestLiveDeletingAMailboxRemovesItFromItsWorkerFirst(t *testing.T) {
	f := newRemovalLiveFixture(t)

	if xerr := f.svc.Delete(context.Background(), f.user.String(), f.mailbox.String()); xerr != nil {
		t.Fatalf("delete: %v", xerr)
	}

	if len(f.pub.removed) != 1 {
		t.Fatalf("published %d removals, want 1: the worker keeps syncing a mailbox that no longer exists", len(f.pub.removed))
	}
	if f.pub.removed[0].workerID != f.worker {
		t.Errorf("removal sent to worker %s, want %s", f.pub.removed[0].workerID, f.worker)
	}
	if f.mailboxExists(t) {
		t.Error("the mailbox row survived the delete")
	}

	// And the worker gets its capacity back, or every disconnect permanently
	// shrinks what that machine can be given.
	count, score := f.workerLoad(t)
	if count != 0 || score != 0 {
		t.Errorf("worker still charged for the deleted mailbox: account_count=%d load_score=%v", count, score)
	}
}

// The refund shares the delete's transaction, so a delete that matches no row
// must leave the worker's capacity exactly as it was. Driven through the
// repository, because the service refuses a foreign owner before it gets here.
func TestLiveDeleteThatMatchesNoRowRefundsNothing(t *testing.T) {
	f := newRemovalLiveFixture(t)

	if xerr := f.svc.emailRepository.Delete(context.Background(), uuid.New().String(), f.mailbox.String(), 1); xerr != errx.ErrNotFound {
		t.Fatalf("error = %v, want not found", xerr)
	}
	if count, score := f.workerLoad(t); count != 1 || score != 1 {
		t.Errorf("capacity was refunded for a mailbox that was not deleted: account_count=%d load_score=%v", count, score)
	}
	if !f.mailboxExists(t) {
		t.Error("the mailbox was deleted by a caller that does not own it")
	}
}

// The lookup that finds the mailbox is deliberately unscoped, so ownership is
// checked in the service. A teammate's user id must not delete this mailbox or
// publish a removal for it.
func TestLiveDeleteRefusesAMailboxTheCallerDoesNotOwn(t *testing.T) {
	f := newRemovalLiveFixture(t)

	if xerr := f.svc.Delete(context.Background(), uuid.New().String(), f.mailbox.String()); xerr != errx.ErrNotFound {
		t.Fatalf("error = %v, want not found", xerr)
	}
	if len(f.pub.removed) != 0 {
		t.Errorf("published %d removals for someone else's mailbox, want 0", len(f.pub.removed))
	}
	if !f.mailboxExists(t) {
		t.Error("someone else's delete removed the mailbox")
	}
}

// A removal that cannot be published leaves the mailbox exactly as it was:
// still there, still assigned, still counted.
func TestLiveDeleteKeepsEverythingWhenTheWorkerCannotBeTold(t *testing.T) {
	f := newRemovalLiveFixture(t)
	f.pub.removeErr = errBusDown

	xerr := f.svc.Delete(context.Background(), f.user.String(), f.mailbox.String())
	if xerr == nil || xerr.Code != errx.ServiceUnavailable {
		t.Fatalf("error = %v, want a 503 so the client retries", xerr)
	}
	if !f.mailboxExists(t) {
		t.Fatal("the mailbox was deleted even though the worker was never told")
	}
	if count, score := f.workerLoad(t); count != 1 || score != 1 {
		t.Errorf("worker load changed for a mailbox that still exists: account_count=%d load_score=%v", count, score)
	}
}

// A mailbox that has ever been scheduled work carries task rows, and a platform
// admin's warmup enforcement carries its own. Both used to reference the
// mailbox with no delete action, so disconnecting anything that had ever warmed
// up or sent a campaign step raised a foreign key violation: the customer got a
// server error and the mailbox stayed connected. Migration 000098 makes that
// work go with the mailbox it belongs to.
//
// Run cmd/migrate against WARMBLY_TEST_DB first; a database still on the old
// constraint fails this the way production did.
func TestLiveDeletingAMailboxWithScheduledWork(t *testing.T) {
	f := newRemovalLiveFixture(t)
	ctx := context.Background()

	if _, err := f.pool.Exec(ctx,
		`INSERT INTO tasks (task_type, email_account_id, status, message_id, scheduled_at)
		 VALUES ('warmup', $1, 'pending', '', now()), ('campaign', $1, 'completed', '', now())`,
		f.mailbox); err != nil {
		t.Fatalf("fixture task: %v", err)
	}
	if _, err := f.pool.Exec(ctx,
		`INSERT INTO warmup_admin_actions (admin_user_id, email_account_id, action, reason)
		 VALUES ($1, $2, 'block', 'fixture')`, f.user, f.mailbox); err != nil {
		t.Fatalf("fixture admin action: %v", err)
	}

	if xerr := f.svc.Delete(ctx, f.user.String(), f.mailbox.String()); xerr != nil {
		t.Fatalf("disconnecting a mailbox that has scheduled work failed: %v", xerr)
	}
	if f.mailboxExists(t) {
		t.Fatal("the mailbox survived its own delete")
	}

	for _, table := range []string{"tasks", "warmup_admin_actions"} {
		var left int
		if err := f.pool.QueryRow(ctx,
			`SELECT count(*) FROM `+table+` WHERE email_account_id = $1`, f.mailbox).Scan(&left); err != nil {
			t.Fatalf("read %s: %v", table, err)
		}
		if left != 0 {
			t.Errorf("%d %s rows point at a mailbox that no longer exists", left, table)
		}
	}
}
