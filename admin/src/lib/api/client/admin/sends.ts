// /admin/sends/*, /admin/tasks/*, /admin/webhooks/* — the send outcome loop
// as the operator sees it: reservations still waiting on a worker result,
// tasks that exhausted their retries, recent task failures, and customer
// webhook delivery health. Shapes mirror internal/models/admin_ops.go.

import { Request } from "@/lib/api/client";
import { buildSearchQuery } from "@/lib/api/client/admin/query";
import type { AdminOpsPagination } from "@/lib/api/client/admin/sync";

// ---- in-flight sends ----

// A reserved campaign send no EMAIL_SENT / EMAIL_FAILED has resolved yet.
export interface AdminInFlightSend {
    campaign_id: string;
    campaign_name: string;
    organization_id?: string;
    organization_name: string;
    contact_id: string;
    contact_email: string;
    sequence_id: string;
    task_id?: string;
    task_status: string;
    // The worker did put the mail on the wire and only the stamp was lost;
    // the reclaimer will stamp it rather than retry.
    has_message_id: boolean;
    email_account_id?: string;
    mailbox_email: string;
    worker_id?: string;
    dispatched_at: string;
    age_seconds: number;
}

export interface AdminInFlightSummary {
    total: number;
    under_5m: number;
    under_30m: number;
    past_reclaim_window: number;
    oldest_dispatched_at?: string;
    // config.CampaignSendReclaimAfterMinutes.
    reclaim_after_minutes: number;
}

export interface AdminInFlightResult {
    summary: AdminInFlightSummary;
    data: AdminInFlightSend[];
}

// ---- dead letters ----

export type AdminDeadLetterStatus = "pending" | "replayed" | "failed";

export interface AdminDeadLetterRow {
    id: string;
    task_id: string;
    task_type: string;
    payload: Record<string, unknown>;
    last_error: string;
    attempts: number;
    max_attempts: number;
    status: AdminDeadLetterStatus | string;
    next_retry_at?: string;
    replayed_at?: string;
    created_at: string;
    updated_at: string;
    organization_id?: string;
    organization_name: string;
}

export interface AdminDeadLettersSearch {
    status?: AdminDeadLetterStatus | "all";
    cursor?: string;
    limit?: number;
}

export interface AdminDeadLettersResult {
    data: AdminDeadLetterRow[];
    pagination: AdminOpsPagination | null;
    // Counts by status across the instance, independent of the filter.
    pending: number;
    replayed: number;
    failed: number;
}

// ---- task failures ----

export interface AdminTaskFailureRow {
    task_id: string;
    task_type: string;
    task_status: string;
    title: string;
    message: string;
    email_account_id: string;
    mailbox_email: string;
    organization_id?: string;
    organization_name: string;
    occurred_at: string;
}

export interface AdminTaskFailuresResult {
    data: AdminTaskFailureRow[];
}

// ---- webhooks ----

export interface AdminWebhookEndpointRow {
    id: string;
    organization_id: string;
    organization_name: string;
    url: string;
    description: string;
    enabled: boolean;
    event_types: string[];
    consecutive_failures: number;
    last_success_at?: string;
    last_failure_at?: string;
    last_failure_reason: string;
    deliveries_last_7d: number;
    failed_last_7d: number;
    drops_last_7d: number;
}

export interface AdminWebhookHealth {
    // Deliveries claimed longer ago than the lease.
    in_flight_stale: number;
    pending_due: number;
    delivered_last_24h: number;
    failed_last_24h: number;
    abandoned_last_24h: number;
    drops_last_7d: number;
    lease_minutes: number;
    // Endpoints with consecutive failures, worst first.
    failing_endpoints: AdminWebhookEndpointRow[];
}

export interface AdminWebhookReclaimResult {
    reclaimed: number;
}

// ---- calls ----

export function listInFlightSends(limit?: number): Promise<AdminInFlightResult> {
    return Request({
        method: "GET",
        url: `/admin/sends/in-flight${buildSearchQuery({ limit })}`,
        authorization: true,
    });
}

export function listDeadLetters(params: AdminDeadLettersSearch = {}): Promise<AdminDeadLettersResult> {
    const { status, ...rest } = params;
    return Request({
        method: "GET",
        url: `/admin/tasks/dead-letters${buildSearchQuery({
            ...rest,
            status: status && status !== "all" ? status : undefined,
        })}`,
        authorization: true,
    });
}

export function replayDeadLetter(id: string): Promise<void> {
    return Request({
        method: "POST",
        url: `/admin/tasks/dead-letters/${id}/replay`,
        authorization: true,
    });
}

export function listTaskFailures(limit?: number): Promise<AdminTaskFailuresResult> {
    return Request({
        method: "GET",
        url: `/admin/tasks/failures${buildSearchQuery({ limit })}`,
        authorization: true,
    });
}

export function getWebhookHealth(): Promise<AdminWebhookHealth> {
    return Request({ method: "GET", url: "/admin/webhooks/health", authorization: true });
}

export function reclaimWebhookDeliveries(): Promise<AdminWebhookReclaimResult> {
    return Request({ method: "POST", url: "/admin/webhooks/reclaim", authorization: true });
}
