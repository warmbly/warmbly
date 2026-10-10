package repository

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func queueUnknownSendResolution(ctx context.Context, tx pgx.Tx, org, mailbox string, resolution *models.SendRecoveryResolution) *errx.Error {
	if resolution.HeldTaskID == uuid.Nil || resolution.EvidenceTaskID != nil || strings.TrimSpace(resolution.ConfirmationReference) == "" {
		return errx.ErrInvalid
	}
	for _, value := range []string{resolution.MessageID, resolution.ProviderMsgID, resolution.ThreadID} {
		if len(value) > 998 || strings.ContainsAny(value, "\r\n\x00") {
			return errx.ErrInvalid
		}
	}
	result := models.SendEmailResult{TaskID: resolution.HeldTaskID, SentAt: time.Now().UTC()}
	switch resolution.EvidenceType {
	case "operator_confirmed_sent":
		if strings.TrimSpace(resolution.MessageID) == "" {
			return errx.ErrInvalid
		}
		result.Success, result.MessageID = true, resolution.MessageID
		result.ProviderMsgID, result.ThreadID = resolution.ProviderMsgID, resolution.ThreadID
	case "operator_confirmed_not_sent":
		if resolution.MessageID != "" || resolution.ProviderMsgID != "" || resolution.ThreadID != "" {
			return errx.ErrInvalid
		}
		result.LegacyErrorMsg = "Operator confirmed with the provider that this task was not sent"
		result.Error = &models.EmailSendError{Message: result.LegacyErrorMsg, Failure: &errx.SendFailure{
			Protocol: "internal", Stage: "prepare", Scope: "mailbox", Disposition: errx.SendRetry, ObservedAt: result.SentAt,
		}}
	default:
		return errx.ErrInvalid
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,268))`, resolution.HeldTaskID.String()); err != nil {
		return errx.InternalError()
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,269))`, mailbox); err != nil {
		return errx.InternalError()
	}
	var native, warmup []byte
	err := tx.QueryRow(ctx, `SELECT t.send_executor_result,w.dispatch_result FROM tasks t
	 JOIN email_accounts ea ON ea.id=t.email_account_id LEFT JOIN warmup_tasks w ON w.task_id=t.id
	 WHERE ea.organization_id=$1 AND ea.id=$2 AND ea.status='active' AND ea.send_recovery_hold AND ea.send_recovery_reason='unknown'
	 AND ea.send_recovery_task_id=$3 AND t.id=$3 AND t.send_result_state='unknown' AND t.send_result_applied_at IS NULL
	 AND t.status IN('completed','failed') AND COALESCE(t.send_executor_started_at,w.dispatch_started_at,t.send_reserved_at,t.completed_at,t.created_at)<NOW()-INTERVAL '2 minutes'
	 FOR UPDATE OF t,ea`, org, mailbox, resolution.HeldTaskID).Scan(&native, &warmup)
	if err == pgx.ErrNoRows {
		return errx.ErrInvalid
	}
	if err != nil {
		return errx.InternalError()
	}
	for _, stored := range [][]byte{native, warmup} {
		if len(stored) == 0 {
			continue
		}
		var actual models.SendEmailResult
		if json.Unmarshal(stored, &actual) != nil || resultState(actual) != "unknown" {
			return errx.ErrInvalid
		}
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return errx.InternalError()
	}
	// Keep admission held until the normal result consumer commits all accounting effects.
	tag, err := tx.Exec(ctx, `INSERT INTO send_recovery_resolutions(organization_id,email_account_id,recovery_task_id,previous_reason,evidence_type,confirmation_reference,confirmed_result)
	 SELECT ea.organization_id,ea.id,t.id,'unknown',$4,$5,$6 FROM email_accounts ea JOIN tasks t ON t.email_account_id=ea.id
	 WHERE ea.organization_id=$1 AND ea.id=$2 AND ea.status='active' AND ea.send_recovery_hold AND ea.send_recovery_reason='unknown'
	 AND ea.send_recovery_task_id=$3 AND t.id=$3 AND t.send_result_state='unknown' AND t.send_result_applied_at IS NULL
	 AND NOT EXISTS(SELECT 1 FROM send_recovery_resolutions r WHERE r.recovery_task_id=t.id AND r.previous_reason='unknown')`,
		org, mailbox, resolution.HeldTaskID, resolution.EvidenceType, strings.TrimSpace(resolution.ConfirmationReference), raw)
	if err != nil {
		return errx.InternalError()
	}
	if tag.RowsAffected() != 1 {
		return errx.ErrInvalid
	}
	return nil
}

func (r *taskRepository) ListUnappliedSendResults(ctx context.Context, limit int) ([]models.SendEmailResult, error) {
	rows, err := r.db.Query(ctx, `WITH recorded AS (
	 SELECT t.id,t.created_at,t.send_result_state,t.send_result_applied_at,t.send_executor_result AS result,1 AS priority
	 FROM tasks t WHERE t.send_executor_result IS NOT NULL AND t.status<>'cancelled' AND t.send_result_applied_at IS NULL
	 UNION ALL
	 SELECT t.id,t.created_at,t.send_result_state,t.send_result_applied_at,t.send_executor_result,1 FROM send_recovery_resolutions r JOIN tasks t ON t.id=r.recovery_task_id
	 WHERE r.previous_reason='unknown' AND r.conflict_detected_at IS NULL AND r.confirmed_result IS NOT NULL
	 AND t.send_executor_result IS NOT NULL AND t.send_result_applied_at IS NOT NULL AND t.status<>'cancelled'
	 UNION ALL
	 SELECT t.id,t.created_at,t.send_result_state,t.send_result_applied_at,w.dispatch_result,1 FROM warmup_tasks w JOIN tasks t ON t.id=w.task_id
	 WHERE w.dispatch_result IS NOT NULL AND t.status<>'cancelled' AND t.send_result_applied_at IS NULL
	 UNION ALL
	 SELECT t.id,t.created_at,t.send_result_state,t.send_result_applied_at,w.dispatch_result,1 FROM send_recovery_resolutions r JOIN tasks t ON t.id=r.recovery_task_id JOIN warmup_tasks w ON w.task_id=t.id
	 WHERE r.previous_reason='unknown' AND r.conflict_detected_at IS NULL AND r.confirmed_result IS NOT NULL
	 AND w.dispatch_result IS NOT NULL AND t.send_result_applied_at IS NOT NULL AND t.status<>'cancelled'
	 UNION ALL
	 SELECT t.id,t.created_at,t.send_result_state,t.send_result_applied_at,r.confirmed_result,2 FROM send_recovery_resolutions r JOIN tasks t ON t.id=r.recovery_task_id
	 WHERE r.confirmed_result IS NOT NULL AND t.send_result_applied_at IS NULL AND t.status<>'cancelled'
	 UNION ALL
	 SELECT t.id,t.created_at,t.send_result_state,t.send_result_applied_at,jsonb_build_object('task_id',t.id,'success',false,'legacy_error','Send failed before reservation',
	 'error',jsonb_build_object('failure',jsonb_build_object('protocol','internal','stage','prepare','scope','mailbox','disposition','retry'))),3
	 FROM tasks t WHERE t.status='failed' AND t.send_result_state='unknown' AND t.send_result_applied_at IS NULL
	 AND t.send_reserved_at IS NULL AND t.send_executor_nonce IS NULL AND t.send_executor_started_at IS NULL
	 AND NOT EXISTS(SELECT 1 FROM warmup_tasks w WHERE w.task_id=t.id AND w.dispatch_nonce IS NOT NULL)),
	 definitive AS (SELECT *,CASE WHEN result->>'success'='true' THEN 'sent' ELSE 'failed' END AS outcome FROM recorded
	 WHERE result->>'success'='true' OR result->'error'->'failure'->>'disposition' IN('retry','throttle','permanent','authentication')
	 OR (result->'error'->>'failure' IS NULL AND result->'error'->>'code' IN
	 ('RECIPIENT_REJECTED','SEND_REJECTED','DOMAIN_AUTH_REJECTED','AUTHENTICATION_FAILED','INVALID_CREDENTIALS','GOOGLE_AUTHENTICATION_FAILED','SENDING_TOO_FAST','QUOTA_EXCEEDED','UNSUPPORTED'))),
	 pending AS (SELECT DISTINCT ON(id) id,created_at,result FROM definitive
	 WHERE send_result_applied_at IS NULL OR outcome IS DISTINCT FROM send_result_state ORDER BY id,priority)
	 SELECT id,result FROM pending ORDER BY created_at LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []models.SendEmailResult
	for rows.Next() {
		var id uuid.UUID
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		var result models.SendEmailResult
		if err := json.Unmarshal(raw, &result); err != nil {
			return nil, err
		}
		if result.TaskID == id && resultState(result) != "unknown" {
			results = append(results, result)
		}
	}
	return results, rows.Err()
}
