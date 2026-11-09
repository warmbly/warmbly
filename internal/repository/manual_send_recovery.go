package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// InterruptedManualSend is a manual send whose claim ended before it reserved anything.
type InterruptedManualSend struct {
	TaskID    uuid.UUID
	MailboxID uuid.UUID
	// Retried is true when the send went back to pending, false when it failed.
	Retried bool
}

// RecoverInterruptedManualSends settles manual sends claimed before claimedBefore
// that never reserved: nothing was handed to a worker, so one claimed at or after
// retrySince goes back to pending and anything older fails.
func (r *taskRepository) RecoverInterruptedManualSends(ctx context.Context, claimedBefore, retrySince time.Time, limit int) ([]InterruptedManualSend, error) {
	rows, err := r.db.Query(ctx, `SELECT id FROM tasks WHERE status='active' AND task_type='email'
	 AND send_executor_nonce IS NULL AND updated_at<$1 ORDER BY updated_at,id LIMIT $2`, claimedBefore, min(max(limit, 1), 500))
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, err
	}
	var out []InterruptedManualSend
	var errs []error
	for _, id := range ids {
		if ctx.Err() != nil {
			break
		}
		send, ok, err := r.recoverInterruptedManualSend(ctx, id, claimedBefore, retrySince)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if ok {
			out = append(out, send)
		}
	}
	return out, errors.Join(append(errs, ctx.Err())...)
}

func (r *taskRepository) recoverInterruptedManualSend(ctx context.Context, id uuid.UUID, claimedBefore, retrySince time.Time) (InterruptedManualSend, bool, error) {
	send := InterruptedManualSend{TaskID: id}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return send, false, err
	}
	defer tx.Rollback(ctx)
	// ReserveOutbound holds this lock until it commits, so a reservation in flight is never overtaken.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,268))`, id.String()); err != nil {
		return send, false, err
	}
	// A test send has no email_tasks row to run again, so it fails.
	err = tx.QueryRow(ctx, `UPDATE tasks t SET status=(CASE WHEN t.updated_at>=$3 AND EXISTS(SELECT 1 FROM email_tasks et WHERE et.task_id=t.id)
	 THEN 'pending' ELSE 'failed' END)::task_status,updated_at=NOW(),dispatch_retry_at=NULL,dispatch_claimed_until=NULL
	 WHERE t.id=$1 AND t.status='active' AND t.task_type='email' AND t.send_executor_nonce IS NULL AND t.updated_at<$2
	 RETURNING t.email_account_id,t.status='pending'`, id, claimedBefore, retrySince).Scan(&send.MailboxID, &send.Retried)
	if errors.Is(err, pgx.ErrNoRows) {
		return send, false, nil
	}
	if err != nil {
		return send, false, err
	}
	if !send.Retried {
		if _, err = tx.Exec(ctx, `INSERT INTO task_failures(task_id,title,message) VALUES($1,'Send interrupted','The send stopped before it was handed to a worker, so nothing was sent')
		 ON CONFLICT(task_id) DO UPDATE SET title=EXCLUDED.title,message=EXCLUDED.message`, id); err != nil {
			return send, false, err
		}
	}
	return send, true, tx.Commit(ctx)
}
