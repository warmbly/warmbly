import type { CampaignReviewSummary, ReviewStatus } from "@/lib/api/models/app/campaigns/Review";

export const STATUS_PILL: Record<ReviewStatus, { label: string; className: string; dot: string }> = {
    waiting: { label: "Waiting", className: "bg-amber-50 text-amber-700 border-amber-200", dot: "bg-amber-500" },
    ready: { label: "Ready", className: "bg-sky-50 text-sky-700 border-sky-200", dot: "bg-sky-500" },
    held: { label: "Paused", className: "bg-violet-50 text-violet-700 border-violet-200", dot: "bg-violet-500" },
    started: { label: "Started", className: "bg-slate-100 text-slate-600 border-slate-200", dot: "bg-slate-300" },
};

// How many leads a list filter holds.
export const filterCount = (counts: CampaignReviewSummary["counts"], f: "all" | ReviewStatus) => (f === "all" ? counts.total : counts[f]);
