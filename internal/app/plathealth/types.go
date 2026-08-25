// Package plathealth evaluates platform liveness, readiness, and dependency
// health as pure functions over injected plane observations. I/O lives in
// adapters so tests can flip one plane at a time without booting Postgres,
// Redis, NATS, or TLS.
package plathealth

import "time"

// Plane is a named operational surface. These are the readiness planes.
const (
	PlaneControlPlane    = "control_plane"
	PlaneDB              = "db"
	PlaneCache           = "cache"
	PlaneQueue           = "queue"
	PlaneEventProcessing = "event_processing"
	PlaneWorkerHeartbeat = "worker_heartbeat"
	PlaneProviderEdge    = "provider_edge"
)

// ProbeCheck names an external-probe check. Distinct from readiness planes.
const (
	CheckDNS             = "dns"
	CheckTLS             = "tls"
	CheckAPI             = "api"
	CheckEventIngest     = "event_ingest"
	CheckReadAfterWrite  = "read_after_write"
	CheckWorkerHeartbeat = "worker_heartbeat"
)

// Incident class names used by the runbook and by Classify.
const (
	ClassControlPlane  = "control_plane"
	ClassDB            = "db"
	ClassCache         = "cache"
	ClassNATS          = "nats"
	ClassEmailProvider = "email_provider"
	ClassContractDrift = "contract_drift"
)

// Check status values written into JSON. Allowlisted.
const (
	StatusOK            = "ok"
	StatusFailed        = "failed"
	StatusTimeout       = "timeout"
	StatusUnobserved    = "unobserved"
	StatusStale         = "stale"
	StatusProcessUpOnly = "process_up_only"
	StatusSkipped       = "skipped"
)

// Overall report status.
const (
	OverallOK       = "ok"
	OverallNotReady = "not_ready"
	OverallLive     = "live"
	OverallNotOK    = "not_ok"
)

// Allowlisted reason codes. Unknown reasons are rewritten to "failed"
// so free-text errors (and any secret or address they might contain)
// never leave this package.
const (
	ReasonTimeout              = "timeout"
	ReasonUnobserved           = "unobserved"
	ReasonFailed               = "failed"
	ReasonStale                = "stale"
	ReasonHTTPProcessUpOnly    = "http_process_up_only"
	ReasonNoFreshHeartbeat     = "no_fresh_heartbeat"
	ReasonNewestHeartbeatStale = "newest_heartbeat_stale"
	ReasonRoundTripMismatch    = "round_trip_mismatch"
	ReasonRoundTripTimeout     = "round_trip_timeout"
	ReasonPublishFailed        = "publish_failed"
	ReasonSubscribeFailed      = "subscribe_failed"
	ReasonContractMissingPlane = "contract_missing_plane"
	ReasonMailTransportMissing = "mail_transport_not_configured"
	ReasonTransportConfigured  = "transport_configured"
	ReasonSMTPPreflightFailed  = "smtp_preflight_failed"
	ReasonHTTPScheme           = "http_scheme"
	ReasonDNSFailed            = "dns_failed"
	ReasonTLSFailed            = "tls_failed"
	ReasonReadyNotReady        = "ready_not_ready"
	ReasonDepsNotReady         = "deps_not_ready"
	ReasonSelectFailed         = "select_failed"
	ReasonCacheMismatch        = "cache_mismatch"
)

// RequiredPlanes is the readiness set. Missing any one fails closed.
var RequiredPlanes = []string{
	PlaneControlPlane,
	PlaneDB,
	PlaneCache,
	PlaneQueue,
	PlaneEventProcessing,
	PlaneWorkerHeartbeat,
	PlaneProviderEdge,
}

// RequiredProbeChecks is the external-probe set. Partial failure is not green.
var RequiredProbeChecks = []string{
	CheckDNS,
	CheckTLS,
	CheckAPI,
	CheckEventIngest,
	CheckReadAfterWrite,
	CheckWorkerHeartbeat,
}

// Observation is one plane sample. Adapters fill this; Evaluate decides status.
type Observation struct {
	Plane     string
	Required  bool
	Observed  bool
	OK        bool
	Timeout   bool
	Stale     bool
	LatencyMS int64
	Reason    string
}

// HeartbeatSnapshot is a PII-free view of worker liveness. No worker IDs,
// IPs, hostnames, or mailbox fields.
type HeartbeatSnapshot struct {
	Observed bool
	Fresh    int
	Stale    int
}

// PlaneResult is the allowlisted JSON shape for one plane.
type PlaneResult struct {
	Plane     string `json:"plane"`
	Status    string `json:"status"`
	Required  bool   `json:"required"`
	LatencyMS int64  `json:"latency_ms"`
	Reason    string `json:"reason,omitempty"`
}

// Report is the allowlisted JSON shape for /ready and /health/deps.
type Report struct {
	Live            bool          `json:"live"`
	Ready           bool          `json:"ready"`
	Status          string        `json:"status"`
	CheckedAt       time.Time     `json:"checked_at"`
	TimeoutMS       int64         `json:"timeout_ms"`
	Planes          []PlaneResult `json:"planes"`
	IncidentClasses []string      `json:"incident_classes,omitempty"`
}

// CheckResult is the allowlisted JSON shape for one external-probe check.
type CheckResult struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	Required  bool   `json:"required"`
	LatencyMS int64  `json:"latency_ms"`
	Reason    string `json:"reason,omitempty"`
}

// ProbeReport is the allowlisted JSON shape for cmd/opsprobe.
type ProbeReport struct {
	Green           bool          `json:"green"`
	Status          string        `json:"status"`
	CheckedAt       time.Time     `json:"checked_at"`
	Checks          []CheckResult `json:"checks"`
	IncidentClasses []string      `json:"incident_classes,omitempty"`
}

// CheckInput is one fixture/injected probe observation.
type CheckInput struct {
	Observed  bool   `json:"observed"`
	OK        bool   `json:"ok"`
	Timeout   bool   `json:"timeout"`
	Stale     bool   `json:"stale"`
	Reason    string `json:"reason,omitempty"`
	LatencyMS int64  `json:"latency_ms,omitempty"`
}

// APIInput is the API check. HealthStatus 200 alone is not overall green.
type APIInput struct {
	Observed      bool  `json:"observed"`
	HealthStatus  int   `json:"health_status"`
	LiveStatus    int   `json:"live_status"`
	ReadyStatus   int   `json:"ready_status"`
	Ready         *bool `json:"ready"`
	ProcessUpOnly bool  `json:"process_up_only"`
	DepsReady     *bool `json:"deps_ready"`
	LatencyMS     int64 `json:"latency_ms,omitempty"`
}

// ProbeInput is the injected/fixture form of the external probe.
type ProbeInput struct {
	DNS             CheckInput `json:"dns"`
	TLS             CheckInput `json:"tls"`
	API             APIInput   `json:"api"`
	EventIngest     CheckInput `json:"event_ingest"`
	ReadAfterWrite  CheckInput `json:"read_after_write"`
	WorkerHeartbeat CheckInput `json:"worker_heartbeat"`
	CheckedAt       time.Time  `json:"checked_at,omitempty"`
}

// DefaultTimeout bounds each plane or probe check. Fail closed on expiry.
const DefaultTimeout = 3 * time.Second

// HeartbeatWindow matches repository.WorkerLivenessWindow so placement and
// health agree on what "fresh" means. The SQL interval is a string.
const HeartbeatWindow = "10 minutes"

// HeartbeatSQL counts active workers inside/outside the liveness window.
// It returns only integers. No worker ids, IPs, or mailbox fields.
const HeartbeatSQL = `
SELECT
  COUNT(*) FILTER (WHERE last_seen_at > now() - $1::interval)::int AS fresh,
  COUNT(*) FILTER (WHERE last_seen_at IS NULL OR last_seen_at <= now() - $1::interval)::int AS stale
FROM workers
WHERE active = true
`

// ProbeTopic is the labeled non-commercial bus subject used by ingest and
// read-after-write. It is not a campaign, warmup, or inbound lead topic.
const ProbeTopic = "ops.health.probe"

// ProbeConsumerGroup is a fixed JetStream durable so health checks do not
// accumulate consumers.
const ProbeConsumerGroup = "ops-health-probe"
