package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// Every mailbox read the dashboard uses says whether the mailbox was bought in the store.
func TestLiveMailboxReadsCarryPurchased(t *testing.T) {
	handle, pool := liveContactDB(t)
	ctx := context.Background()
	user, org, acc := uuid.New(), uuid.New(), uuid.New()
	exec := func(q string, args ...any) {
		if _, err := pool.Exec(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO users (id, first_name, last_name, email) VALUES ($1, 'P', 'Q', $2)`, user, uuid.NewString()+"@example.test")
	exec(`INSERT INTO organizations (id, name, owner_user_id) VALUES ($1, 'P', $2)`, org, user)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, org)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, user)
	})
	exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain, signature_html, provider) VALUES ($1,$2,$3,$4,'P','','','smtp_imap')`, acc, user, org, uuid.NewString()+"@example.test")
	repo := NewEmailRepositoryWithSealer(handle, nil)
	exec(`INSERT INTO store_domains (id, organization_id, graphkit_domain_id, name, status) VALUES ($1, $2, $3, 'x.com', 'active')`, acc, org, "dom_"+acc.String())
	exec(`INSERT INTO store_mailboxes (id, organization_id, domain_id, graphkit_mailbox_id, email, provider, status, paid_through, email_account_id)
	      VALUES ($1, $2, $1, $3, 'a@x.com', 'google', 'active', now(), $1)`, acc, org, "mbx_"+acc.String())
	e, xerr := repo.GetByID(ctx, acc)
	if xerr != nil || e == nil || !e.Purchased {
		t.Fatalf("GetByID = %+v %v", e, xerr)
	}
	if e, xerr := repo.Get(ctx, org.String(), acc.String()); xerr != nil || !e.Purchased {
		t.Fatalf("Get: %v", xerr)
	}
	name := "Renamed"
	if e, xerr := repo.Update(ctx, org.String(), acc.String(), &models.UpdateEmail{Name: &name}); xerr != nil || !e.Purchased {
		t.Fatalf("Update: %+v %v", e, xerr)
	}
	if res, xerr := repo.Search(ctx, org.String(), "", nil, nil, 10, nil); xerr != nil || len(res.Data) == 0 || !res.Data[0].Purchased {
		t.Fatalf("Search: %+v %v", res, xerr)
	}
	scope := NewAccountScope(&org)
	if _, xerr := repo.GetByTags(ctx, scope, []string{uuid.NewString()}); xerr != nil {
		t.Fatalf("GetByTags: %v", xerr)
	}
	if _, xerr := repo.GetAllActiveInScope(ctx, scope); xerr != nil {
		t.Fatalf("GetAllActiveInScope: %v", xerr)
	}
	if _, xerr := repo.GetByCampaignSenders(ctx, scope, uuid.New()); xerr != nil {
		t.Fatalf("GetByCampaignSenders: %v", xerr)
	}
	if e.PurchaseCancelledAt != nil {
		t.Fatalf("a live purchase reads as cancelled at %v", e.PurchaseCancelledAt)
	}
	exec(`UPDATE store_mailboxes SET status = 'cancelled', cancels_at = now() WHERE id = $1`, acc)
	if e, xerr := repo.Get(ctx, org.String(), acc.String()); xerr != nil || !e.Purchased || e.PurchaseCancelledAt == nil {
		t.Fatalf("Get after cancellation = %+v %v", e, xerr)
	}
}
