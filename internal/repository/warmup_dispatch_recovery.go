package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/models"
)

const unstartedWarmupDispatch = `t.task_type='warmup' AND t.status='completed'
	AND t.send_result_state='unknown'
	AND ((t.send_result_applied_at IS NULL AND t.send_result_evidence IS NULL AND t.send_released_at IS NULL)
	 OR (t.send_executor_nonce IS NULL AND t.send_reserved_at IS NULL AND w.dispatch_nonce IS NOT NULL
	     AND t.send_result_applied_at<t.completed_at AND t.send_released_at<t.completed_at
	     AND t.send_result_evidence->>'protocol'='internal' AND t.send_result_evidence->>'stage'='prepare'
	     AND t.send_result_evidence->>'disposition'='retry' AND t.send_result_evidence->>'scope'='mailbox'))
	AND COALESCE(t.send_reserved_at,t.completed_at)<$1
	AND (t.send_executor_nonce IS NOT NULL OR w.dispatch_nonce IS NOT NULL)
	AND t.send_executor_started_at IS NULL AND t.send_executor_result IS NULL
	AND w.dispatch_started_at IS NULL AND w.dispatch_result IS NULL
	AND NOT EXISTS(SELECT 1 FROM outbound_attempts a WHERE a.task_id=t.id)
	AND NOT EXISTS(SELECT 1 FROM warmup_tokens wt WHERE wt.task_id=t.id AND wt.sent_message_id<>'')
	AND NOT EXISTS(SELECT 1 FROM warmup_received wr WHERE wr.task_id=t.id)`

// Only nonce-gated commands that never began execution can be safely retired.
func (r *taskRepository) RecoverUnstartedWarmupDispatches(ctx context.Context, before time.Time, limit int) (int, error) {
	rows, err := r.db.Query(ctx, `SELECT t.id,t.email_account_id FROM tasks t JOIN warmup_tasks w ON w.task_id=t.id
		WHERE `+unstartedWarmupDispatch+` ORDER BY COALESCE(t.send_reserved_at,t.completed_at),t.id LIMIT $2`, before, limit)
	if err != nil {
		return 0, err
	}
	type candidate struct{ task, mailbox uuid.UUID }
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err = rows.Scan(&c.task, &c.mailbox); err != nil {
			rows.Close()
			return 0, err
		}
		candidates = append(candidates, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	recovered := 0
	for _, c := range candidates {
		ok, err := r.retireUnstartedWarmupDispatch(ctx, c.task, c.mailbox, before)
		if err != nil {
			return recovered, err
		}
		if ok {
			recovered++
		}
	}
	return recovered, nil
}

func (r *taskRepository) retireUnstartedWarmupDispatch(ctx context.Context, task, mailbox uuid.UUID, before time.Time) (bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if err = lockSend(ctx, tx, task, mailbox); err != nil {
		return false, err
	}
	var stalePreparation bool
	err = tx.QueryRow(ctx, `SELECT t.send_result_applied_at IS NOT NULL FROM tasks t JOIN warmup_tasks w ON w.task_id=t.id
		WHERE t.id=$2 AND t.email_account_id=$3 AND `+unstartedWarmupDispatch, before, task, mailbox).Scan(&stalePreparation)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	tag, err := tx.Exec(ctx, `UPDATE tasks t SET status='cancelled',updated_at=NOW(),send_result_state='failed',send_result_applied_at=NOW(),send_released_at=NOW()
		FROM warmup_tasks w WHERE w.task_id=t.id AND t.id=$2 AND t.email_account_id=$3 AND `+unstartedWarmupDispatch, before, task, mailbox)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	if _, err = tx.Exec(ctx, `UPDATE warmup_statistics SET emails_sent=GREATEST(emails_sent-1,0),
		emails_replied=GREATEST(emails_replied-CASE WHEN w.parent_task_id IS NULL THEN 0 ELSE 1 END,0)
		FROM warmup_tasks w JOIN tasks t ON t.id=w.task_id
		WHERE t.id=$2 AND warmup_statistics.email_account_id=$1 AND date=DATE(t.completed_at)`, mailbox, task); err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM warmup_tokens WHERE task_id=$1`, task); err != nil {
		return false, err
	}
	if stalePreparation {
		if _, err = tx.Exec(ctx, `UPDATE email_accounts SET send_recovery_reason='unknown'
			WHERE id=$1 AND send_recovery_task_id=$2 AND send_recovery_reason='conflict'`, mailbox, task); err != nil {
			return false, err
		}
	}
	if err = persistSendHold(ctx, tx, mailbox, models.SendEmailResult{TaskID: task}, "failed"); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}
