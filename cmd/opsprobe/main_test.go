package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/warmbly/warmbly/internal/app/plathealth"
)

func TestRunFixtureAllOK(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := run([]string{"--fixture", testdata(t, "all-ok.json")}, &out, &errBuf)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s stdout=%s", code, errBuf.String(), out.String())
	}
	var report plathealth.ProbeReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if !report.Green {
		t.Fatalf("expected green: %s", out.Bytes())
	}
	assertNamedChecks(t, report)
}

func TestRunFixtureStaleHeartbeat(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := run([]string{"--fixture", testdata(t, "api-200-stale-heartbeat.json")}, &out, &errBuf)
	if code != 1 {
		t.Fatalf("exit=%d want 1 stderr=%s stdout=%s", code, errBuf.String(), out.String())
	}
	var report plathealth.ProbeReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Green {
		t.Fatal("API 200 plus stale heartbeat must not be green")
	}
	assertNamedChecks(t, report)
	found := false
	for _, c := range report.Checks {
		if c.Name == plathealth.CheckWorkerHeartbeat && c.Status == plathealth.StatusStale {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected stale worker_heartbeat: %s", out.Bytes())
	}
	if hits := plathealth.PIIFindings(out.Bytes()); len(hits) > 0 {
		t.Fatalf("pii: %v %s", hits, out.Bytes())
	}
}

func TestRunFixturePartialFail(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := run([]string{"--fixture", testdata(t, "partial-fail.json")}, &out, &errBuf)
	if code != 1 {
		t.Fatalf("exit=%d want 1 stderr=%s stdout=%s", code, errBuf.String(), out.String())
	}
	var report plathealth.ProbeReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Green || report.Status == plathealth.OverallOK {
		t.Fatalf("partial fail collapsed to ok: %s", out.Bytes())
	}
	assertNamedChecks(t, report)
}

func TestRunUsage(t *testing.T) {
	var out, errBuf bytes.Buffer
	if code := run(nil, &out, &errBuf); code != 2 {
		t.Fatalf("exit=%d want 2", code)
	}
}

func testdata(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("..", "..", "internal", "app", "plathealth", "testdata", name)
}

func assertNamedChecks(t *testing.T, r plathealth.ProbeReport) {
	t.Helper()
	got := map[string]bool{}
	for _, c := range r.Checks {
		got[c.Name] = true
	}
	for _, name := range plathealth.RequiredProbeChecks {
		if !got[name] {
			t.Fatalf("missing check %s", name)
		}
	}
}
