package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
)

type WarmupRecoveryRepository interface {
	IsKnown(ctx context.Context, accountID uuid.UUID, token string, messageIDs []string) (bool, error)
	RememberMessage(ctx context.Context, accountID uuid.UUID, messageID string, receivedAt time.Time) error
	EnqueueFiling(ctx context.Context, action models.WarmupEmailAction) (uuid.UUID, error)
	ClaimFilings(ctx context.Context, limit int) ([]models.WarmupEmailAction, error)
	CompleteFiling(ctx context.Context, accountID, filingID uuid.UUID) error
	PurgeExpiredIdentifiers(ctx context.Context) error
}

type warmupRecoveryRepository struct{ db *pgxpool.Pool }

type WarmupDisconnectSource interface {
	WarmupDisconnectMessageIDs(ctx context.Context, accountID uuid.UUID, limit int) ([]string, error)
}

func (r *emailRepository) WarmupDisconnectMessageIDs(ctx context.Context, accountID uuid.UUID, limit int) ([]string, error) {
	rows, err := r.DB.Pool.Query(ctx, `SELECT message_id FROM (
		SELECT message_id, created_at FROM warmup_received WHERE retired_at IS NULL AND (email_account_id = $1 OR sender_account_id = $1)
		UNION ALL SELECT sent_message_id, created_at FROM warmup_tokens WHERE sent_retired_at IS NULL AND (sender_account_id = $1 OR recipient_account_id = $1)
		UNION ALL SELECT message_id, created_at FROM warmup_thread_messages WHERE email_account_id = $1
		UNION ALL SELECT payload->>'rfc_message_id', created_at FROM warmup_pending_filings WHERE email_account_id = $1
	) messages WHERE btrim(message_id, '<> ') <> '' GROUP BY message_id ORDER BY MAX(created_at) DESC LIMIT $2`, accountID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func NewWarmupRecoveryRepository(db *pgxpool.Pool) WarmupRecoveryRepository {
	return &warmupRecoveryRepository{db: db}
}

func (r *warmupRecoveryRepository) RememberMessage(ctx context.Context, accountID uuid.UUID, messageID string, receivedAt time.Time) error {
	ids := normalizeMessageIDs([]string{messageID})
	if len(ids) == 0 {
		return nil
	}
	if receivedAt.IsZero() || receivedAt.After(time.Now()) {
		receivedAt = time.Now()
	}
	_, err := r.db.Exec(ctx, `SELECT remember_warmup_identifier($1, $2, $3)`, accountID, "message:"+ids[0], receivedAt)
	return err
}

func (r *warmupRecoveryRepository) IsKnown(ctx context.Context, accountID uuid.UUID, token string, messageIDs []string) (bool, error) {
	var identifiers []string
	if id, err := uuid.Parse(token); err == nil {
		identifiers = append(identifiers, "token:"+id.String())
	}
	for _, id := range normalizeMessageIDs(messageIDs) {
		identifiers = append(identifiers, "message:"+id)
	}
	if len(identifiers) == 0 {
		return false, nil
	}
	var known bool
	err := r.db.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM email_accounts ea
		JOIN warmup_recovery_identifiers wi ON wi.organization_id = ea.organization_id
		  AND wi.mailbox_hash = sha256(convert_to(lower(btrim(ea.email)), 'UTF8'))
		WHERE ea.id = $1 AND wi.expires_at > NOW()
		  AND wi.identifier_hash IN (SELECT sha256(convert_to(identifier, 'UTF8')) FROM unnest($2::text[]) identifier)
	)`, accountID, identifiers).Scan(&known)
	return known, err
}

func (r *warmupRecoveryRepository) EnqueueFiling(ctx context.Context, action models.WarmupEmailAction) (uuid.UUID, error) {
	key := strings.Trim(action.RFCMessageID, "<> \t\r\n")
	if key == "" {
		key = action.InternalID
	}
	if key == "" {
		return uuid.Nil, fmt.Errorf("warmup filing has no stable message identifier")
	}
	payload, err := json.Marshal(action)
	if err != nil {
		return uuid.Nil, err
	}
	var id uuid.UUID
	err = r.db.QueryRow(ctx, `INSERT INTO warmup_pending_filings (email_account_id, message_key, payload)
		VALUES ($1, $2, $3) ON CONFLICT (email_account_id, message_key)
		DO UPDATE SET payload = EXCLUDED.payload RETURNING id`, action.EmailID, key, payload).Scan(&id)
	return id, err
}

func (r *warmupRecoveryRepository) ClaimFilings(ctx context.Context, limit int) ([]models.WarmupEmailAction, error) {
	rows, err := r.db.Query(ctx, `UPDATE warmup_pending_filings SET next_attempt_at = NOW() + CASE
		  WHEN created_at > NOW() - INTERVAL '20 minutes' THEN INTERVAL '5 minutes'
		  WHEN created_at > NOW() - INTERVAL '1 hour' THEN INTERVAL '15 minutes'
		  ELSE INTERVAL '30 minutes' END
		WHERE id IN (SELECT f.id FROM warmup_pending_filings f
		  JOIN email_accounts a ON a.id = f.email_account_id
		  WHERE f.next_attempt_at <= NOW() AND (f.provider_retry_at IS NULL OR f.provider_retry_at <= NOW()) AND a.worker_id IS NOT NULL
		  ORDER BY f.next_attempt_at LIMIT $1 FOR UPDATE OF f SKIP LOCKED)
		RETURNING id, payload`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var actions []models.WarmupEmailAction
	for rows.Next() {
		var id uuid.UUID
		var payload []byte
		if err := rows.Scan(&id, &payload); err != nil {
			return nil, err
		}
		var action models.WarmupEmailAction
		if err := json.Unmarshal(payload, &action); err != nil {
			return nil, err
		}
		action.FilingID = id.String()
		actions = append(actions, action)
	}
	return actions, rows.Err()
}

func (r *warmupRecoveryRepository) CompleteFiling(ctx context.Context, accountID, filingID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM warmup_pending_filings WHERE id = $1 AND email_account_id = $2`, filingID, accountID)
	return err
}

func (r *warmupRecoveryRepository) PurgeExpiredIdentifiers(ctx context.Context) error {
	_, err := r.db.Exec(ctx, `DELETE FROM warmup_recovery_identifiers WHERE (organization_id, mailbox_hash, identifier_hash) IN (
		SELECT organization_id, mailbox_hash, identifier_hash FROM warmup_recovery_identifiers
		WHERE expires_at <= NOW() ORDER BY expires_at LIMIT 2000)`)
	return err
}
