package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// A purchase's end is read through its connected mailbox, stamped once, and never connected again.
func TestLiveStoreMailboxEnd(t *testing.T) {
	handle, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 401)
	ctx := context.Background()
	repo := NewStoreRepository(handle)
	user, org, other, acc := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, q, args...); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	exec(`INSERT INTO users (id, first_name, last_name, email) VALUES ($1, 'E', 'F', $2)`, user, uuid.NewString()+"@example.test")
	exec(`INSERT INTO organizations (id, name, owner_user_id) VALUES ($1, 'A', $3), ($2, 'B', $3)`, org, other, user)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = ANY($1)`, []uuid.UUID{org, other})
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, user)
	})
	exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain, signature_html, provider) VALUES ($1,$2,$3,$4,'E','','','gmail')`, acc, user, org, uuid.NewString()+"@example.test")
	exec(`INSERT INTO store_domains (id, organization_id, graphkit_domain_id, name, status) VALUES ($1, $2, $3, 'end.test', 'active')`, acc, org, "dom_"+acc.String())
	exec(`INSERT INTO store_mailboxes (id, organization_id, domain_id, graphkit_mailbox_id, email, provider, status, paid_through, email_account_id, connect_state)
	      VALUES ($1, $2, $1, $3, 'a@end.test', 'google', 'active', now(), $1, 'connected')`, acc, org, "mbx_"+acc.String())

	if m, err := repo.GetMailboxByAccount(ctx, other, acc); err != nil || m != nil {
		t.Fatalf("another workspace read the purchase: %+v %v", m, err)
	}
	if m, err := repo.GetMailboxByAccount(ctx, org, acc); err != nil || m == nil || m.CancelledAt != nil {
		t.Fatalf("purchase = %+v %v", m, err)
	}
	if won, _ := repo.MarkMailboxEnded(ctx, org, acc, time.Now()); won {
		t.Fatal("a live mailbox was marked ended")
	}
	exec(`UPDATE store_mailboxes SET status = 'cancelled' WHERE id = $1`, acc)
	if rows, err := repo.UnmarkedEndedMailboxes(ctx, 1000); err != nil || !hasStoreMailbox(rows, acc) {
		t.Fatalf("unmarked = %d rows, %v", len(rows), err)
	}
	if won, _ := repo.MarkMailboxEnded(ctx, other, acc, time.Now()); won {
		t.Fatal("another workspace marked the purchase")
	}
	if won, err := repo.MarkMailboxEnded(ctx, org, acc, time.Now()); !won || err != nil {
		t.Fatalf("mark = %v %v", won, err)
	}
	if won, _ := repo.MarkMailboxEnded(ctx, org, acc, time.Now()); won {
		t.Fatal("marked twice")
	}
	if rows, _ := repo.UnmarkedEndedMailboxes(ctx, 1000); hasStoreMailbox(rows, acc) {
		t.Fatal("a marked mailbox is still swept")
	}
	// Deleting the history detaches the purchase; even reset to waiting it is never claimed.
	exec(`DELETE FROM email_accounts WHERE id = $1`, acc)
	exec(`UPDATE store_mailboxes SET status = 'active', connect_state = 'waiting' WHERE id = $1`, acc)
	if rows, _ := repo.PendingConnects(ctx, time.Now(), 1000); hasStoreMailbox(rows, acc) {
		t.Fatal("an ended mailbox is offered for connecting")
	}
	if claimed, _ := repo.ClaimConnect(ctx, org, acc, time.Now()); claimed {
		t.Fatal("an ended mailbox was claimed for connecting")
	}
}

func hasStoreMailbox(rows []models.StoreMailbox, id uuid.UUID) bool {
	for _, r := range rows {
		if r.ID == id {
			return true
		}
	}
	return false
}
