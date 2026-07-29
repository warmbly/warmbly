package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// SkillRepository persists org AI skills (playbooks).
type SkillRepository interface {
	Create(ctx context.Context, s *models.AISkill) (*models.AISkill, error)
	Update(ctx context.Context, s *models.AISkill) error
	Delete(ctx context.Context, orgID, id uuid.UUID) error
	Get(ctx context.Context, orgID, id uuid.UUID) (*models.AISkill, error)
	GetByName(ctx context.Context, orgID uuid.UUID, name string) (*models.AISkill, error)
	List(ctx context.Context, orgID uuid.UUID, enabledOnly bool) ([]models.AISkill, error)
}

type skillRepository struct {
	DB *db.DB
}

func NewSkillRepository(database *db.DB) SkillRepository {
	return &skillRepository{DB: database}
}

const skillCols = `id, org_id, name, description, content, enabled, created_at, updated_at`

func scanSkill(row pgx.Row, s *models.AISkill) error {
	return row.Scan(&s.ID, &s.OrgID, &s.Name, &s.Description, &s.Content, &s.Enabled, &s.CreatedAt, &s.UpdatedAt)
}

func (r *skillRepository) Create(ctx context.Context, s *models.AISkill) (*models.AISkill, error) {
	out := &models.AISkill{}
	err := scanSkill(r.DB.QueryRow(ctx, `
		INSERT INTO ai_skills (org_id, name, description, content, enabled)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING `+skillCols, s.OrgID, s.Name, s.Description, s.Content, s.Enabled), out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *skillRepository) Update(ctx context.Context, s *models.AISkill) error {
	_, err := r.DB.Exec(ctx, `
		UPDATE ai_skills SET name = $3, description = $4, content = $5, enabled = $6, updated_at = now()
		WHERE id = $1 AND org_id = $2`,
		s.ID, s.OrgID, s.Name, s.Description, s.Content, s.Enabled)
	return err
}

func (r *skillRepository) Delete(ctx context.Context, orgID, id uuid.UUID) error {
	_, err := r.DB.Exec(ctx, `DELETE FROM ai_skills WHERE id = $1 AND org_id = $2`, id, orgID)
	return err
}

func (r *skillRepository) Get(ctx context.Context, orgID, id uuid.UUID) (*models.AISkill, error) {
	s := &models.AISkill{}
	err := scanSkill(r.DB.QueryRow(ctx, `SELECT `+skillCols+` FROM ai_skills WHERE id = $1 AND org_id = $2`, id, orgID), s)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return s, nil
}

func (r *skillRepository) GetByName(ctx context.Context, orgID uuid.UUID, name string) (*models.AISkill, error) {
	s := &models.AISkill{}
	err := scanSkill(r.DB.QueryRow(ctx, `SELECT `+skillCols+` FROM ai_skills WHERE org_id = $1 AND lower(name) = lower($2)`, orgID, name), s)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return s, nil
}

func (r *skillRepository) List(ctx context.Context, orgID uuid.UUID, enabledOnly bool) ([]models.AISkill, error) {
	query := `SELECT ` + skillCols + ` FROM ai_skills WHERE org_id = $1`
	if enabledOnly {
		query += ` AND enabled = true`
	}
	query += ` ORDER BY created_at DESC`
	rows, err := r.DB.Query(ctx, query, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.AISkill, 0)
	for rows.Next() {
		var s models.AISkill
		if err := scanSkill(rows, &s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
