// /admin/warmup-content/* — admin control + visibility surface for the
// offline AI warmup-content generator.
//
// The generator runs OUT OF BAND: `generate` enqueues a background job and
// returns a job id; the Jobs view polls for progress. This module just shapes
// the requests and returns the backend's `{ data }` / `{ pagination }`
// envelopes. Types are co-located here since this surface is self-contained.

import { Request } from "@/lib/api/client";
import { buildSearchQuery } from "@/lib/api/client/admin/query";

// --------------------------------------------------------------------
// Types
// --------------------------------------------------------------------

export interface WarmupContentPoolBreakdown {
    pool_type: string;
    segment: string;
    source: string;
    active: number;
    archived: number;
}

export interface WarmupSegmentStock {
    segment: string;
    active: number;
    target: number;
    recent_sends: number;
    average_daily_sends: number;
}

export interface WarmupContentOverview {
    total_active: number;
    total_archived: number;
    by_pool: WarmupContentPoolBreakdown[];
    last_generated_at: string | null;
    ai_enabled: boolean;
    schedule_enabled: boolean;
    /** Whether an AI client is wired on the backend (OPENAI_API_KEY set). */
    ai_configured: boolean;
    cadence_hours: number;
    refresh_enabled: boolean;
    refresh_per_run: number;
    ai_selection_share: number;
    daily_generation_cap: number;
    generated_today: number;
    stock: WarmupSegmentStock[];
}

export interface WarmupConversationRow {
    id: string;
    pool_type: string;
    segment: string;
    source: string;
    theme: string;
    subject: string;
    description: string;
    message_count: number;
    status: string;
    lint_passed: boolean;
    usage_count: number;
    created_at: string;
}

export interface WarmupConversationDetail {
    id: string;
    pool_type: string;
    segment: string;
    source: string;
    theme: string;
    subject: string;
    description: string;
    messages: string[];
    status: string;
    lint_passed: boolean;
    usage_count: number;
    generated_by_job_id: string | null;
    created_at: string;
    updated_at: string;
}

/** Whether a job ran inline ("sync") or via the OpenAI Batch API ("batch"). */
export type WarmupGenerationMode = "sync" | "batch";

/**
 * OpenAI Batch lifecycle status, surfaced verbatim for batch-mode jobs.
 * Terminal states are `completed`, `failed`, `expired`, and `cancelled`.
 */
export type WarmupBatchStatus =
    | "validating"
    | "in_progress"
    | "finalizing"
    | "completed"
    | "failed"
    | "expired"
    | "cancelling"
    | "cancelled";

const TERMINAL_BATCH_STATUS: ReadonlySet<string> = new Set([
    "completed",
    "failed",
    "expired",
    "cancelled",
]);

const TERMINAL_JOB_STATUS: ReadonlySet<string> = new Set([
    "completed",
    "succeeded",
    "failed",
    "error",
    "cancelled",
    "canceled",
]);

export interface WarmupGenerationJob {
    id: string;
    requested_by: string | null;
    trigger: string;
    pool_type: string;
    segment: string;
    theme: string;
    model: string;
    requested_count: number;
    generated_count: number;
    lint_rejected_count: number;
    failed_count: number;
    status: string;
    error: string;
    started_at: string | null;
    finished_at: string | null;
    created_at: string;
    /** "sync" for inline jobs, "batch" for OpenAI Batch API jobs. */
    mode?: WarmupGenerationMode;
    /** Batch-only fields — present when `mode === "batch"`. */
    batch_id?: string | null;
    batch_input_file_id?: string | null;
    batch_output_file_id?: string | null;
    batch_status?: WarmupBatchStatus | null;
    completion_window?: string | null;
}

/**
 * True while a job is still doing work and should keep being polled. Covers
 * both the inline `status` lifecycle and the OpenAI `batch_status` lifecycle.
 */
export function isJobActive(job: WarmupGenerationJob): boolean {
    if (job.mode === "batch") {
        const bs = job.batch_status ?? "";
        if (bs) return !TERMINAL_BATCH_STATUS.has(bs);
    }
    return !TERMINAL_JOB_STATUS.has(job.status);
}

/** Only batch jobs that are still in flight can be cancelled. */
export function isJobCancellable(job: WarmupGenerationJob): boolean {
    if (job.mode !== "batch") return false;
    const bs = job.batch_status ?? "";
    if (!bs) return !TERMINAL_JOB_STATUS.has(job.status);
    return !TERMINAL_BATCH_STATUS.has(bs);
}

export interface WarmupAbRow {
    content_source: string;
    sent: number;
    spam_placements: number;
    spam_placement_rate: number;
}

export interface Pagination {
    total: number;
    has_more: boolean;
    next_cursor: string | null;
}

export interface WarmupConversationsResult {
    data: WarmupConversationRow[];
    pagination: Pagination;
}

export interface WarmupGenerationJobsResult {
    data: WarmupGenerationJob[];
    pagination: Pagination;
}

export interface WarmupAbResult {
    data: WarmupAbRow[];
    window_days: number;
}

export interface ListConversationsParams {
    pool?: string;
    segment?: string;
    source?: string;
    status?: string;
    cursor?: string;
    limit?: number;
}

export interface ListJobsParams {
    cursor?: string;
    limit?: number;
}

// --------------------------------------------------------------------
// Overview
// --------------------------------------------------------------------

export function getWarmupContentOverview(): Promise<WarmupContentOverview> {
    return Request({
        method: "GET",
        url: "/admin/warmup-content/overview",
        authorization: true,
    });
}

// --------------------------------------------------------------------
// Library (conversations)
// --------------------------------------------------------------------

export function listWarmupConversations(
    params: ListConversationsParams = {},
): Promise<WarmupConversationsResult> {
    return Request({
        method: "GET",
        url: `/admin/warmup-content/conversations${buildSearchQuery(
            params as Record<string, unknown>,
        )}`,
        authorization: true,
    });
}

export function getWarmupConversation(
    id: string,
): Promise<{ data: WarmupConversationDetail }> {
    return Request({
        method: "GET",
        url: `/admin/warmup-content/conversations/${id}`,
        authorization: true,
    });
}

export function archiveWarmupConversation(id: string): Promise<{ ok: true }> {
    return Request({
        method: "POST",
        url: `/admin/warmup-content/conversations/${id}/archive`,
        authorization: true,
    });
}

export function unarchiveWarmupConversation(id: string): Promise<{ ok: true }> {
    return Request({
        method: "POST",
        url: `/admin/warmup-content/conversations/${id}/unarchive`,
        authorization: true,
    });
}

export function deleteWarmupConversation(id: string): Promise<{ ok: true }> {
    return Request({
        method: "DELETE",
        url: `/admin/warmup-content/conversations/${id}`,
        authorization: true,
    });
}

/** Cancel an in-flight batch job. 400 if it isn't a cancellable batch job. */
export function cancelWarmupBatch(jobId: string): Promise<{ ok: true }> {
    return Request({
        method: "POST",
        url: `/admin/warmup-content/jobs/${jobId}/cancel`,
        authorization: true,
    });
}

// --------------------------------------------------------------------
// Jobs
// --------------------------------------------------------------------

export function listWarmupGenerationJobs(
    params: ListJobsParams = {},
): Promise<WarmupGenerationJobsResult> {
    return Request({
        method: "GET",
        url: `/admin/warmup-content/jobs${buildSearchQuery(
            params as Record<string, unknown>,
        )}`,
        authorization: true,
    });
}

export function getWarmupGenerationJob(
    id: string,
): Promise<{ data: WarmupGenerationJob }> {
    return Request({
        method: "GET",
        url: `/admin/warmup-content/jobs/${id}`,
        authorization: true,
    });
}

// --------------------------------------------------------------------
// A/B (content source vs spam placement)
// --------------------------------------------------------------------

export function getWarmupContentAb(days?: number): Promise<WarmupAbResult> {
    return Request({
        method: "GET",
        url: `/admin/warmup-content/ab${buildSearchQuery({ days })}`,
        authorization: true,
    });
}
