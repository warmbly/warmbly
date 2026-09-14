package instancecheck

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"path/filepath"
	"time"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
)

const (
	docsEventBus     = "/development/configuration/#event-bus"
	docsStorage      = "/development/configuration/#storage"
	docsHealthWorker = "/development/instance-health/#workers"
	docsHealthDB     = "/development/instance-health/#database"
	docsHealthRedis  = "/development/instance-health/#redis"
)

// workerLivenessWindow is how stale a heartbeat may be before the placement
// layer stops considering a worker live.
const workerLivenessWindow = 5 * time.Minute

func infraChecks() []check {
	return []check{
		{id: "no_worker_heartbeat", run: checkNoWorkerHeartbeat},
		{id: "codec_not_json", run: checkCodecNotJSON},
		{id: "migrations_dirty", run: checkMigrationsDirty},
		{id: "warmup_pools_missing", run: checkWarmupPoolsMissing},
		{id: "blob_root_missing", run: checkBlobRootMissing},
		{id: "redis_unreachable", run: checkRedisUnreachable},
	}
}

func checkNoWorkerHeartbeat(ctx context.Context, d Deps, in Input) *Finding {
	if d.DB == nil {
		return nil
	}
	var lastSeen *time.Time
	var assigned int
	err := d.DB.QueryRow(ctx, `
		SELECT (SELECT max(last_seen_at) FROM fleet_nodes WHERE role = 'worker'),
		       (SELECT count(*) FROM email_accounts WHERE worker_id IS NOT NULL)
	`).Scan(&lastSeen, &assigned)
	if err != nil || assigned == 0 {
		return nil
	}

	const tail = "Nothing is being sent or synced. Check that the worker process is running and that " +
		"ENCRYPTED_KEYS_BACKEND_URL and ENCRYPTED_KEYS_WORKER_TOKEN are set: an empty value makes the worker " +
		"start normally and never register."

	if lastSeen == nil {
		return result(CategoryWorkers, SeverityError, "No worker has checked in",
			fmt.Sprintf("No worker has ever checked in, and %d mailboxes are assigned to workers. %s", assigned, tail),
			docsHealthWorker)
	}
	age := time.Since(*lastSeen)
	if age < workerLivenessWindow {
		return nil
	}
	return result(CategoryWorkers, SeverityError, "No worker has checked in",
		fmt.Sprintf("No worker has checked in for %s, and %d mailboxes are assigned to workers. %s",
			humanizeDuration(age), assigned, tail),
		docsHealthWorker)
}

func checkCodecNotJSON(ctx context.Context, d Deps, in Input) *Finding {
	codec := config.CodecProvider()
	if codec == "json" || d.DB == nil {
		return nil
	}
	var workers int
	if err := d.DB.QueryRow(ctx, `SELECT count(*) FROM fleet_nodes WHERE role = 'worker'`).Scan(&workers); err != nil || workers == 0 {
		return nil
	}
	return result(CategoryWorkers, SeverityError, "Codec is not JSON",
		fmt.Sprintf("CODEC_PROVIDER is %s. Worker command and result envelopes carry untyped bodies that Avro "+
			"cannot serialize, so every worker command will fail to encode. Set CODEC_PROVIDER=json.", codec),
		docsEventBus)
}

func checkMigrationsDirty(ctx context.Context, d Deps, in Input) *Finding {
	if d.DB == nil {
		return nil
	}
	var version int64
	var dirty bool
	if err := d.DB.QueryRow(ctx, `SELECT version, dirty FROM schema_migrations LIMIT 1`).Scan(&version, &dirty); err != nil {
		return nil
	}
	if !dirty {
		return nil
	}
	return result(CategoryData, SeverityError, "The database schema is dirty",
		fmt.Sprintf("The database schema is dirty at version %d. The backend applies migrations at boot; "+
			"a dirty row means one failed halfway and must be resolved before this instance is used.", version),
		docsHealthDB)
}

// checkWarmupPoolsMissing: without both pools every warmup tick ends on
// "warmup pool not found" and nothing else says why.
// CountSeededWarmupPools reports how many of the two pools migration 000156
// seeds are present; the backend asserts on it once at boot as well.
func CountSeededWarmupPools(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	var n int
	err := pool.QueryRow(ctx, `SELECT count(*) FROM warmup_pools WHERE id IN ($1, $2)`,
		models.WarmupPoolFreeID, models.WarmupPoolPremiumID).Scan(&n)
	return n, err
}

func checkWarmupPoolsMissing(ctx context.Context, d Deps, in Input) *Finding {
	if d.DB == nil {
		return nil
	}
	pools, err := CountSeededWarmupPools(ctx, d.DB)
	if err != nil || pools == 2 {
		return nil
	}
	return result(CategoryData, SeverityError, "Warmup pools are missing",
		fmt.Sprintf("Only %d of the 2 warmup pools exist, so warmup cannot place any mailbox. "+
			"Migration 000156 created them and will not run again; restore the two rows under their fixed ids (the docs page has the statement).", pools),
		docsHealthDB)
}

func checkBlobRootMissing(ctx context.Context, d Deps, in Input) *Finding {
	if config.BlobProvider() != "filesystem" {
		return nil
	}
	root := env("BLOB_FS_ROOT")
	if root == "" {
		return result(CategoryData, SeverityError, "Blob storage root is unusable",
			"BLOB_PROVIDER is filesystem but BLOB_FS_ROOT is not set. Email bodies, attachments and avatars cannot be stored. "+
				"The backend, the consumer and every worker on this host must share this path.",
			docsStorage)
	}

	problem := ""
	info, err := os.Stat(root)
	switch {
	case err != nil || !info.IsDir():
		problem = "does not exist"
	default:
		probe, cerr := os.CreateTemp(root, ".warmbly-health-*")
		if cerr != nil {
			problem = "is not writable"
		} else {
			name := probe.Name()
			_ = probe.Close()
			_ = os.Remove(name)
		}
	}
	if problem == "" {
		return nil
	}

	return result(CategoryData, SeverityError, "Blob storage root is unusable",
		fmt.Sprintf("BLOB_FS_ROOT (%s) %s. Email bodies, attachments and avatars cannot be stored. "+
			"The backend, the consumer and every worker on this host must share this path.",
			filepath.Clean(root), problem),
		docsStorage)
}

func checkRedisUnreachable(ctx context.Context, d Deps, in Input) *Finding {
	if d.Cache == nil {
		return nil
	}
	if err := d.Cache.Ping(ctx).Err(); err == nil {
		return nil
	}
	return result(CategoryData, SeverityError, "Redis is not reachable",
		"Redis is not reachable. Rate limits, the organization key cache and the realtime bridge are all down.",
		docsHealthRedis)
}
