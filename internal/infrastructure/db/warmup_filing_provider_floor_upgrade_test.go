package db

import (
	"context"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestLiveWarmupFilingProviderFloorUpgradePreservesLegacyLeasesAndRollback(t *testing.T) {
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
	name := "filing_floor_upgrade_" + uuid.NewString()[:8]
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
	if err := m.Migrate(283); err != nil {
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
	version := func(want uint) {
		t.Helper()
		got, dirty, err := m.Version()
		if err != nil || dirty || got != want {
			t.Fatalf("migration version=%d dirty=%v want=%d err=%v", got, dirty, want, err)
		}
	}
	version(283)
	exec(`SET TIME ZONE 'Pacific/Honolulu'`)
	user, org, mailbox := uuid.New(), uuid.New(), uuid.New()
	exec(`INSERT INTO users(id,email,first_name,last_name)VALUES($1,$2,'Upgrade','')`, user, user.String()+"@example.test")
	exec(`INSERT INTO organizations(id,name,owner_user_id)VALUES($1,'Upgrade',$2)`, org, user)
	exec(`INSERT INTO email_accounts(id,user_id,organization_id,email,name,signature_plain,signature_html,provider)VALUES($1,$2,$3,$4,'Upgrade','','','smtp_imap')`, mailbox, user, org, mailbox.String()+"@example.test")
	type filing struct {
		id      uuid.UUID
		key     string
		next    time.Time
		created time.Time
		payload string
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	rows := []filing{
		{uuid.New(), "legacy_due", now.Add(-time.Minute), now.Add(-time.Hour), ""},
		{uuid.New(), "legacy_lease", now.Add(time.Hour), now.Add(-2 * time.Hour), ""},
		{uuid.New(), "floor_extends_lease", now.Add(-time.Minute), now.Add(-3 * time.Hour), ""},
		{uuid.New(), "lease_extends_floor", now.Add(40 * time.Minute), now.Add(-4 * time.Hour), ""},
	}
	for i := range rows {
		r := &rows[i]
		exec(`INSERT INTO warmup_pending_filings(id,email_account_id,message_key,payload,next_attempt_at,created_at)VALUES($1,$2,$3,'{"actions":["file"],"rfc_message_id":"<legacy@example.test>"}',$4,$5)`, r.id, mailbox, r.key, r.next, r.created)
		if err := conn.QueryRow(t.Context(), `SELECT payload::text FROM warmup_pending_filings WHERE id=$1`, r.id).Scan(&r.payload); err != nil {
			t.Fatal(err)
		}
	}
	assertRows := func(withFloor bool) {
		t.Helper()
		var count int
		if err := conn.QueryRow(t.Context(), `SELECT count(*) FROM warmup_pending_filings`).Scan(&count); err != nil || count != len(rows) {
			t.Fatalf("pending row count=%d want=%d err=%v", count, len(rows), err)
		}
		for _, r := range rows {
			var account uuid.UUID
			var key, payload string
			var next, created time.Time
			if err := conn.QueryRow(t.Context(), `SELECT email_account_id,message_key,payload::text,next_attempt_at,created_at FROM warmup_pending_filings WHERE id=$1`, r.id).Scan(&account, &key, &payload, &next, &created); err != nil || account != mailbox || key != r.key || payload != r.payload || !next.Equal(r.next) || !created.Equal(r.created) {
				t.Fatalf("migration changed retained filing %s: next=%s want=%s err=%v", r.key, next, r.next, err)
			}
			if withFloor {
				var floor *time.Time
				if err := conn.QueryRow(t.Context(), `SELECT provider_retry_at FROM warmup_pending_filings WHERE id=$1`, r.id).Scan(&floor); err != nil || floor != nil {
					t.Fatalf("migration invented provider proof for %s: floor=%v err=%v", r.key, floor, err)
				}
			}
		}
	}
	if err := m.Migrate(284); err != nil {
		t.Fatal(err)
	}
	version(284)
	assertRows(true)
	var nullable, noDefault bool
	if err := conn.QueryRow(t.Context(), `SELECT is_nullable='YES',column_default IS NULL FROM information_schema.columns WHERE table_schema='public' AND table_name='warmup_pending_filings' AND column_name='provider_retry_at'`).Scan(&nullable, &noDefault); err != nil || !nullable || !noDefault {
		t.Fatalf("provider floor acquired an inferred default: nullable=%v noDefault=%v err=%v", nullable, noDefault, err)
	}
	t.Log("283 -> 284: all four pending legacy rows, payloads and leases preserved; no provider floor inferred")
	floor := now.Add(10 * time.Minute)
	exec(`UPDATE warmup_pending_filings SET provider_retry_at=$2 WHERE id IN($1,$3)`, rows[2].id, floor, rows[3].id)
	if err := m.Steps(-1); err != nil {
		t.Fatal(err)
	}
	version(283)
	rows[2].next = floor
	assertRows(false)
	var absent bool
	if err := conn.QueryRow(t.Context(), `SELECT NOT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='warmup_pending_filings' AND column_name='provider_retry_at')`).Scan(&absent); err != nil || !absent {
		t.Fatalf("downgrade retained provider column: absent=%v err=%v", absent, err)
	}
	var due []uuid.UUID
	if err := conn.QueryRow(t.Context(), `SELECT array_agg(id) FROM warmup_pending_filings WHERE next_attempt_at <= NOW()`).Scan(&due); err != nil || len(due) != 1 || due[0] != rows[0].id {
		t.Fatalf("downgrade shortened provider floor or legacy lease: due=%v err=%v", due, err)
	}
	t.Log("284 -> 283: floor moved to ordinary retry time, longer lease retained, legacy due row stays due; no evidence rows removed")
	if err := m.Migrate(284); err != nil {
		t.Fatal(err)
	}
	version(284)
	assertRows(true)
	t.Log("283 -> 284 re-upgrade: retained queue deadlines unchanged; no provider proof reconstructed")
}
