// /admin/sync/* — the mailbox sync governor: every mailbox's backfill and
// fair-use throttle state as the platform recorded it from SYNC_STATE.
// Shapes mirror internal/models/admin_ops.go (snake_case).

import { Request } from "@/lib/api/client";
import { buildSearchQuery } from "@/lib/api/client/admin/query";

export interface AdminOpsPagination {
    total: number | null;
    next_cursor: string | null;
    has_more: boolean;
}

export type AdminSyncStateFilter =
    | "all"
    | "throttled"
    | "backfilling"
    | "stalled"
    | "pending"
    | "complete";

export const ADMIN_SYNC_STATES: AdminSyncStateFilter[] = [
    "all",
    "throttled",
    "backfilling",
    "stalled",
    "pending",
    "complete",
];

export interface AdminSyncSearch {
    state?: AdminSyncStateFilter;
    q?: string;
    cursor?: string;
    limit?: number;
}

export interface AdminSyncSummary {
    total: number;
    throttled: number;
    backfilling: number;
    stalled: number;
    pending: number;
    complete: number;
    deferred: number;
}

export interface AdminSyncRow {
    email_id: string;
    user_id: string;
    email: string;
    provider: string;
    account_status: string;
    organization_id?: string;
    organization_name: string;
    worker_id?: string;

    backfill_status: string;
    backfill_synced: number;
    backfill_since?: string;
    backfill_started_at?: string;
    backfill_completed_at?: string;

    throttled_until?: string;
    throttle_reason: string;
    deferred: number;
    // A running backfill whose state has not moved for an hour.
    stalled: boolean;
    last_synced_at?: string;
    updated_at: string;
}

export interface AdminSyncResult {
    data: AdminSyncRow[];
    pagination: AdminOpsPagination | null;
    summary: AdminSyncSummary;
}

// Both POSTs re-ship the mailbox to its worker after the write; `reloaded`
// reports whether that took, and `reload_error` says why when it did not.
export interface AdminSyncActionResult {
    email_id: string;
    cleared?: boolean;
    reset?: boolean;
    reloaded: boolean;
    reload_error?: string;
}

export function searchSync(params: AdminSyncSearch = {}): Promise<AdminSyncResult> {
    return Request({
        method: "GET",
        url: `/admin/sync${buildSearchQuery(params as Record<string, unknown>)}`,
        authorization: true,
    });
}

export function clearSyncThrottle(emailId: string): Promise<AdminSyncActionResult> {
    return Request({
        method: "POST",
        url: `/admin/sync/${emailId}/clear-throttle`,
        authorization: true,
    });
}

export function restartSyncBackfill(emailId: string): Promise<AdminSyncActionResult> {
    return Request({
        method: "POST",
        url: `/admin/sync/${emailId}/restart-backfill`,
        authorization: true,
    });
}
