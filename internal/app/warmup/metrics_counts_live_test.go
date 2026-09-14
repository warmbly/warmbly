package warmup

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// The two combined counters (#492) must split their tables the way the four
// scans they replace did: placements apart from complaints, complaints apart
// from bounces, and nothing from before the floor or outside the window.
func TestLiveCombinedMetricCountsSplitTheirTables(t *testing.T) {
	repo, handle := liveWarmupRepo(t)
	f := newFreePoolAccount(t, handle)
	ctx := context.Background()
	exec := func(sql string, args ...any) { t.Helper(); execSQL(t, handle.Pool, sql, args...) }

	// Warmup spam reports: 3 placements and 2 complaints in the window, one of
	// each type before the floor.
	report := func(kind, offset string, n int) {
		exec(`INSERT INTO warmup_spam_reports (reporter_account_id, reported_account_id, message_id, report_type, created_at)
		      SELECT $1, $1, gen_random_uuid()::text, $2, NOW() - $3::interval FROM generate_series(1, $4)`, f.account, kind, offset, n)
	}
	report("spam_placement", "1 hour", 3)
	report("user_complaint", "1 hour", 1)
	report("spam_folder", "1 hour", 1)
	report("spam_placement", "3 hours", 1)
	report("user_complaint", "3 hours", 1)
	exec(`UPDATE warmup_pool_participants SET health_signals_from = NOW() - INTERVAL '2 hours' WHERE email_account_id = $1`, f.account)

	// Deliverability events hang off this mailbox's tasks: 2 complaints and 4
	// bounces after the floor, one bounce before it, one open that counts as neither.
	task := uuid.New()
	exec(`INSERT INTO tasks (id, task_type, email_account_id, status, message_id) VALUES ($1, 'campaign', $2, 'completed', '')`, task, f.account)
	t.Cleanup(func() {
		execSQL(t, handle.Pool, `DELETE FROM deliverability_events WHERE organization_id = $1`, f.org)
		execSQL(t, handle.Pool, `DELETE FROM warmup_spam_reports WHERE reported_account_id = $1`, f.account)
		execSQL(t, handle.Pool, `DELETE FROM tasks WHERE id = $1`, task)
	})
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
