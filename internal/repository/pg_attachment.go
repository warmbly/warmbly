package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// AttachmentRepository persists campaign attachment metadata. Binary content
// lives in object storage; these rows track ownership, size (for quota), and
// the S3 key so the worker can fetch the bytes at send time.
type AttachmentRepository interface {
	Create(ctx context.Context, att *models.CampaignAttachment) error
	GetByID(ctx context.Context, id uuid.UUID) (*models.CampaignAttachment, error)
	ListByCampaign(ctx context.Context, campaignID uuid.UUID) ([]models.CampaignAttachment, error)
	// ListForStep returns what one step's send carries: the campaign-wide files
	// (no sequence_id) plus the ones scoped to that step. uuid.Nil asks for the
	// campaign-wide set alone.
	ListForStep(ctx context.Context, campaignID, sequenceID uuid.UUID) ([]models.CampaignAttachment, error)
	// StepBelongsToCampaign guards the sequence_id an upload names: the FK only
	// proves the step exists, not that it is a step of this campaign, and one
	// pointed at another campaign's step would never be sent by either.
	StepBelongsToCampaign(ctx context.Context, campaignID, sequenceID uuid.UUID) (bool, error)
	Delete(ctx context.Context, id uuid.UUID) error
	// SumStorageUsedByOrg totals the bytes of every attachment owned by the org
	// (joined through campaigns) — the basis for the per-plan storage quota.
	SumStorageUsedByOrg(ctx context.Context, orgID uuid.UUID) (int64, error)
	// CreateWithinQuota inserts the row only if the organization's total stays
	// within the limit. The limit is resolved by limitFn AFTER the org's
	// attachment lock (LockStorageQuota) is held, and the check and the insert
	// share that transaction, so two uploads in flight cannot both read the
	// same total and both pass, and a plan change cannot be raced past
	// (issue #326). Returns created=false, the total it saw and the limit it
	// applied when the file does not fit.
	CreateWithinQuota(ctx context.Context, att *models.CampaignAttachment, orgID uuid.UUID, limitFn StorageLimitFunc) (created bool, used, limit int64, err error)
}

// StorageLimitFunc resolves an organization's storage quota in bytes. It is
// called under the quota lock so the value cannot go stale before the insert.
type StorageLimitFunc func(ctx context.Context) (int64, error)

// LockStorageQuota serialises quota checks for one organization inside the
// calling transaction. Every writer of campaign_attachments that checks the
// quota takes it first, so the sum it reads cannot go stale before its insert.
func LockStorageQuota(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('campaign_attachments'), hashtext($1::text))`, orgID.String())
	return err
}

// storageUsedTx is SumStorageUsedByOrg inside a transaction.
func storageUsedTx(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) (int64, error) {
	var total int64
	err := tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(ca.size), 0)
		FROM campaign_attachments ca
		JOIN campaigns c ON c.id = ca.campaign_id
		WHERE c.organization_id = $1
	`, orgID).Scan(&total)
	return total, err
}

type attachmentRepository struct {
	DB *db.DB
}

func NewAttachmentRepository(database *db.DB) AttachmentRepository {
	return &attachmentRepository{DB: database}
}

const attachmentCols = `id, campaign_id, sequence_id, user_id, filename, size, mime_type, s3_key, created_at`

func scanAttachment(row pgx.Row, a *models.CampaignAttachment) error {
	return row.Scan(
		&a.ID, &a.CampaignID, &a.SequenceID, &a.UserID,
		&a.Filename, &a.Size, &a.MimeType, &a.S3Key, &a.CreatedAt,
	)
}

func (r *attachmentRepository) Create(ctx context.Context, att *models.CampaignAttachment) error {
	return scanAttachment(r.DB.QueryRow(ctx, `
		INSERT INTO campaign_attachments (campaign_id, sequence_id, user_id, filename, size, mime_type, s3_key)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING `+attachmentCols,
		att.CampaignID, att.SequenceID, att.UserID, att.Filename, att.Size, att.MimeType, att.S3Key,
	), att)
}

func (r *attachmentRepository) CreateWithinQuota(ctx context.Context, att *models.CampaignAttachment, orgID uuid.UUID, limitFn StorageLimitFunc) (bool, int64, int64, error) {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return false, 0, 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := LockStorageQuota(ctx, tx, orgID); err != nil {
		return false, 0, 0, err
	}
	limit, err := limitFn(ctx)
	if err != nil {
		return false, 0, 0, err
	}
	used, err := storageUsedTx(ctx, tx, orgID)
	if err != nil {
		return false, 0, limit, err
	}
	if used+att.Size > limit {
		return false, used, limit, nil
	}
	if err := scanAttachment(tx.QueryRow(ctx, `
		INSERT INTO campaign_attachments (campaign_id, sequence_id, user_id, filename, size, mime_type, s3_key)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING `+attachmentCols,
		att.CampaignID, att.SequenceID, att.UserID, att.Filename, att.Size, att.MimeType, att.S3Key,
	), att); err != nil {
		return false, used, limit, err
	}
	return true, used + att.Size, limit, tx.Commit(ctx)
}

func (r *attachmentRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.CampaignAttachment, error) {
	a := &models.CampaignAttachment{}
	err := scanAttachment(r.DB.QueryRow(ctx, `SELECT `+attachmentCols+` FROM campaign_attachments WHERE id = $1`, id), a)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return a, nil
}

func (r *attachmentRepository) ListByCampaign(ctx context.Context, campaignID uuid.UUID) ([]models.CampaignAttachment, error) {
	rows, err := r.DB.Query(ctx, `SELECT `+attachmentCols+` FROM campaign_attachments WHERE campaign_id = $1 ORDER BY created_at ASC`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.CampaignAttachment, 0)
	for rows.Next() {
		var a models.CampaignAttachment
		if err := scanAttachment(rows, &a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *attachmentRepository) ListForStep(ctx context.Context, campaignID, sequenceID uuid.UUID) ([]models.CampaignAttachment, error) {
	rows, err := r.DB.Query(ctx, `SELECT `+attachmentCols+`
		FROM campaign_attachments
		WHERE campaign_id = $1 AND (sequence_id IS NULL OR sequence_id = $2)
		ORDER BY created_at ASC`, campaignID, sequenceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.CampaignAttachment, 0)
	for rows.Next() {
		var a models.CampaignAttachment
		if err := scanAttachment(rows, &a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *attachmentRepository) StepBelongsToCampaign(ctx context.Context, campaignID, sequenceID uuid.UUID) (bool, error) {
	var ok bool
	err := r.DB.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM sequences WHERE id = $1 AND campaign_id = $2)`,
		sequenceID, campaignID).Scan(&ok)
	return ok, err
}

func (r *attachmentRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.DB.Exec(ctx, `DELETE FROM campaign_attachments WHERE id = $1`, id)
	return err
}

func (r *attachmentRepository) SumStorageUsedByOrg(ctx context.Context, orgID uuid.UUID) (int64, error) {
	var total int64
	err := r.DB.QueryRow(ctx, `
		SELECT COALESCE(SUM(ca.size), 0)
		FROM campaign_attachments ca
		JOIN campaigns c ON c.id = ca.campaign_id
		WHERE c.organization_id = $1
	`, orgID).Scan(&total)
	return total, err
}
