package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/monitoring"
	"github.com/warmbly/warmbly/internal/models"
)

func TestLiveMonitoringQueueSettlementAndFailureSemantics(t *testing.T) {
	_, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 277)
	f := newWarmupUsageFixture(t, pool)
	now := time.Now().UTC().Truncate(time.Microsecond)
	r := NewMonitoringRepository(pool)
	baseline := map[string]models.MonitoringSource{}
	for _, id := range []string{"jobs", "dead_letters", "webhooks", "notifications", "result_effects"} {
		baseline[id] = collectMonitoring(t, r, id, now)
	}
	id := uuid.New()
	f.exec(`INSERT INTO tasks(id,task_type,email_account_id,status,message_id)VALUES($1,'email',$2,'active','')`, id, f.account)
	for _, row := range []struct {
		status   string
		at       time.Time
		attempts int
	}{{"pending", now.Add(-time.Minute), 1}, {"pending", now.Add(time.Minute), 1}, {"pending", now.Add(-time.Minute), 5}, {"replayed", now, 1}} {
		f.exec(`INSERT INTO task_dead_letters(task_id,task_type,status,next_retry_at,attempts,max_attempts,payload,last_error)VALUES($1,'email',$2,$3,$4,5,'{"private":"body"}','private cursor')`, id, row.status, row.at, row.attempts)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM task_dead_letters WHERE task_id=$1`, id) })
	job := uuid.NewString()
	f.exec(`INSERT INTO scheduled_job_runs(name,last_status,last_finished_at,error_count,run_count,last_error)VALUES($1,'error',$2,999,1000,'private error')`, job, now.Add(-time.Minute))
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM scheduled_job_runs WHERE name=$1`, job) })
	endpoint := uuid.New()
	f.exec(`INSERT INTO webhook_endpoints(id,organization_id,url,secret,consecutive_failures,first_failure_at,last_failure_at)VALUES($1,$2,'https://private.example.test/secret','private signing key',5,$3,$4)`, endpoint, f.org, now.Add(-73*time.Hour), now.Add(-time.Minute))
	for _, row := range []struct {
		status      string
		at, updated time.Time
	}{{"pending", now.Add(-time.Minute), now}, {"pending", now.Add(time.Minute), now}, {"in_flight", now, now.Add(-6 * time.Minute)}, {"abandoned", now, now}} {
		f.exec(`INSERT INTO webhook_deliveries(endpoint_id,organization_id,event_type,event_id,payload,status,next_attempt_at,updated_at)VALUES($1,$2,'task.send',$3,'{"private":"body"}',$4,$5,$6)`, endpoint, f.org, uuid.New(), row.status, row.at, row.updated)
	}
	for _, row := range []struct {
		state    string
		at       time.Time
		attempts int
	}{{"pending", now.Add(-3 * time.Minute), 1}, {"sending", now.Add(-11 * time.Minute), 1}, {"sent", now, 1}, {"skipped", now, 3}} {
		f.exec(`INSERT INTO notifications(user_id,organization_id,category,title,email_state,email_due_at,email_attempts)VALUES($1,$2,'monitoring','private subject',$3,$4,$5)`, f.user, f.org, row.state, row.at, row.attempts)
	}
	f.exec(`INSERT INTO send_result_effects(task_id,effect_key,organization_id,kind,payload,attempts)VALUES($1,'monitoring',$2,'notification','{"private":"body"}',2)`, id, f.org)
	for source, deltas := range map[string]map[string]int64{
		"jobs": {"job_error_recent": 1}, "dead_letters": {"dlq_due": 1, "dlq_wait": 1, "dlq_exhausted": 1, "dlq_replayed": 1}, "webhooks": {"webhook_due": 1, "webhook_wait": 1, "webhook_stale_claim": 1, "webhook_abandoned_24h": 1, "webhook_endpoint_persistent": 1}, "notifications": {"notification_aging": 1, "notification_stale": 1, "notification_settled_24h": 1, "notification_exhausted": 1}, "result_effects": {"effects_pending": 1, "effects_repeated": 1},
	} {
		got := collectMonitoring(t, NewMonitoringRepository(monitoringReadOnlyPool(t, pool)), source, now)
		for id, delta := range deltas {
			m := measuredMonitoring(t, got, id)
			b := measuredMonitoring(t, baseline[source], id)
			if *m.Count != *b.Count+delta {
				t.Fatal(source, id, m)
			}
		}
	}
	settled := measuredMonitoring(t, collectMonitoring(t, r, "notifications", now), "notification_settled_24h")
	if settled.Condition == models.MonitoringHealthy {
		t.Fatal("notification settlement claimed delivery")
	}
}

func TestLiveMonitoringOptionalSchemaEmptyAndCollectionTimeout(t *testing.T) {
	_, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 277)
	r := NewMonitoringRepository(pool)
	now := time.Now().UTC()
	absent := collectMonitoring(t, r, "arrivals", now)
	if absent.Reason != "schema_absent" {
		t.Skip("optional schema already exists")
	}
	if _, err := pool.Exec(t.Context(), `CREATE TABLE public.sync_arrival_outbox(email_id uuid,organization_id uuid,created_at timestamptz,retry_at timestamptz,locked_until timestamptz,stage int)`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DROP TABLE public.sync_arrival_outbox`) })
	present := collectMonitoring(t, NewMonitoringRepository(monitoringReadOnlyPool(t, pool)), "arrivals", now)
	for _, m := range present.Metrics {
		if m.Count == nil || *m.Count != 0 || m.Availability != models.MonitoringFresh || m.Condition == models.MonitoringHealthy {
			t.Fatal("present-empty misrepresented", m)
		}
	}
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(t.Context(), `LOCK TABLE tasks IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
	defer cancel()
	_, err = r.collect(ctx, now, "dispatch", monitoringSections["dispatch"])
	if err == nil || monitoring.ErrorReason(err) != "timeout" {
		t.Fatal("blocked query became success", err)
	}
}

func TestMonitoringNilDatabaseCollectorsRemainUnavailable(t *testing.T) {
	var r *MonitoringRepository
	for _, source := range r.Sources() {
		out, err := source.Collect(t.Context(), time.Now())
		if err != nil || out.Availability != models.MonitoringUnavailable || out.Reason != "dependency_missing" {
			t.Fatal(source.ID, out, err)
		}
	}
}
