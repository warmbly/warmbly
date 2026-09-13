package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/app/behavior"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/encrypt"
	"github.com/warmbly/warmbly/internal/repository"
)

// Live checks for issue #469: the sending profile keeps its own send counter
// (BehaviorRepository.CountSendsBetween) and it never got the dispatch check
// #312 gave the fixed-schedule counter, so a mailbox reported sends it had not
// made. Both halves of what it counted were phantoms — a completed campaign
// task is usually a wake-up that sent nothing, and a pending one has not sent
// yet — and the deferral path wakes the chain every CampaignMaxDeferMinutes,
// so a mailbox parked outside its rolled workday overnight burned its whole
// daily plan before the workday opened.
//
// Same harness and env var as live_integration_test.go.

// pendingCampaignTask writes the chain's next wake-up: one pending campaign
// task on the fixture mailbox, scheduled at `at`. Inserted directly rather than
// through CreateTaskWithLock, which allows only one pending task per campaign.
func (f *liveFixture) pendingCampaignTask(t *testing.T, at time.Time) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	taskID := uuid.New()
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO tasks (id, task_type, email_account_id, status, message_id, scheduled_at, created_at, updated_at)
		VALUES ($1, 'campaign', $2, 'pending', '', $3, NOW(), NOW())`, taskID, f.mailbox, at); err != nil {
		t.Fatalf("pending task: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO campaign_tasks (task_id, campaign_id) VALUES ($1, $2)`,
		taskID, f.campaign); err != nil {
		t.Fatalf("link task: %v", err)
	}
	return taskID
}

// liveBehavior builds the behaviour service over the live pool, which is what
// both the dashboard's "today" card and the schedulers' volume gates read.
func liveBehavior(t *testing.T, handle *db.DB, pool *pgxpool.Pool) behavior.Service {
	t.Helper()
	enc, err := encrypt.NewEncrypter([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("encrypter: %v", err)
	}
	return behavior.NewService(repository.NewBehaviorRepository(pool), repository.NewEmailRepostory(handle, enc))
}

// openAllDayProfile is a profile that is never closed, so anything a test
// observes about volume comes from the counter and not from the workday.
func openAllDayProfile() models.SendingBehavior {
	b := liveProfile()
	b.WorkStartMin, b.WorkStartMax = 0, 1
	b.WorkEndMin, b.WorkEndMax = 1438, 1439
	b.LunchEnabled = false
	b.Weekdays = models.BehaviorWeekdaysAll
	return b
}

// TestLiveProfileBudgetIgnoresWakeUps is the reported symptom: a mailbox whose
// chain woke up all night and sent nothing must report nothing sent, and must
// still have its whole rolled plan to spend.
func TestLiveProfileBudgetIgnoresWakeUps(t *testing.T) {
	handle, pool := liveDB(t)
	f := newLiveFixture(t, pool, "UTC")
	ctx := context.Background()

	b := openAllDayProfile()
	f.setProfile(t, b)

	// Ten deferral wake-ups since midnight, plus the pending successor the
	// last of them scheduled. None of the eleven put an email on the wire.
	midnight := behavior.PlanDateFor(time.Now(), time.UTC)
	for i := 0; i < 10; i++ {
		f.completeCampaignTask(t, nil, nil, midnight.Add(time.Duration(i)*time.Minute))
	}
	f.pendingCampaignTask(t, time.Now().Add(10*time.Minute))

	view, err := liveBehavior(t, handle, pool).Today(ctx, f.mailbox)
	if err != nil {
		t.Fatalf("today: %v", err)
	}
	if view.SentToday != 0 {
		t.Fatalf("sent_today = %d for a mailbox that sent nothing; the profile counted wake-ups as sends (issue #469)", view.SentToday)
	}
	if view.RemainingToday != view.DailyLimit {
		t.Fatalf("remaining_today = %d of a %d plan, want the whole plan intact", view.RemainingToday, view.DailyLimit)
	}
}

// TestLiveProfileBudgetMatchesTheFixedScheduleCounter pins the two counters
// together: the profile's number and the mailbox's number have to agree over
// the same mailbox and the same day, or "6 of 37" sits next to "0 of 45" on
// one screen again.
func TestLiveProfileBudgetMatchesTheFixedScheduleCounter(t *testing.T) {
	handle, pool := liveDB(t)
	f := newLiveFixture(t, pool, "UTC")
	ctx := context.Background()

	f.setProfile(t, openAllDayProfile())

	// One real send (the step's reservation points at the task), one send only
	// the worker's Message-ID vouches for, one wake-up, one pending successor.
	f.addSentLead(t)
	confirmed := f.completeCampaignTask(t, nil, nil, time.Now().Add(-time.Hour))
	if _, err := pool.Exec(ctx, `UPDATE tasks SET message_id = '<confirmed@test.local>' WHERE id = $1`, confirmed); err != nil {
		t.Fatal(err)
	}
	f.completeCampaignTask(t, nil, nil, time.Now().Add(-time.Minute))
	f.pendingCampaignTask(t, time.Now().Add(10*time.Minute))

	view, err := liveBehavior(t, handle, pool).Today(ctx, f.mailbox)
	if err != nil {
		t.Fatalf("today: %v", err)
	}
	fixed, err := repository.NewTaskRepository(pool).CountCampaignEmailsSentToday(ctx, f.mailbox)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if fixed != 2 {
		t.Fatalf("the fixed-schedule counter reads %d, want the two real sends", fixed)
	}
	if view.SentToday != fixed {
		t.Fatalf("profile counter reads %d, fixed-schedule counter reads %d — they must count the same thing", view.SentToday, fixed)
	}
}

// TestLiveProfileStillSendsAfterANightOfDeferrals is the consequence that made
// this worth reporting: with the wake-ups counted, a plan this size was gone
// before the mailbox ever opened, so every pass deferred and the campaign went
// out silent for the whole day.
func TestLiveProfileStillSendsAfterANightOfDeferrals(t *testing.T) {
	handle, pool := liveDB(t)
	f := newLiveFixture(t, pool, "UTC")
	ctx := context.Background()

	b := openAllDayProfile()
	b.DailyLimitMin, b.DailyLimitMax = 10, 10
	b.HourlyLimitMin, b.HourlyLimitMax = 5, 5
	f.setProfile(t, b)

	// More wake-ups than the whole rolled plan, none of them a send.
	midnight := behavior.PlanDateFor(time.Now(), time.UTC)
	for i := 0; i < 12; i++ {
		f.completeCampaignTask(t, nil, nil, midnight.Add(time.Duration(i)*time.Minute))
	}

	at, pair, accountID, err := liveScheduler(t, handle, pool).CalculateNextCampaignTime(ctx, f.campaign)
	if errors.Is(err, ErrCampaignDeferred) {
		t.Fatalf("the pass deferred at %s: a night of wake-ups spent the rolled plan (issue #469)", at)
	}
	if err != nil {
		t.Fatalf("the campaign should be sendable, got %v", err)
	}
	if pair == nil || accountID != f.mailbox {
		t.Fatalf("no sendable pair from the fixture mailbox: pair=%v account=%s", pair, accountID)
	}
	assertFuture(t, at)
}

// TestLiveProfileBudgetSpendsOnRealSends is the other half: the day's plan has
// to run out on sends that really happened.
func TestLiveProfileBudgetSpendsOnRealSends(t *testing.T) {
	handle, pool := liveDB(t)
	f := newLiveFixture(t, pool, "UTC")
	ctx := context.Background()

	b := openAllDayProfile()
	b.DailyLimitMin, b.DailyLimitMax = 1, 1
	f.setProfile(t, b)

	f.addSentLead(t)

	svc := liveBehavior(t, handle, pool)
	view, err := svc.Today(ctx, f.mailbox)
	if err != nil {
		t.Fatalf("today: %v", err)
	}
	if view.SentToday != 1 || view.RemainingToday != 0 {
		t.Fatalf("sent_today=%d remaining_today=%d, want the one real send to have spent a plan of %d",
			view.SentToday, view.RemainingToday, view.DailyLimit)
	}

	account, xerr := repository.NewEmailRepostory(handle, testEncrypter(t)).GetByID(ctx, f.mailbox)
	if xerr != nil {
		t.Fatalf("load mailbox: %v", xerr)
	}
	if left := svc.RemainingToday(ctx, svc.Resolve(ctx, account), time.Now()); left != 0 {
		t.Fatalf("RemainingToday = %d with the plan spent, want 0", left)
	}
}

// testEncrypter is the throwaway key the live fixtures use; these graphs store
// no sealed credentials.
func testEncrypter(t *testing.T) *encrypt.Encrypter {
	t.Helper()
	enc, err := encrypt.NewEncrypter([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("encrypter: %v", err)
	}
	return enc
}
