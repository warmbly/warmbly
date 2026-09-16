package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/models"
)

// ListWarmupReviewCandidates pages existing messages without loading bodies or changing provider mail.
func (r *uniboxRepository) ListWarmupReviewCandidates(ctx context.Context, afterID uuid.UUID, limit int) ([]models.JobEventNewEmail, error) {
	rows, err := r.db.Query(ctx, `
		SELECT u.user_id, u.id, u.email_id, u.message_id, u.thread_id, u.flags, u.from_addr, u.subject
		FROM unibox_emails u
		WHERE u.id > $1 AND (
		    EXISTS (SELECT 1 FROM cloud_link_mailboxes c WHERE c.email_account_id = u.email_id)
		    OR EXISTS (SELECT 1 FROM warmup_tokens t WHERE t.sender_account_id = u.email_id OR t.recipient_account_id = u.email_id)
		    OR EXISTS (SELECT 1 FROM warmup_received w WHERE w.email_account_id = u.email_id)
		)
		ORDER BY u.id LIMIT $2`, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []models.JobEventNewEmail
	for rows.Next() {
		e := models.JobEventNewEmail{Message: &models.EmailMessageStoreData{}}
		m := e.Message
		if err := rows.Scan(&e.UserID, &m.ID, &m.EmailID, &m.MessageID, &m.ThreadID, &m.Flags, &m.FromAddr, &m.Subject); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// DeferWarmupVerification keeps uncertain arrivals out of Unibox across outages and restarts.
func (r *uniboxRepository) DeferWarmupVerification(ctx context.Context, e *models.JobEventNewEmail) error {
	if e == nil || e.Message == nil || e.Message.ID == uuid.Nil || e.Message.EmailID == uuid.Nil || e.UserID == uuid.Nil {
		return fmt.Errorf("cannot defer warmup verification without message, mailbox and owner IDs")
	}
	payload, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx, `INSERT INTO unibox_pending_emails (id, email_account_id, payload, retry_at)
		VALUES ($1, $2, $3, NOW() + INTERVAL '1 minute') ON CONFLICT (id) DO NOTHING`, e.Message.ID, e.Message.EmailID, payload)
	return err
}

// ClaimPendingWarmupVerification leases rows so failed or interrupted checks remain retryable.
func (r *uniboxRepository) ClaimPendingWarmupVerification(ctx context.Context, limit int) ([]models.JobEventNewEmail, error) {
	rows, err := r.db.Query(ctx, `UPDATE unibox_pending_emails SET retry_at = NOW() + INTERVAL '5 minutes'
		WHERE id IN (SELECT id FROM unibox_pending_emails WHERE retry_at <= NOW()
		ORDER BY retry_at LIMIT $1 FOR UPDATE SKIP LOCKED) RETURNING payload`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []models.JobEventNewEmail
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var e models.JobEventNewEmail
		if err := json.Unmarshal(payload, &e); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// ProcessPendingWarmupVerification serializes verification with provider edits and removals.
func (r *uniboxRepository) ProcessPendingWarmupVerification(ctx context.Context, id uuid.UUID, process func(*models.JobEventNewEmail) error) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var payload []byte
	err = tx.QueryRow(ctx, `SELECT payload FROM unibox_pending_emails WHERE id=$1 FOR UPDATE`, id).Scan(&payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var e models.JobEventNewEmail
	if err := json.Unmarshal(payload, &e); err != nil {
		return err
	}
	if err := process(&e); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM unibox_pending_emails WHERE id=$1`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// UpdatePendingEmail preserves provider changes while an arrival awaits verification.
func (r *uniboxRepository) UpdatePendingEmail(ctx context.Context, userID, id uuid.UUID, update func(*models.EmailMessageStoreData)) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var payload []byte
	err = tx.QueryRow(ctx, `SELECT payload FROM unibox_pending_emails
		WHERE id=$1 AND payload->>'user_id'=$2 FOR UPDATE`, id, userID.String()).Scan(&payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var e models.JobEventNewEmail
	if err := json.Unmarshal(payload, &e); err != nil {
		return err
	}
	update(e.Message)
	payload, err = json.Marshal(e)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE unibox_pending_emails SET payload=$2 WHERE id=$1`, id, payload); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
