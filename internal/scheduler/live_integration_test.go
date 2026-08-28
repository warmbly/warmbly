package scheduler

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/app/behavior"
	"github.com/warmbly/warmbly/internal/app/warmupramp"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/encrypt"
	"github.com/warmbly/warmbly/internal/repository"
)

// Live scheduler checks against a real Postgres. Skipped unless WARMBLY_TEST_DB
// is set, so `go test ./...` stays hermetic; run them against the dev stack
// with:
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/scheduler/ -run Live -v
//
// These exist because every other test in this package stubs the database out.
// The thing most worth proving — that a sending-behaviour profile actually
// moves a real campaign's next send into the mailbox's rolled workday — can
// only be proven against the real query paths.

func liveDB(t *testing.T) (*db.DB, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("WARMBLY_TEST_DB")
	if dsn == "" {
		t.Skip("WARMBLY_TEST_DB not set")
	}
	handle, err := db.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { handle.Pool.Close() })
	if err := handle.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
	return handle, handle.Pool
}

// liveFixture builds an isolated org/user/mailbox/campaign/contact graph and
// returns the ids, removing the whole graph on cleanup.
type liveFixture struct {
	pool     *pgxpool.Pool
	user     uuid.UUID
	org      uuid.UUID
	mailbox  uuid.UUID
	campaign uuid.UUID
}

func newLiveFixture(t *testing.T, pool *pgxpool.Pool, timezone string) *liveFixture {
	t.Helper()
	ctx := context.Background()
	f := &liveFixture{
		pool: pool, user: uuid.New(), org: uuid.New(),
		mailbox: uuid.New(), campaign: uuid.New(),
	}

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture %q: %v", sql[:min(60, len(sql))], err)
		}
	}

	exec(`INSERT INTO users (id, email, first_name, last_name)
	      VALUES ($1, $2, 'Live', 'Test')`, f.user, "live-"+f.user.String()[:8]+"@test.local")
	exec(`INSERT INTO organizations (id, name, slug, owner_user_id)
	      VALUES ($1, 'Live Test', $2, $3)`, f.org, "live-"+f.org.String()[:8], f.user)
	exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name,
	          signature_plain, signature_html, provider, status, campaign_limit, min_wait_time, timezone)
	      VALUES ($1, $2, $3, $4, 'Live', '', '', 'smtp_imap', 'active', 50, 600, $5)`,
		f.mailbox, f.user, f.org, "live-"+f.mailbox.String()[:8]+"@test.local", timezone)

	// An always-open campaign window, so anything the test observes about
	// timing comes from the mailbox profile and not the campaign schedule.
	exec(`INSERT INTO campaigns (id, user_id, organization_id, name, description, status,
	          daily_limit, timezone, days, start_time, end_time, rotation_mode, updated_at, created_at)
	      VALUES ($1, $2, $3, 'Live Test', '', 'active', 50, 'UTC', 127, '00:00', '23:59',
	              'least_recently_used', NOW(), NOW())`, f.campaign, f.user, f.org)

	seq := uuid.New()
	exec(`INSERT INTO sequences (id, campaign_id, organization_id, name, subject,
	          body_plain, body_html, wait_after, position, kind)
	      VALUES ($1, $2, $3, 'Step 1', 'Hi', 'Hello', '<p>Hello</p>', 0, 0, 'email')`, seq, f.campaign, f.org)

	contact := uuid.New()
	exec(`INSERT INTO contacts (id, user_id, organization_id, email, first_name, last_name, company, phone, custom_fields)
	      VALUES ($1, $2, $3, $4, 'Live', 'Contact', '', '', '{}')`,
		contact, f.user, f.org, "lead-"+contact.String()[:8]+"@test.local")
	exec(`INSERT INTO campaign_leads (campaign_id, contact_id, position) VALUES ($1, $2, 0)`, f.campaign, contact)

	// Teardown in FK order, innermost first. Each statement carries ONLY the
	// argument it uses: pgx rejects a call whose argument count does not match
	// the placeholders, and the first version of this passed all four ids to
	// every statement, so every delete failed — silently, because the errors
	// were discarded. Surfacing them is what makes the leak impossible to miss.
	t.Cleanup(func() {
		c := context.Background()
		steps := []struct {
			sql string
			arg any
		}{
			{`DELETE FROM campaign_tasks WHERE task_id IN (SELECT id FROM tasks WHERE email_account_id = $1)`, f.mailbox},
			{`DELETE FROM tasks WHERE email_account_id = $1`, f.mailbox},
			{`DELETE FROM campaign_contact_progress WHERE campaign_id = $1`, f.campaign},
			{`DELETE FROM campaign_leads WHERE campaign_id = $1`, f.campaign},
			{`DELETE FROM sequences WHERE campaign_id = $1`, f.campaign},
			{`DELETE FROM campaigns WHERE id = $1`, f.campaign},
			{`DELETE FROM email_account_daily_plan WHERE email_account_id = $1`, f.mailbox},
			{`DELETE FROM email_account_behavior WHERE email_account_id = $1`, f.mailbox},
			{`DELETE FROM email_accounts WHERE id = $1`, f.mailbox},
			{`DELETE FROM contacts WHERE organization_id = $1`, f.org},
			{`DELETE FROM organizations WHERE id = $1`, f.org},
			{`DELETE FROM users WHERE id = $1`, f.user},
		}
		for _, step := range steps {
			if _, err := pool.Exec(c, step.sql, step.arg); err != nil {
				t.Errorf("cleanup %q: %v", step.sql, err)
			}
		}
	})
	return f
}

// setProfile writes a sending-behaviour profile and clears any plan already
// rolled for the mailbox, so the next scheduling pass rolls from these ranges.
func (f *liveFixture) setProfile(t *testing.T, b models.SendingBehavior) {
	t.Helper()
	if err := b.Validate(); err != nil {
		t.Fatalf("profile is invalid before we even store it: %v", err)
	}
	repo := repository.NewBehaviorRepository(f.pool)
	b.EmailAccountID = f.mailbox
	if _, err := repo.UpsertBehavior(context.Background(), &b); err != nil {
		t.Fatalf("upsert profile: %v", err)
	}
	if _, err := f.pool.Exec(context.Background(),
		`DELETE FROM email_account_daily_plan WHERE email_account_id = $1`, f.mailbox); err != nil {
		t.Fatalf("clear plans: %v", err)
	}
}

func liveScheduler(t *testing.T, handle *db.DB, pool *pgxpool.Pool) SchedulerService {
	t.Helper()
	// Any 32-byte key works: these fixtures store no sealed credentials, and
	// the scheduler never decrypts. It is only needed to build the repository.
	enc, err := encrypt.NewEncrypter([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("encrypter: %v", err)
	}
	emailRepo := repository.NewEmailRepostory(handle, enc)
	s := NewSchedulerService(
		repository.NewTaskRepository(pool),
		repository.NewWarmupRepository(pool),
		repository.NewCampaignProgressRepository(pool),
		emailRepo,
		repository.NewCampaignRepostory(handle),
		repository.NewContactRepostory(handle),
		nil,
	)
	if aware, ok := s.(BehaviorAware); ok {
		aware.WireBehavior(behavior.NewService(repository.NewBehaviorRepository(pool), emailRepo))
	}
	// Send-time optimization is off in the defaults, so wiring this changes
	// nothing until a test writes an enabled settings row for its org.
	if aware, ok := s.(OutreachAware); ok {
		aware.WireOutreach(repository.NewAdvancedOutreachRepository(pool))
	}
	return s
}

// scheduleSlot runs a scheduling pass and returns the computed slot. A pass
// whose slot sits beyond the not-due grace now (correctly) reports
// ErrCampaignDeferred instead of handing back a sendable pair; for these
// placement assertions the slot is what matters, so both outcomes pass.
func scheduleSlot(t *testing.T, s SchedulerService, campaign uuid.UUID) (time.Time, uuid.UUID) {
	t.Helper()
	at, _, accountID, err := s.CalculateNextCampaignTime(context.Background(), campaign)
	if err != nil && !errors.Is(err, ErrCampaignDeferred) {
		t.Fatalf("schedule: %v", err)
	}
	return at, accountID
}

func liveProfile() models.SendingBehavior {
	b := models.DefaultSendingBehavior(uuid.Nil)
	b.Enabled = true
	return b
}

// TestLiveCampaignSendLandsInsideTheRolledWorkday is the headline check: a real
// campaign, a real mailbox, the real scheduler, and a send that has to land
// inside the workday the mailbox rolled for itself.
func TestLiveCampaignSendLandsInsideTheRolledWorkday(t *testing.T) {
	handle, pool := liveDB(t)
	f := newLiveFixture(t, pool, "UTC")

	// A narrow window that cannot be hit by accident: 15:20-15:25 to 16:00-16:05.
	b := liveProfile()
	b.WorkStartMin, b.WorkStartMax = 920, 925
	b.WorkEndMin, b.WorkEndMax = 960, 965
	b.LunchEnabled = false
	b.Weekdays = models.BehaviorWeekdaysAll
	f.setProfile(t, b)

	at, accountID := scheduleSlot(t, liveScheduler(t, handle, pool), f.campaign)
	assertFuture(t, at)
	if accountID != f.mailbox {
		t.Fatalf("picked mailbox %s, want the fixture's %s", accountID, f.mailbox)
	}

	plan := behavior.RollPlan(withID(b, f.mailbox), behavior.PlanDateFor(at, time.UTC))
	minute := behavior.MinuteOfDay(at, time.UTC)
	if !plan.Contains(minute) {
		t.Fatalf("send scheduled at %s (minute %d), outside the rolled workday %d-%d",
			at.UTC().Format("2006-01-02 15:04:05"), minute, plan.WorkStartMinute, plan.WorkEndMinute)
	}
	t.Logf("scheduled %s — inside the rolled workday %02d:%02d-%02d:%02d",
		at.UTC().Format("2006-01-02 15:04:05"),
		plan.WorkStartMinute/60, plan.WorkStartMinute%60, plan.WorkEndMinute/60, plan.WorkEndMinute%60)
}

// TestLiveSendSkipsTheLunchBreak proves the break is a real hole in the day and
// not just a number in a table.
func TestLiveSendSkipsTheLunchBreak(t *testing.T) {
	handle, pool := liveDB(t)
	f := newLiveFixture(t, pool, "UTC")

	// A 12:00-16:00 workday that is almost entirely break: lunch runs 12:02 to
	// 15:57, leaving only a couple of minutes open at each end. A scheduler
	// that ignored the break would land in the 235-minute hole rather than the
	// ~5 minutes of real window.
	b := liveProfile()
	b.WorkStartMin, b.WorkStartMax = 720, 722
	b.WorkEndMin, b.WorkEndMax = 960, 962
	b.LunchEnabled = true
	// 12:10-15:50, chosen so the break survives EVERY roll: its start is always
	// after the latest possible workday start (722) and its end always before
	// the earliest possible finish (960). RollPlan drops a break that does not
	// clear both, which is correct but makes a knife-edge fixture flaky.
	b.LunchEarliest, b.LunchLatest = 730, 730
	b.LunchMinMinutes, b.LunchMaxMinutes = 220, 220
	b.Weekdays = models.BehaviorWeekdaysAll
	f.setProfile(t, b)

	at, _ := scheduleSlot(t, liveScheduler(t, handle, pool), f.campaign)

	assertFuture(t, at)

	plan := behavior.RollPlan(withID(b, f.mailbox), behavior.PlanDateFor(at, time.UTC))
	if !plan.HasLunch() {
		t.Fatal("fixture should have produced a break")
	}
	minute := behavior.MinuteOfDay(at, time.UTC)
	if minute >= *plan.LunchStartMinute && minute < *plan.LunchEndMinute {
		t.Fatalf("send scheduled at minute %d, inside the break %d-%d",
			minute, *plan.LunchStartMinute, *plan.LunchEndMinute)
	}
	t.Logf("scheduled %s (minute %d), break is %d-%d — outside it",
		at.UTC().Format("2006-01-02 15:04:05"), minute, *plan.LunchStartMinute, *plan.LunchEndMinute)
}

// TestLiveNonWorkingDayRollsForward proves the weekday mask moves a send to the
// next working day rather than sending anyway.
func TestLiveNonWorkingDayRollsForward(t *testing.T) {
	handle, pool := liveDB(t)
	f := newLiveFixture(t, pool, "UTC")

	// Work only on the weekday two days from now, so today is never valid.
	target := time.Now().UTC().AddDate(0, 0, 2).Weekday()
	b := liveProfile()
	b.LunchEnabled = false
	b.Weekdays = 1 << ((int(target) + 6) % 7)
	f.setProfile(t, b)

	at, _ := scheduleSlot(t, liveScheduler(t, handle, pool), f.campaign)
	assertFuture(t, at)
	if at.UTC().Weekday() != target {
		t.Fatalf("scheduled on %s, want the only working day %s (%s)",
			at.UTC().Weekday(), target, at.UTC().Format("2006-01-02 15:04"))
	}
	t.Logf("scheduled %s — the only working day in the mask", at.UTC().Format("2006-01-02 15:04 Mon"))
}

// TestLiveTimezoneIsTheMailboxOwn proves the workday follows the mailbox's own
// clock rather than the server's.
func TestLiveTimezoneIsTheMailboxOwn(t *testing.T) {
	handle, pool := liveDB(t)
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	f := newLiveFixture(t, pool, "Asia/Tokyo")

	b := liveProfile()
	b.WorkStartMin, b.WorkStartMax = 600, 605 // 10:00-10:05 Tokyo
	b.WorkEndMin, b.WorkEndMax = 660, 665     // 11:00-11:05 Tokyo
	b.LunchEnabled = false
	b.Weekdays = models.BehaviorWeekdaysAll
	f.setProfile(t, b)

	at, _ := scheduleSlot(t, liveScheduler(t, handle, pool), f.campaign)

	assertFuture(t, at)

	local := behavior.MinuteOfDay(at, tokyo)
	if local < 600 || local >= 665 {
		t.Fatalf("scheduled at %s Tokyo (minute %d), outside the 10:00-11:05 Tokyo window",
			at.In(tokyo).Format("2006-01-02 15:04"), local)
	}
	t.Logf("scheduled %s Tokyo (%s UTC) — the mailbox's own clock, not the server's",
		at.In(tokyo).Format("15:04"), at.UTC().Format("15:04"))
}

// TestLiveDisabledProfileChangesNothing is the opt-out guarantee: a mailbox with
// no profile must schedule exactly as it did before this feature existed.
func TestLiveDisabledProfileChangesNothing(t *testing.T) {
	handle, pool := liveDB(t)
	f := newLiveFixture(t, pool, "UTC")

	// A profile that WOULD force a narrow window, but switched off.
	b := liveProfile()
	b.Enabled = false
	b.WorkStartMin, b.WorkStartMax = 920, 925
	b.WorkEndMin, b.WorkEndMax = 960, 965
	b.LunchEnabled = false
	f.setProfile(t, b)

	at, _ := scheduleSlot(t, liveScheduler(t, handle, pool), f.campaign)
	assertFuture(t, at)

	// With behaviour off the send is placed by the legacy path, which is free to
	// land outside 15:20-16:05. The guarantee we assert is that nothing was
	// persisted: a disabled profile must not roll or store a plan.
	var plans int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM email_account_daily_plan WHERE email_account_id = $1`, f.mailbox).Scan(&plans); err != nil {
		t.Fatalf("count plans: %v", err)
	}
	if plans != 0 {
		t.Fatalf("a disabled profile wrote %d plan rows; it must write none", plans)
	}
	t.Logf("scheduled %s with no plan persisted — legacy path intact", at.UTC().Format("2006-01-02 15:04"))
}

// assertFuture guards the invariant every one of these tests depends on: a
// scheduled slot must be in the future. A past instant fires immediately and
// silently defeats every window the profile describes, and the minute-of-day
// assertions below would still pass on one.
func assertFuture(t *testing.T, at time.Time) {
	t.Helper()
	if !at.After(time.Now().Add(-time.Second)) {
		t.Fatalf("scheduled %s, which is in the past (now %s)",
			at.UTC().Format("2006-01-02 15:04:05"), time.Now().UTC().Format("2006-01-02 15:04:05"))
	}
}

func withID(b models.SendingBehavior, id uuid.UUID) models.SendingBehavior {
	b.EmailAccountID = id
	return b
}

// bookTask inserts a pending campaign task for the fixture's mailbox at a given
// instant. Pending tasks count against the plan's budgets, because a booked
// slot has already spent it even though the mail has not gone out yet.
func (f *liveFixture) bookTask(t *testing.T, at time.Time) {
	t.Helper()
	id := uuid.New()
	if _, err := f.pool.Exec(context.Background(), `
		INSERT INTO tasks (id, task_type, email_account_id, status, message_id, scheduled_at)
		VALUES ($1, 'campaign', $2, 'pending', '', $3)`, id, f.mailbox, at); err != nil {
		t.Fatalf("book task: %v", err)
	}
	if _, err := f.pool.Exec(context.Background(),
		`INSERT INTO campaign_tasks (task_id, campaign_id) VALUES ($1, $2)`, id, f.campaign); err != nil {
		t.Fatalf("book campaign task: %v", err)
	}
}

// allDayProfile is an always-open workday, so the only thing that can move a
// send is a volume ceiling.
func allDayProfile() models.SendingBehavior {
	b := liveProfile()
	b.WorkStartMin, b.WorkStartMax = 0, 1
	b.WorkEndMin, b.WorkEndMax = 1438, 1439
	b.LunchEnabled = false
	b.Weekdays = models.BehaviorWeekdaysAll
	return b
}

// TestLiveHourlyCeilingPushesToALaterHour proves the hourly cap is a real gate
// and not just a stored number. Without it a mailbox could empty its whole day
// into one hour and then sit silent, which is a louder pattern than volume.
func TestLiveHourlyCeilingPushesToALaterHour(t *testing.T) {
	handle, pool := liveDB(t)
	f := newLiveFixture(t, pool, "UTC")

	b := allDayProfile()
	b.HourlyLimitMin, b.HourlyLimitMax = 1, 1 // one send per clock hour
	b.DailyLimitMin, b.DailyLimitMax = 40, 40
	f.setProfile(t, b)

	// Spend this hour's single slot.
	now := time.Now().UTC()
	f.bookTask(t, now)

	at, _ := scheduleSlot(t, liveScheduler(t, handle, pool), f.campaign)
	assertFuture(t, at)

	if at.UTC().Truncate(time.Hour).Equal(now.Truncate(time.Hour)) {
		t.Fatalf("scheduled %s — same hour as the slot already booked (%s); the hourly ceiling did not bind",
			at.UTC().Format("2006-01-02 15:04"), now.Format("15:04"))
	}
	t.Logf("hour %02d was full, next send placed at %s", now.Hour(), at.UTC().Format("2006-01-02 15:04"))
}

// TestLiveDailyCeilingPushesToTheNextDay proves the day's rolled cold budget is
// enforced, not decorative.
func TestLiveDailyCeilingPushesToTheNextDay(t *testing.T) {
	handle, pool := liveDB(t)
	f := newLiveFixture(t, pool, "UTC")

	b := allDayProfile()
	b.DailyLimitMin, b.DailyLimitMax = 1, 1 // one cold send per day
	b.HourlyLimitMin, b.HourlyLimitMax = 50, 50
	f.setProfile(t, b)

	now := time.Now().UTC()
	f.bookTask(t, now) // spends the whole day

	at, _ := scheduleSlot(t, liveScheduler(t, handle, pool), f.campaign)
	assertFuture(t, at)

	if at.UTC().Format("2006-01-02") == now.Format("2006-01-02") {
		t.Fatalf("scheduled %s — same day as the send already booked; the daily ceiling did not bind",
			at.UTC().Format("2006-01-02 15:04"))
	}
	t.Logf("today's single slot was spent, next send placed on %s", at.UTC().Format("2006-01-02 15:04"))
}

// TestLiveGapIsDrawnFromTheProfile proves the send-to-send spacing comes from
// the profile band rather than the mailbox's fixed min_wait_time.
//
// The probe has to survive the ±20 minute jitter every campaign slot carries,
// so it uses a gap FAR ABOVE the fixed 600s: a one-hour band cannot produce a
// slot less than 40 minutes out, while the 600s path cannot produce one more
// than ~30 minutes out. The two are cleanly separable; a narrow band is not.
func TestLiveGapIsDrawnFromTheProfile(t *testing.T) {
	handle, pool := liveDB(t)
	f := newLiveFixture(t, pool, "UTC")

	b := allDayProfile()
	b.GapMinSeconds, b.GapMaxSeconds = 3600, 3600 // exactly one hour
	b.DailyLimitMin, b.DailyLimitMax = 40, 40
	b.HourlyLimitMin, b.HourlyLimitMax = 50, 50
	f.setProfile(t, b)

	// A send completed a moment ago, so the next slot is governed by the gap.
	id := uuid.New()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO tasks (id, task_type, email_account_id, status, message_id, scheduled_at, completed_at)
		VALUES ($1, 'campaign', $2, 'completed', '', NOW(), NOW())`, id, f.mailbox); err != nil {
		t.Fatalf("seed completed task: %v", err)
	}

	at, _ := scheduleSlot(t, liveScheduler(t, handle, pool), f.campaign)
	assertFuture(t, at)

	gap := time.Until(at)
	if gap < 40*time.Minute {
		t.Fatalf("next send is only %s away; a one-hour profile gap cannot produce that, so the fixed 600s min_wait_time is still in charge",
			gap.Round(time.Second))
	}
	t.Logf("next send %s away — the profile's one-hour band, not the mailbox's 600s gap", gap.Round(time.Second))
}

// TestLiveFollowUpWaitIsHonored is the regression check for follow-ups riding
// an early tick (issue #171 follow-up): once step 1 is sent, a scheduling pass
// that runs immediately afterwards must NOT hand back the "wait 3 days" step as
// sendable. It must report the campaign deferred, with the slot parked at the
// follow-up's real time, so the task handler reschedules instead of sending a
// three-day follow-up seconds after step one.
func TestLiveFollowUpWaitIsHonored(t *testing.T) {
	handle, pool := liveDB(t)
	f := newLiveFixture(t, pool, "UTC")
	ctx := context.Background()

	step2 := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO sequences (id, campaign_id, organization_id, name, subject,
			body_plain, body_html, wait_after, position, kind)
		VALUES ($1, $2, $3, 'Step 2', 'Bump', 'Bump', '<p>Bump</p>', 3, 1, 'email')`,
		step2, f.campaign, f.org); err != nil {
		t.Fatalf("insert step 2: %v", err)
	}

	var step1, contact uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT id FROM sequences WHERE campaign_id = $1 AND position = 0`, f.campaign).Scan(&step1); err != nil {
		t.Fatalf("load step 1: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT contact_id FROM campaign_leads WHERE campaign_id = $1`, f.campaign).Scan(&contact); err != nil {
		t.Fatalf("load contact: %v", err)
	}

	// Connect step 1 -> step 2 (routing has no implicit next step; the wizard
	// writes this catch-all branch at creation).
	if _, err := pool.Exec(ctx, `
		UPDATE sequences SET conditions = jsonb_build_object('branches', jsonb_build_array(
			jsonb_build_object('branch_id', 'live-else', 'target_step_id', $1::text)))
		WHERE campaign_id = $2 AND position = 0`, step2.String(), f.campaign); err != nil {
		t.Fatalf("connect steps: %v", err)
	}

	// Step 1 went out moments ago; routing now points at the wait-gated step 2.
	if _, err := pool.Exec(ctx, `
		INSERT INTO campaign_contact_progress (campaign_id, contact_id, sequence_id, sent_at)
		VALUES ($1, $2, $3, NOW())`, f.campaign, contact, step1); err != nil {
		t.Fatalf("stamp step 1 sent: %v", err)
	}

	at, pair, _, err := liveScheduler(t, handle, pool).CalculateNextCampaignTime(ctx, f.campaign)
	if !errors.Is(err, ErrCampaignDeferred) {
		t.Fatalf("want ErrCampaignDeferred for a not-yet-due follow-up, got pair=%v err=%v at=%s", pair, err, at)
	}
	if pair != nil {
		t.Fatal("a deferred result must not carry a sendable pair")
	}
	if time.Until(at) < 47*time.Hour {
		t.Fatalf("deferred slot %s is too soon for a 3-day wait", at)
	}
	t.Logf("follow-up correctly deferred to %s", at.UTC().Format("2006-01-02 15:04:05"))
}

// TestLiveWaitingFollowUpDoesNotBlockOtherLeads is the multi-lead stall from
// issue #171: lead A got step 1 and is routed to a "wait 3 days" follow-up,
// lead B has never been sent. B's first email is due now and must be picked,
// instead of the campaign parking on A's follow-up with B queued behind it.
func TestLiveWaitingFollowUpDoesNotBlockOtherLeads(t *testing.T) {
	handle, pool := liveDB(t)
	f := newLiveFixture(t, pool, "UTC")
	ctx := context.Background()

	step2 := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO sequences (id, campaign_id, organization_id, name, subject,
			body_plain, body_html, wait_after, position, kind)
		VALUES ($1, $2, $3, 'Step 2', 'Bump', 'Bump', '<p>Bump</p>', 3, 1, 'email')`,
		step2, f.campaign, f.org); err != nil {
		t.Fatalf("insert step 2: %v", err)
	}
	var step1, leadA uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT id FROM sequences WHERE campaign_id = $1 AND position = 0`, f.campaign).Scan(&step1); err != nil {
		t.Fatalf("load step 1: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT contact_id FROM campaign_leads WHERE campaign_id = $1`, f.campaign).Scan(&leadA); err != nil {
		t.Fatalf("load lead A: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE sequences SET conditions = jsonb_build_object('branches', jsonb_build_array(
			jsonb_build_object('branch_id', 'live-else', 'target_step_id', $1::text)))
		WHERE campaign_id = $2 AND position = 0`, step2.String(), f.campaign); err != nil {
		t.Fatalf("connect steps: %v", err)
	}

	// Lead B joins after A (created_at ordering puts A first) and has never
	// been sent anything.
	leadB := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO contacts (id, user_id, organization_id, email, first_name, last_name, company, phone, custom_fields, created_at)
		VALUES ($1, $2, $3, $4, 'Live', 'Second', '', '', '{}', NOW() + interval '1 second')`,
		leadB, f.user, f.org, "lead-"+leadB.String()[:8]+"@test.local"); err != nil {
		t.Fatalf("insert lead B: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO campaign_leads (campaign_id, contact_id, position) VALUES ($1, $2, 1)`,
		f.campaign, leadB); err != nil {
		t.Fatalf("add lead B: %v", err)
	}

	// A's step 1 went out moments ago; A now routes to the wait-gated step 2.
	if _, err := pool.Exec(ctx, `
		INSERT INTO campaign_contact_progress (campaign_id, contact_id, sequence_id, sent_at)
		VALUES ($1, $2, $3, NOW())`, f.campaign, leadA, step1); err != nil {
		t.Fatalf("stamp A step 1: %v", err)
	}

	s := liveScheduler(t, handle, pool)
	at, pair, _, err := s.CalculateNextCampaignTime(ctx, f.campaign)
	if err != nil {
		t.Fatalf("want B's first step now, got err=%v at=%s", err, at)
	}
	if pair == nil || pair.ContactID != leadB || pair.SequenceID != step1 || !pair.IsNewLead {
		t.Fatalf("want lead B / step 1 as a new lead, got %+v", pair)
	}

	// Once B has had step 1 too, nothing is due for 3 days: the campaign
	// defers to then instead of completing or sending a follow-up early.
	if _, err := pool.Exec(ctx, `
		INSERT INTO campaign_contact_progress (campaign_id, contact_id, sequence_id, sent_at)
		VALUES ($1, $2, $3, NOW())`, f.campaign, leadB, step1); err != nil {
		t.Fatalf("stamp B step 1: %v", err)
	}
	at, pair, _, err = s.CalculateNextCampaignTime(ctx, f.campaign)
	if !errors.Is(err, ErrCampaignDeferred) || pair != nil {
		t.Fatalf("want ErrCampaignDeferred with no pair, got pair=%v err=%v", pair, err)
	}
	if until := time.Until(at); until < 71*time.Hour || until > 73*time.Hour {
		t.Fatalf("deferred slot %s is not ~3 days out", at)
	}
}

// TestLiveWaitNodeGatesTheStepAfterIt: a contact who passed through a "wait 90
// minutes" node must not be routed onward by a tick that fires before the
// delay elapses (multi-lead campaigns tick constantly).
func TestLiveWaitNodeGatesTheStepAfterIt(t *testing.T) {
	handle, pool := liveDB(t)
	f := newLiveFixture(t, pool, "UTC")
	ctx := context.Background()

	waitNode, step2 := uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO sequences (id, campaign_id, organization_id, name, subject,
			body_plain, body_html, wait_after, position, kind, action, conditions)
		VALUES ($1, $2, $3, 'Wait', '', '', '', 0, 1, 'wait',
			'{"type":"wait","wait_minutes":90}',
			jsonb_build_object('branches', jsonb_build_array(
				jsonb_build_object('branch_id', 'live-else', 'target_step_id', $5::text)))),
		       ($4, $2, $3, 'Step 2', 'Bump', 'Bump', '<p>Bump</p>', 0, 2, 'email', '{}', '{}')`,
		waitNode, f.campaign, f.org, step2, step2.String()); err != nil {
		t.Fatalf("insert wait node + step 2: %v", err)
	}
	var contact uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT contact_id FROM campaign_leads WHERE campaign_id = $1`, f.campaign).Scan(&contact); err != nil {
		t.Fatalf("load contact: %v", err)
	}
	// step 1 -> wait -> step 2; the contact just passed the wait node.
	if _, err := pool.Exec(ctx, `
		UPDATE sequences SET conditions = jsonb_build_object('branches', jsonb_build_array(
			jsonb_build_object('branch_id', 'live-else', 'target_step_id', $1::text)))
		WHERE campaign_id = $2 AND position = 0`, waitNode.String(), f.campaign); err != nil {
		t.Fatalf("connect steps: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO campaign_contact_progress (campaign_id, contact_id, sequence_id, sent_at)
		VALUES ($1, $2, (SELECT id FROM sequences WHERE campaign_id = $1 AND position = 0), NOW() - interval '1 minute'),
		       ($1, $2, $3, NOW())`, f.campaign, contact, waitNode); err != nil {
		t.Fatalf("stamp progress: %v", err)
	}

	at, pair, _, err := liveScheduler(t, handle, pool).CalculateNextCampaignTime(ctx, f.campaign)
	if !errors.Is(err, ErrCampaignDeferred) || pair != nil {
		t.Fatalf("want ErrCampaignDeferred with no pair while the wait runs, got pair=%v err=%v", pair, err)
	}
	if until := time.Until(at); until < 85*time.Minute || until > 95*time.Minute {
		t.Fatalf("deferred slot %s is not ~90 minutes out", at)
	}
}

// TestLiveInFlightSendIsNotOfferedAgain is issue #169 at the scheduler level:
// lead A's first email is on the bus but its sent_at stamp never landed (the
// tick died, or the progress write failed). The scheduler must move on to lead
// B instead of handing A's step back and emailing them twice.
func TestLiveInFlightSendIsNotOfferedAgain(t *testing.T) {
	handle, pool := liveDB(t)
	f := newLiveFixture(t, pool, "UTC")
	ctx := context.Background()

	var step1, leadA uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT id FROM sequences WHERE campaign_id = $1 AND position = 0`, f.campaign).Scan(&step1); err != nil {
		t.Fatalf("load step 1: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT contact_id FROM campaign_leads WHERE campaign_id = $1`, f.campaign).Scan(&leadA); err != nil {
		t.Fatalf("load lead A: %v", err)
	}

	leadB := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO contacts (id, user_id, organization_id, email, first_name, last_name, company, phone, custom_fields, created_at)
		VALUES ($1, $2, $3, $4, 'Live', 'Second', '', '', '{}', NOW() + interval '1 second')`,
		leadB, f.user, f.org, "lead-"+leadB.String()[:8]+"@test.local"); err != nil {
		t.Fatalf("insert lead B: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO campaign_leads (campaign_id, contact_id, position) VALUES ($1, $2, 1)`,
		f.campaign, leadB); err != nil {
		t.Fatalf("add lead B: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM campaign_leads WHERE contact_id = $1`, leadB)
		_, _ = pool.Exec(context.Background(), `DELETE FROM contacts WHERE id = $1`, leadB)
	})

	// A's send was reserved and dispatched; the stamp never landed.
	progress := repository.NewCampaignProgressRepository(pool)
	reserved, err := progress.ReserveSend(ctx, f.campaign, leadA, step1, uuid.New(), true)
	if err != nil || !reserved {
		t.Fatalf("reserve A's send: reserved=%v err=%v", reserved, err)
	}

	s := liveScheduler(t, handle, pool)
	_, pair, _, err := s.CalculateNextCampaignTime(ctx, f.campaign)
	if err != nil {
		t.Fatalf("want lead B's first step, got err=%v", err)
	}
	if pair == nil || pair.ContactID != leadB {
		t.Fatalf("want lead B, got %+v (A's in-flight send was offered again: issue #169)", pair)
	}

	// With B dispatched too, nothing is left: the campaign completes rather than
	// re-offering either in-flight step.
	if reserved, err := progress.ReserveSend(ctx, f.campaign, leadB, step1, uuid.New(), true); err != nil || !reserved {
		t.Fatalf("reserve B's send: reserved=%v err=%v", reserved, err)
	}
	if _, pair, _, err = s.CalculateNextCampaignTime(ctx, f.campaign); !errors.Is(err, ErrCampaignCompleted) || pair != nil {
		t.Fatalf("want ErrCampaignCompleted with no pair once both sends are in flight, got pair=%v err=%v", pair, err)
	}
}

// TestLiveInFlightFollowUpIsNotOfferedAgain covers the same window one step
// further in: the follow-up, not the first email, is the one whose stamp was
// lost. Routing reaches it through the branch, so the loop guard is what has to
// stop it.
func TestLiveInFlightFollowUpIsNotOfferedAgain(t *testing.T) {
	handle, pool := liveDB(t)
	f := newLiveFixture(t, pool, "UTC")
	ctx := context.Background()

	step2 := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO sequences (id, campaign_id, organization_id, name, subject,
			body_plain, body_html, wait_after, position, kind)
		VALUES ($1, $2, $3, 'Step 2', 'Bump', 'Bump', '<p>Bump</p>', 0, 1, 'email')`,
		step2, f.campaign, f.org); err != nil {
		t.Fatalf("insert step 2: %v", err)
	}
	var step1, contact uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT id FROM sequences WHERE campaign_id = $1 AND position = 0`, f.campaign).Scan(&step1); err != nil {
		t.Fatalf("load step 1: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT contact_id FROM campaign_leads WHERE campaign_id = $1`, f.campaign).Scan(&contact); err != nil {
		t.Fatalf("load contact: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE sequences SET conditions = jsonb_build_object('branches', jsonb_build_array(
			jsonb_build_object('branch_id', 'live-else', 'target_step_id', $1::text)))
		WHERE campaign_id = $2 AND position = 0`, step2.String(), f.campaign); err != nil {
		t.Fatalf("connect steps: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO campaign_contact_progress (campaign_id, contact_id, sequence_id, sent_at, dispatched_at)
		VALUES ($1, $2, $3, NOW(), NOW())`, f.campaign, contact, step1); err != nil {
		t.Fatalf("stamp step 1 sent: %v", err)
	}

	progress := repository.NewCampaignProgressRepository(pool)
	s := liveScheduler(t, handle, pool)

	// The follow-up is due now (wait_after 0) and would be sent...
	if _, pair, _, err := s.CalculateNextCampaignTime(ctx, f.campaign); err != nil || pair == nil || pair.SequenceID != step2 {
		t.Fatalf("precondition: step 2 should be due, got pair=%v err=%v", pair, err)
	}
	// ...so dispatch it, and lose the stamp.
	if reserved, err := progress.ReserveSend(ctx, f.campaign, contact, step2, uuid.New(), false); err != nil || !reserved {
		t.Fatalf("reserve the follow-up: reserved=%v err=%v", reserved, err)
	}
	if _, pair, _, err := s.CalculateNextCampaignTime(ctx, f.campaign); !errors.Is(err, ErrCampaignCompleted) || pair != nil {
		t.Fatalf("the in-flight follow-up was offered again: pair=%v err=%v", pair, err)
	}
}

// enableSendTimeOptimization writes an org settings row turning recipient-hour
// scheduling on, and removes it again on cleanup.
func (f *liveFixture) enableSendTimeOptimization(t *testing.T, hours []int, avoidWeekends bool) {
	t.Helper()
	ctx := context.Background()
	settings := models.DefaultAdvancedOutreachSettings()
	settings.SendTimeOptimization.Enabled = true
	settings.SendTimeOptimization.UseContactTimezone = true
	settings.SendTimeOptimization.DefaultContactTimezone = "UTC"
	settings.SendTimeOptimization.PreferredHours = hours
	settings.SendTimeOptimization.WeekendWeightMultiplier = 1
	if avoidWeekends {
		settings.SendTimeOptimization.WeekendWeightMultiplier = 0.5
	}
	repo := repository.NewAdvancedOutreachRepository(f.pool)
	if err := repo.UpsertOutreachSettings(ctx, f.org, f.user, &settings); err != nil {
		t.Fatalf("write outreach settings: %v", err)
	}
	t.Cleanup(func() {
		if _, err := f.pool.Exec(context.Background(),
			`DELETE FROM outreach_settings WHERE organization_id = $1`, f.org); err != nil {
			t.Errorf("cleanup outreach settings: %v", err)
		}
	})
}

// setContactTimezone stamps the fixture's single lead with a timezone field.
func (f *liveFixture) setContactTimezone(t *testing.T, tz string) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(),
		`UPDATE contacts SET custom_fields = jsonb_build_object('timezone', $2::text)
		 WHERE organization_id = $1`, f.org, tz); err != nil {
		t.Fatalf("set contact timezone: %v", err)
	}
}

// TestLiveRecipientTimezoneMovesTheSlot is issue #156 end to end: the setting
// was writable and documented but had no caller, so enabling it changed
// nothing. The slot must now land in the recipient's own morning, not the
// sending mailbox's.
func TestLiveRecipientTimezoneMovesTheSlot(t *testing.T) {
	handle, pool := liveDB(t)
	f := newLiveFixture(t, pool, "UTC")
	f.enableSendTimeOptimization(t, []int{9}, false)
	f.setContactTimezone(t, "America/Denver")

	at, _ := scheduleSlot(t, liveScheduler(t, handle, pool), f.campaign)

	loc, err := time.LoadLocation("America/Denver")
	if err != nil {
		t.Skip("tzdata unavailable")
	}
	if got := at.In(loc).Hour(); got != 9 {
		t.Errorf("send scheduled at %s (%02d:00 Denver), want the recipient's 9am",
			at.In(loc).Format(time.RFC3339), got)
	}
	if !at.After(time.Now()) {
		t.Errorf("slot %s is not in the future; optimization must delay, never backdate", at)
	}
}

// TestLiveSendTimeOptimizationOffLeavesTheSlotAlone pins the default: an org
// that never opted in keeps sending on the mailbox's clock. Without this, the
// wiring would silently re-time every existing workspace's campaigns.
func TestLiveSendTimeOptimizationOffLeavesTheSlotAlone(t *testing.T) {
	handle, pool := liveDB(t)
	f := newLiveFixture(t, pool, "UTC")
	f.setContactTimezone(t, "Asia/Tokyo")

	// No settings row at all — the repository hands back the defaults.
	at, _ := scheduleSlot(t, liveScheduler(t, handle, pool), f.campaign)

	// The campaign window is always open, so an unoptimized slot is imminent.
	// A snap to Tokyo's 9am would push it hours out.
	if time.Until(at) > 2*time.Hour {
		t.Errorf("slot %s is %v out; optimization must stay off by default",
			at, time.Until(at).Truncate(time.Minute))
	}
}

// TestLiveRecipientTimezoneNeverOutlivesTheCampaign proves the end-date guard:
// a recipient hour that falls past the campaign's end must not defer the send
// until the campaign simply expires unsent.
func TestLiveRecipientTimezoneNeverOutlivesTheCampaign(t *testing.T) {
	handle, pool := liveDB(t)
	f := newLiveFixture(t, pool, "UTC")
	// An hour that is always far away, plus an end date an hour out.
	f.enableSendTimeOptimization(t, []int{3}, false)
	f.setContactTimezone(t, "Asia/Tokyo")
	// end_date is `timestamp without time zone`, so it is written and read
	// back in UTC to keep the host's local offset out of the comparison.
	var end time.Time
	if err := pool.QueryRow(context.Background(),
		`UPDATE campaigns SET end_date = (NOW() AT TIME ZONE 'utc') + interval '1 hour'
		 WHERE id = $1 RETURNING end_date`, f.campaign).Scan(&end); err != nil {
		t.Fatalf("set end date: %v", err)
	}
	end = end.UTC()

	at, _ := scheduleSlot(t, liveScheduler(t, handle, pool), f.campaign)
	if at.UTC().After(end) {
		t.Errorf("slot %s is past the campaign end %s; the guard did not hold", at.UTC(), end)
	}
}

// TestLiveUnreachableRecipientHourStillSends is the livelock guard. When the
// recipient's preferred hours and the campaign's sending window never overlap,
// raising a hard floor at the recipient's hour would defer the send on every
// tick forever. The optimization has to yield instead.
func TestLiveUnreachableRecipientHourStillSends(t *testing.T) {
	handle, pool := liveDB(t)
	f := newLiveFixture(t, pool, "UTC")

	// The campaign may only send 12:00-13:00 UTC...
	if _, err := pool.Exec(context.Background(),
		`UPDATE campaigns SET start_time = '12:00', end_time = '13:00' WHERE id = $1`, f.campaign); err != nil {
		t.Fatalf("narrow the campaign window: %v", err)
	}
	// ...and the recipient may only be mailed at 09:00 Tokyo, which is 00:00
	// UTC. The two never meet.
	f.enableSendTimeOptimization(t, []int{9}, false)
	f.setContactTimezone(t, "Asia/Tokyo")

	at, _ := scheduleSlot(t, liveScheduler(t, handle, pool), f.campaign)

	// The slot must land in the campaign's window, not at the recipient hour
	// the sender can never serve.
	if h := at.UTC().Hour(); h != 12 {
		t.Errorf("slot %s is at %02d:00 UTC, want the campaign's 12:00 window",
			at.UTC().Format(time.RFC3339), h)
	}
	if time.Until(at) > 25*time.Hour {
		t.Errorf("slot %s is %v out; the send is being deferred rather than scheduled",
			at, time.Until(at).Truncate(time.Minute))
	}
}

// TestLiveReachableRecipientHourIsPreferred is the same shape with an overlap:
// the preference must actually be honored when the sender can serve it.
func TestLiveReachableRecipientHourIsPreferred(t *testing.T) {
	handle, pool := liveDB(t)
	f := newLiveFixture(t, pool, "UTC")

	// 09:00 Denver is 16:00 UTC in daylight time and 16:00 sits in this window.
	if _, err := pool.Exec(context.Background(),
		`UPDATE campaigns SET start_time = '06:00', end_time = '23:00' WHERE id = $1`, f.campaign); err != nil {
		t.Fatalf("widen the campaign window: %v", err)
	}
	f.enableSendTimeOptimization(t, []int{9}, false)
	f.setContactTimezone(t, "America/Denver")

	at, _ := scheduleSlot(t, liveScheduler(t, handle, pool), f.campaign)

	loc, err := time.LoadLocation("America/Denver")
	if err != nil {
		t.Skip("tzdata unavailable")
	}
	if h := at.In(loc).Hour(); h != 9 {
		t.Errorf("slot %s is %02d:00 Denver, want the recipient's 9am", at.In(loc), h)
	}
}

// graduateMailbox marks the fixture's mailbox as having warmed for daysWarming,
// and optionally anchors its cold ramp coldDaysAgo in the past.
func (f *liveFixture) graduateMailbox(t *testing.T, daysWarming int, coldDaysAgo *int) {
	t.Helper()
	var coldStart any
	if coldDaysAgo != nil {
		coldStart = time.Now().Add(-time.Duration(*coldDaysAgo) * 24 * time.Hour)
	}
	if _, err := f.pool.Exec(context.Background(),
		`UPDATE email_accounts
		    SET warmup = $2, warmup_paused_at = NULL, cold_ramp_started_at = $3
		  WHERE id = $1`,
		f.mailbox, time.Now().Add(-time.Duration(daysWarming)*24*time.Hour), coldStart); err != nil {
		t.Fatalf("graduate mailbox: %v", err)
	}
}

// dailyCapacity reports how many cold sends the scheduler will allow the
// fixture's mailbox today, which is what the graduation ceiling gates.
func (f *liveFixture) dailyCapacity(t *testing.T, handle *db.DB) int {
	t.Helper()
	repo := repository.NewWarmupRepository(f.pool)
	states, err := repo.ColdRampStateForAccounts(context.Background(),
		[]uuid.UUID{f.mailbox}, time.Now().Add(-warmupramp.LookbackWindow))
	if err != nil {
		t.Fatalf("cold ramp state: %v", err)
	}
	_ = handle
	return coldCeilingFor(states[f.mailbox], 50)
}

// TestLiveGraduationStopsTheOvernightJumpToFullCap is issue #147: a mailbox at
// its warmup ceiling joining a campaign must not reach the cold cap that day.
func TestLiveGraduationStopsTheOvernightJumpToFullCap(t *testing.T) {
	handle, pool := liveDB(t)
	f := newLiveFixture(t, pool, "UTC")
	f.graduateMailbox(t, 30, nil) // a month of warmup, first cold day

	if got := f.dailyCapacity(t, handle); got != 20 {
		t.Errorf("first cold day allows %d, want the graduation start of 20 rather than the 50 cap", got)
	}
}

func TestLiveGraduationClimbsToTheCap(t *testing.T) {
	handle, pool := liveDB(t)
	f := newLiveFixture(t, pool, "UTC")

	three := 3
	f.graduateMailbox(t, 30, &three)
	if got := f.dailyCapacity(t, handle); got != 35 {
		t.Errorf("cold day 3 allows %d, want 20 + 3*5", got)
	}

	long := 60
	f.graduateMailbox(t, 30, &long)
	if got := f.dailyCapacity(t, handle); got != 50 {
		t.Errorf("a long-graduated mailbox allows %d, want the full 50 cap", got)
	}
}

// A mailbox that never used warmup keeps its cap: gating it would cap customers
// who never opted into warmup, which is not what this gate is for.
func TestLiveGraduationDoesNotGateAMailboxThatNeverWarmed(t *testing.T) {
	handle, pool := liveDB(t)
	f := newLiveFixture(t, pool, "UTC")

	if got := f.dailyCapacity(t, handle); got != 50 {
		t.Errorf("a never-warmed mailbox allows %d, want its full 50 cap", got)
	}
}
