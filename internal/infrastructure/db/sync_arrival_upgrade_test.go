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

func TestLiveSyncArrivalUpgradePreservesLegacyMaps(t *testing.T) {
	dsn := os.Getenv("WARMBLY_TEST_DB")
	if dsn == "" {
		t.Skip("WARMBLY_TEST_DB not set")
	}
	u, err := url.Parse(dsn)
	if err != nil || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") {
		t.Fatal("upgrade fixture requires local PostgreSQL")
	}
	ctx := t.Context()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(context.Background())
	name := "arrival_upgrade_" + uuid.NewString()[:8]
	quoted := pgx.Identifier{name}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+quoted); err != nil {
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
	if err := m.Migrate(277); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	user, org, mailbox, id := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, row := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO users(id,email,first_name,last_name)VALUES($1,$2,'Legacy','Fixture')`, []any{user, user.String() + "@test.local"}},
		{`INSERT INTO organizations(id,name,slug,owner_user_id)VALUES($1,'Legacy',$2,$3)`, []any{org, org.String(), user}},
		{`INSERT INTO email_accounts(id,user_id,organization_id,email,name,signature_plain,signature_html,provider)VALUES($1,$2,$3,$4,'Legacy','','','smtp_imap')`, []any{mailbox, user, org, mailbox.String() + "@test.local"}},
		{`INSERT INTO email_sync_state(email_id,user_id,backfill_status,backfill_synced)VALUES($1,$2,'complete',42)`, []any{mailbox, user}},
		{`INSERT INTO email_message_map(user_id,email_id,message_id,id,thread_id)VALUES($1,$2,'legacy',$3,'thread')`, []any{user, mailbox, id}},
	} {
		if _, err := conn.Exec(ctx, row.sql, row.args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.Up(); err != nil {
		t.Fatal(err)
	}
	var retained bool
	if err := conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM email_message_map m JOIN email_sync_state s ON s.email_id=m.email_id WHERE m.id=$1 AND s.backfill_status='complete' AND s.backfill_synced=42 AND m.thread_id='thread')`, id).Scan(&retained); err != nil || !retained {
		t.Fatal("upgrade changed legacy map or completed import", err)
	}
	var count int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM sync_arrival_outbox`).Scan(&count); err != nil || count != 0 {
		t.Fatal("upgrade invented pending arrivals", err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO sync_arrival_outbox(user_id,email_id,organization_id,message_id,id,payload)VALUES($1,$2,$3,'legacy',$4,'fixture')`, user, mailbox, org, id); err != nil {
		t.Fatal(err)
	}
	down, err := migrationsFS.ReadFile("migrations/000279_sync_arrival_outbox.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, string(down)); err == nil || !strings.Contains(err.Error(), "unresolved sync arrival") {
		t.Fatalf("downgrade discarded pending delivery: %v", err)
	}
	if _, err := conn.Exec(ctx, `DELETE FROM sync_arrival_outbox`); err != nil {
		t.Fatal(err)
	}
	if err := m.Steps(-1); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM email_message_map WHERE id=$1`, id).Scan(&count); err != nil || count != 1 {
		t.Fatal("downgrade removed canonical map", err)
	}
	if err := m.Up(); err != nil {
		t.Fatal(err)
	}
}
