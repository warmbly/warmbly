package webhook

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/warmbly/warmbly/internal/repository"
)

func TestLiveDeliveryWorkerWakesOnANotificationFromAnotherProcess(t *testing.T) {
	dsn := os.Getenv("WARMBLY_TEST_DB")
	if dsn == "" {
		t.Skip("WARMBLY_TEST_DB not set")
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	w := NewDeliveryWorker(nil, DeliveryWorkerOptions{})
	connect := func(ctx context.Context) (*pgx.Conn, error) { return pgx.Connect(ctx, dsn) }
	go w.Listen(ctx, connect)

	wait := func(what string) {
		t.Helper()
		select {
		case <-w.wake:
		case <-time.After(5 * time.Second):
			t.Fatalf("no wake for %s", what)
		}
	}
	// The listener wakes once on connecting, for anything enqueued before LISTEN took effect.
	wait("the connected listener")

	other, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close(context.Background())
	if _, err := other.Exec(ctx, `SELECT pg_notify($1, '')`, repository.WebhookDeliveryChannel); err != nil {
		t.Fatal(err)
	}
	wait("a notification sent on another connection")
}
