package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// CompanyEnrichContactRepository reads complete snapshots for guarded imports.
type CompanyEnrichContactRepository struct {
	contacts ContactRepository
	queryRow func(context.Context, string, ...any) pgx.Row
}

func NewCompanyEnrichContactRepository(database *db.DB, contacts ContactRepository) *CompanyEnrichContactRepository {
	return &CompanyEnrichContactRepository{contacts: contacts, queryRow: func(ctx context.Context, query string, args ...any) pgx.Row {
		return resultDB(ctx, database).QueryRow(ctx, query, args...)
	}}
}

// GetByIDsAndOrganization returns hydrated rows without losing verification,
// subscription or membership fields in the enrichment response.
func (r *CompanyEnrichContactRepository) GetByIDsAndOrganization(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID) ([]models.Contact, *errx.Error) {
	if orgID == uuid.Nil {
		return nil, errx.ErrNotFound
	}
	rows := make([]models.Contact, 0, len(ids))
	for _, id := range ids {
		detail, xerr := r.contacts.GetDetail(ctx, uuid.Nil, &orgID, id)
		if xerr != nil {
			return nil, xerr
		}
		if detail != nil {
			// Decode the table snapshot into the public contact model, keeping
			// verification/mailhost fields as well as profile fields. Unknown
			// internal columns are discarded by the typed decoder.
			var raw []byte
			var updatedAt, createdAt time.Time
			// The original contact timestamps are timestamp WITHOUT time zone.
			// Let pgx decode them, rather than asking JSON's RFC3339 parser to
			// invent a zone; the exact updated_at value is also our write guard.
			err := r.queryRow(ctx, `SELECT to_jsonb(c) - 'updated_at' - 'created_at', c.updated_at, c.created_at
FROM contacts c WHERE c.organization_id = $1 AND c.id = $2`, orgID, id).Scan(&raw, &updatedAt, &createdAt)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, errx.ErrNotFound
			}
			if err != nil {
				db.CaptureError(err, "", nil, "CompanyEnrich contact snapshot")
				return nil, errx.InternalError()
			}
			var snapshot models.Contact
			if err := json.Unmarshal(raw, &snapshot); err != nil {
				return nil, errx.InternalError()
			}
			snapshot.UpdatedAt, snapshot.CreatedAt = updatedAt, createdAt
			snapshot.Campaigns, snapshot.Categories = detail.Campaigns, detail.Categories
			rows = append(rows, snapshot)
		}
	}
	return rows, nil
}
