package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CustomDomainRepository answers the one question the on-demand TLS gate asks:
// is this hostname a custom domain some workspace on this instance has already
// verified? Read-only by construction; nothing here mints or changes a domain.
type CustomDomainRepository interface {
	IsVerified(ctx context.Context, host string) (bool, error)
	// Owners are the organizations holding host verified as a tracking, forms or instance-served redirect domain, or through their agency's portal domain.
	Owners(ctx context.Context, host string) ([]uuid.UUID, error)
}

type customDomainRepository struct {
	db *pgxpool.Pool
}

func NewCustomDomainRepository(db *pgxpool.Pool) CustomDomainRepository {
	return &customDomainRepository{db: db}
}

// IsVerified reports whether host is a verified custom tracking domain (on a
// mailbox or a campaign override), a verified custom forms domain, a verified
// agency portal domain, or a verified root redirect this instance serves itself.
//
// Verified is the whole gate. Verification is a DNS check, so it completes
// before any certificate is needed and there is no ordering problem, and a
// domain that has not passed it is never built into a link, which makes it a
// name nothing legitimate will ever ask for.
//
// Each arm is a partial-index hit (migration 000144) and the query stops at
// the first match.
func (r *customDomainRepository) IsVerified(ctx context.Context, host string) (bool, error) {
	if host == "" {
		return false, nil
	}
	var ok bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM email_accounts
			WHERE tracking_domain = $1 AND tracking_domain_verified
			UNION ALL
			SELECT 1 FROM campaigns
			WHERE tracking_domain = $1 AND tracking_domain_verified
			UNION ALL
			SELECT 1 FROM organizations
			WHERE forms_domain = $1 AND forms_domain_verified
			UNION ALL
			SELECT 1 FROM domain_redirects
			WHERE verified AND served_by = 'instance' AND (domain = $1 OR (include_www AND 'www.' || domain = $1))
			UNION ALL
			SELECT 1 FROM agency_brands
			WHERE portal_domain = $1 AND portal_domain_verified
		)
	`, host).Scan(&ok)
	if err != nil {
		return false, err
	}
	return ok, nil
}

// Owners lists the organizations that hold host verified under any custom-domain kind; an agency portal host is held by every client.
func (r *customDomainRepository) Owners(ctx context.Context, host string) ([]uuid.UUID, error) {
	if host == "" {
		return nil, nil
	}
	rows, err := r.db.Query(ctx, `
		SELECT organization_id FROM email_accounts
		WHERE tracking_domain = $1 AND tracking_domain_verified AND organization_id IS NOT NULL
		UNION
		SELECT organization_id FROM campaigns
		WHERE tracking_domain = $1 AND tracking_domain_verified AND organization_id IS NOT NULL
		UNION
		SELECT id FROM organizations
		WHERE forms_domain = $1 AND forms_domain_verified
		UNION
		SELECT organization_id FROM domain_redirects
		WHERE verified AND served_by = 'instance' AND (domain = $1 OR (include_www AND 'www.' || domain = $1))
		UNION
		SELECT ac.organization_id FROM agency_brands ab
		JOIN agency_clients ac ON ac.agency_id = ab.agency_id
		WHERE ab.portal_domain = $1 AND ab.portal_domain_verified
	`, host)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
