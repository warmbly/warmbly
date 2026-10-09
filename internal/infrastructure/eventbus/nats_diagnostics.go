package eventbus

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/warmbly/warmbly/internal/models"
)

func natsDiagnosticReason(err error) string {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "timeout"
	}
	if errors.Is(err, jetstream.ErrStreamNotFound) || errors.Is(err, jetstream.ErrConsumerNotFound) {
		return "resource_absent"
	}
	if strings.Contains(strings.ToLower(err.Error()), "permission") || strings.Contains(strings.ToLower(err.Error()), "authorization") {
		return "permission_denied"
	}
	return "collection_failed"
}

func (b *NATSBus) Diagnose(ctx context.Context, at time.Time, scopes []DiagnosticScope) models.MonitoringSource {
	out := models.MonitoringSource{Metrics: []models.MonitoringMetric{}}
	var measured int64
	for _, scope := range scopes {
		if b == nil || b.js == nil || b.nc == nil || !b.nc.IsConnected() {
			out.Metrics = append(out.Metrics, diagnosticUnavailable(scope, at, "dependency_missing"))
			continue
		}
		consumer, err := b.js.Consumer(ctx, b.stream, b.durable(scope.Group, scope.Topics))
		if err != nil {
			out.Metrics = append(out.Metrics, diagnosticUnavailable(scope, at, natsDiagnosticReason(err)))
			continue
		}
		info, err := consumer.Info(ctx)
		if err != nil {
			out.Metrics = append(out.Metrics, diagnosticUnavailable(scope, at, natsDiagnosticReason(err)))
			continue
		}
		if info == nil {
			out.Metrics = append(out.Metrics, diagnosticUnavailable(scope, at, "collection_failed"))
			continue
		}
		if info.NumPending > uint64(^uint64(0)>>1) || info.NumAckPending < 0 || info.NumRedelivered < 0 {
			out.Metrics = append(out.Metrics, diagnosticUnavailable(scope, at, "collection_failed"))
			continue
		}
		measured++
		out.Metrics = append(out.Metrics,
			diagnosticMetric(scope, at, "pending", "messages", "JetStream messages awaiting consumer delivery, not Kafka committed lag or provider sends.", int64(info.NumPending)),
			diagnosticMetric(scope, at, "ack_pending", "messages", "Delivered messages waiting for acknowledgement; distinct from pending.", int64(info.NumAckPending)),
			diagnosticMetric(scope, at, "redelivered", "messages", "Current redelivered message count, not a lifetime or recent retry rate.", int64(info.NumRedelivered)),
		)
	}
	diagnosticCoverage(&out, measured, int64(len(scopes)))
	return out
}
