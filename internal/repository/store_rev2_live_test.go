package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

func TestLiveStoreRevisionTwoStatements(t *testing.T) {
	handle, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 401)
	ctx := context.Background()
	repo := NewStoreRepository(handle)
	user, org := uuid.New(), uuid.New()
	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, q, args...); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	mustExec(`INSERT INTO users (id, first_name, last_name, email) VALUES ($1, 'Store', 'Test', $2)`, user, "store-"+uuid.NewString()+"@example.test")
	mustExec(`INSERT INTO organizations (id, name, owner_user_id) VALUES ($1, 'Store R2', $2)`, org, user)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, org)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, user)
	})
	if err := repo.UpsertAccount(ctx, org, "acct_r2", nil); err != nil {
		t.Fatal(err)
	}

	// The plan notice is a compare-and-set: stamped once, cleared once.
	now := time.Now()
	for i, want := range []bool{true, false} {
		if won, err := repo.SetPlanNotice(ctx, org, &now); err != nil || won != want {
			t.Fatalf("stamp %d = %v, %v", i, won, err)
		}
	}
	if won, _ := repo.SetPlanNotice(ctx, org, nil); !won {
		t.Fatal("clearing the notice did not change the row")
	}

	// UpdateOrderState hands back the status it replaced, so one caller sees the transition.
	quote := &models.StoreQuote{ID: uuid.New(), OrganizationID: org, GraphKitQuoteID: "qt_r2", Snapshot: json.RawMessage(`{}`), ExpiresAt: now.Add(time.Minute)}
	if err := repo.CreateQuote(ctx, quote); err != nil {
		t.Fatal(err)
	}
	order := &models.StoreOrder{ID: uuid.New(), OrganizationID: org, QuoteID: quote.ID, Status: models.StoreOrderProcessing,
		Quote: json.RawMessage(`{}`), ChargeKey: "store:order:" + uuid.NewString()}
	if _, _, err := repo.CreateOrder(ctx, order); err != nil {
		t.Fatal(err)
	}
	if before, err := repo.UpdateOrderState(ctx, org, order.ID, models.StoreOrderCompleted, json.RawMessage(`[]`), &now); err != nil || before != models.StoreOrderProcessing {
		t.Fatalf("first update replaced %q, %v", before, err)
	}
	if before, _ := repo.UpdateOrderState(ctx, org, order.ID, models.StoreOrderCompleted, json.RawMessage(`[]`), &now); before != models.StoreOrderCompleted {
		t.Fatalf("second update replaced %q, want completed", before)
	}

	// A connected domain goes only while it has no live mailbox; a purchased one never by this path.
	conn, err := repo.UpsertDomain(ctx, &models.StoreDomain{ID: uuid.New(), OrganizationID: org, GraphKitDomainID: "dom_r2c_" + uuid.NewString(),
		Name: "r2-own.test", Status: "active", Source: models.StoreDomainConnected, Details: json.RawMessage(`{"dns_host":"graphkit"}`)})
	if err != nil || conn.Source != models.StoreDomainConnected || conn.AutoRenew {
		t.Fatalf("connected domain = %+v, %v", conn, err)
	}
	bought, err := repo.UpsertDomain(ctx, &models.StoreDomain{ID: uuid.New(), OrganizationID: org, GraphKitDomainID: "dom_r2p_" + uuid.NewString(),
		Name: "r2-bought.test", Status: "active"})
	if err != nil || bought.Source != models.StoreDomainPurchased || !bought.AutoRenew {
		t.Fatalf("purchased domain = %+v, %v", bought, err)
	}
	box, err := repo.UpsertMailbox(ctx, &models.StoreMailbox{ID: uuid.New(), OrganizationID: org, DomainID: conn.ID, GraphKitMailboxID: "mbx_r2_" + uuid.NewString(),
		Email: "ada@r2-own.test", Provider: "google", Status: "active", IsAdmin: true, PaidThrough: now.Add(24 * time.Hour)})
	if err != nil || !box.IsAdmin {
		t.Fatalf("mailbox = %+v, %v", box, err)
	}
	if admin, _ := repo.DomainAdminEmail(ctx, org, conn.ID); admin != "ada@r2-own.test" {
		t.Fatalf("admin = %q", admin)
	}
	if held, _ := repo.HasLiveMailboxes(ctx, org); !held {
		t.Fatal("a live purchased mailbox is not seen")
	}
	if gone, _ := repo.DeleteConnectedDomain(ctx, org, conn.ID); gone {
		t.Fatal("a connected domain with a live mailbox was removed")
	}
	if gone, _ := repo.DeleteConnectedDomain(ctx, org, bought.ID); gone {
		t.Fatal("a purchased domain was removed")
	}
	if err := repo.SetMailboxCancel(ctx, org, box.ID, "cancelled", &now); err != nil {
		t.Fatal(err)
	}
	if gone, err := repo.DeleteConnectedDomain(ctx, org, conn.ID); err != nil || !gone {
		t.Fatalf("removing the emptied connected domain = %v, %v", gone, err)
	}
}
