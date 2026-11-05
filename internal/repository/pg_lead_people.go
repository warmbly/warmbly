package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/warmbly/warmbly/internal/models"
)

// ErrLeadPersonExists is returned when a rename lands on someone already on the lead.
var ErrLeadPersonExists = errors.New("this person is already on the lead")

// SaveSite stores what is known about a lead's website without touching the
// rest of its enrichment.
func (r *leadFinderRepository) SaveSite(ctx context.Context, orgID, leadID uuid.UUID, site models.LeadSite) error {
	_, err := r.db.Exec(ctx, `UPDATE leads SET site = $3, updated_at = NOW() WHERE organization_id = $1 AND id = $2`,
		orgID, leadID, jsonOr(site, "{}"))
	return err
}

const leadPersonColumns = `id, organization_id, lead_id, name, name_key, first_name, last_name, title, seniority, email,
	email_source, email_status, email_confidence, linkedin_url, phone, source, source_url, contact_id, created_at, updated_at`

// leadPersonOrder lists the people who decide first, then those with an address.
const leadPersonOrder = `CASE seniority WHEN 'owner' THEN 0 WHEN 'executive' THEN 1 WHEN 'manager' THEN 2 ELSE 3 END,
	email = '', email_confidence DESC, name`

func scanLeadPerson(row pgx.Row) (models.LeadPerson, error) {
	var p models.LeadPerson
	var confidence int16
	err := row.Scan(&p.ID, &p.OrganizationID, &p.LeadID, &p.Name, &p.NameKey, &p.FirstName, &p.LastName, &p.Title,
		&p.Seniority, &p.Email, &p.EmailSource, &p.EmailStatus, &confidence, &p.LinkedInURL, &p.Phone, &p.Source,
		&p.SourceURL, &p.ContactID, &p.CreatedAt, &p.UpdatedAt)
	p.EmailConfidence = int(confidence)
	return p, err
}

func (r *leadFinderRepository) PeopleForLeads(ctx context.Context, orgID uuid.UUID, leadIDs []uuid.UUID) (map[uuid.UUID][]models.LeadPerson, error) {
	out := map[uuid.UUID][]models.LeadPerson{}
	if len(leadIDs) == 0 {
		return out, nil
	}
	rows, err := r.db.Query(ctx, `SELECT `+leadPersonColumns+` FROM lead_people
		WHERE organization_id = $1 AND lead_id = ANY($2) ORDER BY lead_id, `+leadPersonOrder, orgID, leadIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		p, err := scanLeadPerson(rows)
		if err != nil {
			return nil, err
		}
		out[p.LeadID] = append(out[p.LeadID], p)
	}
	return out, rows.Err()
}

func (r *leadFinderRepository) GetPerson(ctx context.Context, orgID, leadID, id uuid.UUID) (*models.LeadPerson, error) {
	p, err := scanLeadPerson(r.db.QueryRow(ctx, `SELECT `+leadPersonColumns+` FROM lead_people
		WHERE organization_id = $1 AND lead_id = $2 AND id = $3`, orgID, leadID, id))
	if isNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// SavePeople writes a lead's people. A person with an id is updated as given;
// anyone else is inserted, or merged onto the row already holding their name
// without overwriting what a teammate typed in. Returns ErrLeadNotFound when
// the lead is not the organization's.
func (r *leadFinderRepository) SavePeople(ctx context.Context, orgID, leadID uuid.UUID, people []models.LeadPerson) ([]models.LeadPerson, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var owned bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM leads WHERE organization_id = $1 AND id = $2)`, orgID, leadID).Scan(&owned); err != nil {
		return nil, err
	}
	if !owned {
		return nil, ErrLeadNotFound
	}
	out := make([]models.LeadPerson, 0, len(people))
	for _, p := range people {
		if p.NameKey == "" || p.Name == "" {
			continue
		}
		args := []any{orgID, leadID, truncateRunes(p.Name, 120), truncateRunes(p.NameKey, 120), truncateRunes(p.FirstName, 60),
			truncateRunes(p.LastName, 60), truncateRunes(p.Title, 120), p.Seniority, truncateRunes(p.Email, 254), p.EmailSource,
			p.EmailStatus, p.EmailConfidence, truncateRunes(p.LinkedInURL, 300), truncateRunes(p.Phone, 40), p.Source,
			truncateRunes(p.SourceURL, 500)}
		var saved models.LeadPerson
		if p.ID != uuid.Nil {
			saved, err = scanLeadPerson(tx.QueryRow(ctx, `
				UPDATE lead_people SET name = $3, name_key = $4, first_name = $5, last_name = $6, title = $7, seniority = $8,
					email = $9, email_source = $10, email_status = $11, email_confidence = $12, linkedin_url = $13, phone = $14,
					source = $15, source_url = $16, updated_at = NOW()
				WHERE organization_id = $1 AND lead_id = $2 AND id = $17
				RETURNING `+leadPersonColumns, append(args, p.ID)...))
		} else {
			// A typed-in address is never replaced; a guess gives way to anything firmer.
			saved, err = scanLeadPerson(tx.QueryRow(ctx, `
				INSERT INTO lead_people AS cur (organization_id, lead_id, name, name_key, first_name, last_name, title, seniority,
					email, email_source, email_status, email_confidence, linkedin_url, phone, source, source_url)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
				ON CONFLICT (lead_id, name_key) DO UPDATE SET
					title = CASE WHEN cur.title = '' THEN EXCLUDED.title ELSE cur.title END,
					seniority = CASE WHEN cur.title = '' THEN EXCLUDED.seniority ELSE cur.seniority END,
					email = CASE WHEN `+replaceEmailSQL+` THEN EXCLUDED.email ELSE cur.email END,
					email_source = CASE WHEN `+replaceEmailSQL+` THEN EXCLUDED.email_source ELSE cur.email_source END,
					email_status = CASE WHEN `+replaceEmailSQL+` THEN EXCLUDED.email_status ELSE cur.email_status END,
					email_confidence = CASE WHEN `+replaceEmailSQL+` THEN EXCLUDED.email_confidence ELSE cur.email_confidence END,
					linkedin_url = COALESCE(NULLIF(cur.linkedin_url, ''), EXCLUDED.linkedin_url),
					phone = COALESCE(NULLIF(cur.phone, ''), EXCLUDED.phone),
					source_url = COALESCE(NULLIF(cur.source_url, ''), EXCLUDED.source_url),
					updated_at = NOW()
				RETURNING `+leadPersonColumns, args...))
		}
		if isNoRows(err) {
			continue
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrLeadPersonExists
		}
		if err != nil {
			return nil, err
		}
		out = append(out, saved)
	}
	return out, tx.Commit(ctx)
}

// replaceEmailSQL decides whether an incoming address takes over a stored one.
const replaceEmailSQL = `EXCLUDED.email <> '' AND cur.email_source <> 'manual' AND (cur.email = ''
	OR (cur.email_source = 'pattern' AND (EXCLUDED.email_source <> 'pattern' OR EXCLUDED.email_confidence >= cur.email_confidence)))`

func (r *leadFinderRepository) DeletePerson(ctx context.Context, orgID, leadID, id uuid.UUID) (bool, error) {
	tag, err := r.db.Exec(ctx, `DELETE FROM lead_people WHERE organization_id = $1 AND lead_id = $2 AND id = $3`, orgID, leadID, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// LinkPersonContacts records which contact each person became.
func (r *leadFinderRepository) LinkPersonContacts(ctx context.Context, orgID uuid.UUID, links map[uuid.UUID]uuid.UUID) error {
	if len(links) == 0 {
		return nil
	}
	personIDs := make([]uuid.UUID, 0, len(links))
	contactIDs := make([]uuid.UUID, 0, len(links))
	for p, c := range links {
		personIDs = append(personIDs, p)
		contactIDs = append(contactIDs, c)
	}
	_, err := r.db.Exec(ctx, `
		UPDATE lead_people p SET contact_id = m.contact_id, updated_at = NOW()
		FROM UNNEST($2::uuid[], $3::uuid[]) AS m(person_id, contact_id)
		WHERE p.organization_id = $1 AND p.id = m.person_id
		  AND EXISTS (SELECT 1 FROM contacts c WHERE c.id = m.contact_id AND c.organization_id = $1)`,
		orgID, personIDs, contactIDs)
	return err
}
