package plathealth

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/infrastructure/eventbus"
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
	if by[CheckAPI].Status != StatusFailed || by[CheckAPI].Reason != ReasonReadyNotReady {
		t.Fatalf("api = %q/%q, want failed/%s", by[CheckAPI].Status, by[CheckAPI].Reason, ReasonReadyNotReady)
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
	if by[CheckAPI].Status != StatusFailed {
		t.Fatalf("api=%q, want failed because readiness is false", by[CheckAPI].Status)
	}
}

func TestEvaluateProbeReadyFailureCannotGoGreen(t *testing.T) {
	t.Parallel()
	in := mustFixture(t, "all-ok.json")
	ready := false
	in.API.Ready = &ready
	in.API.ReadyStatus = http.StatusServiceUnavailable
	r := EvaluateProbe(in)
	if r.Green {
		t.Fatal("ready=false must make the external probe non-green")
	}
	got := indexChecks(r)[CheckAPI]
	if got.Status != StatusFailed || got.Reason != ReasonReadyNotReady {
		t.Fatalf("api = %q/%q, want failed/%s", got.Status, got.Reason, ReasonReadyNotReady)
	}
}

func TestEvaluateProbeLiveWithoutReadinessIsProcessUpOnly(t *testing.T) {
	t.Parallel()
	in := mustFixture(t, "all-ok.json")
	in.API.Ready = nil
	in.API.DepsReady = nil
	in.API.ProcessUpOnly = false
	r := EvaluateProbe(in)
	got := indexChecks(r)[CheckAPI]
	if got.Status != StatusProcessUpOnly || r.Green {
		t.Fatalf("api=%q green=%v, want process_up_only and non-green", got.Status, r.Green)
	}
}

func TestRunProbeReadsDependencyMatrixOnce(t *testing.T) {
	t.Parallel()
	report := Evaluate(true, allOKObs(), time.Now().UTC(), DefaultTimeout)
	body, err := MarshalReport(report)
	if err != nil {
		t.Fatal(err)
	}
	var readyCalls atomic.Int32
	var depsCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"status":"ok"}`)) })
	mux.HandleFunc("/live", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"status":"live","live":true}`)) })
	mux.HandleFunc("/ready", func(w http.ResponseWriter, _ *http.Request) {
		readyCalls.Add(1)
		_, _ = w.Write(body)
	})
	mux.HandleFunc("/health/deps", func(w http.ResponseWriter, _ *http.Request) {
		depsCalls.Add(1)
		_, _ = w.Write(body)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	got, err := RunProbe(context.Background(), ProbeConfig{BaseURL: srv.URL, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Green {
		t.Fatalf("probe not green: %+v", got)
	}
	if readyCalls.Load() != 1 || depsCalls.Load() != 0 {
		t.Fatalf("ready calls=%d deps calls=%d, want 1 and 0", readyCalls.Load(), depsCalls.Load())
	}
}

func TestLiftDepsKeepsQueueAndEventProcessingSeparate(t *testing.T) {
	t.Parallel()
	in := ProbeInput{}
	liftDeps(&in, &depsBody{Planes: []struct {
		Plane  string `json:"plane"`
		Status string `json:"status"`
		Reason string `json:"reason"`
	}{
		{Plane: PlaneQueue, Status: StatusFailed, Reason: ReasonPublishFailed},
		{Plane: PlaneEventProcessing, Status: StatusOK},
		{Plane: PlaneWorkerHeartbeat, Status: StatusOK},
	}})
	if in.EventIngest.OK || in.EventIngest.Reason != ReasonPublishFailed {
		t.Fatalf("queue result overwritten: %+v", in.EventIngest)
	}
	if !in.ReadAfterWrite.OK {
		t.Fatalf("event processing result = %+v, want ok", in.ReadAfterWrite)
	}
}

func TestLiftDepsMarksEachMissingPlaneAsContractDrift(t *testing.T) {
	t.Parallel()
	in := ProbeInput{}
	liftDeps(&in, &depsBody{})
	for name, check := range map[string]CheckInput{
		CheckEventIngest:     in.EventIngest,
		CheckReadAfterWrite:  in.ReadAfterWrite,
		CheckWorkerHeartbeat: in.WorkerHeartbeat,
	} {
		if check.Reason != ReasonContractMissingPlane {
			t.Fatalf("%s reason=%q, want contract drift", name, check.Reason)
		}
	}
}

func TestLiveTLSRecordsFailureLatency(t *testing.T) {
	t.Parallel()
	got := liveTLS(context.Background(), ProbeConfig{
		Timeout: time.Second,
		TLSDial: func(context.Context, string, string, *tls.Config) error {
			time.Sleep(5 * time.Millisecond)
			return errors.New("dial failed")
		},
	}, "example.com:443", "example.com")
	if got.OK || got.LatencyMS <= 0 {
		t.Fatalf("TLS failure = %+v, want failure with measured latency", got)
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

func TestProbeBusRoundTripSubscribeCanceledAfterMatch(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ingest, raw := ProbeBusRoundTrip(ctx, newDeliverThenCancelBus())
	if !ingest.OK {
		t.Fatalf("publish half failed: %+v", ingest)
	}
	if !raw.OK {
		t.Fatalf("matched probe_id must stay success when Subscribe returns Canceled, got %+v", raw)
	}
}

func TestProbeBusRoundTripDrainsStaleProbeBeforeCurrentOne(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	bus := newRecordHandlerBus()
	ingest, raw := ProbeBusRoundTrip(ctx, bus)
	if !ingest.OK || !raw.OK {
		t.Fatalf("own probe must still succeed: ingest=%+v raw=%+v", ingest, raw)
	}
	got := bus.handlerErrors()
	if len(got) < 2 {
		t.Fatalf("handler should see foreign then own, got %d results", len(got))
	}
	if got[0] != nil {
		t.Fatalf("stale probe should be drained, handler err=%v", got[0])
	}
	if got[1] != nil {
		t.Fatalf("own probe_id should be ACKed, handler err=%v", got[1])
	}
}

func TestRunProbeLiveBusKeepsHeartbeatFromDeps(t *testing.T) {
	t.Parallel()
	srv := newDepsServer(t, true, "ok")
	defer srv.Close()
	r, err := RunProbe(context.Background(), ProbeConfig{
		BaseURL: srv.URL,
		Bus:     NewMemoryBus(),
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	by := indexChecks(r)
	if by[CheckWorkerHeartbeat].Status != StatusOK {
		t.Fatalf("documented --base-url --nats-url path left heartbeat %q (want ok from /health/deps)", by[CheckWorkerHeartbeat].Status)
	}
	if by[CheckEventIngest].Status != StatusOK || by[CheckReadAfterWrite].Status != StatusOK {
		t.Fatalf("bus should replace ingest/raw, got ingest=%q raw=%q", by[CheckEventIngest].Status, by[CheckReadAfterWrite].Status)
	}
	if !r.Green {
		t.Fatalf("live command with fresh deps + working bus must be able to go green: %+v", r)
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

func TestDecodeProbeInputRejectsMultipleDocuments(t *testing.T) {
	t.Parallel()
	_, err := DecodeProbeInput(bytes.NewReader([]byte(`{} {}`)))
	if err == nil {
		t.Fatal("expected multiple JSON documents to fail")
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
	hb := Observation{Plane: PlaneWorkerHeartbeat, Observed: true, Required: true, OK: true}
	if heartbeatStatus == "stale" || !ready {
		hb.OK = false
		hb.Stale = true
		hb.Reason = ReasonNewestHeartbeatStale
	}
	report := Evaluate(true, []Observation{
		{Plane: PlaneControlPlane, Observed: true, OK: true, Required: true},
		{Plane: PlaneDB, Observed: true, OK: true, Required: true},
		{Plane: PlaneCache, Observed: true, OK: true, Required: true},
		{Plane: PlaneQueue, Observed: true, OK: true, Required: true},
		{Plane: PlaneEventProcessing, Observed: true, OK: ready, Required: true},
		hb,
		{Plane: PlaneProviderEdge, Observed: true, OK: true, Required: true},
	}, time.Now().UTC(), DefaultTimeout)
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

// deliverThenCancelBus delivers the published payload then returns Canceled
// from Subscribe, which is the race that used to flip a successful consume
// into round_trip_mismatch.
type deliverThenCancelBus struct {
	mu      sync.Mutex
	payload []byte
	ready   chan struct{}
	once    sync.Once
}

func newDeliverThenCancelBus() *deliverThenCancelBus {
	return &deliverThenCancelBus{ready: make(chan struct{})}
}

func (b *deliverThenCancelBus) Publish(_ context.Context, _, _ string, payload []byte) error {
	b.mu.Lock()
	b.payload = append([]byte(nil), payload...)
	b.mu.Unlock()
	b.once.Do(func() { close(b.ready) })
	return nil
}

func (b *deliverThenCancelBus) Subscribe(ctx context.Context, _ []string, _ string, handler eventbus.Handler) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-b.ready:
	}
	b.mu.Lock()
	p := append([]byte(nil), b.payload...)
	b.mu.Unlock()
	_ = handler(ctx, eventbus.Message{Topic: ProbeTopic, Payload: p})
	return context.Canceled
}

func (b *deliverThenCancelBus) Close() error { return nil }
func (b *deliverThenCancelBus) Name() string { return "deliver-then-cancel" }

// recordHandlerBus delivers a stale probe first, then the published one.
type recordHandlerBus struct {
	mu        sync.Mutex
	published []byte
	ready     chan struct{}
	once      sync.Once
	results   []error
}

func newRecordHandlerBus() *recordHandlerBus {
	return &recordHandlerBus{ready: make(chan struct{})}
}

func (b *recordHandlerBus) Publish(_ context.Context, _, _ string, payload []byte) error {
	b.mu.Lock()
	b.published = append([]byte(nil), payload...)
	b.mu.Unlock()
	b.once.Do(func() { close(b.ready) })
	return nil
}

func (b *recordHandlerBus) Subscribe(ctx context.Context, _ []string, _ string, handler eventbus.Handler) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-b.ready:
	}
	foreign, err := json.Marshal(probePayload{Kind: "ops_health_probe", ProbeID: "00000000-0000-0000-0000-000000000000"})
	if err != nil {
		return err
	}
	errF := handler(ctx, eventbus.Message{Topic: ProbeTopic, Payload: foreign})
	b.mu.Lock()
	b.results = append(b.results, errF)
	own := append([]byte(nil), b.published...)
	b.mu.Unlock()
	errOwn := handler(ctx, eventbus.Message{Topic: ProbeTopic, Payload: own})
	b.mu.Lock()
	b.results = append(b.results, errOwn)
	b.mu.Unlock()
	<-ctx.Done()
	return ctx.Err()
}

func (b *recordHandlerBus) handlerErrors() []error {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]error, len(b.results))
	copy(out, b.results)
	return out
}

func (b *recordHandlerBus) Close() error { return nil }
func (b *recordHandlerBus) Name() string { return "record-handler" }
