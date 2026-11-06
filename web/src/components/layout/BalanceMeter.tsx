// BalanceMeter: the workspace balance in the top header, beside the plan badge.
//
// The trigger blends with the header chrome and shows dollars left (or spent
// against the tightest spend limit when one is set, since that is the real
// ceiling). It takes on color only when something needs attention: amber when
// low and nothing will reload it, red when used up. The panel has the full
// picture and a way to add money. Rendered for members who manage billing (the
// endpoints are gated on it) and refreshed by the realtime spine.

import React from "react";
import { Link } from "@tanstack/react-router";
import { AnimatePresence, motion } from "framer-motion";
import { ArrowUpRightIcon, PlusIcon } from "lucide-react";
import useClickOutside from "@/hooks/useClickOutside";
import AnimatedNumber from "@/components/ui/AnimatedNumber";
import { useBalance } from "@/lib/api/hooks/app/subscription/useBalance";
import useCreditUsage from "@/lib/api/hooks/app/subscription/useCreditUsage";
import type { Balance } from "@/lib/api/models/app/subscription/Credits";
import { usePermission } from "@/hooks/usePermission";
import useAiMetered from "@/hooks/useAiMetered";
import { DitherMeter } from "@/components/ui/dither";
import { balanceAlert, reloadCovers } from "@/components/app/billing/balanceModel";
import { fmtUsd, fmtUsdTrim } from "@/lib/money";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores";

export function BalanceMeter() {
    const isTest = useAppStore((s) => s.currentOrganization?.category === "test");
    const canSee = usePermission("MANAGE_BILLING");
    const metered = useAiMetered();
    const balance = useBalance(canSee && metered);
    // Fetched up front so the trigger can show the tightest spend window without a click.
    const usage = useCreditUsage(30, canSee && metered);
    const [open, setOpen] = React.useState(false);
    const ref = React.useRef<HTMLDivElement>(null);
    const close = React.useCallback(() => setOpen(false), []);
    useClickOutside(open, close, ref);

    // Nothing to gauge when AI is not metered (self-host without billing).
    if (!canSee || !metered || !balance.data || balance.data.unlimited) return null;

    const b = balance.data;
    const alert = balanceAlert(b);
    const covered = reloadCovers(b);
    const empty = alert === "out";
    const low = alert === "low" && !covered;

    const u = usage.data;
    const spendWindow =
        u?.limit_daily_usd != null
            ? { spent: u.spent_today_usd, limit: u.limit_daily_usd, word: "today", short: "today" }
            : u?.limit_weekly_usd != null
              ? { spent: u.spent_week_usd, limit: u.limit_weekly_usd, word: "this week", short: "wk" }
              : u?.limit_monthly_usd != null
                ? { spent: u.spent_month_usd, limit: u.limit_monthly_usd, word: "this month", short: "mo" }
                : null;

    const fraction = spendWindow
        ? Math.max(0, 1 - spendWindow.spent / Math.max(0.01, spendWindow.limit))
        : b.monthly_allowance_usd > 0
          ? Math.min(1, b.monthly_usd / b.monthly_allowance_usd)
          : b.balance_usd > 0
            ? 1
            : 0;

    const label = spendWindow
        ? `${fmtUsd(spendWindow.spent)} of ${fmtUsdTrim(spendWindow.limit)} ${isTest ? "test balance " : ""}spent ${spendWindow.word}`
        : `${fmtUsd(b.balance_usd)} ${isTest ? "test balance" : "balance"} left`;

    return (
        <div ref={ref} className="relative">
            <button
                type="button"
                onClick={() => setOpen((o) => !o)}
                aria-label={label}
                aria-expanded={open}
                title={open ? undefined : label}
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
                {isTest && <span>Test balance</span>}
                {spendWindow ? (
                    <span className="flex items-baseline gap-1">
                        <span className="flex items-baseline">
                            <AnimatedNumber value={spendWindow.spent} format={fmtUsd} />
                            <span className="opacity-60 font-normal">/{fmtUsdTrim(spendWindow.limit)}</span>
                        </span>
                        <span className="opacity-60 font-normal">{spendWindow.short}</span>
                    </span>
                ) : (
                    <AnimatedNumber value={b.balance_usd} format={fmtUsd} />
                )}
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
                        <MeterPanel b={b} low={low} empty={empty} covered={covered} isTest={isTest} />
                        <div className="flex items-center border-t border-slate-200">
                            <Link
                                to="/app/settings/billing/{-$tab}"
                                params={{ tab: "balance" }}
                                onClick={close}
                                className="flex-1 flex items-center gap-1 px-3 h-9 text-[11.5px] font-medium text-sky-700 hover:bg-sky-50 transition-colors"
                            >
                                <PlusIcon className="w-3 h-3" />
                                Add money
                            </Link>
                            <Link
                                to="/app/settings/billing/{-$tab}"
                                params={{ tab: "balance" }}
                                hash="ai-spend"
                                onClick={close}
                                className="flex items-center gap-1 px-3 h-9 border-l border-slate-200 text-[11.5px] font-medium text-slate-600 hover:text-slate-900 hover:bg-slate-50 transition-colors"
                            >
                                Usage &amp; limits
                                <ArrowUpRightIcon className="w-3 h-3" />
                            </Link>
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}

// The panel body (shares the usage query cache with the trigger).
function MeterPanel({
    b,
    low,
    empty,
    covered,
    isTest,
}: {
    b: Balance;
    low: boolean;
    empty: boolean;
    covered: boolean;
    isTest: boolean;
}) {
    const usage = useCreditUsage();
    const allowance = b.monthly_allowance_usd;
    const planFraction = allowance > 0 ? b.monthly_usd / allowance : 0;
    const reset = resetLabel(b.next_reset_at);
    const ar = b.auto_reload;

    return (
        <div className="px-3 pt-3 pb-2.5 space-y-3.5">
            <div className="flex items-end justify-between">
                <div>
                    <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                        {isTest ? "Test balance" : "Balance"}
                    </div>
                    <div className="mt-1 flex items-baseline gap-1.5">
                        <AnimatedNumber
                            value={b.balance_usd}
                            format={fmtUsd}
                            className={cn(
                                "text-[22px] leading-none font-semibold tracking-tight tabular-nums",
                                empty ? "text-red-600" : "text-slate-900",
                            )}
                        />
                        <span className="text-[11px] text-slate-400">left</span>
                    </div>
                </div>
                {(empty || low || ar?.enabled) && (
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
                        {empty ? "Used up" : low ? "Running low" : "Reload on"}
                    </div>
                )}
            </div>

            <div>
                <div className="flex items-baseline justify-between text-[11.5px]">
                    <span className="text-slate-600">{isTest ? "Test allowance" : "Included this cycle"}</span>
                    <span className="tabular-nums text-slate-900 font-medium">
                        {fmtUsd(b.monthly_usd)}
                        <span className="text-slate-400 font-normal"> / {fmtUsd(allowance)}</span>
                    </span>
                </div>
                <DitherMeter
                    frac={Math.min(1, Math.max(0, planFraction))}
                    tone={empty ? "rose" : low ? "amber" : "slate"}
                    height={4}
                    className="mt-1.5"
                />
                <div className="mt-1 flex justify-between text-[10.5px] text-slate-400">
                    <span>{fmtUsd(b.monthly_used_usd)} used</span>
                    {reset && <span>{reset}</span>}
                </div>
            </div>

            <div className="flex items-baseline justify-between text-[11.5px]">
                <span className="text-slate-600">Added money</span>
                <span className={cn("tabular-nums font-medium", b.purchased_usd > 0 ? "text-slate-900" : "text-slate-400")}>
                    {fmtUsd(b.purchased_usd)}
                    <span className="text-slate-400 font-normal"> never expires</span>
                </span>
            </div>

            <div className="space-y-1.5">
                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Spent</span>
                {usage.isPending || !usage.data ? (
                    <div className="space-y-1.5 animate-pulse">
                        <div className="h-3.5 rounded bg-slate-100" />
                        <div className="h-3.5 rounded bg-slate-100" />
                        <div className="h-3.5 rounded bg-slate-100" />
                    </div>
                ) : (
                    <>
                        <SpendRow label="Today" spent={usage.data.spent_today_usd} limit={usage.data.limit_daily_usd} />
                        <SpendRow label="This week" spent={usage.data.spent_week_usd} limit={usage.data.limit_weekly_usd} />
                        <SpendRow label="This month" spent={usage.data.spent_month_usd} limit={usage.data.limit_monthly_usd} />
                    </>
                )}
            </div>

            <div className="text-[10.5px] text-slate-400">
                {ar?.enabled
                    ? covered
                        ? `Adds ${fmtUsdTrim(ar.amount_usd)} automatically below ${fmtUsd(ar.threshold_usd)}.`
                        : "Automatic reload reached its monthly limit."
                    : "Automatic reload is off."}
            </div>
        </div>
    );
}

// One spend window: the amount and, with a limit, a bar that warns as it nears.
function SpendRow({ label, spent, limit }: { label: string; spent: number; limit: number | null }) {
    const fraction = limit ? Math.min(1, spent / limit) : null;
    return (
        <div>
            <div className="flex items-baseline justify-between text-[11.5px]">
                <span className="text-slate-600">{label}</span>
                <span className="tabular-nums text-slate-900 font-medium">
                    {fmtUsd(spent)}
                    {limit != null && <span className="text-slate-400 font-normal"> of {fmtUsdTrim(limit)}</span>}
                </span>
            </div>
            {fraction != null && (
                <DitherMeter
                    frac={Math.min(1, fraction)}
                    tone={fraction >= 1 ? "rose" : fraction >= 0.8 ? "amber" : "slate"}
                    height={4}
                    className="mt-1"
                />
            )}
        </div>
    );
}

// Small ring gauge drawn with currentColor so it follows the trigger's tone.
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

function resetLabel(iso: string | Date | null): string | null {
    if (!iso) return null;
    const days = Math.ceil((new Date(iso).getTime() - Date.now()) / 86_400_000);
    if (days <= 0) return "Resets soon";
    if (days === 1) return "Resets tomorrow";
    return `Resets in ${days} days`;
}
