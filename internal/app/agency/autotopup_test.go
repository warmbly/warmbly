package agency

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

// fakeCharger records what was charged and answers as told.
type fakeCharger struct {
	card     *SavedCard
	declined bool
	keys     []string
	cents    []int64
}

func (f *fakeCharger) Card(context.Context, *models.Agency) (*SavedCard, error) { return f.card, nil }

func (f *fakeCharger) Charge(_ context.Context, _ *models.Agency, _ *SavedCard, cents int64, key string, _ map[string]string) (string, bool, error) {
	f.keys = append(f.keys, key)
	f.cents = append(f.cents, cents)
	if f.declined {
		return "", true, nil
	}
	return "pi_" + key, false, nil
}

// topupRepo is operatorRepo plus the automatic top-up's own writes.
type topupRepo struct {
	*operatorRepo
	disabled string
}

func (r *topupRepo) AutoTopupCount(context.Context, uuid.UUID, time.Time) (int, error) {
	n := 0
	for _, t := range r.recorded {
		if t.Automatic {
			n++
		}
	}
	return n, nil
}

func (r *topupRepo) DisableAutoTopup(_ context.Context, _ uuid.UUID, failure string) error {
	r.disabled = failure
	r.agency.AutoTopupEnabled = false
	r.agency.AutoTopupFailure = &failure
	return nil
}

func (r *topupRepo) SetAutoTopup(_ context.Context, _ uuid.UUID, enabled bool, threshold, maxPacks int) error {
	r.agency.AutoTopupEnabled, r.agency.AutoTopupThreshold, r.agency.AutoTopupMaxPacks, r.agency.AutoTopupFailure = enabled, threshold, maxPacks, nil
	return nil
}

func newTopupService(a *models.Agency, used int64, charger *fakeCharger, now time.Time) (*Service, *topupRepo, uuid.UUID) {
	actor := uuid.New()
	repo := &topupRepo{operatorRepo: &operatorRepo{
		fakeRepo: &fakeRepo{agency: a, grants: map[uuid.UUID]uuid.UUID{}, sends: map[uuid.UUID]int64{uuid.New(): used}},
		staff:    map[uuid.UUID]bool{actor: true}, untils: map[uuid.UUID]*time.Time{},
	}}
	s := NewService(repo, operatorOrgs{}, operatorUsers{}, operatorSubs{}, fakePlans{pool: intp(150_000)})
	s.billing = true
	s.now = func() time.Time { return now }
	s.charger = charger
	return s, repo, actor
}

func topupAgency(now time.Time, threshold, maxPacks int) *models.Agency {
	a := activeAgency(now)
	a.StripeCustomerID, a.StripeSubscriptionID = strp("cus_1"), strp("sub_1")
	a.AutoTopupEnabled, a.AutoTopupThreshold, a.AutoTopupMaxPacks = true, threshold, maxPacks
	return a
}

func TestAutoTopupBuysOnePackWhenThePoolIsLow(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	a := topupAgency(now, 10_000, 2)
	charger := &fakeCharger{card: &SavedCard{ID: "pm_1", Brand: "visa", Last4: "4242"}}
	// 145,000 of 150,000 used: 5,000 left, under the 10,000 threshold.
	s, repo, _ := newTopupService(a, 145_000, charger, now)
	ctx := context.Background()

	s.autoTopup(ctx, a)
	if len(repo.recorded) != 1 || !repo.recorded[0].Automatic || repo.recorded[0].Sends != models.AgencyTopupSends || repo.recorded[0].AmountCents != 8_500 {
		t.Fatalf("recorded: %+v", repo.recorded)
	}
	if len(charger.cents) != 1 || charger.cents[0] != 8_500 {
		t.Fatalf("charged: %v", charger.cents)
	}
	// 55,000 are left after the pack, so the next sweep buys nothing more.
	s.autoTopup(ctx, a)
	if len(charger.keys) != 1 {
		t.Fatalf("a pool back above its threshold was charged again: %v", charger.keys)
	}
}

func TestAutoTopupStopsAtThePeriodsPacks(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	a := topupAgency(now, 10_000, 2)
	charger := &fakeCharger{card: &SavedCard{ID: "pm_1"}}
	// So far over the pool that every pack leaves it empty.
	s, repo, _ := newTopupService(a, 400_000, charger, now)
	ctx := context.Background()
	for range 5 {
		s.autoTopup(ctx, a)
	}
	if len(repo.recorded) != 2 || len(charger.keys) != 2 {
		t.Fatalf("bought %d packs with %d charges, want the period's 2", len(repo.recorded), len(charger.keys))
	}
	if charger.keys[0] == charger.keys[1] {
		t.Fatal("each pack of a period needs its own charge key")
	}
}

func TestAutoTopupLeavesAHealthyPoolAlone(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	a := topupAgency(now, 10_000, 2)
	charger := &fakeCharger{card: &SavedCard{ID: "pm_1"}}
	s, repo, _ := newTopupService(a, 20_000, charger, now)
	s.autoTopup(context.Background(), a)
	a.AutoTopupEnabled = false
	s.autoTopup(context.Background(), a)
	if len(charger.keys) != 0 || len(repo.recorded) != 0 {
		t.Fatalf("charged %v, recorded %v", charger.keys, repo.recorded)
	}
}

func TestAutoTopupTurnsItselfOffWhenTheCardRefuses(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()

	a := topupAgency(now, 10_000, 2)
	s, repo, _ := newTopupService(a, 149_000, &fakeCharger{card: &SavedCard{ID: "pm_1"}, declined: true}, now)
	s.autoTopup(ctx, a)
	if repo.disabled != models.AgencyAutoTopupDeclined || a.AutoTopupEnabled || len(repo.recorded) != 0 {
		t.Fatalf("declined: disabled=%q enabled=%v recorded=%v", repo.disabled, a.AutoTopupEnabled, repo.recorded)
	}

	b := topupAgency(now, 10_000, 2)
	charger := &fakeCharger{}
	s, repo, _ = newTopupService(b, 149_000, charger, now)
	s.autoTopup(ctx, b)
	if repo.disabled != models.AgencyAutoTopupNoCard || len(charger.keys) != 0 {
		t.Fatalf("no card: disabled=%q charges=%v", repo.disabled, charger.keys)
	}
}

func TestSetAutoTopupNeedsACardAndSaneLimits(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()
	a := activeAgency(now)
	a.StripeCustomerID = strp("cus_1")
	charger := &fakeCharger{}
	s, _, actor := newTopupService(a, 0, charger, now)

	if _, xerr := s.SetAutoTopup(ctx, a.ID, actor, true, 10_000, 2); xerr == nil || xerr.Identifier != "agency_no_payment_method" {
		t.Fatalf("no card: got %v", xerr)
	}
	charger.card = &SavedCard{ID: "pm_1", Brand: "visa", Last4: "4242"}
	for _, c := range [][2]int{{-1, 2}, {10_000, 0}, {10_000, 21}} {
		if _, xerr := s.SetAutoTopup(ctx, a.ID, actor, true, c[0], c[1]); xerr == nil || xerr.Identifier != "invalid_auto_topup" {
			t.Fatalf("threshold %d, packs %d: got %v", c[0], c[1], xerr)
		}
	}
	view, xerr := s.SetAutoTopup(ctx, a.ID, actor, true, 10_000, 3)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if !view.Enabled || view.ThresholdSends != 10_000 || view.MaxPacks != 3 || view.Card == nil || view.Card.Last4 != "4242" || view.PackCents != 8_500 || !view.Available {
		t.Fatalf("view: %+v", view)
	}
	// Turning it off needs no card.
	charger.card = nil
	if view, xerr := s.SetAutoTopup(ctx, a.ID, actor, false, 10_000, 3); xerr != nil || view.Enabled {
		t.Fatalf("off: %+v %v", view, xerr)
	}
}
