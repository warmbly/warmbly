// Small shared presentational components for the warmup-content section:
// the pool badge and the overview stat card. Kept JSX-only (no constant/fn
// exports) so React Fast Refresh stays happy — pure helpers live in
// `shared.ts`.

import type { ReactNode } from "react";
import { Badge } from "@/components/ui/badge";

export function PoolBadge({ pool }: { pool: string }) {
    return (
        <Badge
            variant="outline"
            className={`text-[10px] ${
                pool === "premium"
                    ? "border-purple-300 bg-purple-50 text-purple-700"
                    : "border-zinc-300 text-zinc-700"
            }`}
        >
            {pool}
        </Badge>
    );
}

export function StatCard({
    icon,
    title,
    value,
    hint,
    tone,
}: {
    icon: ReactNode;
    title: string;
    value: string;
    hint?: string;
    tone?: string;
}) {
    return (
        <div className="rounded-lg border border-border bg-card p-3">
            <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
                {icon}
                <span>{title}</span>
            </div>
            <div className={`mt-1 text-2xl font-semibold tabular-nums ${tone ?? ""}`}>
                {value}
            </div>
            {hint && (
                <div className="mt-0.5 truncate text-[10px] text-muted-foreground">
                    {hint}
                </div>
            )}
        </div>
    );
}
