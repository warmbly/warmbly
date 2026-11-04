package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/encrypt"
)

func TestLiveWebhookEnqueueNotifiesAndStoresTheDelivery(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newSharedOrgFixture(t, pool)
	ctx := t.Context()
	enc, err := encrypt.NewEncrypter(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	repo := NewWebhookRepositorySealed(pool, enc)
	endpoint := &models.WebhookEndpoint{OrganizationID: f.org, URL: "https://example.com/hooks/notify", Enabled: true}
	if err := repo.CreateEndpoint(ctx, endpoint, "secret", "token"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM webhook_endpoints WHERE id = $1`, endpoint.ID)
	})

	listener, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = listener.Exec(context.Background(), "UNLISTEN *")
		listener.Release()
	}()
	if _, err := listener.Exec(ctx, "LISTEN "+WebhookDeliveryChannel); err != nil {
		t.Fatal(err)
	}

	lastDeliveryNotify.Store(0)
	delivery := &models.WebhookDelivery{EndpointID: endpoint.ID, OrganizationID: f.org, EventType: "test.notify", EventID: uuid.New(), Payload: []byte(`{}`)}
	if err := repo.EnqueueDelivery(ctx, delivery); err != nil {
		t.Fatal(err)
	}
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	n, err := listener.Conn().WaitForNotification(waitCtx)
	if err != nil {
		t.Fatalf("an enqueued delivery woke no listener: %v", err)
	}
	if n.Channel != WebhookDeliveryChannel {
		t.Fatalf("notified on %q", n.Channel)
	}
	var queued int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM webhook_deliveries WHERE id = $1`, delivery.ID).Scan(&queued); err != nil || queued != 1 {
		t.Fatalf("the notifying enqueue stored %d rows: %v", queued, err)
	}
}
