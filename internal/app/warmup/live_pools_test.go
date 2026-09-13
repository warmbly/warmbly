package warmup

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ensureWarmupPools creates the free and premium pools when they are missing.
// Pools are seed data, not migration data, so a scratch database has none;
// before this the package's live fixtures either skipped on that or inserted
// a participant row for a pool that was not there and failed on its absence,
// which is why they had never run on a fresh database. A seeded database is
// left as it is.
func ensureWarmupPools(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	for _, p := range []struct{ kind, name string }{{"free", "Free warmup pool"}, {"premium", "Premium warmup pool"}} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO warmup_pools (pool_type, name)
			SELECT $1::warmup_pool_type, $2
			WHERE NOT EXISTS (SELECT 1 FROM warmup_pools WHERE pool_type = $1::warmup_pool_type)`,
			p.kind, p.name); err != nil {
			t.Fatalf("ensure %s pool: %v", p.kind, err)
		}
	}
}
