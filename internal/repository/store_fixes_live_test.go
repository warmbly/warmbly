package repository

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// Concurrent debits under one key take the money once and every caller gets the same transaction.
func TestLiveStoreMoneyConcurrentSameKeyDebitReplays(t *testing.T) {
	f := newStoreMoneyFixture(t)
	ctx := context.Background()
	if _, _, err := f.credits.GrantPurchased(ctx, f.org, 50*usd1, "top_up", "invoice:in_race"); err != nil {
		t.Fatal(err)
	}
	key := "store:order:" + uuid.NewString() + ":0"
	var wg sync.WaitGroup
	ids := make([]uuid.UUID, 8)
	for i := range ids {
		wg.Go(func() {
			_, txn, _, err := f.credits.Debit(ctx, f.org, storeDebit(5*usd1, key))
			if err != nil {
				t.Errorf("debit %d: %v", i, err)
				return
			}
			ids[i] = txn.ID
		})
	}
	wg.Wait()
	for _, id := range ids {
		if id != ids[0] {
			t.Fatalf("debits under one key returned different transactions: %v", ids)
		}
	}
	if l, _ := f.credits.GetBalance(ctx, f.org); l.Purchased != 45*usd1 {
		t.Fatalf("purchased %d, want one debit", l.Purchased)
	}
}

// A workspace holding a running store mailbox cannot be deleted by any path until the mailbox is cancelled.
func TestLiveStoreWorkspaceDeletionWaitsForPurchasesToEnd(t *testing.T) {
	f := newStoreMoneyFixture(t)
	ctx := context.Background()
	m := f.mailbox(f.domain().ID, time.Now().Add(24*time.Hour))
	f.exec(`UPDATE store_domains SET auto_renew = false WHERE organization_id = $1`, f.org)
	dz := NewDangerZoneRepository(f.pool)
	orgs := NewOrganizationRepository(f.pool)
	now := time.Now()
	pending := func(kind models.DeletionResourceType, id uuid.UUID) error {
		return dz.CreatePending(ctx, &models.ScheduledDeletion{ID: uuid.New(), ResourceType: kind, ResourceID: id, OrganizationID: &f.org,
			RequestedByUserID: f.user, ScheduledAt: now, ExecuteAfter: now, GraceDays: 0, Status: models.DeletionStatusPending})
	}
	if err := pending(models.DeletionResourceOrganization, f.org); !errors.Is(err, ErrStorePurchasesActive) {
		t.Fatalf("schedule workspace deletion = %v", err)
	}
	if err := pending(models.DeletionResourceUser, f.user); !errors.Is(err, ErrStorePurchasesActive) {
		t.Fatalf("schedule account deletion = %v", err)
	}
	if _, err := dz.HardDeleteOrganization(ctx, f.org); !errors.Is(err, ErrStorePurchasesActive) {
		t.Fatalf("hard delete workspace = %v", err)
	}
	if _, err := dz.HardDeleteUser(ctx, f.user, nil); !errors.Is(err, ErrStorePurchasesActive) {
		t.Fatalf("hard delete owner = %v", err)
	}
	if err := orgs.Delete(ctx, f.org); !errors.Is(err, ErrStorePurchasesActive) {
		t.Fatalf("delete workspace = %v", err)
	}
	f.exec(`UPDATE store_mailboxes SET status = 'cancelled', cancels_at = now() WHERE id = $1`, m.ID)
	if _, err := dz.HardDeleteOrganization(ctx, f.org); err != nil {
		t.Fatalf("hard delete after the cancellation = %v", err)
	}
}
