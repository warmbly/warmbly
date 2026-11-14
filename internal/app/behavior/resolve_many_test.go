package behavior

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type batchBehaviorRepo struct {
	repository.BehaviorRepository
	stored map[uuid.UUID]models.SendingBehavior
	err    error
	calls  int
	ids    []uuid.UUID
}

func (r *batchBehaviorRepo) GetBehaviors(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]models.SendingBehavior, error) {
	r.calls++
	r.ids = ids
	return r.stored, r.err
}

func (r *batchBehaviorRepo) EnsurePlan(_ context.Context, plan models.DailyPlan) (*models.DailyPlan, error) {
	return &plan, nil
}

// resolveManyUncached preserves the BASE implementation for differential checks.
func resolveManyUncached(s *service, ctx context.Context, accounts []models.Email) map[uuid.UUID]Resolved {
	out := make(map[uuid.UUID]Resolved, len(accounts))
	if len(accounts) == 0 {
		return out
	}
	ids := make([]uuid.UUID, 0, len(accounts))
	for _, a := range accounts {
		ids = append(ids, a.ID)
	}
	stored, err := s.behaviorRepo.GetBehaviors(ctx, ids)
	if err != nil {
		stored = map[uuid.UUID]models.SendingBehavior{}
	}
	for _, a := range accounts {
		b, ok := stored[a.ID]
		if !ok {
			b = models.DefaultSendingBehavior(a.ID)
		}
		b.Timezone = a.ClockTimezone()
		out[a.ID] = Resolved{Enabled: b.Enabled, Behavior: b, Loc: loadLocation(a.ClockTimezone()), accountID: a.ID, svc: s, ctx: ctx, cache: map[string]models.DailyPlan{}, sent: map[[2]int64]int{}}
	}
	return out
}

func TestResolveManyTimezoneReusePreservesProfilesAndPlans(t *testing.T) {
	zones := []string{"Europe/Budapest", "Europe/Budapest", "America/New_York", "Asia/Kolkata", "", "UTC", "not/a/timezone"}
	accounts := make([]models.Email, len(zones))
	stored := map[uuid.UUID]models.SendingBehavior{}
	for i, zone := range zones {
		accounts[i] = models.Email{ID: uuid.New(), Timezone: zone}
		if i%2 == 0 {
			b := models.DefaultSendingBehavior(accounts[i].ID)
			b.Enabled = true
			stored[b.EmailAccountID] = b
		}
	}
	for _, repoErr := range []error{nil, errors.New("batch unavailable")} {
		repo := &batchBehaviorRepo{stored: stored, err: repoErr}
		s := &service{behaviorRepo: repo}
		ctx := context.Background()
		want := resolveManyUncached(s, ctx, accounts)
		repo.calls = 0
		got := s.ResolveMany(ctx, accounts)
		if repo.calls != 1 || len(repo.ids) != len(accounts) {
			t.Fatalf("batch calls=%d ids=%d", repo.calls, len(repo.ids))
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatal("resolved profiles differ from the uncached implementation")
		}
		if got[accounts[0].ID].Loc != got[accounts[1].ID].Loc {
			t.Fatal("shared timezone was parsed more than once")
		}
		for _, a := range accounts {
			g, w := got[a.ID], want[a.ID]
			for _, date := range []time.Time{
				time.Date(2026, 3, 29, 1, 30, 0, 0, time.UTC),
				time.Date(2026, 10, 25, 1, 30, 0, 0, time.UTC),
				PlanDateFor(time.Now(), g.Loc),
			} {
				if !reflect.DeepEqual(g.PlanOn(date), w.PlanOn(date)) {
					t.Fatalf("daily plan changed for timezone %q at %s", a.Timezone, date)
				}
			}
		}
		got[accounts[0].ID].cache["isolation"] = models.DailyPlan{}
		if _, ok := got[accounts[1].ID].cache["isolation"]; ok {
			t.Fatal("mailbox plan caches must remain separate")
		}
	}
}

func TestResolveManyEmptyDoesNotRead(t *testing.T) {
	repo := &batchBehaviorRepo{}
	s := &service{behaviorRepo: repo}
	if got := s.ResolveMany(context.Background(), nil); len(got) != 0 || repo.calls != 0 {
		t.Fatalf("empty batch: profiles=%d reads=%d", len(got), repo.calls)
	}
}

func BenchmarkResolveManyTimezone(b *testing.B) {
	accounts := make([]models.Email, 100)
	zones := []string{"Europe/Budapest", "America/New_York", "Asia/Kolkata", "Europe/London"}
	for i := range accounts {
		accounts[i] = models.Email{ID: uuid.New(), Timezone: zones[i%len(zones)]}
	}
	s := &service{behaviorRepo: &batchBehaviorRepo{}}
	ctx := context.Background()
	for _, mode := range []string{"uncached", "per_batch"} {
		b.Run(mode, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				var resolved map[uuid.UUID]Resolved
				if mode == "uncached" {
					resolved = resolveManyUncached(s, ctx, accounts)
				} else {
					resolved = s.ResolveMany(ctx, accounts)
				}
				if len(resolved) != len(accounts) {
					b.Fatal("incomplete batch")
				}
			}
		})
	}
}
