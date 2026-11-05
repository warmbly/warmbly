package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type localDayEmails struct {
	repository.EmailRepository
	account *models.Email
}

func (r localDayEmails) GetByID(context.Context, uuid.UUID) (*models.Email, *errx.Error) {
	return r.account, nil
}

type localDayTasks struct {
	repository.TaskRepository
	sent       int
	start, end time.Time
}

func (r *localDayTasks) CountWarmupEmailsSentBetween(_ context.Context, _ uuid.UUID, start, end time.Time) (int, error) {
	r.start, r.end = start, end
	return r.sent, nil
}

func (r *localDayTasks) GetLastEmailTime(context.Context, uuid.UUID) (*time.Time, error) {
	return nil, nil
}

func (r *localDayTasks) GetBookedSendsForAccount(context.Context, uuid.UUID, time.Time) ([]repository.Task, error) {
	return nil, nil
}

func TestWarmupDailyCountUsesTheMailboxLocalDay(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now().Add(-48 * time.Hour)
	account := &models.Email{
		ID: uuid.New(), Timezone: loc.String(), Warmup: &started,
		WarmupStartTime: "00:01", WarmupEndTime: "23:59", WarmupDays: 127,
		WarmupBase: 10, WarmupIncrease: 1, WarmupMax: 40, MinWaitTime: 600,
	}
	if local := time.Now().In(loc); local.Hour()*60+local.Minute() >= 23*60+58 || local.Hour()*60+local.Minute() < 2 {
		t.Skip("too close to the mailbox's midnight for a stable local day")
	}

	tasks := &localDayTasks{}
	s := &schedulerService{emailRepo: localDayEmails{account: account}, taskRepo: tasks}
	if _, err := s.CalculateNextWarmupTime(t.Context(), account.ID); err != nil {
		t.Fatal(err)
	}
	local := time.Now().In(loc)
	wantStart := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	if !tasks.start.Equal(wantStart) || !tasks.end.Equal(wantStart.AddDate(0, 0, 1)) {
		t.Fatalf("counted [%v, %v), want the Tokyo day [%v, %v)", tasks.start, tasks.end, wantStart, wantStart.AddDate(0, 0, 1))
	}

	// A day already full in Tokyo moves the next send to the next Tokyo day.
	tasks.sent = 1000
	next, err := s.CalculateNextWarmupTime(t.Context(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !next.In(loc).After(wantStart.AddDate(0, 0, 1)) {
		t.Fatalf("full local day scheduled at %v, want the next Tokyo day", next.In(loc))
	}
}
