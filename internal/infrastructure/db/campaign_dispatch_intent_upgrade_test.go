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

func TestLiveCampaignDispatchIntentUpgradePreservesUnverifiedHistoryAndRollback(t *testing.T) {
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
	name := "campaign_intent_upgrade_" + uuid.New().String()[:8]
	quoted := pgx.Identifier{name}.Sanitize()
	if _, err := admin.Exec(t.Context(), "CREATE DATABASE "+quoted); err != nil {
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
	if err := m.Migrate(281); err != nil {
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
	user, org, mailbox, campaign, contact, step := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	legacy, bound, wakeup := uuid.New(), uuid.New(), uuid.New()
	exec(`INSERT INTO users(id,email,first_name,last_name) VALUES($1,$2,'Upgrade','')`, user, user.String()+"@example.test")
	exec(`INSERT INTO organizations(id,name,owner_user_id) VALUES($1,'Upgrade',$2)`, org, user)
	exec(`INSERT INTO email_accounts(id,user_id,organization_id,email,name,signature_plain,signature_html,provider,send_recovery_hold,send_recovery_reason,send_recovery_task_id) VALUES($1,$2,$3,$4,'Upgrade','','','smtp_imap',true,'unknown',$5)`, mailbox, user, org, mailbox.String()+"@example.test", legacy)
	exec(`INSERT INTO campaigns(id,user_id,organization_id,name,description,status,days,timezone,created_at,updated_at) VALUES($1,$2,$3,'Upgrade','','active',127,'UTC',NOW(),NOW())`, campaign, user, org)
	exec(`INSERT INTO contacts(id,user_id,organization_id,email,first_name,last_name,company,phone,custom_fields) VALUES($1,$2,$3,'lead@example.test','','','','','{}')`, contact, user, org)
	exec(`INSERT INTO sequences(id,campaign_id,organization_id,name,subject,body_plain,body_html,wait_after,position) VALUES($1,$2,$3,'Step','Subject','Text','',0,1)`, step, campaign, org)
	for _, task := range []uuid.UUID{legacy, bound} {
		exec(`INSERT INTO tasks(id,email_account_id,task_type,status,message_id) VALUES($1,$2,'campaign','dead_lettered','')`, task, mailbox)
		exec(`INSERT INTO campaign_tasks(task_id,campaign_id) VALUES($1,$2)`, task, campaign)
		exec(`INSERT INTO task_dead_letters(task_id,task_type,payload,last_error,attempts) VALUES($1,'campaign','{"dispatch_intent":"wakeup"}','original',1)`, task)
	}
	exec(`UPDATE campaign_tasks SET contact_id=$2,sequence_id=$3 WHERE task_id=$1`, bound, contact, step)
	if err := m.Up(); err != nil {
		t.Fatal(err)
	}
	var unchanged bool
	if err := conn.QueryRow(t.Context(), `SELECT COUNT(*)=2 AND bool_and(ct.dispatch_intent='unverified' AND t.send_result_state IS NULL AND t.send_result_applied_at IS NULL AND d.status='pending' AND d.attempts=1 AND d.last_error='original' AND d.payload='{"dispatch_intent":"wakeup"}'::jsonb AND ea.send_recovery_hold AND ea.send_recovery_reason='unknown') FROM campaign_tasks ct JOIN tasks t ON t.id=ct.task_id JOIN task_dead_letters d ON d.task_id=t.id JOIN email_accounts ea ON ea.id=t.email_account_id`).Scan(&unchanged); err != nil || !unchanged {
		t.Fatal("upgrade granted authority or changed unknown evidence", unchanged, err)
	}
	exec(`UPDATE campaign_tasks SET dispatch_intent='wakeup' WHERE task_id=$1`, legacy)
	var intent string
	if err := conn.QueryRow(t.Context(), `SELECT dispatch_intent::text FROM campaign_tasks WHERE task_id=$1`, legacy).Scan(&intent); err != nil || intent != "unverified" {
		t.Fatal("legacy promoted to verified wakeup", intent, err)
	}
	exec(`DELETE FROM contacts WHERE id=$1`, contact)
	exec(`DELETE FROM sequences WHERE id=$1`, step)
	exec(`UPDATE campaign_tasks SET dispatch_intent='wakeup' WHERE task_id=$1`, bound)
	if err := conn.QueryRow(t.Context(), `SELECT dispatch_intent FROM campaign_tasks WHERE task_id=$1`, bound).Scan(&intent); err != nil || intent != "send" {
		t.Fatal("FK cleanup forgot prior bound send", intent, err)
	}
	exec(`INSERT INTO tasks(id,email_account_id,task_type,status,message_id) VALUES($1,$2,'campaign','dead_lettered','')`, wakeup, mailbox)
	exec(`INSERT INTO campaign_tasks(task_id,campaign_id,dispatch_intent) VALUES($1,$2,'wakeup')`, wakeup, campaign)
	if _, err := conn.Exec(t.Context(), `UPDATE campaign_tasks SET dispatch_intent='arbitrary' WHERE task_id=$1`, wakeup); err == nil {
		t.Fatal("intent accepted arbitrary value")
	}
	down, err := migrationsFS.ReadFile("migrations/000282_campaign_dispatch_intent.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(t.Context(), string(down)); err == nil || !strings.Contains(err.Error(), "restrictive recovery evidence") {
		t.Fatal("rollback discarded retained send evidence", err)
	}
	exec(`DELETE FROM tasks WHERE id=$1`, bound)
	if _, err := conn.Exec(t.Context(), string(down)); err == nil || !strings.Contains(err.Error(), "restrictive recovery evidence") {
		t.Fatal("rollback discarded unverified pending dead letter", err)
	}
	// Only this isolated fixture is removed to exercise a clean down/up pair.
	exec(`DELETE FROM tasks WHERE id IN($1,$2)`, legacy, wakeup)
	exec(string(down))
	up, err := migrationsFS.ReadFile("migrations/000282_campaign_dispatch_intent.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	exec(string(up))
	exec(`INSERT INTO tasks(id,email_account_id,task_type,status,message_id) VALUES($1,$2,'campaign','dead_lettered','')`, legacy, mailbox)
	exec(`INSERT INTO campaign_tasks(task_id,campaign_id) VALUES($1,$2)`, legacy, campaign)
	if err := conn.QueryRow(t.Context(), `SELECT dispatch_intent::text FROM campaign_tasks WHERE task_id=$1`, legacy).Scan(&intent); err != nil || intent != "unverified" {
		t.Fatal("clean down/up lost restrictive default", intent, err)
	}
}
