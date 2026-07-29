package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/models"
)

// ErrDraftExists is returned by CreateDraft when a pending draft already exists
// for the thread, or the inbound message was already drafted for (the partial
// unique indexes). The inbox agent treats it as "someone else already handled
// this reply" and stops without charging.
var ErrDraftExists = errors.New("a draft already exists for this thread or message")

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
