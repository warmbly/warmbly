package jobs

import (
	"context"
	"time"

	"github.com/warmbly/warmbly/internal/jobrun"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/repository"
)

// WebsiteTrackingRetentionJob prunes page views past each workspace's own window.
type WebsiteTrackingRetentionJob struct {
	repo repository.WebsiteTrackingRepository
}

func NewWebsiteTrackingRetentionJob(repo repository.WebsiteTrackingRepository) *WebsiteTrackingRetentionJob {
	return &WebsiteTrackingRetentionJob{repo: repo}
}

// Run prunes every workspace; one failure does not stop the pass.
func (j *WebsiteTrackingRetentionJob) Run(ctx context.Context) error {
	if j.repo == nil {
		return nil
	}
	cutoffs, err := j.repo.RetentionCutoffs(ctx, time.Now())
	if err != nil {
		errs.CaptureException(err)
		return err
	}
	var last error
	for _, c := range cutoffs {
		if _, err := j.repo.PruneBefore(ctx, c.OrganizationID, c.Before); err != nil {
			errs.CaptureException(err)
			last = err
		}
	}
	return last
}

// Start runs the job once on boot and then on the interval until ctx ends.
func (j *WebsiteTrackingRetentionJob) Start(ctx context.Context, interval time.Duration) {
	jobrun.Loop(ctx, "website_tracking_retention", interval, true, j.Run)
}
