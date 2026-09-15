package unibox

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/repository"
)

// Live cover for what the relay is built on: that marking read reports only
// the messages that actually changed, and that those resolve to the provider
// handles and the worker holding the mailbox. Skipped unless WARMBLY_TEST_DB
// is set:
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/app/unibox/ -run Live -v
type seenLiveFixture struct {
	pool   *pgxpool.Pool
	repo   repository.UniboxRepository
	user   uuid.UUID
	org    uuid.UUID
	worker uuid.UUID
	box    uuid.UUID
	unread uuid.UUID
	read   uuid.UUID
}

func newSeenLiveFixture(t *testing.T) *seenLiveFixture {
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
	f := &seenLiveFixture{
		pool: handle.Pool, user: uuid.New(), org: uuid.New(),
		worker: uuid.New(), box: uuid.New(), unread: uuid.New(), read: uuid.New(),
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := f.pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	exec(`INSERT INTO users (id, email, first_name, last_name) VALUES ($1, $2, 'Seen', 'Test')`,
		f.user, "seen-"+f.user.String()[:8]+"@test.local")
	exec(`INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Seen Test', $2, $3)`,
		f.org, "seen-"+f.org.String()[:8], f.user)
	exec(`INSERT INTO fleet_nodes (id, role, name, address, active, last_seen_at)
	      VALUES ($1, 'worker', 'seen-test', '127.0.0.1', true, now())`, f.worker)
	exec(`INSERT INTO workers (id, account_count, load_score) VALUES ($1, 0, 0)`, f.worker)
	exec(`INSERT INTO email_accounts (id, user_id, organization_id, worker_id, email, name,
	          signature_plain, signature_html, provider, status, campaign_limit, min_wait_time)
	      VALUES ($1, $2, $3, $4, $5, 'Seen', '', '', 'gmail', 'active', 50, 600)`,
		f.box, f.user, f.org, f.worker, "seen-"+f.box.String()[:8]+"@test.local")
	exec(`INSERT INTO unibox_emails (id, user_id, email_id, gmail_id, uid, folder_path, message_id, seen, folder)
	      VALUES ($1, $2, $3, 'gmail-unread', 11, 'INBOX', '<unread@test>', false, 'inbox')`,
		f.unread, f.user, f.box)
	exec(`INSERT INTO unibox_emails (id, user_id, email_id, gmail_id, uid, folder_path, message_id, seen, folder)
	      VALUES ($1, $2, $3, 'gmail-read', 12, 'INBOX', '<read@test>', true, 'inbox')`,
		f.read, f.user, f.box)

	t.Cleanup(func() {
		c := context.Background()
		for _, step := range []struct {
			sql string
			arg any
		}{
			{`DELETE FROM unibox_emails WHERE email_id = $1`, f.box},
			{`DELETE FROM email_accounts WHERE id = $1`, f.box},
			{`DELETE FROM fleet_nodes WHERE id = $1`, f.worker},
			{`DELETE FROM organizations WHERE id = $1`, f.org},
			{`DELETE FROM users WHERE id = $1`, f.user},
		} {
			if _, err := f.pool.Exec(c, step.sql, step.arg); err != nil {
				t.Errorf("cleanup %q: %v", step.sql, err)
			}
		}
	})

	f.repo = repository.NewUniboxRepository(handle)
	return f
}

func TestLiveMarkSeenReportsOnlyRealChanges(t *testing.T) {
	f := newSeenLiveFixture(t)
	ctx := context.Background()

	// Both are asked for; only the unread one is a change, and re-reading a
	// thread that is already read must cost nothing at the provider.
	changed, err := f.repo.MarkSeenBulk(ctx, f.org, []uuid.UUID{f.unread, f.read}, true)
	if err != nil {
		t.Fatalf("mark seen: %v", err)
	}
	if len(changed) != 1 || changed[0] != f.unread {
		t.Fatalf("expected only the unread message to be reported, got %v", changed)
	}

	// A second pass changes nothing at all.
	changed, err = f.repo.MarkSeenBulk(ctx, f.org, []uuid.UUID{f.unread, f.read}, true)
	if err != nil {
		t.Fatalf("mark seen again: %v", err)
	}
	if len(changed) != 0 {
		t.Errorf("a repeated mark-read reported %d changes", len(changed))
	}

	// Marking unread is a change again, in the other direction.
	changed, err = f.repo.MarkSeenBulk(ctx, f.org, []uuid.UUID{f.unread}, false)
	if err != nil || len(changed) != 1 {
		t.Fatalf("mark unread: %v / %v", err, changed)
	}
}

func TestLiveSeenRelayTargets(t *testing.T) {
	f := newSeenLiveFixture(t)
	ctx := context.Background()

	targets, err := f.repo.SeenRelayTargets(ctx, f.org, []uuid.UUID{f.unread, f.read})
	if err != nil {
		t.Fatalf("relay targets: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("expected both messages, got %d", len(targets))
	}
	for _, target := range targets {
		if target.EmailID != f.box || target.WorkerID != f.worker {
			t.Errorf("wrong routing: mailbox=%v worker=%v", target.EmailID, target.WorkerID)
		}
		// All three providers' handles travel; the worker takes what it uses.
		if target.Ref.ProviderID == "" || target.Ref.UID == 0 || target.Ref.Folder != "INBOX" || target.Ref.RFCMessageID == "" {
			t.Errorf("incomplete ref: %+v", target.Ref)
		}
	}

	// A mailbox with no worker has nothing to relay through and must not
	// produce a target keyed on a nil worker.
	if _, err := f.pool.Exec(ctx, `UPDATE email_accounts SET worker_id = NULL WHERE id = $1`, f.box); err != nil {
		t.Fatalf("unassign worker: %v", err)
	}
	targets, err = f.repo.SeenRelayTargets(ctx, f.org, []uuid.UUID{f.unread, f.read})
	if err != nil {
		t.Fatalf("relay targets without a worker: %v", err)
	}
	if len(targets) != 0 {
		t.Errorf("expected no targets for an unplaced mailbox, got %d", len(targets))
	}
}
