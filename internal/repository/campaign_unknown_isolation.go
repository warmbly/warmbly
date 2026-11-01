package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

const isolatableUnknownCampaignSend = `t.task_type='campaign' AND t.status IN ('completed','failed','dead_lettered')
	AND t.send_result_state='unknown' AND t.campaign_send_isolated_at IS NULL
	AND t.send_result_applied_at IS NULL AND t.send_released_at IS NULL
	AND ea.send_recovery_hold AND ea.send_recovery_reason='unknown' AND ea.send_recovery_task_id=t.id
	AND t.send_executor_nonce IS NOT NULL
	AND (t.send_executor_started_at IS NOT NULL OR EXISTS(SELECT 1 FROM outbound_attempts a
	 WHERE a.task_id=t.id AND a.nonce=t.send_executor_nonce AND a.email_account_id=t.email_account_id))
	AND t.send_reserved_at<$1
	AND (t.send_executor_started_at IS NULL OR t.send_executor_started_at<$1)
	AND NOT EXISTS(SELECT 1 FROM outbound_attempts a WHERE a.task_id=t.id AND a.attempted_at >= $1)
	AND (ct.campaign_id IS NULL OR EXISTS(SELECT 1 FROM campaigns c WHERE c.id=ct.campaign_id AND c.organization_id=ea.organization_id))
	AND (ct.contact_id IS NULL OR EXISTS(SELECT 1 FROM contacts contact WHERE contact.id=ct.contact_id AND contact.organization_id=ea.organization_id))
	AND (ct.sequence_id IS NULL OR EXISTS(SELECT 1 FROM sequences s WHERE s.id=ct.sequence_id AND s.organization_id=ea.organization_id AND (ct.campaign_id IS NULL OR s.campaign_id=ct.campaign_id)))
	AND ((ct.campaign_id IS NULL OR ct.contact_id IS NULL OR ct.sequence_id IS NULL)
	 OR NOT EXISTS(SELECT 1 FROM campaign_leads cl WHERE cl.campaign_id=ct.campaign_id AND cl.contact_id=ct.contact_id)
	 OR EXISTS(SELECT 1 FROM campaigns c JOIN contacts contact ON contact.id=ct.contact_id
	 JOIN sequences s ON s.id=ct.sequence_id AND s.campaign_id=c.id
	 JOIN campaign_leads cl ON cl.campaign_id=c.id AND cl.contact_id=contact.id
	 JOIN campaign_contact_progress p ON p.campaign_id=c.id AND p.contact_id=contact.id AND p.sequence_id=s.id
	 WHERE c.id=ct.campaign_id AND c.organization_id=ea.organization_id
	 AND contact.organization_id=c.organization_id AND s.organization_id=c.organization_id
	 AND p.dispatch_task_id=t.id AND p.dispatched_at IS NOT NULL))
	AND NOT EXISTS(SELECT 1 FROM send_recovery_resolutions sr WHERE sr.recovery_task_id=t.id
	 AND (sr.confirmed_result IS NOT NULL OR sr.conflict_detected_at IS NOT NULL))`

const isolatedCampaignHold = `t.campaign_send_isolated_at IS NOT NULL
	AND ea.send_recovery_hold AND ea.send_recovery_reason='unknown' AND ea.send_recovery_task_id=t.id`

// recoveryPausedLeadSQL is independent of ordinary holds and ends only after definitive reconciliation.
func recoveryPausedLeadSQL(alias string) string {
	return alias + `.send_outcome_pending_at IS NOT NULL`
}

func (r *taskRepository) IsolateUnknownCampaignSends(ctx context.Context, before time.Time, limit int) (int, error) {
	limit = min(max(limit, 1), 500)
	var after *time.Time
	afterTask := uuid.Nil
	isolated := 0
	for isolated < limit {
		rows, err := r.db.Query(ctx, `SELECT t.id,t.email_account_id,COALESCE(t.send_reserved_at,t.completed_at,t.updated_at) FROM tasks t
			JOIN campaign_tasks ct ON ct.task_id=t.id JOIN email_accounts ea ON ea.id=t.email_account_id
			WHERE ((`+isolatableUnknownCampaignSend+`) OR (`+isolatedCampaignHold+`))
			AND ($3::timestamptz IS NULL OR (COALESCE(t.send_reserved_at,t.completed_at,t.updated_at),t.id)>($3,$4::uuid))
			ORDER BY COALESCE(t.send_reserved_at,t.completed_at,t.updated_at),t.id LIMIT $2`, before, limit-isolated, after, afterTask)
		if err != nil {
			return isolated, err
		}
		type candidate struct {
			task, mailbox uuid.UUID
			at            time.Time
		}
		var candidates []candidate
		for rows.Next() {
			var c candidate
			if err := rows.Scan(&c.task, &c.mailbox, &c.at); err != nil {
				rows.Close()
				return isolated, err
			}
			candidates = append(candidates, c)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return isolated, err
		}
		if len(candidates) == 0 {
			break
		}
		for _, c := range candidates {
			ok, err := r.isolateUnknownCampaignSend(ctx, c.task, c.mailbox, before)
			if err != nil {
				return isolated, err
			}
			if ok {
				isolated++
			}
		}
		// Protected historical payloads must not starve later recoverable mailboxes.
		last := candidates[len(candidates)-1]
		after, afterTask = &last.at, last.task
	}
	return isolated, nil
}

func (r *taskRepository) isolateUnknownCampaignSend(ctx context.Context, task, mailbox uuid.UUID, before time.Time) (bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if err = lockSend(ctx, tx, task, mailbox); err != nil {
		return false, err
	}
	var executorResult, evidence []byte
	var alreadyIsolated bool
	var campaign, contact, sequence *uuid.UUID
	err = tx.QueryRow(ctx, `SELECT t.send_executor_result,t.send_result_evidence,t.campaign_send_isolated_at IS NOT NULL,
		ct.campaign_id,ct.contact_id,ct.sequence_id FROM tasks t
		JOIN campaign_tasks ct ON ct.task_id=t.id JOIN email_accounts ea ON ea.id=t.email_account_id
		WHERE t.id=$2 AND t.email_account_id=$3 AND ((`+isolatableUnknownCampaignSend+`) OR (`+isolatedCampaignHold+`))
		FOR UPDATE OF t,ct,ea`, before, task, mailbox).Scan(&executorResult, &evidence, &alreadyIsolated, &campaign, &contact, &sequence)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if alreadyIsolated {
		// Older replicas can reassert a historical unknown hold during a rolling upgrade.
		if err = refreshUnknownSendHold(ctx, tx, mailbox, task); err != nil {
			return false, err
		}
		return true, tx.Commit(ctx)
	}
	if len(executorResult) != 0 {
		var result models.SendEmailResult
		if json.Unmarshal(executorResult, &result) != nil || result.TaskID != task || resultState(result) != "unknown" ||
			result.MessageID != "" || result.ProviderMsgID != "" || !result.SentAt.IsZero() {
			return false, nil
		}
	}
	if len(evidence) != 0 {
		var failure errx.SendFailure
		if json.Unmarshal(evidence, &failure) != nil || failure.Disposition != errx.SendAmbiguous {
			return false, nil
		}
	}
	if campaign != nil && contact != nil {
		// ReserveSend and result walkbacks lock progress before the owning lead.
		var locked bool
		err = tx.QueryRow(ctx, `SELECT true FROM campaign_contact_progress
		WHERE campaign_id=$1 AND contact_id=$2 AND sequence_id=$3 AND dispatch_task_id=$4 AND dispatched_at IS NOT NULL
		FOR UPDATE`, campaign, contact, sequence, task).Scan(&locked)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return false, err
		}
		err = tx.QueryRow(ctx, `SELECT true FROM campaign_leads WHERE campaign_id=$1 AND contact_id=$2 FOR UPDATE`, campaign, contact).Scan(&locked)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return false, err
		}
	}
	// Recheck after waiting for routing locks; those writers need not take the send locks.
	tag, err := tx.Exec(ctx, `UPDATE tasks t SET status='cancelled',campaign_send_isolated_at=NOW(),updated_at=NOW()
		FROM campaign_tasks ct,email_accounts ea WHERE t.id=$2 AND ct.task_id=t.id AND ea.id=t.email_account_id
		AND (`+isolatableUnknownCampaignSend+`)`, before, task)
	if err != nil || tag.RowsAffected() == 0 {
		return false, err
	}
	if _, err = tx.Exec(ctx, `UPDATE campaign_leads
		SET send_outcome_pending_at=COALESCE(send_outcome_pending_at,NOW())
		WHERE campaign_id=$1 AND contact_id=$2`, campaign, contact); err != nil {
		return false, err
	}
	if err = resolveSendDeadLetter(ctx, tx, task); err != nil {
		return false, err
	}
	if err = refreshUnknownSendHold(ctx, tx, mailbox, task); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}
