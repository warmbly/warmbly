package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

type sendResultKey struct{}
type sendResultContext struct {
	tx          pgx.Tx
	taskID      uuid.UUID
	effects     *[]func(context.Context)
	effectError *error
}

func AfterSendResultCommit(ctx context.Context, effect func(context.Context)) {
	if state, ok := ctx.Value(sendResultKey{}).(sendResultContext); ok && state.effects != nil {
		*state.effects = append(*state.effects, effect)
		return
	}
	effect(ctx)
}

type sendResultDB interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type sendResultBeginner interface {
	Begin(context.Context) (pgx.Tx, error)
}

func beginResultTx(ctx context.Context, fallback sendResultBeginner) (pgx.Tx, error) {
	if state, ok := ctx.Value(sendResultKey{}).(sendResultContext); ok {
		return state.tx.Begin(ctx)
	}
	return fallback.Begin(ctx)
}

func resultDB(ctx context.Context, fallback sendResultDB) sendResultDB {
	if state, ok := ctx.Value(sendResultKey{}).(sendResultContext); ok {
		return state.tx
	}
	return fallback
}

func resultTaskID(ctx context.Context) *uuid.UUID {
	if state, ok := ctx.Value(sendResultKey{}).(sendResultContext); ok {
		return &state.taskID
	}
	return nil
}

// SendResultRecovery is implemented by the existing PostgreSQL task repository.
type SendResultRecovery interface {
	ApplySendResult(context.Context, models.SendEmailResult, func(context.Context) error) error
	GetSendAdmission(context.Context, uuid.UUID, uuid.UUID, models.InboxProvider, time.Time) (*SendAdmission, error)
}

type StoredSendResult struct {
	TaskID  uuid.UUID
	Payload json.RawMessage
}

type StoredSendResultRepository interface {
	SendResultRecovery
	ListPendingSendResults(context.Context, uuid.UUID, int) ([]StoredSendResult, error)
}

func (r *taskRepository) ListPendingSendResults(ctx context.Context, after uuid.UUID, limit int) ([]StoredSendResult, error) {
	rows, err := r.db.Query(ctx, `SELECT id,send_executor_result FROM tasks
		WHERE id>$1 AND send_executor_result IS NOT NULL AND send_executor_started_at IS NOT NULL
		AND send_executor_nonce IS NOT NULL
		AND send_result_applied_at IS NULL AND (send_result_state IS NULL OR send_result_state='unknown')
		AND status<>'cancelled' ORDER BY id LIMIT $2`, after, min(max(limit, 1), 200))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []StoredSendResult
	for rows.Next() {
		var result StoredSendResult
		if err := rows.Scan(&result.TaskID, &result.Payload); err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, rows.Err()
}

type SendAdmission struct {
	MailboxID    uuid.UUID
	Provider     models.InboxProvider
	Scope        string
	Allowed      bool
	RetryAt      *time.Time
	RecoveryHold bool
	HoldReason   string
}

// GetSendAdmission is tenant-scoped mailbox/provider evidence, not domain or tenant-wide inference.
func (r *taskRepository) GetSendAdmission(ctx context.Context, orgID, mailboxID uuid.UUID, provider models.InboxProvider, now time.Time) (*SendAdmission, error) {
	a := &SendAdmission{MailboxID: mailboxID, Provider: provider, Scope: "mailbox"}
	var active bool
	var current string
	err := r.db.QueryRow(ctx, `SELECT status = 'active', provider::text, send_cooldown_until, send_recovery_hold, COALESCE(send_recovery_reason, '')
		FROM email_accounts WHERE id = $1 AND organization_id = $2`, mailboxID, orgID).Scan(&active, &current, &a.RetryAt, &a.RecoveryHold, &a.HoldReason)
	if err != nil {
		return nil, err
	}
	a.Allowed = active && current == string(provider) && !a.RecoveryHold && (a.RetryAt == nil || !a.RetryAt.After(now))
	return a, nil
}

func resultState(result models.SendEmailResult) string {
	if result.Success {
		return "sent"
	}
	if result.Error == nil {
		return "unknown"
	}
	if result.Error.Failure != nil {
		switch result.Error.Failure.Disposition {
		case errx.SendRetry, errx.SendThrottle, errx.SendPermanent, errx.SendAuth:
			return "failed"
		default:
			return "unknown"
		}
	}
	// Legacy named refusals are definite; an absent classification proves nothing.
	switch errx.MailErrorCode(result.Error.Code) {
	case errx.MailErrorCodeRecipientRejected, errx.MailErrorCodeSendRejected, errx.MailErrorCodeDomainAuthRejected,
		errx.MailErrorCodeAuthenticationFailed, errx.MailErrorCodeInvalidCredentials, errx.MailErrorCodeGoogleAuth,
		errx.MailErrorCodeSendingTooFast, errx.MailErrorCodeQuotaExceeded, errx.MailErrorCodeUnsupported:
		return "failed"
	}
	return "unknown"
}

// ApplySendResult commits reconciliation and its terminal marker together.
func (r *taskRepository) ApplySendResult(ctx context.Context, result models.SendEmailResult, apply func(context.Context) error) error {
	if result.TaskID == uuid.Nil {
		return nil
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 268))`, result.TaskID.String()); err != nil {
		return err
	}
	var previous *string
	var applied *time.Time
	var mailboxID uuid.UUID
	var status string
	err = tx.QueryRow(ctx, `SELECT send_result_state, send_result_applied_at, email_account_id, status::text
	 FROM tasks WHERE id = $1`, result.TaskID).Scan(&previous, &applied, &mailboxID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 269))`, mailboxID.String()); err != nil {
		return err
	}
	state := resultState(result)
	// Receipt stamping is idempotent; an already-applied failure cannot be refunded again.
	repairDeadLetter := status == "dead_lettered" && previous != nil && *previous == state && state == "sent"
	if !repairDeadLetter && (applied != nil || (previous != nil && *previous != "unknown")) {
		if previous != nil && state != "unknown" && state != *previous {
			if _, err = tx.Exec(ctx, `UPDATE send_recovery_resolutions SET conflict_detected_at=NOW()
				WHERE recovery_task_id=$1 AND previous_reason='unknown' AND confirmed_result IS NOT NULL`, result.TaskID); err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `UPDATE email_accounts SET send_recovery_hold = true, send_recovery_reason = 'conflict', send_recovery_task_id = $2
				WHERE id = $1 AND (NOT send_recovery_hold OR (send_recovery_reason = 'unknown' AND send_recovery_task_id = $2))`, mailboxID, result.TaskID)
			if err != nil {
				return err
			}
			return tx.Commit(ctx)
		}
		if applied != nil && status != "dead_lettered" && previous != nil && state == *previous && state != "unknown" {
			if err = resolveSendDeadLetter(ctx, tx, result.TaskID); err != nil {
				return err
			}
			return tx.Commit(ctx)
		}
		return nil
	}
	if status == "cancelled" {
		return nil
	}
	if state == "failed" && status == "active" {
		return errors.New("send result arrived before task stamping")
	}
	var evidence []byte
	if result.Error != nil && result.Error.Failure != nil {
		evidence, err = json.Marshal(result.Error.Failure)
		if err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE tasks SET send_result_state = $2, send_result_evidence = $3 WHERE id = $1`, result.TaskID, state, evidence); err != nil {
		return err
	}
	var effects []func(context.Context)
	var effectError error
	if state != "unknown" {
		if status == "dead_lettered" && state == "sent" {
			sentAt := result.SentAt
			if sentAt.IsZero() {
				sentAt = time.Now().UTC()
			}
			if _, err = tx.Exec(ctx, `UPDATE tasks SET status='completed',completed_at=COALESCE(completed_at,$2),updated_at=NOW() WHERE id=$1`, result.TaskID, sentAt); err != nil {
				return err
			}
		}
		inner := context.WithValue(ctx, sendResultKey{}, sendResultContext{tx: tx, taskID: result.TaskID, effects: &effects, effectError: &effectError})
		if err = apply(inner); err != nil {
			return err
		}
		if effectError != nil {
			return effectError
		}
		if _, err = tx.Exec(ctx, `UPDATE tasks SET send_result_applied_at = NOW(), send_released_at=CASE WHEN send_result_state='failed' THEN NOW() ELSE send_released_at END WHERE id = $1`, result.TaskID); err != nil {
			return err
		}
		if err = resolveSendDeadLetter(ctx, tx, result.TaskID); err != nil {
			return err
		}
	}
	if err = persistSendHold(ctx, tx, mailboxID, result, state); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	for _, effect := range effects {
		effect(ctx)
	}
	return nil
}

func resolveSendDeadLetter(ctx context.Context, tx pgx.Tx, taskID uuid.UUID) error {
	_, err := tx.Exec(ctx, `UPDATE task_dead_letters SET status='resolved',next_retry_at=NULL,updated_at=NOW()
		WHERE task_id=$1 AND status IN ('pending','failed','replayed')
		AND EXISTS(SELECT 1 FROM tasks t WHERE t.id=$1 AND t.task_type IN ('warmup','campaign','email','placement'))`, taskID)
	return err
}

func persistSendHold(ctx context.Context, tx pgx.Tx, mailboxID uuid.UUID, result models.SendEmailResult, state string) error {
	if state != "unknown" {
		_, err := tx.Exec(ctx, `WITH next_hold AS (
			SELECT id,reason FROM (
			 SELECT t.id,'unknown' AS reason,1 AS priority,t.created_at FROM tasks t
			 WHERE t.email_account_id=$1 AND t.send_result_state='unknown'
			 UNION ALL
			 SELECT t.id,'conflict',2,t.created_at FROM send_recovery_resolutions r JOIN tasks t ON t.id=r.recovery_task_id
			 WHERE t.email_account_id=$1 AND r.previous_reason='unknown' AND r.conflict_detected_at IS NOT NULL
			 AND NOT EXISTS(SELECT 1 FROM send_recovery_resolutions resolved WHERE resolved.recovery_task_id=t.id AND resolved.previous_reason='conflict')
			) candidates ORDER BY priority,created_at,id LIMIT 1)
			UPDATE email_accounts ea SET send_recovery_task_id=n.id,send_recovery_hold=n.id IS NOT NULL,send_recovery_reason=n.reason
			FROM (SELECT 1) singleton LEFT JOIN next_hold n ON true
			WHERE ea.id=$1 AND ea.send_recovery_task_id=$2 AND ea.send_recovery_reason='unknown'`, mailboxID, result.TaskID)
		if err != nil {
			return err
		}
	}
	if state == "sent" {
		return nil
	}
	var f *errx.SendFailure
	if result.Error != nil {
		f = result.Error.Failure
	}
	if state == "unknown" {
		_, err := tx.Exec(ctx, `UPDATE email_accounts SET send_recovery_hold = true, send_recovery_task_id = $2,
			send_recovery_reason = 'unknown', send_cooldown_provider = provider::text
			WHERE id = $1 AND (NOT send_recovery_hold OR send_recovery_reason = 'unknown')`, mailboxID, result.TaskID)
		return err
	}
	if f == nil {
		if result.Error != nil && (result.Error.Code == string(errx.MailErrorCodeSendingTooFast) || result.Error.Code == string(errx.MailErrorCodeQuotaExceeded)) {
			base := result.SentAt
			if base.IsZero() {
				base = time.Now().UTC()
			}
			_, err := tx.Exec(ctx, `UPDATE email_accounts SET send_cooldown_until=GREATEST(send_cooldown_until,$2),send_cooldown_provider=provider::text WHERE id=$1`, mailboxID, base.Add(5*time.Minute))
			return err
		}
		return nil
	}
	if f.Disposition == errx.SendRetry || f.Disposition == errx.SendThrottle {
		if f.Protocol == "internal" && f.Stage == "prepare" {
			return nil
		}
		until := f.RetryAt
		if until == nil {
			// Bounded product backoff; not a provider promise.
			base := f.ObservedAt
			if base.IsZero() {
				base = result.SentAt
			}
			if base.IsZero() {
				base = time.Now().UTC()
			}
			at := base.Add(5 * time.Minute)
			until = &at
		}
		_, err := tx.Exec(ctx, `UPDATE email_accounts SET send_cooldown_until = GREATEST(send_cooldown_until, $2), send_cooldown_provider = provider::text WHERE id = $1`, mailboxID, until)
		return err
	}
	if f.Disposition == errx.SendAuth || f.Disposition == errx.SendPermanent && f.Scope != "recipient" {
		_, err := tx.Exec(ctx, `UPDATE email_accounts SET send_recovery_hold = true, send_recovery_task_id = $2,
			send_recovery_reason = $3, send_cooldown_provider = provider::text WHERE id = $1`, mailboxID, result.TaskID, f.Disposition)
		return err
	}
	return nil
}
