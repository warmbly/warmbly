package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// A dedicated connection holds the lock while intents commit independently before network calls.
func (r *cloudLinkRepository) WithReconciliationLock(ctx context.Context, fn func() error) error {
	conn, err := pgx.ConnectConfig(ctx, r.db.Config().ConnConfig)
	if err != nil {
		return err
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = conn.Close(closeCtx)
	}()
	if _, err = conn.Exec(ctx, `SELECT pg_advisory_lock(266, 1)`); err != nil {
		return err
	}
	return fn()
}

func (r *cloudLinkRepository) SetDisconnectPending(ctx context.Context, instanceID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `UPDATE cloud_link SET disconnect_pending = true WHERE instance_id = $1`, instanceID)
	return err
}

func (r *cloudLinkRepository) BeginEnrollment(ctx context.Context, accountID, remoteID, instanceID uuid.UUID) error {
	tag, err := r.db.Exec(ctx, `INSERT INTO cloud_link_mailboxes (email_account_id, remote_id, instance_id, enrollment_state)
 SELECT ea.id, $2, l.instance_id, 'pending_enroll' FROM email_accounts ea JOIN cloud_link l
 ON (l.organization_id = ea.organization_id OR l.organization_id IS NULL)
 WHERE ea.id = $1 AND l.instance_id = $3 AND NOT l.disconnect_pending
 ON CONFLICT (email_account_id) DO UPDATE
 SET enrollment_state = 'pending_enroll'
 WHERE cloud_link_mailboxes.enrollment_state <> 'pending_remove' AND NOT cloud_link_mailboxes.managed
 AND cloud_link_mailboxes.instance_id = EXCLUDED.instance_id`, accountID, remoteID, instanceID)
	if err == nil && tag.RowsAffected() != 1 {
		return errors.New("cloud enrollment removal pending")
	}
	return err
}

func (r *cloudLinkRepository) BeginRemoval(ctx context.Context, accountID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `UPDATE cloud_link_mailboxes SET enrollment_state = 'pending_remove' WHERE email_account_id = $1`, accountID)
	return err
}

func (r *cloudLinkRepository) InvalidateStanding(ctx context.Context, accountID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `UPDATE cloud_link_mailboxes SET standing_observed_at = NULL WHERE email_account_id = $1`, accountID)
	return err
}
