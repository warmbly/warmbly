package agency

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"
	"github.com/warmbly/warmbly/internal/models"
)

func TestPeriodRollsUpBeforeSummingPoolDays(t *testing.T) {
	now := time.Date(2026, 11, 8, 9, 0, 0, 0, time.UTC)
	org := uuid.New()
	a := activeAgency(now)
	// A billing anchor at 13:30 UTC: pool days start at 13:30, so a period is whole days.
	start := time.Date(2026, 10, 20, 13, 30, 0, 0, time.UTC)
	end := time.Date(2026, 11, 20, 13, 30, 0, 0, time.UTC)
	a.CurrentPeriodStart, a.CurrentPeriodEnd = &start, &end
	repo := &fakeRepo{agency: a, client: &models.AgencyClient{OrganizationID: org}, sends: map[uuid.UUID]int64{org: 10}}
	s := newTestService(repo, intp(1_000), true, now)

	if _, ok := s.DailyLimit(context.Background(), org); !ok {
		t.Fatal("client of an active agency")
	}
	if len(repo.rollups) != 1 || repo.sendsCalls != 1 {
		t.Fatalf("one rollup then one sum per read, got %d rollups, %d sums", len(repo.rollups), repo.sendsCalls)
	}
	// 09:00 on Nov 8 is in the pool day that began at 13:30 on Nov 7.
	days := repo.rollups[0]
	wantToday := time.Date(2026, 11, 7, 13, 30, 0, 0, time.UTC)
	if len(days) != 2 || !days[1].Start.Equal(wantToday) || !days[0].Start.Equal(wantToday.AddDate(0, 0, -1)) {
		t.Fatalf("rollup must cover the current and previous pool day, got %+v", days)
	}
	if !days[1].Day.Equal(time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("pool day date: got %s", days[1].Day)
	}
	if from, to := repo.usageRange[0], repo.usageRange[1]; !from.Equal(time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)) ||
		!to.Equal(time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("usage must sum exactly the period's pool days, got %s..%s", from, to)
	}
}

func TestDailyLimitIsFixedWhileThePoolReadIsReused(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	org := uuid.New()
	repo := &fakeRepo{
		agency: activeAgency(now), client: &models.AgencyClient{OrganizationID: org},
		sends: map[uuid.UUID]int64{org: 900}, sentToday: 40,
	}
	s := newTestService(repo, intp(1_000), true, now)
	limit, ok := s.DailyLimit(context.Background(), org)
	if !ok || limit != 40+100 {
		t.Fatalf("got %d, want 140 (40 sent today + 100 left)", limit)
	}
	// The client keeps sending; a fresh daily count must not raise a limit built on a cached pool.
	repo.sentToday = 140
	if again, _ := s.DailyLimit(context.Background(), org); again != limit {
		t.Fatalf("limit moved from %d to %d inside the cache window, so the pool could be overspent", limit, again)
	}
	if repo.sendsCalls != 1 {
		t.Fatalf("pool read %d times inside its window", repo.sendsCalls)
	}

	// After the window both halves are read again together.
	s.now = func() time.Time { return now.Add(poolTTL + time.Second) }
	repo.sends = map[uuid.UUID]int64{org: 1_000}
	if fresh, _ := s.DailyLimit(context.Background(), org); fresh != 140 {
		t.Fatalf("got %d, want 140 (140 sent today, nothing left)", fresh)
	}
}

func TestDailyLimitRereadsALowPoolSooner(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	org := uuid.New()
	repo := &fakeRepo{agency: activeAgency(now), client: &models.AgencyClient{OrganizationID: org}, sends: map[uuid.UUID]int64{org: 970}}
	s := newTestService(repo, intp(1_000), true, now)
	_, _ = s.DailyLimit(context.Background(), org)
	s.now = func() time.Time { return now.Add(lowPoolTTL + time.Second) }
	_, _ = s.DailyLimit(context.Background(), org)
	if repo.sendsCalls != 2 {
		t.Fatalf("with 3%% of the pool left it must be read again after %s, read %d times", lowPoolTTL, repo.sendsCalls)
	}

	healthy := &fakeRepo{agency: activeAgency(now), client: &models.AgencyClient{OrganizationID: org}, sends: map[uuid.UUID]int64{org: 500}}
	hs := newTestService(healthy, intp(1_000), true, now)
	_, _ = hs.DailyLimit(context.Background(), org)
	hs.now = func() time.Time { return now.Add(lowPoolTTL + time.Second) }
	_, _ = hs.DailyLimit(context.Background(), org)
	if healthy.sendsCalls != 1 {
		t.Fatalf("a healthy pool keeps its read for %s, read %d times", poolTTL, healthy.sendsCalls)
	}
}

type refundRepo struct {
	*fakeRepo
	byIntent map[string]uuid.UUID
	refunds  []string
}

func (r *refundRepo) RefundTopup(_ context.Context, pi string) (uuid.UUID, bool, error) {
	r.refunds = append(r.refunds, pi)
	id, ok := r.byIntent[pi]
	return id, ok, nil
}

func chargeEvent(t *testing.T, ch map[string]any) *stripe.Event {
	t.Helper()
	raw, err := json.Marshal(ch)
	if err != nil {
		t.Fatal(err)
	}
	return &stripe.Event{Type: "charge.refunded", Data: &stripe.EventData{Raw: raw}}
}

func TestRefundedTopupLeavesThePool(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	agencyID := uuid.New()
	repo := &refundRepo{fakeRepo: &fakeRepo{}, byIntent: map[string]uuid.UUID{"pi_pack": agencyID}}
	s := newTestService(repo.fakeRepo, intp(1_000), true, now)
	s.repo = repo
	s.pools[agencyID] = poolEntry{at: now}
	ctx := context.Background()

	handled, xerr := s.HandleStripeEvent(ctx, chargeEvent(t, map[string]any{
		"id": "ch_1", "amount": 5000, "amount_refunded": 5000, "refunded": true, "payment_intent": "pi_pack",
	}))
	if xerr != nil || !handled || len(repo.refunds) != 1 {
		t.Fatalf("a full refund of a pack must be marked: handled %v, refunds %v, err %v", handled, repo.refunds, xerr)
	}
	if _, cached := s.pools[agencyID]; cached {
		t.Fatal("the pool read must be dropped so the pack leaves it at once")
	}

	handled, _ = s.HandleStripeEvent(ctx, chargeEvent(t, map[string]any{
		"id": "ch_2", "amount": 5000, "amount_refunded": 1000, "refunded": false, "payment_intent": "pi_pack",
		"metadata": map[string]string{metaPurpose: purposeAgencyTopup},
	}))
	if !handled || len(repo.refunds) != 1 {
		t.Fatal("a partial refund keeps the pack and stays off the workspace path")
	}

	handled, _ = s.HandleStripeEvent(ctx, chargeEvent(t, map[string]any{
		"id": "ch_3", "amount": 900, "amount_refunded": 900, "refunded": true, "payment_intent": "pi_workspace",
	}))
	if handled {
		t.Fatal("a refund of a charge that paid no pack belongs to the workspace path")
	}
}
