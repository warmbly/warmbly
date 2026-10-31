package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// liveUnknownSend matches refreshSendHold's candidates: an unknown send that still holds its mailbox.
const liveUnknownSend = `t.send_result_state='unknown' AND t.warmup_send_retired_at IS NULL
	AND t.campaign_send_isolated_at IS NULL AND t.send_hold_lifted_at IS NULL`

// unstartedHeldSend never reached the provider: BeginOutbound refuses it once released under the same lock.
const unstartedHeldSend = `t.task_type IN ('campaign','email','placement') AND t.status='completed'
	AND t.send_result_applied_at IS NULL AND t.send_released_at IS NULL
	AND t.send_executor_nonce IS NOT NULL AND t.send_executor_worker IS NOT NULL
	AND t.send_executor_started_at IS NULL AND t.send_executor_result IS NULL
	AND t.send_reserved_at<$2
	AND NOT EXISTS(SELECT 1 FROM outbound_attempts a WHERE a.task_id=t.id)`

// liftableHeldSend has started, so it can never run again; only its mailbox hold is lifted.
const liftableHeldSend = `t.task_type IN ('email','placement','cloud_warmup')
	AND t.status IN ('completed','failed','dead_lettered')
	AND t.send_result_applied_at IS NULL AND t.send_released_at IS NULL
	AND t.send_executor_nonce IS NOT NULL
	AND (t.send_executor_started_at IS NOT NULL OR EXISTS(SELECT 1 FROM outbound_attempts a WHERE a.task_id=t.id))
	AND COALESCE(t.send_reserved_at,t.completed_at,t.updated_at)<$1
	AND (t.send_executor_started_at IS NULL OR t.send_executor_started_at<$1)`

const recoverableUnknownHold = `ea.send_recovery_hold AND ea.send_recovery_reason='unknown' AND ea.send_recovery_task_id IS NOT NULL
	AND (t.id IS NULL
	 OR COALESCE((` + liveUnknownSend + `) AND (` + unstartedHeldSend + `),false)
	 OR COALESCE((` + liveUnknownSend + `) AND (` + liftableHeldSend + `),false))`

// RecoverUnknownSendHolds frees mailboxes held by a send that can never run again or never started.
// Campaign and warmup sends that did start keep their own isolation and retirement paths.
func (r *taskRepository) RecoverUnknownSendHolds(ctx context.Context, startedBefore, unstartedBefore time.Time, limit int) (int, error) {
	rows, err := r.db.Query(ctx, `SELECT ea.id,ea.send_recovery_task_id FROM email_accounts ea
		LEFT JOIN tasks t ON t.id=ea.send_recovery_task_id AND t.email_account_id=ea.id
		WHERE `+recoverableUnknownHold+` ORDER BY ea.id LIMIT $3`, startedBefore, unstartedBefore, min(max(limit, 1), 500))
	if err != nil {
		return 0, err
	}
	type held struct{ mailbox, task uuid.UUID }
	var candidates []held
	for rows.Next() {
		var h held
		if err := rows.Scan(&h.mailbox, &h.task); err != nil {
			rows.Close()
			return 0, err
		}
		candidates = append(candidates, h)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	recovered := 0
	for _, h := range candidates {
		ok, err := r.recoverUnknownSendHold(ctx, h.mailbox, h.task, startedBefore, unstartedBefore)
		if err != nil {
			return recovered, err
		}
		if ok {
			recovered++
		}
	}
	return recovered, nil
}

func (r *taskRepository) recoverUnknownSendHold(ctx context.Context, mailbox, task uuid.UUID, startedBefore, unstartedBefore time.Time) (bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if err = lockSend(ctx, tx, task, mailbox); err != nil {
		return false, err
	}
	var missing, unstarted, liftable bool
	var worker, nonce *uuid.UUID
	err = tx.QueryRow(ctx, `SELECT t.id IS NULL,
		COALESCE(t.id IS NOT NULL AND (`+liveUnknownSend+`) AND (`+unstartedHeldSend+`),false),
		COALESCE(t.id IS NOT NULL AND (`+liveUnknownSend+`) AND (`+liftableHeldSend+`),false),
		t.send_executor_worker,t.send_executor_nonce
		FROM email_accounts ea LEFT JOIN tasks t ON t.id=ea.send_recovery_task_id AND t.email_account_id=ea.id
		WHERE ea.id=$3 AND ea.send_recovery_task_id=$4 AND `+recoverableUnknownHold+`
		FOR UPDATE OF ea`, startedBefore, unstartedBefore, mailbox, task).Scan(&missing, &unstarted, &liftable, &worker, &nonce)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	switch {
	case missing:
		// The held send is gone; recompute the hold from what is still unknown. A settled send keeps the hold its result left.
	case unstarted && worker != nil && nonce != nil:
		if err = tx.Rollback(ctx); err != nil {
			return false, err
		}
		// The ordinary release path walks the reservation back and refreshes the hold.
		return true, r.cancelOutbound(ctx, task, mailbox, *worker, *nonce, nil)
	case liftable:
		if _, err = tx.Exec(ctx, `UPDATE tasks SET send_hold_lifted_at=NOW(),updated_at=NOW() WHERE id=$1`, task); err != nil {
			return false, err
		}
		if err = resolveSendDeadLetter(ctx, tx, task); err != nil {
			return false, err
		}
	default:
		return false, nil
	}
	if err = refreshUnknownSendHold(ctx, tx, mailbox, task); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}
