package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrFormsDomainTaken: another organization holds the forms domain verified.
var ErrFormsDomainTaken = errors.New("forms domain verified by another organization")

// formsDomainConstraint is the unique index from migration 000327.
const formsDomainConstraint = "organizations_forms_domain_one_organization"

func isFormsDomainTaken(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == formsDomainConstraint
}

// OrgFormsDomain is one organization's custom forms domain, for the sweep.
type OrgFormsDomain struct {
	OrganizationID uuid.UUID
	Domain         string
	Verified       bool
}

func (r *organizationRepository) GetFormsDomain(ctx context.Context, orgID uuid.UUID) (string, bool, error) {
	var domain string
	var verified bool
	err := r.db.QueryRow(ctx, `
		SELECT forms_domain, forms_domain_verified FROM organizations WHERE id = $1
	`, orgID).Scan(&domain, &verified)
	if err != nil {
		return "", false, err
	}
	return domain, verified, nil
}

// SetFormsDomain stores a value and clears the verdict when the name changed; saving the same name keeps it.
func (r *organizationRepository) SetFormsDomain(ctx context.Context, orgID uuid.UUID, domain string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE organizations
		SET forms_domain = $2,
		    forms_domain_verified = forms_domain_verified AND forms_domain = $2,
		    forms_domain_verified_at = CASE WHEN forms_domain = $2 THEN forms_domain_verified_at END,
		    updated_at = now()
		WHERE id = $1
	`, orgID, domain)
	return err
}

// SetFormsDomainVerified records a verdict; ErrFormsDomainTaken when another organization holds the domain verified.
func (r *organizationRepository) SetFormsDomainVerified(ctx context.Context, orgID uuid.UUID, verified bool, at *time.Time) error {
	_, err := r.db.Exec(ctx, `
		UPDATE organizations
		SET forms_domain_verified = $2, forms_domain_verified_at = $3, updated_at = now()
		WHERE id = $1
	`, orgID, verified, at)
	if isFormsDomainTaken(err) {
		return ErrFormsDomainTaken
	}
	return err
}

// FormsDomainHeldElsewhere reports whether an organization other than orgID has domain verified as a forms, tracking or redirect domain, or an agency holds it as a portal domain.
func (r *organizationRepository) FormsDomainHeldElsewhere(ctx context.Context, orgID uuid.UUID, domain string) (bool, error) {
	if domain == "" {
		return false, nil
	}
	var held bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM organizations
			WHERE forms_domain = $1 AND forms_domain_verified AND id <> $2
			UNION ALL
			SELECT 1 FROM email_accounts
			WHERE tracking_domain = $1 AND tracking_domain_verified AND organization_id IS DISTINCT FROM $2
			UNION ALL
			SELECT 1 FROM campaigns
			WHERE tracking_domain = $1 AND tracking_domain_verified AND organization_id IS DISTINCT FROM $2
			UNION ALL
			SELECT 1 FROM domain_redirects
			WHERE verified AND served_by = 'instance' AND organization_id <> $2
			  AND (domain = $1 OR (include_www AND 'www.' || domain = $1))
			UNION ALL
			SELECT 1 FROM agency_brands
			WHERE portal_domain = $1 AND portal_domain_verified
		)
	`, domain, orgID).Scan(&held)
	return held, err
}

func (r *organizationRepository) ListFormsDomains(ctx context.Context) ([]OrgFormsDomain, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, forms_domain, forms_domain_verified
		FROM organizations
		WHERE forms_domain <> '' AND deletion_scheduled_for IS NULL
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []OrgFormsDomain{}
	for rows.Next() {
		var d OrgFormsDomain
		if err := rows.Scan(&d.OrganizationID, &d.Domain, &d.Verified); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
