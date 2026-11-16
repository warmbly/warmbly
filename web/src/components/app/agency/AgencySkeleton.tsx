// Outlines of the agency lists while their first page loads, shaped like the rows they become.

import type { CSSProperties } from "react";
import { cn } from "@/lib/utils";

// Fixed widths, so the outline never reshuffles between renders.
const WIDTHS = [62, 78, 54, 70, 46, 84, 58, 66, 50, 74];

const rise = (i: number): CSSProperties => ({ animationDelay: `${i * 35}ms` });
const RISE = "animate-in fade-in slide-in-from-bottom-1 duration-500 fill-mode-both motion-reduce:animate-none";

export function SkeletonBar({ className, style }: { className?: string; style?: CSSProperties }) {
    return <span aria-hidden className={cn("skeleton-sync block rounded", className)} style={style} />;
}

/** A list's rows in outline: an avatar, a title, a second line when `lines` is 2, and a trailing figure. */
export function AgencySkeletonRows({ rows = 6, lines = 1, avatar = true }: { rows?: number; lines?: 1 | 2; avatar?: boolean }) {
    return (
        <div role="status" aria-label="Loading">
            {Array.from({ length: rows }, (_, i) => (
                <div key={i} className={cn("px-5 flex items-center gap-3 border-b border-slate-200/60", lines === 2 ? "h-14" : "h-11", RISE)} style={rise(i)}>
                    {avatar && <SkeletonBar className="size-6 rounded-md shrink-0" />}
                    <div className="min-w-0 flex-1 space-y-2">
                        <SkeletonBar className="h-3" style={{ width: `${WIDTHS[i % WIDTHS.length] * 2.2}px`, maxWidth: "100%" }} />
                        {lines === 2 && <SkeletonBar className="h-2.5" style={{ width: `${WIDTHS[(i + 3) % WIDTHS.length] * 3}px`, maxWidth: "85%" }} />}
                    </div>
                    <SkeletonBar className="hidden sm:block h-2.5 w-14 shrink-0" />
                </div>
            ))}
        </div>
    );
}
