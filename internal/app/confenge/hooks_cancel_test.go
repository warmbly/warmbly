package confenge

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/confenge/dispatch"
)

func TestCancelQueuedForRecipientEmailAndPhone(t *testing.T) {
	clock := &dispatch.FixedClock{T: time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)}
	store := dispatch.NewMemoryStore()
	cfg := dispatch.DefaultConfig()
	cfg.WindowStart = "00:00"
	cfg.WindowEnd = "23:59"
	cfg.Timezone = "UTC"
	cfg.MinGap = 0
	gov := dispatch.NewGovernor(cfg, store, clock)

	svc := &service{cfg: Config{Enabled: true}, governor: gov}
	org := uuid.New()
	ctx := context.Background()
	email := "lead@example.com"
	phone := "+5511888777666"

	_ = gov.Enqueue(ctx, dispatch.EnqueueRequest{
		OrganizationID: org, Channel: dispatch.ChannelEmail, DraftID: uuid.New(),
		MessageKey: "email:draft:a", RecipientRef: email, DueAt: clock.Now(),
	})
	_ = gov.Enqueue(ctx, dispatch.EnqueueRequest{
		OrganizationID: org, Channel: dispatch.ChannelWhatsApp, DraftID: uuid.New(),
		MessageKey: "wa:draft:b", RecipientRef: phone, DueAt: clock.Now(),
	})

	svc.cancelQueuedForRecipient(ctx, org, email, phone, "DO_NOT_CONTACT")

	if item, _ := gov.ClaimNextQueued(ctx); item != nil {
		t.Fatalf("expected empty queue after dual cancel, got %s", item.MessageKey)
	}
	sent, _ := store.CountSendsSince(ctx, clock.Now().Add(-dispatch.RollingWindow))
	if sent != 0 {
		t.Fatalf("cancel must not consume success slots, got %d", sent)
	}
}
