package repository

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/app/instancesettings"
	"github.com/warmbly/warmbly/internal/app/monitoring"
	"github.com/warmbly/warmbly/internal/models"
)

func TestMonitoringAggregatesDoNotLinkToCampaignOnlyDetails(t *testing.T) {
	for _, id := range []string{"sends", "dispatch"} {
		if link := monitoringSections[id].investigate; link != "" {
			t.Errorf("%s links to %q, which does not cover its measured tasks", id, link)
		}
	}
}

func collectMonitoring(t *testing.T, r *MonitoringRepository, id string, at time.Time) models.MonitoringSource {
	t.Helper()
	for _, source := range r.Sources() {
		if source.ID == id {
			ctx, cancel := context.WithTimeout(t.Context(), monitoring.SourceTimeout)
			defer cancel()
			out, err := source.Collect(ctx, at)
			if err != nil {
				t.Fatalf("%s: %v", id, err)
			}
			return out
		}
	}
	t.Fatalf("missing source %s", id)
	return models.MonitoringSource{}
}

func measuredMonitoring(t *testing.T, source models.MonitoringSource, id string) models.MonitoringMetric {
	t.Helper()
	for _, m := range source.Metrics {
		if m.ID == id {
			return m
		}
	}
	t.Fatalf("missing metric %s", id)
	return models.MonitoringMetric{}
}

func monitoringReadOnlyPool(t *testing.T, pool *pgxpool.Pool) *pgxpool.Pool {
	t.Helper()
	cfg := pool.Config().Copy()
	cfg.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	ro, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ro.Close)
	return ro
}

func TestLiveMonitoringReadOnlySnapshotAndPrivacy(t *testing.T) {
	_, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 277)
	f := newWarmupUsageFixture(t, pool)
	f.exec(`UPDATE email_accounts SET signature_plain='private-monitoring-content',email='private-monitoring@example.test' WHERE id=$1`, f.account)
	r := NewMonitoringRepository(monitoringReadOnlyPool(t, pool))
	out := monitoring.New(r.Sources()).Snapshot(t.Context(), models.AdminPermViewAnalytics|models.AdminPermViewUsers|models.AdminPermViewWorkers|models.AdminPermViewCampaigns|models.AdminPermViewOrganizations|models.AdminPermManageSettings)
	for _, source := range out.Sources {
		if source.ID == "arrivals" {
			if source.Reason != "schema_absent" || source.Availability != models.MonitoringUnavailable {
				t.Fatal(source)
			}
			continue
		}
		if source.Availability != models.MonitoringFresh {
			t.Fatalf("source %s unavailable: %+v", source.ID, source)
		}
		if source.ID == "worker_samples" {
			for _, m := range source.Metrics {
				if m.Count == nil && (m.Availability != models.MonitoringUnavailable || m.Condition != models.MonitoringUnknown) {
					t.Fatal(m)
				}
			}
		}
	}
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"private-monitoring", f.account.String(), f.org.String(), f.user.String()} {
		if strings.Contains(string(raw), private) {
			t.Fatalf("private evidence %q leaked", private)
		}
	}
	if out.Coverage == "complete" {
		t.Fatal("absent optional arrival source became all-clear")
	}
}

func TestLiveMonitoringProviderEvidenceHoldsAndDispatch(t *testing.T) {
	_, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 277)
	f := newWarmupUsageFixture(t, pool)
	r := NewMonitoringRepository(pool)
	now := time.Now().UTC().Truncate(time.Microsecond)
	baseSends, baseDispatch, baseSafety := collectMonitoring(t, r, "sends", now), collectMonitoring(t, r, "dispatch", now), collectMonitoring(t, r, "send_safety", now)
	count := func(source models.MonitoringSource, id string) int64 {
		t.Helper()
		m := measuredMonitoring(t, source, id)
		if m.Count == nil {
			t.Fatal(m)
		}
		return *m.Count
	}
	id := uuid.New()
	at := now.Add(-10 * time.Minute)
	f.exec(`INSERT INTO tasks(id,task_type,email_account_id,status,message_id,completed_at,send_result_state,send_result_applied_at) VALUES($1,'warmup',$2,'completed','private-task-message',$3,'sent',$3)`, id, f.account, at)
	f.exec(`INSERT INTO warmup_tokens(token,task_id,sender_account_id,recipient_account_id,sent_message_id) VALUES($1,$2,$3,$3,'')`, uuid.New(), id, f.account)
	if source := collectMonitoring(t, r, "sends", now); count(source, "confirmed_warmup_1h") != count(baseSends, "confirmed_warmup_1h") {
		t.Fatal("completed task or send result without provider token counted")
	}
	f.exec(`UPDATE warmup_tokens SET sent_message_id='private-provider-id' WHERE task_id=$1`, id)
	if source := collectMonitoring(t, r, "sends", now); count(source, "confirmed_warmup_1h") != count(baseSends, "confirmed_warmup_1h")+1 {
		t.Fatal("provider token not counted")
	}
	f.exec(`UPDATE tasks SET send_result_applied_at=NULL WHERE id=$1`, id)
	if source := collectMonitoring(t, r, "sends", now); count(source, "confirmed_warmup_proxy_1h") != count(baseSends, "confirmed_warmup_proxy_1h")+1 {
		t.Fatal("legacy clock proxy missing")
	}
	for _, reason := range []string{"unknown", "conflict", "permanent", "authentication"} {
		f.exec(`UPDATE email_accounts SET send_recovery_hold=true,send_recovery_reason=$2 WHERE id=$1`, f.account, reason)
		if source := collectMonitoring(t, r, "send_safety", now); count(source, "holds_"+reason) != count(baseSafety, "holds_"+reason)+1 {
			t.Fatal(reason, source)
		}
	}
	f.exec(`UPDATE email_accounts SET send_cooldown_until=$2 WHERE id=$1`, f.account, now)
	if source := collectMonitoring(t, r, "send_safety", now); count(source, "send_cooldowns") != count(baseSafety, "send_cooldowns") {
		t.Fatal("expired cooldown counted")
	}
	f.exec(`UPDATE email_accounts SET send_cooldown_until=$2 WHERE id=$1`, f.account, now.Add(time.Minute))
	if source := collectMonitoring(t, r, "send_safety", now); count(source, "send_cooldowns") != count(baseSafety, "send_cooldowns")+1 {
		t.Fatal(source)
	}
	f.exec(`INSERT INTO tasks(id,task_type,email_account_id,status,message_id,scheduled_at,dispatch_retry_at,dispatch_failure_since,dispatch_failure_at) VALUES($1,'warmup',$2,'pending','',$3,$4,$5,$6)`, uuid.New(), f.account, now.Add(-time.Hour), now.Add(time.Minute), now.Add(-20*time.Minute), now.Add(-time.Minute))
	f.exec(`INSERT INTO tasks(id,task_type,email_account_id,status,message_id,scheduled_at) VALUES($1,'warmup',$2,'pending','',$3),($4,'warmup',$2,'pending','',$5)`, uuid.New(), f.account, now.Add(-time.Minute), uuid.New(), now.Add(time.Hour))
	dispatch := collectMonitoring(t, r, "dispatch", now)
	for id, delta := range map[string]int64{"dispatch_due": 1, "retry_wait": 1, "future_schedule": 1, "dispatch_persistent": 1} {
		if count(dispatch, id) != count(baseDispatch, id)+delta {
			t.Fatal(id, dispatch)
		}
	}
	if measuredMonitoring(t, dispatch, "retry_wait").Condition != models.MonitoringRetry {
		t.Fatal("retry became failure")
	}
}

func TestLiveMonitoringSyncAuthAndDomainPolicy(t *testing.T) {
	_, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 277)
	f := newWarmupUsageFixture(t, pool)
	r := NewMonitoringRepository(pool)
	now := time.Now().UTC().Truncate(time.Microsecond)
	f.exec(`UPDATE email_accounts SET status='active' WHERE id=$1`, f.account)
	baseSync, baseErrors, baseDomain := collectMonitoring(t, r, "sync", now), collectMonitoring(t, r, "mailbox_errors", now), collectMonitoring(t, r, "domain_auth", now)
	count := func(source models.MonitoringSource, id string) int64 {
		t.Helper()
		return *measuredMonitoring(t, source, id).Count
	}
	f.exec(`INSERT INTO email_sync_state(email_id,user_id,backfill_status,backfill_synced,deferred,last_synced_at,throttled_until,throttle_reason,updated_at) VALUES($1,$2,'running',900,7,$3,$4,'daily',$3)`, f.account, f.user, now.Add(-2*time.Hour), now.Add(time.Minute))
	sync := collectMonitoring(t, r, "sync", now)
	if count(sync, "sync_deferred") != count(baseSync, "sync_deferred")+7 || count(sync, "sync_hold_daily") != count(baseSync, "sync_hold_daily")+1 || measuredMonitoring(t, sync, "sync_hold_daily").Condition != models.MonitoringDailyLimit || count(sync, "backfill_stale_running") != count(baseSync, "backfill_stale_running") {
		t.Fatal(sync)
	}
	f.exec(`UPDATE email_sync_state SET throttled_until=$2 WHERE email_id=$1`, f.account, now)
	if sync = collectMonitoring(t, r, "sync", now); count(sync, "sync_hold_daily") != count(baseSync, "sync_hold_daily") || count(sync, "backfill_stale_running") != count(baseSync, "backfill_stale_running")+1 {
		t.Fatal("expired hold remains active", sync)
	}
	for _, code := range []string{"AUTHENTICATION_FAILED", "INVALID_CREDENTIALS"} {
		f.exec(`INSERT INTO email_account_errors(id,email_account_id,user_id,error_code,severity,title,message,created_at) VALUES($1,$2,$3,$4,'CRITICAL','private error','private provider cursor',$5)`, uuid.New(), f.account, f.user, code, now.Add(-2*time.Hour))
	}
	errors := collectMonitoring(t, r, "mailbox_errors", now)
	credential := measuredMonitoring(t, errors, "credential_errors")
	base := measuredMonitoring(t, baseErrors, "credential_errors")
	if *credential.Count != *base.Count+2 || *credential.AffectedMailboxes != *base.AffectedMailboxes+1 || credential.Condition != models.MonitoringRecoveryUnverified {
		t.Fatal(credential)
	}
	f.exec(`UPDATE email_accounts SET status='inactive',auth_state='failing',auth_checked_at=$2,auth_failing_since=$3 WHERE id=$1`, f.account, now.Add(-time.Hour), now.Add(-25*time.Hour))
	if errors = collectMonitoring(t, r, "mailbox_errors", now); count(errors, "inactive_credential_errors") != count(baseErrors, "inactive_credential_errors")+2 {
		t.Fatal(errors)
	}
	store := instancesettings.NewStore(pool)
	policy, err := store.Get(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var original []byte
	err = pool.QueryRow(t.Context(), `SELECT doc FROM instance_settings WHERE id=true`).Scan(&original)
	hadRow := err == nil
	t.Cleanup(func() {
		if hadRow {
			_, _ = pool.Exec(context.Background(), `UPDATE instance_settings SET doc=$1::jsonb WHERE id=true`, original)
		} else {
			_, _ = pool.Exec(context.Background(), `DELETE FROM instance_settings WHERE id=true`)
		}
	})
	policy.Deliverability.EnforceDomainAuth = false
	if err := store.Put(t.Context(), policy, nil); err != nil {
		t.Fatal(err)
	}
	if source := collectMonitoring(t, r, "domain_auth", now); count(source, "domain_blocked") != 0 {
		t.Fatal("disabled enforcement blocked", source)
	}
	policy.Deliverability.EnforceDomainAuth = true
	policy.Deliverability.AuthGraceHours = 24
	if err := store.Put(t.Context(), policy, nil); err != nil {
		t.Fatal(err)
	}
	if source := collectMonitoring(t, r, "domain_auth", now); count(source, "domain_blocked") != count(baseDomain, "domain_blocked")+1 {
		t.Fatal("enforcement/grace ignored", source)
	}
	f.exec(`UPDATE email_accounts SET auth_failing_since=$2 WHERE id=$1`, f.account, now.Add(-23*time.Hour))
	if source := collectMonitoring(t, r, "domain_auth", now); count(source, "domain_blocked") != count(baseDomain, "domain_blocked") {
		t.Fatal("grace blocked", source)
	}
}

func TestLiveMonitoringLoadingIncidentRecoveryNeedsConfirmedToken(t *testing.T) {
	_, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 277)
	f := newWarmupUsageFixture(t, pool)
	r := NewMonitoringRepository(pool)
	now := time.Now().UTC().Truncate(time.Microsecond)
	f.exec(`UPDATE email_accounts SET status='active',warmup=$2,test_mode='legacy' WHERE id=$1`, f.account, now.Add(-24*time.Hour))
	base := collectMonitoring(t, r, "warmup_loading", now)
	for _, at := range []time.Time{now.Add(-80 * time.Minute), now.Add(-40 * time.Minute), now.Add(-time.Minute)} {
		id := uuid.New()
		f.exec(`INSERT INTO tasks(id,task_type,email_account_id,status,message_id,updated_at) VALUES($1,'warmup',$2,'failed','',$3)`, id, f.account, at)
		f.exec(`INSERT INTO task_failures(task_id,title,message) VALUES($1,'private', $2)`, id, models.MailboxNotLoadedPrefix+"private-provider-cursor")
	}
	count := func(source models.MonitoringSource, id string) int64 { return *measuredMonitoring(t, source, id).Count }
	if source := collectMonitoring(t, r, "warmup_loading", now); count(source, "loading_persistent") != count(base, "loading_persistent")+1 {
		t.Fatal(source)
	}
	id := uuid.New()
	f.exec(`INSERT INTO tasks(id,task_type,email_account_id,status,message_id,completed_at) VALUES($1,'warmup',$2,'completed','not-confirmed',$3)`, id, f.account, now)
	f.exec(`INSERT INTO warmup_tokens(token,task_id,sender_account_id,recipient_account_id,sent_message_id) VALUES($1,$2,$3,$3,'')`, uuid.New(), id, f.account)
	if source := collectMonitoring(t, r, "warmup_loading", now); count(source, "loading_persistent") != count(base, "loading_persistent")+1 {
		t.Fatal("empty token cleared loading", source)
	}
	f.exec(`UPDATE warmup_tokens SET sent_message_id='confirmed' WHERE task_id=$1`, id)
	if source := collectMonitoring(t, r, "warmup_loading", now); count(source, "loading_persistent") != count(base, "loading_persistent") {
		t.Fatal("scoped provider confirmation did not close loading", source)
	}
}
