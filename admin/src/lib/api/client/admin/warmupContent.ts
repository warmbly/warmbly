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

export interface WarmupContentOverview {
    total_active: number;
    total_archived: number;
    by_pool: WarmupContentPoolBreakdown[];
    last_generated_at: string | null;
    ai_enabled: boolean;
    schedule_enabled: boolean;
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

export interface WarmupGenerationPoolConfig {
    pool_type: string;
    enabled: boolean;
    target_active_threads: number;
    segments: string[];
}

export interface WarmupGenerationEngagement {
    spam_rescue_rate: number;
    mark_important_rate: number;
    mark_read_rate: number;
    star_rate: number;
    min_dwell_seconds: number;
    max_dwell_seconds: number;
}

export interface WarmupGenerationSettings {
    enabled: boolean;
    schedule_enabled: boolean;
    cadence_hours: number;
    model: string;
    max_messages_per_thread: number;
    daily_generation_cap: number;
    /** 0-100 — share of warmup sends that draw from AI-generated content. */
    ai_selection_share: number;
    pools: WarmupGenerationPoolConfig[];
    engagement: WarmupGenerationEngagement;
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

export interface GenerateContentRequest {
    count: number;
    /**
     * Reputation pool the threads nominally belong to. Optional — warmup
     * content is a single shared library, so the UI omits this and the
     * backend defaults it to "premium".
     */
    pool_type?: string;
    segment?: string;
    theme?: string;
    model?: string;
}

/**
 * Batch generation request. Every field is optional; the backend applies
 * defaults. If `themes` is non-empty the backend fans out one batch job per
 * theme, and `count` is interpreted as threads-per-job (default 100, clamped
 * to 2000 and the daily cap). `completion_window` defaults to "24h".
 */
export interface GenerateBatchRequest {
    pool_type?: string;
    segment?: string;
    theme?: string;
    themes?: string[];
    model?: string;
    count?: number;
    max_messages?: number;
    completion_window?: string;
}

export interface GenerateBatchResult {
    job_ids: string[];
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

// --------------------------------------------------------------------
// Generate (offline job)
// --------------------------------------------------------------------

export function generateWarmupContent(
    body: GenerateContentRequest,
): Promise<{ job_id: string }> {
    return Request({
        method: "POST",
        url: "/admin/warmup-content/generate",
        data: body,
        authorization: true,
    });
}

// --------------------------------------------------------------------
// Batch generate (OpenAI Batch API — async, cheaper, large volume)
// --------------------------------------------------------------------

/**
 * Enqueue one or more OpenAI Batch jobs. Returns the created job ids; watch
 * them on the Jobs view via `listWarmupGenerationJobs`. A 400 with code
 * `not_configured` / `daily_cap_reached` means generation can't run yet.
 */
export function submitWarmupBatch(
    body: GenerateBatchRequest,
): Promise<GenerateBatchResult> {
    return Request({
        method: "POST",
        url: "/admin/warmup-content/batch",
        data: body,
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
// Settings
// --------------------------------------------------------------------

export function getWarmupGenerationSettings(): Promise<{
    data: WarmupGenerationSettings;
}> {
    return Request({
        method: "GET",
        url: "/admin/warmup-content/settings",
        authorization: true,
    });
}

export function updateWarmupGenerationSettings(
    body: WarmupGenerationSettings,
): Promise<{ ok: true }> {
    return Request({
        method: "PUT",
        url: "/admin/warmup-content/settings",
        data: body,
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
