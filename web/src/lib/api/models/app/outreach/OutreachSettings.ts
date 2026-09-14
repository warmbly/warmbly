// Workspace-wide outreach controls, mirroring models.AdvancedOutreachSettings.
// PATCH /outreach/settings replaces the whole object, so an edit must send the
// blocks it is not changing back unmodified.

export interface SendTimeOptimizationSettings {
    enabled: boolean;
    use_contact_timezone: boolean;
    default_contact_timezone: string;
    preferred_hours: number[];
    weekend_weight_multiplier: number;
}

export interface PreflightSettings {
    enabled: boolean;
    check_tracking_domain: boolean;
    check_unsubscribe_header: boolean;
    check_ab_variant_configured: boolean;
    check_daily_limit: boolean;
    check_schedule_window: boolean;
    check_content_score: boolean;
    min_content_score: number;
}

// The classifier buckets a reply can land in. "automated" is a machine reply
// that is not a vacation notice: an autoresponder, a ticket acknowledgement,
// a bounce or a delivery report.
export type ReplyIntent =
    | "positive"
    | "question"
    | "neutral"
    | "negative"
    | "out_of_office"
    | "automated";

export interface ReplyIntentSettings {
    enabled: boolean;
    positive_keywords: string[];
    negative_keywords: string[];
    out_of_office_keywords: string[];
    question_keywords: string[];
    auto_create_crm_task: boolean;
    // Which intents get a follow-up task. null/absent means the default set.
    crm_task_intents?: ReplyIntent[] | null;
    auto_pause_on_negative: boolean;
    auto_suppress_on_unsubscribe_keyword: boolean;
    // Park a contact's next step while their mailbox says they are away.
    hold_on_out_of_office: boolean;
    // The fallback hold, used when the auto-reply names no return date.
    out_of_office_hold_days: number;
}

// Matches models.DefaultCRMTaskIntents: every human intent, no automated one.
export const DEFAULT_CRM_TASK_INTENTS: ReplyIntent[] = [
    "positive",
    "question",
    "neutral",
    "negative",
];

// The two machine classes are one choice in the UI: a vacation notice and a
// helpdesk autoresponder are the same kind of noise on a task list.
export const AUTOMATED_INTENTS: ReplyIntent[] = ["out_of_office", "automated"];

export const REPLY_INTENT_CHOICES: { id: ReplyIntent; label: string; hint: string }[] = [
    { id: "positive", label: "Positive", hint: "Interested, wants a call" },
    { id: "question", label: "Question", hint: "Asked something" },
    { id: "neutral", label: "Neutral", hint: "A human reply we could not bucket" },
    { id: "negative", label: "Negative", hint: "Not interested" },
    { id: "out_of_office", label: "Automated", hint: "Out of office, autoresponders, bounces" },
];

/** The effective set: an unset list means the default, not "none". */
export function taskIntents(s?: ReplyIntentSettings): ReplyIntent[] {
    return s?.crm_task_intents ?? DEFAULT_CRM_TASK_INTENTS;
}

// The in-body opt-out every campaign email carries unless a campaign
// overrides it. "text" is a reply-to-opt-out sentence, "link" a sentence with
// a real unsubscribe link, "off" nothing.
export type UnsubscribeMode = "text" | "link" | "off";

export interface UnsubscribeSettings {
    mode: UnsubscribeMode;
    text: string;
    link_intro: string;
    link_text: string;
}

export const DEFAULT_UNSUBSCRIBE: UnsubscribeSettings = {
    mode: "text",
    text: "If this isn't relevant, just reply and let me know and I won't email you again.",
    link_intro: "Not the right person, or not interested?",
    link_text: "Unsubscribe",
};

export interface OutreachSettings {
    bounce_pipeline: Record<string, unknown>;
    task_reliability: Record<string, unknown>;
    ab_testing: Record<string, unknown>;
    reply_intent: ReplyIntentSettings;
    send_time_optimization: SendTimeOptimizationSettings;
    preflight: PreflightSettings;
    dashboard: Record<string, unknown>;
    unsubscribe: UnsubscribeSettings;
    custom?: Record<string, unknown>;
}

// Business hours, matching the backend default.
export const DEFAULT_PREFERRED_HOURS = [9, 10, 11, 14, 15, 16];

export function formatHour(h: number): string {
    if (h === 0) return "12am";
    if (h === 12) return "12pm";
    return h < 12 ? `${h}am` : `${h - 12}pm`;
}

// Collapses a sorted hour list into "9-11am, 2-4pm" for the summary line.
export function describeHours(hours: number[]): string {
    const sorted = [...new Set(hours)].filter((h) => h >= 0 && h <= 23).sort((a, b) => a - b);
    if (sorted.length === 0) return "no hours selected";
    const runs: number[][] = [];
    for (const h of sorted) {
        const last = runs[runs.length - 1];
        if (last && h === last[last.length - 1] + 1) last.push(h);
        else runs.push([h]);
    }
    return runs
        .map((run) => (run.length === 1 ? formatHour(run[0]) : `${formatHour(run[0])}-${formatHour(run[run.length - 1])}`))
        .join(", ");
}
