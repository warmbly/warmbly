package models

import "time"

type MonitoringAvailability string
type MonitoringCondition string

const (
	MonitoringFresh              MonitoringAvailability = "fresh"
	MonitoringStale              MonitoringAvailability = "stale"
	MonitoringUnavailable        MonitoringAvailability = "unavailable"
	MonitoringHealthy            MonitoringCondition    = "healthy"
	MonitoringUnknown            MonitoringCondition    = "unknown"
	MonitoringNoEvidence         MonitoringCondition    = "no_recent_evidence"
	MonitoringRecentFailure      MonitoringCondition    = "recent_failure"
	MonitoringPersistentFailure  MonitoringCondition    = "persistent_failure"
	MonitoringRecoveryUnverified MonitoringCondition    = "recovery_unverified"
	MonitoringSafetyHold         MonitoringCondition    = "safety_hold"
	MonitoringBacklog            MonitoringCondition    = "backlog"
	MonitoringInFlight           MonitoringCondition    = "in_flight"
	MonitoringSchedule           MonitoringCondition    = "waiting_schedule"
	MonitoringRetry              MonitoringCondition    = "waiting_retry"
	MonitoringBudget             MonitoringCondition    = "budget"
	MonitoringDailyLimit         MonitoringCondition    = "daily_limit"
	MonitoringTelemetryStale     MonitoringCondition    = "stale_telemetry"
)

// MonitoringMetric carries aggregates and optional bounded broker evidence; counts may overlap.
type MonitoringMetric struct {
	Broker                  *MonitoringBrokerObservation `json:"broker,omitempty"`
	ID                      string                       `json:"id"`
	Title                   string                       `json:"title"`
	Unit                    string                       `json:"unit"`
	ScopeID                 string                       `json:"scope_id,omitempty"`
	Availability            MonitoringAvailability       `json:"availability"`
	Condition               MonitoringCondition          `json:"condition"`
	Severity                string                       `json:"severity"`
	Reason                  string                       `json:"reason,omitempty"`
	Note                    string                       `json:"note"`
	Count                   *int64                       `json:"count"`
	AffectedMailboxes       *int64                       `json:"affected_mailboxes"`
	AffectedOrganizations   *int64                       `json:"affected_organizations"`
	UnknownOrganizationRows *int64                       `json:"unknown_organization_rows"`
	ObservedAt              *time.Time                   `json:"observed_at"`
	EvidenceAt              *time.Time                   `json:"evidence_at"`
	LatestEvidenceAt        *time.Time                   `json:"latest_evidence_at"`
	EvidenceAgeSeconds      *int64                       `json:"evidence_age_seconds"`
	NextEligibleAt          *time.Time                   `json:"next_eligible_at"`
	WindowStart             *time.Time                   `json:"window_start"`
	WindowEnd               *time.Time                   `json:"window_end"`
	ThresholdSeconds        *int64                       `json:"threshold_seconds"`
	Investigate             string                       `json:"investigate,omitempty"`
}

type MonitoringBrokerPartition struct {
	Topic        string `json:"topic"`
	Partition    int32  `json:"partition"`
	Committed    int64  `json:"committed"`
	Earliest     int64  `json:"earliest"`
	Latest       int64  `json:"latest"`
	CommittedLag int64  `json:"committed_lag"`
}

type MonitoringBrokerObservation struct {
	ConsumerGroup string                      `json:"consumer_group"`
	Topics        []string                    `json:"topics"`
	Partitions    []MonitoringBrokerPartition `json:"partitions"`
}

type MonitoringSource struct {
	ID             string                 `json:"id"`
	Availability   MonitoringAvailability `json:"availability"`
	Coverage       string                 `json:"coverage"`
	Reason         string                 `json:"reason,omitempty"`
	ObservedAt     *time.Time             `json:"observed_at"`
	CheckedAt      time.Time              `json:"checked_at"`
	MeasuredScopes *int64                 `json:"measured_scopes"`
	ExpectedScopes *int64                 `json:"expected_scopes"`
	Metrics        []MonitoringMetric     `json:"metrics"`
}

type MonitoringSnapshot struct {
	Version      string             `json:"version"`
	CheckedAt    time.Time          `json:"checked_at"`
	RefreshAfter time.Time          `json:"refresh_after"`
	Coverage     string             `json:"coverage"`
	Sources      []MonitoringSource `json:"sources"`
}
