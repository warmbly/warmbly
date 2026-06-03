// Shared constants + pure helpers for the warmup-content section. These were
// previously module-private to the single page; now that each tab is its own
// route they live here so every page renders the same tones + date formatting.
//
// Keep this file JSX-free — the badge/stat-card *components* live in
// `components.tsx` so React Fast Refresh stays happy.

const JOB_STATUS_TONE: Record<string, string> = {
    pending: "border-amber-300 bg-amber-50 text-amber-700",
    queued: "border-amber-300 bg-amber-50 text-amber-700",
    running: "border-amber-300 bg-amber-50 text-amber-700",
    completed: "border-emerald-300 bg-emerald-50 text-emerald-700",
    succeeded: "border-emerald-300 bg-emerald-50 text-emerald-700",
    failed: "border-red-300 bg-red-50 text-red-700",
    error: "border-red-300 bg-red-50 text-red-700",
    cancelled: "border-zinc-300 bg-zinc-50 text-zinc-600",
    canceled: "border-zinc-300 bg-zinc-50 text-zinc-600",
};

export function jobTone(status: string): string {
    return JOB_STATUS_TONE[status] ?? "border-zinc-300 bg-zinc-50 text-zinc-600";
}

// OpenAI Batch lifecycle tones. In-flight states read amber/sky, terminal-good
// reads emerald, terminal-bad reads red, and cancelled reads neutral.
const BATCH_STATUS_TONE: Record<string, string> = {
    validating: "border-amber-300 bg-amber-50 text-amber-700",
    in_progress: "border-sky-300 bg-sky-50 text-sky-700",
    finalizing: "border-sky-300 bg-sky-50 text-sky-700",
    cancelling: "border-amber-300 bg-amber-50 text-amber-700",
    completed: "border-emerald-300 bg-emerald-50 text-emerald-700",
    failed: "border-red-300 bg-red-50 text-red-700",
    expired: "border-red-300 bg-red-50 text-red-700",
    cancelled: "border-zinc-300 bg-zinc-50 text-zinc-600",
};

export function batchTone(status: string): string {
    return BATCH_STATUS_TONE[status] ?? "border-zinc-300 bg-zinc-50 text-zinc-600";
}

export const CONTENT_STATUS_TONE: Record<string, string> = {
    active: "border-emerald-300 bg-emerald-50 text-emerald-700",
    archived: "border-zinc-300 bg-zinc-50 text-zinc-600",
    draft: "border-amber-300 bg-amber-50 text-amber-700",
};

export function fmtDate(s: string | null | undefined): string {
    if (!s) return "—";
    return new Date(s).toLocaleString();
}
