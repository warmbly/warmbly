// Small presentational pieces shared by the placement test list, detail and
// campaign surfaces.
import React from "react";
import { EyeIcon, EyeOffIcon, InfoIcon, SplitIcon } from "lucide-react";
import { DitherStack } from "@/components/ui/dither";
import { cn } from "@/lib/utils";
import type {
    PlacementCounts,
    PlacementFolder,
    PlacementOverview,
    PlacementTest,
    PlacementTestStatus,
} from "@/lib/api/models/app/placement/Placement";
import { PANEL_HINT, PANEL_LABEL } from "@/lib/api/models/app/placement/Placement";
import { FOLDER, LANDED_FOLDERS, STATUS, isTracked } from "./placementTests";

export function StatusChip({ status, counts }: { status: PlacementTestStatus; counts?: PlacementCounts }) {
    const s = STATUS[status] ?? STATUS.failed;
    const progress = status === "running" && counts ? ` ${Math.max(0, counts.total - counts.pending)}/${counts.total}` : "";
    return (
        <span className={cn("inline-flex items-center gap-1.5 h-5 px-1.5 rounded-md border text-[10.5px] font-medium whitespace-nowrap", s.chip)}>
            <span className={cn("size-1.5 rounded-full", s.dot)} />
            {s.label}
            {progress && <span className="font-mono tabular-nums">{progress}</span>}
        </span>
    );
}

export function TrackingBadge({ test }: { test: Pick<PlacementTest, "open_tracking" | "link_tracking" | "compare_group_id"> }) {
    const tracked = isTracked(test);
    const Icon = tracked ? EyeIcon : EyeOffIcon;
    const parts = [test.open_tracking && "opens", test.link_tracking && "clicks"].filter(Boolean).join(" and ");
    return (
        <span className="inline-flex items-center gap-1">
            <span
                title={tracked ? `Tracks ${parts}` : "No open pixel or tracked links"}
                className={cn(
                    "inline-flex items-center gap-1 h-5 px-1.5 rounded-md text-[10.5px] font-medium whitespace-nowrap",
                    tracked ? "bg-amber-50 text-amber-700" : "bg-slate-100 text-slate-600",
                )}
            >
                <Icon className="w-3 h-3" />
                {tracked ? "Tracked" : "Untracked"}
            </span>
            {test.compare_group_id && (
                <span
                    title="Half of a with and without tracking comparison"
                    className="inline-flex items-center gap-1 h-5 px-1.5 rounded-md bg-sky-50 text-sky-700 text-[10.5px] font-medium"
                >
                    <SplitIcon className="w-3 h-3" />
                    <span className="hidden sm:inline">Compare</span>
                </span>
            )}
        </span>
    );
}

export function FolderChip({ folder }: { folder: PlacementFolder }) {
    const f = FOLDER[folder] ?? FOLDER.pending;
    return (
        <span className={cn("inline-flex items-center gap-1.5 h-5 px-1.5 rounded-md border text-[10.5px] font-medium whitespace-nowrap", f.chip)}>
            <span className={cn("size-1.5 rounded-full", f.dot)} />
            {f.label}
        </span>
    );
}

// Inbox, tabs, spam and never arrived as shares of every copy in the test, so
// the part still waiting shows as the empty track.
export function PlacementBar({ counts, height = 6, className }: { counts: PlacementCounts; height?: number; className?: string }) {
    const { total, inbox, promotions, other, spam, missing } = counts;
    const segments = React.useMemo(() => {
        const denom = Math.max(1, total);
        const n: Record<(typeof LANDED_FOLDERS)[number], number> = { inbox, promotions, other, spam, missing };
        return LANDED_FOLDERS.filter((f) => n[f] > 0).map((f) => ({ frac: n[f] / denom, tone: FOLDER[f].tone }));
    }, [total, inbox, promotions, other, spam, missing]);
    const title = LANDED_FOLDERS.map((f) => `${FOLDER[f].label} ${counts[f]}`).join(", ");
    return (
        <div title={title} className={className}>
            <DitherStack segments={segments} height={height} />
        </div>
    );
}

export function PlacementLegend({ className }: { className?: string }) {
    return (
        <div className={cn("flex flex-wrap items-center gap-x-3 gap-y-1", className)}>
            {LANDED_FOLDERS.map((f) => (
                <span key={f} className="inline-flex items-center gap-1.5 text-[11px] text-slate-500">
                    <span className={cn("size-1.5 rounded-full", FOLDER[f].dot)} />
                    {FOLDER[f].label}
                </span>
            ))}
        </div>
    );
}

// Seed results are a signal about the copy and the sender, not a forecast.
export function PlacementCaveat({ className }: { className?: string }) {
    return (
        <div className={cn("flex gap-2 rounded-md border border-slate-200 bg-slate-50/60 px-3 py-2.5", className)}>
            <InfoIcon className="w-3.5 h-3.5 text-slate-400 shrink-0 mt-0.5" />
            <p className="text-[11.5px] leading-relaxed text-slate-500">
                Seed results are a signal, not a forecast. One test is noise, so look for the same result across a few.
                Seed inboxes have no history with your sender and do not sit behind corporate filters, so real
                recipients can see something different.
            </p>
        </div>
    );
}

// One card per seed panel: whether it can run a test, how many seeds it has
// and which providers they cover.
export function PanelStrip({ overview }: { overview: PlacementOverview }) {
    return (
        <div
            className={cn(
                "grid grid-cols-1 border-b border-slate-200",
                overview.panels.length >= 3 ? "md:grid-cols-3" : overview.panels.length === 2 ? "md:grid-cols-2" : "",
            )}
        >
            {overview.panels.map((p, i) => {
                const families = p.families ?? [];
                return (
                    <div
                        key={p.panel}
                        className={cn(
                            "px-5 py-3 min-w-0",
                            i < overview.panels.length - 1 && "border-b md:border-b-0 md:border-r border-slate-200",
                        )}
                    >
                        <div className="flex items-center gap-2">
                            <span className={cn("size-1.5 rounded-full shrink-0", p.available ? "bg-emerald-500" : "bg-slate-300")} />
                            <span className="text-[12.5px] font-medium text-slate-900 truncate">{PANEL_LABEL[p.panel]}</span>
                            <span className="ml-auto font-mono text-[11px] text-slate-500 tabular-nums shrink-0">
                                {p.seeds} seed{p.seeds === 1 ? "" : "s"}
                            </span>
                        </div>
                        <p className="mt-0.5 text-[11px] text-slate-400 leading-relaxed">
                            {p.available ? PANEL_HINT[p.panel] : p.reason || "Not available on this workspace."}
                            {p.available && (p.metered ? " Counts toward your monthly tests." : " Not counted toward your monthly tests.")}
                        </p>
                        {families.length > 0 && (
                            <div className="mt-2 flex flex-wrap gap-1">
                                {families.map((f) => (
                                    <span
                                        key={f.family}
                                        className="inline-flex items-center gap-1 h-5 px-1.5 rounded bg-slate-100 text-[10.5px] text-slate-600"
                                    >
                                        {f.label}
                                        <span className="font-mono text-slate-400 tabular-nums">{f.seeds}</span>
                                    </span>
                                ))}
                            </div>
                        )}
                    </div>
                );
            })}
        </div>
    );
}
