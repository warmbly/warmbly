package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// MailboxPushRepository records the provider push channel each mailbox holds
// and resolves who to wake when one fires.
type MailboxPushRepository interface {
	// PutPushState stores the channel a worker reported; nil removes it.
	PutPushState(ctx context.Context, userID, emailID uuid.UUID, state *models.MailboxPushState) error
	// GetPushState returns nil, nil when the mailbox holds no channel.
	GetPushState(ctx context.Context, emailID uuid.UUID) (*models.MailboxPushState, error)
	// WakeTargets resolves active mailboxes to the live worker holding each,
	// leaving out any whose worker cannot act on SYNC_WAKE.
	WakeTargets(ctx context.Context, emailIDs []uuid.UUID) ([]SyncWakeTarget, error)
	// GmailWakeTargets is WakeTargets for every mailbox at address holding a Gmail watch.
	GmailWakeTargets(ctx context.Context, address string) ([]SyncWakeTarget, error)
}

// SyncWakeTarget is one mailbox and the worker to wake for it.
type SyncWakeTarget struct {
	EmailID  uuid.UUID
	UserID   uuid.UUID
	WorkerID uuid.UUID
}

type pgMailboxPushRepository struct {
	db *db.DB
}

func NewMailboxPushRepository(d *db.DB) MailboxPushRepository {
	return &pgMailboxPushRepository{db: d}
}

func (r *pgMailboxPushRepository) PutPushState(ctx context.Context, userID, emailID uuid.UUID, state *models.MailboxPushState) error {
	if state == nil {
		_, err := r.db.Exec(ctx, `DELETE FROM mailbox_push_channels p USING email_accounts a
			WHERE p.email_account_id = a.id AND a.id = $1 AND a.user_id = $2`, emailID, userID)
		if err != nil {
			return fmt.Errorf("mailbox_push_channels: delete: %w", err)
		}
		return nil
	}
	switch state.Provider {
	case models.MailboxPushGmail, models.MailboxPushGraph:
	default:
		return fmt.Errorf("mailbox_push_channels: unknown provider %q", state.Provider)
	}
	_, err := r.db.Exec(ctx, `INSERT INTO mailbox_push_channels (email_account_id, provider, subscription_id, credential, address, expires_at, updated_at)
		SELECT a.id, $3, NULLIF($4, ''), NULLIF($5, ''), lower(a.email), $6, now() FROM email_accounts a WHERE a.id = $1 AND a.user_id = $2
		ON CONFLICT (email_account_id) DO UPDATE SET provider = EXCLUDED.provider, subscription_id = EXCLUDED.subscription_id,
			credential = EXCLUDED.credential, address = EXCLUDED.address, expires_at = EXCLUDED.expires_at, updated_at = now()`,
		emailID, userID, string(state.Provider), state.SubscriptionID, state.Credential, state.ExpiresAt)
	if err != nil {
		return fmt.Errorf("mailbox_push_channels: put: %w", err)
	}
	return nil
}

func (r *pgMailboxPushRepository) GetPushState(ctx context.Context, emailID uuid.UUID) (*models.MailboxPushState, error) {
	var s models.MailboxPushState
	var provider string
	var sub, cred *string
	err := r.db.QueryRow(ctx, `SELECT provider, subscription_id, credential, expires_at FROM mailbox_push_channels WHERE email_account_id = $1`, emailID).
		Scan(&provider, &sub, &cred, &s.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("mailbox_push_channels: get: %w", err)
	}
	s.Provider = models.MailboxPushProvider(provider)
	if sub != nil {
		s.SubscriptionID = *sub
	}
	if cred != nil {
		s.Credential = *cred
	}
	return &s, nil
}

const wakeTargetSelect = `SELECT a.id, a.user_id, a.worker_id FROM email_accounts a
	JOIN fleet_nodes n ON n.id = a.worker_id
	WHERE a.status = 'active' AND n.role = 'worker' AND n.active AND n.sync_wake_protocol >= 1
	  AND n.last_seen_at > now() - $2::interval`

func (r *pgMailboxPushRepository) WakeTargets(ctx context.Context, emailIDs []uuid.UUID) ([]SyncWakeTarget, error) {
	if len(emailIDs) == 0 {
		return nil, nil
	}
	return r.wakeTargets(ctx, wakeTargetSelect+` AND a.id = ANY($1)`, emailIDs, WorkerLivenessWindow)
}

func (r *pgMailboxPushRepository) GmailWakeTargets(ctx context.Context, address string) ([]SyncWakeTarget, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return nil, nil
	}
	return r.wakeTargets(ctx, wakeTargetSelect+` AND a.provider = 'gmail' AND a.id IN
		(SELECT p.email_account_id FROM mailbox_push_channels p WHERE p.provider = 'gmail' AND p.address = lower($1))`,
		address, WorkerLivenessWindow)
}

func (r *pgMailboxPushRepository) wakeTargets(ctx context.Context, q string, args ...any) ([]SyncWakeTarget, error) {
	rows, err := r.db.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("mailbox push: wake targets: %w", err)
	}
	defer rows.Close()
	var out []SyncWakeTarget
	for rows.Next() {
		var t SyncWakeTarget
		if err := rows.Scan(&t.EmailID, &t.UserID, &t.WorkerID); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
