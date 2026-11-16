// /admin/fleet/* — placement as the operator sees it: every worker against
// its capacity row, the decision log the control loops write, and the
// isolated-egress reservations. Shapes mirror the fleet section of
// internal/models/admin_ops.go (snake_case, as the backend serializes them).

import { Request } from "@/lib/api/client";
import type { WorkerHealthState } from "@/lib/api/models/admin";
import type { NodeUsage } from "./fleetNodes";

export interface AdminFleetWorkerRow {
    worker_id: string;
    name: string;
    ip_addr: string;
    active: boolean;
    region: string;
    health_state: WorkerHealthState;
    install_state: string;
    last_seen_at?: string | null;
    live: boolean;
    account_count: number;
    tags: string[] | null;
    usage: NodeUsage;

    load_score: number;
    base_capacity: number;
    health_multiplier: number;
    age_multiplier: number;
    effective_capacity: number;
    /** Assigned mailbox count divided by the worker's planning target. */
    utilization: number;
    sends_attempted_1h: number;
    sends_succeeded_1h: number;
    bounces_hard_1h: number;
    bounces_soft_1h: number;
    complaints_1h: number;
    auth_errors_1h: number;
}

export interface AdminFleetDecision {
    id: number;
    kind: string;
    worker_id?: string | null;
    worker_name: string;
    mailbox_id?: string | null;
    /** Raw JSON as the control loop recorded it; shape depends on `kind`. */
    before?: unknown;
    after?: unknown;
    reason: string;
    triggered_by: string;
    created_at: string;
}

export interface AdminDedicatedAssignment {
    id: string;
    worker_id: string;
    worker_name: string;
    worker_live: boolean;
    organization_id: string;
    organization_name: string;
    subscription_id: string;
    assigned_at: string;
    released_at?: string | null;
    account_count: number;
}

export interface AdminReserveWorkerRequest {
    organization_id: string;
    subscription_id: string;
}

// Mirrors the /reserve handler. Reserving no longer drains anything: the
// rotation loop moves other tenants off on its own schedule.
export interface AdminReserveWorkerResponse {
    ok: boolean;
    worker_id: string;
    new_reservation: boolean;
}

export interface AdminReleaseDedicatedResponse {
    ok: boolean;
    worker_id: string;
    accounts_remaining: number;
}

export interface FleetDecisionsParams {
    kind?: string;
    worker_id?: string;
    limit?: number;
}

export function getFleetCapacity(): Promise<{ data: AdminFleetWorkerRow[] | null }> {
    return Request({
        method: "GET",
        url: "/admin/fleet/capacity",
        authorization: true,
    });
}

export function listFleetDecisions(
    params: FleetDecisionsParams = {},
): Promise<{ data: AdminFleetDecision[] | null }> {
    const usp = new URLSearchParams();
    if (params.kind) usp.set("kind", params.kind);
    if (params.worker_id) usp.set("worker_id", params.worker_id);
    if (params.limit) usp.set("limit", String(params.limit));
    const q = usp.toString();
    return Request({
        method: "GET",
        url: `/admin/fleet/decisions${q ? `?${q}` : ""}`,
        authorization: true,
    });
}

export function listDedicatedAssignments(): Promise<{ data: AdminDedicatedAssignment[] | null }> {
    return Request({
        method: "GET",
        url: "/admin/fleet/dedicated",
        authorization: true,
    });
}

// Releases the reservation. Nothing migrates: the worker carries no category to
// reset, and the workspace's mailboxes stay put until the rotation loop finds
// them a better home on its own schedule.
export function releaseDedicatedWorker(orgId: string): Promise<AdminReleaseDedicatedResponse> {
    return Request({
        method: "POST",
        url: `/admin/fleet/dedicated/${orgId}/release`,
        authorization: true,
    });
}

// Reserves a worker for one organization. It only writes the binding; the
// mailboxes already on it drift away on the rotation loop.
export function convertWorkerToDedicated(
    workerId: string,
    body: AdminReserveWorkerRequest,
): Promise<AdminReserveWorkerResponse> {
    return Request({
        method: "POST",
        url: `/admin/workers/${workerId}/reserve`,
        authorization: true,
        data: body,
    });
}

// The worker Auto Scaling Group's size against its cap; mirrors fleetscale.Status.
export interface AdminFleetScale {
    group: string;
    min: number;
    max: number;
    desired: number;
    in_service: number;
    pending: number;
    /** Highest cap the backend accepts; 0 when the group carries no ceiling tag. */
    ceiling: number;
}

/** The code answered by an instance that does not manage a worker Auto Scaling Group. */
export const FLEET_AUTOSCALING_NOT_CONFIGURED = "fleet_autoscaling_not_configured";

export function getFleetScale(): Promise<AdminFleetScale> {
    return Request({
        method: "GET",
        url: "/admin/fleet/scale",
        authorization: true,
    });
}

// Moves the cap; the backend refuses anything below the running fleet or above the ceiling.
export function setFleetMax(max: number): Promise<AdminFleetScale> {
    return Request({
        method: "PATCH",
        url: "/admin/fleet/scale",
        authorization: true,
        data: { max },
    });
}
