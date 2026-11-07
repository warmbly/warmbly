package repository

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const legacyCampaignSendToday = `t.status = 'completed' AND t.task_type = 'campaign'
	AND (t.completed_at AT TIME ZONE 'UTC')::date = (NOW() AT TIME ZONE 'UTC')::date
	AND ` + taskDispatchedEmail

const legacyPlacementSendToday = `t.task_type = 'placement'
	AND ((t.status = 'completed' AND (t.completed_at AT TIME ZONE 'UTC')::date = (NOW() AT TIME ZONE 'UTC')::date)
	OR (t.status IN ('pending', 'active') AND t.scheduled_at < (((NOW() AT TIME ZONE 'UTC')::date + 1)::timestamp AT TIME ZONE 'UTC')))`

const legacyLastEmailTimesSQL = `SELECT id, MAX(at) FROM (
	SELECT t.email_account_id AS id, t.completed_at AS at FROM tasks t
	WHERE t.email_account_id = ANY($1) AND t.status = 'completed'
	AND t.completed_at IS NOT NULL AND ` + taskDispatchedEmail + `
	UNION ALL SELECT ea.id, ea.send_claimed_at FROM email_accounts ea
	WHERE ea.id = ANY($1) AND ea.send_claimed_at IS NOT NULL
) last GROUP BY id`

const legacyLastSendTimesSQL = `SELECT t.email_account_id, MAX(t.completed_at) FROM tasks t
	WHERE t.email_account_id = ANY($1) AND t.status = 'completed' AND t.task_type = $2::task_type
	AND t.completed_at IS NOT NULL AND ` + taskDispatchedEmail + ` GROUP BY t.email_account_id`

func taskClockMap(t *testing.T, pool *pgxpool.Pool, query string, args ...any) map[uuid.UUID]time.Time {
	t.Helper()
	rows, err := pool.Query(t.Context(), query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := make(map[uuid.UUID]time.Time)
	for rows.Next() {
		var id uuid.UUID
		var at time.Time
		if err := rows.Scan(&id, &at); err != nil {
			t.Fatal(err)
		}
		out[id] = at.UTC()
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestLiveTaskLedgerRangesAndClocksMatchLegacy(t *testing.T) {
	f, _ := lineageFixture(t)
	ctx := t.Context()
	sf := newSharedOrgFixture(t, f.pool)
	sequence := uuid.New()
	if _, err := f.pool.Exec(ctx, `INSERT INTO sequences(id,campaign_id,organization_id,name,subject,body_plain,body_html)
		VALUES($1,$2,$3,'Ledger','Ledger','','')`, sequence, sf.campaign, sf.org); err != nil {
		t.Fatal(err)
	}
	for _, zone := range []string{"UTC", "America/New_York", "Europe/Budapest", "Pacific/Auckland"} {
		t.Run(zone, func(t *testing.T) {
			cfg := f.pool.Config().Copy()
			cfg.ConnConfig.RuntimeParams["timezone"] = zone
			pool, err := pgxpool.NewWithConfig(ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			r := NewTaskRepository(pool)
			if _, err := pool.Exec(ctx, `DELETE FROM tasks WHERE email_account_id = ANY($1)`, []uuid.UUID{f.sender, f.recipient}); err != nil {
				t.Fatal(err)
			}
			var utcStart, utcEnd, localStart, localEnd time.Time
			if err := pool.QueryRow(ctx, `SELECT
				(NOW() AT TIME ZONE 'UTC')::date::timestamp AT TIME ZONE 'UTC',
				((NOW() AT TIME ZONE 'UTC')::date + 1)::timestamp AT TIME ZONE 'UTC',
				CURRENT_DATE::timestamptz, (CURRENT_DATE + 1)::timestamptz`).Scan(&utcStart, &utcEnd, &localStart, &localEnd); err != nil {
				t.Fatal(err)
			}
			for _, kind := range []string{"campaign", "warmup", "email", "placement"} {
				for _, status := range []string{"completed", "pending", "active", "failed"} {
					for _, at := range []any{utcStart.Add(-time.Microsecond), utcStart, utcEnd.Add(-time.Microsecond), utcEnd,
						localStart.Add(-time.Microsecond), localStart, localEnd.Add(-time.Microsecond), localEnd, nil} {
						for _, message := range []string{"", "sent@test.local"} {
							if _, err := pool.Exec(ctx, `INSERT INTO tasks(email_account_id,task_type,status,message_id,completed_at,scheduled_at)
								VALUES($1,$2::task_type,$3::task_status,$4,$5,$5)`, f.sender, kind, status, message, at); err != nil {
								t.Fatal(err)
							}
						}
					}
				}
			}
			reserved := uuid.New()
			if _, err := pool.Exec(ctx, `INSERT INTO tasks(id,email_account_id,task_type,status,message_id,completed_at)
				VALUES($1,$2,'campaign','completed','',$3)`, reserved, f.sender, utcEnd.Add(2*time.Hour)); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `INSERT INTO campaign_contact_progress(campaign_id,contact_id,sequence_id,dispatch_task_id)
				VALUES($1,$2,$3,$4) ON CONFLICT(campaign_id,contact_id,sequence_id) DO UPDATE SET dispatch_task_id=$4`, sf.campaign, sf.contact, sequence, reserved); err != nil {
				t.Fatal(err)
			}
			ids := []uuid.UUID{f.sender, f.recipient, f.sender, uuid.New()}
			var wantCold, wantAll, wantWarm int
			if err := pool.QueryRow(ctx, `SELECT
				(SELECT COUNT(*) FROM tasks t WHERE email_account_id=$1 AND `+legacyCampaignSendToday+`) +
				(SELECT COUNT(*) FROM tasks t WHERE email_account_id=$1 AND `+legacyPlacementSendToday+`),
				(SELECT COUNT(*) FROM tasks t WHERE email_account_id=$1 AND status='completed' AND DATE(completed_at)=CURRENT_DATE AND `+taskDispatchedEmail+`),
				(SELECT COUNT(*) FROM tasks WHERE email_account_id=$1 AND status='completed' AND task_type='warmup' AND DATE(completed_at)=CURRENT_DATE)`, f.sender).Scan(&wantCold, &wantAll, &wantWarm); err != nil {
				t.Fatal(err)
			}
			if got, err := r.CountCampaignEmailsSentToday(ctx, f.sender); err != nil || got != wantCold {
				t.Fatalf("cold count=%d want=%d err=%v", got, wantCold, err)
			}
			if got, err := r.CountCampaignEmailsSentTodayByAccounts(ctx, ids); err != nil || got[f.sender] != wantCold || len(got) != 1 {
				t.Fatalf("cold batch=%v want=%d err=%v", got, wantCold, err)
			}
			if got, err := r.CountEmailsSentToday(ctx, f.sender); err != nil || got != wantAll {
				t.Fatalf("all count=%d want=%d err=%v", got, wantAll, err)
			}
			if got, err := r.CountWarmupEmailsSentBetween(ctx, f.sender, localStart, localEnd); err != nil || got != wantWarm {
				t.Fatalf("warmup count=%d want=%d err=%v", got, wantWarm, err)
			}
			for _, claim := range []any{nil, utcStart.Add(-48 * time.Hour), utcEnd.Add(48 * time.Hour)} {
				if _, err := pool.Exec(ctx, `UPDATE email_accounts SET send_claimed_at=$2 WHERE id=ANY($1)`, []uuid.UUID{f.sender, f.recipient}, claim); err != nil {
					t.Fatal(err)
				}
				want := taskClockMap(t, pool, legacyLastEmailTimesSQL, ids)
				got, err := r.GetLastEmailTimes(ctx, ids)
				if err != nil || !reflect.DeepEqual(taskClockUTC(got), want) {
					t.Fatalf("last clock=%v want=%v err=%v", got, want, err)
				}
				for _, id := range ids {
					last, err := r.GetLastEmailTime(ctx, id)
					at, ok := want[id]
					if err != nil || (last != nil) != ok || ok && !last.Equal(at) {
						t.Fatalf("single clock=%v want=%v present=%t err=%v", last, at, ok, err)
					}
				}
			}
			for _, kind := range []string{"campaign", "warmup", "email", "placement"} {
				want := taskClockMap(t, pool, legacyLastSendTimesSQL, ids, kind)
				got, err := r.GetLastSendTimes(ctx, ids, kind)
				if err != nil || !reflect.DeepEqual(taskClockUTC(got), want) {
					t.Fatalf("last %s=%v want=%v err=%v", kind, got, want, err)
				}
			}
			if got, err := r.GetLastEmailTimes(ctx, nil); err != nil || len(got) != 0 {
				t.Fatalf("empty clock=%v err=%v", got, err)
			}
			if got, err := r.GetLastSendTimes(ctx, nil, "campaign"); err != nil || len(got) != 0 {
				t.Fatalf("empty send clock=%v err=%v", got, err)
			}
		})
	}
}

func taskClockUTC(in map[uuid.UUID]time.Time) map[uuid.UUID]time.Time {
	for id, at := range in {
		in[id] = at.UTC()
	}
	return in
}

func TestLiveTaskLedgerDayRangePreservesDST(t *testing.T) {
	f, _ := lineageFixture(t)
	for _, day := range []string{"2026-03-08", "2026-11-01"} {
		var mismatches int
		err := f.pool.QueryRow(t.Context(), `SELECT COUNT(*) FROM generate_series(
			($1::date::timestamp AT TIME ZONE 'America/New_York') - interval '2 hours',
			(($1::date+1)::timestamp AT TIME ZONE 'America/New_York') + interval '2 hours', interval '1 second') s(at)
			WHERE ((at AT TIME ZONE 'America/New_York')::date = $1::date) IS DISTINCT FROM
			(at >= ($1::date::timestamp AT TIME ZONE 'America/New_York') AND
			 at < (($1::date+1)::timestamp AT TIME ZONE 'America/New_York'))`, day).Scan(&mismatches)
		if err != nil || mismatches != 0 {
			t.Fatalf("DST day %s mismatches=%d err=%v", day, mismatches, err)
		}
	}
}

type taskPressureNode struct {
	RelationName string             `json:"Relation Name"`
	Rows         float64            `json:"Actual Rows"`
	Loops        float64            `json:"Actual Loops"`
	Filtered     float64            `json:"Rows Removed by Filter"`
	Plans        []taskPressureNode `json:"Plans"`
}

func taskPressureExplain(t *testing.T, conn *pgx.Conn, query, args, relation string) (float64, float64, string) {
	t.Helper()
	if _, err := conn.Exec(t.Context(), `PREPARE task_pressure AS `+query); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := conn.Exec(t.Context(), `DEALLOCATE task_pressure`); err != nil {
			t.Error(err)
		}
	}()
	var raw []byte
	for range 4 {
		if err := conn.QueryRow(t.Context(), `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) EXECUTE task_pressure(`+args+`)`).Scan(&raw); err != nil {
			t.Fatal(err)
		}
	}
	var plans []struct {
		Plan          taskPressureNode `json:"Plan"`
		ExecutionTime float64          `json:"Execution Time"`
	}
	if err := json.Unmarshal(raw, &plans); err != nil || len(plans) != 1 {
		t.Fatalf("plan decode: %v", err)
	}
	var examined float64
	var visit func(taskPressureNode)
	visit = func(p taskPressureNode) {
		if p.RelationName == relation {
			examined += (p.Rows + p.Filtered) * p.Loops
		}
		for _, child := range p.Plans {
			visit(child)
		}
	}
	visit(plans[0].Plan)
	return examined, plans[0].ExecutionTime, string(raw)
}

func TestLiveTaskQueryPressureUsesBoundedHistory(t *testing.T) {
	f, _ := lineageFixture(t)
	ctx := t.Context()
	ids := []uuid.UUID{f.sender, f.recipient}
	for range 22 {
		id := uuid.New()
		if _, err := f.pool.Exec(ctx, `INSERT INTO email_accounts(id,user_id,organization_id,email,name,signature_plain,signature_html,provider)
			VALUES($1,$2,$3,$4,'Plan','','','smtp_imap')`, id, f.user, f.org, id.String()+"@test.local"); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO tasks(email_account_id,task_type,status,message_id,completed_at)
		SELECT a, 'campaign', 'completed', 'history@test.local', NOW()-interval '2 days' - make_interval(secs => g)
		FROM unnest($1::uuid[]) a CROSS JOIN generate_series(1,2500) g`, ids); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO tasks(email_account_id,task_type,status,message_id,completed_at)
		SELECT a, 'campaign', 'completed', 'today@test.local', NOW() FROM unnest($1::uuid[]) a`, ids); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `ANALYZE tasks`); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.ConnectConfig(ctx, f.pool.Config().ConnConfig.Copy())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `SET plan_cache_mode=force_generic_plan`); err != nil {
		t.Fatal(err)
	}
	array := "ARRAY['" + strings.Join(uuidStrings(ids), "','") + "']::uuid[]"
	for _, tc := range []struct{ name, before, after, args string }{
		{"daily_campaign", `SELECT COUNT(*) FROM tasks t WHERE email_account_id=$1 AND ` + legacyCampaignSendToday,
			`SELECT COUNT(*) FROM tasks t WHERE email_account_id=$1 AND ` + campaignSendToday, fmt.Sprintf("'%s'", f.sender)},
		{"pool_clock", legacyLastEmailTimesSQL, lastEmailTimesSQL, array},
		{"rotation_clock", legacyLastSendTimesSQL, lastSendTimesSQL, array + ",'campaign'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			beforeRows, beforeMS, _ := taskPressureExplain(t, conn, tc.before, tc.args, "tasks")
			afterRows, afterMS, plan := taskPressureExplain(t, conn, tc.after, tc.args, "tasks")
			t.Logf("synthetic fixture: 24 mailboxes x 2501 sends; generic plan before %.3fms / %.0f task rows, after %.3fms / %.0f task rows", beforeMS, beforeRows, afterMS, afterRows)
			if afterRows >= beforeRows || !strings.Contains(plan, `"Index Name": "idx_tasks_`) {
				t.Fatalf("expected fewer task rows and an indexed lookup: %s", plan)
			}
			if tc.name != "daily_campaign" && !strings.Contains(plan, `"Node Type": "Limit"`) {
				t.Fatalf("send clocks must bound each mailbox lookup: %s", plan)
			}
			if tc.name == "daily_campaign" && !strings.Contains(plan, "completed_at >=") {
				t.Fatalf("ledger must bound indexed completed_at: %s", plan)
			}
		})
	}
}

func uuidStrings(ids []uuid.UUID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}
