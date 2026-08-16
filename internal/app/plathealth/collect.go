package plathealth

import (
	"bytes"
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
	Timeout   time.Duration
	Now       func() time.Time
	DB        func(context.Context) error
	Cache     func(context.Context) error
	Bus       eventbus.EventBus
	Heartbeat func(context.Context) (HeartbeatSnapshot, error)
	Provider  func(context.Context) error
}

// Collector runs adapters with a per-check timeout and evaluates the report.
type Collector struct {
	opts Options
}

// NewCollector returns a collector. Timeout defaults to DefaultTimeout.
func NewCollector(opts Options) *Collector {
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultTimeout
	}
	if opts.Now == nil {
		opts.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Collector{opts: opts}
}

// Report runs every plane and evaluates readiness. The process is live
// because this method is executing.
func (c *Collector) Report(ctx context.Context) Report {
	return Evaluate(true, c.Observe(ctx), c.opts.Now(), c.opts.Timeout)
}

// Observe runs adapters in parallel. One hung plane cannot stall the rest.
func (c *Collector) Observe(ctx context.Context) []Observation {
	obs := make([]Observation, len(RequiredPlanes))
	var wg sync.WaitGroup
	for i, plane := range RequiredPlanes {
		wg.Add(1)
		go func(i int, plane string) {
			defer wg.Done()
			obs[i] = c.observeOne(ctx, plane)
		}(i, plane)
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
	case PlaneQueue:
		return c.observeQueue(ctx)
	case PlaneEventProcessing:
		return c.observeEventProcessing(ctx)
	case PlaneWorkerHeartbeat:
		return c.observeHeartbeat(ctx)
	case PlaneProviderEdge:
		return fromErr(plane, c.opts.Provider, ctx, ReasonSMTPPreflightFailed)
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

func (c *Collector) observeQueue(ctx context.Context) Observation {
	if c.opts.Bus == nil {
		return Observation{Plane: PlaneQueue, Required: true, Observed: false, Reason: ReasonUnobserved}
	}
	id := uuid.NewString()
	payload, _ := json.Marshal(probePayload{Kind: "ops_health_probe", ProbeID: id})
	if err := c.opts.Bus.Publish(ctx, ProbeTopic, id, payload); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return Observation{Plane: PlaneQueue, Required: true, Observed: true, Timeout: true, Reason: ReasonTimeout}
		}
		return Observation{Plane: PlaneQueue, Required: true, Observed: true, OK: false, Reason: ReasonPublishFailed}
	}
	return Observation{Plane: PlaneQueue, Required: true, Observed: true, OK: true}
}

func (c *Collector) observeEventProcessing(ctx context.Context) Observation {
	ingest, raw := ProbeBusRoundTrip(ctx, c.opts.Bus)
	// Event processing requires the consume half (read-after-write). Publish
	// alone is the queue plane.
	if raw.Timeout || ingest.Timeout {
		return Observation{Plane: PlaneEventProcessing, Required: true, Observed: true, Timeout: true, Reason: ReasonRoundTripTimeout}
	}
	if !raw.Observed {
		return Observation{Plane: PlaneEventProcessing, Required: true, Observed: false, Reason: raw.Reason}
	}
	if !raw.OK {
		return Observation{Plane: PlaneEventProcessing, Required: true, Observed: true, OK: false, Reason: raw.Reason}
	}
	return Observation{Plane: PlaneEventProcessing, Required: true, Observed: true, OK: true, LatencyMS: raw.LatencyMS}
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

type probePayload struct {
	Kind    string `json:"kind"`
	ProbeID string `json:"probe_id"`
}

// ProbeBusRoundTrip publishes a labeled probe payload on the shipped EventBus
// and waits to consume the same probe_id. ingest is the publish half;
// raw (read-after-write) is the consume half.
func ProbeBusRoundTrip(ctx context.Context, bus eventbus.EventBus) (ingest, raw Observation) {
	ingest = Observation{Plane: PlaneQueue, Required: true}
	raw = Observation{Plane: PlaneEventProcessing, Required: true}
	if bus == nil {
		ingest.Reason = ReasonUnobserved
		raw.Reason = ReasonUnobserved
		return ingest, raw
	}
	id := uuid.NewString()
	payload, _ := json.Marshal(probePayload{Kind: "ops_health_probe", ProbeID: id})
	start := time.Now()

	got := make(chan struct{})
	subCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	subErr := make(chan error, 1)
	go func() {
		subErr <- bus.Subscribe(subCtx, []string{ProbeTopic}, ProbeConsumerGroup, func(_ context.Context, msg eventbus.Message) error {
			if !bytes.Contains(msg.Payload, []byte(id)) {
				return nil
			}
			select {
			case <-got:
			default:
				close(got)
			}
			cancel()
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

	select {
	case <-got:
		raw.Observed = true
		raw.OK = true
		raw.LatencyMS = time.Since(start).Milliseconds()
		return ingest, raw
	case err := <-subErr:
		raw.Observed = true
		raw.OK = false
		if err != nil && !errors.Is(err, context.Canceled) {
			raw.Reason = ReasonSubscribeFailed
		} else {
			raw.Reason = ReasonRoundTripMismatch
		}
		return ingest, raw
	case <-ctx.Done():
		raw.Observed = true
		raw.Timeout = true
		raw.Reason = ReasonRoundTripTimeout
		return ingest, raw
	}
}
