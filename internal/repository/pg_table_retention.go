package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// PruneBatch is how many rows one delete statement removes.
	PruneBatch = 5000
	// prunePause lets autovacuum, replication and live writes in between batches.
	prunePause = 50 * time.Millisecond
)

// DeleteInBatches runs a ctid-selecting delete until it removes fewer than a
// full batch. selectCtids is `SELECT ctid FROM t WHERE ... LIMIT $1` with the
// batch size as $1 and its own arguments after it.
func DeleteInBatches(ctx context.Context, db *pgxpool.Pool, table, selectCtids string, args ...any) (int64, error) {
	query := `DELETE FROM ` + table + ` WHERE ctid = ANY(ARRAY(` + selectCtids + `))`
	var total int64
	for {
		tag, err := db.Exec(ctx, query, append([]any{PruneBatch}, args...)...)
		if err != nil {
			return total, err
		}
		total += tag.RowsAffected()
		if tag.RowsAffected() < PruneBatch {
			return total, nil
		}
		select {
		case <-ctx.Done():
			return total, ctx.Err()
		case <-time.After(prunePause):
		}
	}
}

// TableRetentionRepository prunes history that has a retention window and no
// owner of its own: feeds, logs and finished bookkeeping rows.
type TableRetentionRepository struct {
	db *pgxpool.Pool
}

// NewTableRetentionRepository builds the repository.
func NewTableRetentionRepository(db *pgxpool.Pool) *TableRetentionRepository {
	return &TableRetentionRepository{db: db}
}

// PruneCampaignLogs drops campaign activity-feed entries older than before.
func (r *TableRetentionRepository) PruneCampaignLogs(ctx context.Context, before time.Time) (int64, error) {
	return DeleteInBatches(ctx, r.db, "campaign_logs",
		`SELECT ctid FROM campaign_logs WHERE created_at < $2 LIMIT $1`, before)
}

// PruneNotifications drops notifications older than before, except one still owed an email.
func (r *TableRetentionRepository) PruneNotifications(ctx context.Context, before time.Time) (int64, error) {
	return DeleteInBatches(ctx, r.db, "notifications",
		`SELECT ctid FROM notifications WHERE created_at < $2
		   AND email_state NOT IN ('pending', 'sending') LIMIT $1`, before)
}

// PruneWebhookDeliveries drops settled deliveries last touched before before; one still retrying stays.
func (r *TableRetentionRepository) PruneWebhookDeliveries(ctx context.Context, before time.Time) (int64, error) {
	return DeleteInBatches(ctx, r.db, "webhook_deliveries",
		`SELECT ctid FROM webhook_deliveries WHERE status IN ('delivered', 'failed', 'abandoned')
		   AND updated_at < $2 LIMIT $1`, before)
}

// PruneWorkerHealthSamples drops worker health samples older than before.
func (r *TableRetentionRepository) PruneWorkerHealthSamples(ctx context.Context, before time.Time) (int64, error) {
	return DeleteInBatches(ctx, r.db, "worker_health_samples",
		`SELECT ctid FROM worker_health_samples WHERE observed_at < $2 LIMIT $1`, before)
}

// PruneDeliveredSendResultEffects drops send-result effects delivered before before. An effect whose
// task could still apply a result again (not applied, or dead-lettered and repairable) stays, so it is never re-sent.
func (r *TableRetentionRepository) PruneDeliveredSendResultEffects(ctx context.Context, before time.Time) (int64, error) {
	return DeleteInBatches(ctx, r.db, "send_result_effects",
		`SELECT e.ctid FROM send_result_effects e WHERE e.delivered_at IS NOT NULL AND e.delivered_at < $2
		   AND NOT EXISTS (SELECT 1 FROM tasks t WHERE t.id = e.task_id
		                   AND (t.send_result_applied_at IS NULL OR t.status = 'dead_lettered'))
		 LIMIT $1`, before)
}
