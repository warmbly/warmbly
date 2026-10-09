package repository

import (
	"context"
	"time"

	"github.com/warmbly/warmbly/internal/app/monitoring"
	"github.com/warmbly/warmbly/internal/infrastructure/eventbus"
	"github.com/warmbly/warmbly/internal/infrastructure/kafka"
	"github.com/warmbly/warmbly/internal/models"
)

func (r *MonitoringRepository) BrokerSources(bus eventbus.EventBus) []monitoring.Source {
	collect := func(ctx context.Context, at time.Time, scopes []eventbus.DiagnosticScope) models.MonitoringSource {
		if diagnostics, ok := bus.(eventbus.Diagnostics); ok {
			return diagnostics.Diagnose(ctx, at, scopes)
		}
		return models.MonitoringSource{Availability: models.MonitoringUnavailable, Coverage: "unavailable", Reason: "unsupported"}
	}
	return []monitoring.Source{
		{ID: "backend_event_broker", Permission: models.AdminPermViewAnalytics, Collect: func(ctx context.Context, at time.Time) (models.MonitoringSource, error) {
			return collect(ctx, at, []eventbus.DiagnosticScope{{ID: "worker_events", Group: "consumer-group", Topics: []string{kafka.TopicWorkerEvents}}}), nil
		}},
		{ID: "worker_command_broker", Permission: models.AdminPermViewWorkers, Collect: func(ctx context.Context, at time.Time) (models.MonitoringSource, error) {
			if r == nil || r.pool == nil {
				return models.MonitoringSource{Availability: models.MonitoringUnavailable, Coverage: "unavailable", Reason: "dependency_missing"}, nil
			}
			ids, expected, err := r.MonitoringWorkerIDs(ctx)
			if err != nil {
				return models.MonitoringSource{}, err
			}
			scopes := []eventbus.DiagnosticScope{}
			for _, id := range ids {
				scopes = append(scopes, eventbus.DiagnosticScope{ID: id, Group: "worker-" + id, Topics: []string{kafka.GetWorkerTopic(id)}})
			}
			out := collect(ctx, at, scopes)
			out.ExpectedScopes = &expected
			if int64(len(scopes)) < expected && out.Availability != models.MonitoringUnavailable {
				out.Coverage = "partial"
				out.Reason = "scope_limit"
			}
			for i := range out.Metrics {
				out.Metrics[i].Investigate = "/workers/" + out.Metrics[i].ScopeID
			}
			return out, nil
		}},
	}
}
