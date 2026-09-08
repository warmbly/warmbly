// Long free text in a table cell (an error message, a failure reason):
// truncated with the full text as a tooltip, and a toggle to show it all.

import { useState } from "react";
import { cn } from "@/lib/utils";

export function ExpandableText({
    text,
    max = 90,
    className,
    mono,
}: {
    text: string;
    max?: number;
    className?: string;
    mono?: boolean;
}) {
    const [open, setOpen] = useState(false);
    if (!text) return <span className="text-xs text-muted-foreground">—</span>;
    const long = text.length > max;
    const shown = open || !long ? text : `${text.slice(0, max).trimEnd()}…`;
    return (
        <span className={cn("text-xs", mono && "font-mono text-[11px]", className)}>
            <span className={open ? "whitespace-pre-wrap break-words" : "break-words"} title={long && !open ? text : undefined}>
                {shown}
            </span>
            {long && (
                <button
                    type="button"
                    onClick={(e) => {
                        e.stopPropagation();
                        setOpen((v) => !v);
                    }}
                    className="ml-1 text-[11px] text-[var(--admin-accent-strong)] hover:underline"
                >
                    {open ? "less" : "more"}
                </button>
            )}
        </span>
    );
}
