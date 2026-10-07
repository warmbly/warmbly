// Inbox placement tests (mirrors internal/models/placement.go and
// internal/app/placement/view.go). Timestamps arrive as ISO strings and are
// revived into Date objects by Request.
import type Pagination from "../Pagination";
import type { TemplateScoreIssue } from "../campaigns/TemplateScore";

export type PlacementPanel = "instance" | "workspace" | "cloud";
export type PlacementTracking = "campaign" | "on" | "off" | "compare";
export type PlacementPace = "spaced" | "quick";
export type PlacementTestStatus = "running" | "completed" | "cancelled" | "failed";
export type PlacementOrigin = "manual" | "monitor" | "admin" | "remote" | "batch";
export type PlacementFolder = "pending" | "inbox" | "promotions" | "other" | "spam" | "missing" | "failed" | "cancelled" | "unknown" | "archive" | "custom";

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
    /** Free tests used this month; null limit = unmetered. */
    limit: number | null;
    /** Price of a test past the free ones, 0 when tests cannot be paid for. */
    credits_per_test: number;
    /** Spendable credits, null when credits are off. */
    credit_balance: number | null;
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
	unknown?: number;
	archive?: number;
	custom?: number;
	observed_receipts?: number;
	classified_receipts?: number;
	unresolved?: number;
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
    /** The batch that started this test, if any. */
    batch_id: string | null;
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
    pace: PlacementPace;
    /** Credits this test cost past the month's free tests; 0 when it was free. */
    credits_charged: number;
    /** Credits a paid test that delivered nothing got back. */
    credits_refunded: number;
    /** When a paid test's charge was settled (refunded or kept), null until it finishes. */
    credits_settled_at: Date | null;
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
    first_folder?: string;
    observed_at?: Date;
    late_observation?: boolean;
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
    /** Only on the workspace panel: send to these seed inboxes and no others. */
    seed_ids?: string[];
    /** Only seeds at these provider families. */
    families?: string[];
    pace?: PlacementPace;
    /** The most credits the caller agreed to pay for a test past the month's free tests. */
    max_credits?: number;
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

// ---- Batches: one placement test run from many senders (internal/app/placement/batch.go).

export type PlacementBatchStatus = "queued" | "running" | "completed" | "completed_with_warnings" | "cancelled" | "failed";
export type PlacementBatchSenderStatus = "queued" | "deferred" | "running" | "completed" | "skipped" | "failed" | "cancelled";
export type PlacementSenderScopeType = "campaign" | "workspace";
export type PlacementSampleMode = "all" | "random" | "percent" | "per_domain" | "per_provider";
export type PlacementUnavailable = "skip" | "defer";

export interface PlacementSenderScope {
    type: PlacementSenderScopeType;
    campaign_id?: string;
    /** Mailbox host families (google_workspace, microsoft365, ...). */
    providers?: string[];
    domains?: string[];
    tag_ids?: string[];
    include_inactive?: boolean;
    /** Only mailboxes with no finished placement test in this many days. */
    untested_days?: number;
}

export interface PlacementSample {
    mode: PlacementSampleMode;
    count?: number;
    percent?: number;
    /** Spread a random or percent sample across providers or domains. */
    stratify?: "" | "provider" | "domain";
}

export interface PlacementBatchSelection {
    /** How many ids were named, when chosen by hand. */
    sender_account_ids?: number;
    sender_scope?: PlacementSenderScope;
    sample: PlacementSample;
    /** Senders the scope resolved to before sampling. */
    matched: number;
}

export interface PlacementBatchProgress {
    total: number;
    queued: number;
    deferred: number;
    running: number;
    completed: number;
    skipped: number;
    failed: number;
    cancelled: number;
}

export interface PlacementBatch {
    id: string;
    created_by: string | null;
    campaign_id: string | null;
    sequence_id: string | null;
    contact_id: string | null;
    subject: string;
    tracking: PlacementTracking;
    panel: PlacementPanel;
    pace: PlacementPace;
    families: string[];
    seed_ids: string[];
    on_unavailable: PlacementUnavailable;
    selection: PlacementBatchSelection;
    sender_count: number;
    max_credits: number;
    credits_spent: number;
    status: PlacementBatchStatus;
    error?: string;
    /** Deferred senders are retried until then, then skipped. */
    retry_until: Date;
    created_at: Date;
    started_at: Date | null;
    finished_at: Date | null;
    progress: PlacementBatchProgress;
    /** Headline copy: the tracked half of a tracking comparison. */
    summary: PlacementCounts;
}

export interface PlacementBatchGroup {
    key: string;
    label: string;
    senders: number;
    /** Senders in the group that finished a test. */
    tested: number;
    counts: PlacementCounts;
}

export interface PlacementBatchMatrixRow {
    domain: string;
    recipients: PlacementFamilyCounts[];
}

export interface PlacementBatchDetail extends PlacementBatch {
    /** The untracked half of a tracking comparison. */
    untracked?: PlacementCounts;
    /** Worst inbox rate first. */
    domains: PlacementBatchGroup[];
    providers: PlacementBatchGroup[];
    recipients: PlacementFamilyCounts[];
    /** Sending domain by recipient provider, domains in the order above. */
    matrix: PlacementBatchMatrixRow[];
    content: PlacementContentCheck;
}

export interface PlacementBatchSender {
    id: string;
    batch_id: string;
    email_account_id: string | null;
    sender_email: string;
    sender_domain: string;
    sender_family: string;
    sender_family_label: string;
    status: PlacementBatchSenderStatus;
    /** A refusal code (placement_daily_budget, ...) and its sentence. */
    reason?: string;
    detail?: string;
    attempts: number;
    next_attempt_at: Date;
    started_at: Date | null;
    finished_at: Date | null;
    summary: PlacementCounts;
    /** The sender's tests; two for a comparison, untracked first. */
    test_ids: string[];
}

export interface PlacementBatchList {
    data: PlacementBatch[] | null;
    pagination: Pagination;
}

export interface PlacementBatchSenderList {
    data: PlacementBatchSender[] | null;
    pagination: Pagination;
}

export type PlacementBatchSenderSort = "worst" | "best" | "email" | "status";

export interface PlacementBatchRequest {
    /** Exactly one of sender_account_ids and sender_scope. */
    sender_account_ids?: string[];
    sender_scope?: PlacementSenderScope;
    sample?: PlacementSample;
    campaign_id?: string;
    sequence_id?: string;
    contact_id?: string;
    subject?: string;
    body_html?: string;
    body_plain?: string;
    tracking?: PlacementTracking;
    panel?: PlacementPanel;
    pace?: PlacementPace;
    families?: string[];
    seed_ids?: string[];
    on_unavailable?: PlacementUnavailable;
    /** The most credits agreed across the whole batch for tests past the free ones. */
    max_credits?: number;
}

export interface PlacementBatchProviderCount {
    key: string;
    label: string;
    senders: number;
}

export interface PlacementBatchPreview {
    matched: number;
    selected: number;
    /** Selected mailboxes that are not connected right now. */
    inactive: number;
    domains: number;
    providers: PlacementBatchProviderCount[];
    /** 2 for a tracking comparison. */
    variants: number;
    tests: number;
    seeds_per_test: number;
    /** The most copies the batch sends; daily limits only make it fewer. */
    max_sends: number;
    metered: boolean;
    free_tests: number;
    paid_tests: number;
    /** The most the paid tests cost. */
    credits: number;
    usage: PlacementUsage;
    /** The instance's ceiling on senders in one batch. */
    senders_max: number;
    /** Senders of this workspace that send at once. */
    concurrency: number;
}

export interface PlacementCoverage {
    /** Connected sending mailboxes. */
    mailboxes: number;
    tested_7d: number;
    tested_30d: number;
    never_tested: number;
}
