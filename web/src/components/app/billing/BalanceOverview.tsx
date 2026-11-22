// The top of Billing > Balance: what is on the balance, how long it lasts at today's spend, the automatic
// top-up, and, where the store is on, what the balance pays for.
import { AlertTriangleIcon, CreditCardIcon, GlobeIcon, PlusIcon, RefreshCwIcon } from "lucide-react";
import { motion, useReducedMotion } from "framer-motion";
import ProviderLogo from "@/components/app/emails/ProviderLogo";
import AnimatedNumber from "@/components/ui/AnimatedNumber";
import { useStoreEnabled } from "@/lib/api/hooks/app/store/useStore";
import type { Balance } from "@/lib/api/models/app/subscription/Credits";
import { fmtMonthly } from "@/lib/buy/money";
import { HOVER_LIFT, PRESS_SPRING } from "@/lib/buy/motion";
import { fmtUsd, fmtUsdTrim, topUpCharge } from "@/lib/money";
import { cn } from "@/lib/utils";
import { RELOAD_FAILURE_COPY, balanceAlert, expiresSoon, fmtClock, fmtDay, paymentMethodExpiry, paymentMethodLabel, reloadCovers } from "./balanceModel";
import type { FundsTab } from "./AddFundsDialog";
import { lastsDays, lastsLabel, ordinal } from "./funds";

export default function BalanceOverview({
    b,
    canBuy,
    onAddFunds,
    onPaymentMethod,
}: {
    b: Balance;
    canBuy: boolean;
    onAddFunds: (tab: FundsTab) => void;
    onPaymentMethod: () => void;
}) {
    const { enabled: storeOn, status } = useStoreEnabled();
    return (
        <div className={cn("grid grid-cols-1 gap-3", storeOn && "lg:grid-cols-[minmax(0,1.5fr)_minmax(0,1fr)]")}>
            <BalanceCard b={b} canBuy={canBuy} onAddFunds={onAddFunds} onPaymentMethod={onPaymentMethod} />
            {storeOn && status && (
                <RatesPanel monthlyMicros={status.mailbox_monthly_micros} atCost={status.domain_markup === "at_cost"} billingDay={b.burn?.billing_day ?? status.billing_day ?? null} />
            )}
        </div>
    );
}

function BalanceCard({ b, canBuy, onAddFunds, onPaymentMethod }: { b: Balance; canBuy: boolean; onAddFunds: (tab: FundsTab) => void; onPaymentMethod: () => void }) {
    const reduced = useReducedMotion();
    const alert = balanceAlert(b);
    const burn = b.burn;
    const perDay = burn?.per_day_usd ?? 0;
    const days = lastsDays(b.balance_usd, perDay);
    const parts = burn
        ? [
              burn.mailboxes_per_day_usd > 0 ? `mailboxes ${fmtUsd(burn.mailboxes_per_day_usd)}` : null,
              burn.renewals_per_day_usd > 0 ? `renewals ${fmtUsd(burn.renewals_per_day_usd)}` : null,
              burn.ai_per_day_usd > 0 ? `AI ${fmtUsd(burn.ai_per_day_usd)}` : null,
          ].filter(Boolean)
        : [];
    const ar = b.auto_reload;
    const failureCode = ar && !ar.enabled ? ar.failure_code : null;
    const failure = failureCode ? RELOAD_FAILURE_COPY[failureCode] : null;
    const retryAt = ar?.enabled && ar.retry_at ? new Date(ar.retry_at) : null;
    const pm = b.payment_method ?? null;
    const exp = pm ? paymentMethodExpiry(pm) : null;
    const expiring = !!pm && !!exp && expiresSoon(pm);
    const last = ar?.last_reload ?? null;
    return (
        <section aria-label="Balance" className="rounded-2xl border border-slate-200 bg-white p-5">
            <div className="flex flex-wrap items-start gap-x-4 gap-y-3">
                <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                        <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Balance</span>
                        {alert && (
                            <span className={cn("inline-flex items-center gap-1 text-[10.5px] font-medium", alert === "out" ? "text-red-600" : "text-amber-600")}>
                                <span className={cn("size-1.5 rounded-full", alert === "out" ? "bg-red-500" : "bg-amber-500")} />
                                {alert === "out" ? "Used up, AI is paused" : reloadCovers(b) ? "Low, tops up automatically" : "Running low"}
                            </span>
                        )}
                    </div>
                    <AnimatedNumber
                        value={b.balance_usd}
                        format={(n) => fmtUsd(n)}
                        className={cn("mt-1.5 block text-[32px] leading-none font-semibold tracking-[-0.02em] tabular-nums", alert === "out" ? "text-red-600" : "text-slate-900")}
                    />
                    {b.monthly_allowance_usd > 0 && (
                        <p className="mt-1.5 text-[11.5px] text-slate-500">
                            Includes {fmtUsd(b.monthly_usd)} of {fmtUsd(b.monthly_allowance_usd)} included AI usage
                            {b.next_reset_at ? `, resets ${fmtDay(b.next_reset_at)}` : ""}
                        </p>
                    )}
                </div>
                <div className="flex items-center gap-2">
                    <motion.button
                        type="button"
                        onClick={() => onAddFunds("once")}
                        disabled={!canBuy}
                        whileHover={reduced || !canBuy ? undefined : HOVER_LIFT}
                        whileTap={reduced || !canBuy ? undefined : { scale: 0.97 }}
                        transition={PRESS_SPRING}
                        className="h-8 px-3 rounded-lg bg-sky-600 hover:bg-sky-700 text-white text-[12.5px] font-semibold inline-flex items-center gap-1.5 transition-colors outline-none focus-visible:ring-2 focus-visible:ring-sky-300 focus-visible:ring-offset-2 disabled:opacity-55 disabled:cursor-not-allowed"
                    >
                        <PlusIcon className="w-3.5 h-3.5" /> Add funds
                    </motion.button>
                    <button
                        type="button"
                        onClick={onPaymentMethod}
                        className="h-8 px-3 rounded-lg border border-slate-200 bg-white hover:border-slate-300 text-slate-700 hover:text-slate-900 text-[12.5px] font-medium inline-flex items-center gap-1.5 transition-colors outline-none focus-visible:ring-2 focus-visible:ring-sky-200"
                    >
                        <CreditCardIcon className="w-3.5 h-3.5 text-slate-400" /> Payment method
                    </button>
                </div>
            </div>

            <div className="mt-4 rounded-xl bg-slate-50 border border-slate-100 px-3.5 py-2.5">
                <div className="flex items-center justify-between gap-3">
                    <span className="text-[13px] font-medium text-slate-800">{lastsLabel(days)}</span>
                    {perDay > 0 && <span className="text-[11.5px] text-slate-500 tabular-nums">{fmtUsd(perDay)}/day avg</span>}
                </div>
                {parts.length > 0 && <p className="mt-0.5 text-[11px] text-slate-400">A day of {parts.join(", ")}</p>}
            </div>

            {!canBuy && <p className="mt-3 text-[11.5px] text-slate-500">Adding funds needs an active paid plan.</p>}
            {failure ? (
                <div role="status" className="mt-3 flex flex-wrap items-start gap-x-2 gap-y-1.5 text-[11.5px] text-amber-800 leading-relaxed">
                    <AlertTriangleIcon className="w-3.5 h-3.5 mt-px shrink-0" />
                    <span className="min-w-0 flex-1">
                        Auto top-up stopped{ar?.failed_at ? ` ${fmtDay(ar.failed_at)}` : ""}. {failure}{" "}
                        {canBuy && (
                            <button type="button" onClick={() => onAddFunds("auto")} className="font-medium underline underline-offset-2">
                                Turn it back on
                            </button>
                        )}
                    </span>
                    {failureCode !== "unavailable" && <NoticeButton onClick={onPaymentMethod}>{pm ? "Update card" : "Add a card"}</NoticeButton>}
                </div>
            ) : ar ? (
                <div className="mt-3 space-y-1">
                    <p className="flex items-center gap-1.5 text-[11.5px] text-slate-500">
                        <RefreshCwIcon className="w-3 h-3 text-slate-400 shrink-0" />
                        <span className="min-w-0">
                            {ar.enabled
                                ? `Auto top-up adds ${fmtUsdTrim(ar.amount_usd)} (charged ${fmtUsd(topUpCharge(ar.amount_usd).totalUsd)} with the 3% processing fee) when the balance drops below ${fmtUsd(ar.threshold_usd)}, up to ${fmtUsdTrim(ar.monthly_limit_usd)} a month.`
                                : "Auto top-up is off."}{" "}
                            {(ar.enabled || canBuy) && (
                                <button type="button" onClick={() => onAddFunds("auto")} className="font-medium text-sky-700 hover:text-sky-900 transition-colors">
                                    {ar.enabled ? "Change" : "Turn on"}
                                </button>
                            )}
                        </span>
                    </p>
                    {(last || ar.enabled) && (
                        <p className="pl-[18px] flex flex-wrap gap-x-3 text-[11px] text-slate-400 tabular-nums">
                            <span>{last ? `Last top-up ${fmtDay(last.at)}: ${fmtUsd(last.amount_usd)}` : "No top-ups yet"}</span>
                            {ar.enabled && (
                                <span>
                                    {fmtUsd(ar.reloaded_this_month_usd)} of {fmtUsdTrim(ar.monthly_limit_usd)} this month
                                </span>
                            )}
                        </p>
                    )}
                </div>
            ) : null}
            {retryAt && (
                <div role="status" className="mt-3 flex flex-wrap items-start gap-x-2 gap-y-1.5 rounded-lg border border-amber-200 bg-amber-50/70 px-3 py-2 text-[11.5px] text-amber-900 leading-relaxed">
                    <AlertTriangleIcon className="w-3.5 h-3.5 mt-px shrink-0" />
                    <span className="min-w-0 flex-1">
                        The bank declined the last top-up for now. It is tried again{" "}
                        {retryAt.toDateString() === new Date().toDateString() ? `at ${fmtClock(retryAt)}` : `${fmtDay(retryAt)} at ${fmtClock(retryAt)}`}. Auto top-up stays on;
                        update the card if it has changed.
                    </span>
                    <NoticeButton onClick={onPaymentMethod}>Update card</NoticeButton>
                </div>
            )}
            {expiring && pm && (
                <div role="status" className="mt-3 flex flex-wrap items-start gap-x-2 gap-y-1.5 text-[11.5px] text-amber-800 leading-relaxed">
                    <AlertTriangleIcon className="w-3.5 h-3.5 mt-px shrink-0" />
                    <span className="min-w-0 flex-1">
                        {paymentMethodLabel(pm)} expires {exp}. Replace it before a top-up fails.
                    </span>
                    <NoticeButton onClick={onPaymentMethod}>Replace card</NoticeButton>
                </div>
            )}
        </section>
    );
}

function NoticeButton({ onClick, children }: { onClick: () => void; children: React.ReactNode }) {
    return (
        <button
            type="button"
            onClick={onClick}
            className="shrink-0 h-6 px-2 rounded-md border border-amber-300 bg-white text-[11.5px] font-medium text-amber-900 hover:bg-amber-50 transition-colors outline-none focus-visible:ring-2 focus-visible:ring-amber-200"
        >
            {children}
        </button>
    );
}

function RatesPanel({ monthlyMicros, atCost, billingDay }: { monthlyMicros: number; atCost: boolean; billingDay: number | null }) {
    const rows: { icon: React.ReactNode; label: string; value: string }[] = [
        { icon: <ProviderLogo id="google" size="sm" />, label: "Google mailbox", value: fmtMonthly(monthlyMicros) },
        { icon: <ProviderLogo id="microsoft" size="sm" />, label: "Microsoft mailbox", value: fmtMonthly(monthlyMicros) },
        {
            icon: (
                <span className="size-5 rounded border border-slate-200 inline-flex items-center justify-center">
                    <GlobeIcon className="w-3 h-3 text-slate-400" />
                </span>
            ),
            label: "Domains",
            value: atCost ? "At cost" : "As priced in search",
        },
    ];
    return (
        <section aria-label="Your rates" className="rounded-2xl border border-slate-200 bg-white p-5">
            <h3 className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Your rates</h3>
            <ul className="mt-2 divide-y divide-slate-100">
                {rows.map((r) => (
                    <li key={r.label} className="py-2.5 flex items-center gap-2.5">
                        {r.icon}
                        <span className="min-w-0 flex-1 text-[12.5px] text-slate-800">{r.label}</span>
                        <span className="text-[12.5px] font-medium text-slate-900 tabular-nums">{r.value}</span>
                    </li>
                ))}
            </ul>
            <p className="mt-2 text-[11.5px] text-slate-500 leading-relaxed">
                {billingDay
                    ? `Paid from your balance on your monthly billing day, the ${ordinal(billingDay)}.`
                    : "Paid from your balance on your monthly billing day, set by your first mailbox."}
            </p>
        </section>
    );
}
