// The Review tab's loading frames and the styles its email preview adds for AI copy.

import React, { type CSSProperties } from "react";
import { AnimatePresence, motion } from "framer-motion";
import { cn } from "@/lib/utils";

const RISE = "animate-in fade-in slide-in-from-bottom-1 duration-500 fill-mode-both motion-reduce:animate-none";
const rise = (i: number): CSSProperties => ({ animationDelay: `${60 + i * 35}ms` });

// Fixed widths so the outline reads as real names and never reshuffles.
const NAME_WIDTHS = [92, 118, 76, 104, 88, 126, 70, 98, 112, 84];

function Bar({ className, style }: { className?: string; style?: CSSProperties }) {
    return <div className={cn("skeleton-sync rounded", className)} style={style} />;
}

export function LeadRowsSkeleton({ rows = 8, offset = 0 }: { rows?: number; offset?: number }) {
    return (
        <div role="status" aria-label="Loading leads">
            {Array.from({ length: rows }, (_, i) => (
                <div key={i} className={cn("h-10 px-3 flex items-center gap-2.5", RISE)} style={rise(offset + i)}>
                    <Bar className="size-3.5 rounded-[4px]" />
                    <div className="flex-1 min-w-0 flex items-center gap-2">
                        <Bar className="h-3" style={{ width: NAME_WIDTHS[i % NAME_WIDTHS.length] }} />
                        <Bar className="h-2.5" style={{ width: NAME_WIDTHS[(i + 3) % NAME_WIDTHS.length] * 0.7 }} />
                    </div>
                    <Bar className="size-1.5 rounded-full" />
                </div>
            ))}
        </div>
    );
}

export function EmailPaneSkeleton() {
    return (
        <div className="h-full flex flex-col" role="status" aria-label="Loading emails">
            <div className="shrink-0 h-12 border-b border-slate-200">
                <div className={cn("h-full px-5 flex items-center gap-2.5", RISE)} style={rise(0)}>
                    <Bar className="h-3.5 w-32" />
                    <Bar className="h-2.5 w-48" />
                    <Bar className="ml-auto h-7 w-24 rounded-md" />
                </div>
            </div>
            <div className="shrink-0 h-9 border-b border-slate-200">
                <div className={cn("h-full px-5 flex items-center gap-4", RISE)} style={rise(1)}>
                    <Bar className="h-3 w-14" />
                    <Bar className="h-3 w-20" />
                </div>
            </div>
            <div className="flex-1 min-h-0 overflow-hidden">
                <div className="max-w-[820px] px-5 py-3">
                    <div className={cn("rounded-lg border border-slate-200 overflow-hidden", RISE)} style={rise(2)}>
                        <div className="px-5 py-3.5 border-b border-slate-100 space-y-2.5">
                            <Bar className="h-2.5 w-64" />
                            <Bar className="h-2.5 w-48" />
                            <Bar className="h-3 w-80 max-w-full" />
                        </div>
                        <div className="px-5 py-5 space-y-3">
                            {[38, 92, 84, 96, 60, 28].map((w, i) => (
                                <Bar key={i} className={cn("h-3", RISE)} style={{ width: `${w}%`, ...rise(3 + i) }} />
                            ))}
                        </div>
                    </div>
                </div>
            </div>
        </div>
    );
}

export function ReviewSkeleton() {
    return (
        <div className={cn("h-full overflow-hidden flex flex-col", RISE)} role="status" aria-label="Loading">
            <div className="shrink-0 px-3 py-2 flex items-center gap-2 border-b border-slate-200 bg-slate-50/50">
                <Bar className="h-7 w-28 rounded-md" />
                <Bar className="h-7 w-32 rounded-md" />
                <Bar className="ml-auto h-[18px] w-40 rounded-full" />
            </div>
            <div className="flex-1 min-h-0 flex">
                <div className="w-full md:w-[270px] lg:w-[290px] shrink-0 md:border-r border-slate-200">
                    <div className="px-3 py-2 flex items-center gap-2 border-b border-slate-200">
                        <Bar className="size-3.5 rounded-[4px]" />
                        <Bar className="h-7 flex-1 rounded-md" />
                        <Bar className="h-7 w-20 rounded-md" />
                    </div>
                    <LeadRowsSkeleton rows={12} offset={1} />
                </div>
                <div className="hidden md:block flex-1 min-w-0">
                    <EmailPaneSkeleton />
                </div>
            </div>
        </div>
    );
}

// The dot grid a running campaign shows, for work in progress on this tab.
export function RunningGrid({ className }: { className?: string }) {
    return <span className={cn("campaign-grid shrink-0", className)} aria-hidden />;
}

// The running grid with a label that steps through what is happening.
export function RunningLabel({ labels, className }: { labels: string[]; className?: string }) {
    const [stage, setStage] = React.useState(0);
    React.useEffect(() => {
        if (labels.length < 2) return;
        const t = setInterval(() => setStage((s) => Math.min(s + 1, labels.length - 1)), 1500);
        return () => clearInterval(t);
    }, [labels.length]);
    const label = labels[Math.min(stage, labels.length - 1)];
    return (
        <span role="status" className={cn("h-8 pl-3 pr-3.5 rounded-full border border-sky-200 bg-sky-50 inline-flex items-center gap-2 text-sky-600", className)}>
            <RunningGrid />
            <AnimatePresence mode="wait" initial={false}>
                <motion.span
                    key={label}
                    initial={{ opacity: 0, y: 3 }}
                    animate={{ opacity: 1, y: 0 }}
                    exit={{ opacity: 0, y: -3 }}
                    transition={{ duration: 0.15 }}
                    className="text-shimmer whitespace-nowrap text-[12px] font-medium text-sky-800"
                >
                    {label}
                </motion.span>
            </AnimatePresence>
        </span>
    );
}
