// The shared monthly send pool: used, projected to the period's end, and each client's share.

import type { AgencyUsage } from "@/lib/api/models/app/agency/Agency";
import { cn } from "@/lib/utils";
import { formatNumber } from "./styles";

function pct(n: number, total: number) {
    if (total <= 0) return 0;
    return Math.min(100, (n / total) * 100);
}

export function PoolMeter({ usage }: { usage: AgencyUsage }) {
    if (usage.unlimited) {
        return (
            <div className="px-5 py-5 border-b border-slate-200">
                <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Send pool</div>
                <div className="mt-1.5 text-[22px] font-light text-slate-900 tabular-nums">{formatNumber(usage.used)} sent</div>
                <p className="mt-1 text-[11.5px] text-slate-500">
                    No pool cap on this agency. Each mailbox still keeps its own daily limit, spacing and warmup ramp.
                </p>
            </div>
        );
    }
    const usedPct = pct(usage.used, usage.total);
    const projPct = pct(usage.projected, usage.total);
    const over = usage.projected > usage.total;
    const tone = usedPct >= 100 ? "bg-red-500" : usedPct >= 80 ? "bg-amber-500" : "bg-sky-600";
    return (
        <div className="px-5 py-5 border-b border-slate-200">
            <div className="flex items-baseline gap-2 flex-wrap">
                <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Send pool</div>
                <div className="ml-auto text-[11px] text-slate-400">
                    {usage.days_left} {usage.days_left === 1 ? "day" : "days"} left in this period
                </div>
            </div>
            <div className="mt-1.5 flex items-baseline gap-2 flex-wrap">
                <span className="text-[26px] font-light text-slate-900 tabular-nums leading-none">{formatNumber(usage.used)}</span>
                <span className="text-[12.5px] text-slate-500 tabular-nums">of {formatNumber(usage.total)} sends</span>
                <span className="ml-auto text-[12.5px] text-slate-600 tabular-nums">{formatNumber(usage.remaining)} left</span>
            </div>
            <div
                className="relative mt-3 h-2 rounded-full bg-slate-100 overflow-hidden"
                role="meter"
                aria-valuemin={0}
                aria-valuemax={usage.total}
                aria-valuenow={usage.used}
                aria-label="Sends used this period"
            >
                <div className="absolute inset-y-0 left-0 bg-slate-200" style={{ width: `${projPct}%` }} />
                <div className={cn("absolute inset-y-0 left-0 rounded-full", tone)} style={{ width: `${usedPct}%` }} />
            </div>
            <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-[11px] text-slate-500 tabular-nums">
                <span>Plan: {formatNumber(usage.pool_size)}</span>
                {usage.topups > 0 && <span>Top-ups: {formatNumber(usage.topups)}</span>}
                <span className={over ? "text-amber-700" : undefined}>
                    At this pace: {formatNumber(usage.projected)} by the period's end
                    {over ? ", more than the pool" : ""}
                </span>
            </div>
        </div>
    );
}

export function ClientBreakdown({ usage }: { usage: AgencyUsage }) {
    const max = Math.max(1, ...usage.clients.map((c) => c.used));
    const sorted = [...usage.clients].sort((a, b) => b.used - a.used);
    return (
        <div>
            {sorted.map((c) => {
                const cap = c.monthly_send_limit ?? null;
                return (
                    <div key={c.organization_id} className="min-h-11 px-5 py-2 flex items-center gap-3 border-b border-slate-200/60">
                        <span className="w-40 md:w-56 shrink-0 text-[12.5px] text-slate-800 truncate">{c.name}</span>
                        <div className="flex-1 min-w-0 h-1.5 rounded-full bg-slate-100 overflow-hidden">
                            <div className="h-full rounded-full bg-sky-500" style={{ width: `${pct(c.used, cap ?? max)}%` }} />
                        </div>
                        <span className="w-36 shrink-0 text-right text-[12px] text-slate-600 tabular-nums">
                            {formatNumber(c.used)}
                            {cap !== null && <span className="text-slate-400"> / {formatNumber(cap)}</span>}
                        </span>
                    </div>
                );
            })}
        </div>
    );
}
