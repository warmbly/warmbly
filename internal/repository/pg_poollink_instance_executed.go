package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// instanceExecutedSQL holds for a legacy mailbox whose credential stayed on its linked instance.
func instanceExecutedSQL(account string) string {
	return `EXISTS (SELECT 1 FROM pool_link_mailboxes plx WHERE plx.email_account_id = ` + account + ` AND plx.executor = 'instance')`
}

// actionRoutableSQL holds when a mailbox has a worker to carry its warmup actions.
func actionRoutableSQL(ea string) string {
	return `(` + ea + `.worker_id IS NOT NULL AND NOT ` + instanceExecutedSQL(ea+".id") + `)`
}

// ErrPoolLinkNotInstanceExecuted is a conversion of a mailbox the link does not hold.
var ErrPoolLinkNotInstanceExecuted = errors.New("mailbox is not enrolled by this instance")

// PoolLinkConversionRepository hands a legacy instance-run mailbox back to Cloud.
type PoolLinkConversionRepository interface {
	ConvertToCloud(ctx context.Context, instanceID, remoteID uuid.UUID) error
}

// ConvertToCloud returns a legacy instance-run mailbox to Cloud; its credential was written first.
func (r *poolLinkRepository) ConvertToCloud(ctx context.Context, instanceID, remoteID uuid.UUID) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var account uuid.UUID
	err = tx.QueryRow(ctx, `UPDATE pool_link_mailboxes SET executor = 'cloud' WHERE instance_id = $1 AND remote_id = $2 RETURNING email_account_id`, instanceID, remoteID).Scan(&account)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrPoolLinkNotInstanceExecuted
	}
	if err != nil {
		return err
	}
	if err := cancelInstanceCommandsTx(ctx, tx, account); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// cancelInstanceCommandsTx closes retired-protocol commands, leased ones included; only never-started sends are released.
func cancelInstanceCommandsTx(ctx context.Context, tx pgx.Tx, account uuid.UUID) error {
	rows, err := tx.Query(ctx, `UPDATE pool_link_commands SET status = 'cancelled'
		WHERE email_account_id = $1 AND status IN ('pending', 'leased') RETURNING task_id, dispatch_nonce`, account)
	if err != nil {
		return err
	}
	type pendingSend struct{ task, nonce uuid.UUID }
	var sends []pendingSend
	for rows.Next() {
		var task, nonce *uuid.UUID
		if err := rows.Scan(&task, &nonce); err != nil {
			rows.Close()
			return err
		}
		if task != nil && nonce != nil {
			sends = append(sends, pendingSend{*task, *nonce})
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, s := range sends {
		if err := lockSend(ctx, tx, s.task, account); err != nil {
			return err
		}
		if err := cancelInstanceSendTx(ctx, tx, s.task, account, s.nonce); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `DELETE FROM pool_link_reporter_trust WHERE email_account_id = $1`, account)
	return err
}

func cancelInstanceSendTx(ctx context.Context, tx pgx.Tx, task, mailbox, nonce uuid.UUID) error {
	tag, err := tx.Exec(ctx, `UPDATE tasks SET send_released_at=NOW(),send_result_state='failed',send_result_applied_at=NOW(),status='cancelled'
		WHERE id=$1 AND email_account_id=$2 AND task_type='warmup' AND send_executor_worker IS NULL AND send_executor_nonce=$3
		AND send_executor_started_at IS NULL AND send_released_at IS NULL
		AND EXISTS(SELECT 1 FROM warmup_tasks w WHERE w.task_id=tasks.id AND w.dispatch_nonce=$3
		AND w.dispatch_worker_id IS NULL AND w.dispatch_started_at IS NULL AND w.dispatch_result IS NULL)`, task, mailbox, nonce)
	if err != nil || tag.RowsAffected() == 0 {
		return err
	}
	if err = refundWarmupCharge(ctx, tx, task, mailbox); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM warmup_tokens WHERE task_id=$1`, task); err != nil {
		return err
	}
	return refreshUnknownSendHold(ctx, tx, mailbox, task)
}

// InstanceExecuted reports whether a linked instance, not a worker, runs a mailbox.
func (r *emailRepository) InstanceExecuted(ctx context.Context, id uuid.UUID) (bool, error) {
	var yes bool
	err := r.DB.QueryRow(ctx, `SELECT `+instanceExecutedSQL("$1::uuid"), id).Scan(&yes)
	return yes, err
}

// RestoreMailboxCredential writes the credential of a mailbox returning to Cloud execution, recreating the row its handover deleted.
func (r *emailRepository) RestoreMailboxCredential(ctx context.Context, id uuid.UUID, oauth *models.PoolLinkOAuthCredential, creds *models.SmtpImap) error {
	orgID, err := r.mailboxOrganization(ctx, id)
	if err != nil {
		return err
	}
	switch {
	case oauth != nil && oauth.RefreshToken != "":
		sealed, err := r.sealCredentials(ctx, orgID, oauth.AccessToken, oauth.RefreshToken)
		if err != nil {
			return err
		}
		_, err = r.DB.Exec(ctx, `INSERT INTO email_accounts_oauth (email_account_id, access_token, refresh_token, expires_at) VALUES ($1, $2, $3, $4)
			ON CONFLICT (email_account_id) DO UPDATE SET access_token = EXCLUDED.access_token, refresh_token = EXCLUDED.refresh_token, expires_at = EXCLUDED.expires_at, granted_scopes = NULL`,
			id, sealed[0], sealed[1], oauth.ExpiresAt)
		return err
	case creds != nil && creds.SMTP != nil && creds.IMAP != nil:
		sealed, err := r.sealCredentials(ctx, orgID, creds.SMTP.Host, creds.SMTP.Username, creds.SMTP.Password, creds.IMAP.Host, creds.IMAP.Username, creds.IMAP.Password)
		if err != nil {
			return err
		}
		_, err = r.DB.Exec(ctx, `INSERT INTO email_accounts_smtp_imap (email_account_id, smtp_host, smtp_port, smtp_user, smtp_password, smtp_security,
			imap_host, imap_port, imap_user, imap_password, imap_security, smtp_tls_unverified, smtp_tls_pin, imap_tls_unverified, imap_tls_pin)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
			ON CONFLICT (email_account_id) DO UPDATE SET smtp_host = EXCLUDED.smtp_host, smtp_port = EXCLUDED.smtp_port, smtp_user = EXCLUDED.smtp_user,
			  smtp_password = EXCLUDED.smtp_password, smtp_security = EXCLUDED.smtp_security, imap_host = EXCLUDED.imap_host, imap_port = EXCLUDED.imap_port,
			  imap_user = EXCLUDED.imap_user, imap_password = EXCLUDED.imap_password, imap_security = EXCLUDED.imap_security,
			  smtp_tls_unverified = EXCLUDED.smtp_tls_unverified, smtp_tls_pin = EXCLUDED.smtp_tls_pin,
			  imap_tls_unverified = EXCLUDED.imap_tls_unverified, imap_tls_pin = EXCLUDED.imap_tls_pin`,
			id, sealed[0], creds.SMTP.Port, sealed[1], sealed[2], models.ResolveSMTPSecurity(creds.SMTP.Security, creds.SMTP.Port),
			sealed[3], creds.IMAP.Port, sealed[4], sealed[5], models.ResolveIMAPSecurity(creds.IMAP.Security, creds.IMAP.Port),
			creds.SMTP.TLSUnverified, creds.SMTP.StoredTLSPin(), creds.IMAP.TLSUnverified, creds.IMAP.StoredTLSPin())
		return err
	}
	return errx.ErrInvalid
}

// trustedReporterSQL keeps placement reports from a reporter judged untrusted under the retired protocol out of the window.
func trustedReporterSQL(account string) string {
	return `NOT (` + instanceExecutedSQL(account) + ` AND EXISTS (SELECT 1 FROM pool_link_reporter_trust prt WHERE prt.email_account_id = ` + account + ` AND prt.trust_state = 'untrusted'))`
}
