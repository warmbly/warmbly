// Labels and summaries shared by the reports list, detail and dialogs.

import type ClientReport from "@/lib/api/models/app/reports/ClientReport";
import type { ClientReportRange, ClientReportSchedule, ClientReportSections } from "@/lib/api/models/app/reports/ClientReport";

export const RANGE_OPTIONS: { value: ClientReportRange; label: string }[] = [
    { value: "last_7_days", label: "Last 7 days" },
    { value: "last_30_days", label: "Last 30 days" },
    { value: "this_month", label: "This month" },
    { value: "last_month", label: "Last month" },
];

export function rangeLabel(range: ClientReportRange): string {
    return RANGE_OPTIONS.find((o) => o.value === range)?.label ?? range;
}

export const SECTION_OPTIONS: { key: keyof ClientReportSections; label: string; hint: string }[] = [
    { key: "overview", label: "Overview", hint: "Sent, opens, clicks, replies and bounces" },
    { key: "daily_chart", label: "Daily chart", hint: "Sends, opens and replies per day" },
    { key: "campaigns", label: "Campaigns", hint: "One row per campaign in scope" },
    { key: "replies", label: "Replies", hint: "Who replied, to which campaign, and when" },
    { key: "positive_replies", label: "Positive replies", hint: "Count interested replies separately" },
    { key: "mailboxes", label: "Mailboxes", hint: "Mailbox health and warmup activity" },
];

export const DEFAULT_SECTIONS: ClientReportSections = {
    overview: true,
    campaigns: true,
    replies: true,
    positive_replies: true,
    mailboxes: true,
    daily_chart: true,
};

export const WEEKDAYS = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];

export type LinkState = "shared" | "expired" | "off";

export function linkState(report: Pick<ClientReport, "link">): LinkState {
    if (!report.link) return "off";
    return report.link.expired ? "expired" : "shared";
}

export function ordinal(n: number): string {
    const s = ["th", "st", "nd", "rd"];
    const v = n % 100;
    return `${n}${s[(v - 20) % 10] ?? s[v] ?? s[0]}`;
}

export function hourLabel(hour: number): string {
    return `${String(hour).padStart(2, "0")}:00`;
}

/** "Weekly on Monday at 09:00", or the monthly equivalent. */
export function scheduleSummary(s: ClientReportSchedule): string {
    const when = s.cadence === "weekly" ? `Weekly on ${WEEKDAYS[s.weekday] ?? "Monday"}` : `Monthly on the ${ordinal(s.month_day)}`;
    return `${when} at ${hourLabel(s.hour)}`;
}

export function scopeSummary(report: Pick<ClientReport, "access">): string {
    const a = report.access;
    if (!a || a.scope !== "restricted") return "Whole workspace";
    const parts = [
        countLabel(a.campaign_folder_ids?.length ?? 0, "folder"),
        countLabel(a.campaign_ids?.length ?? 0, "campaign"),
        countLabel(a.email_account_ids?.length ?? 0, "mailbox", "mailboxes"),
    ].filter(Boolean);
    return parts.length ? parts.join(", ") : "Nothing selected";
}

function countLabel(n: number, one: string, many = `${one}s`): string {
    return n === 0 ? "" : `${n} ${n === 1 ? one : many}`;
}

export function formatDateTime(d: Date | string | null | undefined): string {
    if (!d) return "";
    const date = d instanceof Date ? d : new Date(d);
    return date.toLocaleString(undefined, { month: "short", day: "numeric", year: "numeric", hour: "2-digit", minute: "2-digit" });
}

export function formatDate(d: Date | string | null | undefined): string {
    if (!d) return "";
    const date = d instanceof Date ? d : new Date(d);
    return date.toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric" });
}

/** "May 1 to May 31, 2026" from YYYY-MM-DD bounds, read as calendar days. */
export function dayRange(from: string, to: string): string {
    const f = new Date(`${from}T00:00:00Z`);
    const t = new Date(`${to}T00:00:00Z`);
    const opts: Intl.DateTimeFormatOptions = { month: "short", day: "numeric", timeZone: "UTC" };
    return `${f.toLocaleDateString(undefined, opts)} to ${t.toLocaleDateString(undefined, { ...opts, year: "numeric" })}`;
}

/** The link's expiry in words, for the list and the share panel. */
export function linkExpiry(report: Pick<ClientReport, "link">): string {
    const l = report.link;
    if (!l) return "";
    if (l.expired) return "Expired";
    if (!l.expires_at) return "Never expires";
    return `Expires ${formatDate(l.expires_at)}`;
}
