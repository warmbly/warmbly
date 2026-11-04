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
	// PutFromWorker stores a report only while the reporting worker owns the mailbox at the reported generation,
	// and returns what the report replaced (nil when the mailbox never reported).
	PutFromWorker(ctx context.Context, userID, emailID, workerID, generation uuid.UUID, state *models.SyncState, before func(context.Context, *SyncStatePrior) error) (bool, *SyncStatePrior, error)
	// Get returns nil, nil when the mailbox has never reported.
	Get(ctx context.Context, emailID uuid.UUID) (*models.SyncState, error)
	// IsOwnConversation reports whether any of the RFC message ids or the
	// provider thread id belongs to something this mailbox sent or already
	// holds: a campaign task, a mapped message, or a stored unibox thread.
	IsOwnConversation(ctx context.Context, userID, emailID uuid.UUID, messageIDs []string, threadID string) (bool, error)
	// ListFolderMessages returns what the platform holds for one mailbox
	// folder in the current UIDVALIDITY generation, keyed by UID. The IMAP
	// drafts reconciliation uses it to find the rows the server expunged.
	ListFolderMessages(ctx context.Context, userID, emailID uuid.UUID, folderPath string, uidValidity uint32) ([]StoredFolderMessage, error)
	// ListProviderFolderMessages returns the newest rows the provider last
	// placed in one of folders, for providers that key messages by id.
	ListProviderFolderMessages(ctx context.Context, userID, emailID uuid.UUID, folders []string, limit int) ([]ProviderFolderMessage, error)
	ListProviderMessages(ctx context.Context, userID, emailID uuid.UUID, after *uuid.UUID, limit int) ([]ProviderFolderMessage, error)
}

func (r *pgEmailSyncStateRepository) ListProviderMessages(ctx context.Context, userID, emailID uuid.UUID, after *uuid.UUID, limit int) ([]ProviderFolderMessage, error) {
	const q = `SELECT id, gmail_id, message_id, provider_folder, internal_date, flags FROM unibox_emails
		WHERE user_id = $1 AND email_id = $2 AND gmail_id <> '' AND ($3::uuid IS NULL OR id > $3)
		ORDER BY id LIMIT $4`
	rows, err := r.db.Query(ctx, q, userID, emailID, after, limit)
	if err != nil {
		return nil, fmt.Errorf("email_sync_state: list provider messages: %w", err)
	}
	defer rows.Close()
	out := make([]ProviderFolderMessage, 0)
	for rows.Next() {
		var m ProviderFolderMessage
		if err := rows.Scan(&m.ID, &m.ProviderID, &m.MessageID, &m.ProviderFolder, &m.InternalDate, &m.Flags); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SyncStatePrior is the stored state a report replaces, read under the report's lock.
type SyncStatePrior struct {
	BackfillStatus models.SyncBackfillStatus
	// ThrottledUntil is nil once the hold has expired.
	ThrottledUntil *time.Time
	LastSyncedAt   *time.Time
}

// PriorOf summarizes a stored state; nil stays nil.
func PriorOf(st *models.SyncState) *SyncStatePrior {
	if st == nil {
		return nil
	}
	return &SyncStatePrior{BackfillStatus: st.BackfillStatus, ThrottledUntil: st.ThrottledUntil, LastSyncedAt: st.LastSyncedAt}
}

type pgEmailSyncStateRepository struct {
	db *db.DB
}

type syncReportKey struct{}

func NewEmailSyncStateRepository(d *db.DB) EmailSyncStateRepository {
	return &pgEmailSyncStateRepository{db: d}
}

func (r *pgEmailSyncStateRepository) Put(ctx context.Context, userID, emailID uuid.UUID, state *models.SyncState) error {
	_, _, err := r.put(ctx, userID, emailID, nil, uuid.Nil, state, nil)
	return err
}

func (r *pgEmailSyncStateRepository) PutFromWorker(ctx context.Context, userID, emailID, workerID, generation uuid.UUID, state *models.SyncState, before func(context.Context, *SyncStatePrior) error) (bool, *SyncStatePrior, error) {
	if workerID == uuid.Nil {
		return false, nil, nil
	}
	return r.put(ctx, userID, emailID, &workerID, generation, state, before)
}

func (r *pgEmailSyncStateRepository) put(ctx context.Context, userID, emailID uuid.UUID, workerID *uuid.UUID, generation uuid.UUID, state *models.SyncState, before func(context.Context, *SyncStatePrior) error) (bool, *SyncStatePrior, error) {
	if state == nil {
		return false, nil, nil
	}
	cursor, err := json.Marshal(state.BackfillCursor)
	if err != nil {
		return false, nil, fmt.Errorf("email_sync_state: cursor: %w", err)
	}
	status := state.BackfillStatus
	if status == "" {
		status = models.SyncBackfillPending
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, nil, fmt.Errorf("email_sync_state: begin: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := lockTransportMailbox(ctx, tx, emailID); err != nil {
		return false, nil, fmt.Errorf("email_sync_state: lock: %w", err)
	}
	var assigned *uuid.UUID
	var current uuid.UUID
	var priorStatus *string
	var priorThrottle, priorSynced *time.Time
	// NO KEY UPDATE holds off reassignment without blocking the mailbox's foreign-key inserts.
	err = tx.QueryRow(ctx, `SELECT a.worker_id, a.transport_generation, s.backfill_status::text, s.throttled_until, s.last_synced_at
		FROM email_accounts a LEFT JOIN email_sync_state s ON s.email_id = a.id
		WHERE a.id=$1 FOR NO KEY UPDATE OF a`, emailID).Scan(&assigned, &current, &priorStatus, &priorThrottle, &priorSynced)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, nil, fmt.Errorf("email_sync_state: owner: %w", err)
	}
	if workerID != nil && (assigned == nil || *assigned != *workerID) {
		return false, nil, nil
	}
	// Reports from releases that predate the incarnation generation carry only the completed-pass one, or none.
	if generation == uuid.Nil {
		generation = state.TransportGeneration
	}
	if generation != uuid.Nil && err == nil && generation != current {
		return false, nil, nil
	}
	var prior *SyncStatePrior
	if priorStatus != nil {
		prior = &SyncStatePrior{BackfillStatus: models.SyncBackfillStatus(*priorStatus), LastSyncedAt: priorSynced}
		if priorThrottle != nil && priorThrottle.After(time.Now()) {
			prior.ThrottledUntil = priorThrottle
		}
	}
	if before != nil {
		if err := before(context.WithValue(ctx, syncReportKey{}, tx), prior); err != nil {
			return false, nil, err
		}
	}
	const q = `
		-- Retain original coverage clocks for replay ordering, allowing at most five seconds of lead.
		WITH coverage AS (
			SELECT $15::timestamptz IS NULL OR ($15 > to_timestamp(0) AND $15 <= now() + interval '5 seconds') AS valid
		), saved AS (
		INSERT INTO email_sync_state (
			email_id, user_id, backfill_status, backfill_cursor, backfill_synced,
			backfill_since, backfill_started_at, backfill_completed_at,
			throttled_until, throttle_reason, deferred, last_synced_at,
			folders_skipped_cap, folders_skipped_conflict, folder_coverage_observed_at, updated_at
		) SELECT $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11,
		    CASE WHEN $12::timestamptz > to_timestamp(0) AND $12 <= now() THEN $12 END,
		    CASE WHEN valid THEN $13 ELSE 0 END, CASE WHEN valid THEN $14 ELSE 0 END,
		    CASE WHEN valid THEN $15::timestamptz END, now()
		FROM coverage
		WHERE true
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
			(folders_skipped_cap, folders_skipped_conflict, folder_coverage_observed_at) = (
				SELECT
					CASE WHEN fresh THEN EXCLUDED.folders_skipped_cap ELSE email_sync_state.folders_skipped_cap END,
					CASE WHEN fresh THEN EXCLUDED.folders_skipped_conflict ELSE email_sync_state.folders_skipped_conflict END,
					CASE WHEN fresh THEN EXCLUDED.folder_coverage_observed_at ELSE email_sync_state.folder_coverage_observed_at END
				FROM (SELECT CASE
					WHEN NOT (SELECT valid FROM coverage) THEN false
					WHEN EXCLUDED.folder_coverage_observed_at IS NOT NULL THEN
						(email_sync_state.folder_coverage_observed_at IS NULL AND email_sync_state.last_synced_at IS NULL)
						OR EXCLUDED.folder_coverage_observed_at > GREATEST(email_sync_state.folder_coverage_observed_at, email_sync_state.last_synced_at)
					WHEN email_sync_state.folder_coverage_observed_at IS NOT NULL THEN
						EXCLUDED.last_synced_at > email_sync_state.folder_coverage_observed_at
						AND (email_sync_state.last_synced_at IS NULL OR EXCLUDED.last_synced_at >= email_sync_state.last_synced_at)
					ELSE EXCLUDED.last_synced_at IS NULL OR email_sync_state.last_synced_at IS NULL OR EXCLUDED.last_synced_at >= email_sync_state.last_synced_at
				END AS fresh) coverage
			),
			last_synced_at = GREATEST(EXCLUDED.last_synced_at, email_sync_state.last_synced_at),
			updated_at = now()
		-- A redelivered report leaves the row, and its dead tuple, alone.
		WHERE (email_sync_state.user_id, email_sync_state.backfill_status, email_sync_state.backfill_cursor, email_sync_state.backfill_synced,
		       email_sync_state.backfill_since, email_sync_state.backfill_started_at, email_sync_state.backfill_completed_at,
		       email_sync_state.throttled_until, email_sync_state.throttle_reason, email_sync_state.deferred, email_sync_state.last_synced_at,
		       email_sync_state.folders_skipped_cap, email_sync_state.folders_skipped_conflict, email_sync_state.folder_coverage_observed_at)
		    IS DISTINCT FROM (EXCLUDED.user_id, EXCLUDED.backfill_status, EXCLUDED.backfill_cursor, EXCLUDED.backfill_synced,
		       EXCLUDED.backfill_since, EXCLUDED.backfill_started_at, EXCLUDED.backfill_completed_at,
		       EXCLUDED.throttled_until, EXCLUDED.throttle_reason, EXCLUDED.deferred, GREATEST(EXCLUDED.last_synced_at, email_sync_state.last_synced_at),
		       EXCLUDED.folders_skipped_cap, EXCLUDED.folders_skipped_conflict, EXCLUDED.folder_coverage_observed_at)
		RETURNING 1)
		-- "Last synced" reads at minute precision, so a busy mailbox does not rewrite its account row every pass.
		UPDATE email_accounts SET last_synced_at = $12 WHERE id = $1 AND $12::timestamptz IS NOT NULL
		    AND (last_synced_at IS NULL OR last_synced_at < $12::timestamptz - interval '1 minute')
		    AND $12::timestamptz > to_timestamp(0) AND $12::timestamptz <= now() AND ($16::uuid IS NULL OR worker_id = $16)
	`
	_, err = tx.Exec(ctx, q,
		emailID, userID, string(status), cursor, state.BackfillSynced,
		state.BackfillSince, state.BackfillStartedAt, state.BackfillCompletedAt,
		state.ThrottledUntil, state.ThrottleReason, state.Deferred, state.LastSyncedAt,
		state.FoldersSkippedCap, state.FoldersSkippedConflict, state.FolderCoverageObservedAt, workerID,
	)
	if err != nil {
		return false, nil, fmt.Errorf("email_sync_state: put: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, nil, fmt.Errorf("email_sync_state: commit: %w", err)
	}
	return true, prior, nil
}

func (r *pgEmailSyncStateRepository) Get(ctx context.Context, emailID uuid.UUID) (*models.SyncState, error) {
	const q = `
		SELECT backfill_status, backfill_cursor, backfill_synced,
		       backfill_since, backfill_started_at, backfill_completed_at,
		       throttled_until, throttle_reason, deferred, last_synced_at,
		       folders_skipped_cap, folders_skipped_conflict, folder_coverage_observed_at
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
		&st.FoldersSkippedCap, &st.FoldersSkippedConflict, &st.FolderCoverageObservedAt,
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

// Empty IDs are filtered before querying, making the partial Message-ID index safe.
const ownConversationSQL = `
		SELECT EXISTS (
			SELECT 1 FROM tasks
			WHERE email_account_id = $2 AND cardinality($3::text[]) > 0 AND message_id <> '' AND message_id = ANY($3)
		) OR EXISTS (
			SELECT 1
			FROM tasks t
			JOIN email_accounts sender ON sender.id = t.email_account_id
			JOIN email_accounts here ON here.id = $2 AND here.organization_id = sender.organization_id
			WHERE cardinality($3::text[]) > 0 AND t.message_id <> '' AND t.message_id = ANY($3)
			  AND t.reply_to <> ''
			  AND lower(t.reply_to) IN (lower(here.email), lower(COALESCE(NULLIF(here.send_as_email, ''), here.email)))
		) OR EXISTS (
			SELECT 1 FROM email_message_map
			WHERE user_id = $1 AND email_id = $2 AND cardinality($3::text[]) > 0 AND message_id = ANY($3)
		) OR EXISTS (
			SELECT 1 FROM unibox_emails
			WHERE user_id = $1 AND email_id = $2 AND $4 <> '' AND thread_id = $4
		)
	`

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
	var own bool
	if err := r.db.QueryRow(ctx, ownConversationSQL, userID, emailID, ids, threadID).Scan(&own); err != nil {
		return false, fmt.Errorf("email_sync_state: own conversation: %w", err)
	}
	return own, nil
}

func (r *pgEmailSyncStateRepository) ListFolderMessages(ctx context.Context, userID, emailID uuid.UUID, folderPath string, uidValidity uint32) ([]StoredFolderMessage, error) {
	const q = `
		SELECT uid, id, message_id
		FROM unibox_emails
		WHERE user_id = $1 AND email_id = $2 AND folder_path = $3 AND mailbox = $4
	`
	rows, err := r.db.Query(ctx, q, userID, emailID, folderPath, uidValidity)
	if err != nil {
		return nil, fmt.Errorf("email_sync_state: list folder messages: %w", err)
	}
	defer rows.Close()

	out := make([]StoredFolderMessage, 0)
	for rows.Next() {
		var m StoredFolderMessage
		if err := rows.Scan(&m.UID, &m.ID, &m.MessageID); err != nil {
			return nil, fmt.Errorf("email_sync_state: scan folder message: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("email_sync_state: list folder messages: %w", err)
	}
	return out, nil
}

func (r *pgEmailSyncStateRepository) ListProviderFolderMessages(ctx context.Context, userID, emailID uuid.UUID, folders []string, limit int) ([]ProviderFolderMessage, error) {
	const q = `
		SELECT id, gmail_id, provider_folder, internal_date
		FROM unibox_emails
		WHERE user_id = $1 AND email_id = $2 AND provider_folder = ANY($3) AND gmail_id <> ''
		ORDER BY internal_date DESC
		LIMIT $4
	`
	rows, err := r.db.Query(ctx, q, userID, emailID, folders, limit)
	if err != nil {
		return nil, fmt.Errorf("email_sync_state: list provider folder messages: %w", err)
	}
	defer rows.Close()

	out := make([]ProviderFolderMessage, 0)
	for rows.Next() {
		var m ProviderFolderMessage
		if err := rows.Scan(&m.ID, &m.ProviderID, &m.ProviderFolder, &m.InternalDate); err != nil {
			return nil, fmt.Errorf("email_sync_state: scan provider folder message: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("email_sync_state: list provider folder messages: %w", err)
	}
	return out, nil
}
