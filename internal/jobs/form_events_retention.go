package jobs

import (
	"context"
	"time"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/jobrun"
	"github.com/warmbly/warmbly/internal/repository"
)

// FormEventsRetentionJob prunes funnel events past the retention window. The
// window is an instance setting, read on every pass, so an operator who
// shortens it in the admin panel sees the next sweep honour it.
type FormEventsRetentionJob struct {
	repo      repository.FormEventRepository
	retention RetentionSource
}

func NewFormEventsRetentionJob(repo repository.FormEventRepository) *FormEventsRetentionJob {
	return &FormEventsRetentionJob{repo: repo}
}

// WireRetention attaches the instance settings the window is read from. Unset
// keeps the compiled default.
func (j *FormEventsRetentionJob) WireRetention(src RetentionSource) *FormEventsRetentionJob {
	j.retention = src
	return j
}

func (j *FormEventsRetentionJob) Run(ctx context.Context) error {
	if j.repo == nil {
		return nil
	}
	days := config.FormEventsRetentionDaysDefault
	if j.retention != nil {
		days = j.retention.RetentionWindows(ctx).FormEventDays
	}
	before := time.Now().AddDate(0, 0, -days)
	if _, xerr := j.repo.PruneBefore(ctx, before); xerr != nil {
		return xerr
	}
	return nil
}

// Start runs the job once on boot and then on the interval until ctx ends.
func (j *FormEventsRetentionJob) Start(ctx context.Context, interval time.Duration) {
	jobrun.Loop(ctx, "form_events_retention", interval, true, j.Run)
}
