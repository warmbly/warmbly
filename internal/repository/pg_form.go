package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

type FormRepository interface {
	List(ctx context.Context, orgID uuid.UUID) ([]models.Form, *errx.Error)
	Get(ctx context.Context, orgID, id uuid.UUID) (*models.Form, *errx.Error)
	// GetByPublicID serves the public page; the unguessable token is the
	// capability, so there is no org filter.
	GetByPublicID(ctx context.Context, publicID string) (*models.Form, *errx.Error)
	Create(ctx context.Context, orgID uuid.UUID, createdBy *uuid.UUID, f *models.Form) (*models.Form, *errx.Error)
	Update(ctx context.Context, orgID uuid.UUID, f *models.Form) (*models.Form, *errx.Error)
	Delete(ctx context.Context, orgID, id uuid.UUID) *errx.Error
	// UpdateAssets sets the uploaded brand asset URLs; a nil pointer leaves
	// that column untouched.
	UpdateAssets(ctx context.Context, orgID, id uuid.UUID, logoURL, coverURL, backgroundURL *string) (*models.Form, *errx.Error)
	// RecordView bumps the view counter; best-effort, called on public GETs.
	RecordView(ctx context.Context, formID uuid.UUID) *errx.Error
	// CreateSubmission stores the answers and bumps the form's counters.
	CreateSubmission(ctx context.Context, sub *models.FormSubmission) (*models.FormSubmission, *errx.Error)
	ListSubmissions(ctx context.Context, orgID, formID uuid.UUID, limit int, before *time.Time) ([]models.FormSubmission, bool, *errx.Error)
	// LogSubmissionActivity adds the form_submitted event to the contact
	// timeline; best-effort, called after the submission is stored.
	LogSubmissionActivity(ctx context.Context, orgID, contactID, formID uuid.UUID, formName string, submissionID uuid.UUID) *errx.Error
	DeleteSubmission(ctx context.Context, orgID, formID, subID uuid.UUID) *errx.Error
}

type formRepository struct {
	DB *db.DB
}

func NewFormRepository(d *db.DB) FormRepository {
	return &formRepository{DB: d}
}

const formColumns = `f.id, f.organization_id, f.created_by, f.public_id, f.name, f.status, f.fields, f.design,
	f.success_message, f.redirect_url, f.campaign_id, f.allowed_domains, f.captcha_enabled,
	f.logo_url, f.cover_url, f.background_url,
	f.views_count, f.submissions_count, f.last_submission_at, f.published_at,
	(SELECT COALESCE(array_agg(fc.category_id), ARRAY[]::uuid[]) FROM form_categories fc WHERE fc.form_id = f.id),
	f.created_at, f.updated_at`

func scanForm(row pgx.Row) (*models.Form, error) {
	var f models.Form
	var fieldsRaw, designRaw []byte
	if err := row.Scan(&f.ID, &f.OrganizationID, &f.CreatedBy, &f.PublicID, &f.Name, &f.Status, &fieldsRaw, &designRaw,
		&f.SuccessMessage, &f.RedirectURL, &f.CampaignID, &f.AllowedDomains, &f.CaptchaEnabled,
		&f.LogoURL, &f.CoverURL, &f.BackgroundURL,
		&f.ViewsCount, &f.SubmissionsCount, &f.LastSubmissionAt, &f.PublishedAt,
		&f.CategoryIDs, &f.CreatedAt, &f.UpdatedAt); err != nil {
		return nil, err
	}
	f.Fields = []models.FormField{}
	if len(fieldsRaw) > 0 {
		if err := json.Unmarshal(fieldsRaw, &f.Fields); err != nil {
			return nil, err
		}
	}
	if len(designRaw) > 0 {
		if err := json.Unmarshal(designRaw, &f.Design); err != nil {
			return nil, err
		}
	}
	if f.AllowedDomains == nil {
		f.AllowedDomains = []string{}
	}
	if f.CategoryIDs == nil {
		f.CategoryIDs = []uuid.UUID{}
	}
	return &f, nil
}

func (r *formRepository) List(ctx context.Context, orgID uuid.UUID) ([]models.Form, *errx.Error) {
	rows, err := r.DB.Query(ctx, `SELECT `+formColumns+` FROM forms f WHERE f.organization_id = $1 ORDER BY f.created_at DESC`, orgID)
	if err != nil {
		db.CaptureError(err, "forms list", nil, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()
	out := []models.Form{}
	for rows.Next() {
		f, err := scanForm(rows)
		if err != nil {
			db.CaptureError(err, "forms list", nil, "scan")
			return nil, errx.InternalError()
		}
		out = append(out, *f)
	}
	return out, nil
}

func (r *formRepository) Get(ctx context.Context, orgID, id uuid.UUID) (*models.Form, *errx.Error) {
	f, err := scanForm(r.DB.QueryRow(ctx, `SELECT `+formColumns+` FROM forms f WHERE f.organization_id = $1 AND f.id = $2`, orgID, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errx.New(errx.NotFound, "form not found")
		}
		db.CaptureError(err, "forms get", nil, "query")
		return nil, errx.InternalError()
	}
	return f, nil
}

func (r *formRepository) GetByPublicID(ctx context.Context, publicID string) (*models.Form, *errx.Error) {
	f, err := scanForm(r.DB.QueryRow(ctx, `SELECT `+formColumns+` FROM forms f WHERE f.public_id = $1`, publicID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errx.New(errx.NotFound, "form not found")
		}
		db.CaptureError(err, "forms get public", nil, "query")
		return nil, errx.InternalError()
	}
	return f, nil
}

// formWriteValues renders the columns a Go zero value would corrupt: a form
// with no domains and no fields yet is exactly what "New form" creates, and
// its nil slices would bind as NULL against a NOT NULL allowed_domains and a
// fields column CHECKed to hold a JSON array (issue #343).
func formWriteValues(f *models.Form) (fields, design []byte, domains []string, err error) {
	list := f.Fields
	if list == nil {
		list = []models.FormField{}
	}
	if fields, err = json.Marshal(list); err != nil {
		return nil, nil, nil, err
	}
	if design, err = json.Marshal(f.Design); err != nil {
		return nil, nil, nil, err
	}
	return fields, design, textArray(f.AllowedDomains), nil
}

func (r *formRepository) Create(ctx context.Context, orgID uuid.UUID, createdBy *uuid.UUID, f *models.Form) (*models.Form, *errx.Error) {
	var count int
	if err := r.DB.QueryRow(ctx, `SELECT COUNT(*) FROM forms WHERE organization_id = $1`, orgID).Scan(&count); err != nil {
		db.CaptureError(err, "forms count", nil, "query")
		return nil, errx.InternalError()
	}
	if count >= models.FormsPerOrgMax {
		return nil, errx.New(errx.BadRequest, fmt.Sprintf("at most %d forms per organization", models.FormsPerOrgMax))
	}

	fields, design, domains, err := formWriteValues(f)
	if err != nil {
		db.CaptureError(err, "forms create", nil, "marshal")
		return nil, errx.InternalError()
	}

	tx, err := r.DB.Begin(ctx)
	if err != nil {
		db.CaptureError(err, "forms create", nil, "begin")
		return nil, errx.InternalError()
	}
	defer tx.Rollback(ctx)

	var id uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO forms (organization_id, created_by, public_id, name, status, fields, design,
			success_message, redirect_url, campaign_id, allowed_domains, captcha_enabled)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING id
	`, orgID, createdBy, f.PublicID, f.Name, f.Status, fields, design,
		f.SuccessMessage, f.RedirectURL, f.CampaignID, domains, f.CaptchaEnabled).Scan(&id)
	if err != nil {
		db.CaptureError(err, "forms create", nil, "insert")
		return nil, errx.InternalError()
	}
	if xerr := setFormCategories(ctx, tx, orgID, id, f.CategoryIDs); xerr != nil {
		return nil, xerr
	}
	if err := tx.Commit(ctx); err != nil {
		db.CaptureError(err, "forms create", nil, "commit")
		return nil, errx.InternalError()
	}
	return r.Get(ctx, orgID, id)
}

func (r *formRepository) Update(ctx context.Context, orgID uuid.UUID, f *models.Form) (*models.Form, *errx.Error) {
	fields, design, domains, err := formWriteValues(f)
	if err != nil {
		db.CaptureError(err, "forms update", nil, "marshal")
		return nil, errx.InternalError()
	}

	tx, err := r.DB.Begin(ctx)
	if err != nil {
		db.CaptureError(err, "forms update", nil, "begin")
		return nil, errx.InternalError()
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `
		UPDATE forms SET name = $3, status = $4, fields = $5, design = $6, success_message = $7,
			redirect_url = $8, campaign_id = $9, allowed_domains = $10, captcha_enabled = $11,
			published_at = $12, updated_at = NOW()
		WHERE organization_id = $1 AND id = $2
	`, orgID, f.ID, f.Name, f.Status, fields, design, f.SuccessMessage,
		f.RedirectURL, f.CampaignID, domains, f.CaptchaEnabled, f.PublishedAt)
	if err != nil {
		db.CaptureError(err, "forms update", nil, "exec")
		return nil, errx.InternalError()
	}
	if tag.RowsAffected() == 0 {
		return nil, errx.New(errx.NotFound, "form not found")
	}
	if _, err := tx.Exec(ctx, `DELETE FROM form_categories WHERE form_id = $1`, f.ID); err != nil {
		db.CaptureError(err, "forms update", nil, "categories clear")
		return nil, errx.InternalError()
	}
	if xerr := setFormCategories(ctx, tx, orgID, f.ID, f.CategoryIDs); xerr != nil {
		return nil, xerr
	}
	if err := tx.Commit(ctx); err != nil {
		db.CaptureError(err, "forms update", nil, "commit")
		return nil, errx.InternalError()
	}
	return r.Get(ctx, orgID, f.ID)
}

// setFormCategories links the picked categories, quietly dropping ids that no
// longer exist or belong to another workspace (a stale picker must not fail the
// save, and a borrowed id must not file leads under a foreign category).
func setFormCategories(ctx context.Context, tx pgx.Tx, orgID, formID uuid.UUID, categoryIDs []uuid.UUID) *errx.Error {
	if len(categoryIDs) == 0 {
		return nil
	}
	if len(categoryIDs) > models.FormMaxCategories {
		return errx.New(errx.BadRequest, fmt.Sprintf("at most %d categories per form", models.FormMaxCategories))
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO form_categories (form_id, category_id)
		SELECT $1, c.id FROM categories c WHERE c.id = ANY($2) AND c.organization_id = $3
		ON CONFLICT (form_id, category_id) DO NOTHING
	`, formID, categoryIDs, orgID)
	if err != nil {
		db.CaptureError(err, "forms categories", nil, "insert")
		return errx.InternalError()
	}
	return nil
}

func (r *formRepository) Delete(ctx context.Context, orgID, id uuid.UUID) *errx.Error {
	tag, err := r.DB.Exec(ctx, `DELETE FROM forms WHERE organization_id = $1 AND id = $2`, orgID, id)
	if err != nil {
		db.CaptureError(err, "forms delete", nil, "exec")
		return errx.InternalError()
	}
	if tag.RowsAffected() == 0 {
		return errx.New(errx.NotFound, "form not found")
	}
	return nil
}

func (r *formRepository) UpdateAssets(ctx context.Context, orgID, id uuid.UUID, logoURL, coverURL, backgroundURL *string) (*models.Form, *errx.Error) {
	tag, err := r.DB.Exec(ctx, `
		UPDATE forms SET
			logo_url = COALESCE($3, logo_url),
			cover_url = COALESCE($4, cover_url),
			background_url = COALESCE($5, background_url),
			updated_at = NOW()
		WHERE organization_id = $1 AND id = $2
	`, orgID, id, logoURL, coverURL, backgroundURL)
	if err != nil {
		db.CaptureError(err, "forms assets", nil, "exec")
		return nil, errx.InternalError()
	}
	if tag.RowsAffected() == 0 {
		return nil, errx.New(errx.NotFound, "form not found")
	}
	return r.Get(ctx, orgID, id)
}

func (r *formRepository) RecordView(ctx context.Context, formID uuid.UUID) *errx.Error {
	if _, err := r.DB.Exec(ctx, `UPDATE forms SET views_count = views_count + 1 WHERE id = $1`, formID); err != nil {
		db.CaptureError(err, "forms view", nil, "exec")
		return errx.InternalError()
	}
	return nil
}

func (r *formRepository) CreateSubmission(ctx context.Context, sub *models.FormSubmission) (*models.FormSubmission, *errx.Error) {
	data, err := json.Marshal(sub.Data)
	if err != nil {
		return nil, errx.InternalError()
	}

	tx, err := r.DB.Begin(ctx)
	if err != nil {
		db.CaptureError(err, "form submission", nil, "begin")
		return nil, errx.InternalError()
	}
	defer tx.Rollback(ctx)

	err = tx.QueryRow(ctx, `
		INSERT INTO form_submissions (form_id, organization_id, contact_id, campaign_id, data, source_url)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at
	`, sub.FormID, sub.OrganizationID, sub.ContactID, sub.CampaignID, data, sub.SourceURL).Scan(&sub.ID, &sub.CreatedAt)
	if err != nil {
		db.CaptureError(err, "form submission", nil, "insert")
		return nil, errx.InternalError()
	}
	if _, err := tx.Exec(ctx, `
		UPDATE forms SET submissions_count = submissions_count + 1, last_submission_at = NOW() WHERE id = $1
	`, sub.FormID); err != nil {
		db.CaptureError(err, "form submission", nil, "counters")
		return nil, errx.InternalError()
	}
	if err := tx.Commit(ctx); err != nil {
		db.CaptureError(err, "form submission", nil, "commit")
		return nil, errx.InternalError()
	}
	return sub, nil
}

func (r *formRepository) ListSubmissions(ctx context.Context, orgID, formID uuid.UUID, limit int, before *time.Time) ([]models.FormSubmission, bool, *errx.Error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := r.DB.Query(ctx, `
		SELECT s.id, s.form_id, s.organization_id, s.contact_id, s.campaign_id, s.data, s.source_url, s.created_at,
			COALESCE(c.email, ''), COALESCE(TRIM(c.first_name || ' ' || c.last_name), ''), COALESCE(cp.name, '')
		FROM form_submissions s
		LEFT JOIN contacts c ON c.id = s.contact_id
		LEFT JOIN campaigns cp ON cp.id = s.campaign_id
		WHERE s.organization_id = $1 AND s.form_id = $2 AND ($3::timestamptz IS NULL OR s.created_at < $3)
		ORDER BY s.created_at DESC
		LIMIT $4
	`, orgID, formID, before, limit+1)
	if err != nil {
		db.CaptureError(err, "form submissions list", nil, "query")
		return nil, false, errx.InternalError()
	}
	defer rows.Close()
	out := []models.FormSubmission{}
	for rows.Next() {
		var s models.FormSubmission
		var data []byte
		if err := rows.Scan(&s.ID, &s.FormID, &s.OrganizationID, &s.ContactID, &s.CampaignID, &data, &s.SourceURL, &s.CreatedAt,
			&s.ContactEmail, &s.ContactName, &s.CampaignName); err != nil {
			db.CaptureError(err, "form submissions list", nil, "scan")
			return nil, false, errx.InternalError()
		}
		s.Data = map[string]any{}
		if len(data) > 0 {
			if err := json.Unmarshal(data, &s.Data); err != nil {
				db.CaptureError(err, "form submissions list", nil, "unmarshal")
				return nil, false, errx.InternalError()
			}
		}
		out = append(out, s)
	}
	hasMore := len(out) > limit
	if hasMore {
		out = out[:limit]
	}
	return out, hasMore, nil
}

func (r *formRepository) DeleteSubmission(ctx context.Context, orgID, formID, subID uuid.UUID) *errx.Error {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		db.CaptureError(err, "form submission delete", nil, "begin")
		return errx.InternalError()
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `
		DELETE FROM form_submissions WHERE organization_id = $1 AND form_id = $2 AND id = $3
	`, orgID, formID, subID)
	if err != nil {
		db.CaptureError(err, "form submission delete", nil, "exec")
		return errx.InternalError()
	}
	if tag.RowsAffected() == 0 {
		return errx.New(errx.NotFound, "submission not found")
	}
	if _, err := tx.Exec(ctx, `
		UPDATE forms SET submissions_count = GREATEST(submissions_count - 1, 0) WHERE id = $1
	`, formID); err != nil {
		db.CaptureError(err, "form submission delete", nil, "counters")
		return errx.InternalError()
	}
	if err := tx.Commit(ctx); err != nil {
		db.CaptureError(err, "form submission delete", nil, "commit")
		return errx.InternalError()
	}
	return nil
}

func (r *formRepository) LogSubmissionActivity(ctx context.Context, orgID, contactID, formID uuid.UUID, formName string, submissionID uuid.UUID) *errx.Error {
	_, err := r.DB.Exec(ctx, `
		INSERT INTO contact_activities (contact_id, organization_id, user_id, activity_type, metadata)
		VALUES ($1, $2, NULL, 'form_submitted',
			jsonb_build_object('form_id', $3::uuid, 'form_name', $4::text, 'submission_id', $5::uuid))
	`, contactID, orgID, formID, formName, submissionID)
	if err != nil {
		db.CaptureError(err, "form activity", nil, "exec")
		return errx.InternalError()
	}
	return nil
}
