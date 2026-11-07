// Package fleetmetrics publishes the worker fleet's resource usage to
// CloudWatch, the signals an AWS Auto Scaling Group scales the fleet on.
// Optional: a deployment that does not set FLEET_METRICS_CLOUDWATCH runs none of it.
package fleetmetrics

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
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/repository"
)

// Namespace and the metric names are the contract with the Auto Scaling policies in deploy/terraform/aws/workers.
const (
	Namespace = "Warmbly/Fleet"
	// MemoryMetric is memory in use across live workers; the fleet scales on it.
	MemoryMetric = "WorkerMemoryPercent"
	// MemoryMaxMetric is the fullest single worker; a hot worker adds one.
	MemoryMaxMetric = "WorkerMemoryMaxPercent"
	// UtilizationMetric is assigned mailboxes over planned capacity, published for dashboards only.
	UtilizationMetric = "WorkerUtilizationPercent"
	interval          = time.Minute
)

// FleetName is FLEET_METRICS_CLOUDWATCH, the Fleet dimension; empty publishes nothing.
func FleetName() string {
	return strings.TrimSpace(os.Getenv("FLEET_METRICS_CLOUDWATCH"))
}

// Usage is what the fleet's live workers last reported. A false Known means no
// live worker reported memory; nothing is published then, which neither grows
// nor shrinks the fleet.
type Usage struct {
	Known            bool
	MemoryPercent    float64
	MemoryMaxPercent float64
	Utilization      float64
}

// Read takes the fleet's usage from the workers' heartbeats.
func Read(ctx context.Context, d *db.DB) (Usage, error) {
	var used, total, hottest, load, capacity float64
	var reporting int
	err := d.QueryRow(ctx, `WITH live AS (
			SELECT n.id, n.capacity_target, n.memory_used_mb, n.memory_limit_mb FROM fleet_nodes n
			WHERE n.role = 'worker' AND n.active AND n.last_seen_at > now() - $1::interval)
		SELECT
			COALESCE(sum(memory_used_mb) FILTER (WHERE memory_limit_mb > 0), 0)::float8,
			COALESCE(sum(memory_limit_mb) FILTER (WHERE memory_limit_mb > 0), 0)::float8,
			COALESCE(max(memory_used_mb::float8 * 100 / memory_limit_mb) FILTER (WHERE memory_limit_mb > 0), 0)::float8,
			count(*) FILTER (WHERE memory_limit_mb > 0),
			(SELECT count(*) FROM email_accounts a JOIN live ON live.id = a.worker_id WHERE a.status = 'active')::float8,
			COALESCE(sum(capacity_target), 0)::float8
		FROM live`, repository.WorkerLivenessWindow).Scan(&used, &total, &hottest, &reporting, &load, &capacity)
	if err != nil {
		return Usage{}, fmt.Errorf("fleet usage: %w", err)
	}
	if reporting == 0 || total <= 0 {
		return Usage{}, nil
	}
	u := Usage{Known: true, MemoryPercent: used * 100 / total, MemoryMaxPercent: hottest}
	if capacity > 0 {
		u.Utilization = load * 100 / capacity
	}
	return u, nil
}

// Run publishes the fleet's usage every minute until ctx ends.
func Run(ctx context.Context, d *db.DB, fleet string) error {
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("fleet metrics: aws config: %w", err)
	}
	client := cloudwatch.NewFromConfig(cfg)
	dimension := []types.Dimension{{Name: aws.String("Fleet"), Value: aws.String(fleet)}}
	datum := func(name string, value float64) types.MetricDatum {
		return types.MetricDatum{MetricName: aws.String(name), Dimensions: dimension, Unit: types.StandardUnitPercent, Value: aws.Float64(value)}
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	publish := func() {
		// A stalled read or request must not hold back the next minute's sample.
		ctx, cancel := context.WithTimeout(ctx, interval)
		defer cancel()
		if u, err := Read(ctx, d); err != nil {
			log.Warn().Err(err).Msg("fleet usage not published")
		} else if !u.Known {
			log.Debug().Msg("no live worker reported memory; fleet usage not published")
		} else if _, err := client.PutMetricData(ctx, &cloudwatch.PutMetricDataInput{
			Namespace: aws.String(Namespace),
			MetricData: []types.MetricDatum{
				datum(MemoryMetric, u.MemoryPercent),
				datum(MemoryMaxMetric, u.MemoryMaxPercent),
				datum(UtilizationMetric, u.Utilization),
			},
		}); err != nil {
			log.Warn().Err(err).Msg("fleet usage not published")
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
