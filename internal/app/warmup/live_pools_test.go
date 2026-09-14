package warmup

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/models"
)

// livePoolIDs are the pools migration 000154 seeds on every instance.
var livePoolIDs = map[string]uuid.UUID{
	"free":    models.WarmupPoolFreeID,
	"premium": models.WarmupPoolPremiumID,
}

// seededWarmupPools returns the canonical pool ids, failing loudly on a
// database that predates the migration rather than on a membership row that
// was never inserted.
func seededWarmupPools(t *testing.T, pool *pgxpool.Pool) map[string]uuid.UUID {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM warmup_pools WHERE id IN ($1, $2)`, livePoolIDs["free"], livePoolIDs["premium"]).Scan(&n); err != nil || n != 2 {
		t.Fatalf("the canonical warmup pools are not seeded (migrate to 000154 or later): found %d, err %v", n, err)
	}
	return livePoolIDs
}

// execSQL runs one fixture statement and fails the test on error.
func execSQL(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("fixture %q: %v", sql[:min(60, len(sql))], err)
	}
}

// A fresh instance warms within its own pools from the first tick, and there
// is exactly one pool per type, which is what GetPoolByType's LIMIT 1 assumes.
func TestLiveWarmupPoolsAreSeededAndUnique(t *testing.T) {
	repo, handle := liveWarmupRepo(t)
	seededWarmupPools(t, handle.Pool)
	for kind, want := range livePoolIDs {
		got, err := repo.GetPoolByType(context.Background(), kind)
		if err != nil || got == nil || got.ID != want {
			t.Fatalf("GetPoolByType(%s) = %v, %v; want the seeded pool %s", kind, got, err, want)
		}
	}
	var perType int
	if err := handle.Pool.QueryRow(context.Background(),
		`SELECT max(c) FROM (SELECT count(*) AS c FROM warmup_pools GROUP BY pool_type) x`).Scan(&perType); err != nil {
		t.Fatalf("count pools: %v", err)
	}
	if perType != 1 {
		t.Fatalf("a pool type has %d pools, want exactly 1", perType)
	}
	if _, err := handle.Pool.Exec(context.Background(),
		`INSERT INTO warmup_pools (pool_type, name) VALUES ('free', 'a second free pool')`); err == nil {
		execSQL(t, handle.Pool, `DELETE FROM warmup_pools WHERE name = 'a second free pool'`)
		t.Fatal("a second free pool was accepted; warmup_pools_pool_type_key is missing")
	}
}
