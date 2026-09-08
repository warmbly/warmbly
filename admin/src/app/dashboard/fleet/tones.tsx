// Badge tones shared by the fleet tabs, derived from the legends so the
// pills here read the same as everywhere else.

import { Badge } from "@/components/ui/badge";
import { WORKER_HEALTH_LEGEND, WORKER_RISK_POOL_LEGEND } from "@/lib/legends";
import { cn } from "@/lib/utils";

const HEALTH_TONE = Object.fromEntries(WORKER_HEALTH_LEGEND.map((e) => [e.term, e.tone ?? ""]));
const RISK_TONE = Object.fromEntries(WORKER_RISK_POOL_LEGEND.map((e) => [e.term, e.tone ?? ""]));
const FALLBACK = "border-zinc-300 text-zinc-600";

export function HealthPill({ state }: { state: string }) {
    return (
        <Badge variant="outline" className={cn("text-[10px]", HEALTH_TONE[state] ?? FALLBACK)}>
            {state || "unknown"}
        </Badge>
    );
}

export function RiskPoolPill({ pool }: { pool: string }) {
    return (
        <Badge variant="outline" className={cn("text-[10px]", RISK_TONE[pool] ?? FALLBACK)}>
            {pool || "—"}
        </Badge>
    );
}

export function TierPill({ freeTier }: { freeTier: boolean }) {
    return (
        <Badge
            variant="outline"
            className={cn(
                "text-[10px]",
                freeTier ? "border-zinc-300 text-zinc-700" : "border-purple-300 bg-purple-50 text-purple-700",
            )}
        >
            {freeTier ? "free" : "premium"}
        </Badge>
    );
}

export function TypePill({ type }: { type: string }) {
    return (
        <Badge
            variant="outline"
            className={cn(
                "text-[10px]",
                type === "dedicated" ? "border-sky-300 bg-sky-50 text-sky-700" : "border-zinc-300 text-zinc-700",
            )}
        >
            {type}
        </Badge>
    );
}

export function LiveDot({ live, title }: { live: boolean; title?: string }) {
    return (
        <span className="inline-flex items-center gap-1.5 text-xs" title={title}>
            <span
                className={cn(
                    "inline-block size-2 rounded-full",
                    live ? "bg-emerald-500" : "bg-zinc-300",
                )}
            />
            <span className={live ? "text-emerald-700" : "text-muted-foreground"}>
                {live ? "live" : "offline"}
            </span>
        </span>
    );
}
