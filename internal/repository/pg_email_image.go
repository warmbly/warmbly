package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// EmailImageRepository persists the workspace image library used inside email
// bodies. Binary content lives in object storage under a public key; these rows
// track ownership, size (for the shared storage quota) and the public URL the
// composer inserts.
type EmailImageRepository interface {
	// CreateWithinQuota inserts the row only if the organization's total
	// storage stays within the limit, under the same lock and against the same
	// total as campaign attachments, so an image and an attachment racing each
	// other cannot both pass. Returns created=false with the total it saw and
	// the limit it applied when the image does not fit.
	CreateWithinQuota(ctx context.Context, img *models.EmailImage, limitFn StorageLimitFunc) (created bool, used, limit int64, err error)
	// ListByOrg keyset-paginates the library: rows strictly older than
	// (beforeCreatedAt, beforeID), newest first. Pass zero values for the
	// first page.
	ListByOrg(ctx context.Context, orgID uuid.UUID, limit int, beforeCreatedAt time.Time, beforeID uuid.UUID) ([]models.EmailImage, error)
	GetByID(ctx context.Context, id uuid.UUID) (*models.EmailImage, error)
	Delete(ctx context.Context, id uuid.UUID) error
	// ListReferenced returns orgID's images whose URL appears in the steps or
	// variants of campaignID (uuid.Nil for none) or in any of texts.
	ListReferenced(ctx context.Context, orgID, campaignID uuid.UUID, texts []string) ([]models.EmailImage, error)
}

type emailImageRepository struct {
	DB *db.DB
}

func NewEmailImageRepository(database *db.DB) EmailImageRepository {
	return &emailImageRepository{DB: database}
}

const emailImageCols = `id, organization_id, user_id, filename, mime_type, size, width, height, storage_key, url, created_at`

func scanEmailImage(row pgx.Row, i *models.EmailImage) error {
	return row.Scan(
		&i.ID, &i.OrganizationID, &i.UserID, &i.Filename, &i.MimeType,
		&i.Size, &i.Width, &i.Height, &i.StorageKey, &i.URL, &i.CreatedAt,
	)
}

func (r *emailImageRepository) CreateWithinQuota(ctx context.Context, img *models.EmailImage, limitFn StorageLimitFunc) (bool, int64, int64, error) {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return false, 0, 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := LockStorageQuota(ctx, tx, img.OrganizationID); err != nil {
		return false, 0, 0, err
	}
	limit, err := limitFn(ctx)
	if err != nil {
		return false, 0, 0, err
	}
	used, err := storageUsedTx(ctx, tx, img.OrganizationID)
	if err != nil {
		return false, 0, limit, err
	}
	if used+img.Size > limit {
		return false, used, limit, nil
	}
	if err := scanEmailImage(tx.QueryRow(ctx, `
		INSERT INTO email_images (organization_id, user_id, filename, mime_type, size, width, height, storage_key, url)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING `+emailImageCols,
		img.OrganizationID, img.UserID, img.Filename, img.MimeType,
		img.Size, img.Width, img.Height, img.StorageKey, img.URL,
	), img); err != nil {
		return false, used, limit, err
	}
	return true, used + img.Size, limit, tx.Commit(ctx)
}

func (r *emailImageRepository) ListByOrg(ctx context.Context, orgID uuid.UUID, limit int, beforeCreatedAt time.Time, beforeID uuid.UUID) ([]models.EmailImage, error) {
	if limit <= 0 || limit > 200 {
		limit = 60
	}
	query := `SELECT ` + emailImageCols + `
		FROM email_images WHERE organization_id = $1
		ORDER BY created_at DESC, id DESC LIMIT $2`
	args := []any{orgID, limit}
	if !beforeCreatedAt.IsZero() {
		query = `SELECT ` + emailImageCols + `
		FROM email_images WHERE organization_id = $1 AND (created_at, id) < ($3, $4)
		ORDER BY created_at DESC, id DESC LIMIT $2`
		args = append(args, beforeCreatedAt, beforeID)
	}
	rows, err := r.DB.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.EmailImage, 0)
	for rows.Next() {
		var img models.EmailImage
		if err := scanEmailImage(rows, &img); err != nil {
			return nil, err
		}
		out = append(out, img)
	}
	return out, rows.Err()
}

func (r *emailImageRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.EmailImage, error) {
	img := &models.EmailImage{}
	err := scanEmailImage(r.DB.QueryRow(ctx, `SELECT `+emailImageCols+` FROM email_images WHERE id = $1`, id), img)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return img, nil
}

func (r *emailImageRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.DB.Exec(ctx, `DELETE FROM email_images WHERE id = $1`, id)
	return err
}

func (r *emailImageRepository) ListReferenced(ctx context.Context, orgID, campaignID uuid.UUID, texts []string) ([]models.EmailImage, error) {
	query := `SELECT ` + emailImageCols + `
		FROM email_images i
		WHERE i.organization_id = $1 AND i.url <> '' AND (
			EXISTS (SELECT 1 FROM sequences s
				WHERE s.campaign_id = $2 AND s.organization_id = $1
				  AND (strpos(s.body_html, i.url) > 0 OR strpos(s.body_plain, i.url) > 0))
			OR EXISTS (SELECT 1 FROM campaign_ab_variants v JOIN campaigns c ON c.id = v.campaign_id
				WHERE v.campaign_id = $2 AND c.organization_id = $1
				  AND (strpos(v.body_html, i.url) > 0 OR strpos(v.body_plain, i.url) > 0))
			OR EXISTS (SELECT 1 FROM unnest($3::text[]) AS t(body) WHERE strpos(t.body, i.url) > 0)
		)
		ORDER BY i.created_at, i.id`
	if texts == nil {
		texts = []string{}
	}
	rows, err := r.DB.Query(ctx, query, orgID, campaignID, texts)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.EmailImage, 0)
	for rows.Next() {
		var img models.EmailImage
		if err := scanEmailImage(rows, &img); err != nil {
			return nil, err
		}
		out = append(out, img)
	}
	return out, rows.Err()
}
