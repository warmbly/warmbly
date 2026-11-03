// Package fleetmetrics publishes the worker fleet's planned utilization to
// CloudWatch, the signal an AWS Auto Scaling Group scales the fleet on.
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

const (
	// Namespace and MetricName are the contract with the Auto Scaling policy in deploy/terraform/aws/workers.
	Namespace  = "Warmbly/Fleet"
	MetricName = "WorkerUtilizationPercent"
	interval   = time.Minute
)

// FleetName is FLEET_METRICS_CLOUDWATCH, the Fleet dimension; empty publishes nothing.
func FleetName() string {
	return strings.TrimSpace(os.Getenv("FLEET_METRICS_CLOUDWATCH"))
}

// Utilization is active mailboxes on live workers over the live workers' summed capacity target.
func Utilization(ctx context.Context, d *db.DB) (float64, error) {
	var load, capacity float64
	err := d.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM email_accounts a JOIN fleet_nodes n ON n.id = a.worker_id
		  WHERE a.status = 'active' AND n.role = 'worker' AND n.active AND n.last_seen_at > now() - $1::interval)::float8,
		(SELECT COALESCE(sum(n.capacity_target), 0) FROM fleet_nodes n
		  WHERE n.role = 'worker' AND n.active AND n.last_seen_at > now() - $1::interval)::float8`,
		repository.WorkerLivenessWindow).Scan(&load, &capacity)
	if err != nil {
		return 0, fmt.Errorf("fleet utilization: %w", err)
	}
	if capacity <= 0 {
		// No live worker: report full so the fleet scales out rather than in.
		return 100, nil
	}
	return load * 100 / capacity, nil
}

// Run publishes the fleet's utilization every minute until ctx ends.
func Run(ctx context.Context, d *db.DB, fleet string) error {
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("fleet metrics: aws config: %w", err)
	}
	client := cloudwatch.NewFromConfig(cfg)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if value, err := Utilization(ctx, d); err != nil {
			log.Warn().Err(err).Msg("fleet utilization not published")
		} else if _, err := client.PutMetricData(ctx, &cloudwatch.PutMetricDataInput{
			Namespace: aws.String(Namespace),
			MetricData: []types.MetricDatum{{
				MetricName: aws.String(MetricName),
				Dimensions: []types.Dimension{{Name: aws.String("Fleet"), Value: aws.String(fleet)}},
				Unit:       types.StandardUnitPercent,
				Value:      aws.Float64(value),
			}},
		}); err != nil {
			log.Warn().Err(err).Msg("fleet utilization not published")
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
