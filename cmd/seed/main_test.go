// Tests for the dev seeder.
//
// These tests need a real Postgres because the seeder issues real SQL. To
// avoid clobbering a developer's working database by accident, the tests
// only run when SEED_TEST_DB is set:
//
//   make test-seed       (sets SEED_TEST_DB to the docker-compose postgres)
//
// What's covered:
//
//   - seedBaseline + seedRich complete without error on a freshly migrated DB
//   - All expected fixture rows are present afterwards
//   - Running the seed a second time produces no errors and no duplicates
//     (idempotency is the whole point of the seeder)

package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
)

func openTestDB(t *testing.T) (*pgxpool.Pool, context.Context) {
	t.Helper()
	dsn := os.Getenv("SEED_TEST_DB")
	if dsn == "" {
		t.Skip("set SEED_TEST_DB to run seeder tests (e.g. make test-seed)")
	}

	// Run migrations first — the seeder assumes a complete schema.
	if err := db.RunMigrations(dsn); err != nil {
		t.Fatalf("migrations: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	// Wipe the seed-managed tables only. CASCADE handles join tables and
	// dependent rows. Wrapped in a transaction so the test either starts
	// from a clean slate or fails loudly.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	for _, stmt := range []string{
		`DELETE FROM campaign_leads WHERE contact_id::text LIKE '66666666-%'`,
		`DELETE FROM sequences WHERE id::text LIKE '55555555-%'`,
		`DELETE FROM campaigns WHERE id::text LIKE '44444444-%'`,
		`DELETE FROM contacts WHERE id::text LIKE '66666666-%'`,
		`DELETE FROM warmup_pool_participants WHERE email_account_id::text LIKE '33333333-%'`,
		`DELETE FROM email_accounts WHERE id::text LIKE '33333333-%'`,
		`DELETE FROM workers WHERE id IN ($1, $2, $3)`,
		`DELETE FROM organization_members WHERE organization_id::text LIKE '22222222-%'`,
		`DELETE FROM organizations WHERE id::text LIKE '22222222-%'`,
		`DELETE FROM users WHERE id::text LIKE '11111111-%'`,
	} {
		if stmt == `DELETE FROM workers WHERE id IN ($1, $2, $3)` {
			_, err = tx.Exec(ctx, stmt, workerShared, workerPremium, workerDedicated)
		} else {
			_, err = tx.Exec(ctx, stmt)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			t.Fatalf("cleanup %q: %v", stmt, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit cleanup: %v", err)
	}

	return pool, ctx
}

func TestSeedBaseline_FreshAndIdempotent(t *testing.T) {
	pool, ctx := openTestDB(t)

	// First run.
	if err := seedBaseline(ctx, pool); err != nil {
		t.Fatalf("first seedBaseline: %v", err)
	}
	assertUserExists(t, pool, ctx, "dev@warmbly.com")
	assertOrgExists(t, pool, ctx, "dev")
	usersBefore := countRows(t, pool, ctx, `SELECT COUNT(*) FROM users WHERE id::text LIKE '11111111-%'`)
	if usersBefore != 1 {
		t.Fatalf("expected 1 dev user, got %d", usersBefore)
	}

	// Second run should not error and should not duplicate.
	if err := seedBaseline(ctx, pool); err != nil {
		t.Fatalf("second seedBaseline: %v", err)
	}
	usersAfter := countRows(t, pool, ctx, `SELECT COUNT(*) FROM users WHERE id::text LIKE '11111111-%'`)
	if usersAfter != usersBefore {
		t.Fatalf("seedBaseline not idempotent: %d → %d", usersBefore, usersAfter)
	}
}

func TestSeedRich_FreshAndIdempotent(t *testing.T) {
	pool, ctx := openTestDB(t)

	// Rich depends on baseline.
	if err := seedBaseline(ctx, pool); err != nil {
		t.Fatalf("seedBaseline: %v", err)
	}
	if err := seedRich(ctx, pool); err != nil {
		t.Fatalf("first seedRich: %v", err)
	}

	// Expected fixture counts after rich seed.
	checks := []struct {
		name     string
		sql      string
		expected int64
	}{
		{"dev + 3 rich users", `SELECT COUNT(*) FROM users WHERE id::text LIKE '11111111-%'`, 4},
		{"4 orgs (dev + 3)", `SELECT COUNT(*) FROM organizations WHERE id::text LIKE '22222222-%'`, 4},
		{"3 workers", `SELECT COUNT(*) FROM workers WHERE id IN ($1, $2, $3)`, 3},
		{"6 email accounts", `SELECT COUNT(*) FROM email_accounts WHERE id::text LIKE '33333333-%'`, 6},
		{"1 campaign", `SELECT COUNT(*) FROM campaigns WHERE id::text LIKE '44444444-%'`, 1},
		{"2 sequences", `SELECT COUNT(*) FROM sequences WHERE id::text LIKE '55555555-%'`, 2},
		{"10 contacts", `SELECT COUNT(*) FROM contacts WHERE id::text LIKE '66666666-%'`, 10},
		{"2 unsubscribed contacts", `SELECT COUNT(*) FROM contacts WHERE id::text LIKE '66666666-%' AND subscribed = false`, 2},
		{"10 campaign leads", `SELECT COUNT(*) FROM campaign_leads cl JOIN contacts c ON c.id = cl.contact_id WHERE c.id::text LIKE '66666666-%'`, 10},
	}
	for _, ch := range checks {
		var got int64
		var err error
		if ch.sql == `SELECT COUNT(*) FROM workers WHERE id IN ($1, $2, $3)` {
			err = pool.QueryRow(ctx, ch.sql, workerShared, workerPremium, workerDedicated).Scan(&got)
		} else {
			err = pool.QueryRow(ctx, ch.sql).Scan(&got)
		}
		if err != nil {
			t.Fatalf("%s: query: %v", ch.name, err)
		}
		if got != ch.expected {
			t.Errorf("%s: got %d, want %d", ch.name, got, ch.expected)
		}
	}

	// Re-run rich; counts must not change.
	if err := seedRich(ctx, pool); err != nil {
		t.Fatalf("second seedRich: %v", err)
	}
	for _, ch := range checks {
		var got int64
		var err error
		if ch.sql == `SELECT COUNT(*) FROM workers WHERE id IN ($1, $2, $3)` {
			err = pool.QueryRow(ctx, ch.sql, workerShared, workerPremium, workerDedicated).Scan(&got)
		} else {
			err = pool.QueryRow(ctx, ch.sql).Scan(&got)
		}
		if err != nil {
			t.Fatalf("%s (after re-run): query: %v", ch.name, err)
		}
		if got != ch.expected {
			t.Errorf("%s not idempotent: got %d, want %d", ch.name, got, ch.expected)
		}
	}
}

func TestSeedRich_WarmupPoolMembership(t *testing.T) {
	pool, ctx := openTestDB(t)
	if err := seedBaseline(ctx, pool); err != nil {
		t.Fatalf("seedBaseline: %v", err)
	}
	if err := seedRich(ctx, pool); err != nil {
		t.Fatalf("seedRich: %v", err)
	}

	// 2 Acme accounts (free) should be in the free pool; 4 Beta+Gamma
	// accounts in premium.
	cases := []struct {
		pool string
		want int64
	}{
		{"free", 2},
		{"premium", 4},
	}
	for _, c := range cases {
		var got int64
		err := pool.QueryRow(ctx, `
			SELECT COUNT(*)
			FROM warmup_pool_participants wp
			JOIN warmup_pools p ON p.id = wp.pool_id
			JOIN email_accounts ea ON ea.id = wp.email_account_id
			WHERE p.pool_type = $1
			  AND ea.id::text LIKE '33333333-%'
		`, c.pool).Scan(&got)
		if err != nil {
			t.Fatalf("pool %s: %v", c.pool, err)
		}
		if got != c.want {
			t.Errorf("pool %s: got %d members, want %d", c.pool, got, c.want)
		}
	}
}

// helpers

func assertUserExists(t *testing.T, pool *pgxpool.Pool, ctx context.Context, email string) {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE email = $1)`, email).Scan(&exists); err != nil {
		t.Fatalf("query user %s: %v", email, err)
	}
	if !exists {
		t.Fatalf("user %s not found after seed", email)
	}
}

func assertOrgExists(t *testing.T, pool *pgxpool.Pool, ctx context.Context, slug string) {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM organizations WHERE slug = $1)`, slug).Scan(&exists); err != nil {
		t.Fatalf("query org %s: %v", slug, err)
	}
	if !exists {
		t.Fatalf("org %s not found after seed", slug)
	}
}

func countRows(t *testing.T, pool *pgxpool.Pool, ctx context.Context, sql string) int64 {
	t.Helper()
	var n int64
	if err := pool.QueryRow(ctx, sql).Scan(&n); err != nil {
		t.Fatalf("count query %q: %v", sql, err)
	}
	return n
}
