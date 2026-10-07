package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/models"
)

type WarmupDispatchState struct {
	OrganizationID *uuid.UUID              `json:"organization_id,omitempty"`
	Recipients     []string                `json:"recipients,omitempty"`
	State          string                  `json:"state"`
	Result         *models.SendEmailResult `json:"result,omitempty"`
}

type WarmupDispatchRepository interface {
	AuthorizeWarmupDispatch(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (uuid.UUID, error)
	InspectWarmupDispatch(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*WarmupDispatchState, error)
	BeginWarmupDispatch(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID) (*WarmupDispatchState, error)
	FinishWarmupDispatch(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, models.SendEmailResult) error
	DeferWarmupDispatch(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, time.Time) error
}

func (r *workerRepository) SupportsWarmupSendProtocol(ctx context.Context, worker uuid.UUID) (bool, error) {
	var supported bool
	err := r.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM fleet_nodes WHERE id=$1 AND role='worker' AND active AND last_seen_at>NOW()-$2::interval AND warmup_send_protocol>=1)`, worker, WorkerLivenessWindow).Scan(&supported)
	return supported, err
}

func (r *taskRepository) AuthorizeWarmupDispatch(ctx context.Context, task, mailbox, worker uuid.UUID) (uuid.UUID, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,268))`, task.String()); err != nil {
		return uuid.Nil, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,269))`, mailbox.String()); err != nil {
		return uuid.Nil, err
	}
	nonce := uuid.New()
	var saved uuid.UUID
	err = tx.QueryRow(ctx, `UPDATE warmup_tasks w SET dispatch_nonce=$4,dispatch_worker_id=$3
        FROM tasks t JOIN email_accounts ea ON ea.id=t.email_account_id JOIN fleet_nodes n ON n.id=ea.worker_id
        WHERE w.task_id=t.id AND t.id=$1 AND t.email_account_id=$2 AND ea.worker_id=$3 AND t.status='active'
        AND w.lineage_version=1 AND w.dispatch_nonce IS NULL AND ea.status='active'
        AND NOT ea.send_recovery_hold AND (ea.send_cooldown_until IS NULL OR ea.send_cooldown_until<=NOW())
        AND n.role='worker' AND n.active AND n.last_seen_at>NOW()-$5::interval AND n.warmup_send_protocol>=1
		RETURNING w.dispatch_nonce`, task, mailbox, worker, nonce, WorkerLivenessWindow).Scan(&saved)
	if err != nil {
		return uuid.Nil, err
	}
	if _, err = tx.Exec(ctx, `UPDATE tasks SET status='completed',completed_at=NOW(),updated_at=NOW(),send_result_state='unknown' WHERE id=$1`, task); err != nil {
		return uuid.Nil, err
	}
	if _, err = tx.Exec(ctx, `UPDATE email_accounts SET send_recovery_hold=true,send_recovery_reason='unknown',send_recovery_task_id=$2 WHERE id=$1`, mailbox, task); err != nil {
		return uuid.Nil, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO warmup_statistics(email_account_id,date,emails_sent,emails_replied,target_volume)
        SELECT $2,CURRENT_DATE,1,CASE WHEN parent_task_id IS NULL THEN 0 ELSE 1 END,0 FROM warmup_tasks WHERE task_id=$1
        ON CONFLICT(email_account_id,date) DO UPDATE SET emails_sent=warmup_statistics.emails_sent+1,emails_replied=warmup_statistics.emails_replied+EXCLUDED.emails_replied`, task, mailbox); err != nil {
		return uuid.Nil, err
	}
	return saved, tx.Commit(ctx)
}

func (r *taskRepository) InspectWarmupDispatch(ctx context.Context, task, mailbox, worker uuid.UUID) (*WarmupDispatchState, error) {
	var nonce *uuid.UUID
	var started *time.Time
	var result []byte
	var assigned *uuid.UUID
	err := r.db.QueryRow(ctx, `SELECT w.dispatch_nonce,w.dispatch_started_at,w.dispatch_result,w.dispatch_worker_id
        FROM warmup_tasks w JOIN tasks t ON t.id=w.task_id WHERE t.id=$1 AND t.email_account_id=$2`, task, mailbox).Scan(&nonce, &started, &result, &assigned)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && nonce == nil {
		return &WarmupDispatchState{State: "legacy"}, nil
	}
	if err != nil {
		return nil, err
	}
	if assigned == nil || *assigned != worker {
		return &WarmupDispatchState{State: "denied"}, nil
	}
	out := &WarmupDispatchState{State: "authorized"}
	if started != nil {
		out.State = "started"
	}
	if len(result) > 0 {
		out.State = "finished"
		out.Result = &models.SendEmailResult{}
		if err := json.Unmarshal(result, out.Result); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (r *taskRepository) BeginWarmupDispatch(ctx context.Context, task, mailbox, worker, nonce uuid.UUID) (*WarmupDispatchState, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,268))`, task.String()); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,269))`, mailbox.String()); err != nil {
		return nil, err
	}
	tag, err := tx.Exec(ctx, `UPDATE warmup_tasks w SET dispatch_started_at=NOW()
        FROM tasks t JOIN email_accounts ea ON ea.id=t.email_account_id JOIN fleet_nodes n ON n.id=ea.worker_id
        WHERE w.task_id=t.id AND t.id=$1 AND ea.id=$2 AND w.dispatch_worker_id=$3 AND ea.worker_id=$3
        AND w.dispatch_nonce=$4 AND w.dispatch_started_at IS NULL AND w.dispatch_result IS NULL
        AND t.status='completed' AND t.send_result_applied_at IS NULL AND t.send_result_state='unknown'
        AND n.active AND n.warmup_send_protocol>=1 AND ea.status='active'
        AND ea.send_recovery_hold AND ea.send_recovery_reason='unknown' AND ea.send_recovery_task_id=t.id AND (ea.send_cooldown_until IS NULL OR ea.send_cooldown_until<=NOW())
        AND (ea.warmup IS NOT NULL AND ea.warmup_paused_at IS NULL OR EXISTS(SELECT 1 FROM campaigns c WHERE c.organization_id=ea.organization_id AND c.status='active' AND (
            EXISTS(SELECT 1 FROM campaign_email_tags ct JOIN email_tags et ON et.tag_id=ct.tag_id WHERE ct.campaign_id=c.id AND et.email_id=ea.id)
            OR EXISTS(SELECT 1 FROM campaign_senders cs WHERE cs.campaign_id=c.id AND cs.email_account_id=ea.id AND cs.enabled)
            OR (NOT EXISTS(SELECT 1 FROM campaign_email_tags ct WHERE ct.campaign_id=c.id) AND NOT EXISTS(SELECT 1 FROM campaign_senders cs WHERE cs.campaign_id=c.id AND cs.enabled)))))
        AND EXISTS(SELECT 1 FROM warmup_pool_participants wpp JOIN email_accounts ea ON ea.id=wpp.email_account_id WHERE ea.id=$2 AND wpp.participant_role='sender_receiver' AND `+testSenderSQL+` AND `+poolEligibleSQL+`)
        AND EXISTS(SELECT 1 FROM warmup_tokens wt JOIN email_accounts ea ON ea.id=wt.recipient_account_id JOIN warmup_pool_participants wpp ON wpp.email_account_id=ea.id WHERE wt.task_id=t.id AND `+partnerEligibleSQL+`)`, task, mailbox, worker, nonce)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		state, err := r.InspectWarmupDispatch(ctx, task, mailbox, worker)
		if err != nil {
			return nil, err
		}
		if state.State == "authorized" {
			if err := r.DeferWarmupDispatch(ctx, task, mailbox, worker, nonce, time.Now().Add(5*time.Minute)); err != nil {
				return nil, err
			}
			return &WarmupDispatchState{State: "deferred"}, nil
		}
		return state, nil
	}
	if _, err = tx.Exec(ctx, `UPDATE tasks SET send_result_state='unknown' WHERE id=$1`, task); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `UPDATE email_accounts SET send_recovery_hold=true,send_recovery_reason='unknown',send_recovery_task_id=$2 WHERE id=$1`, mailbox, task); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &WarmupDispatchState{State: "execute"}, nil
}

func (r *taskRepository) DeferWarmupDispatch(ctx context.Context, task, mailbox, worker, nonce uuid.UUID, at time.Time) error {
	tx, err := r.warmupSchedulingTx(ctx, task)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,268))`, task.String()); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,269))`, mailbox.String()); err != nil {
		return err
	}
	var reply bool
	var completed time.Time
	err = tx.QueryRow(ctx, `UPDATE warmup_tasks w SET dispatch_nonce=NULL,dispatch_worker_id=NULL,schedule_revision=schedule_revision+1
        FROM tasks t WHERE w.task_id=t.id AND t.id=$1 AND t.email_account_id=$2 AND w.dispatch_worker_id=$3 AND w.dispatch_nonce=$4
        AND w.dispatch_started_at IS NULL AND w.dispatch_result IS NULL AND t.send_result_applied_at IS NULL
        RETURNING w.parent_task_id IS NOT NULL,t.completed_at`, task, mailbox, worker, nonce).Scan(&reply, &completed)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE warmup_statistics SET emails_sent=GREATEST(emails_sent-1,0),emails_replied=GREATEST(emails_replied-CASE WHEN $3 THEN 1 ELSE 0 END,0) WHERE email_account_id=$1 AND date=DATE($2)`, mailbox, completed, reply); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM warmup_tokens WHERE task_id=$1`, task); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE tasks SET status='pending',completed_at=NULL,scheduled_at=$2,updated_at=NOW(),send_result_state=NULL,message_id='' WHERE id=$1`, task, at); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE email_accounts SET send_recovery_hold=false,send_recovery_reason=NULL,send_recovery_task_id=NULL
        WHERE id=$1 AND send_recovery_reason='unknown' AND send_recovery_task_id=$2 AND NOT EXISTS(SELECT 1 FROM tasks WHERE email_account_id=$1 AND send_result_state='unknown')`, mailbox, task); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *taskRepository) FinishWarmupDispatch(ctx context.Context, task, mailbox, worker uuid.UUID, result models.SendEmailResult) error {
	if result.TaskID != task {
		return errors.New("dispatch task mismatch")
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	tag, err := r.db.Exec(ctx, `UPDATE warmup_tasks w SET dispatch_result=$4 FROM tasks t WHERE w.task_id=t.id AND t.id=$1 AND t.email_account_id=$2 AND w.dispatch_worker_id=$3 AND w.dispatch_started_at IS NOT NULL AND w.dispatch_result IS NULL`, task, mailbox, worker, raw)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	state, err := r.InspectWarmupDispatch(ctx, task, mailbox, worker)
	if err != nil {
		return err
	}
	if state.State != "finished" {
		return errors.New("dispatch was not started")
	}
	previous, err := json.Marshal(state.Result)
	if err != nil {
		return err
	}
	if !bytes.Equal(previous, raw) {
		return errors.New("conflicting dispatch result")
	}
	return nil
}
