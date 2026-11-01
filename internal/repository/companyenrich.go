package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/displayname"
)

// CompanyEnrichContactRepository narrows enrichment writes to profile fields
// and guards the exact contact snapshot used to prepare the update.
type CompanyEnrichContactRepository struct {
	contacts ContactRepository
	exec     func(context.Context, string, ...any) (pgconn.CommandTag, error)
	queryRow func(context.Context, string, ...any) pgx.Row
}

func NewCompanyEnrichContactRepository(database *db.DB, contacts ContactRepository) *CompanyEnrichContactRepository {
	return &CompanyEnrichContactRepository{contacts: contacts, exec: func(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
		return resultDB(ctx, database).Exec(ctx, query, args...)
	}, queryRow: func(ctx context.Context, query string, args ...any) pgx.Row {
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

const companyEnrichProfileSQL = `
UPDATE contacts SET
 first_name = COALESCE($4, first_name),
 last_name = COALESCE($5, last_name),
 company = COALESCE($6, company),
 custom_fields = COALESCE(custom_fields, '{}'::jsonb) || $7::jsonb,
 updated_at = clock_timestamp()
WHERE organization_id = $1 AND id = $2 AND updated_at = $3 AND email = $8
 AND octet_length((COALESCE(custom_fields, '{}'::jsonb) || $7::jsonb)::text) <= $9
`

// ApplyCompanyEnrichProfile atomically refuses a stale/moved contact. The SQL
// never writes address, consent, verification, membership or source columns.
func (r *CompanyEnrichContactRepository) ApplyCompanyEnrichProfile(ctx context.Context, orgID, id uuid.UUID, expectedAt time.Time, expectedEmail string, patch *models.CompanyEnrichProfilePatch) *errx.Error {
	if orgID == uuid.Nil || id == uuid.Nil || expectedAt.IsZero() || expectedEmail == "" || patch == nil {
		return errx.New(errx.BadRequest, "a current contact snapshot is required")
	}
	fields, xerr := normalizeCustomFields(patch.CustomFields)
	if xerr != nil {
		return xerr
	}
	raw, err := json.Marshal(fields)
	if err != nil || len(raw) > config.MaxContactSize {
		return errx.ErrContactSize
	}
	clean := func(s *string, kind displayname.Kind) *string {
		if s == nil {
			return nil
		}
		value := displayname.Clean(*s, kind)
		if value == "" {
			return nil
		}
		return &value
	}
	tag, err := r.exec(ctx, companyEnrichProfileSQL, orgID, id, expectedAt,
		clean(patch.FirstName, displayname.Person), clean(patch.LastName, displayname.Person), clean(patch.Company, displayname.Workspace),
		raw, expectedEmail, config.MaxContactSize)
	if err != nil {
		db.CaptureError(err, companyEnrichProfileSQL, nil, "CompanyEnrich profile update")
		return errx.InternalError()
	}
	if tag.RowsAffected() != 1 {
		return errx.NewWithIdentifier(errx.Conflict, "companyenrich_contact_changed", "The contact changed or its custom fields exceed the size limit; no enrichment fields were saved")
	}
	return nil
}
