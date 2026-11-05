// Placement pieces shared by the campaign copy rows and the template list and drawer.
import { DitherStack, type DitherTone } from "@/components/ui/dither";
import { cn } from "@/lib/utils";
import {
    PROVIDER_GROUP_LABEL,
    type WarmupCopyGroupPlacement,
    type WarmupCopyPlacement,
    type WarmupCopyProviderGroup,
} from "@/lib/api/models/app/warmup/WarmupCopy";
import { fmtRate, rateTone } from "@/components/app/placement/tests/placementTests";

interface PlacementShares {
    // Inbox and tabs over reported receipts; null before any arrive.
    landed: number | null;
    spam: number | null;
}

function placementShares(p: WarmupCopyPlacement): PlacementShares {
    if (p.reported <= 0) return { landed: null, spam: null };
    return { landed: (p.inbox + p.tabs) / p.reported, spam: p.spam / p.reported };
}

const SEGMENTS: { key: "inbox" | "tabs" | "spam"; label: string; tone: DitherTone; dot: string }[] = [
    { key: "inbox", label: "Inbox", tone: "emerald", dot: "bg-emerald-500" },
    { key: "tabs", label: "Tabs", tone: "violet", dot: "bg-violet-500" },
    { key: "spam", label: "Spam", tone: "rose", dot: "bg-rose-500" },
];

// Shares of everything sent, so receipts not reported yet show as the empty track.
export function CopyPlacementBar({ placement, height = 6, className }: { placement: WarmupCopyPlacement; height?: number; className?: string }) {
    const denom = Math.max(1, placement.sent);
    const segments = SEGMENTS.filter((s) => placement[s.key] > 0).map((s) => ({ frac: placement[s.key] / denom, tone: s.tone }));
    const title = `${SEGMENTS.map((s) => `${s.label} ${placement[s.key]}`).join(", ")}, not reported ${Math.max(0, placement.sent - placement.reported)}`;
    return (
        <div title={title} className={className}>
            <DitherStack segments={segments} height={height} />
        </div>
    );
}

export function CopyPlacementLegend({ className }: { className?: string }) {
    return (
        <div className={cn("flex flex-wrap items-center gap-x-3 gap-y-1", className)}>
            {SEGMENTS.map((s) => (
                <span key={s.key} className="inline-flex items-center gap-1.5 text-[11px] text-slate-500">
                    <span className={cn("size-1.5 rounded-full", s.dot)} />
                    {s.label}
                </span>
            ))}
            <span className="inline-flex items-center gap-1.5 text-[11px] text-slate-500">
                <span className="size-1.5 rounded-full bg-slate-200" />
                Not reported yet
            </span>
        </div>
    );
}

// One line: the bar, then inbox share, spam share, rescued and sent.
export function CopyPlacementSummary({ placement, className }: { placement: WarmupCopyPlacement; className?: string }) {
    if (placement.sent === 0) {
        return <span className={cn("text-[11.5px] text-slate-400", className)}>Not sent yet in this window</span>;
    }
    const shares = placementShares(placement);
    return (
        <div className={cn("flex items-center gap-3 min-w-0", className)}>
            <CopyPlacementBar placement={placement} className="w-20 sm:w-28 shrink-0" />
            <span className={cn("font-mono text-[12px] tabular-nums shrink-0", rateTone(shares.landed))} title="Inbox and tabs, of the receipts reported so far">
                {fmtRate(shares.landed)}
            </span>
            <span
                className={cn("font-mono text-[11.5px] tabular-nums shrink-0", placement.spam > 0 ? "text-rose-600" : "text-slate-400")}
                title="Spam, of the receipts reported so far"
            >
                {fmtRate(shares.spam)} spam
            </span>
            {placement.rescued > 0 && (
                <span className="hidden md:inline text-[11px] text-slate-500 tabular-nums shrink-0">
                    {placement.rescued.toLocaleString()} rescued
                </span>
            )}
            <span className="hidden sm:inline text-[11px] text-slate-400 tabular-nums shrink-0">{placement.sent.toLocaleString()} sent</span>
        </div>
    );
}

const GROUP_ORDER: WarmupCopyProviderGroup[] = ["google", "microsoft", "yahoo", "other"];

function groupLabel(group: string): string {
    return PROVIDER_GROUP_LABEL[group as WarmupCopyProviderGroup] ?? group;
}

// Per provider group, with the hosts outside the big three last.
export function GroupBreakdown({ groups, className }: { groups: WarmupCopyGroupPlacement[] | null | undefined; className?: string }) {
    const rows = [...(groups ?? [])].sort(
        (a, b) => rank(a.group) - rank(b.group) || a.group.localeCompare(b.group),
    );
    if (rows.length === 0) {
        return <p className={cn("text-[11.5px] text-slate-400", className)}>Nothing sent in this window yet.</p>;
    }
    return (
        <div className={cn("overflow-x-auto", className)}>
            <table className="w-full text-left">
                <thead>
                    <tr className="h-7 text-[10px] uppercase tracking-[0.14em] text-slate-400">
                        <th className="pr-3 font-medium">Provider</th>
                        <th className="px-2 font-medium text-right">Sent</th>
                        <th className="px-2 font-medium text-right">Inbox</th>
                        <th className="px-2 font-medium text-right">Tabs</th>
                        <th className="px-2 font-medium text-right">Spam</th>
                        <th className="px-2 font-medium text-right hidden sm:table-cell">Rescued</th>
                        <th className="pl-2 font-medium w-[28%] hidden sm:table-cell">Where it landed</th>
                    </tr>
                </thead>
                <tbody className="divide-y divide-slate-200/60">
                    {rows.map((g) => (
                        <tr key={g.group} className="h-8 text-[12px] text-slate-700">
                            <td className="pr-3 whitespace-nowrap">
                                {groupLabel(g.group)}
                                {g.group === "other" && <span className="ml-1.5 text-[10.5px] text-slate-400">not in the headline</span>}
                            </td>
                            <td className="px-2 text-right font-mono tabular-nums">{g.sent.toLocaleString()}</td>
                            <td className="px-2 text-right font-mono tabular-nums text-emerald-700">{g.inbox.toLocaleString()}</td>
                            <td className="px-2 text-right font-mono tabular-nums text-violet-700">{g.tabs.toLocaleString()}</td>
                            <td className={cn("px-2 text-right font-mono tabular-nums", g.spam > 0 ? "text-rose-600" : "text-slate-400")}>
                                {g.spam.toLocaleString()}
                            </td>
                            <td className="px-2 text-right font-mono tabular-nums hidden sm:table-cell">{g.rescued.toLocaleString()}</td>
                            <td className="pl-2 hidden sm:table-cell">{g.sent > 0 && <CopyPlacementBar placement={g} height={5} />}</td>
                        </tr>
                    ))}
                </tbody>
            </table>
        </div>
    );
}

function rank(group: string): number {
    const i = GROUP_ORDER.indexOf(group as WarmupCopyProviderGroup);
    return i === -1 ? GROUP_ORDER.length : i;
}

// Headline numbers leave out hosts that run their own filters.
export const HEADLINE_NOTE =
    "Headline numbers count recipients at Google, Microsoft and Yahoo, which filter on sender reputation. Other providers appear only in the breakdown.";
