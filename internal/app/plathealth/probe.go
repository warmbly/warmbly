package plathealth

import (
	"encoding/json"
	"io"
	"time"
)

// EvaluateProbe turns injected TLS/DNS/API/ingest/read-after-write/heartbeat
// observations into a per-check report. API HTTP 200 on the process-up
// path is never enough for green when another required check failed, is
// stale, timed out, or is unobserved.
func EvaluateProbe(in ProbeInput) ProbeReport {
	now := in.CheckedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}

	checks := []CheckResult{
		checkFromInput(CheckDNS, in.DNS, true),
		tlsCheck(in.TLS),
		apiCheck(in.API),
		checkFromInput(CheckEventIngest, in.EventIngest, true),
		checkFromInput(CheckReadAfterWrite, in.ReadAfterWrite, true),
		heartbeatCheck(in.WorkerHeartbeat),
	}

	green := true
	for _, c := range checks {
		if !c.Required {
			continue
		}
		if c.Status != StatusOK {
			green = false
		}
	}

	status := OverallOK
	if !green {
		status = OverallNotOK
	}
	r := ProbeReport{
		Green:     green,
		Status:    status,
		CheckedAt: now.UTC(),
		Checks:    checks,
	}
	r.IncidentClasses = ClassifyProbe(r)
	return r
}

func tlsCheck(in CheckInput) CheckResult {
	if AllowReason(in.Reason) == ReasonHTTPScheme {
		return CheckResult{
			Name:      CheckTLS,
			Status:    StatusSkipped,
			Required:  false,
			LatencyMS: in.LatencyMS,
			Reason:    ReasonHTTPScheme,
		}
	}
	return checkFromInput(CheckTLS, in, true)
}

func heartbeatCheck(in CheckInput) CheckResult {
	c := checkFromInput(CheckWorkerHeartbeat, in, true)
	if in.Stale && c.Status != StatusTimeout {
		c.Status = StatusStale
		if c.Reason == "" {
			c.Reason = ReasonNewestHeartbeatStale
		}
	}
	return c
}

func apiCheck(in APIInput) CheckResult {
	c := CheckResult{Name: CheckAPI, Required: true, LatencyMS: in.LatencyMS}
	if !in.Observed {
		c.Status = StatusUnobserved
		c.Reason = ReasonUnobserved
		return c
	}
	// /live 200 means the control-plane process is up. /health 200 is the
	// same process-up signal and is not treated as all-planes healthy:
	// process_up_only is a non-green API status when /live was not seen.
	liveUp := in.LiveStatus == 200
	healthUp := in.HealthStatus == 200
	if !liveUp && !healthUp {
		c.Status = StatusFailed
		c.Reason = ReasonFailed
		return c
	}
	if in.ProcessUpOnly || (healthUp && !liveUp && in.Ready == nil && in.DepsReady == nil) {
		c.Status = StatusProcessUpOnly
		c.Reason = ReasonHTTPProcessUpOnly
		return c
	}
	c.Status = StatusOK
	return c
}

func checkFromInput(name string, in CheckInput, required bool) CheckResult {
	c := CheckResult{Name: name, Required: required, LatencyMS: in.LatencyMS}
	reason := AllowReason(in.Reason)
	switch {
	case in.Timeout:
		c.Status = StatusTimeout
		if reason == "" {
			reason = ReasonTimeout
		}
		c.Reason = reason
	case in.Stale:
		c.Status = StatusStale
		if reason == "" {
			reason = ReasonStale
		}
		c.Reason = reason
	case !in.Observed:
		c.Status = StatusUnobserved
		if reason == "" {
			reason = ReasonUnobserved
		}
		c.Reason = reason
	case in.OK:
		c.Status = StatusOK
	default:
		c.Status = StatusFailed
		if reason == "" {
			reason = ReasonFailed
		}
		c.Reason = reason
	}
	return c
}

// ClassifyProbe maps failed probe checks onto the six incident classes.
func ClassifyProbe(r ProbeReport) []string {
	seen := make(map[string]bool, 6)
	var out []string
	add := func(c string) {
		if c == "" || seen[c] {
			return
		}
		seen[c] = true
		out = append(out, c)
	}
	have := make(map[string]bool, len(r.Checks))
	for _, c := range r.Checks {
		have[c.Name] = true
		if c.Status == StatusOK || c.Status == StatusSkipped {
			continue
		}
		switch c.Name {
		case CheckDNS, CheckTLS, CheckAPI:
			add(ClassControlPlane)
		case CheckEventIngest, CheckReadAfterWrite:
			add(ClassNATS)
		case CheckWorkerHeartbeat:
			add(ClassControlPlane)
		}
		if c.Reason == ReasonContractMissingPlane {
			add(ClassContractDrift)
		}
	}
	for _, name := range RequiredProbeChecks {
		if !have[name] {
			add(ClassContractDrift)
		}
	}
	return out
}

// DecodeProbeInput reads a fixture JSON document into ProbeInput.
func DecodeProbeInput(r io.Reader) (ProbeInput, error) {
	var in ProbeInput
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return ProbeInput{}, err
	}
	return in, nil
}
