// The shared AI balance: this period's allowance, what was bought, and each client's spend from it.

import type { AgencyAIBalance } from "@/lib/api/models/app/agency/Agency";
import { fmtUsd } from "@/lib/money";
import { cn } from "@/lib/utils";

function pct(n: number, total: number) {
    if (total <= 0) return 0;
    return Math.min(100, (n / total) * 100);
}

export function AIBalanceMeter({ balance }: { balance: AgencyAIBalance }) {
    const total = balance.allowance_usd + balance.purchased_usd;
    const leftPct = pct(balance.balance_usd, Math.max(total, balance.balance_usd));
    const tone = balance.balance_usd <= 0 ? "bg-red-500" : leftPct < 20 ? "bg-amber-500" : "bg-sky-600";
    return (
        <div className="px-5 py-5 border-b border-slate-200">
            <div className="flex items-baseline gap-2 flex-wrap">
                <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">AI balance</div>
                <div className="ml-auto text-[11px] text-slate-400">Shared by every client</div>
            </div>
            <div className="mt-1.5 flex items-baseline gap-2 flex-wrap">
                <span className="text-[26px] font-light text-slate-900 tabular-nums leading-none">{fmtUsd(balance.balance_usd)}</span>
                <span className="text-[12.5px] text-slate-500">left this period</span>
            </div>
            <div
                className="relative mt-3 h-2 rounded-full bg-slate-100 overflow-hidden"
                role="meter"
                aria-valuemin={0}
                aria-valuemax={Math.max(total, balance.balance_usd)}
                aria-valuenow={balance.balance_usd}
                aria-label="AI balance left this period"
            >
                <div className={cn("absolute inset-y-0 left-0 rounded-full", tone)} style={{ width: `${leftPct}%` }} />
            </div>
            <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-[11px] text-slate-500 tabular-nums">
                <span>
                    Plan: {fmtUsd(balance.allowance_left_usd)} of {fmtUsd(balance.allowance_usd)} left
                </span>
                {balance.purchased_usd > 0 && <span>Bought: {fmtUsd(balance.purchased_usd)}, never expires</span>}
                <span>Spent by clients: {fmtUsd(balance.spent_usd)}</span>
                {balance.moved_usd > 0 && <span>Moved to clients: {fmtUsd(balance.moved_usd)}</span>}
            </div>
        </div>
    );
}

export function AIClientBreakdown({ balance }: { balance: AgencyAIBalance }) {
    const max = Math.max(0.01, ...balance.clients.map((c) => c.spent_usd));
    const sorted = [...balance.clients].sort((a, b) => b.spent_usd - a.spent_usd);
    return (
        <div>
            {sorted.map((c) => {
                const cap = c.monthly_ai_limit_usd ?? null;
                return (
                    <div key={c.organization_id} className="min-h-11 px-5 py-2 flex items-center gap-3 border-b border-slate-200/60">
                        <span className="w-40 md:w-56 shrink-0 text-[12.5px] text-slate-800 truncate">{c.name}</span>
                        <div className="flex-1 min-w-0 h-1.5 rounded-full bg-slate-100 overflow-hidden">
                            <div className="h-full rounded-full bg-sky-500" style={{ width: `${pct(c.spent_usd, cap ?? max)}%` }} />
                        </div>
                        <span className="w-44 shrink-0 text-right text-[12px] text-slate-600 tabular-nums">
                            {fmtUsd(c.spent_usd)}
                            {cap !== null && <span className="text-slate-400"> / {fmtUsd(cap)}</span>}
                            {c.moved_usd > 0 && <span className="block text-[11px] text-slate-400">{fmtUsd(c.moved_usd)} moved</span>}
                        </span>
                    </div>
                );
            })}
        </div>
    );
}
