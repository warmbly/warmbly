package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// EmailSyncStateRepository persists what the platform knows about a mailbox's
// sync: backfill progress and cursor, fair-use throttle, last-synced time.
// The worker relays state over the bus; only the consumer and the backend
// (loader, API) touch this table.
type EmailSyncStateRepository interface {
	// Put stores the full state and stamps email_accounts.last_synced_at, the
	// column the admin and dashboard "Last synced" surfaces read.
	Put(ctx context.Context, userID, emailID uuid.UUID, state *models.SyncState) error
	// Get returns nil, nil when the mailbox has never reported.
	Get(ctx context.Context, emailID uuid.UUID) (*models.SyncState, error)
	// IsOwnConversation reports whether any of the RFC message ids or the
	// provider thread id belongs to something this mailbox sent or already
	// holds: a campaign task, a mapped message, or a stored unibox thread.
	IsOwnConversation(ctx context.Context, userID, emailID uuid.UUID, messageIDs []string, threadID string) (bool, error)
}

type pgEmailSyncStateRepository struct {
	db *db.DB
}

func NewEmailSyncStateRepository(d *db.DB) EmailSyncStateRepository {
	return &pgEmailSyncStateRepository{db: d}
}

func (r *pgEmailSyncStateRepository) Put(ctx context.Context, userID, emailID uuid.UUID, state *models.SyncState) error {
	if state == nil {
		return nil
	}
	cursor, err := json.Marshal(state.BackfillCursor)
	if err != nil {
		return fmt.Errorf("email_sync_state: cursor: %w", err)
	}
	status := state.BackfillStatus
	if status == "" {
		status = models.SyncBackfillPending
	}
	const q = `
		INSERT INTO email_sync_state (
			email_id, user_id, backfill_status, backfill_cursor, backfill_synced,
			backfill_since, backfill_started_at, backfill_completed_at,
			throttled_until, throttle_reason, deferred, last_synced_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, now())
		ON CONFLICT (email_id) DO UPDATE SET
			user_id = EXCLUDED.user_id,
			backfill_status = EXCLUDED.backfill_status,
			backfill_cursor = EXCLUDED.backfill_cursor,
			backfill_synced = EXCLUDED.backfill_synced,
			backfill_since = EXCLUDED.backfill_since,
			backfill_started_at = EXCLUDED.backfill_started_at,
			backfill_completed_at = EXCLUDED.backfill_completed_at,
			throttled_until = EXCLUDED.throttled_until,
			throttle_reason = EXCLUDED.throttle_reason,
			deferred = EXCLUDED.deferred,
			last_synced_at = COALESCE(EXCLUDED.last_synced_at, email_sync_state.last_synced_at),
			updated_at = now()
	`
	if _, err := r.db.Exec(ctx, q,
		emailID, userID, string(status), cursor, state.BackfillSynced,
		state.BackfillSince, state.BackfillStartedAt, state.BackfillCompletedAt,
		state.ThrottledUntil, state.ThrottleReason, state.Deferred, state.LastSyncedAt,
	); err != nil {
		return fmt.Errorf("email_sync_state: put: %w", err)
	}
	if state.LastSyncedAt != nil {
		const touch = `UPDATE email_accounts SET last_synced_at = $2 WHERE id = $1 AND (last_synced_at IS NULL OR last_synced_at < $2)`
		if _, err := r.db.Exec(ctx, touch, emailID, *state.LastSyncedAt); err != nil {
			return fmt.Errorf("email_sync_state: touch last_synced_at: %w", err)
		}
	}
	return nil
}

func (r *pgEmailSyncStateRepository) Get(ctx context.Context, emailID uuid.UUID) (*models.SyncState, error) {
	const q = `
		SELECT backfill_status, backfill_cursor, backfill_synced,
		       backfill_since, backfill_started_at, backfill_completed_at,
		       throttled_until, throttle_reason, deferred, last_synced_at
		FROM email_sync_state
		WHERE email_id = $1
	`
	var (
		st     models.SyncState
		status string
		cursor []byte
	)
	err := r.db.QueryRow(ctx, q, emailID).Scan(
		&status, &cursor, &st.BackfillSynced,
		&st.BackfillSince, &st.BackfillStartedAt, &st.BackfillCompletedAt,
		&st.ThrottledUntil, &st.ThrottleReason, &st.Deferred, &st.LastSyncedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("email_sync_state: get: %w", err)
	}
	st.BackfillStatus = models.SyncBackfillStatus(status)
	if len(cursor) > 0 {
		if uerr := json.Unmarshal(cursor, &st.BackfillCursor); uerr != nil {
			return nil, fmt.Errorf("email_sync_state: cursor: %w", uerr)
		}
	}
	// A throttle that already expired is not a throttle; a stale row must
	// not make a reassigned mailbox start out paused.
	if st.ThrottledUntil != nil && !st.ThrottledUntil.After(time.Now()) {
		st.ThrottledUntil = nil
		st.ThrottleReason = ""
	}
	return &st, nil
}

func (r *pgEmailSyncStateRepository) IsOwnConversation(ctx context.Context, userID, emailID uuid.UUID, messageIDs []string, threadID string) (bool, error) {
	ids := make([]string, 0, len(messageIDs))
	for _, id := range messageIDs {
		if id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 && threadID == "" {
		return false, nil
	}
	// Three sources, cheapest first: campaign and reply sends record their
	// Message-ID on tasks; IMAP maps sent-folder mail by RFC id; Gmail and
	// Graph key the map by provider id, so for them the stored unibox thread
	// is what links a reply back to the mailbox's own message.
	const q = `
		SELECT EXISTS (
			SELECT 1 FROM tasks
			WHERE email_account_id = $2 AND cardinality($3::text[]) > 0 AND message_id = ANY($3)
		) OR EXISTS (
			SELECT 1 FROM email_message_map
			WHERE user_id = $1 AND email_id = $2 AND cardinality($3::text[]) > 0 AND message_id = ANY($3)
		) OR EXISTS (
			SELECT 1 FROM unibox_emails
			WHERE user_id = $1 AND email_id = $2 AND $4 <> '' AND thread_id = $4
		)
	`
	var own bool
	if err := r.db.QueryRow(ctx, q, userID, emailID, ids, threadID).Scan(&own); err != nil {
		return false, fmt.Errorf("email_sync_state: own conversation: %w", err)
	}
	return own, nil
}
