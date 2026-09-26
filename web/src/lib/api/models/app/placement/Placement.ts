// Inbox placement tests (mirrors internal/models/placement.go and
// internal/app/placement/view.go). Timestamps arrive as ISO strings and are
// revived into Date objects by Request.
import type Pagination from "../Pagination";
import type { TemplateScoreIssue } from "../campaigns/TemplateScore";

export type PlacementPanel = "instance" | "workspace" | "cloud";
export type PlacementTracking = "campaign" | "on" | "off" | "compare";
export type PlacementTestStatus = "running" | "completed" | "cancelled" | "failed";
export type PlacementOrigin = "manual" | "monitor" | "admin" | "remote";
export type PlacementFolder = "pending" | "inbox" | "promotions" | "other" | "spam" | "missing" | "failed" | "cancelled";

export interface PlacementPanelFamily {
    family: string;
    label: string;
    seeds: number;
}

export interface PlacementPanelInfo {
    panel: PlacementPanel;
    available: boolean;
    /** One sentence saying why, when the panel is unavailable. */
    reason?: string;
    seeds: number;
    families: PlacementPanelFamily[] | null;
    /** Counts against the monthly allowance. */
    metered: boolean;
}

export interface PlacementUsage {
    used: number;
    /** null = unmetered. */
    limit: number | null;
    period_start: Date;
    period_end: Date;
}

export interface PlacementOverview {
    panels: PlacementPanelInfo[];
    usage: PlacementUsage;
    workspace_seeds: number;
    /** Most seeds one test sends to. */
    seeds_per_test: number;
    spacing_seconds: number;
}

// Rates are fractions 0..1 of `delivered`, null while nothing has a verdict.
export interface PlacementCounts {
    total: number;
    pending: number;
    inbox: number;
    promotions: number;
    other: number;
    spam: number;
    missing: number;
    failed: number;
    cancelled: number;
    /** inbox + promotions + other + spam + missing. */
    delivered: number;
    inbox_rate: number | null;
    tabs_rate: number | null;
    spam_rate: number | null;
    missing_rate: number | null;
}

export interface PlacementFamilyCounts {
    family: string;
    label: string;
    counts: PlacementCounts;
}

export interface PlacementTest {
    id: string;
    sender_account_id: string | null;
    sender_email: string;
    created_by: string | null;
    campaign_id: string | null;
    sequence_id: string | null;
    contact_id: string | null;
    monitor_id: string | null;
    subject: string;
    /** Only on GET /placement/tests/:id. */
    body_html?: string;
    body_plain?: string;
    open_tracking: boolean;
    link_tracking: boolean;
    compare_group_id: string | null;
    origin: PlacementOrigin;
    panel: PlacementPanel;
    status: PlacementTestStatus;
    error?: string;
    created_at: Date;
    finished_at: Date | null;
    summary: PlacementCounts;
    families: PlacementFamilyCounts[] | null;
}

export interface PlacementResult {
    /** Masked ("a***@gmail.com") on the instance and cloud panels. */
    seed: string;
    family: string;
    family_label: string;
    folder: PlacementFolder;
    scheduled_at: Date | null;
    sent_at: Date | null;
    detected_at: Date | null;
    error?: string;
}

export interface PlacementContentCheck {
    score: number;
    issues: TemplateScoreIssue[] | null;
}

export interface PlacementTestDetail extends PlacementTest {
    results: PlacementResult[] | null;
    content: PlacementContentCheck;
    /** The other half of a tracking comparison. */
    compare?: PlacementTest;
}

export interface PlacementTestList {
    data: PlacementTest[] | null;
    pagination: Pagination;
}

export interface CreatePlacementTestRequest {
    sender_account_id: string;
    campaign_id?: string;
    sequence_id?: string;
    contact_id?: string;
    subject?: string;
    body_html?: string;
    body_plain?: string;
    tracking?: PlacementTracking;
    panel?: PlacementPanel;
}

export interface PlacementWorkspaceSeed {
    email_account_id: string;
    email: string;
    family: string;
    label: string;
    status: "active" | "inactive" | "revoked" | string;
    seed: boolean;
    /** Why the mailbox cannot be toggled right now. */
    blocker?: string;
}

export interface PlacementMonitor {
    id: string;
    campaign_id: string;
    created_by: string | null;
    enabled: boolean;
    interval_days: number;
    panel: PlacementPanel;
    /** Primary-inbox percentage (0-100) below which the monitor alerts. */
    alert_below: number;
    pause_on_alert: boolean;
    next_run_at: Date;
    last_run_at: Date | null;
    last_test_id: string | null;
    last_alert_at: Date | null;
    last_error?: string;
    created_at: Date;
    updated_at: Date;
}

export interface PlacementMonitorInput {
    enabled?: boolean;
    interval_days?: number;
    panel?: PlacementPanel;
    alert_below?: number;
    pause_on_alert?: boolean;
}

export const PLACEMENT_MONITOR_INTERVAL_MIN = 1;
export const PLACEMENT_MONITOR_INTERVAL_MAX = 30;

export const PANEL_LABEL: Record<PlacementPanel, string> = {
    instance: "Shared panel",
    workspace: "Your seed inboxes",
    cloud: "Warmbly Cloud panel",
};

export const PANEL_HINT: Record<PlacementPanel, string> = {
    instance: "Seed inboxes run for every workspace on this instance.",
    workspace: "Test mailboxes your workspace marked as seeds.",
    cloud: "Warmbly Cloud's seed inboxes, reached through your linked account.",
};
