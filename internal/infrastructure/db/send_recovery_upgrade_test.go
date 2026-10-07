package db

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestLiveSendRecoveryUpgradesReleased265WithoutInventingHistory(t *testing.T) {
	dsn := os.Getenv("WARMBLY_TEST_DB")
	if dsn == "" {
		t.Skip("WARMBLY_TEST_DB not set")
	}
	u, err := url.Parse(dsn)
	if err != nil || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") {
		t.Fatal("upgrade fixture requires a local PostgreSQL URL")
	}
	ctx := t.Context()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(context.Background())
	name := "recovery_upgrade_" + uuid.New().String()[:8]
	quoted := pgx.Identifier{name}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+quoted); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec(context.Background(), "DROP DATABASE "+quoted); err != nil {
			t.Errorf("drop fixture: %v", err)
		}
	}()
	u.Path = "/" + name
	fixtureDSN := u.String()
	source, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	m, err := openMigrate(source, fixtureDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err := m.Migrate(265); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, fixtureDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	user, org, mailbox, sentTask, unknownTask := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, row := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO users (id,email,first_name,last_name) VALUES ($1,$2,'Legacy','Fixture')`, []any{user, user.String() + "@example.test"}},
		{`INSERT INTO organizations (id,name,slug,owner_user_id) VALUES ($1,'Legacy',$2,$3)`, []any{org, org.String(), user}},
		{`INSERT INTO email_accounts (id,user_id,organization_id,email,name,signature_plain,signature_html,provider,status,warmup_max,campaign_limit,send_lifecycle,send_lifecycle_reason) VALUES ($1,$2,$3,$4,'Legacy','','','smtp_imap','inactive',17,23,'resting','existing health hold')`, []any{mailbox, user, org, mailbox.String() + "@example.test"}},
		{`INSERT INTO tasks (id,email_account_id,task_type,status,message_id) VALUES ($1,$3,'email','completed','<known@provider.test>'),($2,$3,'email','completed','')`, []any{sentTask, unknownTask, mailbox}},
	} {
		if _, err := conn.Exec(ctx, row.sql, row.args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.Up(); err != nil {
		t.Fatal(err)
	}
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	var down []byte
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), "_send_result_recovery.down.sql") {
			down, err = migrationsFS.ReadFile("migrations/" + entry.Name())
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(down) == 0 {
		t.Fatal("recovery down migration missing")
	}
	for _, restriction := range []string{
		`UPDATE email_accounts SET send_recovery_hold=true WHERE id=$1`,
		`UPDATE email_accounts SET send_cooldown_until=NOW()+INTERVAL '10 minutes' WHERE id=$1`,
	} {
		if _, err := conn.Exec(ctx, restriction, mailbox); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(ctx, string(down)); err == nil || !strings.Contains(err.Error(), "unresolved send recovery") {
			t.Fatalf("rollback discarded restriction: %v", err)
		}
		if _, err := conn.Exec(ctx, `UPDATE email_accounts SET send_recovery_hold=false,send_cooldown_until=NULL WHERE id=$1`, mailbox); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := conn.Exec(ctx, `UPDATE tasks SET send_result_state='unknown' WHERE id=$1`, unknownTask); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, string(down)); err == nil || !strings.Contains(err.Error(), "unresolved send recovery") {
		t.Fatalf("rollback discarded unknown outcome: %v", err)
	}
	if _, err := conn.Exec(ctx, `UPDATE tasks SET send_result_state=NULL WHERE id=$1`, unknownTask); err != nil {
		t.Fatal(err)
	}
	assertLegacy := func() {
		t.Helper()
		var status, lifecycle, reason string
		var warmupMax, campaignLimit int
		var hold bool
		var cooldown *time.Time
		if err := conn.QueryRow(ctx, `SELECT status::text,send_lifecycle,send_lifecycle_reason,warmup_max,campaign_limit,send_recovery_hold,send_cooldown_until FROM email_accounts WHERE id=$1`, mailbox).Scan(&status, &lifecycle, &reason, &warmupMax, &campaignLimit, &hold, &cooldown); err != nil {
			t.Fatal(err)
		}
		if status != "inactive" || lifecycle != "resting" || reason != "existing health hold" || warmupMax != 17 || campaignLimit != 23 || hold || cooldown != nil {
			t.Fatal("migration altered legacy mailbox settings, holds, or fabricated cooldown")
		}
		for id, want := range map[uuid.UUID]string{sentTask: "<known@provider.test>", unknownTask: ""} {
			var state *string
			var applied *time.Time
			var evidence []byte
			var message string
			if err := conn.QueryRow(ctx, `SELECT message_id,send_result_state,send_result_applied_at,send_result_evidence FROM tasks WHERE id=$1`, id).Scan(&message, &state, &applied, &evidence); err != nil {
				t.Fatal(err)
			}
			if message != want || state != nil || applied != nil || evidence != nil {
				t.Fatal("migration rewrote legacy provider identity or invented result evidence")
			}
		}
	}
	assertLegacy()
	if err := RunMigrations(fixtureDSN); err != nil {
		t.Fatal(err)
	}
	assertLegacy()
	if err := m.Migrate(265); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatal(err)
	}
	if err := m.Up(); err != nil {
		t.Fatal(err)
	}
	assertLegacy()
}
