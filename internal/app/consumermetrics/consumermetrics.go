// Package consumermetrics publishes the worker-event backlog to CloudWatch,
// the signal the consumer's ECS service scales on.
// Optional: a deployment that does not set CONSUMER_METRICS_CLOUDWATCH runs none of it.
package consumermetrics

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/infrastructure/eventbus"
	"github.com/warmbly/warmbly/internal/infrastructure/kafka"
)

// Namespace and BacklogMetric are the contract with the scaling alarms in deploy/terraform/aws/runtime.
const (
	Namespace = "Warmbly/Consumer"
	// BacklogMetric is worker events published and not yet taken by any consumer task.
	BacklogMetric = "WorkerEventBacklog"
	interval      = time.Minute
)

// Name is CONSUMER_METRICS_CLOUDWATCH, the Consumer dimension; empty publishes nothing.
func Name() string {
	return strings.TrimSpace(os.Getenv("CONSUMER_METRICS_CLOUDWATCH"))
}

// Backlog reads the consumer group's backlog off the bus; false when the bus cannot say.
func Backlog(ctx context.Context, bus eventbus.Diagnostics) (int64, bool) {
	out := bus.Diagnose(ctx, time.Now(), []eventbus.DiagnosticScope{{ID: "worker_events", Group: "consumer-group", Topics: []string{kafka.TopicWorkerEvents}}})
	for _, m := range out.Metrics {
		// NATS reports undelivered messages, Kafka the committed lag.
		if (m.ID == "pending" || m.ID == "committed_lag") && m.Count != nil {
			return *m.Count, true
		}
	}
	return 0, false
}

// Run publishes the backlog every minute until ctx ends. An unknown backlog
// publishes nothing, which neither grows nor shrinks the service.
func Run(ctx context.Context, bus eventbus.Diagnostics, name string) error {
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("consumer metrics: aws config: %w", err)
	}
	client := cloudwatch.NewFromConfig(cfg)
	dimension := []types.Dimension{{Name: aws.String("Consumer"), Value: aws.String(name)}}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	publish := func() {
		// A stalled read or request must not hold back the next minute's sample.
		ctx, cancel := context.WithTimeout(ctx, interval)
		defer cancel()
		backlog, ok := Backlog(ctx, bus)
		if !ok {
			log.Warn().Msg("worker event backlog unknown; not published")
			return
		}
		if _, err := client.PutMetricData(ctx, &cloudwatch.PutMetricDataInput{
			Namespace: aws.String(Namespace),
			MetricData: []types.MetricDatum{{
				MetricName: aws.String(BacklogMetric), Dimensions: dimension, Unit: types.StandardUnitCount, Value: aws.Float64(float64(backlog)),
			}},
		}); err != nil {
			log.Warn().Err(err).Msg("worker event backlog not published")
		}
	}
	for {
		publish()
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
