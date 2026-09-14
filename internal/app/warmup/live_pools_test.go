package warmup

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The canonical pool ids the sandbox seeds; every live suite that needs a pool
// keys on them, so a scratch database ends up with the same rows a dev one has.
var livePoolIDs = map[string]uuid.UUID{
	"free":    uuid.MustParse("77777777-aaaa-0000-0000-000000000001"),
	"premium": uuid.MustParse("77777777-aaaa-0000-0000-000000000002"),
}

// ensureWarmupPools creates the canonical pools when absent. Pools are seed
// data, not migration data, so a scratch database has none until this runs.
func ensureWarmupPools(t *testing.T, pool *pgxpool.Pool) map[string]uuid.UUID {
	t.Helper()
	execSQL(t, pool, `
		INSERT INTO warmup_pools (id, pool_type, name, description, max_participants)
		VALUES ($1, 'free', 'Free warmup pool', 'Created by the live tests', 1000),
		       ($2, 'premium', 'Premium warmup pool', 'Created by the live tests', 1000)
		ON CONFLICT (id) DO NOTHING`, livePoolIDs["free"], livePoolIDs["premium"])
	return livePoolIDs
}

// execSQL runs one fixture statement and fails the test on error.
func execSQL(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("fixture %q: %v", sql[:min(60, len(sql))], err)
	}
}
