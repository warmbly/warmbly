// Top metric strip for the unibox.
//
// Numbers come from /unibox/overview so the strip is server-truth,
// not a sample of whatever happens to be loaded in the list.
//
// On phones and tablets the desktop ScopeRail is hidden, so we
// render a "Scope" pill in the strip that opens a ScopeSheet. The
// pill always sits on the left edge so it stays reachable even when
// the rest of the strip scrolls horizontally.

import { LayoutGridIcon, XIcon } from "lucide-react";
import useUniboxOverview from "@/lib/api/hooks/app/unibox/useUniboxOverview";
import AnimatedNumber from "@/components/ui/AnimatedNumber";
import { cn } from "@/lib/utils";

interface UniboxHeaderProps {
    scopeLabel: string;
    onClearScope?: () => void;
    onOpenScopeSheet?: () => void;
}

export function UniboxHeader({
    scopeLabel,
    onClearScope,
    onOpenScopeSheet,
}: UniboxHeaderProps) {
    const overview = useUniboxOverview();
    const data = overview.data;

    return (
        <header className="h-10 px-3 sm:px-4 border-b border-slate-200 bg-white flex items-center gap-2 sm:gap-3 shrink-0 overflow-x-auto">
            {onOpenScopeSheet && (
                <button
                    type="button"
                    onClick={onOpenScopeSheet}
                    aria-label="Switch scope"
                    className="lg:hidden inline-flex items-center gap-1 h-6 px-1.5 rounded-md border border-slate-200 text-slate-600 hover:text-slate-900 hover:bg-slate-50 text-[11.5px] font-medium transition-colors shrink-0"
                >
                    <LayoutGridIcon className="w-3 h-3" />
                    Scope
                </button>
            )}

            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-semibold shrink-0 hidden sm:inline">
                Inbox
            </span>

            {scopeLabel !== "All" && (
                <button
                    type="button"
                    onClick={onClearScope}
                    className="inline-flex items-center gap-1 h-5 pl-1.5 pr-1 rounded bg-sky-50 text-sky-700 text-[11px] font-medium hover:bg-sky-100 transition-colors shrink-0"
                    aria-label="Clear scope"
                >
                    {scopeLabel}
                    <XIcon className="w-2.5 h-2.5" />
                </button>
            )}

            <div className="h-4 w-px bg-slate-200 shrink-0 hidden sm:block" />

            <div className="flex items-center gap-3.5 min-w-0">
                <Stat
                    label="unread"
                    value={data?.unread ?? 0}
                    tone={data && data.unread > 0 ? "accent" : "default"}
                    muted={!data || data.unread === 0}
                />
                <Stat label="awaiting" value={data?.awaiting_reply ?? 0} />
                <Stat label="today" value={data?.today ?? 0} />
                <Stat label="week" value={data?.week ?? 0} />
                <Stat label="snoozed" value={data?.snoozed ?? 0} muted />
                <Stat label="mailboxes" value={data?.mailboxes.length ?? 0} muted />
            </div>

            <div className="ml-auto inline-flex items-center gap-1.5 text-[11px] text-emerald-600 font-medium shrink-0">
                <span className="relative flex size-1.5">
                    <span className="absolute inline-flex h-full w-full rounded-full bg-emerald-500 opacity-60 animate-ping" />
                    <span className="relative inline-flex size-1.5 rounded-full bg-emerald-500" />
                </span>
                live
            </div>
        </header>
    );
}

function Stat({
    label,
    value,
    tone = "default",
    muted,
}: {
    label: string;
    value: number;
    tone?: "default" | "accent";
    muted?: boolean;
}) {
    return (
        <div className="inline-flex items-baseline gap-1 shrink-0">
            <span
                className={cn(
                    "font-mono tabular-nums text-[12.5px] font-semibold",
                    tone === "accent" ? "text-sky-600" : muted ? "text-slate-400" : "text-slate-900",
                )}
            >
                <AnimatedNumber value={value} />
            </span>
            <span className="text-[10.5px] text-slate-500">{label}</span>
        </div>
    );
}
