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

export interface OutreachSettings {
    bounce_pipeline: Record<string, unknown>;
    task_reliability: Record<string, unknown>;
    ab_testing: Record<string, unknown>;
    reply_intent: Record<string, unknown>;
    send_time_optimization: SendTimeOptimizationSettings;
    preflight: Record<string, unknown>;
    dashboard: Record<string, unknown>;
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
