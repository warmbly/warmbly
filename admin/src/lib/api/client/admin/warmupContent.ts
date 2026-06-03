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
    pool_type: string;
    segment?: string;
    theme?: string;
    model?: string;
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
