package warmup

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// Live checks of warmup health evaluation against a real Postgres. Skipped
// unless WARMBLY_TEST_DB is set:
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/app/warmup/ -run Live -v
//
// Issue #195: health evaluation never ran for a free-pool account.
// GetParticipantHealth compared the driver's not-found error with
// `err == sql.ErrNoRows`, but the pool is pgx, whose ErrNoRows wraps
// sql.ErrNoRows rather than being it, so "not in this pool" surfaced as a hard
// error and the account's own pool was never reached. The probe is pure SQL and
// the failure lives in the driver boundary, so it only reproduces live. The
// floor test proves health_signals_from with spam placements, a band that
// still exists.

func liveWarmupRepo(t *testing.T) (repository.WarmupRepository, *db.DB) {
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
	return repository.NewWarmupRepository(handle.Pool), handle
}

// freePoolAccount is a mailbox that participates in the free warmup pool and
// has no premium row, which is the shape every account on a self-hosted install
// has.
type freePoolAccount struct {
	user, org, account uuid.UUID
}

func newFreePoolAccount(t *testing.T, handle *db.DB) *freePoolAccount {
	t.Helper()
	pool := handle.Pool

	f := &freePoolAccount{user: uuid.New(), org: uuid.New(), account: uuid.New()}

	exec := func(sql string, args ...any) { t.Helper(); execSQL(t, pool, sql, args...) }

	exec(`INSERT INTO users (id, email, first_name, last_name) VALUES ($1, $2, 'Health', 'Live')`,
		f.user, "wh-"+f.user.String()[:8]+"@test.local")
	exec(`INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Warmup Health Live', $2, $3)`,
		f.org, "wh-"+f.org.String()[:8], f.user)
	exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name,
	          signature_plain, signature_html, provider, status, campaign_limit, min_wait_time, timezone)
	      VALUES ($1, $2, $3, $4, 'Health', '', '', 'smtp_imap', 'active', 50, 600, 'UTC')`,
		f.account, f.user, f.org, "wh-"+f.account.String()[:8]+"@test.local")

	pools := seededWarmupPools(t, pool)
	// Free pool only. No premium row, which is the whole point.
	exec(`INSERT INTO warmup_pool_participants (pool_id, email_account_id) VALUES ($1, $2)`, pools["free"], f.account)

	t.Cleanup(func() {
		c := context.Background()
		for _, s := range []struct {
			sql string
			arg any
		}{
			{`DELETE FROM warmup_pool_participants WHERE email_account_id = $1`, f.account},
			{`DELETE FROM email_accounts WHERE id = $1`, f.account},
			{`DELETE FROM organizations WHERE id = $1`, f.org},
			{`DELETE FROM users WHERE id = $1`, f.user},
		} {
			if _, err := pool.Exec(c, s.sql, s.arg); err != nil {
				t.Errorf("cleanup %q: %v", s.sql, err)
			}
		}
	})

	return f
}

// The driver-boundary bug itself: "not in this pool" is not an error.
func TestLiveGetParticipantHealthReportsAbsenceNotFailure(t *testing.T) {
	repo, handle := liveWarmupRepo(t)
	f := newFreePoolAccount(t, handle)

	health, err := repo.GetParticipantHealth(context.Background(), f.account, "premium")
	if err != nil {
		t.Fatalf("premium probe returned an error for an account that is simply not in that pool: %v", err)
	}
	if health != nil {
		t.Fatal("expected no premium participant")
	}

	health, err = repo.GetParticipantHealth(context.Background(), f.account, "free")
	if err != nil {
		t.Fatalf("free probe: %v", err)
	}
	if health == nil {
		t.Fatal("expected the free participant row")
	}
}

// EvaluateAllParticipants is the hourly sweep, and the same absence-as-error
// made it skip every free-pool account without a word.
func TestLiveSweepEvaluatesAFreePoolAccount(t *testing.T) {
	repo, handle := liveWarmupRepo(t)
	f := newFreePoolAccount(t, handle)
	svc := NewService(repo)
	ctx := context.Background()

	evaluated, _, xerr := svc.EvaluateAllParticipants(ctx)
	if xerr != nil {
		t.Fatalf("sweep: %v", xerr)
	}
	if evaluated == 0 {
		t.Fatal("the sweep evaluated nobody")
	}

	health, err := repo.GetParticipantHealth(ctx, f.account, "free")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if health.LastHealthEvaluatedAt == nil {
		t.Fatal("the sweep skipped this account: last_health_evaluated_at is still null")
	}
}

// The cutover floor (migration 000096): a mailbox is not judged on signals from
// before it was being judged. Without it, the first evaluation after this fix
// would reach back over everything collected while nothing was watching,
// including the spurious Junk placements Graph produced before #199 and #201.
func TestLiveHealthSignalsBeforeTheFloorAreNotCounted(t *testing.T) {
	repo, handle := liveWarmupRepo(t)
	f := newFreePoolAccount(t, handle)
	svc := NewService(repo)
	ctx := context.Background()
	// placements is one placement per send, a full sample, stamped at the
	// given offset from now. Sends are counted by day, so they stay in view;
	// placements carry a timestamp, which is what the floor is applied to.
	placements := func(offset string) {
		execSQL(t, handle.Pool, `
			INSERT INTO warmup_spam_reports (reporter_account_id, reported_account_id, message_id, report_type, created_at)
			SELECT $1, $1, gen_random_uuid()::text, 'spam_placement', NOW() - $2::interval
			FROM generate_series(1, $3)`, f.account, offset, minSpamPlacementSample)
	}
	execSQL(t, handle.Pool, `INSERT INTO warmup_statistics (email_account_id, date, emails_sent, emails_replied, target_volume)
	      VALUES ($1, CURRENT_DATE, $2, 0, $2)`, f.account, minSpamPlacementSample)
	placements("2 hours")
	execSQL(t, handle.Pool, `UPDATE warmup_pool_participants SET health_signals_from = NOW() - INTERVAL '1 hour'
	      WHERE email_account_id = $1`, f.account)

	health, err := svc.(*service).evaluateAndPersistAnyPool(ctx, f.account)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if health.HealthState != models.WarmupHealthHealthy {
		t.Fatalf("health_state is %q; placements from before the floor were counted against the mailbox",
			health.HealthState)
	}

	// The same placements after the floor are the catastrophic band.
	placements("0 seconds")
	health, err = svc.(*service).evaluateAndPersistAnyPool(ctx, f.account)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if health.HealthState != models.WarmupHealthBlocked {
		t.Fatalf("health_state is %q after %d placements past the floor, want %q",
			health.HealthState, minSpamPlacementSample, models.WarmupHealthBlocked)
	}
}
