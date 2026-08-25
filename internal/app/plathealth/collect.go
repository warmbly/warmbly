package plathealth

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/infrastructure/eventbus"
)

// ErrUnobserved is returned by an adapter that could not even attempt a check.
var ErrUnobserved = errors.New("unobserved")

// Options wires I/O adapters. Nil funcs are unobserved (fail closed).
type Options struct {
	Timeout          time.Duration
	ReportCacheTTL   time.Duration
	ProviderCacheTTL time.Duration
	Now              func() time.Time
	DB               func(context.Context) error
	Cache            func(context.Context) error
	Bus              eventbus.EventBus
	Heartbeat        func(context.Context) (HeartbeatSnapshot, error)
	Provider         func(context.Context) error
}

// Collector runs adapters with a per-check timeout and evaluates the report.
type Collector struct {
	opts             Options
	reportMu         sync.Mutex
	reportCachedAt   time.Time
	reportCached     Report
	providerMu       sync.Mutex
	providerCachedAt time.Time
	providerCached   Observation
}

// NewCollector returns a collector. Timeout defaults to DefaultTimeout.
func NewCollector(opts Options) *Collector {
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultTimeout
	}
	if opts.ReportCacheTTL == 0 {
		opts.ReportCacheTTL = 5 * time.Second
	} else if opts.ReportCacheTTL < 0 {
		opts.ReportCacheTTL = 0
	}
	if opts.ProviderCacheTTL == 0 {
		opts.ProviderCacheTTL = 5 * time.Minute
	} else if opts.ProviderCacheTTL < 0 {
		opts.ProviderCacheTTL = 0
	}
	if opts.Now == nil {
		opts.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Collector{opts: opts}
}

// Report runs every plane and evaluates readiness. The process is live
// because this method is executing.
func (c *Collector) Report(ctx context.Context) Report {
	c.reportMu.Lock()
	defer c.reportMu.Unlock()
	now := c.opts.Now()
	if c.opts.ReportCacheTTL > 0 && !c.reportCachedAt.IsZero() &&
		now.Sub(c.reportCachedAt) >= 0 && now.Sub(c.reportCachedAt) < c.opts.ReportCacheTTL {
		return c.reportCached
	}
	report := Evaluate(true, c.Observe(ctx), now, c.opts.Timeout)
	c.reportCached = report
	c.reportCachedAt = now
	return report
}

// Observe runs adapters in parallel. One hung plane cannot stall the rest.
func (c *Collector) Observe(ctx context.Context) []Observation {
	obs := make([]Observation, len(RequiredPlanes))
	var wg sync.WaitGroup
	queueIndex := -1
	eventIndex := -1
	for i, plane := range RequiredPlanes {
		switch plane {
		case PlaneQueue:
			queueIndex = i
			continue
		case PlaneEventProcessing:
			eventIndex = i
			continue
		}
		wg.Add(1)
		go func(i int, plane string) {
			defer wg.Done()
			obs[i] = c.observeOne(ctx, plane)
		}(i, plane)
	}
	if queueIndex >= 0 && eventIndex >= 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, c.opts.Timeout)
			defer cancel()
			ingest, consumed := ProbeBusRoundTrip(cctx, c.opts.Bus)
			ingest.Plane = PlaneQueue
			ingest.Required = true
			consumed.Plane = PlaneEventProcessing
			consumed.Required = true
			obs[queueIndex] = ingest
			obs[eventIndex] = consumed
		}()
	}
	wg.Wait()
	return obs
}

func (c *Collector) observeOne(ctx context.Context, plane string) Observation {
	cctx, cancel := context.WithTimeout(ctx, c.opts.Timeout)
	defer cancel()
	start := time.Now()
	ch := make(chan Observation, 1)
	go func() {
		ch <- c.runPlane(cctx, plane)
	}()
	select {
	case o := <-ch:
		if o.LatencyMS == 0 {
			o.LatencyMS = time.Since(start).Milliseconds()
		}
		if errors.Is(cctx.Err(), context.DeadlineExceeded) && !o.OK {
			o.Timeout = true
			if o.Reason == "" {
				o.Reason = ReasonTimeout
			}
		}
		o.Plane = plane
		o.Required = true
		return o
	case <-cctx.Done():
		return Observation{
			Plane:     plane,
			Required:  true,
			Observed:  true,
			Timeout:   true,
			OK:        false,
			LatencyMS: c.opts.Timeout.Milliseconds(),
			Reason:    ReasonTimeout,
		}
	}
}

func (c *Collector) runPlane(ctx context.Context, plane string) Observation {
	switch plane {
	case PlaneControlPlane:
		return Observation{Plane: plane, Required: true, Observed: true, OK: true}
	case PlaneDB:
		return fromErr(plane, c.opts.DB, ctx, ReasonSelectFailed)
	case PlaneCache:
		return fromErr(plane, c.opts.Cache, ctx, ReasonCacheMismatch)
	case PlaneWorkerHeartbeat:
		return c.observeHeartbeat(ctx)
	case PlaneProviderEdge:
		return c.observeProvider(ctx)
	default:
		return Observation{Plane: plane, Required: true, Observed: false, Reason: ReasonUnobserved}
	}
}

func fromErr(plane string, fn func(context.Context) error, ctx context.Context, failReason string) Observation {
	if fn == nil {
		return Observation{Plane: plane, Required: true, Observed: false, Reason: ReasonUnobserved}
	}
	err := fn(ctx)
	if err == nil {
		return Observation{Plane: plane, Required: true, Observed: true, OK: true}
	}
	if errors.Is(err, ErrUnobserved) {
		return Observation{Plane: plane, Required: true, Observed: false, Reason: ReasonUnobserved}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return Observation{Plane: plane, Required: true, Observed: true, Timeout: true, Reason: ReasonTimeout}
	}
	return Observation{Plane: plane, Required: true, Observed: true, OK: false, Reason: failReason}
}

func (c *Collector) observeHeartbeat(ctx context.Context) Observation {
	if c.opts.Heartbeat == nil {
		return Observation{Plane: PlaneWorkerHeartbeat, Required: true, Observed: false, Reason: ReasonUnobserved}
	}
	snap, err := c.opts.Heartbeat(ctx)
	if err != nil {
		if errors.Is(err, ErrUnobserved) {
			return Observation{Plane: PlaneWorkerHeartbeat, Required: true, Observed: false, Reason: ReasonUnobserved}
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return Observation{Plane: PlaneWorkerHeartbeat, Required: true, Observed: true, Timeout: true, Reason: ReasonTimeout}
		}
		return Observation{Plane: PlaneWorkerHeartbeat, Required: true, Observed: true, OK: false, Reason: ReasonFailed}
	}
	if !snap.Observed {
		return Observation{Plane: PlaneWorkerHeartbeat, Required: true, Observed: false, Reason: ReasonUnobserved}
	}
	if snap.Fresh == 0 && snap.Stale > 0 {
		return Observation{Plane: PlaneWorkerHeartbeat, Required: true, Observed: true, Stale: true, Reason: ReasonNewestHeartbeatStale}
	}
	if snap.Fresh == 0 {
		return Observation{Plane: PlaneWorkerHeartbeat, Required: true, Observed: true, OK: false, Reason: ReasonNoFreshHeartbeat}
	}
	return Observation{Plane: PlaneWorkerHeartbeat, Required: true, Observed: true, OK: true}
}

func (c *Collector) observeProvider(ctx context.Context) Observation {
	c.providerMu.Lock()
	defer c.providerMu.Unlock()
	now := c.opts.Now()
	if c.opts.ProviderCacheTTL > 0 && !c.providerCachedAt.IsZero() &&
		now.Sub(c.providerCachedAt) >= 0 && now.Sub(c.providerCachedAt) < c.opts.ProviderCacheTTL {
		return c.providerCached
	}
	observed := fromErr(PlaneProviderEdge, c.opts.Provider, ctx, ReasonSMTPPreflightFailed)
	c.providerCached = observed
	c.providerCachedAt = now
	return observed
}

type probePayload struct {
	Kind    string    `json:"kind"`
	ProbeID string    `json:"probe_id"`
	SentAt  time.Time `json:"sent_at"`
}

// ProbeBusRoundTrip publishes a labeled payload and waits for a recent probe event.
func ProbeBusRoundTrip(ctx context.Context, bus eventbus.EventBus) (ingest, raw Observation) {
	ingest = Observation{Plane: PlaneQueue, Required: true}
	raw = Observation{Plane: PlaneEventProcessing, Required: true}
	if bus == nil {
		ingest.Reason = ReasonUnobserved
		raw.Reason = ReasonUnobserved
		return ingest, raw
	}
	id := uuid.NewString()
	start := time.Now().UTC()
	payload, _ := json.Marshal(probePayload{Kind: "ops_health_probe", ProbeID: id, SentAt: start})
	consumerGroup := ProbeConsumerGroup
	if bus.Name() == "nats" {
		consumerGroup = eventbus.TransientConsumerPrefix + ProbeConsumerGroup + "-" + id
	}

	got := make(chan struct{})
	subCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	subErr := make(chan error, 1)
	go func() {
		subErr <- bus.Subscribe(subCtx, []string{ProbeTopic}, consumerGroup, func(_ context.Context, msg eventbus.Message) error {
			var received probePayload
			if err := json.Unmarshal(msg.Payload, &received); err != nil || received.Kind != "ops_health_probe" {
				return nil
			}
			if received.SentAt.IsZero() || received.SentAt.Before(start.Add(-DefaultTimeout)) {
				return nil
			}
			select {
			case <-got:
			default:
				close(got)
			}
			return nil
		})
	}()

	if err := bus.Publish(ctx, ProbeTopic, id, payload); err != nil {
		ingest.Observed = true
		ingest.OK = false
		if errors.Is(err, context.DeadlineExceeded) {
			ingest.Timeout = true
			ingest.Reason = ReasonTimeout
		} else {
			ingest.Reason = ReasonPublishFailed
		}
		raw.Observed = true
		raw.OK = false
		raw.Reason = ReasonPublishFailed
		return ingest, raw
	}
	ingest.Observed = true
	ingest.OK = true
	ingest.LatencyMS = time.Since(start).Milliseconds()

	success := func() (Observation, Observation) {
		raw.Observed = true
		raw.OK = true
		raw.Reason = ""
		raw.Timeout = false
		raw.LatencyMS = time.Since(start).Milliseconds()
		return ingest, raw
	}
	matched := func() bool {
		select {
		case <-got:
			return true
		default:
			return false
		}
	}

	select {
	case <-got:
		return success()
	case err := <-subErr:
		// Subscribe often returns Canceled (or exits) after a match. A
		// received probe_id is success even if that error wins the select.
		if matched() {
			return success()
		}
		raw.Observed = true
		raw.OK = false
		if err != nil && !errors.Is(err, context.Canceled) {
			raw.Reason = ReasonSubscribeFailed
		} else {
			raw.Reason = ReasonRoundTripMismatch
		}
		return ingest, raw
	case <-ctx.Done():
		if matched() {
			return success()
		}
		raw.Observed = true
		raw.Timeout = true
		raw.Reason = ReasonRoundTripTimeout
		return ingest, raw
	}
}
