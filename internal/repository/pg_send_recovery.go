package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

type sendResultKey struct{}

const refundedWarmupChargePredicate = `t.task_type='warmup' AND EXISTS(SELECT 1 FROM warmup_tasks charge
	WHERE charge.task_id=t.id AND charge.warmup_charged_date IS NOT NULL
	AND charge.warmup_reply_charged IS NOT NULL AND charge.warmup_refunded_at IS NOT NULL)`

const repairableSendDeadLetterPredicate = `t.status='dead_lettered' AND (t.send_result_state='sent'
	OR (t.send_result_state='failed' AND ` + refundedWarmupChargePredicate + `))`

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
	if tx, ok := ctx.Value(syncReportKey{}).(pgx.Tx); ok {
		return tx.Begin(ctx)
	}
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
		AND (status<>'cancelled' OR campaign_send_isolated_at IS NOT NULL) ORDER BY id LIMIT $2`, after, min(max(limit, 1), 200))
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
	// PauseReason is why RetryAt was set, empty when an earlier release wrote it.
	PauseReason string
}

type SendRecoveryHold struct {
	Reason   string
	Warmup   bool
	Campaign bool
}

func (r *emailRepository) GetSendRecoveryHolds(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]SendRecoveryHold, error) {
	// Every send holds its mailbox in flight; an unknown outcome surfaces only once it outlasts automatic recovery.
	query := `SELECT ea.id,COALESCE(ea.send_recovery_reason,''),COALESCE(t.task_type='warmup',false),
		COALESCE(t.task_type='campaign' AND t.send_executor_nonce IS NOT NULL AND t.send_executor_started_at IS NOT NULL
		AND EXISTS(SELECT 1 FROM campaign_tasks ct WHERE ct.task_id=t.id),false) FROM email_accounts ea
		LEFT JOIN tasks t ON t.id=ea.send_recovery_task_id AND t.email_account_id=ea.id
		LEFT JOIN warmup_tasks w ON w.task_id=t.id
		WHERE ea.id=ANY($1) AND ea.send_recovery_hold
		AND (ea.send_recovery_reason IS DISTINCT FROM 'unknown'
		 OR COALESCE(GREATEST(t.send_reserved_at,t.completed_at,t.send_executor_started_at,w.dispatch_started_at),t.updated_at)
		  <NOW()-make_interval(mins=>$2))`
	var rows pgx.Rows
	var err error
	if r.transportSnapshot != nil {
		rows, err = r.transportSnapshot.Query(ctx, query, ids, config.UnknownSendHoldVisibleAfterMinutes)
	} else {
		rows, err = r.DB.Query(ctx, query, ids, config.UnknownSendHoldVisibleAfterMinutes)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	holds := make(map[uuid.UUID]SendRecoveryHold)
	for rows.Next() {
		var id uuid.UUID
		var hold SendRecoveryHold
		if err := rows.Scan(&id, &hold.Reason, &hold.Warmup, &hold.Campaign); err != nil {
			return nil, err
		}
		hold.Campaign = hold.Campaign && config.CampaignSendIsolationEnabled()
		holds[id] = hold
	}
	return holds, rows.Err()
}

// GetSendPauses reads each mailbox's provider pause: a timed pause still running, or a refusal hold and its next re-probe.
func (r *emailRepository) GetSendPauses(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]models.SendPauseInfo, error) {
	rows, err := r.DB.Query(ctx, `SELECT ea.id,
		CASE WHEN ea.send_recovery_hold THEN ea.send_recovery_reason ELSE COALESCE(ea.send_cooldown_reason,'') END,
		ea.send_recovery_hold,
		CASE WHEN NOT ea.send_recovery_hold THEN ea.send_cooldown_until END,
		CASE WHEN ea.send_recovery_hold AND ea.send_recovery_reason IN('permanent','domain_auth') THEN COALESCE(ea.send_recovery_probe_at,
		 (SELECT COALESCE(t.send_result_applied_at,t.completed_at,t.updated_at) FROM tasks t WHERE t.id=ea.send_recovery_task_id)+make_interval(secs=>$2)) END,
		COALESCE(ea.send_pause_detail,'')
		FROM email_accounts ea WHERE ea.id=ANY($1)
		AND ((ea.send_recovery_hold AND ea.send_recovery_reason IN('permanent','domain_auth','authentication'))
		 OR (NOT ea.send_recovery_hold AND ea.send_cooldown_until>NOW()))`, ids, config.SendHoldProbe.Seconds())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[uuid.UUID]models.SendPauseInfo)
	for rows.Next() {
		var id uuid.UUID
		var p models.SendPauseInfo
		if err := rows.Scan(&id, &p.Reason, &p.Held, &p.Until, &p.RetryAt, &p.ProviderMessage); err != nil {
			return nil, err
		}
		p.Resumable = p.Reason != string(errx.SendPauseAuthentication)
		out[id] = p
	}
	return out, rows.Err()
}

// GetSendAdmission is tenant-scoped mailbox/provider evidence, not domain or tenant-wide inference.
func (r *taskRepository) GetSendAdmission(ctx context.Context, orgID, mailboxID uuid.UUID, provider models.InboxProvider, now time.Time) (*SendAdmission, error) {
	a := &SendAdmission{MailboxID: mailboxID, Provider: provider, Scope: "mailbox"}
	var active bool
	var current string
	err := r.db.QueryRow(ctx, `SELECT status = 'active', provider::text, send_cooldown_until, send_recovery_hold, COALESCE(send_recovery_reason, ''), COALESCE(send_cooldown_reason, '')
		FROM email_accounts WHERE id = $1 AND organization_id = $2`, mailboxID, orgID).Scan(&active, &current, &a.RetryAt, &a.RecoveryHold, &a.HoldReason, &a.PauseReason)
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

const SendResultMailboxLockNamespace int64 = 269

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
	var isolated bool
	err = tx.QueryRow(ctx, `SELECT send_result_state, send_result_applied_at, email_account_id, status::text,campaign_send_isolated_at IS NOT NULL
	 FROM tasks WHERE id = $1`, result.TaskID).Scan(&previous, &applied, &mailboxID, &status, &isolated)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, $2))`, mailboxID.String(), SendResultMailboxLockNamespace); err != nil {
		return err
	}
	state := resultState(result)
	// Receipt stamping is idempotent; an already-applied failure cannot be refunded again.
	repairDeadLetter := status == "dead_lettered" && previous != nil && *previous == state && state == "sent"
	if !repairDeadLetter && (applied != nil || (previous != nil && *previous != "unknown")) {
		if previous != nil && *previous != "unknown" && state != "unknown" && state != *previous {
			tag, err := tx.Exec(ctx, `UPDATE send_recovery_resolutions
				SET conflict_detected_at=COALESCE(conflict_detected_at,clock_timestamp()),conflicting_result_state=COALESCE(conflicting_result_state,$2)
				WHERE recovery_task_id=$1 AND recorded_locally_at IS NOT NULL
				AND (previous_reason='unknown' OR evidence_type='authoritative_result_conflict')`, result.TaskID, state)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				_, err = tx.Exec(ctx, `INSERT INTO send_recovery_resolutions
					(organization_id,email_account_id,recovery_task_id,previous_reason,evidence_type,conflict_detected_at,conflicting_result_state,recorded_locally_at)
					SELECT ea.organization_id,ea.id,t.id,'conflict','authoritative_result_conflict',clock_timestamp(),$2,clock_timestamp()
					FROM tasks t JOIN email_accounts ea ON ea.id=t.email_account_id WHERE t.id=$1`, result.TaskID, state)
				if err != nil {
					return err
				}
			}
			if err = refreshUnknownSendHold(ctx, tx, mailboxID, result.TaskID); err != nil {
				return err
			}
			if err = refreshSendHold(ctx, tx, mailboxID, nil, nil); err != nil {
				return err
			}
			return tx.Commit(ctx)
		}
		if applied != nil && previous != nil && state == *previous && state != "unknown" {
			if status == "dead_lettered" {
				// Repair metadata only; the positive refund proof forbids repeating accounting.
				tag, err := tx.Exec(ctx, `UPDATE tasks t SET status='failed',updated_at=NOW() WHERE t.id=$1 AND t.status='dead_lettered'
					AND t.send_result_state='failed' AND t.send_result_applied_at IS NOT NULL AND `+refundedWarmupChargePredicate, result.TaskID)
				if err != nil {
					return err
				}
				if tag.RowsAffected() == 0 {
					return nil
				}
			}
			if err = resolveSendDeadLetter(ctx, tx, result.TaskID); err != nil {
				return err
			}
			return tx.Commit(ctx)
		}
		return nil
	}
	if status == "cancelled" && (!isolated || state == "unknown") {
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
		// Reconcile late evidence without giving the isolated command execution authority.
		if isolated {
			if _, err = tx.Exec(ctx, `UPDATE tasks SET status='completed' WHERE id=$1`, result.TaskID); err != nil {
				return err
			}
		}
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
		if isolated {
			// Serialize confirmations from different mailboxes before checking remaining contact protection.
			if _, err = tx.Exec(ctx, `SELECT 1 FROM campaign_leads cl JOIN campaign_tasks ct
				ON ct.campaign_id=cl.campaign_id AND ct.contact_id=cl.contact_id WHERE ct.task_id=$1 FOR UPDATE OF cl`, result.TaskID); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `UPDATE campaign_leads cl SET send_outcome_pending_at=NULL
				FROM campaign_tasks ct WHERE ct.task_id=$1 AND cl.campaign_id=ct.campaign_id AND cl.contact_id=ct.contact_id
				AND NOT EXISTS(SELECT 1 FROM campaign_tasks other_ct JOIN tasks other_t ON other_t.id=other_ct.task_id
				 WHERE other_ct.campaign_id=cl.campaign_id AND other_ct.contact_id=cl.contact_id
				 AND other_t.campaign_send_isolated_at IS NOT NULL AND other_t.send_result_applied_at IS NULL)`, result.TaskID); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `UPDATE tasks SET status='cancelled' WHERE id=$1`, result.TaskID); err != nil {
				return err
			}
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

func refreshUnknownSendHold(ctx context.Context, tx pgx.Tx, mailboxID, taskID uuid.UUID) error {
	reason := "unknown"
	return refreshSendHold(ctx, tx, mailboxID, &taskID, &reason)
}

func refreshSendHold(ctx context.Context, tx pgx.Tx, mailboxID uuid.UUID, taskID *uuid.UUID, reason *string) error {
	_, err := tx.Exec(ctx, `WITH next_hold AS (
			SELECT id,reason FROM (
			 SELECT t.id,'unknown' AS reason,1 AS priority,t.created_at FROM tasks t
			 WHERE t.email_account_id=$1 AND t.send_result_state='unknown' AND t.warmup_send_retired_at IS NULL AND t.campaign_send_isolated_at IS NULL AND t.send_hold_lifted_at IS NULL
			 UNION ALL
			 SELECT t.id,'conflict',2,t.created_at FROM send_recovery_resolutions r JOIN tasks t ON t.id=r.recovery_task_id
			 WHERE t.email_account_id=$1 AND (r.previous_reason='unknown' OR r.evidence_type='authoritative_result_conflict')
			 AND r.conflict_detected_at IS NOT NULL AND r.conflict_resolved_at IS NULL
			) candidates ORDER BY priority,created_at,id LIMIT 1)
			UPDATE email_accounts ea SET send_recovery_task_id=n.id,send_recovery_hold=n.id IS NOT NULL,send_recovery_reason=n.reason
			FROM (SELECT 1) singleton LEFT JOIN next_hold n ON true
			WHERE ea.id=$1 AND (
			 ($3::text IS NOT NULL AND ea.send_recovery_task_id IS NOT DISTINCT FROM $2::uuid
			  AND ea.send_recovery_reason IS NOT DISTINCT FROM $3::text)
			 OR ($2::uuid IS NULL AND $3::text IS NULL AND NOT ea.send_recovery_hold))`, mailboxID, taskID, reason)
	return err
}

func persistSendHold(ctx context.Context, tx pgx.Tx, mailboxID uuid.UUID, result models.SendEmailResult, state string) error {
	if state != "unknown" {
		if err := refreshUnknownSendHold(ctx, tx, mailboxID, result.TaskID); err != nil {
			return err
		}
	}
	if state == "sent" {
		// A send that got through ends the run of throttles and re-probes.
		_, err := tx.Exec(ctx, `UPDATE email_accounts SET send_cooldown_strikes=0,send_recovery_probes=0
			WHERE id=$1 AND (send_cooldown_strikes<>0 OR send_recovery_probes<>0)`, mailboxID)
		return err
	}
	var f *errx.SendFailure
	if result.Error != nil {
		f = result.Error.Failure
	}
	if state == "unknown" {
		_, err := tx.Exec(ctx, `UPDATE email_accounts SET send_recovery_hold = true, send_recovery_task_id = $2,
			send_recovery_reason = 'unknown', send_cooldown_provider = provider::text
			WHERE id = $1 AND (NOT send_recovery_hold OR send_recovery_reason = 'unknown')
			AND EXISTS(SELECT 1 FROM tasks t WHERE t.id=$2 AND t.campaign_send_isolated_at IS NULL AND t.send_hold_lifted_at IS NULL)`, mailboxID, result.TaskID)
		return err
	}
	return persistSendPause(ctx, tx, mailboxID, result, f)
}

func persistSendPause(ctx context.Context, tx pgx.Tx, mailboxID uuid.UUID, result models.SendEmailResult, f *errx.SendFailure) error {
	code := ""
	if result.Error != nil {
		code = result.Error.Code
	}
	kind := errx.ClassifySendPause(code, f)
	detail := sendPauseDetail(result)
	switch {
	case kind.Timed():
		observed := result.SentAt
		var retryAt *time.Time
		if f != nil {
			retryAt = f.RetryAt
			if !f.ObservedAt.IsZero() {
				observed = f.ObservedAt
			}
		}
		if observed.IsZero() {
			observed = time.Now().UTC()
		}
		return applySendPause(ctx, tx, mailboxID, kind, retryAt, observed, detail)
	case kind.Held():
		// A refusal the owner may fix outside Warmbly is re-probed on a backoff; a sign-in refusal waits for a reconnect.
		_, err := tx.Exec(ctx, `UPDATE email_accounts SET send_recovery_hold = true, send_recovery_task_id = $2,
			send_recovery_reason = $3, send_cooldown_provider = provider::text, send_pause_detail = $4,
			send_recovery_probe_at = CASE WHEN $3 IN ('permanent','domain_auth')
			 THEN NOW() + make_interval(secs => LEAST($5::float8 * power(2, LEAST(send_recovery_probes, 10)), $6::float8)) END
			WHERE id = $1`,
			mailboxID, result.TaskID, string(kind), detail, config.SendHoldProbe.Seconds(), config.SendHoldProbeMax.Seconds())
		return err
	}
	return nil
}

// applySendPause extends a mailbox's timed pause, escalating on a repeated throttle; a later end is never cut short.
func applySendPause(ctx context.Context, tx pgx.Tx, mailboxID uuid.UUID, kind errx.SendPause, retryAt *time.Time, observed time.Time, detail string) error {
	var prev errx.SendPauseState
	var prevKind *string
	if err := tx.QueryRow(ctx, `SELECT send_cooldown_until,send_cooldown_reason,send_cooldown_strikes FROM email_accounts WHERE id=$1 FOR UPDATE`, mailboxID).
		Scan(&prev.Until, &prevKind, &prev.Strikes); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	if prevKind != nil {
		prev.Kind = errx.SendPause(*prevKind)
	}
	next := errx.NextSendPause(kind, prev, retryAt, observed)
	_, err := tx.Exec(ctx, `UPDATE email_accounts SET send_cooldown_until=GREATEST(send_cooldown_until,$2),send_cooldown_reason=$3,
		send_cooldown_strikes=$4,send_pause_detail=$5,send_cooldown_provider=provider::text WHERE id=$1`,
		mailboxID, next.Until, string(next.Kind), next.Strikes, detail)
	return err
}

// sendPauseDetail is the provider's own words, bounded, for the owner to read.
func sendPauseDetail(result models.SendEmailResult) string {
	if result.Error == nil {
		return ""
	}
	msg := strings.TrimSpace(result.Error.Message)
	if len(msg) > 500 {
		msg = msg[:500]
	}
	return strings.ToValidUTF8(msg, "")
}

// ReleaseReconnectedHold lifts an authentication or permanent hold once the mailbox's credentials passed a fresh login check, recording why; any unknown or conflict hold still owed is re-applied.
func (r *emailRepository) ReleaseReconnectedHold(ctx context.Context, orgID, accountID uuid.UUID) (bool, error) {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `WITH held AS (SELECT organization_id,id,send_recovery_task_id,send_recovery_reason FROM email_accounts
		WHERE organization_id=$1 AND id=$2 AND send_recovery_hold AND send_recovery_reason IN('authentication','permanent') FOR UPDATE),
		history AS (INSERT INTO send_recovery_resolutions(organization_id,email_account_id,recovery_task_id,previous_reason,evidence_type,recorded_locally_at)
		SELECT organization_id,id,(SELECT t.id FROM tasks t WHERE t.id=held.send_recovery_task_id),send_recovery_reason,'credentials_reverified',clock_timestamp() FROM held RETURNING email_account_id)
		UPDATE email_accounts ea SET send_recovery_hold=false,send_recovery_reason=NULL,send_recovery_task_id=NULL
		FROM history WHERE ea.id=history.email_account_id`, orgID, accountID)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() != 1 {
		return false, nil
	}
	if err := refreshSendHold(ctx, tx, accountID, nil, nil); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}
