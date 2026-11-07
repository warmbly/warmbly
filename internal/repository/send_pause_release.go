package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/config"
)

// Evidence recorded when a hold is released without a fresh send outcome.
const (
	SendHoldEvidenceProbe           = "hold_probe"
	SendHoldEvidenceOperatorResumed = "operator_resumed"
	SendHoldEvidenceDomainAuth      = "domain_auth_passing"
)

// releaseSendHold lifts one mailbox's hold of the given reasons, recording why; an unknown or conflict hold still owed is re-applied.
func releaseSendHold(ctx context.Context, tx pgx.Tx, mailbox uuid.UUID, reasons []string, evidence string, probed bool) (bool, error) {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,269))`, mailbox.String()); err != nil {
		return false, err
	}
	probe := 0
	if probed {
		probe = 1
	}
	tag, err := tx.Exec(ctx, `WITH held AS(SELECT organization_id,id,send_recovery_task_id,send_recovery_reason FROM email_accounts
		WHERE id=$1 AND send_recovery_hold AND send_recovery_reason=ANY($2) FOR UPDATE),
		history AS(INSERT INTO send_recovery_resolutions(organization_id,email_account_id,recovery_task_id,previous_reason,evidence_type,recorded_locally_at)
		SELECT organization_id,id,(SELECT t.id FROM tasks t WHERE t.id=held.send_recovery_task_id),send_recovery_reason,$3,clock_timestamp() FROM held RETURNING email_account_id)
		UPDATE email_accounts ea SET send_recovery_hold=false,send_recovery_reason=NULL,send_recovery_task_id=NULL,
		send_recovery_probe_at=NULL,send_recovery_probes=ea.send_recovery_probes+$4
		FROM history WHERE ea.id=history.email_account_id`, mailbox, reasons, evidence, probe)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() != 1 {
		return false, nil
	}
	return true, refreshSendHold(ctx, tx, mailbox, nil, nil)
}

// probeDueHold is a refusal hold past its re-probe time; one written before probes existed is due three hours after the refusal.
const probeDueHold = `ea.status='active' AND ea.send_recovery_hold AND ea.send_recovery_reason IN('permanent','domain_auth')
	AND COALESCE(ea.send_recovery_probe_at,
	 (SELECT COALESCE(t.send_result_applied_at,t.completed_at,t.updated_at) FROM tasks t WHERE t.id=ea.send_recovery_task_id)+make_interval(secs=>$1))<=NOW()`

// ReleaseProbeDueHolds lets each held mailbox past its re-probe time try one more send; a refusal holds it again for twice as long.
func (r *taskRepository) ReleaseProbeDueHolds(ctx context.Context, limit int) ([]uuid.UUID, error) {
	rows, err := r.db.Query(ctx, `SELECT ea.id FROM email_accounts ea WHERE `+probeDueHold+` ORDER BY ea.id LIMIT $2`,
		config.SendHoldProbe.Seconds(), min(max(limit, 1), 500))
	if err != nil {
		return nil, err
	}
	candidates, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, err
	}
	var released []uuid.UUID
	var errs []error
	for _, mailbox := range candidates {
		if ctx.Err() != nil {
			break
		}
		ok, err := r.releaseProbe(ctx, mailbox)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if ok {
			released = append(released, mailbox)
		}
	}
	return released, errors.Join(append(errs, ctx.Err())...)
}

func (r *taskRepository) releaseProbe(ctx context.Context, mailbox uuid.UUID) (bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var due bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM email_accounts ea WHERE ea.id=$2 AND `+probeDueHold+`)`, config.SendHoldProbe.Seconds(), mailbox).Scan(&due); err != nil || !due {
		return false, err
	}
	ok, err := releaseSendHold(ctx, tx, mailbox, []string{"permanent", "domain_auth"}, SendHoldEvidenceProbe, true)
	if err != nil || !ok {
		return false, err
	}
	return true, tx.Commit(ctx)
}

// SendPauseRelease is what an owner's "Resume sending" lifted.
type SendPauseRelease struct {
	Hold     bool `json:"hold_released"`
	Cooldown bool `json:"cooldown_cleared"`
}

// ResumeSending is the owner's way out of a provider pause or refusal hold; an unconfirmed send and a sign-in refusal are left to their own recovery.
func (r *emailRepository) ResumeSending(ctx context.Context, orgID, accountID uuid.UUID) (SendPauseRelease, error) {
	var out SendPauseRelease
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	var owned bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM email_accounts WHERE id=$1 AND organization_id=$2)`, accountID, orgID).Scan(&owned); err != nil {
		return out, err
	}
	if !owned {
		return out, pgx.ErrNoRows
	}
	if out.Hold, err = releaseSendHold(ctx, tx, accountID, []string{"permanent", "domain_auth"}, SendHoldEvidenceOperatorResumed, false); err != nil {
		return out, err
	}
	tag, err := tx.Exec(ctx, `UPDATE email_accounts SET send_cooldown_until=NULL WHERE id=$1 AND organization_id=$2 AND send_cooldown_until>NOW()`, accountID, orgID)
	if err != nil {
		return out, err
	}
	out.Cooldown = tag.RowsAffected() == 1
	return out, tx.Commit(ctx)
}

// ReleaseDomainAuthHolds lifts the domain-authentication holds on a domain whose records now pass, optionally within one workspace.
func (r *emailRepository) ReleaseDomainAuthHolds(ctx context.Context, orgID *uuid.UUID, domain string) ([]uuid.UUID, error) {
	rows, err := r.DB.Query(ctx, `SELECT id FROM email_accounts WHERE send_recovery_hold AND send_recovery_reason='domain_auth'
		AND lower(split_part(email,'@',2))=$1 AND ($2::uuid IS NULL OR organization_id=$2::uuid)`, domain, orgID)
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, err
	}
	var released []uuid.UUID
	for _, id := range ids {
		ok, err := r.releaseOne(ctx, id, []string{"domain_auth"}, SendHoldEvidenceDomainAuth)
		if err != nil {
			return released, err
		}
		if ok {
			released = append(released, id)
		}
	}
	return released, nil
}

func (r *emailRepository) releaseOne(ctx context.Context, id uuid.UUID, reasons []string, evidence string) (bool, error) {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	ok, err := releaseSendHold(ctx, tx, id, reasons, evidence, false)
	if err != nil || !ok {
		return false, err
	}
	return true, tx.Commit(ctx)
}
