package analytics

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/pkg/encrypt"
	"github.com/warmbly/warmbly/internal/repository"
)

// The warmup target the mailbox drawer shows has to be the target the
// scheduler will act on. It used to be recomputed here from a private copy of
// the ramp arithmetic, so an early-signal cut in the scheduler would have left
// the dashboard reporting a number the mailbox never sent.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/app/analytics/ -run Live -v

type rampFixture struct {
	pool    *pgxpool.Pool
	user    uuid.UUID
	org     uuid.UUID
	mailbox uuid.UUID
	svc     AnalyticsService
}

func newRampFixture(t *testing.T, daysWarming, base, increase, max int) *rampFixture {
	t.Helper()
	dsn := os.Getenv("WARMBLY_TEST_DB")
	if dsn == "" {
		t.Skip("WARMBLY_TEST_DB not set")
	}
	ctx := context.Background()
	handle, err := db.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { handle.Pool.Close() })

	f := &rampFixture{pool: handle.Pool, user: uuid.New(), org: uuid.New(), mailbox: uuid.New()}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := f.pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture %q: %v", sql[:min(60, len(sql))], err)
		}
	}
	exec(`INSERT INTO users (id, email, first_name, last_name) VALUES ($1, $2, 'Ramp', 'Test')`,
		f.user, "ramp-"+f.user.String()[:8]+"@test.local")
	exec(`INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Ramp Test', $2, $3)`,
		f.org, "ramp-"+f.org.String()[:8], f.user)
	exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain,
	          signature_html, provider, status, campaign_limit, min_wait_time, timezone,
	          warmup, warmup_base, warmup_increase, warmup_max)
	      VALUES ($1, $2, $3, $4, 'Ramp', '', '', 'smtp_imap', 'active', 50, 600, 'UTC',
	              $5, $6, $7, $8)`,
		f.mailbox, f.user, f.org, "ramp-"+f.mailbox.String()[:8]+"@test.local",
		time.Now().Add(-time.Duration(daysWarming)*24*time.Hour), base, increase, max)

	t.Cleanup(func() {
		c := context.Background()
		for _, step := range []struct {
			sql string
			arg any
		}{
			{`DELETE FROM warmup_spam_reports WHERE reported_account_id = $1`, f.mailbox},
			{`DELETE FROM warmup_statistics WHERE email_account_id = $1`, f.mailbox},
			{`DELETE FROM email_accounts WHERE id = $1`, f.mailbox},
			{`DELETE FROM organizations WHERE id = $1`, f.org},
			{`DELETE FROM users WHERE id = $1`, f.user},
		} {
			if _, err := f.pool.Exec(c, step.sql, step.arg); err != nil {
				t.Errorf("cleanup %q: %v", step.sql, err)
			}
		}
	})

	enc, err := encrypt.NewEncrypter([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("encrypter: %v", err)
	}
	f.svc = NewService(
		repository.NewAnalyticsRepository(handle),
		repository.NewEmailRepostory(handle, enc),
		repository.NewCampaignRepostory(handle),
		repository.NewEmailAccountErrorRepository(handle),
		repository.NewWarmupRepository(handle.Pool),
	)
	return f
}

func (f *rampFixture) placement(t *testing.T, hoursAgo int) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(),
		`INSERT INTO warmup_spam_reports (id, reporter_account_id, reported_account_id, message_id, report_type, created_at)
		 VALUES (gen_random_uuid(), $1, $1, $2, 'spam_placement', $3)`,
		f.mailbox, "msg-"+uuid.New().String(),
		time.Now().Add(-time.Duration(hoursAgo)*time.Hour)); err != nil {
		t.Fatalf("record placement: %v", err)
	}
}

func (f *rampFixture) status(t *testing.T) (int, bool) {
	t.Helper()
	st, xerr := f.svc.GetAccountStatus(context.Background(), f.org, f.mailbox)
	if xerr != nil {
		t.Fatalf("account status: %v", xerr.Message)
	}
	if st.WarmupStatus == nil {
		t.Fatal("no warmup status on a warming mailbox")
	}
	return st.WarmupStatus.TargetVolume, st.WarmupStatus.RampHold != nil
}

func TestLiveWarmupTargetIsCleanWithoutASignal(t *testing.T) {
	f := newRampFixture(t, 10, 10, 1, 40)
	target, held := f.status(t)
	if target != 20 {
		t.Errorf("target = %d, want the day-10 ramp of 20", target)
	}
	if held {
		t.Error("ramp reported as held with no placement on record")
	}
}

func TestLiveRecentPlacementCutsTheTargetAndExplainsItself(t *testing.T) {
	f := newRampFixture(t, 10, 10, 1, 40)
	f.placement(t, 2)

	target, held := f.status(t)
	// The ramp holds at day 10 (the placement is fresh) and the day is cut a
	// quarter: 20 -> 15.
	if target != 15 {
		t.Errorf("target = %d, want the 25%% cut of 15", target)
	}
	if !held {
		t.Error("target was cut but nothing explains why; the drawer would show a silent drop")
	}
}

func TestLiveOldPlacementNoLongerCutsButStillShiftsTheRamp(t *testing.T) {
	// A placement 8 days ago is outside both windows: no cut, no hold reported.
	// The three frozen days are still subtracted, so the ramp sits below where
	// an unaffected mailbox of the same age would be.
	f := newRampFixture(t, 10, 10, 1, 40)
	f.placement(t, 8*24)

	target, held := f.status(t)
	if held {
		t.Error("a placement 8 days old must not still be reported as holding the ramp")
	}
	if target != 17 {
		t.Errorf("target = %d, want 17 (day 10 minus the 3 frozen days)", target)
	}
}

// Between 48 and 72 hours after a placement the volume cut has expired but the
// ramp is still frozen. That used to report no hold at all, so the drawer
// showed a ramp that was not climbing with nothing to explain it.
func TestLiveRampStillReportsAHoldAfterTheCutExpires(t *testing.T) {
	f := newRampFixture(t, 10, 10, 1, 40)
	f.placement(t, 60)

	target, held := f.status(t)
	if !held {
		t.Error("the ramp is still frozen at 60h but nothing explains it")
	}
	// The cut is gone, so the target is the frozen ramp at full volume.
	if target != 17 {
		t.Errorf("target = %d, want the uncut frozen ramp of 17", target)
	}
}
