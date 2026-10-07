package models

import (
	"math"
	"time"

	"github.com/google/uuid"
)

// NodeRole is what a process on a machine does. Both roles share the same
// lifecycle - enrol, heartbeat, report usage, self-update - and differ only in
// what else the control plane knows about them: a worker additionally carries
// mailbox placement, a consumer carries nothing extra.
type NodeRole string

const (
	NodeRoleWorker   NodeRole = "worker"
	NodeRoleConsumer NodeRole = "consumer"
)

// Valid reports whether the role is one the control plane accepts. Anything
// else is refused at enrolment rather than stored and puzzled over later.
func (r NodeRole) Valid() bool {
	return r == NodeRoleWorker || r == NodeRoleConsumer
}

// NodeLivenessWindow is how long after its last beat a node is still treated as
// live. Generous relative to the 90s beat interval so one slow request, a GC
// pause or a brief network blip never looks like a dead machine.
const NodeLivenessWindow = 5 * time.Minute

// WorkerOperationalCapacity returns the assigned-mailbox target and utilization.
func WorkerOperationalCapacity(load, base, health float64) (target, utilization float64) {
	health = math.Max(0, math.Min(1, health))
	target = math.Floor(base * health)
	if target < 1 {
		target = 1
	}
	return target, load / target
}

// FleetNode is one Warmbly process running on a machine you own.
//
// Everything here is reported BY the node or resolved FOR it. Nothing is
// configured on it by hand: a node is identified by the id it enrols with, and
// the only operator-set fields are cosmetic (name, notes) or an explicit
// override (pinned_version).
type FleetNode struct {
	ID    uuid.UUID `json:"id"`
	Role  NodeRole  `json:"role"`
	Name  string    `json:"name"`
	Notes string    `json:"notes"`

	// Region is the sign-in geography hint placement scores on. Address is the
	// public IPv4 reported by the node, never a proxy observed by the control plane.
	Region  string `json:"region"`
	Address string `json:"address"`
	// CapacityTarget is the machine's operator-set assigned-mailbox target.
	CapacityTarget float64 `json:"capacity_target"`

	// Version is what the node reports it is running. DesiredVersion is what
	// the control plane wants it to run, resolved per role unless
	// PinnedVersion overrides it for this one machine.
	Version        string `json:"version"`
	PinnedVersion  string `json:"pinned_version,omitempty"`
	DesiredVersion string `json:"desired_version,omitempty"`

	Active     bool       `json:"active"`
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
	EnrolledAt time.Time  `json:"enrolled_at"`

	Usage NodeUsage `json:"usage"`

	// MailboxCount is how many mailboxes this node carries. Set for workers
	// only; nil for a consumer, which carries none by definition.
	MailboxCount *int `json:"mailbox_count,omitempty"`

	LastError string `json:"last_error,omitempty"`

	Tags []string `json:"tags,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// NodeUsage is the resource snapshot a node reports on every beat. A snapshot
// and not a history on purpose: worker_health_samples already keeps the time
// series capacity math reads, and a second per-node metrics table would grow
// without anyone querying it.
type NodeUsage struct {
	CPUPercent    *float64 `json:"cpu_percent,omitempty"`
	MemoryMB      *int     `json:"memory_mb,omitempty"`
	Goroutines    *int     `json:"goroutines,omitempty"`
	UptimeSeconds *int64   `json:"uptime_seconds,omitempty"`
	CPUScope      string   `json:"cpu_scope,omitempty"`
	MemoryScope   string   `json:"memory_scope,omitempty"`
	MemoryUsedMB  *int     `json:"memory_used_mb,omitempty"`
	MemoryLimitMB *int     `json:"memory_limit_mb,omitempty"`
	ResidentMB    *int     `json:"resident_mb,omitempty"`
}

// Live reports whether the node has beaten recently enough to be given work.
// Computed rather than stored so it can never go stale in the row.
func (n *FleetNode) Live() bool {
	if n == nil || !n.Active || n.LastSeenAt == nil {
		return false
	}
	return time.Since(*n.LastSeenAt) <= NodeLivenessWindow
}

// NeedsUpdate reports whether the node is running something other than what
// the control plane wants. An empty DesiredVersion means "no opinion", which
// happens before the first release is resolved and must never be read as
// "downgrade to nothing".
func (n *FleetNode) NeedsUpdate() bool {
	if n == nil || n.DesiredVersion == "" {
		return false
	}
	return n.Version != n.DesiredVersion
}

// NodeHeartbeat is what a node POSTs on every beat.
type NodeHeartbeat struct {
	WarmupSendProtocol int       `json:"warmup_send_protocol,omitempty"`
	NodeID             uuid.UUID `json:"node_id"`
	Role               NodeRole  `json:"role"`
	Name               string    `json:"name,omitempty"`
	Region             string    `json:"region,omitempty"`
	Address            string    `json:"address,omitempty"`
	// CapacityTarget is reported by workers. Zero means no opinion, which
	// preserves the stored/default target for older node versions.
	CapacityTarget float64   `json:"capacity_target,omitempty"`
	Version        string    `json:"version,omitempty"`
	Usage          NodeUsage `json:"usage"`
	// LastError is whatever went wrong since the previous beat, for the
	// dashboard. Empty clears it.
	LastError string `json:"last_error,omitempty"`
	// Booted is set on the first beat of a fresh process. A worker holds its
	// mailboxes in memory only, so the backend reloads them right away instead
	// of leaving it to the reconciler's next pass.
	Booted bool `json:"booted,omitempty"`
	// Stopping is set on the farewell beat, so the row goes inactive at once
	// rather than staying selectable until the beat ages out.
	Stopping bool `json:"stopping,omitempty"`
}

// NodeHeartbeatReply is the control plane's answer. It is the only channel by
// which a node is told to do anything, which is what keeps the model pull-only.
type NodeHeartbeatReply struct {
	// DesiredVersion is the image tag this node should be running. The node
	// compares it to its own and updates itself when they differ. Empty means
	// the control plane has no opinion yet; the node must leave itself alone.
	DesiredVersion string `json:"desired_version,omitempty"`
	// LivenessSeconds tells the node how long its beat is trusted for, so the
	// beat interval and the server's window can never drift apart.
	LivenessSeconds int `json:"liveness_seconds"`
}
