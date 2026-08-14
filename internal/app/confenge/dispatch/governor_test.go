package dispatch

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func testCfg() Config {
	cfg := DefaultConfig()
	// Wide window so wall-clock timezone does not block tests.
	cfg.WindowStart = "00:00"
	cfg.WindowEnd = "23:59"
	cfg.Timezone = "UTC"
	cfg.MinGap = 0 // tests control gap explicitly when needed
	cfg.SendsPerHour = 10
	cfg.LeaseTTL = 5 * time.Minute
	return cfg
}

func newTestGov(t *testing.T, clock *FixedClock) (*Governor, *MemoryStore) {
	t.Helper()
	store := NewMemoryStore()
	g := NewGovernor(testCfg(), store, clock)
	return g, store
}

func reserveAndCommit(t *testing.T, g *Governor, org uuid.UUID, channel, key string) {
	t.Helper()
	ctx := context.Background()
	res, err := g.TryReserve(ctx, ReserveRequest{
		OrganizationID: org, Channel: channel, MessageKey: key,
	})
	if err != nil {
		t.Fatalf("reserve %s: %v", key, err)
	}
	if !res.Allowed {
		t.Fatalf("reserve %s denied: %s", key, res.Reason)
	}
	if res.AlreadyCommitted {
		return
	}
	if err := g.Commit(ctx, res.Reservation.ID); err != nil {
		t.Fatalf("commit %s: %v", key, err)
	}
}

func TestCommitByMessageKeyWaitsForProviderConfirmation(t *testing.T) {
	clock := &FixedClock{T: time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)}
	governor, store := newTestGov(t, clock)
	key := "email:campaign:provider-confirmed"
	result, err := governor.TryReserve(context.Background(), ReserveRequest{OrganizationID: uuid.New(), Channel: ChannelEmail, MessageKey: key})
	if err != nil || !result.Allowed {
		t.Fatalf("reserve: allowed=%v err=%v", result.Allowed, err)
	}
	if _, sent, err := store.GetSendByKey(context.Background(), key); err != nil || sent {
		t.Fatalf("reservation must not count as provider-confirmed: sent=%v err=%v", sent, err)
	}
	if err := governor.CommitByMessageKey(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	if _, sent, err := store.GetSendByKey(context.Background(), key); err != nil || !sent {
		t.Fatalf("provider confirmation must commit send: sent=%v err=%v", sent, err)
	}
}

func TestCap10Blocks11th(t *testing.T) {
	clock := &FixedClock{T: time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)}
	g, _ := newTestGov(t, clock)
	org := uuid.New()
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		reserveAndCommit(t, g, org, ChannelEmail, fmt.Sprintf("email:draft:%d", i))
		clock.Advance(time.Second) // distinct timestamps
	}
	res, err := g.TryReserve(ctx, ReserveRequest{
		OrganizationID: org, Channel: ChannelEmail, MessageKey: "email:draft:11",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Allowed {
		t.Fatal("11th send must be blocked")
	}
	if res.Reason != "cap_reached" {
		t.Fatalf("reason=%s want cap_reached", res.Reason)
	}
}

func TestSlotAfterRollingWindow(t *testing.T) {
	clock := &FixedClock{T: time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)}
	g, _ := newTestGov(t, clock)
	org := uuid.New()
	ctx := context.Background()
	start := clock.Now()
	for i := 0; i < 10; i++ {
		reserveAndCommit(t, g, org, ChannelEmail, fmt.Sprintf("k:%d", i))
		clock.Advance(time.Second)
	}
	// Exactly after 60m from first send, first slot ages out.
	clock.Set(start.Add(RollingWindow).Add(time.Second))
	res, err := g.TryReserve(ctx, ReserveRequest{
		OrganizationID: org, Channel: ChannelWhatsApp, MessageKey: "new-slot",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Allowed {
		t.Fatalf("expected new slot after window, got %s next=%v", res.Reason, res.NextSlot)
	}
}

func TestEmailAndWhatsAppShareCounter(t *testing.T) {
	clock := &FixedClock{T: time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)}
	g, _ := newTestGov(t, clock)
	org := uuid.New()
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		reserveAndCommit(t, g, org, ChannelEmail, fmt.Sprintf("e:%d", i))
		clock.Advance(time.Second)
		reserveAndCommit(t, g, org, ChannelWhatsApp, fmt.Sprintf("w:%d", i))
		clock.Advance(time.Second)
	}
	// 10 total used
	res, err := g.TryReserve(ctx, ReserveRequest{
		OrganizationID: org, Channel: ChannelEmail, MessageKey: "e:extra",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Allowed {
		t.Fatal("shared counter should block 11th across channels")
	}
}

func TestRestartNoBurst(t *testing.T) {
	clock := &FixedClock{T: time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)}
	store := NewMemoryStore()
	cfg := testCfg()
	g1 := NewGovernor(cfg, store, clock)
	org := uuid.New()
	ctx := context.Background()

	// Queue 20 overdue approved items (simulates downtime backlog).
	for i := 0; i < 20; i++ {
		did := uuid.New()
		_ = g1.Enqueue(ctx, EnqueueRequest{
			OrganizationID: org, Channel: ChannelEmail, DraftID: did,
			MessageKey: fmt.Sprintf("backlog:%d", i),
			DueAt:      clock.Now().Add(-2 * time.Hour),
		})
	}
	// "Restart": new governor instance, same store.
	g2 := NewGovernor(cfg, store, clock)
	committed := 0
	for i := 0; i < 20; i++ {
		item, err := g2.ClaimNextQueued(ctx)
		if err != nil || item == nil {
			break
		}
		res, err := g2.TryReserve(ctx, ReserveRequest{
			OrganizationID: item.OrganizationID, Channel: item.Channel, MessageKey: item.MessageKey, DraftID: &item.DraftID,
		})
		if err != nil {
			t.Fatal(err)
		}
		if !res.Allowed {
			// Re-queue for future slot — claimed items must not free-send on denial.
			due := res.NextSlot
			if due.IsZero() {
				due = clock.Now().Add(time.Minute)
			}
			_ = g2.Enqueue(ctx, EnqueueRequest{
				OrganizationID: item.OrganizationID, Channel: item.Channel, DraftID: item.DraftID,
				MessageKey: item.MessageKey, DueAt: due,
			})
			break
		}
		if err := g2.Commit(ctx, res.Reservation.ID); err != nil {
			t.Fatal(err)
		}
		_ = g2.MarkQueue(ctx, item.ID, QueueSent, "")
		committed++
		clock.Advance(time.Second)
	}
	if committed > 10 {
		t.Fatalf("restart burst: committed %d > 10", committed)
	}
	if committed != 10 {
		t.Fatalf("expected exactly 10 after catch-up, got %d", committed)
	}
	queued, _ := store.CountQueued(ctx, nil)
	if queued < 9 {
		t.Fatalf("expected remaining backlog re-queued, got %d", queued)
	}
}

func TestTwoInstancesDoNotExceedCap(t *testing.T) {
	clock := &FixedClock{T: time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)}
	store := NewMemoryStore()
	cfg := testCfg()
	g1 := NewGovernor(cfg, store, clock)
	g2 := NewGovernor(cfg, store, clock)
	org := uuid.New()
	ctx := context.Background()

	var okCount int64
	var wg sync.WaitGroup
	// 30 concurrent attempts across two governors
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			gov := g1
			if i%2 == 0 {
				gov = g2
			}
			res, err := gov.TryReserve(ctx, ReserveRequest{
				OrganizationID: org, Channel: ChannelEmail, MessageKey: fmt.Sprintf("c:%d", i),
			})
			if err != nil {
				return
			}
			if res.Allowed && !res.AlreadyCommitted {
				if err := gov.Commit(ctx, res.Reservation.ID); err == nil {
					atomic.AddInt64(&okCount, 1)
				}
			}
		}(i)
	}
	wg.Wait()
	if okCount > 10 {
		t.Fatalf("two instances committed %d > 10", okCount)
	}
	// With MinGap=0 and shared store+mutex, should be exactly 10
	if okCount != 10 {
		t.Fatalf("expected 10 successful commits, got %d", okCount)
	}
}

func TestRetryDoesNotDoubleCount(t *testing.T) {
	clock := &FixedClock{T: time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)}
	g, store := newTestGov(t, clock)
	org := uuid.New()
	ctx := context.Background()
	key := "wa:draft:" + uuid.New().String()

	res, err := g.TryReserve(ctx, ReserveRequest{OrganizationID: org, Channel: ChannelWhatsApp, MessageKey: key})
	if err != nil || !res.Allowed {
		t.Fatalf("first reserve: allowed=%v err=%v", res.Allowed, err)
	}
	if err := g.Commit(ctx, res.Reservation.ID); err != nil {
		t.Fatal(err)
	}
	// Idempotent retry of same message
	res2, err := g.TryReserve(ctx, ReserveRequest{OrganizationID: org, Channel: ChannelWhatsApp, MessageKey: key})
	if err != nil {
		t.Fatal(err)
	}
	if !res2.Allowed || !res2.AlreadyCommitted {
		t.Fatalf("retry should be AlreadyCommitted, got allowed=%v already=%v reason=%s", res2.Allowed, res2.AlreadyCommitted, res2.Reason)
	}
	n, _ := store.CountSendsSince(ctx, clock.Now().Add(-RollingWindow))
	if n != 1 {
		t.Fatalf("send ledger count=%d want 1", n)
	}
}

func TestProviderFailureRetrySameLease(t *testing.T) {
	clock := &FixedClock{T: time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)}
	g, store := newTestGov(t, clock)
	org := uuid.New()
	ctx := context.Background()
	key := "email:draft:fail1"

	res, err := g.TryReserve(ctx, ReserveRequest{OrganizationID: org, Channel: ChannelEmail, MessageKey: key})
	if err != nil || !res.Allowed {
		t.Fatal(err)
	}
	// Soft failure: release without success count
	if err := g.Release(ctx, res.Reservation.ID, "smtp_timeout"); err != nil {
		t.Fatal(err)
	}
	n, _ := store.CountSendsSince(ctx, clock.Now().Add(-RollingWindow))
	if n != 0 {
		t.Fatalf("failed attempt must not count as success, got %d", n)
	}
	// Retry after min gap: with MinGap=0 can reserve again
	res2, err := g.TryReserve(ctx, ReserveRequest{OrganizationID: org, Channel: ChannelEmail, MessageKey: key})
	if err != nil || !res2.Allowed {
		t.Fatalf("retry after release: %+v err=%v", res2, err)
	}
	if res2.AlreadyCommitted {
		t.Fatal("should not be committed yet")
	}
}

func TestPauseBlocksAll(t *testing.T) {
	clock := &FixedClock{T: time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)}
	g, _ := newTestGov(t, clock)
	org := uuid.New()
	ctx := context.Background()
	if err := g.Pause(ctx, "ops_hold", nil); err != nil {
		t.Fatal(err)
	}
	for _, ch := range []string{ChannelEmail, ChannelWhatsApp} {
		res, err := g.TryReserve(ctx, ReserveRequest{
			OrganizationID: org, Channel: ch, MessageKey: "p:" + ch,
		})
		if err != nil {
			t.Fatal(err)
		}
		if res.Allowed {
			t.Fatalf("pause must block %s", ch)
		}
	}
	if err := g.Resume(ctx, nil); err != nil {
		t.Fatal(err)
	}
	res, err := g.TryReserve(ctx, ReserveRequest{
		OrganizationID: org, Channel: ChannelEmail, MessageKey: "after-resume",
	})
	if err != nil || !res.Allowed {
		t.Fatalf("after resume should allow: %+v %v", res, err)
	}
}

func TestDNCCancelsQueueWithoutConsumingSlot(t *testing.T) {
	clock := &FixedClock{T: time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)}
	g, store := newTestGov(t, clock)
	org := uuid.New()
	ctx := context.Background()
	did := uuid.New()
	key := MessageKeyEmail(did)
	_ = g.Enqueue(ctx, EnqueueRequest{
		OrganizationID: org, Channel: ChannelEmail, DraftID: did, MessageKey: key, DueAt: clock.Now(),
	})
	// DNC between approve and slot
	if err := g.CancelQueued(ctx, key, "DO_NOT_CONTACT"); err != nil {
		t.Fatal(err)
	}
	// Should not be dequeued
	item, err := g.ClaimNextQueued(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if item != nil && item.MessageKey == key {
		t.Fatal("DNC item must not be dequeued")
	}
	// And no send was recorded
	n, _ := store.CountSendsSince(ctx, clock.Now().Add(-RollingWindow))
	if n != 0 {
		t.Fatalf("DNC must not consume success slot, got %d", n)
	}
}

func TestCapChangeViaConfig(t *testing.T) {
	clock := &FixedClock{T: time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)}
	g, _ := newTestGov(t, clock)
	org := uuid.New()
	ctx := context.Background()
	// Lower cap to 2
	cfg := g.Config()
	cfg.SendsPerHour = 2
	g.SetConfig(cfg)
	reserveAndCommit(t, g, org, ChannelEmail, "a")
	clock.Advance(time.Second)
	reserveAndCommit(t, g, org, ChannelEmail, "b")
	clock.Advance(time.Second)
	res, err := g.TryReserve(ctx, ReserveRequest{
		OrganizationID: org, Channel: ChannelEmail, MessageKey: "c",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Allowed {
		t.Fatal("cap=2 should block 3rd")
	}
	// Raise cap
	cfg.SendsPerHour = 5
	g.SetConfig(cfg)
	res, err = g.TryReserve(ctx, ReserveRequest{
		OrganizationID: org, Channel: ChannelEmail, MessageKey: "c",
	})
	if err != nil || !res.Allowed {
		t.Fatalf("cap raise should allow: %+v %v", res, err)
	}
}

func TestMinGapBlocks(t *testing.T) {
	clock := &FixedClock{T: time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)}
	store := NewMemoryStore()
	cfg := testCfg()
	cfg.MinGap = 360 * time.Second
	g := NewGovernor(cfg, store, clock)
	org := uuid.New()
	ctx := context.Background()
	reserveAndCommit(t, g, org, ChannelEmail, "g1")
	res, err := g.TryReserve(ctx, ReserveRequest{
		OrganizationID: org, Channel: ChannelWhatsApp, MessageKey: "g2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Allowed {
		t.Fatal("min gap should block immediate second send")
	}
	if res.Reason != "min_gap" {
		t.Fatalf("reason=%s", res.Reason)
	}
	clock.Advance(360 * time.Second)
	res, err = g.TryReserve(ctx, ReserveRequest{
		OrganizationID: org, Channel: ChannelWhatsApp, MessageKey: "g2",
	})
	if err != nil || !res.Allowed {
		t.Fatalf("after min gap should allow: %+v %v", res, err)
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	t.Setenv("CONFENGE_GLOBAL_SENDS_PER_HOUR", "")
	t.Setenv("CONFENGE_MIN_SEND_GAP_SECONDS", "")
	cfg := LoadConfig()
	if cfg.SendsPerHour != 10 {
		t.Fatalf("default cap=%d", cfg.SendsPerHour)
	}
	if cfg.MinGap != 360*time.Second {
		t.Fatalf("default gap=%v", cfg.MinGap)
	}
	if cfg.Timezone != DefaultTimezone {
		t.Fatalf("tz=%s", cfg.Timezone)
	}
	t.Setenv("CONFENGE_GLOBAL_SENDS_PER_HOUR", "3")
	t.Setenv("CONFENGE_MIN_SEND_GAP_SECONDS", "120")
	cfg = LoadConfig()
	if cfg.SendsPerHour != 3 || cfg.MinGap != 120*time.Second {
		t.Fatalf("env override failed: %+v", cfg)
	}
}

func TestWindowPureMath(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	times := make([]time.Time, 10)
	for i := range times {
		times[i] = now.Add(time.Duration(i) * time.Second)
	}
	snap := WindowSnapshot{
		OccupiedAt: times, LastOccupied: times[9],
		Cap: 10, MinGap: 0, Window: RollingWindow, Now: now.Add(10 * time.Second),
	}
	ok, reason, _ := snap.CanGrant()
	if ok {
		t.Fatal("full window must deny")
	}
	if reason != "cap_reached" {
		t.Fatalf("reason=%s", reason)
	}
	// After oldest ages out
	snap.Now = times[0].Add(RollingWindow).Add(time.Second)
	ok, _, _ = snap.CanGrant()
	if !ok {
		t.Fatal("after window should grant")
	}
}

func TestStatusReports(t *testing.T) {
	clock := &FixedClock{T: time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)}
	g, _ := newTestGov(t, clock)
	org := uuid.New()
	ctx := context.Background()
	reserveAndCommit(t, g, org, ChannelEmail, "s1")
	st, err := g.Status(ctx, &org)
	if err != nil {
		t.Fatal(err)
	}
	if st.SentLastHour != 1 || st.Cap != 10 {
		t.Fatalf("status=%+v", st)
	}
	if st.Paused {
		t.Fatal("should not be paused")
	}
}

func TestCancelByRecipientDropsQueued(t *testing.T) {
	clock := &FixedClock{T: time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)}
	g, store := newTestGov(t, clock)
	org := uuid.New()
	ctx := context.Background()
	did := uuid.New()
	key := MessageKeyEmail(did)
	email := "lead@example.com"
	_ = g.Enqueue(ctx, EnqueueRequest{
		OrganizationID: org, Channel: ChannelEmail, DraftID: did, MessageKey: key,
		RecipientRef: email, DueAt: clock.Now(),
	})
	n, err := g.CancelByRecipient(ctx, org, email, "DO_NOT_CONTACT")
	if err != nil || n != 1 {
		t.Fatalf("cancel n=%d err=%v", n, err)
	}
	item, _ := g.ClaimNextQueued(ctx)
	if item != nil {
		t.Fatal("DNC-cancelled item must not be claimable")
	}
	// No success slot consumed
	sent, _ := store.CountSendsSince(ctx, clock.Now().Add(-RollingWindow))
	if sent != 0 {
		t.Fatalf("sent=%d", sent)
	}
}

func TestCancelByRecipientPhoneAndEmail(t *testing.T) {
	clock := &FixedClock{T: time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)}
	g, _ := newTestGov(t, clock)
	org := uuid.New()
	ctx := context.Background()
	email := "lead@example.com"
	phone := "+5511999999999"
	_ = g.Enqueue(ctx, EnqueueRequest{
		OrganizationID: org, Channel: ChannelEmail, DraftID: uuid.New(),
		MessageKey: "email:1", RecipientRef: email, DueAt: clock.Now(),
	})
	_ = g.Enqueue(ctx, EnqueueRequest{
		OrganizationID: org, Channel: ChannelWhatsApp, DraftID: uuid.New(),
		MessageKey: "wa:1", RecipientRef: phone, DueAt: clock.Now(),
	})
	// DNC by email only must not clear phone-queued WA — cancel both explicitly.
	n1, _ := g.CancelByRecipient(ctx, org, email, "DO_NOT_CONTACT")
	n2, _ := g.CancelByRecipient(ctx, org, phone, "DO_NOT_CONTACT")
	if n1 != 1 || n2 != 1 {
		t.Fatalf("cancel email=%d phone=%d", n1, n2)
	}
	if item, _ := g.ClaimNextQueued(ctx); item != nil {
		t.Fatalf("queue should be empty, got %s", item.MessageKey)
	}
}
