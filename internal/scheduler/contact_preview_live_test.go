package scheduler

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/models"
)

// Live checks for the contact drawer's next-action preview (issue #255). The
// preview must say what the send path would do, so every case here is proven
// against the real routing and placement queries rather than a stub.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/scheduler/ -run LivePreview -v

func livePreviewer(t *testing.T, s SchedulerService) ContactSendPreviewer {
	t.Helper()
	p, ok := s.(ContactSendPreviewer)
	if !ok {
		t.Fatal("scheduler service does not preview contact sends")
	}
	return p
}

func liveLead(t *testing.T, pool *pgxpool.Pool, campaign uuid.UUID) (step1, contact uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	if err := pool.QueryRow(ctx,
		`SELECT id FROM sequences WHERE campaign_id = $1 AND position = 0`, campaign).Scan(&step1); err != nil {
		t.Fatalf("load step 1: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT contact_id FROM campaign_leads WHERE campaign_id = $1`, campaign).Scan(&contact); err != nil {
		t.Fatalf("load contact: %v", err)
	}
	return step1, contact
}

// parkWakeup writes the campaign chain's pending wakeup at `at`, the row a
// real tick leaves behind for its successor.
func parkWakeup(t *testing.T, pool *pgxpool.Pool, f *liveFixture, at time.Time) {
	t.Helper()
	ctx := context.Background()
	id := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO tasks (id, task_type, email_account_id, status, message_id, scheduled_at, created_at, updated_at)
	    VALUES ($1, 'campaign', $2, 'pending', '', $3, NOW(), NOW())`, id, f.mailbox, at); err != nil {
		t.Fatalf("park wakeup: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO campaign_tasks (task_id, campaign_id) VALUES ($1, $2)`, id, f.campaign); err != nil {
		t.Fatalf("link wakeup: %v", err)
	}
}

// A new lead in an always-open campaign is due now: the preview names the
// entry step and reports the campaign chain's own next wakeup as the time it
// will be served.
func TestLivePreviewDueLeadGetsASlot(t *testing.T) {
	handle, pool := liveDB(t)
	f := newLiveFixture(t, pool, "UTC")
	step1, contact := liveLead(t, pool, f.campaign)
	wakeup := time.Now().Add(7 * time.Minute).Truncate(time.Second)
	parkWakeup(t, pool, f, wakeup)

	pv, err := livePreviewer(t, liveScheduler(t, handle, pool)).PreviewContactSend(context.Background(), f.campaign, contact)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if pv.Route.Target == nil || *pv.Route.Target != step1 || !pv.Route.IsNewLead {
		t.Fatalf("want the entry step for a new lead, got route %+v", pv.Route)
	}
	if pv.State != models.NextActionDue || pv.ScheduledAt == nil {
		t.Fatalf("want a due step with a slot, got state=%q scheduled_at=%v constraint=%q", pv.State, pv.ScheduledAt, pv.Constraint)
	}
	if !pv.ScheduledAt.Equal(wakeup) {
		t.Fatalf("slot %s is not the campaign's parked wakeup %s", pv.ScheduledAt.UTC(), wakeup.UTC())
	}
}

// A follow-up inside its wait must not be promised a time: the preview reports
// the step, the wait as the constraint, and an honest not-before.
func TestLivePreviewFollowUpWaitIsReported(t *testing.T) {
	handle, pool := liveDB(t)
	f := newLiveFixture(t, pool, "UTC")
	ctx := context.Background()
	step1, contact := liveLead(t, pool, f.campaign)

	step2 := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO sequences (id, campaign_id, organization_id, name, subject,
			body_plain, body_html, wait_after, position, kind)
		VALUES ($1, $2, $3, 'Step 2', 'Bump', 'Bump', '<p>Bump</p>', 3, 1, 'email')`,
		step2, f.campaign, f.org); err != nil {
		t.Fatalf("insert step 2: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE sequences SET conditions = jsonb_build_object('branches', jsonb_build_array(
			jsonb_build_object('branch_id', 'live-else', 'target_step_id', $1::text)))
		WHERE campaign_id = $2 AND position = 0`, step2.String(), f.campaign); err != nil {
		t.Fatalf("connect steps: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO campaign_contact_progress (campaign_id, contact_id, sequence_id, sent_at)
		VALUES ($1, $2, $3, NOW())`, f.campaign, contact, step1); err != nil {
		t.Fatalf("stamp step 1 sent: %v", err)
	}

	pv, err := livePreviewer(t, liveScheduler(t, handle, pool)).PreviewContactSend(ctx, f.campaign, contact)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if pv.Route.Target == nil || *pv.Route.Target != step2 {
		t.Fatalf("want step 2 routed next, got %+v", pv.Route)
	}
	if pv.State != models.NextActionWaiting || pv.Constraint != ConstraintStepWait {
		t.Fatalf("want waiting on the step wait, got state=%q constraint=%q", pv.State, pv.Constraint)
	}
	if pv.ScheduledAt != nil {
		t.Fatalf("a waiting step must not promise a slot, got %s", pv.ScheduledAt)
	}
	if pv.NotBefore == nil || time.Until(*pv.NotBefore) < 47*time.Hour {
		t.Fatalf("not-before %v is too soon for a 3-day wait", pv.NotBefore)
	}
}

// A lead whose campaign is outside its sending window right now: the preview
// says so, and its not-before lands inside the next window instead of
// inventing a time in the closed one.
func TestLivePreviewSendingWindowBlocksTheStep(t *testing.T) {
	handle, pool := liveDB(t)
	f := newLiveFixture(t, pool, "UTC")
	ctx := context.Background()
	_, contact := liveLead(t, pool, f.campaign)

	// A one-hour window that opens three hours from now. Near midnight the
	// window would wrap, so it is pushed to 01:00-02:00 tomorrow instead.
	now := time.Now().UTC()
	startHour := (now.Hour() + 3) % 24
	if startHour >= 23 || startHour < now.Hour() {
		startHour = 1
	}
	if _, err := pool.Exec(ctx, `UPDATE campaigns SET start_time = $2, end_time = $3 WHERE id = $1`,
		f.campaign, fmt.Sprintf("%02d:00", startHour), fmt.Sprintf("%02d:00", startHour+1)); err != nil {
		t.Fatalf("narrow window: %v", err)
	}

	pv, err := livePreviewer(t, liveScheduler(t, handle, pool)).PreviewContactSend(ctx, f.campaign, contact)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if pv.State != models.NextActionWaiting || pv.Constraint != ConstraintSendingWindow {
		t.Fatalf("want waiting on the sending window, got state=%q constraint=%q scheduled_at=%v", pv.State, pv.Constraint, pv.ScheduledAt)
	}
	if pv.ScheduledAt != nil {
		t.Fatalf("a window-blocked step must not promise a slot, got %s", pv.ScheduledAt)
	}
	if pv.NotBefore == nil {
		t.Fatal("want a not-before inside the next window")
	}
	until := time.Until(*pv.NotBefore)
	if until < 90*time.Minute || until > 27*time.Hour {
		t.Fatalf("not-before %s away does not sit in the next window", until.Round(time.Minute))
	}
	t.Logf("window opens in ~%s; preview says not before %s", until.Round(time.Minute), pv.NotBefore.UTC().Format(time.Kitchen))
}

// A paused campaign schedules nothing, whatever the step says.
func TestLivePreviewPausedCampaignHasNoSlot(t *testing.T) {
	handle, pool := liveDB(t)
	f := newLiveFixture(t, pool, "UTC")
	ctx := context.Background()
	step1, contact := liveLead(t, pool, f.campaign)
	if _, err := pool.Exec(ctx, `UPDATE campaigns SET status = 'paused' WHERE id = $1`, f.campaign); err != nil {
		t.Fatalf("pause: %v", err)
	}

	pv, err := livePreviewer(t, liveScheduler(t, handle, pool)).PreviewContactSend(ctx, f.campaign, contact)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if pv.Route.Target == nil || *pv.Route.Target != step1 {
		t.Fatalf("routing must still name the step, got %+v", pv.Route)
	}
	if pv.State != models.NextActionPaused || pv.Constraint != ConstraintCampaignInactive || pv.ScheduledAt != nil {
		t.Fatalf("want paused with no slot, got state=%q constraint=%q scheduled_at=%v", pv.State, pv.Constraint, pv.ScheduledAt)
	}
}

// The reported symptom of issue #437: the drawer showed a step marked Due whose
// "next slot" walked forward on every refresh — 2:41 PM, then 2:44, then 2:51.
// The preview was running the whole send path for a read, including the layers
// that are not constraints at all: even distribution measured from time.Now()
// with a random multiplier, ±20 minutes of jitter, conflict resolution, the
// distribution curve, a behaviour gap drawn fresh from the mailbox's range,
// weighted rotation re-rolled per call, a randomised sub-minute component, and
// a next-day deferral jittered by up to half an hour so a fleet of chains does
// not wake together.
//
// So the invariant is one sentence and it covers every state: reading an
// unchanged campaign twice gives the same answer twice. It is asserted over the
// whole state table rather than on the one case that was reported, because each
// state reaches the slot through a different set of those layers, and each
// layer was its own drift.
func TestLivePreviewIsStableAcrossReads(t *testing.T) {
	ctx := context.Background()
	exec := func(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("setup %q: %v", sql[:min(70, len(sql))], err)
		}
	}
	// sent records a completed campaign send from a mailbox a moment ago, which
	// is what starts its minimum gap and spends its daily budget.
	sent := func(t *testing.T, pool *pgxpool.Pool, mailbox uuid.UUID) {
		t.Helper()
		exec(t, pool, `INSERT INTO tasks (id, task_type, email_account_id, status, message_id,
		        scheduled_at, completed_at, created_at, updated_at)
		    VALUES ($1, 'campaign', $2, 'completed', 'mid@test.local', NOW(), NOW(), NOW(), NOW())`,
			uuid.New(), mailbox)
	}

	cases := []struct {
		name  string
		state models.ContactNextActionState
		setup func(t *testing.T, pool *pgxpool.Pool, f *liveFixture, contact uuid.UUID)
	}{
		{"due, chain parked", models.NextActionDue, func(t *testing.T, pool *pgxpool.Pool, f *liveFixture, c uuid.UUID) {
			parkWakeup(t, pool, f, time.Now().Add(6*time.Minute).Truncate(time.Second))
		}},
		{"due, chain re-seeding", models.NextActionDue, func(t *testing.T, pool *pgxpool.Pool, f *liveFixture, c uuid.UUID) {}},
		{"entry delay", models.NextActionWaiting, func(t *testing.T, pool *pgxpool.Pool, f *liveFixture, c uuid.UUID) {
			exec(t, pool, `UPDATE campaigns SET entry_delay_minutes = 180 WHERE id = $1`, f.campaign)
		}},
		{"start date", models.NextActionWaiting, func(t *testing.T, pool *pgxpool.Pool, f *liveFixture, c uuid.UUID) {
			exec(t, pool, `UPDATE campaigns SET start_date = NOW() + interval '2 days' WHERE id = $1`, f.campaign)
		}},
		{"sending window", models.NextActionWaiting, func(t *testing.T, pool *pgxpool.Pool, f *liveFixture, c uuid.UUID) {
			h := (time.Now().UTC().Hour() + 4) % 24
			exec(t, pool, `UPDATE campaigns SET start_time = $2, end_time = $3 WHERE id = $1`,
				f.campaign, fmt.Sprintf("%02d:00", h), fmt.Sprintf("%02d:30", h))
		}},
		{"mailbox minimum gap", models.NextActionWaiting, func(t *testing.T, pool *pgxpool.Pool, f *liveFixture, c uuid.UUID) {
			exec(t, pool, `UPDATE email_accounts SET min_wait_time = 5400 WHERE id = $1`, f.mailbox)
			sent(t, pool, f.mailbox)
		}},
		{"daily budget spent", models.NextActionWaiting, func(t *testing.T, pool *pgxpool.Pool, f *liveFixture, c uuid.UUID) {
			exec(t, pool, `UPDATE email_accounts SET campaign_limit = 1 WHERE id = $1`, f.mailbox)
			sent(t, pool, f.mailbox)
		}},
		{"daily new-lead cap", models.NextActionWaiting, func(t *testing.T, pool *pgxpool.Pool, f *liveFixture, c uuid.UUID) {
			exec(t, pool, `UPDATE campaigns SET max_new_leads_per_day = 1 WHERE id = $1`, f.campaign)
			exec(t, pool, `INSERT INTO campaign_daily_sends (campaign_id, send_date, emails_sent, new_leads_started)
			    VALUES ($1, CURRENT_DATE, 1, 1)`, f.campaign)
			exec(t, pool, `UPDATE email_accounts SET campaign_limit = 1 WHERE id = $1`, f.mailbox)
			sent(t, pool, f.mailbox)
		}},
		{"no same-provider mailbox", models.NextActionWaiting, func(t *testing.T, pool *pgxpool.Pool, f *liveFixture, c uuid.UUID) {
			exec(t, pool, `UPDATE contacts SET email = $2 WHERE id = $1`, c, "strict-"+c.String()[:8]+"@gmail.com")
			exec(t, pool, `UPDATE campaigns SET esp_match_mode = 'strict' WHERE id = $1`, f.campaign)
		}},
		// Rotation is a draw. Two mailboxes whose gaps differ by an hour, so a
		// read that re-rolled rotation answered an hour apart.
		{"a pool, not one mailbox", models.NextActionWaiting, func(t *testing.T, pool *pgxpool.Pool, f *liveFixture, c uuid.UUID) {
			exec(t, pool, `UPDATE campaigns SET rotation_mode = 'weighted' WHERE id = $1`, f.campaign)
			second := uuid.New()
			exec(t, pool, `INSERT INTO email_accounts (id, user_id, organization_id, email, name,
			        signature_plain, signature_html, provider, status, campaign_limit, min_wait_time, timezone)
			    VALUES ($1, $2, $3, $4, 'Live Two', '', '', 'smtp_imap', 'active', 50, 7200, 'UTC')`,
				second, f.user, f.org, "live2-"+second.String()[:8]+"@test.local")
			t.Cleanup(func() {
				c := context.Background()
				for _, sql := range []string{
					`DELETE FROM campaign_tasks WHERE task_id IN (SELECT id FROM tasks WHERE email_account_id = $1)`,
					`DELETE FROM tasks WHERE email_account_id = $1`,
					`DELETE FROM email_account_daily_plan WHERE email_account_id = $1`,
					`DELETE FROM email_account_behavior WHERE email_account_id = $1`,
					`DELETE FROM email_accounts WHERE id = $1`,
				} {
					if _, err := pool.Exec(c, sql, second); err != nil {
						t.Errorf("cleanup %q: %v", sql, err)
					}
				}
			})
			exec(t, pool, `UPDATE email_accounts SET min_wait_time = 3600 WHERE id = $1`, f.mailbox)
			sent(t, pool, f.mailbox)
			sent(t, pool, second)
		}},
		// A behaviour profile draws the send-to-send gap from a range on every
		// pass. The floor has to clear the not-due grace or the step reads as
		// due and carries no not-before to compare.
		{"behaviour profile gap", models.NextActionWaiting, func(t *testing.T, pool *pgxpool.Pool, f *liveFixture, c uuid.UUID) {
			b := liveProfile()
			b.GapMinSeconds, b.GapMaxSeconds = 600, 7200
			b.WorkStartMin, b.WorkStartMax = 0, 0
			b.WorkEndMin, b.WorkEndMax = 24*60-1, 24*60-1
			b.LunchEnabled = false
			b.Weekdays = models.BehaviorWeekdaysAll
			f.setProfile(t, b)
			sent(t, pool, f.mailbox)
		}},
		{"no mailbox at all", models.NextActionBlocked, func(t *testing.T, pool *pgxpool.Pool, f *liveFixture, c uuid.UUID) {
			exec(t, pool, `UPDATE email_accounts SET status = 'inactive' WHERE id = $1`, f.mailbox)
		}},
		{"past its end date", models.NextActionBlocked, func(t *testing.T, pool *pgxpool.Pool, f *liveFixture, c uuid.UUID) {
			exec(t, pool, `UPDATE campaigns SET end_date = NOW() - interval '1 day' WHERE id = $1`, f.campaign)
		}},
		{"campaign paused", models.NextActionPaused, func(t *testing.T, pool *pgxpool.Pool, f *liveFixture, c uuid.UUID) {
			exec(t, pool, `UPDATE campaigns SET status = 'paused' WHERE id = $1`, f.campaign)
		}},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			handle, pool := liveDB(t)
			f := newLiveFixture(t, pool, "UTC")
			_, contact := liveLead(t, pool, f.campaign)
			tc.setup(t, pool, f, contact)

			p := livePreviewer(t, liveScheduler(t, handle, pool))
			type answer struct {
				state, constraint      string
				scheduledAt, notBefore string
			}
			show := func(x *time.Time) string {
				if x == nil {
					return "none"
				}
				return x.UTC().Format(time.RFC3339Nano)
			}
			var first answer
			for i := 0; i < 8; i++ {
				pv, err := p.PreviewContactSend(context.Background(), f.campaign, contact)
				if err != nil {
					t.Fatalf("preview %d: %v", i, err)
				}
				if pv.State != tc.state {
					t.Fatalf("read %d: state = %q, want %q (constraint %q)", i, pv.State, tc.state, pv.Constraint)
				}
				got := answer{string(pv.State), string(pv.Constraint), show(pv.ScheduledAt), show(pv.NotBefore)}
				if i == 0 {
					first = got
					continue
				}
				if got != first {
					t.Fatalf("read %d changed its answer with nothing about the campaign changed:\n  first: %+v\n  now:   %+v",
						i, first, got)
				}
			}
			t.Logf("stable over 8 reads: %+v", first)
		})
	}
}
