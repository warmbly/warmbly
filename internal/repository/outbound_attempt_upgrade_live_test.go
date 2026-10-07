package repository

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestLiveReleased265AttemptUpgradePreservesLegacyConsentAndFailedAttempts(t *testing.T) {
	dsn := os.Getenv("WARMBLY_ATTEMPT_TEST_DB")
	if dsn == "" {
		t.Skip("WARMBLY_ATTEMPT_TEST_DB must name an empty dedicated scratch database")
	}
	source, err := iofs.New(os.DirFS("../infrastructure/db"), "migrations")
	if err != nil {
		t.Fatal(err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", source, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if _, _, err := m.Version(); !errors.Is(err, migrate.ErrNilVersion) {
		t.Fatal("requires empty scratch database")
	}
	if err = m.Migrate(265); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	user, org, mailbox, newMailbox, task, nonce := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	exec(`INSERT INTO users(id,email,first_name,last_name)VALUES($1,'upgrade@example.test','Upgrade','Fixture')`, user)
	exec(`INSERT INTO organizations(id,name,slug,owner_user_id)VALUES($1,'Upgrade','upgrade-fixture',$2)`, org, user)
	exec(`INSERT INTO email_accounts(id,user_id,organization_id,email,name,signature_plain,signature_html,provider,status,campaign_limit,min_wait_time,timezone,warmup)VALUES($1,$2,$3,'legacy@example.test','Legacy','','','smtp_imap','active',50,600,'UTC',NOW()-INTERVAL '40 days')`, mailbox, user, org)
	if err = m.Migrate(273); err != nil {
		t.Fatal(err)
	}
	var legacy bool
	if err = pool.QueryRow(ctx, `SELECT test_mode IS NULL AND NOT test_send_enabled AND NOT test_receive_enabled AND warmup IS NOT NULL FROM email_accounts WHERE id=$1`, mailbox).Scan(&legacy); err != nil || !legacy {
		t.Fatal("old consent or history changed", err)
	}
	exec(`INSERT INTO email_accounts(id,user_id,organization_id,email,name,signature_plain,signature_html,provider,status,campaign_limit,min_wait_time,timezone)VALUES($1,$2,$3,'new@example.test','New','','','smtp_imap','active',50,600,'UTC')`, newMailbox, user, org)
	exec(`INSERT INTO tasks(id,task_type,email_account_id,status,message_id,send_executor_nonce,send_executor_started_at,send_recipients,send_released_at,send_result_state,send_result_applied_at)VALUES($1,'email',$2,'failed','failed@example.test',$3,NOW(),ARRAY['r@example.test','r@example.test','c@example.test'],NOW(),'failed',NOW())`, task, mailbox, nonce)
	if err = m.Up(); err != nil {
		t.Fatal(err)
	}
	assertLedger := func() {
		t.Helper()
		var count, recipients int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*),COALESCE(SUM(recipient_count),0) FROM outbound_attempts WHERE nonce=$1 AND task_id=$2 AND provider='smtp_imap'`, nonce, task).Scan(&count, &recipients); err != nil || count != 1 || recipients != 3 {
			t.Fatal("failed/refunded attempt not preserved exactly once", count, recipients, err)
		}
	}
	assertLedger()
	var off bool
	if err = pool.QueryRow(ctx, `SELECT test_mode='off' AND NOT test_send_enabled AND NOT test_receive_enabled FROM email_accounts WHERE id=$1`, newMailbox).Scan(&off); err != nil || !off {
		t.Fatal("new mailbox inferred consent", err)
	}
	if err = m.Up(); !errors.Is(err, migrate.ErrNoChange) {
		t.Fatal("repeat startup", err)
	}
	assertLedger()
	down, err := os.ReadFile("../infrastructure/db/migrations/000274_outbound_attempt_evidence.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, guardErr := tx.Exec(ctx, string(down))
	_ = tx.Rollback(ctx)
	if guardErr == nil || !strings.Contains(guardErr.Error(), "recent provider attempts") {
		t.Fatal("rollback discarded recent negative attempt evidence", guardErr)
	}
	assertLedger()
	exec(`UPDATE outbound_attempts SET attempted_at=NOW()-INTERVAL '25 hours' WHERE nonce=$1`, nonce)
	exec(`UPDATE tasks SET send_executor_started_at=NOW()-INTERVAL '25 hours' WHERE id=$1`, task)
	if err = m.Steps(-1); err != nil {
		t.Fatal("expired-window rollback", err)
	}
	if err = m.Up(); err != nil {
		t.Fatal("re-upgrade", err)
	}
	assertLedger()
	version, dirty, err := m.Version()
	if err != nil || dirty || version != 274 {
		t.Fatal("final migration state", version, dirty, err)
	}
}
