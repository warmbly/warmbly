package jobs

import (
	"context"
	"time"

	"github.com/warmbly/warmbly/internal/observability/errs"

	"github.com/warmbly/warmbly/internal/app/placement"
	"github.com/warmbly/warmbly/internal/jobrun"
)

// PlacementPoller drives placement.Service.Tick: it reads where delivered
// probes landed, closes finished tests, syncs Warmbly Cloud's panel and starts
// due monitors. All policy lives in the service; a short tick keeps a running
// test's panel live without waiting on mailbox sync any longer than it takes.
type PlacementPoller struct {
	svc      placement.Service
	interval time.Duration
	stopCh   chan struct{}
}

// NewPlacementPoller creates the poller.
func NewPlacementPoller(svc placement.Service, interval time.Duration) *PlacementPoller {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &PlacementPoller{
		svc:      svc,
		interval: interval,
		stopCh:   make(chan struct{}),
	}
}

// Run performs one pass. Safe to call often: an idle tick is a handful of
// indexed reads.
func (p *PlacementPoller) Run(ctx context.Context) error {
	if p.svc == nil {
		return nil
	}
	if err := p.svc.Tick(ctx); err != nil {
		errs.CaptureException(err)
		return err
	}
	return nil
}

// Start begins scheduled execution on the configured interval.
func (p *PlacementPoller) Start(ctx context.Context) {
	ctx, cancel := stopContext(ctx, p.stopCh)
	defer cancel()
	jobrun.Loop(ctx, "placement_poller", p.interval, false, p.Run)
}

// Stop halts scheduled execution.
func (p *PlacementPoller) Stop() {
	close(p.stopCh)
}
