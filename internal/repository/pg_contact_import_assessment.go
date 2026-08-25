package repository

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
)

type ContactImportAssessmentRepository interface {
	Create(ctx context.Context, organizationID, userID uuid.UUID, filename string, total int, quality *models.ContactImportQuality) (uuid.UUID, error)
}

type contactImportAssessmentRepository struct {
	db *pgxpool.Pool
}

func NewContactImportAssessmentRepository(db *pgxpool.Pool) ContactImportAssessmentRepository {
	return &contactImportAssessmentRepository{db: db}
}

func (r *contactImportAssessmentRepository) Create(ctx context.Context, organizationID, userID uuid.UUID, filename string, total int, quality *models.ContactImportQuality) (uuid.UUID, error) {
	if quality == nil {
		return uuid.Nil, nil
	}
	summary, err := json.Marshal(quality)
	if err != nil {
		return uuid.Nil, err
	}
	var id uuid.UUID
	err = r.db.QueryRow(ctx, `
		INSERT INTO contact_import_assessments (
			organization_id, user_id, filename, total_rows, invalid_rows,
			disposable_rows, role_rows, risky_tld_rows, blocked, summary
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id
	`, organizationID, userID, filename, total, quality.Invalid, quality.Disposable,
		quality.Role, quality.RiskyTLD, quality.Blocked, summary).Scan(&id)
	return id, err
}
