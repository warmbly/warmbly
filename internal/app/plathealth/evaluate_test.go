package plathealth

import (
	"context"
	"testing"
	"time"
)

func allOKObs() []Observation {
	out := make([]Observation, 0, len(RequiredPlanes))
	for _, p := range RequiredPlanes {
		out = append(out, Observation{Plane: p, Required: true, Observed: true, OK: true})
	}
	return out
}

func replacePlane(obs []Observation, plane string, next Observation) []Observation {
	next.Plane = plane
	next.Required = true
	out := make([]Observation, len(obs))
	copy(out, obs)
	for i := range out {
		if out[i].Plane == plane {
			out[i] = next
			return out
		}
	}
	return append(out, next)
}

func TestEvaluateLiveWhileNotReady(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	obs := replacePlane(allOKObs(), PlaneDB, Observation{Observed: true, OK: false, Reason: ReasonSelectFailed})
	r := Evaluate(true, obs, now, DefaultTimeout)
	if !r.Live {
		t.Fatalf("live: got false, want true (process is up)")
	}
	if r.Ready {
		t.Fatalf("ready: got true, want false when db failed")
	}
	if r.Status != OverallNotReady {
		t.Fatalf("status: got %q, want %q", r.Status, OverallNotReady)
	}
}

func TestEvaluateAllOKIsReady(t *testing.T) {
	t.Parallel()
	r := Evaluate(true, allOKObs(), time.Unix(0, 0).UTC(), DefaultTimeout)
	if !r.Live || !r.Ready || r.Status != OverallOK {
		t.Fatalf("got live=%v ready=%v status=%q", r.Live, r.Ready, r.Status)
	}
	if len(r.Planes) != len(RequiredPlanes) {
		t.Fatalf("planes: got %d, want %d", len(r.Planes), len(RequiredPlanes))
	}
}

func TestEvaluateMissingRequiredPlaneFailsClosed(t *testing.T) {
	t.Parallel()
	// Only control plane observed. HTTP-style process-up is not all-planes healthy.
	r := Evaluate(true, []Observation{{
		Plane:    PlaneControlPlane,
		Required: true,
		Observed: true,
		OK:       true,
	}}, time.Now().UTC(), DefaultTimeout)
	if r.Ready {
		t.Fatal("ready must be false when required planes are unobserved")
	}
	by := indexPlanes(r)
	for _, name := range []string{PlaneDB, PlaneCache, PlaneQueue, PlaneEventProcessing, PlaneWorkerHeartbeat, PlaneProviderEdge} {
		if by[name].Status != StatusUnobserved {
			t.Fatalf("%s status=%q, want unobserved", name, by[name].Status)
		}
	}
	if by[PlaneControlPlane].Status != StatusOK {
		t.Fatalf("control_plane status=%q, want ok", by[PlaneControlPlane].Status)
	}
}

func TestEvaluateEachRequiredPlaneFailureMakesNotReady(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	for _, plane := range RequiredPlanes {
		obs := replacePlane(allOKObs(), plane, Observation{Observed: true, OK: false, Reason: ReasonFailed})
		r := Evaluate(true, obs, now, DefaultTimeout)
		if r.Ready {
			t.Fatalf("ready stayed true when %s failed", plane)
		}
		if indexPlanes(r)[plane].Status != StatusFailed {
			t.Fatalf("%s status=%q, want failed", plane, indexPlanes(r)[plane].Status)
		}
	}
}

func TestEvaluateUnobservedRequiredPlaneMakesNotReady(t *testing.T) {
	t.Parallel()
	obs := replacePlane(allOKObs(), PlaneCache, Observation{Observed: false, Reason: ReasonUnobserved})
	r := Evaluate(true, obs, time.Now().UTC(), DefaultTimeout)
	if r.Ready {
		t.Fatal("ready stayed true when cache was unobserved")
	}
	if indexPlanes(r)[PlaneCache].Status != StatusUnobserved {
		t.Fatalf("cache status=%q", indexPlanes(r)[PlaneCache].Status)
	}
}

func TestEvaluateHTTPProcessUpOnlyIsNotHealthyForNonControlPlane(t *testing.T) {
	t.Parallel()
	obs := replacePlane(allOKObs(), PlaneQueue, Observation{
		Observed: true,
		OK:       true,
		Reason:   ReasonHTTPProcessUpOnly,
	})
	r := Evaluate(true, obs, time.Now().UTC(), DefaultTimeout)
	if r.Ready {
		t.Fatal("ready stayed true when queue only had process-up HTTP 200")
	}
	got := indexPlanes(r)[PlaneQueue]
	if got.Status != StatusProcessUpOnly {
		t.Fatalf("queue status=%q, want process_up_only", got.Status)
	}
}

func TestEvaluateTimeoutIsFailure(t *testing.T) {
	t.Parallel()
	obs := replacePlane(allOKObs(), PlaneNATSAlias(), Observation{Observed: true, Timeout: true, Reason: ReasonTimeout})
	r := Evaluate(true, obs, time.Now().UTC(), 50*time.Millisecond)
	if r.Ready {
		t.Fatal("ready stayed true after timeout")
	}
	if indexPlanes(r)[PlaneQueue].Status != StatusTimeout {
		t.Fatalf("queue status=%q, want timeout", indexPlanes(r)[PlaneQueue].Status)
	}
}

// PlaneNATSAlias keeps the timeout test pointed at the queue plane.
func PlaneNATSAlias() string { return PlaneQueue }

func TestEvaluateStaleHeartbeatMakesNotReady(t *testing.T) {
	t.Parallel()
	obs := replacePlane(allOKObs(), PlaneWorkerHeartbeat, Observation{
		Observed: true,
		Stale:    true,
		Reason:   ReasonNewestHeartbeatStale,
	})
	r := Evaluate(true, obs, time.Now().UTC(), DefaultTimeout)
	if r.Ready {
		t.Fatal("ready stayed true with stale heartbeat")
	}
	if indexPlanes(r)[PlaneWorkerHeartbeat].Status != StatusStale {
		t.Fatalf("heartbeat status=%q", indexPlanes(r)[PlaneWorkerHeartbeat].Status)
	}
}

func TestEvaluateClassifiesIncidentPlanes(t *testing.T) {
	t.Parallel()
	obs := allOKObs()
	obs = replacePlane(obs, PlaneDB, Observation{Observed: true, OK: false})
	obs = replacePlane(obs, PlaneCache, Observation{Observed: true, OK: false})
	obs = replacePlane(obs, PlaneQueue, Observation{Observed: true, OK: false})
	obs = replacePlane(obs, PlaneProviderEdge, Observation{Observed: true, OK: false})
	r := Evaluate(true, obs, time.Now().UTC(), DefaultTimeout)
	want := map[string]bool{ClassDB: true, ClassCache: true, ClassNATS: true, ClassEmailProvider: true}
	got := map[string]bool{}
	for _, c := range r.IncidentClasses {
		got[c] = true
	}
	for name := range want {
		if !got[name] {
			t.Fatalf("missing incident class %s in %v", name, r.IncidentClasses)
		}
	}
}

func TestCollectorTimeoutFailsClosed(t *testing.T) {
	t.Parallel()
	c := NewCollector(Options{
		Timeout: 40 * time.Millisecond,
		Now:     func() time.Time { return time.Unix(1, 0).UTC() },
		DB: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
		Cache: func(context.Context) error { return nil },
		Heartbeat: func(context.Context) (HeartbeatSnapshot, error) {
			return HeartbeatSnapshot{Observed: true, Fresh: 1}, nil
		},
		Provider: func(context.Context) error { return nil },
		Bus:      NewMemoryBus(),
	})
	r := c.Report(context.Background())
	if r.Ready {
		t.Fatal("ready stayed true when db exceeded timeout")
	}
	if indexPlanes(r)[PlaneDB].Status != StatusTimeout {
		t.Fatalf("db status=%q, want timeout", indexPlanes(r)[PlaneDB].Status)
	}
	if !r.Live {
		t.Fatal("live should stay true during a dependency timeout")
	}
}

func TestCollectorNilAdaptersUnobserved(t *testing.T) {
	t.Parallel()
	r := NewCollector(Options{Timeout: 50 * time.Millisecond}).Report(context.Background())
	if r.Ready {
		t.Fatal("nil adapters must fail closed")
	}
	if !r.Live {
		t.Fatal("live should be true")
	}
	by := indexPlanes(r)
	if by[PlaneControlPlane].Status != StatusOK {
		t.Fatalf("control_plane=%q", by[PlaneControlPlane].Status)
	}
	if by[PlaneDB].Status != StatusUnobserved {
		t.Fatalf("db=%q", by[PlaneDB].Status)
	}
}

func TestCollectorBusRoundTripReady(t *testing.T) {
	t.Parallel()
	c := NewCollector(Options{
		Timeout: time.Second,
		DB:      func(context.Context) error { return nil },
		Cache:   func(context.Context) error { return nil },
		Heartbeat: func(context.Context) (HeartbeatSnapshot, error) {
			return HeartbeatSnapshot{Observed: true, Fresh: 2}, nil
		},
		Provider: func(context.Context) error { return nil },
		Bus:      NewMemoryBus(),
	})
	r := c.Report(context.Background())
	if !r.Ready {
		t.Fatalf("want ready, got %+v", r)
	}
}

func TestCollectorDroppedConsumeFailsEventProcessing(t *testing.T) {
	t.Parallel()
	c := NewCollector(Options{
		Timeout: 80 * time.Millisecond,
		DB:      func(context.Context) error { return nil },
		Cache:   func(context.Context) error { return nil },
		Heartbeat: func(context.Context) (HeartbeatSnapshot, error) {
			return HeartbeatSnapshot{Observed: true, Fresh: 1}, nil
		},
		Provider: func(context.Context) error { return nil },
		Bus:      &MemoryBus{Drop: true},
	})
	r := c.Report(context.Background())
	if r.Ready {
		t.Fatal("ready stayed true when consume was dropped")
	}
	if indexPlanes(r)[PlaneQueue].Status != StatusOK {
		t.Fatalf("queue should still be ok on publish-only, got %q", indexPlanes(r)[PlaneQueue].Status)
	}
	got := indexPlanes(r)[PlaneEventProcessing].Status
	if got != StatusTimeout && got != StatusFailed {
		t.Fatalf("event_processing status=%q, want timeout or failed", got)
	}
}

func indexPlanes(r Report) map[string]PlaneResult {
	m := make(map[string]PlaneResult, len(r.Planes))
	for _, p := range r.Planes {
		m[p.Plane] = p
	}
	return m
}
