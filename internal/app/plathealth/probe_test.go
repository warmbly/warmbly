package plathealth

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEvaluateProbeNamesEveryCheck(t *testing.T) {
	t.Parallel()
	r := EvaluateProbe(mustFixture(t, "all-ok.json"))
	got := map[string]CheckResult{}
	for _, c := range r.Checks {
		got[c.Name] = c
	}
	for _, name := range RequiredProbeChecks {
		if _, ok := got[name]; !ok {
			t.Fatalf("missing check %s in %+v", name, r.Checks)
		}
	}
	if !r.Green || r.Status != OverallOK {
		t.Fatalf("all-ok fixture should be green, got %+v", r)
	}
}

func TestEvaluateProbeAPI200StaleHeartbeatIsNotGreen(t *testing.T) {
	t.Parallel()
	r := EvaluateProbe(mustFixture(t, "api-200-stale-heartbeat.json"))
	if r.Green {
		t.Fatal("API 200 plus stale heartbeat must not be green")
	}
	by := indexChecks(r)
	if by[CheckAPI].Status != StatusOK {
		t.Fatalf("api status=%q, want ok (process answers); overall must still be not green", by[CheckAPI].Status)
	}
	if by[CheckWorkerHeartbeat].Status != StatusStale {
		t.Fatalf("heartbeat status=%q, want stale", by[CheckWorkerHeartbeat].Status)
	}
	if len(r.Checks) < 6 {
		t.Fatalf("partial fail collapsed to %d checks", len(r.Checks))
	}
}

func TestEvaluateProbeFailedIngestIsNotGreen(t *testing.T) {
	t.Parallel()
	in := mustFixture(t, "all-ok.json")
	in.EventIngest = CheckInput{Observed: true, OK: false, Reason: ReasonPublishFailed}
	r := EvaluateProbe(in)
	if r.Green {
		t.Fatal("failed ingest must not be green")
	}
	if indexChecks(r)[CheckEventIngest].Status != StatusFailed {
		t.Fatalf("event_ingest=%q", indexChecks(r)[CheckEventIngest].Status)
	}
	if indexChecks(r)[CheckDNS].Status != StatusOK {
		t.Fatal("dns should stay named and ok; must not collapse to a single status")
	}
}

func TestEvaluateProbeFailedReadAfterWriteIsNotGreen(t *testing.T) {
	t.Parallel()
	in := mustFixture(t, "all-ok.json")
	in.ReadAfterWrite = CheckInput{Observed: true, OK: false, Reason: ReasonRoundTripMismatch}
	r := EvaluateProbe(in)
	if r.Green {
		t.Fatal("failed read-after-write must not be green")
	}
	if indexChecks(r)[CheckReadAfterWrite].Status != StatusFailed {
		t.Fatalf("read_after_write=%q", indexChecks(r)[CheckReadAfterWrite].Status)
	}
}

func TestEvaluateProbeProcessUpOnlyIsNotGreen(t *testing.T) {
	t.Parallel()
	r := EvaluateProbe(mustFixture(t, "process-up-only.json"))
	if r.Green {
		t.Fatal("process-up HTTP 200 must not make the probe green")
	}
	if indexChecks(r)[CheckAPI].Status != StatusProcessUpOnly {
		t.Fatalf("api status=%q, want process_up_only", indexChecks(r)[CheckAPI].Status)
	}
	if indexChecks(r)[CheckTLS].Status != StatusSkipped {
		t.Fatalf("tls status=%q, want skipped on http_scheme", indexChecks(r)[CheckTLS].Status)
	}
}

func TestEvaluateProbePartialFailDoesNotCollapse(t *testing.T) {
	t.Parallel()
	r := EvaluateProbe(mustFixture(t, "partial-fail.json"))
	if r.Green {
		t.Fatal("partial fail must not be green")
	}
	if r.Status != OverallNotOK {
		t.Fatalf("status=%q", r.Status)
	}
	by := indexChecks(r)
	if by[CheckDNS].Status != StatusOK || by[CheckTLS].Status != StatusOK {
		t.Fatalf("dns/tls should remain individually ok, got dns=%q tls=%q", by[CheckDNS].Status, by[CheckTLS].Status)
	}
	if by[CheckEventIngest].Status != StatusFailed {
		t.Fatalf("event_ingest=%q", by[CheckEventIngest].Status)
	}
	if by[CheckReadAfterWrite].Status != StatusFailed {
		t.Fatalf("read_after_write=%q", by[CheckReadAfterWrite].Status)
	}
	if by[CheckWorkerHeartbeat].Status != StatusUnobserved {
		t.Fatalf("worker_heartbeat=%q", by[CheckWorkerHeartbeat].Status)
	}
	foundNATS := false
	for _, c := range r.IncidentClasses {
		if c == ClassNATS {
			foundNATS = true
		}
	}
	if !foundNATS {
		t.Fatalf("expected nats class, got %v", r.IncidentClasses)
	}
}

func TestRunProbeFixtureEntryPoint(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r, err := RunProbe(ctx, ProbeConfig{FixturePath: testdata(t, "api-200-stale-heartbeat.json")})
	if err != nil {
		t.Fatal(err)
	}
	if r.Green {
		t.Fatal("fixture with stale heartbeat must not be green")
	}
	if indexChecks(r)[CheckWorkerHeartbeat].Status != StatusStale {
		t.Fatalf("heartbeat=%q", indexChecks(r)[CheckWorkerHeartbeat].Status)
	}
}

func TestRunProbeLiveHTTPPartialFail(t *testing.T) {
	t.Parallel()
	srv := newDepsServer(t, false, "stale")
	defer srv.Close()
	r, err := RunProbe(context.Background(), ProbeConfig{
		BaseURL: srv.URL,
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Green {
		t.Fatalf("live probe must not be green on stale heartbeat: %+v", r)
	}
	by := indexChecks(r)
	for _, name := range RequiredProbeChecks {
		if _, ok := by[name]; !ok {
			t.Fatalf("missing check %s", name)
		}
	}
	if by[CheckWorkerHeartbeat].Status != StatusStale {
		t.Fatalf("heartbeat=%q", by[CheckWorkerHeartbeat].Status)
	}
	if by[CheckAPI].Status != StatusOK {
		t.Fatalf("api=%q (process answers; overall still not green)", by[CheckAPI].Status)
	}
}

func TestRunProbeAllOKFixture(t *testing.T) {
	t.Parallel()
	r, err := RunProbe(context.Background(), ProbeConfig{FixturePath: testdata(t, "all-ok.json")})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Green {
		t.Fatalf("all-ok fixture should be green, got %+v", r)
	}
}

func TestProbeBusRoundTrip(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ingest, raw := ProbeBusRoundTrip(ctx, NewMemoryBus())
	if !ingest.OK || !raw.OK {
		t.Fatalf("round trip failed ingest=%+v raw=%+v", ingest, raw)
	}
}

func TestProbeBusRoundTripPublishFail(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	ingest, raw := ProbeBusRoundTrip(ctx, &MemoryBus{FailPub: true})
	if ingest.OK {
		t.Fatal("publish failure must fail ingest")
	}
	if raw.OK {
		t.Fatal("publish failure must fail read-after-write")
	}
	if ingest.Reason != ReasonPublishFailed {
		t.Fatalf("ingest reason=%q", ingest.Reason)
	}
}

func TestDecodeProbeInputRejectsUnknownFields(t *testing.T) {
	t.Parallel()
	_, err := DecodeProbeInput(bytes.NewReader([]byte(`{"dns":{},"extra":true}`)))
	if err == nil {
		t.Fatal("expected unknown field error")
	}
}

func mustFixture(t *testing.T, name string) ProbeInput {
	t.Helper()
	b, err := os.ReadFile(testdata(t, name))
	if err != nil {
		t.Fatal(err)
	}
	in, err := DecodeProbeInput(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return in
}

func newDepsServer(t *testing.T, ready bool, heartbeatStatus string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	report := Evaluate(true, []Observation{
		{Plane: PlaneControlPlane, Observed: true, OK: true, Required: true},
		{Plane: PlaneDB, Observed: true, OK: true, Required: true},
		{Plane: PlaneCache, Observed: true, OK: true, Required: true},
		{Plane: PlaneQueue, Observed: true, OK: true, Required: true},
		{Plane: PlaneEventProcessing, Observed: true, OK: ready, Required: true},
		{Plane: PlaneWorkerHeartbeat, Observed: true, Stale: !ready, Required: true, Reason: ReasonNewestHeartbeatStale},
		{Plane: PlaneProviderEdge, Observed: true, OK: true, Required: true},
	}, time.Now().UTC(), DefaultTimeout)
	if heartbeatStatus != "stale" {
		// keep Evaluate output
		_ = heartbeatStatus
	}
	body, err := MarshalReport(report)
	if err != nil {
		t.Fatal(err)
	}
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/live", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"live","live":true}`))
	})
	writeMatrix := func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		if !report.Ready {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_, _ = w.Write(body)
	}
	mux.HandleFunc("/ready", func(w http.ResponseWriter, _ *http.Request) { writeMatrix(w) })
	mux.HandleFunc("/health/deps", func(w http.ResponseWriter, _ *http.Request) { writeMatrix(w) })
	return httptest.NewServer(mux)
}

func testdata(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("testdata", name)
}

func indexChecks(r ProbeReport) map[string]CheckResult {
	m := make(map[string]CheckResult, len(r.Checks))
	for _, c := range r.Checks {
		m[c.Name] = c
	}
	return m
}
