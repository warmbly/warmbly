// CreditsMeter — the AI credit gauge in the top header, beside the plan badge.
//
// The trigger blends with the header chrome (no pill background): a small ring
// gauge showing how much of a full monthly allowance is still spendable, plus
// the compact remaining count. It only takes on color when something needs
// attention: amber under the org's low-balance threshold, red at zero.
// Clicking opens a panel with the full picture: plan pool vs allowance with
// the next reset, purchased credits, today/week/month spend against any
// configured limits, and the auto top-up state. Rendered only for members who
// can see billing (the endpoints are manage_billing-gated) and refreshed by
// the realtime spine on purchases, resets, and the low-credit alert.

import React from "react";
import { Link } from "react-router-dom";
import { AnimatePresence, motion } from "framer-motion";
import { ArrowUpRightIcon } from "lucide-react";
import useClickOutside from "@/hooks/useClickOutside";
import AnimatedNumber from "@/components/ui/AnimatedNumber";
import useCredits from "@/lib/api/hooks/app/subscription/useCredits";
import { useCreditSettings } from "@/lib/api/hooks/app/subscription/useCreditSettings";
import useCreditUsage from "@/lib/api/hooks/app/subscription/useCreditUsage";
import type { CreditBalance, AISpendSettings } from "@/lib/api/models/app/subscription/Credits";
import { usePermission } from "@/hooks/usePermission";
import { cn } from "@/lib/utils";

export function CreditsMeter() {
    const canSee = usePermission("MANAGE_BILLING");
    const credits = useCredits();
    const settings = useCreditSettings();
    const [open, setOpen] = React.useState(false);
    const ref = React.useRef<HTMLDivElement>(null);
    const close = React.useCallback(() => setOpen(false), []);
    useClickOutside(ref, close);

    if (!canSee || credits.isPending || !credits.data) return null;

    const c = credits.data;
    const total = c.balance;
    const threshold = settings.data?.low_balance_threshold ?? 25;
    const empty = total <= 0;
    const low = !empty && total <= threshold;

    // Full ring = at least one full monthly allowance still spendable
    // (purchased credits can push past it; the ring just caps at full).
    const fraction =
        c.monthly_allowance > 0
            ? Math.min(1, total / c.monthly_allowance)
            : total > 0
              ? 1
              : 0;

    return (
        <div ref={ref} className="relative">
            <button
                type="button"
                onClick={() => setOpen((o) => !o)}
                aria-label={`${total.toLocaleString()} AI credits left`}
                title={open ? undefined : `${total.toLocaleString()} AI credits left`}
                className={cn(
                    "flex items-center gap-1.5 px-2 h-7 rounded-md text-[11.5px] font-medium tabular-nums transition-colors",
                    empty
                        ? "text-red-600 hover:bg-red-50"
                        : low
                          ? "text-amber-600 hover:bg-amber-50"
                          : "text-slate-500 hover:text-slate-900 hover:bg-slate-200/60",
                )}
            >
                <Ring fraction={fraction} />
                <AnimatedNumber value={total} format={(n) => formatCredits(Math.round(n))} />
            </button>

            <AnimatePresence>
                {open && (
                    <motion.div
                        initial={{ opacity: 0, y: -4 }}
                        animate={{ opacity: 1, y: 0 }}
                        exit={{ opacity: 0, y: -4 }}
                        transition={{ duration: 0.12 }}
                        className="absolute right-0 top-full mt-1.5 w-72 rounded-md border border-slate-200 bg-white shadow-[0_12px_32px_-8px_rgba(15,23,42,0.18)] z-50 overflow-hidden"
                    >
                        <MeterPanel credits={c} settings={settings.data} low={low} empty={empty} />
                        <Link
                            to="/app/settings/billing"
                            onClick={close}
                            className="flex items-center gap-1 px-3 h-9 border-t border-slate-200 text-[11.5px] font-medium text-slate-600 hover:text-slate-900 hover:bg-slate-50 transition-colors"
                        >
                            Usage &amp; spend controls
                            <ArrowUpRightIcon className="w-3 h-3" />
                        </Link>
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}

// The panel body lives in its own component so the usage query (spend per
// window) only fires once the meter is actually opened.
function MeterPanel({
    credits,
    settings,
    low,
    empty,
}: {
    credits: CreditBalance;
    settings?: AISpendSettings;
    low: boolean;
    empty: boolean;
}) {
    const usage = useCreditUsage();
    const planUsed = Math.max(0, credits.monthly_allowance - credits.monthly_balance);
    const planFraction =
        credits.monthly_allowance > 0 ? credits.monthly_balance / credits.monthly_allowance : 0;
    const reset = resetLabel(credits.next_reset_at);
    const pack = credits.packs.find((p) => p.key === settings?.auto_topup_pack);

    return (
        <div className="px-3 pt-3 pb-2.5 space-y-3.5">
            <div className="flex items-end justify-between">
                <div>
                    <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                        AI credits
                    </div>
                    <div className="mt-1 flex items-baseline gap-1.5">
                        <AnimatedNumber
                            value={credits.balance}
                            className={cn(
                                "text-[22px] leading-none font-semibold tracking-tight tabular-nums",
                                empty ? "text-red-600" : "text-slate-900",
                            )}
                        />
                        <span className="text-[11px] text-slate-400">left</span>
                    </div>
                </div>
                {(empty || low || (settings?.auto_topup_enabled && pack)) && (
                    <div
                        className={cn(
                            "flex items-center gap-1.5 text-[10.5px] font-medium pb-0.5",
                            empty ? "text-red-600" : low ? "text-amber-600" : "text-slate-500",
                        )}
                    >
                        <span
                            className={cn(
                                "size-1.5 rounded-full shrink-0",
                                empty ? "bg-red-500" : low ? "bg-amber-500" : "bg-emerald-500",
                            )}
                        />
                        {empty ? "Out of credits" : low ? "Running low" : "Auto top-up on"}
                    </div>
                )}
            </div>

            <div>
                <div className="flex items-baseline justify-between text-[11.5px]">
                    <span className="text-slate-600">Plan credits</span>
                    <span className="tabular-nums text-slate-900 font-medium">
                        {credits.monthly_balance.toLocaleString()}
                        <span className="text-slate-400 font-normal">
                            {" "}
                            / {credits.monthly_allowance.toLocaleString()}
                        </span>
                    </span>
                </div>
                <div className="mt-1.5 h-1 rounded-full bg-slate-100 overflow-hidden">
                    <div
                        className={cn(
                            "h-full rounded-full transition-[width] duration-300",
                            empty ? "bg-red-400" : low ? "bg-amber-400" : "bg-slate-700",
                        )}
                        style={{ width: `${Math.min(100, Math.max(0, planFraction * 100))}%` }}
                    />
                </div>
                <div className="mt-1 flex justify-between text-[10.5px] text-slate-400">
                    <span>{planUsed.toLocaleString()} used this cycle</span>
                    {reset && <span>{reset}</span>}
                </div>
            </div>

            <div className="flex items-baseline justify-between text-[11.5px]">
                <span className="text-slate-600">Extra credits</span>
                <span
                    className={cn(
                        "tabular-nums font-medium",
                        credits.purchased_balance > 0 ? "text-slate-900" : "text-slate-400",
                    )}
                >
                    {credits.purchased_balance.toLocaleString()}
                    <span className="text-slate-400 font-normal"> never expire</span>
                </span>
            </div>

            <div className="space-y-1.5">
                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                    Spent
                </span>
                {usage.isPending || !usage.data ? (
                    <div className="space-y-1.5 animate-pulse">
                        <div className="h-3.5 rounded bg-slate-100" />
                        <div className="h-3.5 rounded bg-slate-100" />
                        <div className="h-3.5 rounded bg-slate-100" />
                    </div>
                ) : (
                    <>
                        <SpendRow label="Today" spent={usage.data.spent_today} limit={usage.data.limit_daily} />
                        <SpendRow label="This week" spent={usage.data.spent_week} limit={usage.data.limit_weekly} />
                        <SpendRow label="This month" spent={usage.data.spent_month} limit={usage.data.limit_monthly} />
                    </>
                )}
            </div>

            {settings?.auto_topup_enabled && pack && (
                <div className="text-[10.5px] text-slate-400">
                    Buys {pack.credits.toLocaleString()} credits automatically when the balance drops below{" "}
                    {settings.auto_topup_threshold.toLocaleString()}.
                </div>
            )}
        </div>
    );
}

// One spend window: the amount, and when a hard limit is set, "of <limit>"
// plus a fill bar that warns as the limit approaches.
function SpendRow({ label, spent, limit }: { label: string; spent: number; limit: number | null }) {
    const fraction = limit ? Math.min(1, spent / limit) : null;
    return (
        <div>
            <div className="flex items-baseline justify-between text-[11.5px]">
                <span className="text-slate-600">{label}</span>
                <span className="tabular-nums text-slate-900 font-medium">
                    {spent.toLocaleString()}
                    {limit != null && (
                        <span className="text-slate-400 font-normal"> of {limit.toLocaleString()}</span>
                    )}
                </span>
            </div>
            {fraction != null && (
                <div className="mt-1 h-1 rounded-full bg-slate-100 overflow-hidden">
                    <div
                        className={cn(
                            "h-full rounded-full",
                            fraction >= 1 ? "bg-red-400" : fraction >= 0.8 ? "bg-amber-400" : "bg-slate-400",
                        )}
                        style={{ width: `${fraction * 100}%` }}
                    />
                </div>
            )}
        </div>
    );
}

// Small ring gauge drawn with currentColor so it follows the trigger's text
// color (neutral slate normally, amber/red when attention is needed).
function Ring({ fraction }: { fraction: number }) {
    const r = 5.5;
    const circumference = 2 * Math.PI * r;
    return (
        <svg viewBox="0 0 14 14" className="w-3.5 h-3.5 -rotate-90 shrink-0" aria-hidden>
            <circle cx="7" cy="7" r={r} fill="none" stroke="currentColor" strokeWidth="2" opacity="0.2" />
            <circle
                cx="7"
                cy="7"
                r={r}
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
                strokeLinecap="round"
                strokeDasharray={circumference}
                strokeDashoffset={circumference * (1 - Math.max(0, Math.min(1, fraction)))}
                className="transition-[stroke-dashoffset] duration-300"
            />
        </svg>
    );
}

// formatCredits keeps the trigger compact: 843, 12.4k, 1.2M.
function formatCredits(n: number): string {
    if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1).replace(/\.0$/, "")}M`;
    if (n >= 10_000) return `${(n / 1_000).toFixed(1).replace(/\.0$/, "")}k`;
    return n.toLocaleString();
}

// resetLabel turns next_reset_at into a short relative note for the panel.
function resetLabel(iso: string | null): string | null {
    if (!iso) return null;
    const days = Math.ceil((new Date(iso).getTime() - Date.now()) / 86_400_000);
    if (days <= 0) return "Resets soon";
    if (days === 1) return "Resets tomorrow";
    return `Resets in ${days} days`;
}
