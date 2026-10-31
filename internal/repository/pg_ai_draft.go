package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/mailhdr"
)

// ErrDraftExists is returned by CreateDraft when a pending draft already exists
// for the thread, or the inbound message was already drafted for (the partial
// unique indexes). The inbox agent treats it as "someone else already handled
// this reply" and stops without charging.
var ErrDraftExists = errors.New("a draft already exists for this thread or message")

// Upsert outcomes the caller maps to API errors.
var (
	// ErrDraftThreadNotFound: the org has no inbound message on the thread.
	ErrDraftThreadNotFound = errors.New("no inbound message on that thread")
	// ErrDraftAlreadySent: an approved draft already answers the newest inbound message.
	ErrDraftAlreadySent = errors.New("an approved draft already answers the newest inbound message")
	// ErrDraftDiscarded: a discarded draft already covers the newest inbound message.
	ErrDraftDiscarded = errors.New("a discarded draft already covers the newest inbound message")
	// ErrDraftMailboxNotAllowed: the draft's mailbox is outside AllowedMailboxes.
	ErrDraftMailboxNotAllowed = errors.New("the thread's mailbox is outside the caller's allowlist")
)

// upsertDraftAttempts bounds the retries after losing an insert race.
const upsertDraftAttempts = 3

// UpsertDraftParams is one externally written reply draft for a thread.
type UpsertDraftParams struct {
	ThreadID    string
	Body        string
	Model       string
	IntentClass string
	Confidence  float64
	// AllowedMailboxes limits the draft to these mailboxes; nil allows any.
	AllowedMailboxes []uuid.UUID
}

// AIDraftRepository stores the inbox agent's suggested replies awaiting human
// review. Drafts are inert data: nothing here sends mail.
type AIDraftRepository interface {
	// CreateDraft inserts a pending draft. Returns ErrDraftExists when a pending
	// draft for the thread (or a draft for the same inbound message) already
	// exists, so the caller can stop without double-drafting or double-charging.
	CreateDraft(ctx context.Context, d *models.AIThreadDraft) error
	// HasActiveDraft reports whether a pending draft exists for the thread, or any
	// draft exists for the inbound message (a cheap pre-check before generating).
	HasActiveDraft(ctx context.Context, orgID uuid.UUID, threadID string, sourceMessageID *uuid.UUID) (bool, error)
	// DeleteDraft removes a draft by id (used to unwind a reserved row when the
	// credit charge fails, so no orphan pending draft lingers).
	DeleteDraft(ctx context.Context, id uuid.UUID) error
	// GetDraft returns one draft scoped to the org, or nil when absent.
	GetDraft(ctx context.Context, orgID, id uuid.UUID) (*models.AIThreadDraft, error)
	// ListPendingDrafts returns the org's pending drafts, newest first.
	ListPendingDrafts(ctx context.Context, orgID uuid.UUID, limit int) ([]models.AIThreadDraft, error)
	// CountPendingDrafts returns the org's pending-draft count (the unibox badge).
	CountPendingDrafts(ctx context.Context, orgID uuid.UUID) (int64, error)
	// SetDraftStatus transitions a PENDING draft to approved/discarded. Returns
	// false when the draft is missing or already resolved (idempotent human action).
	SetDraftStatus(ctx context.Context, orgID, id uuid.UUID, status string) (bool, error)
	// RevertApprovedToPending puts an APPROVED draft back to pending, used when
	// the approve claim succeeded but the actual send then failed. Returns false
	// when the draft is not in the approved state (nothing to revert).
	RevertApprovedToPending(ctx context.Context, orgID, id uuid.UUID) (bool, error)
	// UpsertPendingDraftForThread writes a reply draft for the org's newest inbound
	// message on the thread: it replaces the body of the thread's pending draft, or
	// inserts a pending one filled like the inbox agent's. Approved and discarded
	// drafts are never touched. created reports an insert.
	UpsertPendingDraftForThread(ctx context.Context, orgID uuid.UUID, p UpsertDraftParams) (d *models.AIThreadDraft, created bool, err error)
}

type aiDraftRepository struct {
	db *pgxpool.Pool
}

func NewAIDraftRepository(db *pgxpool.Pool) AIDraftRepository {
	return &aiDraftRepository{db: db}
}

const aiDraftCols = `id, organization_id, email_account_id, owner_user_id, thread_id, source_message_id,
	contact_id, campaign_id, to_addr, subject, in_reply_to, body, intent_class, confidence, model, status,
	created_at, updated_at`

func scanAIDraft(row pgx.Row, d *models.AIThreadDraft) error {
	return row.Scan(&d.ID, &d.OrganizationID, &d.EmailAccountID, &d.OwnerUserID, &d.ThreadID, &d.SourceMessageID,
		&d.ContactID, &d.CampaignID, &d.ToAddr, &d.Subject, &d.InReplyTo, &d.Body, &d.IntentClass, &d.Confidence,
		&d.Model, &d.Status, &d.CreatedAt, &d.UpdatedAt)
}

func (r *aiDraftRepository) CreateDraft(ctx context.Context, d *models.AIThreadDraft) error {
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	now := time.Now().UTC()
	d.CreatedAt = now
	d.UpdatedAt = now
	if d.Status == "" {
		d.Status = models.AIDraftPending
	}
	_, err := r.db.Exec(ctx, `
		INSERT INTO ai_thread_drafts
			(id, organization_id, email_account_id, owner_user_id, thread_id, source_message_id,
			 contact_id, campaign_id, to_addr, subject, in_reply_to, body, intent_class, confidence, model, status,
			 created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$17)`,
		d.ID, d.OrganizationID, d.EmailAccountID, d.OwnerUserID, d.ThreadID, d.SourceMessageID,
		d.ContactID, d.CampaignID, d.ToAddr, d.Subject, d.InReplyTo, d.Body, d.IntentClass, d.Confidence, d.Model, d.Status,
		now)
	if isUniqueViolation(err) {
		return ErrDraftExists
	}
	return err
}

func (r *aiDraftRepository) HasActiveDraft(ctx context.Context, orgID uuid.UUID, threadID string, sourceMessageID *uuid.UUID) (bool, error) {
	var exists bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM ai_thread_drafts
			WHERE (organization_id = $1 AND thread_id = $2 AND status = 'pending')
			   OR ($3::uuid IS NOT NULL AND source_message_id = $3)
		)`, orgID, threadID, sourceMessageID).Scan(&exists)
	return exists, err
}

func (r *aiDraftRepository) DeleteDraft(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM ai_thread_drafts WHERE id = $1`, id)
	return err
}

func (r *aiDraftRepository) GetDraft(ctx context.Context, orgID, id uuid.UUID) (*models.AIThreadDraft, error) {
	var d models.AIThreadDraft
	row := r.db.QueryRow(ctx, `SELECT `+aiDraftCols+` FROM ai_thread_drafts WHERE id = $1 AND organization_id = $2`, id, orgID)
	if err := scanAIDraft(row, &d); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &d, nil
}

func (r *aiDraftRepository) ListPendingDrafts(ctx context.Context, orgID uuid.UUID, limit int) ([]models.AIThreadDraft, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.db.Query(ctx, `SELECT `+aiDraftCols+`
		FROM ai_thread_drafts
		WHERE organization_id = $1 AND status = 'pending'
		ORDER BY created_at DESC LIMIT $2`, orgID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.AIThreadDraft{}
	for rows.Next() {
		var d models.AIThreadDraft
		if err := scanAIDraft(rows, &d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *aiDraftRepository) CountPendingDrafts(ctx context.Context, orgID uuid.UUID) (int64, error) {
	var n int64
	err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM ai_thread_drafts WHERE organization_id = $1 AND status = 'pending'`, orgID).Scan(&n)
	return n, err
}

func (r *aiDraftRepository) SetDraftStatus(ctx context.Context, orgID, id uuid.UUID, status string) (bool, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE ai_thread_drafts SET status = $3, updated_at = NOW()
		WHERE id = $1 AND organization_id = $2 AND status = 'pending'`, id, orgID, status)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *aiDraftRepository) RevertApprovedToPending(ctx context.Context, orgID, id uuid.UUID) (bool, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE ai_thread_drafts SET status = 'pending', updated_at = NOW()
		WHERE id = $1 AND organization_id = $2 AND status = 'approved'`, id, orgID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// draftInbound is the inbound message a draft answers, with its mailbox owner.
type draftInbound struct {
	id        uuid.UUID
	emailID   uuid.UUID
	ownerID   uuid.UUID
	sender    string
	messageID string
	subject   string
	arrivedAt time.Time
}

func (r *aiDraftRepository) UpsertPendingDraftForThread(ctx context.Context, orgID uuid.UUID, p UpsertDraftParams) (*models.AIThreadDraft, bool, error) {
	var err error
	for range upsertDraftAttempts {
		var d *models.AIThreadDraft
		var created bool
		d, created, err = r.upsertPendingDraftOnce(ctx, orgID, p)
		// ErrDraftExists means a concurrent insert won; the next pass finds its row.
		if !errors.Is(err, ErrDraftExists) {
			return d, created, err
		}
	}
	return nil, false, err
}

func mailboxInList(allowed []uuid.UUID, id uuid.UUID) bool {
	if allowed == nil {
		return true
	}
	for _, a := range allowed {
		if a == id {
			return true
		}
	}
	return false
}

func (r *aiDraftRepository) upsertPendingDraftOnce(ctx context.Context, orgID uuid.UUID, p UpsertDraftParams) (*models.AIThreadDraft, bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	in, err := newestInboundForDraft(ctx, tx, orgID, p.ThreadID)
	if err != nil {
		return nil, false, err
	}
	if !mailboxInList(p.AllowedMailboxes, in.emailID) {
		return nil, false, ErrDraftMailboxNotAllowed
	}

	// Lock the thread's draft, a pending one first, so a concurrent approve waits.
	var cur models.AIThreadDraft
	err = scanAIDraft(tx.QueryRow(ctx, `SELECT `+aiDraftCols+`
		FROM ai_thread_drafts
		WHERE organization_id = $1 AND thread_id = $2
		ORDER BY (status = 'pending') DESC, created_at DESC, id DESC
		LIMIT 1 FOR UPDATE`, orgID, p.ThreadID), &cur)
	hasCur := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, err
	}

	// A pending draft on an older message is retargeted unless a resolved draft covers the newest.
	pending := hasCur && cur.Status == models.AIDraftPending
	retarget := pending && (cur.SourceMessageID == nil || *cur.SourceMessageID != in.id)
	if !pending || retarget {
		if err := resolvedDraftBlock(ctx, tx, orgID, p.ThreadID, in); err != nil {
			return nil, false, err
		}
	}

	if pending {
		if !mailboxInList(p.AllowedMailboxes, cur.EmailAccountID) {
			return nil, false, ErrDraftMailboxNotAllowed
		}
		var d models.AIThreadDraft
		if !retarget {
			if err := scanAIDraft(tx.QueryRow(ctx, `
				UPDATE ai_thread_drafts
				SET body = $2, model = $3, intent_class = $4, confidence = $5, updated_at = NOW()
				WHERE id = $1
				RETURNING `+aiDraftCols, cur.ID, p.Body, p.Model, p.IntentClass, p.Confidence), &d); err != nil {
				return nil, false, err
			}
			return &d, false, tx.Commit(ctx)
		}
		// Re-point recipient, subject, threading, sender links and mailbox at the newest message.
		contactID := contactForDraft(ctx, tx, orgID, in.sender)
		var campaignID *uuid.UUID
		if contactID != nil {
			campaignID = campaignForDraft(ctx, tx, orgID, *contactID)
		}
		if err := scanAIDraft(tx.QueryRow(ctx, `
			UPDATE ai_thread_drafts
			SET body = $2, model = $3, intent_class = $4, confidence = $5,
			    source_message_id = $6, to_addr = $7, subject = $8, in_reply_to = $9,
			    contact_id = $10, campaign_id = $11, email_account_id = $12, owner_user_id = $13, updated_at = NOW()
			WHERE id = $1
			RETURNING `+aiDraftCols, cur.ID, p.Body, p.Model, p.IntentClass, p.Confidence,
			in.id, in.sender, draftReplySubject(in.subject), in.messageID, contactID, campaignID, in.emailID, in.ownerID), &d); err != nil {
			return nil, false, err
		}
		return &d, false, tx.Commit(ctx)
	}

	d := models.AIThreadDraft{
		ID:              uuid.New(),
		OrganizationID:  orgID,
		EmailAccountID:  in.emailID,
		OwnerUserID:     in.ownerID,
		ThreadID:        p.ThreadID,
		SourceMessageID: &in.id,
		ToAddr:          in.sender,
		Subject:         draftReplySubject(in.subject),
		InReplyTo:       in.messageID,
		Body:            p.Body,
		IntentClass:     p.IntentClass,
		Confidence:      p.Confidence,
		Model:           p.Model,
		Status:          models.AIDraftPending,
	}
	d.ContactID = contactForDraft(ctx, tx, orgID, in.sender)
	if d.ContactID != nil {
		d.CampaignID = campaignForDraft(ctx, tx, orgID, *d.ContactID)
	}
	now := time.Now().UTC()
	d.CreatedAt, d.UpdatedAt = now, now
	_, err = tx.Exec(ctx, `
		INSERT INTO ai_thread_drafts
			(id, organization_id, email_account_id, owner_user_id, thread_id, source_message_id,
			 contact_id, campaign_id, to_addr, subject, in_reply_to, body, intent_class, confidence, model, status,
			 created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$17)`,
		d.ID, d.OrganizationID, d.EmailAccountID, d.OwnerUserID, d.ThreadID, d.SourceMessageID,
		d.ContactID, d.CampaignID, d.ToAddr, d.Subject, d.InReplyTo, d.Body, d.IntentClass, d.Confidence, d.Model, d.Status, now)
	if isUniqueViolation(err) {
		return nil, false, ErrDraftExists
	}
	if err != nil {
		return nil, false, err
	}
	return &d, true, tx.Commit(ctx)
}

// resolvedDraftBlock refuses a draft when a resolved one already answers the message.
func resolvedDraftBlock(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, threadID string, in *draftInbound) error {
	var status string
	err := tx.QueryRow(ctx, `
		SELECT status FROM ai_thread_drafts
		WHERE organization_id = $1
		  AND status <> 'pending'
		  AND (source_message_id = $2 OR (thread_id = $3 AND created_at >= $4))
		ORDER BY (status = 'approved') DESC, created_at DESC
		LIMIT 1`, orgID, in.id, threadID, in.arrivedAt).Scan(&status)
	switch {
	case err == nil && status == models.AIDraftApproved:
		return ErrDraftAlreadySent
	case err == nil:
		return ErrDraftDiscarded
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	}
	return err
}

// newestInboundForDraft is the org's newest message on the thread that came from
// someone else: not in an outbound or junk folder and not from the mailbox itself.
func newestInboundForDraft(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, threadID string) (*draftInbound, error) {
	rows, err := tx.Query(ctx, `
		SELECT ue.id, ue.email_id, ea.user_id, ea.email, ue.from_addr, ue.message_id, ue.subject, ue.internal_date
		FROM unibox_emails ue
		JOIN email_accounts ea ON ea.id = ue.email_id
		WHERE ea.organization_id = $1
		  AND COALESCE(NULLIF(ue.thread_id, ''), ue.id::text) = $2
		  AND ue.folder NOT IN ('sent', 'drafts', 'spam', 'trash')
		ORDER BY ue.internal_date DESC, ue.id DESC`, orgID, threadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var in draftInbound
		var mailbox string
		var from []string
		if err := rows.Scan(&in.id, &in.emailID, &in.ownerID, &mailbox, &from, &in.messageID, &in.subject, &in.arrivedAt); err != nil {
			return nil, err
		}
		if len(from) == 0 {
			continue
		}
		in.sender = strings.ToLower(strings.Trim(mailhdr.Bare(strings.TrimSpace(from[0])), "<>"))
		if in.sender == "" || in.sender == strings.ToLower(strings.TrimSpace(mailbox)) {
			continue
		}
		return &in, nil
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return nil, ErrDraftThreadNotFound
}

// contactForDraft is the org's contact for the sender, nil when there is none.
func contactForDraft(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, email string) *uuid.UUID {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		SELECT id FROM contacts
		WHERE organization_id = $1 AND lower(email) = $2
		ORDER BY created_at DESC LIMIT 1`, orgID, email).Scan(&id)
	if err != nil {
		return nil
	}
	return &id
}

// campaignForDraft is the org campaign the contact most recently replied to, else most recently mailed.
func campaignForDraft(ctx context.Context, tx pgx.Tx, orgID, contactID uuid.UUID) *uuid.UUID {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		SELECT p.campaign_id FROM campaign_contact_progress p
		JOIN campaigns c ON c.id = p.campaign_id AND c.organization_id = $1
		WHERE p.contact_id = $2
		ORDER BY p.replied_at DESC NULLS LAST, p.sent_at DESC NULLS LAST
		LIMIT 1`, orgID, contactID).Scan(&id)
	if err != nil {
		return nil
	}
	return &id
}

// draftReplySubject prefixes "Re: " once, as a normal reply subject.
func draftReplySubject(subject string) string {
	s := strings.TrimSpace(subject)
	if s == "" {
		return "Re:"
	}
	if strings.HasPrefix(strings.ToLower(s), "re:") {
		return s
	}
	return "Re: " + s
}
