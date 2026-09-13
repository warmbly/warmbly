package behavior

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// The send count has two callers with deliberately opposite error contracts,
// and nothing pinned either: Today REPORTS, so an unreadable count has to
// surface as an error rather than as a number a human reads, while the
// schedulers GATE, so the same failure has to spend the budget and delay the
// send instead of risking an over-send.

var errCount = errors.New("count unavailable")

// Both repositories are embedded rather than implemented: only the handful of
// methods the service actually calls on this path are overridden, so widening
// either interface does not drag this file along.
type stubBehaviorRepo struct {
	repository.BehaviorRepository
	behavior models.SendingBehavior
	count    int
	countErr error
}

func (s *stubBehaviorRepo) GetBehavior(context.Context, uuid.UUID) (*models.SendingBehavior, error) {
	b := s.behavior
	return &b, nil
}

func (s *stubBehaviorRepo) EnsurePlan(context.Context, models.DailyPlan) (*models.DailyPlan, error) {
	// Nothing stored: PlanOn keeps the plan it rolled.
	return nil, nil
}

func (s *stubBehaviorRepo) CountSendsBetween(context.Context, uuid.UUID, string, time.Time, time.Time) (int, error) {
	return s.count, s.countErr
}

type stubEmailRepo struct {
	repository.EmailRepository
	account *models.Email
}

func (s stubEmailRepo) GetByID(context.Context, uuid.UUID) (*models.Email, *errx.Error) {
	return s.account, nil
}

// countingService wires the service over the two stubs, with a profile that
// works every day so the weekday mask can never make an assertion vacuous.
func countingService(count int, countErr error) (Service, *models.Email) {
	b := models.DefaultSendingBehavior(testAccount)
	b.Enabled = true
	b.Weekdays = models.BehaviorWeekdaysAll
	account := &models.Email{ID: testAccount, Timezone: "UTC"}
	return NewService(
		&stubBehaviorRepo{behavior: b, count: count, countErr: countErr},
		stubEmailRepo{account: account},
	), account
}

func TestTodayReportsTheCount(t *testing.T) {
	svc, _ := countingService(7, nil)

	view, err := svc.Today(context.Background(), testAccount)
	if err != nil {
		t.Fatalf("today: %v", err)
	}
	if view.SentToday != 7 {
		t.Fatalf("sent_today = %d, want the 7 the counter reported", view.SentToday)
	}
	if view.RemainingToday != view.DailyLimit-7 {
		t.Fatalf("remaining_today = %d, want %d", view.RemainingToday, view.DailyLimit-7)
	}
}

// An unreadable count must not reach the dashboard as the schedulers' sentinel:
// it rendered as `sent_today: 9223372036854775807`, which is worse than an error.
func TestTodayPropagatesACountError(t *testing.T) {
	svc, _ := countingService(0, errCount)

	view, err := svc.Today(context.Background(), testAccount)
	if !errors.Is(err, errCount) {
		t.Fatalf("err = %v, want the count error propagated", err)
	}
	if view != nil {
		t.Fatalf("view = %+v, want nothing alongside the error", view)
	}
}

func TestRemainingFailsClosedOnACountError(t *testing.T) {
	ctx := context.Background()
	ok, account := countingService(0, nil)
	if left := ok.RemainingToday(ctx, ok.Resolve(ctx, account), time.Now()); left <= 0 {
		t.Fatalf("control: RemainingToday = %d with nothing sent, the profile is not gating on volume here", left)
	}

	svc, account := countingService(0, errCount)
	r := svc.Resolve(ctx, account)
	if left := svc.RemainingToday(ctx, r, time.Now()); left != 0 {
		t.Fatalf("RemainingToday = %d on an unreadable count, want 0: an unknown spend delays rather than over-sends", left)
	}
	if left := svc.RemainingThisHour(ctx, r, time.Now()); left != 0 {
		t.Fatalf("RemainingThisHour = %d on an unreadable count, want 0", left)
	}
}
