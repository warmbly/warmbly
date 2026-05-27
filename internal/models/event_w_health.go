package models

import (
	"time"

	"github.com/google/uuid"
)

// WorkerHealthSample is the per-worker telemetry emitted by every worker on
// a fixed cadence (default 30s). The consumer persists it into
// worker_health_samples; the assignment loop reads the aggregated
// worker_capacity_view that's derived from it.
//
// All counters except the gauges (assigned_count, imap_idle_count,
// memory_mb, goroutine_count) are window deltas: they describe what
// happened since the previous sample, not totals. That keeps the schema
// commutative and lets the materialized view sum across the last hour
// without having to remember a baseline.
type WorkerHealthSample struct {
	WorkerID         uuid.UUID `json:"worker_id" avro:"worker_id"`
	ObservedAt       time.Time `json:"observed_at" avro:"observed_at"`
	AssignedCount    int32     `json:"assigned_count" avro:"assigned_count"`
	ImapIdleCount    int32     `json:"imap_idle_count" avro:"imap_idle_count"`
	MemoryMB         int32     `json:"memory_mb" avro:"memory_mb"`
	GoroutineCount   int32     `json:"goroutine_count" avro:"goroutine_count"`
	SendsAttempted   int32     `json:"sends_attempted" avro:"sends_attempted"`
	SendsSucceeded   int32     `json:"sends_succeeded" avro:"sends_succeeded"`
	BouncesHard      int32     `json:"bounces_hard" avro:"bounces_hard"`
	BouncesSoft      int32     `json:"bounces_soft" avro:"bounces_soft"`
	Complaints       int32     `json:"complaints" avro:"complaints"`
	AuthErrors       int32     `json:"auth_errors" avro:"auth_errors"`
	RateLimitErrors  int32     `json:"rate_limit_errors" avro:"rate_limit_errors"`
	SMTPLatencyP50Ms int32     `json:"smtp_latency_p50_ms" avro:"smtp_latency_p50_ms"`
	SMTPLatencyP99Ms int32     `json:"smtp_latency_p99_ms" avro:"smtp_latency_p99_ms"`
}
