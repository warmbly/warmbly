package consumermetrics

import (
	"context"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/infrastructure/eventbus"
	"github.com/warmbly/warmbly/internal/models"
)

type diagnostics []models.MonitoringMetric

func (d diagnostics) Diagnose(context.Context, time.Time, []eventbus.DiagnosticScope) models.MonitoringSource {
	return models.MonitoringSource{Metrics: d}
}

func count(n int64) *int64 { return &n }

func TestBacklogReadsEachBusesUndeliveredCount(t *testing.T) {
	cases := map[string]struct {
		metrics diagnostics
		want    int64
		known   bool
	}{
		"nats":        {diagnostics{{ID: "ack_pending", Count: count(800)}, {ID: "pending", Count: count(1234)}}, 1234, true},
		"kafka":       {diagnostics{{ID: "committed_lag", Count: count(56)}, {ID: "members", Count: count(2)}}, 56, true},
		"idle":        {diagnostics{{ID: "pending", Count: count(0)}}, 0, true},
		"unavailable": {diagnostics{{ID: "broker_queue"}}, 0, false},
		"no count":    {diagnostics{{ID: "pending"}}, 0, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, known := Backlog(context.Background(), tc.metrics)
			if got != tc.want || known != tc.known {
				t.Fatalf("Backlog = %d, %v; want %d, %v", got, known, tc.want, tc.known)
			}
		})
	}
}
