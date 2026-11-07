package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ListNoAccountsPaused lists campaigns parked for having no usable mailbox, longest parked first.
func (r *campaignRepository) ListNoAccountsPaused(ctx context.Context, limit int) ([]uuid.UUID, error) {
	rows, err := resultDB(ctx, r.DB).Query(ctx, `SELECT id FROM campaigns WHERE status='paused_no_accounts'
		ORDER BY last_status_change_at ASC NULLS FIRST, id LIMIT $1`, min(max(limit, 1), 500))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// ResumeFromNoAccounts reactivates a campaign only while it is still parked for having no mailbox, so an owner's pause in between stands.
func (r *campaignRepository) ResumeFromNoAccounts(ctx context.Context, campaignID uuid.UUID) (bool, error) {
	tx, err := beginResultTx(ctx, r.DB)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('campaign_status_' || $1::text))`, campaignID); err != nil {
		return false, err
	}
	tag, err := tx.Exec(ctx, `UPDATE campaigns SET status='active',idle_since=NULL,last_status_change_at=NOW(),updated_at=NOW()
		WHERE id=$1 AND status='paused_no_accounts'`, campaignID)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() != 1 {
		return false, nil
	}
	return true, tx.Commit(ctx)
}
