package repository

import (
	"context"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/infrastructure/db"
)

// CampaignAudience is one campaign's list quality, measured on demand rather
// than from the advisor's periodic rollup. A launch gate has to see the list as
// it is now, not as it was at the last sweep.
type CampaignAudience struct {
	Total int
	// Verification counts, restricted to DELIVERABLE leads. Counting an
	// invalid address that is also suppressed would put it in the numerator
	// while Deliverable excludes it from the denominator, which can push the
	// projected rate above 100%.
	Invalid  int
	Risky    int
	Unknown  int
	CatchAll int
	// Suppressed and Unsubscribed overlap: a contact can be both. Deliverable
	// is counted separately in SQL rather than subtracted from Total, because
	// subtracting two overlapping counts double-removes those contacts and
	// inflates every share computed against the remainder.
	Suppressed   int
	Unsubscribed int
	// Deliverable is the count that will actually be sent to.
	Deliverable int
	RoleAddress int
	FreeMail    int
}

// CampaignAudienceRepository reads a campaign's audience for the launch gate.
type CampaignAudienceRepository interface {
	GetCampaignAudience(ctx context.Context, orgID, campaignID uuid.UUID) (CampaignAudience, error)
}

type campaignAudienceRepository struct {
	DB *db.DB
}

func NewCampaignAudienceRepository(database *db.DB) CampaignAudienceRepository {
	return &campaignAudienceRepository{DB: database}
}

func (r *campaignAudienceRepository) GetCampaignAudience(ctx context.Context, orgID, campaignID uuid.UUID) (CampaignAudience, error) {
	var a CampaignAudience
	// Same role and free-mail vocabularies the advisor uses, so a customer is
	// not told two different numbers for the same list.
	// sendable is the same predicate Deliverable counts, reused so every
	// verification count shares one denominator.
	const sendable = `ct.subscribed IS NOT FALSE AND NOT EXISTS (
		SELECT 1 FROM suppressed_recipients sr
		WHERE sr.organization_id = $1 AND lower(sr.email) = lower(ct.email)
		  AND (sr.expires_at IS NULL OR sr.expires_at > NOW())
	)`
	err := r.DB.Pool.QueryRow(ctx, `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE `+sendable+` AND ct.verification_status = 'invalid'),
			COUNT(*) FILTER (WHERE `+sendable+` AND ct.verification_status = 'risky'),
			-- 'unknown' is the column default, so this is every address nobody
			-- has checked. NOT NULL, so no null branch is needed.
			COUNT(*) FILTER (WHERE `+sendable+` AND ct.verification_status NOT IN ('valid','invalid','risky')),
			COUNT(*) FILTER (WHERE `+sendable+` AND ct.is_catch_all),
			COUNT(*) FILTER (WHERE EXISTS (
				SELECT 1 FROM suppressed_recipients sr
				WHERE sr.organization_id = $1 AND lower(sr.email) = lower(ct.email)
				  AND (sr.expires_at IS NULL OR sr.expires_at > NOW())
			)),
			COUNT(*) FILTER (WHERE ct.subscribed IS FALSE),
			COUNT(*) FILTER (WHERE `+sendable+`),
			COUNT(*) FILTER (WHERE split_part(lower(ct.email), '@', 1) IN (`+rolePrefixesSQL+`)),
			COUNT(*) FILTER (WHERE split_part(lower(ct.email), '@', 2) IN (`+freeMailDomainsSQL+`))
		FROM campaign_leads cl
		JOIN contacts ct ON ct.id = cl.contact_id
		JOIN campaigns c ON c.id = cl.campaign_id
		WHERE cl.campaign_id = $2 AND c.organization_id = $1
	`, orgID, campaignID).Scan(
		&a.Total, &a.Invalid, &a.Risky, &a.Unknown, &a.CatchAll,
		&a.Suppressed, &a.Unsubscribed, &a.Deliverable, &a.RoleAddress, &a.FreeMail,
	)
	return a, err
}
