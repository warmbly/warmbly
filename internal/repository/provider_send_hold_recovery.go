package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// providerRestoredHold is a Gmail or Outlook refusal of the whole mailbox that a complete sync pass on the same grant has since outlived; with the refused task gone, only a sync in the last hour counts.
// A permanent refusal is a known failed outcome, so releasing it can cost one more refused send, never a duplicate.
const providerRestoredHold = `ea.status='active' AND ea.send_recovery_hold AND ea.send_recovery_reason='permanent'
	AND ea.provider IN('gmail','outlook') AND (ea.send_cooldown_provider IS NULL OR ea.send_cooldown_provider=ea.provider::text)
	AND ((t.id IS NOT NULL AND ss.last_synced_at>COALESCE(t.send_result_applied_at,t.completed_at,t.updated_at))
	 OR (t.id IS NULL AND ss.last_synced_at>NOW()-interval '1 hour'))
	AND NOT EXISTS(SELECT 1 FROM send_recovery_resolutions r WHERE r.email_account_id=ea.id
	 AND r.evidence_type='provider_access_restored' AND r.created_at>$1)`

// ReleaseProviderRestoredHolds lifts permanent holds on API mailboxes whose provider is serving them again, at most once per mailbox per recheck interval.
func (r *taskRepository) ReleaseProviderRestoredHolds(ctx context.Context, releasedBefore time.Time, limit int) (int, error) {
	rows, err := r.db.Query(ctx, `SELECT ea.id FROM email_accounts ea
		JOIN email_sync_state ss ON ss.email_id=ea.id
		LEFT JOIN tasks t ON t.id=ea.send_recovery_task_id AND t.email_account_id=ea.id
		WHERE `+providerRestoredHold+` ORDER BY ea.id LIMIT $2`, releasedBefore, min(max(limit, 1), 500))
	if err != nil {
		return 0, err
	}
	var candidates []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		candidates = append(candidates, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	released := 0
	var errs []error
	for _, mailbox := range candidates {
		if ctx.Err() != nil {
			break
		}
		// One mailbox that cannot be released must not stop the ones after it.
		ok, err := r.releaseProviderRestoredHold(ctx, mailbox, releasedBefore)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if ok {
			released++
		}
	}
	return released, errors.Join(append(errs, ctx.Err())...)
}

func (r *taskRepository) releaseProviderRestoredHold(ctx context.Context, mailbox uuid.UUID, releasedBefore time.Time) (bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,269))`, mailbox.String()); err != nil {
		return false, err
	}
	tag, err := tx.Exec(ctx, `WITH held AS(SELECT ea.organization_id,ea.id,t.id AS send_recovery_task_id FROM email_accounts ea
		JOIN email_sync_state ss ON ss.email_id=ea.id
		LEFT JOIN tasks t ON t.id=ea.send_recovery_task_id AND t.email_account_id=ea.id
		WHERE ea.id=$2 AND `+providerRestoredHold+` FOR UPDATE OF ea),
		history AS(INSERT INTO send_recovery_resolutions(organization_id,email_account_id,recovery_task_id,previous_reason,evidence_type,recorded_locally_at)
		SELECT organization_id,id,send_recovery_task_id,'permanent','provider_access_restored',clock_timestamp() FROM held RETURNING email_account_id)
		UPDATE email_accounts ea SET send_recovery_hold=false,send_recovery_reason=NULL,send_recovery_task_id=NULL
		FROM history WHERE ea.id=history.email_account_id`, releasedBefore, mailbox)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() != 1 {
		return false, nil
	}
	// An unknown send or unresolved conflict still owed holds the mailbox again.
	if err := refreshSendHold(ctx, tx, mailbox, nil, nil); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}
