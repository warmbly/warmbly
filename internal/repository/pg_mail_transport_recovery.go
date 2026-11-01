package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

const MailTransportLockNamespace int64 = 285

func lockTransportMailbox(ctx context.Context, tx pgx.Tx, accountID uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,$2))`, accountID.String(), MailTransportLockNamespace)
	return err
}

func (r *emailAccountErrorRepository) RecordTransportSuccess(ctx context.Context, accountID uuid.UUID, operation models.MailTransportOperation, at time.Time, taskID *uuid.UUID, generation uuid.UUID) *errx.Error {
	if generation == uuid.Nil || !operation.Known() || !models.ValidMailTransportTime(at, time.Now()) {
		return nil
	}
	if operation == models.MailTransportSend && taskID == nil || operation == models.MailTransportSync && taskID != nil {
		return nil
	}
	tx, err := beginResultTx(ctx, r.DB)
	if err != nil {
		return errx.InternalError()
	}
	defer tx.Rollback(ctx)
	if err := lockTransportMailbox(ctx, tx, accountID); err != nil {
		return errx.InternalError()
	}
	var current uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT transport_generation FROM email_accounts WHERE id=$1 AND transport_generation=$2 FOR UPDATE`, accountID, generation).Scan(&current); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return errx.InternalError()
	}
	_, err = tx.Exec(ctx, `INSERT INTO email_account_transport_recovery AS existing(email_account_id,operation,recovered_at,source_task_id)
	    SELECT id,$2,$3,$4 FROM email_accounts WHERE id=$1 AND transport_generation=$5 AND $3::timestamptz <= NOW()
	        AND ($2='sync' OR EXISTS(SELECT 1 FROM tasks WHERE id=$4 AND email_account_id=$1 AND send_result_state='sent'))
	    ON CONFLICT(email_account_id,operation) DO UPDATE
	        SET recovered_at=GREATEST(existing.recovered_at,EXCLUDED.recovered_at),
	        source_task_id=CASE WHEN EXCLUDED.recovered_at >= existing.recovered_at THEN EXCLUDED.source_task_id ELSE existing.source_task_id END`, accountID, operation, at.UTC(), taskID, generation)
	if err != nil {
		return errx.InternalError()
	}
	_, err = tx.Exec(ctx, `UPDATE email_account_errors report SET resolved_at=recovery.recovered_at,resolved_by=$2 || ' recovered'
	    FROM email_account_transport_recovery recovery, email_accounts account
	    WHERE report.email_account_id=$1 AND report.operation=$2 AND report.resolved_at IS NULL
	        AND report.observation_source IN ('worker','receipt')
	        AND account.id=report.email_account_id AND account.transport_generation=$3
	        AND recovery.email_account_id=report.email_account_id AND recovery.operation=report.operation
	        AND report.created_at < recovery.recovered_at - INTERVAL '1 second'
	        AND report.error_code IN ('SERVER_UNREACHABLE','CONNECTION_LOST','RESOURCE_NOT_FOUND','IMAP_UNKNOWN','SENDING_TOO_FAST','QUOTA_EXCEEDED')`, accountID, operation, generation)
	if err != nil {
		return errx.InternalError()
	}
	// Unclassified failures need later success from both transports, not a guessed operation.
	_, err = tx.Exec(ctx, `UPDATE email_account_errors report
	    SET resolved_at=GREATEST(sync.recovered_at,send.recovered_at),resolved_by='unknown recovered'
	    FROM email_account_transport_recovery sync, email_account_transport_recovery send
	    WHERE report.email_account_id=$1 AND (report.operation='unknown' OR report.observation_source='unknown') AND report.resolved_at IS NULL
	        AND sync.email_account_id=report.email_account_id AND sync.operation='sync'
	        AND send.email_account_id=report.email_account_id AND send.operation='send'
	        AND report.created_at > to_timestamp(0)
	        AND report.created_at < LEAST(sync.recovered_at,send.recovered_at) - INTERVAL '1 second'
	        AND report.error_code IN ('SERVER_UNREACHABLE','CONNECTION_LOST','RESOURCE_NOT_FOUND','IMAP_UNKNOWN','SENDING_TOO_FAST','QUOTA_EXCEEDED')`, accountID)
	if err != nil {
		return errx.InternalError()
	}
	if err := tx.Commit(ctx); err != nil {
		return errx.InternalError()
	}
	return nil
}

// LastTransportSuccess returns each mailbox's latest proven sync and send.
func (r *emailAccountErrorRepository) LastTransportSuccess(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]map[models.MailTransportOperation]time.Time, *errx.Error) {
	out := make(map[uuid.UUID]map[models.MailTransportOperation]time.Time, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	query := `SELECT email_account_id, operation, recovered_at FROM email_account_transport_recovery WHERE email_account_id = ANY($1::uuid[])`
	rows, err := r.DB.Query(ctx, query, ids)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var op models.MailTransportOperation
		var at time.Time
		if err := rows.Scan(&id, &op, &at); err != nil {
			return nil, errx.InternalError()
		}
		if out[id] == nil {
			out[id] = map[models.MailTransportOperation]time.Time{}
		}
		out[id][op] = at
	}
	if rows.Err() != nil {
		return nil, errx.InternalError()
	}
	return out, nil
}

// mailTransientBlipSQL is models.MailTransientBlip for an email_account_errors row aliased as alias.
func mailTransientBlipSQL(alias string) string {
	return fmt.Sprintf(`(%[1]s.severity = 'WARNING' AND %[1]s.error_code IN ('%[2]s') AND NOT EXISTS (
		SELECT 1 FROM (VALUES ('sync'), ('send')) need(op)
		WHERE (need.op = %[1]s.operation OR %[1]s.operation NOT IN ('sync', 'send') OR %[1]s.observation_source = 'unknown')
		  AND NOT EXISTS (
			SELECT 1 FROM email_account_transport_recovery blip_rec
			WHERE blip_rec.email_account_id = %[1]s.email_account_id AND blip_rec.operation = need.op
			  AND blip_rec.recovered_at >= NOW() - INTERVAL '%[3]d seconds')))`,
		alias, strings.Join(models.MailTransientErrorCodes, "', '"), int(models.MailTransientErrorGrace/time.Second))
}
