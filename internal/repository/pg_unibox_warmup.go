package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/models"
)

// ListWarmupReviewCandidates pages existing messages without loading bodies or changing provider mail.
func (r *uniboxRepository) ListWarmupReviewCandidates(ctx context.Context, afterID uuid.UUID, limit int) ([]models.JobEventNewEmail, error) {
	rows, err := r.db.Query(ctx, `
		SELECT u.user_id, u.id, u.email_id, u.message_id, u.thread_id, u.flags, u.from_addr, u.subject,
		       u.gmail_id, u.uid, u.mailbox, u.folder_path, u.in_reply_to
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
		// The provider locators come back too: a historical leak is filed by
		// the same worker action as a fresh arrival, and the Gmail path acts on
		// gmail_id rather than the RFC id.
		if err := rows.Scan(&e.UserID, &m.ID, &m.EmailID, &m.MessageID, &m.ThreadID, &m.Flags, &m.FromAddr, &m.Subject,
			&m.GmailID, &m.UID, &m.Mailbox, &m.FolderPath, &m.InReplyTo); err != nil {
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
	_, err = r.db.Exec(ctx, `INSERT INTO unibox_pending_emails (id, email_account_id, payload, user_id, retry_at)
		VALUES ($1, $2, $3, $4, NOW() + INTERVAL '1 minute') ON CONFLICT (id) DO NOTHING`, e.Message.ID, e.Message.EmailID, payload, e.UserID)
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
func (r *uniboxRepository) UpdatePendingEmail(ctx context.Context, userID, id uuid.UUID, update func(*models.EmailMessageStoreData)) (bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var payload []byte
	err = tx.QueryRow(ctx, `SELECT payload FROM unibox_pending_emails
		WHERE id=$1 AND user_id=$2 FOR UPDATE`, id, userID).Scan(&payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var e models.JobEventNewEmail
	if err := json.Unmarshal(payload, &e); err != nil {
		return false, err
	}
	update(e.Message)
	payload, err = json.Marshal(e)
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE unibox_pending_emails SET payload=$2 WHERE id=$1`, id, payload); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

// ListUnprocessedCampaignReplies feeds the repair sweep in the consumer. A
// reply that reply processing refused before claiming (an address check that
// failed, the automation switch) has campaign_reply_processed_at NULL, so
// this is exactly the set that can still be attributed; everything already
// decided is excluded by that column. Bounded to `since` because older mail
// was processed by the code of its day.
func (r *uniboxRepository) ListUnprocessedCampaignReplies(ctx context.Context, since time.Time, afterID uuid.UUID, limit int) ([]models.JobEventNewEmail, error) {
	// The sender in whichever form the sync stored it: "Name <addr>",
	// "Name (addr)" or bare.
	const bareFrom = `lower(COALESCE(
		(regexp_match(COALESCE(u.from_addr[1], ''), '<([^<>]+)>\s*$'))[1],
		(regexp_match(COALESCE(u.from_addr[1], ''), '\(([^()]+)\)\s*$'))[1],
		u.from_addr[1]))`
	rows, err := r.db.Query(ctx, `
		SELECT u.user_id, u.id, u.email_id, u.message_id, u.thread_id, u.flags, u.from_addr, u.to_addr, u.cc, u.bcc,
		       u.reply_to, u.in_reply_to, u.subject, u.snippet, u.body_text, u.folder, u.provider_folder,
		       u.gmail_id, u.uid, u.mailbox, u.folder_path, u.internal_date
		FROM unibox_emails u
		WHERE u.id > $1
		  AND u.created_at >= $2
		  AND u.campaign_reply_processed_at IS NULL
		  AND u.folder NOT IN ('sent', 'drafts')
		  AND u.provider_folder NOT IN ('sent', 'drafts')
		  AND (
		    EXISTS (
		      SELECT 1 FROM tasks t
		      WHERE t.task_type = 'campaign'
		        AND btrim(t.message_id, '<>') = ANY(ARRAY(SELECT btrim(x, '<>') FROM unnest(u.in_reply_to) AS x))
		    )
		    OR EXISTS (
		      SELECT 1 FROM email_accounts ea
		      JOIN contacts co ON co.organization_id = ea.organization_id
		      WHERE ea.id = u.email_id AND lower(co.email) = `+bareFrom+`
		    )
		  )
		ORDER BY u.id LIMIT $3`, afterID, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []models.JobEventNewEmail
	for rows.Next() {
		e := models.JobEventNewEmail{Message: &models.EmailMessageStoreData{}}
		m := e.Message
		if err := rows.Scan(&e.UserID, &m.ID, &m.EmailID, &m.MessageID, &m.ThreadID, &m.Flags, &m.FromAddr, &m.ToAddr, &m.CC, &m.BCC,
			&m.ReplyTo, &m.InReplyTo, &m.Subject, &m.Snippet, &m.BodyText, &m.Folder, &m.ProviderFolder,
			&m.GmailID, &m.UID, &m.Mailbox, &m.FolderPath, &m.InternalDate); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// InboundMessage is a stored inbound message and when reply processing
// handled it (its arrival, for mail processing never claimed).
type InboundMessage struct {
	models.EmailMessageStoreData
	ProcessedAt time.Time
}

func (r *uniboxRepository) ListInboundFrom(ctx context.Context, orgID uuid.UUID, address string, limit int) ([]InboundMessage, error) {
	const bareFrom = `lower(COALESCE(
		(regexp_match(COALESCE(u.from_addr[1], ''), '<([^<>]+)>\s*$'))[1],
		(regexp_match(COALESCE(u.from_addr[1], ''), '\(([^()]+)\)\s*$'))[1],
		u.from_addr[1]))`
	rows, err := r.db.Query(ctx, `
		SELECT u.id, u.email_id, u.message_id, u.thread_id, u.flags, u.from_addr, u.to_addr, u.cc, u.bcc,
		       u.reply_to, u.in_reply_to, u.subject, u.snippet, u.body_text, u.folder, u.provider_folder,
		       u.internal_date, u.created_at, COALESCE(u.campaign_reply_processed_at, u.created_at)
		FROM unibox_emails u
		JOIN email_accounts ea ON ea.id = u.email_id
		WHERE ea.organization_id = $1
		  AND u.folder NOT IN ('sent', 'drafts')
		  AND u.provider_folder NOT IN ('sent', 'drafts')
		  AND strpos(lower(COALESCE(u.from_addr[1], '')), lower(btrim($2))) > 0
		  AND btrim(`+bareFrom+`) = lower(btrim($2))
		ORDER BY u.created_at DESC
		LIMIT $3`, orgID, address, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []InboundMessage
	for rows.Next() {
		var m InboundMessage
		if err := rows.Scan(&m.ID, &m.EmailID, &m.MessageID, &m.ThreadID, &m.Flags, &m.FromAddr, &m.ToAddr, &m.CC, &m.BCC,
			&m.ReplyTo, &m.InReplyTo, &m.Subject, &m.Snippet, &m.BodyText, &m.Folder, &m.ProviderFolder,
			&m.InternalDate, &m.CreatedAt, &m.ProcessedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *uniboxRepository) HasWrittenTo(ctx context.Context, orgID uuid.UUID, address string) (bool, error) {
	var written bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM unibox_emails u
			JOIN email_accounts ea ON ea.id = u.email_id
			WHERE ea.organization_id = $1
			  AND (u.folder = 'sent' OR u.provider_folder = 'sent')
			  AND strpos(lower(array_to_string(u.to_addr || u.cc || u.bcc, ' ')), lower(btrim($2))) > 0
		)`, orgID, address).Scan(&written)
	return written, err
}
