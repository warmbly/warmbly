package jobs

import (
	"context"
	"errors"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/instancesettings"
	"github.com/warmbly/warmbly/internal/jobrun"
)

const (
	// workerHealthSampleKeepDays covers every reader of the samples, the longest of which is one hour.
	workerHealthSampleKeepDays = 7
	// sendResultEffectKeepDays keeps a delivered effect long enough to explain a late duplicate.
	sendResultEffectKeepDays = 7
)

// TableRetentionStore is the prune half of repository.TableRetentionRepository.
type TableRetentionStore interface {
	PruneCampaignLogs(ctx context.Context, before time.Time) (int64, error)
	PruneNotifications(ctx context.Context, before time.Time) (int64, error)
	PruneWebhookDeliveries(ctx context.Context, before time.Time) (int64, error)
	PruneWorkerHealthSamples(ctx context.Context, before time.Time) (int64, error)
	PruneDeliveredSendResultEffects(ctx context.Context, before time.Time) (int64, error)
}

// TableRetentionJob prunes feeds, logs and finished bookkeeping past their
// windows. The windows that bound product history are instance settings.
type TableRetentionJob struct {
	store     TableRetentionStore
	retention RetentionSource
}

// NewTableRetentionJob builds the job; without a settings source the compiled defaults apply.
func NewTableRetentionJob(store TableRetentionStore, retention RetentionSource) *TableRetentionJob {
	return &TableRetentionJob{store: store, retention: retention}
}

// Start runs the prune on every replica's loop; jobrun gives each slot to one of them.
func (j *TableRetentionJob) Start(ctx context.Context, interval time.Duration) {
	if j.store == nil {
		return
	}
	jobrun.Loop(ctx, "table_retention", interval, true, j.Run)
}

// Run executes one pass and keeps going past a failing table.
func (j *TableRetentionJob) Run(ctx context.Context) error {
	windows := instancesettings.DefaultRetention()
	if j.retention != nil {
		windows = j.retention.RetentionWindows(ctx)
	}
	now := time.Now()
	steps := []struct {
		table string
		prune func(context.Context, time.Time) (int64, error)
		days  int
	}{
		{"campaign_logs", j.store.PruneCampaignLogs, windows.CampaignLogDays},
		{"notifications", j.store.PruneNotifications, windows.NotificationDays},
		{"webhook_deliveries", j.store.PruneWebhookDeliveries, windows.WebhookDeliveryDays},
		{"worker_health_samples", j.store.PruneWorkerHealthSamples, workerHealthSampleKeepDays},
		{"send_result_effects", j.store.PruneDeliveredSendResultEffects, sendResultEffectKeepDays},
	}
	var failures []error
	for _, s := range steps {
		if s.days <= 0 {
			continue
		}
		n, err := s.prune(ctx, now.AddDate(0, 0, -s.days))
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			failures = append(failures, err)
			log.Warn().Err(err).Str("table", s.table).Msg("table retention: prune failed")
			continue
		}
		if n > 0 {
			log.Info().Int64("deleted", n).Str("table", s.table).Msg("table retention: pruned")
		}
	}
	return errors.Join(failures...)
}
