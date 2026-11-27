package repository

import (
	"context"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// errInvalidSocial is a social handle or consent basis that does not fit its channel.
func errInvalidSocial(err error) *errx.Error {
	return errx.NewWithIdentifier(errx.BadRequest, "invalid_social_handle", "Invalid social identity: "+err.Error()+".")
}

// contactSocialPatch is a contact's validated social patch: rows to write and channels to clear.
type contactSocialPatch struct {
	set   []models.SocialIdentityRow
	clear []string
}

func (p contactSocialPatch) empty() bool { return len(p.set) == 0 && len(p.clear) == 0 }

func parseContactSocial(p models.ContactSocialPatch, fields map[string]string) (contactSocialPatch, *errx.Error) {
	set, clear, err := models.NormalizeSocialPatch(models.WithLinkedInField(p, fields))
	if err != nil {
		return contactSocialPatch{}, errInvalidSocial(err)
	}
	return contactSocialPatch{set: set, clear: clear}, nil
}

// writeContactSocial applies a patch to one of the workspace's contacts. enrich keeps a stored consent basis the
// patch leaves blank, as a re-import enriches rather than erases.
func writeContactSocial(ctx context.Context, q sendResultDB, orgID, contactID uuid.UUID, p contactSocialPatch, enrich bool) error {
	if len(p.clear) > 0 {
		query := `DELETE FROM contact_social_identities WHERE organization_id = $1 AND contact_id = $2 AND channel = ANY($3)`
		if _, err := q.Exec(ctx, query, orgID, contactID, p.clear); err != nil {
			db.CaptureError(err, query, nil, "exec")
			return err
		}
	}
	for _, row := range p.set {
		query := `INSERT INTO contact_social_identities (contact_id, channel, organization_id, handle, normalized,
				consent_basis, consent_recorded_at)
			SELECT c.id, $3, c.organization_id, $4, $5, $6, CASE WHEN $6::text IS NULL THEN NULL ELSE now() END
			FROM contacts c WHERE c.id = $2 AND c.organization_id = $1
			ON CONFLICT (contact_id, channel) DO UPDATE SET handle = EXCLUDED.handle, normalized = EXCLUDED.normalized,
				consent_basis = CASE WHEN $7 AND EXCLUDED.consent_basis IS NULL THEN contact_social_identities.consent_basis
					ELSE EXCLUDED.consent_basis END,
				consent_recorded_at = CASE
					WHEN $7 AND EXCLUDED.consent_basis IS NULL THEN contact_social_identities.consent_recorded_at
					WHEN EXCLUDED.consent_basis IS NOT DISTINCT FROM contact_social_identities.consent_basis
						THEN contact_social_identities.consent_recorded_at
					ELSE EXCLUDED.consent_recorded_at END,
				updated_at = now()
			WHERE contact_social_identities.organization_id = $1`
		if _, err := q.Exec(ctx, query, orgID, contactID, row.Channel, row.Handle, row.Normalized, row.ConsentBasis, enrich); err != nil {
			db.CaptureError(err, query, nil, "exec")
			return err
		}
	}
	return nil
}

// loadContactSocial is the identities of contacts by id; orgID narrows it when the caller has one.
func loadContactSocial(ctx context.Context, q sendResultDB, orgID *uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]models.ContactSocial, error) {
	out := make(map[uuid.UUID]models.ContactSocial, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	query := `SELECT contact_id, channel, handle, consent_basis, consent_recorded_at FROM contact_social_identities
		WHERE contact_id = ANY($1) AND ($2::uuid IS NULL OR organization_id = $2)`
	rows, err := q.Query(ctx, query, ids, orgID)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var ch string
		var v models.ContactSocialIdentity
		if err := rows.Scan(&id, &ch, &v.Handle, &v.ConsentBasis, &v.ConsentRecordedAt); err != nil {
			return nil, err
		}
		if out[id] == nil {
			out[id] = models.ContactSocial{}
		}
		out[id][ch] = v
	}
	return out, rows.Err()
}

// attachContactSocial fills Social on each contact; a failed read leaves them empty rather than failing the read.
func attachContactSocial(ctx context.Context, q sendResultDB, orgID *uuid.UUID, contacts []*models.Contact) {
	ids := make([]uuid.UUID, 0, len(contacts))
	for _, c := range contacts {
		c.Social = models.ContactSocial{}
		ids = append(ids, c.ID)
	}
	got, err := loadContactSocial(ctx, q, orgID, ids)
	if err != nil {
		return
	}
	for _, c := range contacts {
		if s, ok := got[c.ID]; ok {
			c.Social = s
		}
	}
}

// attachContactSocialSlice is attachContactSocial over a slice of values.
func attachContactSocialSlice(ctx context.Context, q sendResultDB, orgID *uuid.UUID, contacts []models.Contact) {
	ptrs := make([]*models.Contact, len(contacts))
	for i := range contacts {
		ptrs[i] = &contacts[i]
	}
	attachContactSocial(ctx, q, orgID, ptrs)
}
