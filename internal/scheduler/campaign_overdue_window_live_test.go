package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// A campaign's sending window is a promise about when mail leaves, and an
// OVERDUE follow-up used to break it.
//
// Every schedule gate in the placer is asked about the step's candidate time,
// which for a follow-up starts at "when its wait elapsed". Once that moment is
// in the past, two gates stop working: nextScheduleSlot deliberately returns an
// instant that was ALREADY inside a window unchanged ("keep the exact
// instant"), and the end-date comparison finds a candidate that predates the
// end date. So the hard floor sat in the past, the not-due check read the step
// as due, and the task sent it immediately — hours after the window closed, or
// after the campaign was over.
//
// The chain reaches those hours routinely: a deferral is capped at
// config.CampaignMaxDeferMinutes so a wake-up near closing time lands past it,
// the reconciler re-seeds a dead chain a minute from now whatever the clock
// says, and a dispatch failure is retried the same way.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/scheduler/ -run LiveOverdue -v

// overdueFollowUp gives the fixture's lead a step 1 sent at sentAt and a step 2
// routed out of it with no wait, so step 2 has been due since sentAt.
func overdueFollowUp(t *testing.T, pool *pgxpool.Pool, f *liveFixture, step1, contact uuid.UUID, sentAt time.Time) {
	t.Helper()
	ctx := context.Background()
	step2 := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO sequences (id, campaign_id, organization_id, name, subject,
	        body_plain, body_html, wait_after, position, kind)
	    VALUES ($1, $2, $3, 'Step 2', 'Bump', 'Bump', '<p>Bump</p>', 0, 1, 'email')`,
		step2, f.campaign, f.org); err != nil {
		t.Fatalf("add step 2: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE sequences SET conditions = jsonb_build_object('branches',
	        jsonb_build_array(jsonb_build_object('branch_id', 'else', 'target_step_id', $1::text)))
	    WHERE id = $2`, step2.String(), step1); err != nil {
		t.Fatalf("connect steps: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO campaign_contact_progress
	    (campaign_id, contact_id, sequence_id, sent_at) VALUES ($1, $2, $3, $4)`,
		f.campaign, contact, step1, sentAt); err != nil {
		t.Fatalf("stamp step 1: %v", err)
	}
}

// hhmm renders an instant as the campaigns table's time-of-day.
func hhmm(t time.Time) string { return t.UTC().Format("15:04") }

// An overdue follow-up must wait for the window to reopen, not go out the
// moment the chain happens to wake up outside it.
func TestLiveOverdueFollowUpWaitsForTheWindowToReopen(t *testing.T) {
	handle, pool := liveDB(t)
	f := newLiveFixture(t, pool, "UTC")
	ctx := context.Background()
	step1, contact := liveLead(t, pool, f.campaign)

	// A window that closed an hour ago, and a step 1 that went out inside it.
	now := time.Now().UTC()
	opened, closed := now.Add(-3*time.Hour), now.Add(-1*time.Hour)
	if _, err := pool.Exec(ctx,
		`UPDATE campaigns SET start_time = $2, end_time = $3, days = 127 WHERE id = $1`,
		f.campaign, hhmm(opened), hhmm(closed)); err != nil {
		t.Fatalf("set the window: %v", err)
	}
	overdueFollowUp(t, pool, f, step1, contact, now.Add(-2*time.Hour))

	at, pair, _, err := liveScheduler(t, handle, pool).CalculateNextCampaignTime(ctx, f.campaign)
	if pair != nil && err == nil {
		t.Fatalf("the scheduler authorised a send at %s, %s after the window closed at %s",
			hhmm(now), time.Since(closed).Round(time.Minute), hhmm(closed))
	}
	if !errors.Is(err, ErrCampaignDeferred) {
		t.Fatalf("want a deferral, got %v", err)
	}
	// The wake-up it parks is a future moment inside the window. Which DAY is
	// deliberately not asserted: the distribution curve can push an early
	// window's candidate past that day's close and cost it a day, which is its
	// own thing and not what this test is about.
	if !at.After(now) {
		t.Fatalf("deferred to %s, which is not in the future", at.UTC().Format(time.RFC3339))
	}
	openMin := opened.Hour()*60 + opened.Minute()
	closeMin := closed.Hour()*60 + closed.Minute()
	if m := at.UTC().Hour()*60 + at.UTC().Minute(); m < openMin || m > closeMin {
		t.Fatalf("deferred to %s, which is outside the %s-%s window it must wait for",
			at.UTC().Format(time.RFC3339), hhmm(opened), hhmm(closed))
	}
}

// The same step, with the window still open, must still go out promptly: the
// fix is a floor on the candidate, not a new wait.
func TestLiveOverdueFollowUpStillSendsInsideTheWindow(t *testing.T) {
	handle, pool := liveDB(t)
	f := newLiveFixture(t, pool, "UTC")
	ctx := context.Background()
	step1, contact := liveLead(t, pool, f.campaign)

	// A window that opened two hours ago and is open for another two.
	now := time.Now().UTC()
	if _, err := pool.Exec(ctx,
		`UPDATE campaigns SET start_time = $2, end_time = $3, days = 127 WHERE id = $1`,
		f.campaign, hhmm(now.Add(-2*time.Hour)), hhmm(now.Add(2*time.Hour))); err != nil {
		t.Fatalf("set the window: %v", err)
	}
	overdueFollowUp(t, pool, f, step1, contact, now.Add(-90*time.Minute))

	_, pair, _, err := liveScheduler(t, handle, pool).CalculateNextCampaignTime(ctx, f.campaign)
	if err != nil || pair == nil {
		t.Fatalf("an overdue follow-up inside the window must send, got pair=%v err=%v", pair, err)
	}
}

// The end date is the other gate a past candidate slipped under: the comparison
// asked whether the moment the step came due was past the end date, not whether
// now is.
func TestLiveOverdueFollowUpRespectsTheEndDate(t *testing.T) {
	handle, pool := liveDB(t)
	f := newLiveFixture(t, pool, "UTC")
	ctx := context.Background()
	step1, contact := liveLead(t, pool, f.campaign)

	now := time.Now().UTC()
	if _, err := pool.Exec(ctx, `UPDATE campaigns SET end_date = $2 WHERE id = $1`,
		f.campaign, now.Add(-time.Hour)); err != nil {
		t.Fatalf("set the end date: %v", err)
	}
	overdueFollowUp(t, pool, f, step1, contact, now.Add(-2*time.Hour))

	_, pair, _, err := liveScheduler(t, handle, pool).CalculateNextCampaignTime(ctx, f.campaign)
	if pair != nil && err == nil {
		t.Fatal("the scheduler authorised a send for a campaign that ended an hour ago")
	}
	if !errors.Is(err, ErrCampaignEnded) {
		t.Fatalf("want the campaign reported as ended, got %v", err)
	}
}
