package db

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestLiveWarmupChargeUpgradeKeepsLegacyUnverifiedAndPreservesEvidence(t *testing.T) {
	dsn := os.Getenv("WARMBLY_TEST_DB")
	if dsn == "" {
		t.Skip("WARMBLY_TEST_DB not set")
	}
	u, err := url.Parse(dsn)
	if err != nil || (u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1") {
		t.Fatal("upgrade requires local PostgreSQL")
	}
	admin, err := pgx.Connect(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(context.Background())
	name := "warmup_charge_upgrade_" + uuid.New().String()[:8]
	quoted := pgx.Identifier{name}.Sanitize()
	if _, err = admin.Exec(t.Context(), "CREATE DATABASE "+quoted); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec(context.Background(), "DROP DATABASE "+quoted); err != nil {
			t.Error(err)
		}
	}()
	u.Path = "/" + name
	source, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	m, err := openMigrate(source, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err = m.Migrate(282); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(t.Context(), u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := conn.Exec(t.Context(), sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	user, org, mailbox, completed, deadletter := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	exec(`INSERT INTO users(id,email,first_name,last_name) VALUES($1,$2,'Upgrade','')`, user, user.String()+"@example.test")
	exec(`INSERT INTO organizations(id,name,owner_user_id) VALUES($1,'Upgrade',$2)`, org, user)
	exec(`INSERT INTO email_accounts(id,user_id,organization_id,email,name,signature_plain,signature_html,provider,send_recovery_hold,send_recovery_reason,send_recovery_task_id) VALUES($1,$2,$3,$4,'Upgrade','','','smtp_imap',true,'unknown',$5)`, mailbox, user, org, mailbox.String()+"@example.test", deadletter)
	for i, task := range []uuid.UUID{completed, deadletter} {
		status := "completed"
		if i == 1 {
			status = "dead_lettered"
		}
		exec(`INSERT INTO tasks(id,email_account_id,task_type,status,message_id,completed_at,send_reserved_at,send_executor_nonce,send_result_state) VALUES($1,$2,'warmup',$3,'',NOW(),NOW(),$4,'unknown')`, task, mailbox, status, uuid.New())
		exec(`INSERT INTO warmup_tasks(task_id,lineage_version,dispatch_nonce) VALUES($1,1,$2)`, task, uuid.New())
	}
	exec(`INSERT INTO warmup_statistics(email_account_id,date,emails_sent,emails_replied,target_volume) VALUES($1,(NOW() AT TIME ZONE 'UTC')::date,2,1,10)`, mailbox)
	if err = m.Migrate(283); err != nil {
		t.Fatal(err)
	}
	var untouched bool
	if err = conn.QueryRow(t.Context(), `SELECT BOOL_AND(w.warmup_charged_date IS NULL AND w.warmup_reply_charged IS NULL AND w.warmup_refunded_at IS NULL AND t.completed_at IS NOT NULL AND t.send_reserved_at IS NOT NULL AND t.send_executor_nonce IS NOT NULL AND w.dispatch_nonce IS NOT NULL AND t.send_result_state='unknown' AND t.send_result_applied_at IS NULL AND ea.send_recovery_hold AND ea.send_recovery_task_id=$2 AND ws.emails_sent=2 AND ws.emails_replied=1) FROM warmup_tasks w JOIN tasks t ON t.id=w.task_id JOIN email_accounts ea ON ea.id=t.email_account_id JOIN warmup_statistics ws ON ws.email_account_id=ea.id WHERE t.email_account_id=$1`, mailbox, deadletter).Scan(&untouched); err != nil || !untouched {
		t.Fatalf("upgrade fabricated charge proof or altered unknown recovery: untouched=%v error=%v", untouched, err)
	}
	if _, err = conn.Exec(t.Context(), `UPDATE warmup_tasks SET warmup_charged_date=(NOW() AT TIME ZONE 'UTC')::date WHERE task_id=$1`, completed); err == nil {
		t.Fatal("partial charge provenance accepted")
	}
	exec(`UPDATE warmup_tasks SET warmup_charged_date=(NOW() AT TIME ZONE 'UTC')::date,warmup_reply_charged=false WHERE task_id=$1`, completed)
	down, err := migrationsFS.ReadFile("migrations/000283_warmup_charge_provenance.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, refunded := range []bool{false, true} {
		if refunded {
			exec(`UPDATE warmup_tasks SET warmup_refunded_at=NOW() WHERE task_id=$1`, completed)
		}
		if _, err = conn.Exec(t.Context(), string(down)); err == nil || !strings.Contains(err.Error(), "Cannot remove recorded warmup charge") {
			t.Fatalf("rollback discarded charge/refund evidence (refunded=%v): %v", refunded, err)
		}
	}
	exec(`UPDATE warmup_tasks SET warmup_charged_date=NULL,warmup_reply_charged=NULL,warmup_refunded_at=NULL WHERE task_id=$1`, completed)
	if err = m.Steps(-1); err != nil {
		t.Fatal(err)
	}
	if err = m.Steps(1); err != nil {
		t.Fatal(err)
	}
	version, dirty, err := m.Version()
	if err != nil || version != 283 || dirty {
		t.Fatalf("final migrated schema version=%d dirty=%v error=%v", version, dirty, err)
	}
}
