package repository

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/infrastructure/db"
)

// OrgConduct is one organization's recent outcomes with recipients: what it
// sent, and how much of it recipients rejected or reported. Rates are left to
// the caller, which applies its own sample floor and bands.
type OrgConduct struct {
	OrganizationID uuid.UUID
	Sent           int
	Bounced        int
	Complained     int
}

// OrgConductRepository reads what an organization's mail did to the people who
// received it. This is the evidence the fused posture may act on; every other
// org-level detector describes how a workspace looks, which an agency and a
// ring share.
type OrgConductRepository interface {
	// OrgRecipientOutcomes returns per-organization send, bounce and complaint
	// counts over a window, for organizations past a sample floor. Below the
	// floor a rate means nothing and the organization is not returned.
	OrgRecipientOutcomes(ctx context.Context, minSent int, window time.Duration) ([]OrgConduct, error)
}

type orgConductRepository struct {
	DB *db.DB
}

func NewOrgConductRepository(database *db.DB) OrgConductRepository {
	return &orgConductRepository{DB: database}
}

func (r *orgConductRepository) OrgRecipientOutcomes(ctx context.Context, minSent int, window time.Duration) ([]OrgConduct, error) {
	if minSent < 1 {
		minSent = 1
	}
	// Numerator and denominator come from the same rows, so a bounce is always
	// a bounce of a send that is in the window. Counting complaints from
	// deliverability_events instead would mix in events whose send is not.
	rows, err := r.DB.Pool.Query(ctx, `
		SELECT c.organization_id,
		       COUNT(*) AS sent,
		       COUNT(*) FILTER (WHERE ccp.bounced_at IS NOT NULL)    AS bounced,
		       COUNT(*) FILTER (WHERE ccp.complained_at IS NOT NULL) AS complained
		  FROM campaign_contact_progress ccp
		  JOIN campaigns c ON c.id = ccp.campaign_id
		 WHERE ccp.sent_at IS NOT NULL
		   AND `+progressIsEmailStep("ccp")+`
		   AND ccp.sent_at >= NOW() - $2::interval
		   AND c.organization_id IS NOT NULL
		 GROUP BY c.organization_id
		HAVING COUNT(*) >= $1
	`, minSent, window.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []OrgConduct
	for rows.Next() {
		var c OrgConduct
		if err := rows.Scan(&c.OrganizationID, &c.Sent, &c.Bounced, &c.Complained); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
