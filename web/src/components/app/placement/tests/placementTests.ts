// Shared vocabulary for inbox placement tests: folder labels and tones, rate
// formatting, and the messages for every refusal the start endpoint returns.
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import type { DitherTone } from "@/components/ui/dither";
import type {
    PlacementCounts,
    PlacementFolder,
    PlacementOverview,
    PlacementPanel,
    PlacementTest,
    PlacementTestStatus,
} from "@/lib/api/models/app/placement/Placement";

export interface FolderStyle {
    label: string;
    tone: DitherTone;
    dot: string;
    text: string;
    chip: string;
}

export const FOLDER: Record<PlacementFolder, FolderStyle> = {
    inbox: { label: "Inbox", tone: "emerald", dot: "bg-emerald-500", text: "text-emerald-600", chip: "bg-emerald-50 text-emerald-700 border-emerald-200" },
    promotions: { label: "Promotions", tone: "violet", dot: "bg-violet-500", text: "text-violet-600", chip: "bg-violet-50 text-violet-700 border-violet-200" },
    other: { label: "Other tab", tone: "sky", dot: "bg-sky-500", text: "text-sky-600", chip: "bg-sky-50 text-sky-700 border-sky-200" },
    spam: { label: "Spam", tone: "rose", dot: "bg-rose-500", text: "text-rose-600", chip: "bg-rose-50 text-rose-700 border-rose-200" },
    missing: { label: "Never arrived", tone: "slate", dot: "bg-slate-500", text: "text-slate-600", chip: "bg-slate-100 text-slate-700 border-slate-200" },
    pending: { label: "Waiting", tone: "slate", dot: "bg-slate-300", text: "text-slate-400", chip: "bg-white text-slate-500 border-slate-200" },
    failed: { label: "Not sent", tone: "amber", dot: "bg-amber-500", text: "text-amber-600", chip: "bg-amber-50 text-amber-700 border-amber-200" },
    cancelled: { label: "Cancelled", tone: "slate", dot: "bg-slate-300", text: "text-slate-400", chip: "bg-slate-50 text-slate-500 border-slate-200" },
};

// The folders a copy that left can land in, in the order the bars stack.
export const LANDED_FOLDERS = ["inbox", "promotions", "other", "spam", "missing"] as const;

export const STATUS: Record<PlacementTestStatus, { label: string; chip: string; dot: string }> = {
    running: { label: "Running", chip: "bg-sky-50 text-sky-700 border-sky-200", dot: "bg-sky-500 animate-pulse" },
    completed: { label: "Completed", chip: "bg-emerald-50 text-emerald-700 border-emerald-200", dot: "bg-emerald-500" },
    cancelled: { label: "Cancelled", chip: "bg-slate-50 text-slate-600 border-slate-200", dot: "bg-slate-400" },
    failed: { label: "Failed", chip: "bg-rose-50 text-rose-700 border-rose-200", dot: "bg-rose-500" },
};

export const ORIGIN_LABEL: Record<string, string> = {
    manual: "Manual",
    monitor: "Monitor",
    admin: "Operator",
    remote: "Linked instance",
};

/** A 0..1 fraction as a whole percentage, or a dash while there is none. */
export function fmtRate(r: number | null | undefined): string {
    if (r == null) return "—";
    return `${Math.round(r * 100)}%`;
}

export function rateTone(r: number | null | undefined): string {
    if (r == null) return "text-slate-400";
    if (r >= 0.8) return "text-emerald-600";
    if (r >= 0.6) return "text-amber-600";
    return "text-rose-600";
}

/** Copies that have a verdict, sent or not. */
export function resolvedCount(c: PlacementCounts): number {
    return Math.max(0, c.total - c.pending);
}

export function isTracked(t: Pick<PlacementTest, "open_tracking" | "link_tracking">): boolean {
    return t.open_tracking || t.link_tracking;
}

export function fmtDate(d: Date | string | null | undefined): string {
    if (!d) return "—";
    const date = d instanceof Date ? d : new Date(d);
    if (Number.isNaN(date.getTime())) return "—";
    return date.toLocaleString(undefined, { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" });
}

export function fmtDay(d: Date | string | null | undefined): string {
    if (!d) return "—";
    const date = d instanceof Date ? d : new Date(d);
    if (Number.isNaN(date.getTime())) return "—";
    return date.toLocaleDateString(undefined, { month: "short", day: "numeric" });
}

export function usageLabel(o: PlacementOverview | undefined): string {
    if (!o) return "";
    const { used, limit } = o.usage;
    if (limit == null) return "Unlimited";
    return `${used} of ${limit} test${limit === 1 ? "" : "s"} this month`;
}

// Which part of the new-test form a refusal is about, so it shows beside it.
export type PlacementErrorField = "sender" | "source" | "tracking" | "panel" | "general";

export function placementErrorMessage(
    err: AppError,
    ctx: { resetsOn?: Date | null; panel?: PlacementPanel } = {},
): { field: PlacementErrorField; message: string } {
    switch (err?.code) {
        case "placement_not_entitled":
            return { field: "general", message: "Placement tests need an active trial or subscription." };
        case "placement_quota_exceeded":
            return {
                field: "panel",
                message: `This month's placement tests are used up${ctx.resetsOn ? ` until ${fmtDay(ctx.resetsOn)}` : ""}. Tests on your own seed inboxes are never counted.`,
            };
        case "placement_too_many_running":
            return { field: "general", message: "Three tests are already running in this workspace. Wait for one to finish, or cancel one." };
        case "placement_sender_busy":
            return { field: "sender", message: "This mailbox is still sending another test. Pick another mailbox or wait until that one has sent every copy." };
        case "placement_sender_unavailable":
            return { field: "sender", message: "This mailbox cannot send a test: it is not connected, or it is a seed inbox itself." };
        case "placement_daily_budget":
            return { field: "sender", message: "This mailbox does not have enough of today's sending limit left for every copy. Pick another mailbox or try again tomorrow." };
        case "placement_no_seeds":
            return {
                field: "panel",
                message:
                    ctx.panel === "workspace"
                        ? "None of your seed inboxes can take this test. Add a seed inbox on a different domain than the sender."
                        : "This panel has no seed inbox this mailbox can reach. Seeds on the sender's own domain are always skipped.",
            };
        case "placement_panel_unavailable":
            return { field: "panel", message: "The Warmbly Cloud panel needs this instance linked to Warmbly Cloud." };
        case "placement_invalid_tracking":
            return { field: "tracking", message: "This campaign sends plain text, which carries no tracking. Pick Off or As the campaign." };
        default:
            return { field: "general", message: buildError(err) };
    }
}
