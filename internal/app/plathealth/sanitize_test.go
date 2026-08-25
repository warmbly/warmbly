package plathealth

import (
	"strings"
	"testing"
	"time"
)

func TestAllowReasonDropsSecrets(t *testing.T) {
	t.Parallel()
	if got := AllowReason("password=hunter2"); got != ReasonFailed {
		t.Fatalf("got %q", got)
	}
	if got := AllowReason("smtp auth failed for user=ops@example.com token=abc"); got != ReasonFailed {
		t.Fatalf("got %q", got)
	}
	if got := AllowReason(ReasonTimeout); got != ReasonTimeout {
		t.Fatalf("got %q", got)
	}
}

func TestReportJSONHasNoPII(t *testing.T) {
	t.Parallel()
	obs := replacePlane(allOKObs(), PlaneProviderEdge, Observation{
		Observed: true,
		OK:       false,
		Reason:   "relay auth failed for alice@example.com password=sekrit token=abcd",
	})
	r := Evaluate(true, obs, time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC), DefaultTimeout)
	raw, err := MarshalReport(r)
	if err != nil {
		t.Fatal(err)
	}
	if hits := PIIFindings(raw); len(hits) > 0 {
		t.Fatalf("pii in report %v: %s", hits, raw)
	}
	if strings.Contains(string(raw), "alice@") || strings.Contains(string(raw), "sekrit") {
		t.Fatalf("secret leaked: %s", raw)
	}
	if !strings.Contains(string(raw), ReasonFailed) && !strings.Contains(string(raw), ReasonSMTPPreflightFailed) {
		// rewritten reason must be allowlisted, never the raw error
		if strings.Contains(string(raw), "password=") {
			t.Fatalf("raw error leaked: %s", raw)
		}
	}
}

func TestProbeJSONHasNoPII(t *testing.T) {
	t.Parallel()
	in := mustFixture(t, "partial-fail.json")
	in.EventIngest.Reason = "webhook secret=whsec_live body=hi recipient=bob@x.com"
	r := EvaluateProbe(in)
	raw, err := MarshalProbe(r)
	if err != nil {
		t.Fatal(err)
	}
	if hits := PIIFindings(raw); len(hits) > 0 {
		t.Fatalf("pii in probe %v: %s", hits, raw)
	}
	if strings.Contains(string(raw), "whsec_") || strings.Contains(string(raw), "bob@") {
		t.Fatalf("secret leaked: %s", raw)
	}
}

func TestPIIFindingsCatchesBannedShapes(t *testing.T) {
	t.Parallel()
	samples := []string{
		`{"to":"lead@example.com"}`,
		`{"token":"abc"}`,
		`{"body":"hello"}`,
		`Authorization: Bearer abc`,
	}
	for _, s := range samples {
		if hits := PIIFindings([]byte(s)); len(hits) == 0 {
			t.Fatalf("expected a finding for %s", s)
		}
	}
	clean := `{"status":"ok","planes":[{"plane":"db","status":"failed","reason":"select_failed"}]}`
	if hits := PIIFindings([]byte(clean)); len(hits) > 0 {
		t.Fatalf("false positive on clean payload: %v", hits)
	}
}
