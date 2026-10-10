package eventbus

import (
	"context"
	"time"

	"github.com/warmbly/warmbly/internal/models"
)

type DiagnosticScope struct {
	IncludePartitions bool
	ID                string
	Group             string
	Topics            []string
}

// Diagnostics is optional and does not extend the worker EventBus envelope.
type Diagnostics interface {
	Diagnose(context.Context, time.Time, []DiagnosticScope) models.MonitoringSource
}

func diagnosticUnavailable(scope DiagnosticScope, at time.Time, reason string) models.MonitoringMetric {
	return models.MonitoringMetric{ID: "broker_queue", Title: "Broker queue observation", ScopeID: scope.ID, Unit: "messages", Availability: models.MonitoringUnavailable, Condition: models.MonitoringUnknown, Severity: "info", Reason: reason, ObservedAt: &at, Note: "Unavailable queue observation is not zero backlog or a broker outage."}
}

func diagnosticMetric(scope DiagnosticScope, at time.Time, id, unit, note string, count int64) models.MonitoringMetric {
	return models.MonitoringMetric{ID: id, Title: "Broker " + id, ScopeID: scope.ID, Unit: unit, Availability: models.MonitoringFresh, Condition: models.MonitoringUnknown, Severity: "info", Count: &count, ObservedAt: &at, EvidenceAt: &at, LatestEvidenceAt: &at, Note: note}
}

func diagnosticCoverage(out *models.MonitoringSource, measured, expected int64) {
	out.MeasuredScopes = &measured
	out.ExpectedScopes = &expected
	out.Coverage = "complete"
	if measured < expected {
		out.Coverage = "partial"
	}
	if measured == 0 && expected > 0 {
		out.Availability = models.MonitoringUnavailable
		out.Reason = "collection_failed"
		out.Coverage = "unavailable"
	}
	if expected == 0 {
		out.Coverage = "partial"
		out.Reason = "no_registered_scopes"
	}
}
