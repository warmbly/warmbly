// Mirror of models/apollo.go.

import type { IntegrationHealth, IntegrationStatus } from "./Integration";

export interface ApolloList {
    id: string;
    name: string;
    count: number;
}

export interface ApolloRunResult {
    read: number;
    imported: number;
    updated: number;
    skipped: number;
    failed: number;
    no_email: number;
    unverified: number;
    suppressed: number;
    truncated?: boolean;
    already_seen: number;
    next_page?: number;
}

export interface ApolloImportSource {
    id: string;
    organization_id: string;
    connection_id: string;
    created_by_user_id?: string;
    name: string;
    list_id: string;
    list_name: string;
    campaign_id?: string | null;
    category_ids: string[];
    segment_ids: string[];
    verified_only: boolean;
    recurring: boolean;
    enabled: boolean;
    status: "idle" | "running" | "error";
    last_run_at?: string;
    last_result?: ApolloRunResult;
    last_error?: string;
    total_imported: number;
    created_at: string;
    updated_at: string;
}

export interface CreateApolloImportSourceInput {
    name?: string;
    list_id: string;
    list_name: string;
    campaign_id?: string;
    category_ids?: string[];
    segment_ids?: string[];
    verified_only?: boolean;
    recurring?: boolean;
}

export interface UpdateApolloImportSourceInput {
    name?: string;
    campaign_id?: string | null;
    category_ids?: string[];
    segment_ids?: string[];
    verified_only?: boolean;
    recurring?: boolean;
    enabled?: boolean;
}

export interface ApolloPreviewRow {
    apollo_id: string;
    name: string;
    email: string;
    email_status: string;
    title: string;
    company: string;
    already_in_warmbly: boolean;
    will_import: boolean;
}

export interface ApolloPreview {
    total: number;
    sample: ApolloPreviewRow[];
}

export type ApolloEnrichmentStatus = "queued" | "running" | "done" | "error";

export interface ApolloEnrichmentRun {
    id: string;
    organization_id: string;
    connection_id: string;
    requested_by_user_id?: string;
    overwrite: boolean;
    status: ApolloEnrichmentStatus;
    total: number;
    processed: number;
    enriched: number;
    not_found: number;
    failed: number;
    credits_consumed: number;
    last_error?: string;
    started_at?: string;
    finished_at?: string;
    created_at: string;
    updated_at: string;
}

export interface ApolloOverview {
    connection_id: string;
    label: string;
    status: IntegrationStatus;
    health: IntegrationHealth;
    health_detail?: string;
    account_name?: string;
    account_email?: string;
    sources: number;
    total_imported: number;
    last_run_at?: string;
}

// Apollo's email_status values, as the preview shows them.
export const APOLLO_EMAIL_STATUS_LABELS: Record<string, string> = {
    verified: "Verified",
    guessed: "Guessed",
    unavailable: "Unavailable",
    bounced: "Bounced",
    pending_manual_fulfillment: "Pending",
    extrapolated: "Extrapolated",
    likely_to_engage: "Likely to engage",
};
