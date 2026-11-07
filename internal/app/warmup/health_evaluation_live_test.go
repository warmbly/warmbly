package warmup

import (
	"context"
	"math"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/warmupramp"
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
	requireSchemaVersion(t, handle.Pool, 156)
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

	requireSeededPools(t, pool)
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

	// Free pool only. No premium row, which is the whole point.
	exec(`INSERT INTO warmup_pool_participants (pool_id, email_account_id) VALUES ($1, $2)`, models.WarmupPoolFreeID, f.account)

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

// insertSpamReports files n reports of one kind against the mailbox, stamped
// offset ago. Rows cascade away with the mailbox.
func insertSpamReports(t *testing.T, handle *db.DB, account uuid.UUID, kind, offset string, n int) {
	t.Helper()
	execSQL(t, handle.Pool, `
		INSERT INTO warmup_spam_reports (reporter_account_id, reported_account_id, message_id, report_type, created_at)
		SELECT $1, $1, gen_random_uuid()::text, $2, NOW() - $3::interval
		FROM generate_series(1, $4)`, account, kind, offset, n)
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
	partner := newFreePoolAccount(t, handle)
	execSQL(t, handle.Pool, `UPDATE email_accounts SET provider = 'gmail' WHERE id = $1`, partner.account)
	svc := NewService(repo)
	ctx := context.Background()
	// A full sample of deliveries, every one junked; the floor is applied to
	// the receipts' timestamps.
	deliverWarmup(t, handle, f.account, partner.account, "2 hours", minSpamPlacementSample, true)
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

	// The same placements after the floor slow the mailbox down, and no more.
	deliverWarmup(t, handle, f.account, partner.account, "0 seconds", minSpamPlacementSample, true)
	health, err = svc.(*service).evaluateAndPersistAnyPool(ctx, f.account)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if health.HealthState != models.WarmupHealthThrottled {
		t.Fatalf("health_state is %q after %d placements past the floor, want %q",
			health.HealthState, minSpamPlacementSample, models.WarmupHealthThrottled)
	}
}

// deliverWarmup files n verified receipts of sender's warmup mail at recipient,
// stamped offset ago, each also a spam placement when junked. Rows cascade
// away with the mailboxes.
func deliverWarmup(t *testing.T, handle *db.DB, sender, recipient uuid.UUID, offset string, n int, junked bool) []string {
	t.Helper()
	ids := make([]string, n)
	for i := range ids {
		ids[i] = "<" + uuid.NewString() + "@test.local>"
	}
	landing := "inbox"
	if junked {
		landing = "spam"
	}
	// Placement counts only a receipt whose landing was observed (000267).
	execSQL(t, handle.Pool, `
		INSERT INTO warmup_received (email_account_id, internal_id, message_id, sender_account_id, created_at, first_landing)
		SELECT $1, gen_random_uuid(), m, $2, NOW() - $3::interval, $5 FROM unnest($4::text[]) AS m`,
		recipient, sender, offset, ids, landing)
	if junked {
		execSQL(t, handle.Pool, `
			INSERT INTO warmup_spam_reports (reporter_account_id, reported_account_id, message_id, report_type, created_at)
			SELECT $1, $2, m, 'spam_placement', NOW() - $3::interval FROM unnest($4::text[]) AS m`,
			recipient, sender, offset, ids)
	}
	return ids
}

func TestLiveHealthPlacementRecoveryUsesTheDisplayedUTCWindow(t *testing.T) {
	repo, handle := liveWarmupRepo(t)
	f, partner := newFreePoolAccount(t, handle), newFreePoolAccount(t, handle)
	now := time.Date(2026, 10, 16, 4, 30, 0, 0, time.UTC)
	start := models.WarmupPlacementWindowStart(now)
	execSQL(t, handle.Pool, `UPDATE email_accounts SET provider = 'gmail' WHERE id = $1`, partner.account)
	execSQL(t, handle.Pool, `UPDATE warmup_pool_participants SET health_state = 'throttled',
		last_health_reason = $2, blocked_until = $3, health_signals_from = $4 WHERE email_account_id = $1`,
		f.account, placementReasonMarker, now.Add(72*time.Hour), start.Add(-48*time.Hour))
	for _, batch := range []struct {
		at          time.Time
		inbox, spam int
	}{
		{start.Add(-time.Microsecond), 5, 4},
		{start, 28, 2},
	} {
		execSQL(t, handle.Pool, `INSERT INTO warmup_received
			(email_account_id, internal_id, message_id, sender_account_id, created_at, first_landing)
			SELECT $1, gen_random_uuid(), gen_random_uuid()::text, $2, $3,
			CASE WHEN n <= $4 THEN 'inbox' ELSE 'spam' END FROM generate_series(1, $5) n`,
			partner.account, f.account, batch.at, batch.inbox, batch.inbox+batch.spam)
	}
	old, err := repo.HealthMetricCounts(t.Context(), f.account, now.Add(-7*24*time.Hour), now.Add(-7*24*time.Hour), now.Add(-30*24*time.Hour))
	if err != nil || old.Placement.MajorDelivered != 39 || old.Placement.MajorSpam != 6 {
		t.Fatalf("old rolling sample = %+v, err = %v", old.Placement, err)
	}
	snapshot, err := repository.NewAdvisorRepository(handle).LoadSnapshot(t.Context(), f.org, now)
	if err != nil || len(snapshot.Mailboxes) != 1 {
		t.Fatalf("advisor snapshot = %+v, err = %v", snapshot, err)
	}
	if p := snapshot.Mailboxes[0].WarmupPlacement; p.MajorDelivered != 30 || p.MajorSpam != 2 {
		t.Fatalf("advisor did not use the displayed placement window: %+v", p)
	}
	svc := NewService(repo).(*service)
	svc.now = func() time.Time { return now }
	health, xerr := svc.evaluateAndPersistAnyPool(t.Context(), f.account)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if health.HealthState != models.WarmupHealthHealthy || health.BlockedUntil != nil || warmupramp.HealthVolumeMultiplier(health.HealthState) != 1 {
		t.Fatalf("recovered mailbox kept a future throttle: %+v", health)
	}
	if math.Abs(health.LastHealthScore-100.0*2/30) > 0.001 {
		t.Fatalf("health did not use the displayed 30-delivery sample: %+v", health)
	}
	// The placement boundary must not erase complaints or tampering from the preceding rolling week.
	execSQL(t, handle.Pool, `INSERT INTO warmup_tampering_events(email_account_id, message_id, kind, created_at)
		VALUES ($1, gen_random_uuid()::text, 'deletion', $2)`, f.account, start.Add(-time.Microsecond))
	execSQL(t, handle.Pool, `INSERT INTO warmup_spam_reports(reporter_account_id, reported_account_id, message_id, report_type, created_at)
		VALUES ($1, $2, gen_random_uuid()::text, 'user_complaint', $3)`, partner.account, f.account, start.Add(-time.Microsecond))
	m, err := svc.loadMetrics(t.Context(), f.account, health)
	if err != nil || m.PlacementSample != 30 || m.DeletionsLast7d != 1 || m.UserComplaintsLast7d != 1 {
		t.Fatalf("separate safety windows = %+v, err = %v", m, err)
	}
}
