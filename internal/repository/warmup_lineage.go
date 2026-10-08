package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/pkg/mailhdr"
)

// WarmupLineageRepository keeps receipt authority separate from pair selection.
type WarmupLineageRepository interface {
	RecordVerifiedWarmupParent(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string, WarmupReceiptProof) error
	BindWarmupSuccessor(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, time.Time) (bool, error)
	ExactWarmupParent(context.Context, uuid.UUID) (*WarmupReplyCandidate, error)
	WarmupExecutionContext(context.Context, uuid.UUID) (uuid.UUID, *WarmupReplyCandidate, error)
	SaveWarmupLineage(context.Context, uuid.UUID, WarmupLineage) error
	ClaimWarmupTask(context.Context, uuid.UUID, time.Time) (bool, error)
	ReleaseUnpublishedWarmupTask(context.Context, uuid.UUID, time.Time) error
	RescheduleWarmupTask(context.Context, uuid.UUID, time.Time) error
	UnqueuedWarmupTasks(context.Context, int) ([]WarmupQueueRevision, error)
	AckWarmupQueue(context.Context, WarmupQueueRevision, string) (bool, error)
}

type WarmupLineage struct {
	Subject          string
	References       []string
	ScenarioVersion  string
	RenderingVersion string
	MaxTurns         int
}

type WarmupQueueRevision struct {
	TaskID         uuid.UUID
	At             time.Time
	Revision       int64
	PreviousHandle *string
}

type WarmupReceiptProof struct {
	Token         uuid.UUID
	SenderAddress string
	MessageID     string
}

func (r *taskRepository) RecordVerifiedWarmupParent(ctx context.Context, parent, recipient, internalID uuid.UUID, threadID string, proof WarmupReceiptProof) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('warmup_parent_' || $1::text))`, parent); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE warmup_received wr SET task_id=$1, provider_thread_id=NULLIF($4,'')
        WHERE wr.email_account_id=$2 AND wr.internal_id=$3 AND wr.task_id IS NULL
        AND wr.message_id<>''
        AND NOT EXISTS(SELECT 1 FROM warmup_received prior WHERE prior.task_id=$1)
        AND EXISTS (SELECT 1 FROM warmup_tokens wt JOIN warmup_tasks original ON original.task_id=wt.task_id
                    JOIN tasks sent ON sent.id=wt.task_id JOIN email_accounts sender ON sender.id=wt.sender_account_id WHERE wt.task_id=$1
                    AND original.lineage_version=1 AND wt.recipient_account_id=$2 AND wt.sender_account_id=wr.sender_account_id
                    AND sent.status='completed' AND sent.email_account_id=wt.sender_account_id
                    AND wt.expires_at>NOW() AND wt.consumed_at IS NULL AND wt.sent_retired_at IS NULL
                    AND lower(sender.email)=lower($6) AND wt.sent_message_id<>''
                    AND (btrim(wt.sent_message_id,'<>')=btrim(wr.message_id,'<>') OR wt.token=$5))`,
		parent, recipient, internalID, threadID, proof.Token, strings.TrimSpace(proof.SenderAddress))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var same bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM warmup_received WHERE email_account_id=$2 AND internal_id=$3 AND task_id=$1)`, parent, recipient, internalID).Scan(&same)
		if err != nil {
			return err
		}
		if !same {
			var legacy bool
			if err := tx.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM warmup_tasks WHERE task_id=$1 AND lineage_version=1)`, parent).Scan(&legacy); err != nil {
				return err
			}
			if legacy {
				return nil
			}
			return errors.New("verified warmup receipt context unavailable")
		}
	}
	if err := attachDiagnosticAuth(ctx, tx, parent, recipient); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *taskRepository) BindWarmupSuccessor(ctx context.Context, parent, recipient, internalID uuid.UUID, at time.Time) (bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('warmup_task_' || $1::text))`, recipient); err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('warmup_parent_' || $1::text))`, parent); err != nil {
		return false, err
	}
	var id uuid.UUID
	err = tx.QueryRow(ctx, `SELECT t.id FROM tasks t JOIN warmup_tasks w ON w.task_id=t.id
        WHERE t.email_account_id=$1 AND t.task_type='warmup' AND t.status='pending'
        AND w.target_account_id IS NULL AND w.parent_task_id IS NULL
        ORDER BY t.scheduled_at NULLS FIRST LIMIT 1 FOR UPDATE OF t`, recipient).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		id = uuid.New()
		if _, err = tx.Exec(ctx, `INSERT INTO tasks(id,task_type,email_account_id,status,scheduled_at,message_id) VALUES($1,'warmup',$2,'pending',$3,'')`, id, recipient, at); err != nil {
			return false, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO warmup_tasks(task_id) VALUES($1)`, id); err != nil {
			return false, err
		}
		err = nil
	}
	if err != nil {
		return false, err
	}
	tag, err := tx.Exec(ctx, `UPDATE warmup_tasks successor
        SET target_account_id=wt.sender_account_id, parent_task_id=$2, parent_received_id=$3,
            parent_message_id=wr.message_id, schedule_revision=successor.schedule_revision+1
        FROM warmup_tokens wt JOIN warmup_tasks original ON original.task_id=wt.task_id
        JOIN warmup_received wr ON wr.task_id=wt.task_id AND wr.email_account_id=wt.recipient_account_id
        JOIN email_accounts ea ON ea.id=wt.recipient_account_id
        WHERE successor.task_id=$1 AND wt.task_id=$2 AND wr.internal_id=$3 AND wt.recipient_account_id=$4
        AND ea.status='active' AND ea.warmup IS NOT NULL AND ea.warmup_paused_at IS NULL
        AND wr.retired_at IS NULL AND wr.created_at>NOW()-INTERVAL '7 days'
        AND original.lineage_version=1 AND original.subject<>'' AND wt.conversation_id IS NOT NULL
        AND wt.expires_at>NOW() AND wt.sent_retired_at IS NULL
        AND wt.conversation_turn+1<original.max_turns AND wr.message_id<>''
        AND wt.sent_message_id<>'' AND wr.sender_account_id=wt.sender_account_id
        AND NOT EXISTS(SELECT 1 FROM warmup_tasks prior WHERE prior.parent_task_id=$2)`, id, parent, internalID, recipient)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	if _, err = tx.Exec(ctx, `UPDATE tasks SET scheduled_at=GREATEST($2,(SELECT created_at+INTERVAL '45 minutes' FROM warmup_received WHERE email_account_id=$3 AND internal_id=$4)),updated_at=NOW() WHERE id=$1`, id, at, recipient, internalID); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func (r *taskRepository) ExactWarmupParent(ctx context.Context, successor uuid.UUID) (*WarmupReplyCandidate, error) {
	c := &WarmupReplyCandidate{}
	err := r.db.QueryRow(ctx, `SELECT wr.message_id, original.subject, wr.provider_thread_id,
        COALESCE(wt.conversation_theme,''),COALESCE(wt.content_source,''),wt.conversation_id,wt.conversation_turn,
        COALESCE(original.reference_ids,'{}'::text[]),original.scenario_version,original.rendering_version,original.max_turns
        FROM warmup_tasks reply JOIN tasks t ON t.id=reply.task_id
        JOIN warmup_tokens wt ON wt.task_id=reply.parent_task_id AND wt.recipient_account_id=t.email_account_id AND wt.sender_account_id=reply.target_account_id
        JOIN warmup_tasks original ON original.task_id=wt.task_id
        JOIN warmup_received wr ON wr.task_id=wt.task_id AND wr.internal_id=reply.parent_received_id AND wr.email_account_id=t.email_account_id
        WHERE reply.task_id=$1 AND wr.message_id=reply.parent_message_id AND wr.retired_at IS NULL
        AND wr.created_at>NOW()-INTERVAL '7 days' AND original.lineage_version=1
        AND wt.expires_at>NOW() AND wt.sent_retired_at IS NULL
        AND wt.conversation_turn+1<original.max_turns
        AND wt.sent_message_id<>'' AND wr.sender_account_id=wt.sender_account_id`, successor).Scan(&c.MessageID, &c.Subject, &c.ThreadID, &c.ConversationTheme, &c.ContentSource, &c.ConversationID, &c.ConversationTurn, &c.References, &c.ScenarioVersion, &c.RenderingVersion, &c.MaxTurns)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return c, err
}

func (r *taskRepository) WarmupExecutionContext(ctx context.Context, task uuid.UUID) (uuid.UUID, *WarmupReplyCandidate, error) {
	var recipient uuid.UUID
	c := &WarmupReplyCandidate{}
	err := r.db.QueryRow(ctx, `SELECT wt.recipient_account_id,w.subject,wt.content_source,wt.conversation_id,wt.conversation_turn,w.scenario_version,w.rendering_version,w.max_turns
        FROM warmup_tasks w JOIN warmup_tokens wt ON wt.task_id=w.task_id JOIN tasks t ON t.id=w.task_id
        JOIN email_accounts sender ON sender.id=t.email_account_id JOIN email_accounts receiver ON receiver.id=wt.recipient_account_id
        WHERE w.task_id=$1 AND wt.sender_account_id=t.email_account_id AND w.lineage_version=1
        AND wt.expires_at>NOW() AND wt.sent_retired_at IS NULL AND wt.conversation_id IS NOT NULL
        AND NOT recipient_suppressed(sender.organization_id,receiver.email)`, task).Scan(&recipient, &c.Subject, &c.ContentSource, &c.ConversationID, &c.ConversationTurn, &c.ScenarioVersion, &c.RenderingVersion, &c.MaxTurns)
	return recipient, c, err
}

func (r *taskRepository) SaveWarmupLineage(ctx context.Context, taskID uuid.UUID, l WarmupLineage) error {
	if l.Subject == "" || l.ScenarioVersion == "" || l.RenderingVersion == "" || l.MaxTurns < 1 || l.MaxTurns > 6 {
		return errors.New("invalid diagnostic lineage")
	}
	if _, err := mailhdr.References(l.References); err != nil {
		return err
	}
	tag, err := r.db.Exec(ctx, `UPDATE warmup_tasks SET lineage_version=1,subject=$2,reference_ids=$3,scenario_version=$4,rendering_version=$5,max_turns=$6 WHERE task_id=$1 AND dispatch_nonce IS NULL`, taskID, l.Subject, l.References, l.ScenarioVersion, l.RenderingVersion, l.MaxTurns)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errors.New("diagnostic lineage unavailable")
	}
	return nil
}

func (r *taskRepository) ClaimWarmupTask(ctx context.Context, id uuid.UUID, now time.Time) (bool, error) {
	tx, err := r.warmupSchedulingTx(ctx, id)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE tasks t SET status='active',updated_at=NOW() WHERE id=$1 AND task_type='warmup' AND status='pending' AND (scheduled_at IS NULL OR scheduled_at<=$2)
    AND NOT EXISTS(SELECT 1 FROM tasks other WHERE other.email_account_id=t.email_account_id AND other.task_type='warmup' AND other.status='active')`, id, now)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, tx.Commit(ctx)
}

func (r *taskRepository) warmupSchedulingTx(ctx context.Context, id uuid.UUID) (pgx.Tx, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	var account uuid.UUID
	if err = tx.QueryRow(ctx, `SELECT email_account_id FROM tasks WHERE id=$1`, id).Scan(&account); err == nil {
		_, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('warmup_task_' || $1::text))`, account)
	}
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	return tx, nil
}

func (r *taskRepository) ReleaseUnpublishedWarmupTask(ctx context.Context, id uuid.UUID, at time.Time) error {
	tx, err := r.warmupSchedulingTx(ctx, id)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `WITH released AS(UPDATE tasks t SET status='pending',scheduled_at=$2,updated_at=NOW() FROM warmup_tasks w
        WHERE t.id=$1 AND w.task_id=t.id AND t.status='active' AND w.dispatch_nonce IS NULL RETURNING t.id)
        UPDATE warmup_tasks SET schedule_revision=schedule_revision+1 WHERE task_id IN(SELECT id FROM released)`, id, at)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *taskRepository) RescheduleWarmupTask(ctx context.Context, id uuid.UUID, at time.Time) error {
	tx, err := r.warmupSchedulingTx(ctx, id)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `WITH moved AS (UPDATE tasks SET scheduled_at=$2,updated_at=NOW() WHERE id=$1 AND status='pending' RETURNING id)
        UPDATE warmup_tasks SET schedule_revision=schedule_revision+1 WHERE task_id IN (SELECT id FROM moved)`, id, at)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *taskRepository) UnqueuedWarmupTasks(ctx context.Context, limit int) ([]WarmupQueueRevision, error) {
	rows, err := r.db.Query(ctx, `SELECT t.id,t.scheduled_at,w.schedule_revision,t.cloud_task_name FROM tasks t JOIN warmup_tasks w ON w.task_id=t.id WHERE t.status='pending' AND t.scheduled_at IS NOT NULL AND (w.schedule_revision>w.queued_revision OR t.scheduled_at<NOW()-INTERVAL '10 minutes' AND t.updated_at<NOW()-INTERVAL '10 minutes') ORDER BY t.scheduled_at LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WarmupQueueRevision{}
	for rows.Next() {
		var v WarmupQueueRevision
		if err := rows.Scan(&v.TaskID, &v.At, &v.Revision, &v.PreviousHandle); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *taskRepository) AckWarmupQueue(ctx context.Context, v WarmupQueueRevision, handle string) (bool, error) {
	tx, err := r.warmupSchedulingTx(ctx, v.TaskID)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `WITH queued AS (UPDATE warmup_tasks w SET queued_revision=$2 FROM tasks t WHERE w.task_id=$1 AND t.id=w.task_id AND t.status='pending' AND t.scheduled_at=$3 AND w.schedule_revision=$2 RETURNING w.task_id)
        UPDATE tasks SET cloud_task_name=$4,updated_at=NOW() WHERE id IN(SELECT task_id FROM queued)`, v.TaskID, v.Revision, v.At, handle)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, tx.Commit(ctx)
}

func (r *taskRepository) CountWarmupDispatchReservations(ctx context.Context, id uuid.UUID, start, end time.Time) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM tasks WHERE email_account_id=$1 AND task_type='warmup' AND status IN('active','completed') AND COALESCE(completed_at,updated_at)>=$2 AND COALESCE(completed_at,updated_at)<$3`, id, start, end).Scan(&n)
	return n, err
}

func (r *taskRepository) WarmupDispatchUsage(ctx context.Context, id, excluded uuid.UUID, start, end, hourStart, hourEnd time.Time) (int, int, int, *time.Time, error) {
	var warmup, daily, hourly int
	var last *time.Time
	err := r.db.QueryRow(ctx, `SELECT
        COUNT(*) FILTER(WHERE task_type='warmup' AND COALESCE(completed_at,updated_at)>=$3 AND COALESCE(completed_at,updated_at)<$4),
        COUNT(*) FILTER(WHERE COALESCE(completed_at,updated_at)>=$3 AND COALESCE(completed_at,updated_at)<$4),
        COUNT(*) FILTER(WHERE COALESCE(completed_at,updated_at)>=$5 AND COALESCE(completed_at,updated_at)<$6),
        MAX(COALESCE(completed_at,updated_at))
        FROM tasks WHERE email_account_id=$1 AND id<>$2 AND status IN('active','completed') AND task_type IN('warmup','campaign','email','placement') AND message_id<>''`, id, excluded, start, end, hourStart, hourEnd).Scan(&warmup, &daily, &hourly, &last)
	return warmup, daily, hourly, last, err
}
