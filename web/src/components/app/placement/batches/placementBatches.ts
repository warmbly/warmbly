// Shared vocabulary for placement batches: status labels and tones, the
// summary lines, and the messages for every refusal the batch endpoints return.
import type { AppError } from "@/lib/api/client/normalizeError";
import type { DitherTone } from "@/components/ui/dither";
import type {
    PlacementBatch,
    PlacementBatchProgress,
    PlacementBatchSenderStatus,
    PlacementBatchStatus,
    PlacementPanel,
} from "@/lib/api/models/app/placement/Placement";
import { placementErrorMessage } from "../tests/placementTests";

// Mirrors config.PlacementBatchRetryDays.
export const BATCH_RETRY_DAYS = 7;
// Mirrors config.PlacementBatchOpenPerOrgMax.
export const BATCH_OPEN_MAX = 5;

export const BATCH_STATUS: Record<PlacementBatchStatus, { label: string; chip: string; dot: string }> = {
    queued: { label: "Queued", chip: "bg-white text-slate-600 border-slate-200", dot: "bg-slate-400" },
    running: { label: "Running", chip: "bg-sky-50 text-sky-700 border-sky-200", dot: "bg-sky-500 animate-pulse" },
    completed: { label: "Completed", chip: "bg-emerald-50 text-emerald-700 border-emerald-200", dot: "bg-emerald-500" },
    completed_with_warnings: {
        label: "Completed with warnings",
        chip: "bg-amber-50 text-amber-700 border-amber-200",
        dot: "bg-amber-500",
    },
    cancelled: { label: "Cancelled", chip: "bg-slate-50 text-slate-600 border-slate-200", dot: "bg-slate-400" },
    failed: { label: "Failed", chip: "bg-rose-50 text-rose-700 border-rose-200", dot: "bg-rose-500" },
};

export const SENDER_STATUS: Record<PlacementBatchSenderStatus, { label: string; chip: string; dot: string }> = {
    queued: { label: "Queued", chip: "bg-white text-slate-500 border-slate-200", dot: "bg-slate-300" },
    deferred: { label: "Deferred", chip: "bg-amber-50 text-amber-700 border-amber-200", dot: "bg-amber-400" },
    running: { label: "Sending", chip: "bg-sky-50 text-sky-700 border-sky-200", dot: "bg-sky-500 animate-pulse" },
    completed: { label: "Tested", chip: "bg-emerald-50 text-emerald-700 border-emerald-200", dot: "bg-emerald-500" },
    skipped: { label: "Skipped", chip: "bg-amber-50 text-amber-700 border-amber-200", dot: "bg-amber-500" },
    failed: { label: "Failed", chip: "bg-rose-50 text-rose-700 border-rose-200", dot: "bg-rose-500" },
    cancelled: { label: "Cancelled", chip: "bg-slate-50 text-slate-500 border-slate-200", dot: "bg-slate-300" },
};

// Short labels for why a sender was skipped or deferred; `detail` has the sentence.
export const SENDER_REASON: Record<string, string> = {
    placement_daily_budget: "Daily limit reached",
    placement_sender_busy: "Sending another test",
    placement_sender_unavailable: "Not connected",
    placement_invalid_seeds: "Seed choice no longer valid",
    placement_no_seeds: "No seed it can reach",
    placement_sender_deleted: "Mailbox deleted",
    placement_batch_retry_expired: "Retry window ended",
    placement_batch_start_failed: "Could not start",
    placement_batch_copy_unavailable: "Copy no longer available",
    placement_quota_exceeded: "Monthly tests used up",
    insufficient_credits: "Balance too low",
    usage_cap_exceeded: "Spend limit reached",
};

export function batchOpen(status: PlacementBatchStatus): boolean {
    return status === "queued" || status === "running";
}

// Progress parts in display order, with the tone each takes in the bar.
const PROGRESS_PARTS: { key: keyof Omit<PlacementBatchProgress, "total">; label: string; tone: DitherTone }[] = [
    { key: "completed", label: "tested", tone: "emerald" },
    { key: "running", label: "sending", tone: "sky" },
    { key: "queued", label: "queued", tone: "slate" },
    { key: "deferred", label: "deferred", tone: "amber" },
    { key: "skipped", label: "skipped", tone: "amber" },
    { key: "failed", label: "failed", tone: "rose" },
    { key: "cancelled", label: "cancelled", tone: "slate" },
];

/** "120 tested · 20 sending · 5 skipped", only the parts that are not zero. */
export function progressLine(p: PlacementBatchProgress): string {
    return PROGRESS_PARTS.filter((part) => p[part.key] > 0)
        .map((part) => `${p[part.key].toLocaleString()} ${part.label}`)
        .join(" · ");
}

/** Bar segments for senders that are done one way or another, or sending. */
export function progressSegments(p: PlacementBatchProgress): { frac: number; tone: DitherTone }[] {
    const denom = Math.max(1, p.total);
    return PROGRESS_PARTS.filter((part) => part.key !== "queued" && part.key !== "deferred" && p[part.key] > 0).map((part) => ({
        frac: p[part.key] / denom,
        tone: part.tone,
    }));
}

/** Senders that have finished one way or another. */
export function doneCount(p: PlacementBatchProgress): number {
    return p.completed + p.skipped + p.failed + p.cancelled;
}

/** "Campaign senders · 10% sample". */
export function scopeSummary(b: Pick<PlacementBatch, "selection">): string {
    const { selection } = b;
    const parts: string[] = [];
    const scope = selection.sender_scope;
    if (selection.sender_account_ids) {
        parts.push(`${selection.sender_account_ids.toLocaleString()} chosen mailbox${selection.sender_account_ids === 1 ? "" : "es"}`);
    } else if (scope?.type === "campaign") {
        parts.push("Campaign senders");
    } else if (scope) {
        parts.push("All mailboxes");
    }
    if (scope?.untested_days) parts.push(`untested for ${scope.untested_days} days`);
    const s = selection.sample;
    switch (s?.mode) {
        case "random":
            parts.push(`random ${(s.count ?? 0).toLocaleString()}`);
            break;
        case "percent":
            parts.push(`${s.percent ?? 0}% sample`);
            break;
        case "per_domain":
            parts.push(`${s.count ?? 0} per domain`);
            break;
        case "per_provider":
            parts.push(`${s.count ?? 0} per provider`);
            break;
    }
    return parts.join(" · ");
}

/** "about 2 hours", "about 25 minutes", "under 2 minutes". */
export function fmtDuration(seconds: number): string {
    if (seconds < 90) return "under 2 minutes";
    const minutes = Math.round(seconds / 60);
    if (minutes < 90) return `about ${minutes} minutes`;
    const hours = Math.round(minutes / 60);
    if (hours < 36) return `about ${hours} hours`;
    const days = Math.round(hours / 24);
    return `about ${days} days`;
}

// Which step of the new-batch dialog a refusal is about, so it shows there.
export type BatchErrorField = "senders" | "email" | "review" | "general";

export function batchErrorMessage(
    err: AppError,
    ctx: { resetsOn?: Date | null; panel?: PlacementPanel } = {},
): { field: BatchErrorField; message: string } {
    switch (err?.code) {
        case "placement_batch_empty":
            return { field: "senders", message: "No sending mailbox matches this selection. Widen the filters or choose other mailboxes." };
        case "placement_batch_too_large":
            return { field: "senders", message: err.message || "This batch has more senders than this instance allows. Narrow the selection or take a sample." };
        case "placement_too_many_batches":
            return {
                field: "general",
                message: `${BATCH_OPEN_MAX} batches are already running in this workspace. Wait for one to finish, or cancel one.`,
            };
        case "placement_quota_exceeded":
            return {
                field: "review",
                message: "This month's free placement tests do not cover this batch, and tests past them cannot be paid for. Take a smaller sample, or use your own seed inboxes, which are never counted.",
            };
        case "insufficient_credits":
            return { field: "review", message: "The workspace balance is too low for this batch. Add money under Settings > Billing, or take a smaller sample." };
        case "usage_cap_exceeded":
            return { field: "review", message: "This batch would go past the workspace's spend limit. An admin can raise it under Settings > Billing." };
        case "placement_no_seeds":
            return {
                field: "email",
                message:
                    ctx.panel === "workspace"
                        ? "None of your seed inboxes can take this batch. Add a seed inbox, or clear the seed choice."
                        : "This panel has no seed inbox these senders can reach.",
            };
    }
    const single = placementErrorMessage(err, { resetsOn: ctx.resetsOn, panel: ctx.panel });
    switch (single.field) {
        case "sender":
            return { field: "senders", message: single.message };
        case "source":
        case "tracking":
        case "panel":
            return { field: "email", message: single.message };
        default:
            return { field: "general", message: single.message };
    }
}
