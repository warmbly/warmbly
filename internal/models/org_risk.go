package models

import (
	"time"

	"github.com/google/uuid"
)

type OrganizationRiskState string

const (
	OrganizationRiskTrusted    OrganizationRiskState = "trusted"
	OrganizationRiskWatch      OrganizationRiskState = "watch"
	OrganizationRiskRestricted OrganizationRiskState = "restricted"
	OrganizationRiskSuspended  OrganizationRiskState = "suspended"
)

// OrganizationRiskSignal is the latest evidence for one stable signal key.
type OrganizationRiskSignal struct {
	Score      int            `json:"score"`
	Reason     string         `json:"reason"`
	Evidence   map[string]any `json:"evidence,omitempty"`
	ObservedAt time.Time      `json:"observed_at"`
}

type OrganizationRisk struct {
	OrganizationID uuid.UUID                         `json:"organization_id"`
	State          OrganizationRiskState             `json:"state"`
	Score          int                               `json:"score"`
	Reason         string                            `json:"reason"`
	Signals        map[string]OrganizationRiskSignal `json:"signals"`
	EvaluatedAt    *time.Time                        `json:"evaluated_at,omitempty"`
}

func OrganizationRiskStateForScore(score int) OrganizationRiskState {
	switch {
	case score >= 80:
		return OrganizationRiskSuspended
	case score >= 50:
		return OrganizationRiskRestricted
	case score >= 20:
		return OrganizationRiskWatch
	default:
		return OrganizationRiskTrusted
	}
}

func (r *OrganizationRisk) Has(scope BanScope) bool {
	return scope == BanScopeSend && r != nil && r.State == OrganizationRiskSuspended
}
