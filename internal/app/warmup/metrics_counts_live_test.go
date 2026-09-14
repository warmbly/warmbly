package warmup

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// The combined counters (#492) split their tables as the four scans they replace did.
func TestLiveCombinedMetricCountsSplitTheirTables(t *testing.T) {
	repo, handle := liveWarmupRepo(t)
	f := newFreePoolAccount(t, handle)
	ctx := context.Background()
	exec := func(sql string, args ...any) { t.Helper(); execSQL(t, handle.Pool, sql, args...) }

	// 3 placements and 2 complaints after the floor, one of each before it.
	report := func(kind, offset string, n int) { insertSpamReports(t, handle, f.account, kind, offset, n) }
	report("spam_placement", "1 hour", 3)
	report("user_complaint", "1 hour", 1)
	report("spam_folder", "1 hour", 1)
	report("spam_placement", "3 hours", 1)
	report("user_complaint", "3 hours", 1)
	exec(`UPDATE warmup_pool_participants SET health_signals_from = NOW() - INTERVAL '2 hours' WHERE email_account_id = $1`, f.account)

	// 2 complaints and 4 bounces after the floor, a bounce before it, an open
	// that is neither. All cascade away with the mailbox and its workspace.
	task := uuid.New()
	exec(`INSERT INTO tasks (id, task_type, email_account_id, status, message_id) VALUES ($1, 'campaign', $2, 'completed', '')`, task, f.account)
	event := func(kind, offset string, n int) {
		exec(`INSERT INTO deliverability_events (organization_id, task_id, event_type, recipient_email, idempotency_key, created_at)
		      SELECT $1, $2, $3, 'r@test.local', gen_random_uuid()::text, NOW() - $4::interval FROM generate_series(1, $5)`, f.org, task, kind, offset, n)
	}
	event("complaint", "1 hour", 2)
	event("bounce", "1 hour", 4)
	event("bounce", "3 hours", 1)
	event("open", "1 hour", 1)

	row, err := repo.GetParticipantHealthForAccount(ctx, f.account)
	if err != nil || row == nil {
		t.Fatalf("participant row: %v", err)
	}
	metrics, err := NewService(repo).(*service).loadMetrics(ctx, f.account, row)
	if err != nil {
		t.Fatalf("loadMetrics: %v", err)
	}
	got := [4]int{metrics.SpamPlacementsLast7d, metrics.UserComplaintsLast7d, metrics.ComplaintsLast30d, metrics.BouncesLast30d}
	if got != [4]int{3, 2, 2, 4} {
		t.Fatalf("placements, complaints, external complaints, bounces = %v, want [3 2 2 4]", got)
	}
}

// The write returns the standing the floor decided; a review-required block
// (blocked_until NULL) returns no row and the row in hand stands.
func TestLiveDecisionComesBackInTheWritesTrip(t *testing.T) {
	repo, handle := liveWarmupRepo(t)
	f := newFreePoolAccount(t, handle)
	ctx := context.Background()
	svc := NewService(repo).(*service)

	health, xerr := svc.evaluateAndPersistAnyPool(ctx, f.account)
	if xerr != nil {
		t.Fatalf("evaluate: %v", xerr)
	}
	if health.HealthState != models.WarmupHealthHealthy || health.PoolType != "free" || health.LastHealthEvaluatedAt == nil {
		t.Fatalf("returned %+v; want the healthy free row as written, stamped", health)
	}

	execSQL(t, handle.Pool, `UPDATE warmup_pool_participants SET health_state = 'blocked', blocked_at = NOW(), blocked_until = NULL, blocked_reason = 'review'
	      WHERE email_account_id = $1`, f.account)
	health, xerr = svc.evaluateAndPersistAnyPool(ctx, f.account)
	if xerr != nil {
		t.Fatalf("evaluate held row: %v", xerr)
	}
	if health.HealthState != models.WarmupHealthBlocked {
		t.Fatalf("a review-required block came back as %q; a clean reading must not overturn it", health.HealthState)
	}
}

// The listing serves the stalest evaluation first, so a sweep that is cut off
// resumes where it left off instead of repeating the same head every hour.
func TestLiveSweepListsTheStalestFirst(t *testing.T) {
	repo, handle := liveWarmupRepo(t)
	fresh := newFreePoolAccount(t, handle)
	stale := newFreePoolAccount(t, handle)
	never := newFreePoolAccount(t, handle)
	execSQL(t, handle.Pool, `UPDATE warmup_pool_participants SET last_health_evaluated_at = NOW() WHERE email_account_id = $1`, fresh.account)
	execSQL(t, handle.Pool, `UPDATE warmup_pool_participants SET last_health_evaluated_at = NOW() - INTERVAL '2 hours' WHERE email_account_id = $1`, stale.account)

	rows, err := repo.ListParticipantHealth(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	pos := map[uuid.UUID]int{}
	for i, r := range rows {
		pos[r.EmailAccountID] = i
	}
	if !(pos[never.account] < pos[stale.account] && pos[stale.account] < pos[fresh.account]) {
		t.Fatalf("order never=%d stale=%d fresh=%d; want never, then stale, then fresh", pos[never.account], pos[stale.account], pos[fresh.account])
	}
}
