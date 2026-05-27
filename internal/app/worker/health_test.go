// Worker-side health collector unit tests. The collector is the only
// path that resets the in-process counters, so any drift here means
// either double-counting (the next sample includes deltas that already
// went out) or under-counting (the next sample misses deltas that
// happened during snapshot). Both are visibility bugs that compound
// over hours.

package worker

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// stubGauges lets HealthSampler run without a real MailManager. The
// counts are returned verbatim so tests can assert the sample carries
// them through unchanged.
type stubGauges struct {
	assigned int
	idle     int
}

func (s stubGauges) AssignedCount() int { return s.assigned }
func (s stubGauges) IdleConnCount() int { return s.idle }

func TestHealthCounters_WindowResets(t *testing.T) {
	c := NewHealthCounters()
	c.RecordSendAttempt()
	c.RecordSendAttempt()
	c.RecordSendSuccess()
	c.RecordBounceHard()
	c.RecordBounceSoft()
	c.RecordComplaint()
	c.RecordAuthError()
	c.RecordRateLimitError()
	c.RecordSMTPLatency(40)
	c.RecordSMTPLatency(120)
	c.RecordSMTPLatency(2000)

	w1 := c.snapshotWindow()
	if w1.sendsAttempted != 2 {
		t.Errorf("sendsAttempted: got %d, want 2", w1.sendsAttempted)
	}
	if w1.sendsSucceeded != 1 {
		t.Errorf("sendsSucceeded: got %d, want 1", w1.sendsSucceeded)
	}
	if w1.bouncesHard != 1 || w1.bouncesSoft != 1 {
		t.Errorf("bounces: hard=%d soft=%d", w1.bouncesHard, w1.bouncesSoft)
	}
	if w1.complaints != 1 || w1.authErrors != 1 || w1.rateLimitErrors != 1 {
		t.Errorf("misc counters didn't roll up: %+v", w1)
	}
	if w1.p50 <= 0 || w1.p99 <= 0 {
		t.Errorf("latency percentiles should be set after 3 observations, got p50=%d p99=%d", w1.p50, w1.p99)
	}
	// p99 should be at least the bucket covering 2000ms (2500ms bucket).
	if w1.p99 < 2000 {
		t.Errorf("p99 should be >= 2000 (we recorded 2000ms), got %d", w1.p99)
	}

	// Second snapshot with no new activity should be fully zero.
	w2 := c.snapshotWindow()
	if w2.sendsAttempted != 0 || w2.sendsSucceeded != 0 ||
		w2.bouncesHard != 0 || w2.bouncesSoft != 0 ||
		w2.complaints != 0 || w2.authErrors != 0 || w2.rateLimitErrors != 0 ||
		w2.p50 != 0 || w2.p99 != 0 {
		t.Errorf("counters not reset after snapshot: %+v", w2)
	}
}

func TestHealthCounters_ConcurrentRecording(t *testing.T) {
	// Atomics give us the safety; this test pins it by hammering the
	// counters from many goroutines, snapshotting once, and verifying
	// the sum matches what we recorded.
	c := NewHealthCounters()
	const goroutines = 16
	const perG = 1000

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < perG; j++ {
				c.RecordSendAttempt()
				c.RecordSMTPLatency(int32(10 + (j % 100)))
			}
		}()
	}
	wg.Wait()

	w := c.snapshotWindow()
	if w.sendsAttempted != goroutines*perG {
		t.Errorf("sendsAttempted under concurrency: got %d, want %d", w.sendsAttempted, goroutines*perG)
	}
}

func TestHealthSampler_ShapeMatchesWireContract(t *testing.T) {
	// HealthSampler is what RunHealth packages into a JobEventTypeWorkerHealth.
	// Lock the field mapping so we can't accidentally drop one and only
	// notice in production.
	c := NewHealthCounters()
	c.RecordSendAttempt()
	c.RecordSendAttempt()
	c.RecordSendAttempt()
	c.RecordSendSuccess()
	c.RecordSendSuccess()
	c.RecordBounceHard()
	c.RecordComplaint()
	c.RecordAuthError()
	c.RecordRateLimitError()
	c.RecordSMTPLatency(15)
	c.RecordSMTPLatency(80)

	wid := uuid.New()
	now := time.Now()
	sample := HealthSampler(now, wid, c, stubGauges{assigned: 7, idle: 4})

	if sample.WorkerID != wid {
		t.Errorf("WorkerID lost: got %s, want %s", sample.WorkerID, wid)
	}
	if sample.ObservedAt.IsZero() {
		t.Errorf("ObservedAt not set")
	}
	if sample.AssignedCount != 7 {
		t.Errorf("AssignedCount = %d, want 7", sample.AssignedCount)
	}
	if sample.ImapIdleCount != 4 {
		t.Errorf("ImapIdleCount = %d, want 4", sample.ImapIdleCount)
	}
	if sample.SendsAttempted != 3 {
		t.Errorf("SendsAttempted = %d, want 3", sample.SendsAttempted)
	}
	if sample.SendsSucceeded != 2 {
		t.Errorf("SendsSucceeded = %d, want 2", sample.SendsSucceeded)
	}
	if sample.BouncesHard != 1 {
		t.Errorf("BouncesHard = %d, want 1", sample.BouncesHard)
	}
	if sample.Complaints != 1 {
		t.Errorf("Complaints = %d, want 1", sample.Complaints)
	}
	if sample.AuthErrors != 1 {
		t.Errorf("AuthErrors = %d, want 1", sample.AuthErrors)
	}
	if sample.RateLimitErrors != 1 {
		t.Errorf("RateLimitErrors = %d, want 1", sample.RateLimitErrors)
	}
	if sample.SMTPLatencyP50Ms <= 0 || sample.SMTPLatencyP99Ms <= 0 {
		t.Errorf("latency percentiles should be set: p50=%d p99=%d", sample.SMTPLatencyP50Ms, sample.SMTPLatencyP99Ms)
	}
	// Memory and goroutine count are runtime gauges; just verify they're
	// non-negative (zero is technically valid in fixture testing).
	if sample.MemoryMB < 0 || sample.GoroutineCount < 0 {
		t.Errorf("runtime gauges are negative: mem=%d goroutines=%d", sample.MemoryMB, sample.GoroutineCount)
	}
}

func TestHealthSampler_EmptySampleAfterReset(t *testing.T) {
	// After one snapshot drains the counters, the next sample with no
	// activity should carry zeros for every delta. Gauges still get
	// whatever the runtime says.
	c := NewHealthCounters()
	c.RecordSendAttempt()
	_ = c.snapshotWindow()

	sample := HealthSampler(time.Now(), uuid.New(), c, stubGauges{})
	if sample.SendsAttempted != 0 || sample.SendsSucceeded != 0 ||
		sample.BouncesHard != 0 || sample.BouncesSoft != 0 ||
		sample.Complaints != 0 || sample.AuthErrors != 0 ||
		sample.RateLimitErrors != 0 ||
		sample.SMTPLatencyP50Ms != 0 || sample.SMTPLatencyP99Ms != 0 {
		t.Errorf("post-reset sample carries stale data: %+v", sample)
	}
}

func TestLatencyPercentile_EmptyHistogram(t *testing.T) {
	buckets := make([]int32, len(healthLatencyBuckets))
	if got := latencyPercentile(buckets, 0.5); got != 0 {
		t.Errorf("empty histogram should return 0, got %d", got)
	}
}

func TestLatencyPercentile_QuantileClamp(t *testing.T) {
	// Out-of-range quantiles should be clamped, not crash.
	buckets := make([]int32, len(healthLatencyBuckets))
	buckets[0] = 10
	if got := latencyPercentile(buckets, -1); got == 0 {
		t.Errorf("q=-1 should clamp to 0 and still return a bucket, got 0")
	}
	if got := latencyPercentile(buckets, 5); got == 0 {
		t.Errorf("q=5 should clamp to 1 and still return a bucket, got 0")
	}
}
