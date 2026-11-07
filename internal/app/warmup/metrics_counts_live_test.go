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
	// Bounces not about the address are not hard bounces; a recipient_invalid one is.
	for _, cause := range []string{"mailbox_full", "reputation_block", "transient", "recipient_invalid"} {
		exec(`INSERT INTO deliverability_events (organization_id, task_id, event_type, recipient_email, idempotency_key, metadata, created_at)
		      VALUES ($1, $2, 'bounce', 'x', gen_random_uuid()::text, jsonb_build_object('bounce_cause', $3::text), NOW() - INTERVAL '1 hour')`, f.org, task, cause)
	}

	// Two deletions and one spam flag after the floor, a deletion before it.
	harm := func(kind, offset string, n int) {
		exec(`INSERT INTO warmup_tampering_events (email_account_id, message_id, kind, created_at)
		      SELECT $1, '<' || gen_random_uuid()::text || '@test.local>', $2, NOW() - $3::interval FROM generate_series(1, $4)`, f.account, kind, offset, n)
	}
	harm("deletion", "1 hour", 2)
	harm("spam_flag", "1 hour", 1)
	harm("deletion", "3 hours", 1)
	t.Cleanup(func() {
		execSQL(t, handle.Pool, `DELETE FROM warmup_tampering_events WHERE email_account_id = $1`, f.account)
	})

	row, err := repo.GetParticipantHealthForAccount(ctx, f.account)
	if err != nil || row == nil {
		t.Fatalf("participant row: %v", err)
	}
	metrics, err := NewService(repo).(*service).loadMetrics(ctx, f.account, row)
	if err != nil {
		t.Fatalf("loadMetrics: %v", err)
	}
	got := [6]int{metrics.SpamPlacementsLast7d, metrics.UserComplaintsLast7d, metrics.ComplaintsLast30d, metrics.BouncesLast30d, metrics.DeletionsLast7d, metrics.SpamFlagsLast7d}
	if got != [6]int{3, 2, 2, 5, 2, 1} {
		t.Fatalf("placements, complaints, external complaints, bounces, deletions, spam flags = %v, want [3 2 2 5 2 1]", got)
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

// Placement is read over verified deliveries, and only a Google, Microsoft or
// Yahoo recipient's junk folder is judged; another host's is carried apart.
func TestLivePlacementIsJudgedAtTheMajorProviders(t *testing.T) {
	repo, handle := liveWarmupRepo(t)
	ctx := context.Background()
	sender := newFreePoolAccount(t, handle)
	gmail := newFreePoolAccount(t, handle)
	small := newFreePoolAccount(t, handle)
	execSQL(t, handle.Pool, `UPDATE email_accounts SET provider = 'gmail' WHERE id = $1`, gmail.account)
	execSQL(t, handle.Pool, `UPDATE warmup_pool_participants SET health_signals_from = NOW() - INTERVAL '1 day' WHERE email_account_id = $1`, sender.account)

	deliverWarmup(t, handle, sender.account, gmail.account, "30 minutes", 18, false)
	deliverWarmup(t, handle, sender.account, gmail.account, "30 minutes", 2, true)
	deliverWarmup(t, handle, sender.account, small.account, "30 minutes", 10, true)
	// Spam reported with no receipt behind it was never verifiably delivered.
	insertSpamReports(t, handle, sender.account, "spam_placement", "10 minutes", 3)

	row, err := repo.GetParticipantHealthForAccount(ctx, sender.account)
	if err != nil || row == nil {
		t.Fatalf("participant row: %v", err)
	}
	m, err := NewService(repo).(*service).loadMetrics(ctx, sender.account, row)
	if err != nil {
		t.Fatalf("loadMetrics: %v", err)
	}
	if m.PlacementSample != 20 || m.SpamPlacementRate != 10 || m.OtherDelivered != 10 || m.OtherSpamRate != 100 {
		t.Fatalf("sample %d, rate %v, other %d at %v%%; want 20 at 10%%, other 10 at 100%%",
			m.PlacementSample, m.SpamPlacementRate, m.OtherDelivered, m.OtherSpamRate)
	}
}
