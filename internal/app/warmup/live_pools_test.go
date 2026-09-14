package warmup

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/models"
)

// requireSchemaVersion fails loudly on a database behind the branch, before a
// fixture writes a row it would then fail to clean up.
func requireSchemaVersion(t *testing.T, pool *pgxpool.Pool, min int64) {
	t.Helper()
	var version int64
	if err := pool.QueryRow(context.Background(), `SELECT version FROM schema_migrations LIMIT 1`).Scan(&version); err != nil || version < min {
		t.Fatalf("WARMBLY_TEST_DB is at schema version %d (err %v); this branch needs %d or later", version, err, min)
	}
}

// requireSeededPools fails loudly on a database that predates migration
// 000155 rather than on a membership row that was never inserted.
func requireSeededPools(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM warmup_pools WHERE id IN ($1, $2)`, models.WarmupPoolFreeID, models.WarmupPoolPremiumID).Scan(&n); err != nil || n != 2 {
		t.Fatalf("the canonical warmup pools are not seeded (migrate to 000155 or later): found %d, err %v", n, err)
	}
}

// poolIDFor maps a pool type a fixture was handed to its seeded id; an
// unknown type is a test bug, not the zero uuid.
func poolIDFor(t *testing.T, poolType string) uuid.UUID {
	t.Helper()
	id, ok := models.WarmupPoolID(poolType)
	if !ok {
		t.Fatalf("unknown warmup pool type %q", poolType)
	}
	return id
}

// execSQL runs one fixture statement and fails the test on error.
func execSQL(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("fixture %q: %v", sql[:min(60, len(sql))], err)
	}
}

// A fresh instance warms within its own pools from the first tick, and there
// is exactly one pool per type under the ids the runtime keys on.
func TestLiveWarmupPoolsAreSeededAndUnique(t *testing.T) {
	_, handle := liveWarmupRepo(t)
	requireSeededPools(t, handle.Pool)
	for kind, want := range map[string]uuid.UUID{"free": models.WarmupPoolFreeID, "premium": models.WarmupPoolPremiumID} {
		var got uuid.UUID
		if err := handle.Pool.QueryRow(context.Background(), `SELECT id FROM warmup_pools WHERE pool_type = $1::warmup_pool_type`, kind).Scan(&got); err != nil || got != want {
			t.Fatalf("pool %s = %s, %v; want the seeded pool %s", kind, got, err, want)
		}
	}
	_, err := handle.Pool.Exec(context.Background(),
		`INSERT INTO warmup_pools (pool_type, name) VALUES ('free', 'a second free pool')`)
	if err == nil {
		execSQL(t, handle.Pool, `DELETE FROM warmup_pools WHERE name = 'a second free pool'`)
		t.Fatal("a second free pool was accepted; warmup_pools_pool_type_key is missing")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" || pgErr.ConstraintName != "warmup_pools_pool_type_key" {
		t.Fatalf("a second free pool was refused for the wrong reason: %v", err)
	}
}
