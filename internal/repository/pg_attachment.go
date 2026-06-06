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
	Delete(ctx context.Context, id uuid.UUID) error
	// SumStorageUsedByOrg totals the bytes of every attachment owned by the org
	// (joined through campaigns) — the basis for the per-plan storage quota.
	SumStorageUsedByOrg(ctx context.Context, orgID uuid.UUID) (int64, error)
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
