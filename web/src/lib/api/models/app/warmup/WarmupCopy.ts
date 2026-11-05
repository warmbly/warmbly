// Warmup own copy: the workspace's running campaign steps and warmup templates
// that its warmup pool carries, and where each landed.

export interface WarmupCopySettings {
    // Percent (0 to 100) of a mailbox's new warmup openings drawn from own copy.
    template_share: number;
    // Send only own copy; an opening with nothing eligible is skipped.
    templates_only: boolean;
    // Warm up the opening steps of running campaigns.
    campaign_copy: boolean;
}

// Headline counts are Google, Microsoft and Yahoo recipients only.
export interface WarmupCopyPlacement {
    sent: number;
    // Sends whose recipient filed a receipt; landing rates are read over this.
    reported: number;
    inbox: number;
    tabs: number;
    spam: number;
    rescued: number;
}

export type WarmupCopyProviderGroup = "google" | "microsoft" | "yahoo" | "other";

export interface WarmupCopyGroupPlacement extends WarmupCopyPlacement {
    group: WarmupCopyProviderGroup | (string & {});
}

export interface WarmupCampaignCopyStep {
    campaign_id: string;
    campaign_name: string;
    campaign_status: string;
    sequence_id: string;
    step_name: string;
    position: number;
    // Null is the step's own copy; otherwise one of its active A/B variants.
    variant_id: string | null;
    variant_name: string;
    // As written, merge fields included.
    subject: string;
    eligible: boolean;
    ineligible_reason?: string;
    placement: WarmupCopyPlacement;
    by_group: WarmupCopyGroupPlacement[] | null;
}

export interface WarmupCampaignCopyList {
    days: number;
    data: WarmupCampaignCopyStep[] | null;
}

export interface WarmupTemplate {
    id: string;
    name: string;
    subject: string;
    // Only returned on create.
    body?: string;
    segment: string;
    usage_count: number;
    // Revived to a Date by the request layer.
    created_at: Date;
    placement?: WarmupCopyPlacement;
}

export interface WarmupTemplateList {
    days: number;
    templates: WarmupTemplate[] | null;
}

export interface WarmupTemplateInput {
    name: string;
    subject: string;
    body: string;
    segment: string;
}

export interface WarmupTemplateCheck {
    subject_length: number;
    word_count: number;
    reading_time_seconds: number;
    link_count: number;
    question_count: number;
    spam_trigger_hits: string[] | null;
    blocked_reason?: string;
}

export interface CreatedWarmupTemplate {
    template: WarmupTemplate;
    check: WarmupTemplateCheck;
}

export interface WarmupTemplateStats {
    template_id: string;
    days: number;
    total: WarmupCopyPlacement;
    groups: WarmupCopyGroupPlacement[] | null;
}

export const WARMUP_COPY_DAYS = [7, 30, 90] as const;
export type WarmupCopyDays = (typeof WARMUP_COPY_DAYS)[number];
export const DEFAULT_WARMUP_COPY_DAYS: WarmupCopyDays = 30;

// The days search param, falling back to the default for anything else.
export function parseWarmupCopyDays(raw: string | null | undefined): WarmupCopyDays {
    const n = Number(raw);
    return (WARMUP_COPY_DAYS as readonly number[]).includes(n) ? (n as WarmupCopyDays) : DEFAULT_WARMUP_COPY_DAYS;
}

export const PROVIDER_GROUP_LABEL: Record<WarmupCopyProviderGroup, string> = {
    google: "Google",
    microsoft: "Microsoft",
    yahoo: "Yahoo",
    other: "Other providers",
};
