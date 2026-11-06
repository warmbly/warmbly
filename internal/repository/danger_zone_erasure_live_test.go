package repository

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/infrastructure/db"
)

// Live cover for deleting a workspace and deleting a person. Skipped unless
// WARMBLY_TEST_DB is set:
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run Live -v
//
// Both paths were one DELETE against a parent row, documented as relying on
// cascades. Only a real schema can say whether those cascades exist, and for
// the workspace four of them did not.

type dangerLiveFixture struct {
	handle  *db.DB
	repo    DangerZoneRepository
	user    uuid.UUID
	org     uuid.UUID
	mailbox uuid.UUID
	worker  uuid.UUID
}

func newDangerLiveFixture(t *testing.T) *dangerLiveFixture {
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

	f := &dangerLiveFixture{
		handle:  handle,
		repo:    NewDangerZoneRepository(handle.Pool),
		user:    uuid.New(),
		org:     uuid.New(),
		mailbox: uuid.New(),
		worker:  uuid.New(),
	}
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := handle.Pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture %q: %v", sql, err)
		}
	}
	exec(`INSERT INTO users (id, email, first_name, last_name) VALUES ($1, $2, 'Danger', 'Test')`,
		f.user, "danger-"+f.user.String()[:8]+"@test.local")
	exec(`INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Danger Test', $2, $3)`,
		f.org, "danger-"+f.org.String()[:8], f.user)
	// The mailbox is placed on a worker, because that is the state the delete
	// has to report back: an unassigned one exercises none of it.
	exec(`INSERT INTO fleet_nodes (id, role, active, last_seen_at) VALUES ($1, 'worker', true, now())`, f.worker)
	exec(`INSERT INTO workers (id) VALUES ($1)`, f.worker)
	exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name,
	          signature_plain, signature_html, provider, status, campaign_limit, min_wait_time, worker_id)
	      VALUES ($1, $2, $3, $4, 'Danger', '', '', 'gmail', 'active', 50, 600, $5)`,
		f.mailbox, f.user, f.org, "danger-"+f.mailbox.String()[:8]+"@test.local", f.worker)
	exec(`INSERT INTO email_accounts_oauth (email_account_id, access_token, refresh_token, expires_at)
	      VALUES ($1, 'sealed-access', 'sealed-refresh', now() + interval '1 hour')`, f.mailbox)

	t.Cleanup(func() {
		c := context.Background()
		for _, step := range []struct {
			sql string
			arg any
		}{
			{`DELETE FROM mailbox_erasures WHERE email_account_id = $1`, f.mailbox},
			{`DELETE FROM email_accounts WHERE id = $1`, f.mailbox},
			{`DELETE FROM organizations WHERE id = $1`, f.org},
			{`DELETE FROM users WHERE id = $1`, f.user},
			// workers.id is a foreign key onto fleet_nodes.id, so in this order.
			{`DELETE FROM workers WHERE id = $1`, f.worker},
			{`DELETE FROM fleet_nodes WHERE id = $1`, f.worker},
		} {
			if _, err := handle.Pool.Exec(c, step.sql, step.arg); err != nil {
				t.Errorf("cleanup %q: %v", step.sql, err)
			}
		}
	})
	return f
}

func (f *dangerLiveFixture) count(t *testing.T, sql string, arg any) int {
	t.Helper()
	var n int
	if err := f.handle.Pool.QueryRow(context.Background(), sql, arg).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// The defect: four foreign keys into organizations had no delete action, so
// deleting a workspace raised a foreign key violation for any workspace that
// had ever held a mailbox, a campaign, a contact or a sequence. A scheduled
// workspace deletion ran its grace period, mailed its warnings, and then failed
// every tick forever with the workspace still there.
func TestLiveDeletingAWorkspaceSucceeds(t *testing.T) {
	f := newDangerLiveFixture(t)

	placements, err := f.repo.HardDeleteOrganization(context.Background(), f.org)
	if err != nil {
		t.Fatalf("a workspace with one mailbox could not be deleted: %v", err)
	}
	// The assignment dies with the row, so it has to come back out of the
	// delete: without it nothing can tell the worker to stop executing a
	// mailbox that no longer exists.
	if len(placements) != 1 || placements[0].EmailID != f.mailbox || placements[0].WorkerID != f.worker {
		t.Errorf("returned %+v, want the mailbox on worker %s so it can be evicted from it", placements, f.worker)
	}
	if n := f.count(t, `SELECT count(*) FROM organizations WHERE id = $1`, f.org); n != 0 {
		t.Error("the workspace is still there")
	}
	if n := f.count(t, `SELECT count(*) FROM email_accounts WHERE id = $1`, f.mailbox); n != 0 {
		t.Error("the workspace's mailbox outlived the workspace")
	}
}

// Deleting the workspace must hand its mailboxes' grants back too. Without
// this, a customer who closes their account leaves Warmbly connected to their
// Gmail with nothing left in the database that knows it.
func TestLiveDeletingAWorkspaceQueuesItsMailboxesForErasure(t *testing.T) {
	f := newDangerLiveFixture(t)

	if _, err := f.repo.HardDeleteOrganization(context.Background(), f.org); err != nil {
		t.Fatalf("delete workspace: %v", err)
	}

	var token, prefix string
	err := f.handle.Pool.QueryRow(context.Background(),
		`SELECT refresh_token, blob_prefix FROM mailbox_erasures WHERE email_account_id = $1`, f.mailbox).
		Scan(&token, &prefix)
	if err != nil {
		t.Fatalf("nothing queued for the workspace's mailbox: %v", err)
	}
	if token != "sealed-refresh" {
		t.Errorf("refresh token = %q, want it copied off the mailbox before the cascade", token)
	}
	if prefix == "" {
		t.Error("no blob prefix recorded, so the workspace's mail stays in the bucket")
	}
}

// A workspace whose mailboxes are in a warmup pool with a standing on record.
// Deleting it has to unwind organizations -> email_accounts ->
// warmup_pool_participants alongside warmup_reputation_ledger, which hangs off
// the organization by its own foreign key. The other workspace tests use a bare
// mailbox and never build that state, so nothing else here would notice if one
// of those links stopped resolving; a foreign key that does not aborts the
// whole deletion rather than losing a row.
//
// The mirror trigger fires on insert and update, not delete, so the cascade
// does not run it. This covers the cascade and the ledger's own key, not the
// trigger's behaviour during a delete.
func TestLiveDeletingAWorkspaceWithWarmupStandingSucceeds(t *testing.T) {
	f := newDangerLiveFixture(t)
	ctx := context.Background()

	// The free pool, seeded under a fixed id on every instance.
	const freePool = "77777777-aaaa-0000-0000-000000000001"
	if _, err := f.handle.Pool.Exec(ctx, `
		INSERT INTO warmup_pool_participants (pool_id, email_account_id, health_state, blocked_at, blocked_until, blocked_reason)
		VALUES ($1, $2, 'blocked', now(), now() + interval '30 days', 'live test')`, freePool, f.mailbox); err != nil {
		t.Fatalf("fixture participant: %v", err)
	}

	// The mirror writes the standing against the address; assert it is there,
	// or the test would pass without having exercised anything.
	var mirrored int
	if err := f.handle.Pool.QueryRow(ctx,
		`SELECT count(*) FROM warmup_reputation_ledger WHERE organization_id = $1`, f.org).Scan(&mirrored); err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	if mirrored != 1 {
		t.Fatalf("the reputation mirror wrote %d rows, want 1: this test is not exercising the trigger", mirrored)
	}

	if _, err := f.repo.HardDeleteOrganization(ctx, f.org); err != nil {
		t.Fatalf("a workspace with a penalised mailbox could not be deleted: %v", err)
	}
	if n := f.count(t, `SELECT count(*) FROM organizations WHERE id = $1`, f.org); n != 0 {
		t.Error("the workspace is still there")
	}
	if n := f.count(t, `SELECT count(*) FROM warmup_reputation_ledger WHERE organization_id = $1`, f.org); n != 0 {
		t.Error("the workspace's reputation ledger outlived the workspace")
	}
	// And the erasure is still queued: the warmup rows must not have displaced it.
	if n := f.count(t, `SELECT count(*) FROM mailbox_erasures WHERE email_account_id = $1`, f.mailbox); n != 1 {
		t.Errorf("queued %d erasures, want 1", n)
	}
}

// Closing an account removes the person and every workspace they own, queues
// the owned mailboxes for erasure, and leaves what they wrote in a workspace
// they did not own with that workspace, naming nobody. The request row stays as
// the record that the account was closed.
func TestLiveDeletingAnAccountRemovesOwnedWorkspacesAndKeepsSharedWork(t *testing.T) {
	f := newDangerLiveFixture(t)
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := f.handle.Pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture %q: %v", sql, err)
		}
	}

	request := uuid.New()
	exec(`INSERT INTO scheduled_deletions (id, resource_type, resource_id, requested_by_user_id,
	          scheduled_at, execute_after, grace_days, status)
	      VALUES ($1, 'user', $2, $2, now(), now(), 30, 'executing')`, request, f.user)

	// Someone else's workspace, where the person being deleted wrote a note.
	other, otherOrg, contact, note := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	exec(`INSERT INTO users (id, email, first_name, last_name) VALUES ($1, $2, 'Other', 'Owner')`,
		other, "danger-"+other.String()[:8]+"@test.local")
	exec(`INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Other', $2, $3)`,
		otherOrg, "danger-"+otherOrg.String()[:8], other)
	exec(`INSERT INTO organization_members (organization_id, user_id) VALUES ($1, $2)`, otherOrg, f.user)
	exec(`INSERT INTO contacts (id, organization_id, user_id, email, first_name, last_name, company, phone, custom_fields)
	      VALUES ($1, $2, $3, 'lead@test.local', '', '', '', '', '{}')`, contact, otherOrg, other)
	exec(`INSERT INTO contact_notes (id, organization_id, contact_id, user_id, content) VALUES ($1, $2, $3, $4, 'kept')`,
		note, otherOrg, contact, f.user)
	t.Cleanup(func() {
		c := context.Background()
		for _, step := range []struct {
			sql string
			arg any
		}{
			{`DELETE FROM scheduled_deletions WHERE id = $1`, request},
			{`DELETE FROM organizations WHERE id = $1`, otherOrg},
			{`DELETE FROM users WHERE id = $1`, other},
		} {
			if _, err := f.handle.Pool.Exec(c, step.sql, step.arg); err != nil {
				t.Errorf("cleanup %q: %v", step.sql, err)
			}
		}
	})

	placements, err := f.repo.HardDeleteUser(ctx, f.user)
	if err != nil {
		t.Fatalf("the account could not be deleted: %v", err)
	}
	if len(placements) != 1 || placements[0].EmailID != f.mailbox || placements[0].WorkerID != f.worker {
		t.Errorf("returned %+v, want the owned workspace's mailbox on worker %s", placements, f.worker)
	}
	if n := f.count(t, `SELECT count(*) FROM users WHERE id = $1`, f.user); n != 0 {
		t.Error("the user is still there")
	}
	if n := f.count(t, `SELECT count(*) FROM organizations WHERE id = $1`, f.org); n != 0 {
		t.Error("the workspace the user owned outlived them")
	}
	if n := f.count(t, `SELECT count(*) FROM mailbox_erasures WHERE email_account_id = $1`, f.mailbox); n != 1 {
		t.Errorf("queued %d erasures for the owned workspace's mailbox, want 1", n)
	}
	if n := f.count(t, `SELECT count(*) FROM contact_notes WHERE id = $1 AND user_id IS NULL`, note); n != 1 {
		t.Error("the note written in another workspace was not kept with its author cleared")
	}
	if n := f.count(t, `SELECT count(*) FROM scheduled_deletions WHERE id = $1 AND requested_by_user_id IS NULL`, request); n != 1 {
		t.Error("the deletion request did not survive as the record of the closure")
	}
}
