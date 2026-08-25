package plathealth

import "time"

// Evaluate turns injected plane observations into a readiness report.
// live is process-up (the server can answer). A live process with a down,
// stale, timed-out, or unobserved required plane is not ready. HTTP 200
// on a process-up path is not treated as all-planes healthy.
func Evaluate(live bool, obs []Observation, now time.Time, timeout time.Duration) Report {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	byPlane := make(map[string]Observation, len(obs))
	for _, o := range obs {
		if o.Plane == "" {
			continue
		}
		byPlane[o.Plane] = o
	}

	planes := make([]PlaneResult, 0, len(RequiredPlanes))
	ready := live
	for _, name := range RequiredPlanes {
		o, ok := byPlane[name]
		if !ok {
			o = Observation{Plane: name, Required: true}
		}
		o.Required = true
		pr := observationToPlane(o)
		planes = append(planes, pr)
		if pr.Status != StatusOK {
			ready = false
		}
	}

	status := OverallNotReady
	if live && ready {
		status = OverallOK
	} else if live && !ready {
		status = OverallNotReady
	}

	r := Report{
		Live:      live,
		Ready:     ready,
		Status:    status,
		CheckedAt: now.UTC(),
		TimeoutMS: timeout.Milliseconds(),
		Planes:    planes,
	}
	r.IncidentClasses = ClassifyReport(r)
	return r
}

func observationToPlane(o Observation) PlaneResult {
	reason := AllowReason(o.Reason)
	status := StatusUnobserved
	switch {
	case o.Timeout:
		status = StatusTimeout
		if reason == "" {
			reason = ReasonTimeout
		}
	case o.Stale:
		status = StatusStale
		if reason == "" {
			reason = ReasonStale
		}
	case reason == ReasonHTTPProcessUpOnly && o.Plane != PlaneControlPlane:
		// Process-up HTTP 200 is not evidence that this plane works.
		status = StatusProcessUpOnly
	case !o.Observed:
		status = StatusUnobserved
		if reason == "" {
			reason = ReasonUnobserved
		}
	case o.OK && reason != ReasonHTTPProcessUpOnly:
		status = StatusOK
		reason = ""
	case o.OK && o.Plane == PlaneControlPlane && reason == ReasonHTTPProcessUpOnly:
		status = StatusOK
		reason = ""
	default:
		status = StatusFailed
		if reason == "" {
			reason = ReasonFailed
		}
	}
	if status == StatusOK {
		reason = ""
	}
	return PlaneResult{
		Plane:     o.Plane,
		Status:    status,
		Required:  o.Required,
		LatencyMS: o.LatencyMS,
		Reason:    reason,
	}
}

// ClassifyReport maps failed/unobserved/stale/timeout planes onto the six
// incident classes. contract_drift is added when the report itself is missing
// a required plane slot (should not happen for Evaluate output).
func ClassifyReport(r Report) []string {
	seen := make(map[string]bool, 6)
	var out []string
	add := func(c string) {
		if c == "" || seen[c] {
			return
		}
		seen[c] = true
		out = append(out, c)
	}
	have := make(map[string]bool, len(r.Planes))
	for _, p := range r.Planes {
		have[p.Plane] = true
		if p.Status == StatusOK || p.Status == StatusSkipped {
			continue
		}
		switch p.Plane {
		case PlaneControlPlane:
			add(ClassControlPlane)
		case PlaneDB:
			add(ClassDB)
		case PlaneCache:
			add(ClassCache)
		case PlaneQueue, PlaneEventProcessing:
			add(ClassNATS)
		case PlaneProviderEdge:
			add(ClassEmailProvider)
		case PlaneWorkerHeartbeat:
			// Heartbeat is a worker-plane failure; it is not a provider or
			// control-plane outage by itself. Surface it under control plane
			// only when the API also failed. Otherwise NATS/execution.
			add(ClassControlPlane)
		}
		if p.Reason == ReasonContractMissingPlane {
			add(ClassContractDrift)
		}
	}
	for _, name := range RequiredPlanes {
		if !have[name] {
			add(ClassContractDrift)
		}
	}
	return out
}
