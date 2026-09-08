package jobs

import (
	"context"
	"time"

	"github.com/warmbly/warmbly/internal/observability/errs"

	"github.com/warmbly/warmbly/internal/app/placement"
	"github.com/warmbly/warmbly/internal/jobrun"
)

// PlacementPoller reconciles pending seed inbox-placement results: each tick it
// looks up each in-flight test's token in the receiving seed's unibox entries
// and classifies where the probe landed (Inbox / Spam / Promotions / other),
// completing the test once every result resolves or the classify timeout
// passes. It is a thin scheduler around placement.Service.ClassifyPending; all
// policy lives in the service. A ~2-minute tick balances responsiveness against
// the latency of mailbox sync delivering the probe into the unibox.
type PlacementPoller struct {
	svc      placement.Service
	interval time.Duration
	stopCh   chan struct{}
}

// NewPlacementPoller creates the poller.
func NewPlacementPoller(svc placement.Service, interval time.Duration) *PlacementPoller {
	if interval <= 0 {
		interval = 2 * time.Minute
	}
	return &PlacementPoller{
		svc:      svc,
		interval: interval,
		stopCh:   make(chan struct{}),
	}
}

// Run performs one classification pass. Safe to call frequently — ClassifyPending
// no-ops when there are no pending results.
func (p *PlacementPoller) Run(ctx context.Context) error {
	if p.svc == nil {
		return nil
	}
	if err := p.svc.ClassifyPending(ctx); err != nil {
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
