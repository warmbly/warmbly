// Worker-side health reporter.
//
// Every HealthInterval (default 30s) a WorkerHealthCollector snapshots the
// in-process counters that describe how the worker is doing, packages them
// into a models.WorkerHealthSample, and publishes the sample to Kafka via
// WorkerService.Produce. The consumer side persists the sample into
// worker_health_samples; the assignment loop reads the rolled-up view.
//
// All counters except the gauges (assigned_count, imap_idle_count,
// memory_mb, goroutine_count) are window deltas: each emission resets the
// counter so the next sample describes "what happened since last time".
// That keeps the wire format commutative and lets the materialized view
// sum across the window without having to remember a baseline per
// worker.
//
// Latency tracking uses a tiny bucketed histogram instead of pulling in a
// third-party lib - workers should stay operationally light (see
// CLAUDE.md "Worker Networking Rules"). The bucket boundaries cover the
// typical SMTP send/receive range (1ms - 30s); anything slower clamps to
// the top bucket, which is the right answer for a placement signal.

package worker

import (
	"context"
	"math"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/models"
)

// DefaultHealthInterval is the cadence at which a worker emits a sample.
// Picked to match the 30s contract documented in the worker health
// migration; deliberately not aligned to HeartbeatTTL because the two
// signals have different ttls and consumers.
const DefaultHealthInterval = 30 * time.Second

// healthLatencyBuckets is the upper bound (ms) of each histogram bucket.
// Picked once and shared across all collectors so percentile lookups
// don't pay an allocation cost in the hot path.
var healthLatencyBuckets = []int32{
	1, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000, 30000,
}

// HealthCounters is the embedded counter set every WorkerService uses to
// track send-side health between health emissions. Each field is atomic
// so callers in goroutines (HandleSendEmail, error handlers) can update
// without coordination. The collector resets every field when it
// snapshots, so a missed emission window does not double-count.
type HealthCounters struct {
	sendsAttempted  atomic.Int32
	sendsSucceeded  atomic.Int32
	bouncesHard     atomic.Int32
	bouncesSoft     atomic.Int32
	complaints      atomic.Int32
	authErrors      atomic.Int32
	rateLimitErrors atomic.Int32

	latencyMu      sync.Mutex
	latencyBuckets []int32 // count per healthLatencyBuckets entry
}

// NewHealthCounters returns a fresh, zeroed HealthCounters ready for use.
func NewHealthCounters() *HealthCounters {
	return &HealthCounters{
		latencyBuckets: make([]int32, len(healthLatencyBuckets)),
	}
}

// RecordSendAttempt is called once per outbound message regardless of
// outcome. RecordSendSuccess is called additionally on success.
func (c *HealthCounters) RecordSendAttempt() { c.sendsAttempted.Add(1) }

// RecordSendSuccess marks the most recent attempt as successful.
func (c *HealthCounters) RecordSendSuccess() { c.sendsSucceeded.Add(1) }

// RecordBounceHard increments the hard bounce counter. A hard bounce is a
// permanent rejection (5xx) and the strongest negative signal for
// placement health.
func (c *HealthCounters) RecordBounceHard() { c.bouncesHard.Add(1) }

// RecordBounceSoft records a temporary failure (4xx). Logged but
// weighted much lower than hard bounces in the capacity view.
func (c *HealthCounters) RecordBounceSoft() { c.bouncesSoft.Add(1) }

// RecordComplaint records a recipient spam complaint. Rare but
// disproportionately bad for IP reputation; the capacity view weights
// each complaint 100x as much as a delivered send when computing the
// health multiplier.
func (c *HealthCounters) RecordComplaint() { c.complaints.Add(1) }

// RecordAuthError increments the auth error counter. A burst of these
// usually means a token expired or credentials were rotated.
func (c *HealthCounters) RecordAuthError() { c.authErrors.Add(1) }

// RecordRateLimitError increments the provider rate-limit counter. Used
// to detect when a worker is sending too aggressively for the provider's
// liking even if no bounces are coming back.
func (c *HealthCounters) RecordRateLimitError() { c.rateLimitErrors.Add(1) }

// RecordSMTPLatency adds a single SMTP latency observation in
// milliseconds to the rolling histogram. Cheap (lock + binary search +
// increment) so safe to call from the send path.
func (c *HealthCounters) RecordSMTPLatency(ms int32) {
	if ms < 0 {
		ms = 0
	}
	idx := sort.Search(len(healthLatencyBuckets), func(i int) bool {
		return ms <= healthLatencyBuckets[i]
	})
	if idx >= len(healthLatencyBuckets) {
		idx = len(healthLatencyBuckets) - 1
	}
	c.latencyMu.Lock()
	c.latencyBuckets[idx]++
	c.latencyMu.Unlock()
}

// snapshotWindow drains the counters into a Snapshot and resets them. It
// is intentionally the only path that mutates state during a tick so the
// reset semantics are obvious in one place.
func (c *HealthCounters) snapshotWindow() healthWindow {
	w := healthWindow{
		sendsAttempted:  c.sendsAttempted.Swap(0),
		sendsSucceeded:  c.sendsSucceeded.Swap(0),
		bouncesHard:     c.bouncesHard.Swap(0),
		bouncesSoft:     c.bouncesSoft.Swap(0),
		complaints:      c.complaints.Swap(0),
		authErrors:      c.authErrors.Swap(0),
		rateLimitErrors: c.rateLimitErrors.Swap(0),
	}

	c.latencyMu.Lock()
	buckets := make([]int32, len(c.latencyBuckets))
	copy(buckets, c.latencyBuckets)
	for i := range c.latencyBuckets {
		c.latencyBuckets[i] = 0
	}
	c.latencyMu.Unlock()

	w.p50 = latencyPercentile(buckets, 0.50)
	w.p99 = latencyPercentile(buckets, 0.99)
	return w
}

// healthWindow is the per-tick delta carried out of the counters.
type healthWindow struct {
	sendsAttempted, sendsSucceeded          int32
	bouncesHard, bouncesSoft                int32
	complaints, authErrors, rateLimitErrors int32
	p50, p99                                int32
}

// latencyPercentile returns the upper bucket bound (in ms) covering the
// requested quantile. With zero observations the answer is 0.
//
// Quantile rounding uses ceil(total*q): the p99 of three samples is
// "the third sample" (i.e. the max), not "the second". The alternative
// (floor) makes tiny-sample p99 numbers look much rosier than reality,
// which is exactly the case the placement loop cares about - the first
// few sends from a fresh worker.
func latencyPercentile(buckets []int32, q float64) int32 {
	var total int32
	for _, b := range buckets {
		total += b
	}
	if total == 0 {
		return 0
	}
	if q < 0 {
		q = 0
	}
	if q > 1 {
		q = 1
	}
	target := int32(math.Ceil(float64(total) * q))
	if target < 1 {
		target = 1
	}
	if target > total {
		target = total
	}
	var seen int32
	for i, b := range buckets {
		seen += b
		if seen >= target {
			return healthLatencyBuckets[i]
		}
	}
	return healthLatencyBuckets[len(healthLatencyBuckets)-1]
}

// AssignedCounter exposes the count of mailboxes currently loaded into the
// worker's MailManager. Defined as an interface so tests can substitute a
// stub without dragging in a full mail manager.
type AssignedCounter interface {
	AssignedCount() int
	IdleConnCount() int
}

// HealthSampler builds a WorkerHealthSample from the in-process counters
// and gauges. Exposed as a function (not a method) so tests can call it
// with a known clock and worker ID.
func HealthSampler(
	now time.Time,
	workerID uuid.UUID,
	counters *HealthCounters,
	gauges AssignedCounter,
) models.WorkerHealthSample {
	w := counters.snapshotWindow()

	var memMB int32
	var goroutines int32
	if runtime.GOOS != "" { // always true; guards against linker stripping in tests
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		memMB = int32(ms.Alloc / (1024 * 1024))
		goroutines = int32(runtime.NumGoroutine())
	}

	var assigned, idle int32
	if gauges != nil {
		assigned = int32(gauges.AssignedCount())
		idle = int32(gauges.IdleConnCount())
	}

	return models.WorkerHealthSample{
		WorkerID:         workerID,
		ObservedAt:       now.UTC(),
		AssignedCount:    assigned,
		ImapIdleCount:    idle,
		MemoryMB:         memMB,
		GoroutineCount:   goroutines,
		SendsAttempted:   w.sendsAttempted,
		SendsSucceeded:   w.sendsSucceeded,
		BouncesHard:      w.bouncesHard,
		BouncesSoft:      w.bouncesSoft,
		Complaints:       w.complaints,
		AuthErrors:       w.authErrors,
		RateLimitErrors:  w.rateLimitErrors,
		SMTPLatencyP50Ms: w.p50,
		SMTPLatencyP99Ms: w.p99,
	}
}

// ensureCounters lazily initialises the WorkerService counters. Called
// from RunHealth (and indirectly from any RecordX accessor that lands
// before RunHealth fires up).
func (s *WorkerService) ensureCounters() {
	s.healthOnce.Do(func() {
		s.HealthCounters = NewHealthCounters()
	})
}

// RecordSendAttempt forwards to the embedded HealthCounters. Provided as
// a thin shim so callers don't have to reach into WorkerService.
func (s *WorkerService) RecordSendAttempt() {
	s.ensureCounters()
	s.HealthCounters.RecordSendAttempt()
}

// RecordSendSuccess forwards to the embedded HealthCounters.
func (s *WorkerService) RecordSendSuccess() {
	s.ensureCounters()
	s.HealthCounters.RecordSendSuccess()
}

// RecordBounceHard forwards to the embedded HealthCounters.
func (s *WorkerService) RecordBounceHard() {
	s.ensureCounters()
	s.HealthCounters.RecordBounceHard()
}

// RecordBounceSoft forwards to the embedded HealthCounters.
func (s *WorkerService) RecordBounceSoft() {
	s.ensureCounters()
	s.HealthCounters.RecordBounceSoft()
}

// RecordComplaint forwards to the embedded HealthCounters.
func (s *WorkerService) RecordComplaint() {
	s.ensureCounters()
	s.HealthCounters.RecordComplaint()
}

// RecordAuthError forwards to the embedded HealthCounters.
func (s *WorkerService) RecordAuthError() {
	s.ensureCounters()
	s.HealthCounters.RecordAuthError()
}

// RecordRateLimitError forwards to the embedded HealthCounters.
func (s *WorkerService) RecordRateLimitError() {
	s.ensureCounters()
	s.HealthCounters.RecordRateLimitError()
}

// RecordSMTPLatency forwards to the embedded HealthCounters.
func (s *WorkerService) RecordSMTPLatency(ms int32) {
	s.ensureCounters()
	s.HealthCounters.RecordSMTPLatency(ms)
}

// workerHealthGauges adapts the WorkerService's MailManager to the
// AssignedCounter interface RunHealth expects. Kept private so tests can
// build their own without touching the real mail manager.
type workerHealthGauges struct {
	s *WorkerService
}

func (g workerHealthGauges) AssignedCount() int {
	if g.s == nil || g.s.mailManager == nil {
		return 0
	}
	g.s.mailManager.RLock()
	defer g.s.mailManager.RUnlock()
	return len(g.s.mailManager.Emails)
}

// IdleConnCount returns the number of mailboxes the manager is holding,
// which is the closest proxy we have to "open IMAP IDLE conns": each
// SMTP/IMAP mailbox in the manager runs a long-lived sync worker that
// drives an IDLE loop while connected. Tracking the precise idle state
// would require plumbing through every wmail client; the manager-count
// is good enough as a placement signal and avoids that coupling. The
// scaffolding can be tightened in a follow-up if we ever need finer
// detail.
func (g workerHealthGauges) IdleConnCount() int {
	return g.AssignedCount()
}

// RunHealth is the worker-side loop that builds and emits a sample on
// every tick. Mirrors the shape of WorkerService.Heartbeat so the two
// can be wired up together in cmd/worker.
func (s *WorkerService) RunHealth(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultHealthInterval
	}
	s.ensureCounters()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info().Str("worker_id", s.ID).Msg("stopping health reporter")
			return
		case <-ticker.C:
			if err := s.emitHealth(ctx); err != nil {
				log.Warn().Err(err).Str("worker_id", s.ID).Msg("failed to emit worker health")
			}
		}
	}
}

// emitHealth snapshots the counters and publishes a sample. Exposed as a
// method (rather than inline in RunHealth) so the test suite can drive
// emissions deterministically.
func (s *WorkerService) emitHealth(_ context.Context) error {
	wid, err := uuid.Parse(s.ID)
	if err != nil {
		// Workers without a parseable UUID still emit samples; the consumer
		// will reject them, but losing a malformed beat is better than
		// silently dropping all telemetry.
		wid = uuid.Nil
	}
	sample := HealthSampler(time.Now(), wid, s.HealthCounters, workerHealthGauges{s: s})
	return s.Produce(models.JobEventTypeWorkerHealth, s.ID, sample)
}
