package models

import (
	"time"

	"github.com/google/uuid"
)

// PartnershipPlanID is the private $90 Partnership plan; sold only to a workspace holding an offer for it.
var PartnershipPlanID = uuid.MustParse("00000000-0000-0000-0000-000000000003")

// PlanOffer lets one workspace buy a private plan; an operator grants it.
type PlanOffer struct {
	OrganizationID uuid.UUID  `json:"organization_id"`
	PlanID         uuid.UUID  `json:"plan_id"`
	GrantedBy      *uuid.UUID `json:"granted_by,omitempty"`
	Reason         string     `json:"reason"`
	GrantedAt      time.Time  `json:"granted_at"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	Plan           *Plan      `json:"plan,omitempty"`
}

// Active reports an offer still open at now.
func (o *PlanOffer) Active(now time.Time) bool {
	return o != nil && (o.ExpiresAt == nil || now.Before(*o.ExpiresAt))
}
