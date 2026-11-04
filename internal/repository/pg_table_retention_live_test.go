package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// ancient is older than any fixture another live test writes, so a prune in
// a shared test database only reaches this test's rows.
const ancientDays = 4000

func TestLiveTableRetentionPrunesOnlyPastWindow(t *testing.T) {
	_, pool := liveContactDB(t)
	ctx := context.Background()
	user, org, campaign, worker, endpoint, account, task, repairable := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("fixture %q: %v", query[:min(60, len(query))], err)
		}
	}
	exec(`INSERT INTO users (id, first_name, last_name, email) VALUES ($1, 'Table', 'Retention', $2)`, user, "table-retention-"+uuid.NewString()+"@example.test")
	exec(`INSERT INTO organizations (id, name, owner_user_id) VALUES ($1, 'Table retention', $2)`, org, user)
	exec(`INSERT INTO campaigns (id, user_id, organization_id, name, description, days, status, updated_at, created_at)
	      VALUES ($1, $2, $3, 'Retention', '', 62, 'active', NOW(), NOW())`, campaign, user, org)
	exec(`INSERT INTO fleet_nodes (id, role, name) VALUES ($1, 'worker', 'table-retention')`, worker)
	exec(`INSERT INTO workers (id) VALUES ($1)`, worker)
	exec(`INSERT INTO webhook_endpoints (id, organization_id, url, secret) VALUES ($1, $2, 'https://example.test/hook', 's')`, endpoint, org)
	exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain, signature_html, provider)
	      VALUES ($1, $2, $3, $4, 'Retention', '', '', 'smtp_imap')`, account, user, org, "table-retention-"+uuid.NewString()+"@example.test")
	exec(`INSERT INTO tasks (id, task_type, email_account_id, status, message_id, send_result_state, send_result_applied_at)
	      VALUES ($1, 'campaign', $3, 'completed', '<retention@x>', 'sent', NOW()), ($2, 'campaign', $3, 'dead_lettered', '<repairable@x>', 'sent', NOW())`, task, repairable, account)
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM tasks WHERE id = ANY($1)`, []uuid.UUID{task, repairable})
		_, _ = pool.Exec(c, `DELETE FROM email_accounts WHERE id = $1`, account)
		_, _ = pool.Exec(c, `DELETE FROM workers WHERE id = $1`, worker)
		_, _ = pool.Exec(c, `DELETE FROM fleet_nodes WHERE id = $1`, worker)
		_, _ = pool.Exec(c, `DELETE FROM organizations WHERE id = $1`, org)
		_, _ = pool.Exec(c, `DELETE FROM users WHERE id = $1`, user)
	})

	old := time.Now().AddDate(0, 0, -ancientDays-1)
	fresh := time.Now()
	exec(`INSERT INTO campaign_logs (campaign_id, event_type, created_at) VALUES ($1, 'old', $2), ($1, 'fresh', $3)`, campaign, old, fresh)
	exec(`INSERT INTO notifications (user_id, organization_id, category, title, created_at, email_state)
	      VALUES ($1, $2, 'c', 'old', $3, 'none'), ($1, $2, 'c', 'owed', $3, 'pending'), ($1, $2, 'c', 'fresh', $4, 'none')`, user, org, old, fresh)
	exec(`INSERT INTO webhook_deliveries (endpoint_id, organization_id, event_type, event_id, payload, status, updated_at)
	      VALUES ($1, $2, 'old', gen_random_uuid(), '{}', 'delivered', $3),
	             ($1, $2, 'retrying', gen_random_uuid(), '{}', 'pending', $3),
	             ($1, $2, 'fresh', gen_random_uuid(), '{}', 'abandoned', $4)`, endpoint, org, old, fresh)
	exec(`INSERT INTO worker_health_samples (worker_id, observed_at) VALUES ($1, $2), ($1, $3)`, worker, old, fresh)
	exec(`INSERT INTO send_result_effects (task_id, effect_key, organization_id, kind, payload, delivered_at)
	      VALUES ($1, 'old', $2, 'webhook', '{}', $3), ($1, 'undelivered', $2, 'webhook', '{}', NULL), ($1, 'fresh', $2, 'webhook', '{}', $4),
	             ($5, 'old', $2, 'webhook', '{}', $3)`, task, org, old, fresh, repairable)
	// More than one batch, so the loop has to come back for the rest.
	exec(`INSERT INTO worker_health_samples (worker_id, observed_at) SELECT $1, $2::timestamptz - g * interval '1 second' FROM generate_series(1, $3::int) g`, worker, old, PruneBatch+7)

	repo := NewTableRetentionRepository(pool)
	before := time.Now().AddDate(0, 0, -ancientDays)
	for name, prune := range map[string]func(context.Context, time.Time) (int64, error){
		"campaign_logs": repo.PruneCampaignLogs, "notifications": repo.PruneNotifications, "webhook_deliveries": repo.PruneWebhookDeliveries,
		"worker_health_samples": repo.PruneWorkerHealthSamples,
		"send_result_effects":   repo.PruneDeliveredSendResultEffects,
	} {
		if _, err := prune(ctx, before); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}

	count := func(query string, args ...any) int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx, query, args...).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}
	for _, c := range []struct {
		what  string
		query string
		arg   uuid.UUID
		want  int
	}{
		{"campaign feed keeps only the fresh entry", `SELECT count(*) FROM campaign_logs WHERE campaign_id = $1 AND event_type = 'fresh'`, campaign, 1},
		{"campaign feed drops the old entry", `SELECT count(*) FROM campaign_logs WHERE campaign_id = $1`, campaign, 1},
		{"notifications keep the fresh one and the one owed an email", `SELECT count(*) FROM notifications WHERE user_id = $1 AND title IN ('fresh', 'owed')`, user, 2},
		{"notifications drop the old one", `SELECT count(*) FROM notifications WHERE user_id = $1`, user, 2},
		{"webhook log keeps the retrying and fresh deliveries", `SELECT count(*) FROM webhook_deliveries WHERE endpoint_id = $1 AND event_type IN ('retrying', 'fresh')`, endpoint, 2},
		{"webhook log drops the old settled delivery", `SELECT count(*) FROM webhook_deliveries WHERE endpoint_id = $1`, endpoint, 2},
		{"health samples keep only the fresh one, across batches", `SELECT count(*) FROM worker_health_samples WHERE worker_id = $1`, worker, 1},
		{"effects keep the undelivered and fresh ones", `SELECT count(*) FROM send_result_effects WHERE task_id = $1`, task, 2},
		{"effects keep an old one whose dead-lettered task can apply its result again", `SELECT count(*) FROM send_result_effects WHERE task_id = $1`, repairable, 1},
	} {
		if got := count(c.query, c.arg); got != c.want {
			t.Errorf("%s: got %d rows, want %d", c.what, got, c.want)
		}
	}
}

// Each prune's row selection is served by an index, so a prune never scans
// the whole table it trims.
func TestLiveRetentionPrunesUseTheirIndexes(t *testing.T) {
	_, pool := liveContactDB(t)
	ctx := context.Background()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SET enable_seqscan = off`); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = conn.Exec(context.Background(), `RESET enable_seqscan`) }()

	for _, c := range []struct{ query, index string }{
		{`SELECT ctid FROM email_opens WHERE opened_at < NOW() - 30 * INTERVAL '1 day' LIMIT 5000`, "idx_email_opens_opened_at"},
		{`SELECT ctid FROM email_link_clicks WHERE clicked_at < NOW() - 30 * INTERVAL '1 day' LIMIT 5000`, "idx_email_link_clicks_clicked_at"},
		{`SELECT ctid FROM campaign_logs WHERE created_at < NOW() LIMIT 5000`, "idx_campaign_logs_created"},
		{`SELECT ctid FROM notifications WHERE created_at < NOW() AND email_state NOT IN ('pending', 'sending') LIMIT 5000`, "idx_notifications_created"},
		{`SELECT ctid FROM webhook_deliveries WHERE status IN ('delivered', 'failed', 'abandoned') AND updated_at < NOW() LIMIT 5000`, "idx_webhook_deliveries_settled"},
		{`SELECT ctid FROM worker_health_samples WHERE observed_at < NOW() LIMIT 5000`, "idx_worker_health_samples_observed"},
		// Either index answers the one-hour window; which one depends on the table's statistics.
		{`SELECT worker_id, sum(sends_attempted) FROM worker_health_samples WHERE observed_at > now() - interval '1 hour' GROUP BY worker_id`, "idx_worker_health_samples_"},
		{`SELECT e.ctid FROM send_result_effects e WHERE e.delivered_at IS NOT NULL AND e.delivered_at < NOW()
		   AND NOT EXISTS (SELECT 1 FROM tasks t WHERE t.id = e.task_id AND (t.send_result_applied_at IS NULL OR t.status = 'dead_lettered')) LIMIT 5000`, "idx_send_result_effects_delivered"},
		{`SELECT internal_id FROM warmup_received WHERE email_account_id = '00000000-0000-0000-0000-000000000001' AND retired_at IS NULL AND created_at < NOW() ORDER BY created_at LIMIT 100`, "idx_warmup_received_account_live"},
		{`SELECT token FROM warmup_tokens WHERE sender_account_id = '00000000-0000-0000-0000-000000000001' AND sent_message_id <> '' AND sent_retired_at IS NULL AND created_at < NOW() ORDER BY created_at LIMIT 100`, "idx_warmup_tokens_sender_live"},
	} {
		rows, err := conn.Query(ctx, `EXPLAIN `+c.query)
		if err != nil {
			t.Fatalf("explain %q: %v", c.query, err)
		}
		var plan strings.Builder
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatal(err)
			}
			plan.WriteString(line + "\n")
		}
		rows.Close()
		if !strings.Contains(plan.String(), c.index) || strings.Contains(plan.String(), "Seq Scan") {
			t.Errorf("%s is not used by %q:\n%s", c.index, c.query, plan.String())
		}
	}
}
