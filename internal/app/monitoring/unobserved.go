package monitoring

import (
	"context"
	"time"

	"github.com/warmbly/warmbly/internal/models"
)

func UnobservedSources() []Source {
	return []Source{
		{ID: "send_wait_attribution", Permission: models.AdminPermViewCampaigns, Collect: func(_ context.Context, _ time.Time) (models.MonitoringSource, error) {
			out := models.MonitoringSource{Availability: models.MonitoringUnavailable, Coverage: "unavailable", Reason: "unsupported", Metrics: []models.MonitoringMetric{}}
			for _, m := range []models.MonitoringMetric{
				{ID: "send_daily_limit_wait", Title: "Live send daily-limit waits", Condition: models.MonitoringDailyLimit},
				{ID: "send_budget_wait", Title: "Live send budget waits", Condition: models.MonitoringBudget},
				{ID: "send_schedule_reason", Title: "Detailed send schedule wait reasons", Condition: models.MonitoringSchedule},
			} {
				m.Unit = "tasks"
				m.Availability = models.MonitoringUnavailable
				m.Severity = "info"
				m.Reason = "unsupported"
				m.Note = "Current scheduler does not persist per-task pacing/daily-cap/budget decisions. Future schedules are observable separately; a specific reason cannot be inferred."
				out.Metrics = append(out.Metrics, m)
			}
			return out, nil
		}},
		{ID: "tracking_broker", Permission: models.AdminPermViewAnalytics, Collect: func(_ context.Context, _ time.Time) (models.MonitoringSource, error) {
			return models.MonitoringSource{Availability: models.MonitoringUnavailable, Coverage: "unavailable", Reason: "unsupported", Metrics: []models.MonitoringMetric{{ID: "tracking_queue", Title: "External tracking consumer backlog", Unit: "messages", Availability: models.MonitoringUnavailable, Condition: models.MonitoringUnknown, Reason: "unsupported", Severity: "info", Note: "This backend does not own the external tracking service's effective consumer scope. Backend worker-events and registered worker commands are measured separately."}}}, nil
		}},
	}
}
