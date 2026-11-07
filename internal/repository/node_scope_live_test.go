package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

// A worker's scope is the mailboxes assigned to it plus, for a bounded grace,
// those it was moved off; the trigger records every move whatever path makes it.
//
//	WARMBLY_TEST_DB=postgres://... go test ./internal/repository/ -run LiveNodeScope -v
func TestLiveNodeScopeFollowsAssignment(t *testing.T) {
	handle, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 337)
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture %q: %v", sql[:min(60, len(sql))], err)
		}
	}
	user, org, other, mailbox, otherMailbox, campaign := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	workerA, workerB, consumer := uuid.New(), uuid.New(), uuid.New()
	tag := org.String()[:8]
	exec(`INSERT INTO users (id, first_name, last_name, email, password_hash) VALUES ($1, 'Scope', 'Live', $2, 'x')`, user, "scope-"+tag+"@fixture.invalid")
	for i, o := range []uuid.UUID{org, other} {
		exec(`INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Scope', $2, $3)`, o, "scope-"+tag+"-"+string(rune('a'+i)), user)
		exec(`INSERT INTO organization_encrypted_keys (organization_id, encrypted_data_key) VALUES ($1, $2)`, o, "sealed-"+o.String())
	}
	for _, w := range []uuid.UUID{workerA, workerB} {
		exec(`INSERT INTO fleet_nodes (id, role, active, last_seen_at) VALUES ($1, 'worker', true, now())`, w)
		exec(`INSERT INTO workers (id) VALUES ($1)`, w)
	}
	exec(`INSERT INTO fleet_nodes (id, role, active, last_seen_at) VALUES ($1, 'consumer', true, now())`, consumer)
	exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain, signature_html, provider, worker_id)
	      VALUES ($1, $2, $3, $4, 'Scope', '', '', 'smtp_imap', $5)`, mailbox, user, org, "scope-"+tag+"-a@fixture.invalid", workerA)
	exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain, signature_html, provider, worker_id)
	      VALUES ($1, $2, $3, $4, 'Scope', '', '', 'smtp_imap', $5)`, otherMailbox, user, other, "scope-"+tag+"-b@fixture.invalid", workerB)
	exec(`INSERT INTO campaigns (id, user_id, organization_id, name, description, days, status, updated_at, created_at)
	      VALUES ($1, $2, $3, 'Scope', '', 62, 'active', NOW(), NOW())`, campaign, user, org)
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM campaigns WHERE id = $1`, campaign)
		_, _ = pool.Exec(c, `DELETE FROM email_accounts WHERE id = ANY($1)`, []uuid.UUID{mailbox, otherMailbox})
		_, _ = pool.Exec(c, `DELETE FROM workers WHERE id = ANY($1)`, []uuid.UUID{workerA, workerB})
		_, _ = pool.Exec(c, `DELETE FROM fleet_nodes WHERE id = ANY($1)`, []uuid.UUID{workerA, workerB, consumer})
		_, _ = pool.Exec(c, `DELETE FROM organization_encrypted_keys WHERE organization_id = ANY($1)`, []uuid.UUID{org, other})
		_, _ = pool.Exec(c, `DELETE FROM organizations WHERE id = ANY($1)`, []uuid.UUID{org, other})
		_, _ = pool.Exec(c, `DELETE FROM users WHERE id = $1`, user)
	})
	repo := NewNodeScopeRepository(handle)
	grace := 30 * time.Minute

	holds := func(worker, user, box uuid.UUID) bool {
		t.Helper()
		ok, err := repo.WorkerHoldsMailbox(ctx, worker, user, box, grace)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	holdsOrg := func(worker, o uuid.UUID) bool {
		t.Helper()
		ok, err := repo.WorkerHoldsOrganization(ctx, worker, o, grace)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}

	if role, found, err := repo.NodeRole(ctx, consumer); err != nil || !found || role != models.NodeRoleConsumer {
		t.Fatalf("consumer role: %q %v %v", role, found, err)
	}
	if !holds(workerA, user, mailbox) || !holds(workerA, uuid.Nil, mailbox) || !holdsOrg(workerA, org) {
		t.Fatal("an assigned mailbox and its organization are in scope")
	}
	if holds(workerA, user, otherMailbox) || holdsOrg(workerA, other) || holds(workerA, uuid.New(), mailbox) {
		t.Fatal("another worker's mailbox, its organization, or a mailbox named under the wrong user are out of scope")
	}
	boxes, err := repo.WorkerMailboxes(ctx, workerA, grace)
	if err != nil || len(boxes) != 1 || boxes[0].ID != mailbox || boxes[0].UserID != user || *boxes[0].OrganizationID != org {
		t.Fatalf("worker A's mailboxes: %+v %v", boxes, err)
	}
	keys, err := repo.OrganizationDataKeys(ctx, []uuid.UUID{org})
	if err != nil || len(keys) != 1 || keys[0] != "sealed-"+org.String() {
		t.Fatalf("data keys: %v %v", keys, err)
	}
	if got, found, err := repo.DataKeyOrganization(ctx, "sealed-"+other.String()); err != nil || !found || got != other {
		t.Fatalf("data key organization: %v %v %v", got, found, err)
	}
	if got, found, err := repo.CampaignOrganization(ctx, campaign); err != nil || !found || got != org {
		t.Fatalf("campaign organization: %v %v %v", got, found, err)
	}

	// A move keeps the previous worker in scope for the grace, and the new one at once.
	exec(`UPDATE email_accounts SET worker_id = $1 WHERE id = $2`, workerB, mailbox)
	if !holds(workerA, user, mailbox) || !holdsOrg(workerA, org) || !holds(workerB, user, mailbox) {
		t.Fatal("within the grace both the previous and the new worker hold the moved mailbox")
	}
	boxes, err = repo.WorkerMailboxes(ctx, workerA, grace)
	if err != nil || len(boxes) != 1 {
		t.Fatalf("the previous worker lists the moved mailbox during the grace: %+v %v", boxes, err)
	}
	exec(`UPDATE email_accounts SET worker_released_at = now() - interval '31 minutes' WHERE id = $1`, mailbox)
	if holds(workerA, user, mailbox) || holdsOrg(workerA, org) {
		t.Fatal("past the grace the previous worker no longer holds the mailbox")
	}
	boxes, err = repo.WorkerMailboxes(ctx, workerA, grace)
	if err != nil || len(boxes) != 0 {
		t.Fatalf("past the grace the previous worker lists nothing: %+v %v", boxes, err)
	}

	// An update that leaves the worker alone records no move.
	exec(`UPDATE email_accounts SET name = 'Renamed' WHERE id = $1`, mailbox)
	var prev *uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT previous_worker_id FROM email_accounts WHERE id = $1`, mailbox).Scan(&prev); err != nil || prev == nil || *prev != workerA {
		t.Fatalf("previous worker after an unrelated update: %v %v", prev, err)
	}
}
