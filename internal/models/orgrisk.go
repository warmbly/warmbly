package models

import (
	"time"

	"github.com/google/uuid"
)

// OrgRiskState is an organization's fused abuse posture. The bands mirror the
// warmup participant health machine, so operators read one vocabulary.
type OrgRiskState string

const (
	// OrgRiskTrusted is the default. Nothing is restricted.
	OrgRiskTrusted OrgRiskState = "trusted"
	// OrgRiskWatch changes nothing a customer can feel. It exists so evidence
	// accumulates before anything is taken away.
	OrgRiskWatch OrgRiskState = "watch"
	// OrgRiskRestricted lowers send caps and forces the free warmup pool, so a
	// risky organization cannot spend the paid pool's shared reputation.
	OrgRiskRestricted OrgRiskState = "restricted"
	// OrgRiskSuspended stops sending entirely, pending review.
	OrgRiskSuspended OrgRiskState = "suspended"
)

// OrgRiskCapMultiplier is how much a band lowers an organization's per-mailbox
// cold cap. Applied by min() only, so it can never raise one.
func (s OrgRiskState) CapMultiplier() float64 {
	switch s {
	case OrgRiskRestricted:
		return 0.25
	case OrgRiskSuspended:
		return 0
	default:
		return 1
	}
}

// BlocksSending reports whether the band stops outbound mail entirely.
func (s OrgRiskState) BlocksSending() bool { return s == OrgRiskSuspended }

// ForcesFreeWarmupPool reports whether the band bars the paid warmup pool.
func (s OrgRiskState) ForcesFreeWarmupPool() bool {
	return s == OrgRiskRestricted || s == OrgRiskSuspended
}

// Valid reports whether s is a band the database will accept.
func (s OrgRiskState) Valid() bool {
	switch s {
	case OrgRiskTrusted, OrgRiskWatch, OrgRiskRestricted, OrgRiskSuspended:
		return true
	}
	return false
}

// OrgRisk is an organization's risk record.
type OrgRisk struct {
	OrganizationID uuid.UUID    `json:"organization_id"`
	State          OrgRiskState `json:"state"`
	Score          int          `json:"score"`
	Reason         string       `json:"reason,omitempty"`
	// Signals is append-only evidence for admin review. It never decides the
	// band; Score and State do.
	Signals     map[string]any `json:"signals,omitempty"`
	EvaluatedAt *time.Time     `json:"evaluated_at,omitempty"`
}

// Restricted reports whether the organization is feeling any restriction.
func (r *OrgRisk) Restricted() bool {
	return r != nil && (r.State == OrgRiskRestricted || r.State == OrgRiskSuspended)
}
