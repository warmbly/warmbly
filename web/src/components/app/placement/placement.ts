// Shared vocabulary for warmup placement views: labels, band colours, the
// UTC date window and the per-provider filter.
import type {
    PlacementBand,
    PlacementCounts,
    PlacementDay,
    PlacementGroup,
    PlacementRate,
} from "@/lib/api/models/app/analytics/WarmupPlacement";
import type { DitherTone } from "@/components/ui/dither";

export type GroupFilter = "all" | PlacementGroup;

export const GROUP_ORDER: PlacementGroup[] = ["google", "microsoft", "yahoo", "other"];

export const GROUP_LABEL: Record<PlacementGroup, string> = {
    google: "Google",
    microsoft: "Microsoft",
    yahoo: "Yahoo & AOL",
    other: "Other providers",
};

// ProviderLogo ids; "other" renders the generic server mark.
export const GROUP_LOGO: Record<PlacementGroup, string> = {
    google: "google",
    microsoft: "microsoft",
    yahoo: "yahoo",
    other: "smtp_imap",
};

export const LANDED = [
    { key: "inbox", label: "Inbox", tone: "emerald", dot: "bg-emerald-500" },
    { key: "tabs", label: "Other tabs", tone: "violet", dot: "bg-violet-500" },
    { key: "spam", label: "Spam", tone: "rose", dot: "bg-rose-500" },
] as const satisfies readonly { key: keyof PlacementCounts; label: string; tone: DitherTone; dot: string }[];

export interface BandStyle {
    label: string;
    text: string;
    dot: string;
    chip: string;
    tone: DitherTone;
}

export const BAND: Record<PlacementBand, BandStyle> = {
    good: { label: "Healthy", text: "text-emerald-600", dot: "bg-emerald-500", chip: "bg-emerald-50 text-emerald-700 border-emerald-200", tone: "emerald" },
    fair: { label: "Watch", text: "text-amber-600", dot: "bg-amber-500", chip: "bg-amber-50 text-amber-700 border-amber-200", tone: "amber" },
    poor: { label: "Landing in spam", text: "text-rose-600", dot: "bg-rose-500", chip: "bg-rose-50 text-rose-700 border-rose-200", tone: "rose" },
    collecting: { label: "Collecting data", text: "text-slate-500", dot: "bg-slate-300", chip: "bg-slate-50 text-slate-600 border-slate-200", tone: "slate" },
    none: { label: "No deliveries yet", text: "text-slate-400", dot: "bg-slate-200", chip: "bg-slate-50 text-slate-500 border-slate-200", tone: "slate" },
};

// Same thresholds as the backend bands, for per-day and per-provider rates.
export function bandForRate(rate: number | null | undefined): PlacementBand {
    if (rate == null) return "none";
    if (rate >= 90) return "good";
    if (rate >= 80) return "fair";
    return "poor";
}

export function fmtPct(v: number | null | undefined, digits = 1): string {
    if (v == null) return "—";
    return `${Number.isInteger(v) ? v : v.toFixed(digits)}%`;
}

export function fmtNum(v: number | undefined): string {
    return (v ?? 0).toLocaleString();
}

export function shortDate(iso: string): string {
    const d = new Date(`${iso}T00:00:00Z`);
    return d.toLocaleDateString("en-US", { month: "short", day: "numeric", timeZone: "UTC" });
}

/** The last `days` UTC days ending today, as YYYY-MM-DD. */
export function utcWindow(days: number): { from: string; to: string } {
    const end = new Date();
    const start = new Date(end.getTime() - (days - 1) * 86_400_000);
    return { from: start.toISOString().slice(0, 10), to: end.toISOString().slice(0, 10) };
}

export const RANGES = [
    { key: "7d", days: 7 },
    { key: "14d", days: 14 },
    { key: "30d", days: 30 },
    { key: "90d", days: 90 },
] as const;
export type RangeKey = (typeof RANGES)[number]["key"];

export interface DayView {
    date: string;
    inbox: number;
    tabs: number;
    spam: number;
    rescued: number;
    delivered: number;
    /** Only meaningful unfiltered; a provider filter has no send-side split. */
    sent: number;
    unconfirmed: number;
    rolling: number | null;
}

// A day narrowed to one recipient group. Sends cannot be split by the
// recipient, so a filtered view carries none.
export function viewDays(days: PlacementDay[], group: GroupFilter, minSample: number, windowDays: number): DayView[] {
    const base = days.map((d) => {
        if (group === "all") {
            return {
                date: d.date,
                inbox: d.inbox,
                tabs: d.tabs,
                spam: d.spam,
                rescued: d.rescued,
                delivered: d.delivered,
                sent: d.sent,
                unconfirmed: d.unconfirmed,
                rolling: d.rolling_inbox_rate,
            };
        }
        const g = d.groups.find((x) => x.group === group);
        const inbox = g?.inbox ?? 0;
        const tabs = g?.tabs ?? 0;
        const spam = g?.spam ?? 0;
        return { date: d.date, inbox, tabs, spam, rescued: g?.rescued ?? 0, delivered: inbox + tabs + spam, sent: 0, unconfirmed: 0, rolling: null };
    });
    if (group === "all") return base;
    // Trailing rate inside the range, with the same floor the server applies.
    return base.map((d, i) => {
        let ok = 0;
        let all = 0;
        for (let k = Math.max(0, i - windowDays + 1); k <= i; k++) {
            ok += base[k].inbox + base[k].tabs;
            all += base[k].delivered;
        }
        return { ...d, rolling: all >= minSample ? Math.round((ok / all) * 10000) / 100 : null };
    });
}

export function totals(days: DayView[]) {
    const t = days.reduce(
        (acc, d) => {
            acc.inbox += d.inbox;
            acc.tabs += d.tabs;
            acc.spam += d.spam;
            acc.rescued += d.rescued;
            acc.sent += d.sent;
            acc.unconfirmed += d.unconfirmed;
            return acc;
        },
        { inbox: 0, tabs: 0, spam: 0, rescued: 0, sent: 0, unconfirmed: 0 },
    );
    const delivered = t.inbox + t.tabs + t.spam;
    return {
        ...t,
        delivered,
        inboxRate: delivered > 0 ? Math.round(((t.inbox + t.tabs) / delivered) * 10000) / 100 : null,
        spamRate: delivered > 0 ? Math.round((t.spam / delivered) * 10000) / 100 : null,
    };
}

/** One line explaining a headline rate, for tooltips and captions. */
export function rateSentence(rate: PlacementRate): string {
    const ok = rate.inbox + rate.tabs;
    if (rate.delivered === 0) return `No warmup deliveries in the last ${rate.window_days} days.`;
    if (rate.inbox_rate == null) {
        return `${rate.delivered} of the ${rate.min_sample} deliveries needed before a rate is shown (last ${rate.window_days} days).`;
    }
    return `${fmtNum(ok)} of ${fmtNum(rate.delivered)} warmup emails reached the inbox over the last ${rate.window_days} days.`;
}
