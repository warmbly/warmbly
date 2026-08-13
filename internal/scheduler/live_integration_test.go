package scheduler

import (
	"context"
	"os"
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
		nil,
		nil,
	)
	if aware, ok := s.(BehaviorAware); ok {
		aware.WireBehavior(behavior.NewService(repository.NewBehaviorRepository(pool), emailRepo))
	}
	return s
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

	at, _, accountID, err := liveScheduler(t, handle, pool).CalculateNextCampaignTime(context.Background(), f.campaign)
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
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

	at, _, _, err := liveScheduler(t, handle, pool).CalculateNextCampaignTime(context.Background(), f.campaign)
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}

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

	at, _, _, err := liveScheduler(t, handle, pool).CalculateNextCampaignTime(context.Background(), f.campaign)
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
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

	at, _, _, err := liveScheduler(t, handle, pool).CalculateNextCampaignTime(context.Background(), f.campaign)
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}

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

	at, _, _, err := liveScheduler(t, handle, pool).CalculateNextCampaignTime(context.Background(), f.campaign)
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
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

	at, _, _, err := liveScheduler(t, handle, pool).CalculateNextCampaignTime(context.Background(), f.campaign)
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
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

	at, _, _, err := liveScheduler(t, handle, pool).CalculateNextCampaignTime(context.Background(), f.campaign)
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
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

	at, _, _, err := liveScheduler(t, handle, pool).CalculateNextCampaignTime(context.Background(), f.campaign)
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	assertFuture(t, at)

	gap := time.Until(at)
	if gap < 40*time.Minute {
		t.Fatalf("next send is only %s away; a one-hour profile gap cannot produce that, so the fixed 600s min_wait_time is still in charge",
			gap.Round(time.Second))
	}
	t.Logf("next send %s away — the profile's one-hour band, not the mailbox's 600s gap", gap.Round(time.Second))
}
