package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
)

// TemplateRepository defines methods for reply template data access
type TemplateRepository interface {
	Create(ctx context.Context, orgID, userID uuid.UUID, data *models.CreateReplyTemplate) (*models.ReplyTemplate, error)
	GetByID(ctx context.Context, orgID, templateID uuid.UUID) (*models.ReplyTemplate, error)
	List(ctx context.Context, orgID uuid.UUID) ([]models.ReplyTemplate, error)
	Update(ctx context.Context, orgID, templateID uuid.UUID, data *models.UpdateReplyTemplate) (*models.ReplyTemplate, error)
	Delete(ctx context.Context, orgID, templateID uuid.UUID) error
}

type templateRepository struct {
	db *pgxpool.Pool
}

// NewTemplateRepository creates a new template repository
func NewTemplateRepository(db *pgxpool.Pool) TemplateRepository {
	return &templateRepository{db: db}
}

// Create inserts a new reply template
func (r *templateRepository) Create(ctx context.Context, orgID, userID uuid.UUID, data *models.CreateReplyTemplate) (*models.ReplyTemplate, error) {
	query := `
		INSERT INTO reply_templates (organization_id, user_id, name, subject, body_html, body_plain, position)
		VALUES ($1, $2, $3, $4, $5, $6, (SELECT COALESCE(MAX(position), 0) + 1 FROM reply_templates WHERE organization_id = $1))
		RETURNING id, organization_id, user_id, name, subject, body_html, body_plain, position, created_at, updated_at
	`

	t := &models.ReplyTemplate{}
	err := r.db.QueryRow(ctx, query,
		orgID,
		userID,
		data.Name,
		data.Subject,
		data.BodyHTML,
		data.BodyPlain,
	).Scan(
		&t.ID,
		&t.OrganizationID,
		&t.UserID,
		&t.Name,
		&t.Subject,
		&t.BodyHTML,
		&t.BodyPlain,
		&t.Position,
		&t.CreatedAt,
		&t.UpdatedAt,
	)

	if err != nil {
		return nil, err
	}

	return t, nil
}

// GetByID retrieves a reply template by ID within an organization
func (r *templateRepository) GetByID(ctx context.Context, orgID, templateID uuid.UUID) (*models.ReplyTemplate, error) {
	query := `
		SELECT id, organization_id, user_id, name, subject, body_html, body_plain, position, created_at, updated_at
		FROM reply_templates
		WHERE id = $1 AND organization_id = $2
	`

	t := &models.ReplyTemplate{}
	err := r.db.QueryRow(ctx, query, templateID, orgID).Scan(
		&t.ID,
		&t.OrganizationID,
		&t.UserID,
		&t.Name,
		&t.Subject,
		&t.BodyHTML,
		&t.BodyPlain,
		&t.Position,
		&t.CreatedAt,
		&t.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	return t, err
}

// List retrieves reply templates for an organization with a hard limit to prevent unbounded queries
func (r *templateRepository) List(ctx context.Context, orgID uuid.UUID) ([]models.ReplyTemplate, error) {
	query := `
		SELECT id, organization_id, user_id, name, subject, body_html, body_plain, position, created_at, updated_at
		FROM reply_templates
		WHERE organization_id = $1
		ORDER BY position ASC
		LIMIT 500
	`

	rows, err := r.db.Query(ctx, query, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var templates []models.ReplyTemplate
	for rows.Next() {
		t := models.ReplyTemplate{}
		err := rows.Scan(
			&t.ID,
			&t.OrganizationID,
			&t.UserID,
			&t.Name,
			&t.Subject,
			&t.BodyHTML,
			&t.BodyPlain,
			&t.Position,
			&t.CreatedAt,
			&t.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		templates = append(templates, t)
	}

	return templates, rows.Err()
}

// Update updates a reply template with non-nil fields
func (r *templateRepository) Update(ctx context.Context, orgID, templateID uuid.UUID, data *models.UpdateReplyTemplate) (*models.ReplyTemplate, error) {
	setClauses := []string{"updated_at = NOW()"}
	args := []interface{}{}
	argIdx := 1

	if data.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, *data.Name)
		argIdx++
	}
	if data.Subject != nil {
		setClauses = append(setClauses, fmt.Sprintf("subject = $%d", argIdx))
		args = append(args, *data.Subject)
		argIdx++
	}
	if data.BodyHTML != nil {
		setClauses = append(setClauses, fmt.Sprintf("body_html = $%d", argIdx))
		args = append(args, *data.BodyHTML)
		argIdx++
	}
	if data.BodyPlain != nil {
		setClauses = append(setClauses, fmt.Sprintf("body_plain = $%d", argIdx))
		args = append(args, *data.BodyPlain)
		argIdx++
	}

	if len(args) == 0 {
		return r.GetByID(ctx, orgID, templateID)
	}

	args = append(args, templateID, orgID)

	query := fmt.Sprintf(`
		UPDATE reply_templates
		SET %s
		WHERE id = $%d AND organization_id = $%d
		RETURNING id, organization_id, user_id, name, subject, body_html, body_plain, position, created_at, updated_at
	`, strings.Join(setClauses, ", "), argIdx, argIdx+1)

	t := &models.ReplyTemplate{}
	err := r.db.QueryRow(ctx, query, args...).Scan(
		&t.ID,
		&t.OrganizationID,
		&t.UserID,
		&t.Name,
		&t.Subject,
		&t.BodyHTML,
		&t.BodyPlain,
		&t.Position,
		&t.CreatedAt,
		&t.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	return t, err
}

// Delete removes a reply template
func (r *templateRepository) Delete(ctx context.Context, orgID, templateID uuid.UUID) error {
	query := `DELETE FROM reply_templates WHERE id = $1 AND organization_id = $2`
	_, err := r.db.Exec(ctx, query, templateID, orgID)
	return err
}

// Ensure the type satisfies the interface at compile time
var _ TemplateRepository = (*templateRepository)(nil)
