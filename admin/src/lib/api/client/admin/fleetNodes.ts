// /admin/fleet/* — the fleet as the operator sees it.
//
// Nodes are pull-based: they enrol with the join token, heartbeat, and ask what
// version they should be running. Nothing here reaches into a machine, so there
// is no install, restart or reboot call to make. Redacted submitted evidence
// is read through the generic node diagnostics API.

import { Request } from "@/lib/api/client";

export type NodeRole = "worker" | "consumer";

export interface NodeUsage {
    cpu_percent?: number;
    memory_mb?: number;
    goroutines?: number;
    uptime_seconds?: number;
    cpu_scope?: "host" | "container";
    memory_scope?: "host" | "container";
    memory_used_mb?: number;
    memory_limit_mb?: number;
    resident_mb?: number;
}

export interface FleetNode {
    id: string;
    role: NodeRole;
    name: string;
    notes: string;
    region: string;
    address: string;
    /** Operator-set assigned-mailbox target reported by a worker. */
    capacity_target: number;
    /** What the node reports it is running. */
    version: string;
    /** Set when this one node is held at a version, overriding the fleet target. */
    pinned_version?: string;
    /** What the control plane wants it to run. Empty means "no opinion". */
    desired_version?: string;
    active: boolean;
    last_seen_at?: string | null;
    enrolled_at: string;
    usage: NodeUsage;
    /** How much mail this node carries. Workers only; absent for a consumer. */
    mailbox_count?: number;
    last_error?: string;
    tags?: string[] | null;
    created_at: string;
    updated_at: string;
}

export function listFleetNodes(role?: NodeRole): Promise<{ data: FleetNode[] }> {
    const q = role ? `?role=${role}` : "";
    return Request({
        method: "GET",
        url: `/admin/fleet/nodes${q}`,
        authorization: true,
    });
}

// The token is returned once and never again: only its hash is stored. It joins
// any number of machines until expires_at; issuing a new one revokes the
// previous token. Nodes already enrolled are unaffected.
export function issueJoinToken(): Promise<{ token: string; expires_at: string; note: string }> {
    return Request({
        method: "POST",
        url: "/admin/fleet/join-token",
        authorization: true,
    });
}

/** Liveness matches the server's window: a node is live if it beat recently. */
export const NODE_LIVENESS_MS = 5 * 60 * 1000;

export function nodeIsLive(n: FleetNode): boolean {
    if (!n.active || !n.last_seen_at) return false;
    return Date.now() - new Date(n.last_seen_at).getTime() <= NODE_LIVENESS_MS;
}

export type NodeState = "live" | "unreachable" | "stopped";

export function nodeState(n: FleetNode): NodeState {
    if (!n.active) return "stopped";
    return nodeIsLive(n) ? "live" : "unreachable";
}

/** True when the node is running something other than what it should be. */
export function nodeNeedsUpdate(n: FleetNode): boolean {
    if (!n.desired_version) return false;
    return n.version !== n.desired_version;
}

export interface FleetRelease {
    channel: "stable" | "dev" | "pinned";
    /** The resolved tag. Empty means nothing has been resolved yet. */
    tag: string;
    resolved_at: string;
    source?: string;
}

export function getFleetRelease(): Promise<FleetRelease> {
    return Request({ method: "GET", url: "/admin/fleet/release", authorization: true });
}

// Setting a tag also pins the channel, so a release landing later does not
// silently undo a deliberate rollback.
export function setFleetRelease(body: {
    channel?: FleetRelease["channel"];
    tag?: string;
}): Promise<FleetRelease> {
    return Request({ method: "PUT", url: "/admin/fleet/release", data: body, authorization: true });
}

export function patchFleetNode(
    id: string,
    body: { name?: string; notes?: string; pinned_version?: string },
): Promise<FleetNode> {
    return Request({ method: "PATCH", url: `/admin/fleet/nodes/${id}`, data: body, authorization: true });
}

// Forgetting a node does not stop it: a process still running re-joins on its
// next heartbeat. Stop the service on the machine too.
export function deleteFleetNode(id: string): Promise<{ ok: boolean; note: string }> {
    return Request({ method: "DELETE", url: `/admin/fleet/nodes/${id}`, authorization: true });
}
