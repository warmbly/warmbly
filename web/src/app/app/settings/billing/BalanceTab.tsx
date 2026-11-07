// Billing > Balance: what is left, adding money, automatic reload, the saved
// card, spend controls, activity and invoices. Returning from Stripe Checkout
// lands here (?top_up=, ?payment_method=) and is confirmed when the balance moves.

import React from "react";
import { useQueryClient, type QueryClient } from "@tanstack/react-query";
import { AlertTriangleIcon, CreditCardIcon, Loader2Icon } from "lucide-react";
import toast from "react-hot-toast/headless";
import { useSearchParams } from "@/hooks/useSearchParams";
import { DitherMeter } from "@/components/ui/dither";
import AnimatedNumber from "@/components/ui/AnimatedNumber";
import TopUpPanel from "@/components/app/billing/TopUpPanel";
import { balanceAlert, expiresSoon, fmtDay, paymentMethodExpiry, paymentMethodLabel, reloadCovers } from "@/components/app/billing/balanceModel";
import { updateAutoReload } from "@/lib/api/client/app/subscription/balance";
import type { AppError } from "@/lib/api/client/normalizeError";
import {
    RESUME_AUTO_RELOAD_KEY,
    balanceInvoicesQuery,
    balanceQuery,
    useAddPaymentMethod,
    useBalance,
} from "@/lib/api/hooks/app/subscription/useBalance";
import type { AutoReloadPatch, Balance } from "@/lib/api/models/app/subscription/Credits";
import buildError from "@/lib/helper/buildError";
import { fmtUsd } from "@/lib/money";
import { cn } from "@/lib/utils";
import { Row, Section } from "../_components/SectionShell";
import AutoReloadSection from "./AutoReloadSection";
import AIUsageCard from "./AIUsageCard";
import BalanceActivity from "./BalanceActivity";
import InvoicesSection from "./InvoicesSection";

export default function BalanceTab({ isPaid }: { isPaid: boolean }) {
    const balance = useBalance();
    const confirming = useCheckoutReturn(balance.data);
    const b = balance.data;
    const ready = !!b && !b.unlimited;

    // The banner and header link to #auto-reload and #ai-spend; scroll once the sections exist.
    React.useEffect(() => {
        if (!ready) return;
        const id = window.location.hash.slice(1);
        if (!id) return;
        const t = setTimeout(() => document.getElementById(id)?.scrollIntoView({ behavior: "smooth", block: "start" }), 200);
        return () => clearTimeout(t);
    }, [ready]);

    if (balance.isPending) {
        return (
            <Section eyebrow="Balance">
                <div className="h-32 rounded bg-slate-100 animate-pulse" />
            </Section>
        );
    }
    if (!b) {
        return (
            <Section eyebrow="Balance">
                <p className="text-[11.5px] text-slate-500">The balance could not be loaded right now.</p>
            </Section>
        );
    }
    if (b.unlimited) {
        return (
            <Section eyebrow="Balance" description="AI use is not metered on this deployment, so there is nothing to pay for.">
                <span />
            </Section>
        );
    }

    return (
        <>
            <div id="balance" className="scroll-mt-14">
                <Section
                    eyebrow="Balance"
                    description="AI features are charged from this balance at what each call actually uses. Your plan's included usage is spent first and resets each cycle; money you add never expires."
                >
                    {confirming && (
                        <div role="status" className="flex items-center gap-2 rounded-md border border-sky-200 bg-sky-50 px-3 py-2 text-[12px] text-sky-800">
                            <Loader2Icon className="w-3.5 h-3.5 animate-spin shrink-0" />
                            Payment received. Adding it to the balance, this usually takes a few seconds.
                        </div>
                    )}
                    <BalanceSummary b={b} />
                    <TopUpPanel balance={b} canBuy={b.can_add_money ?? isPaid} />
                </Section>
            </div>
            <AutoReloadSection balance={b} />
            <PaymentMethodSection b={b} />
            <div id="ai-spend" className="scroll-mt-14">
                <AIUsageCard />
            </div>
            <BalanceActivity />
            <InvoicesSection />
        </>
    );
}

function BalanceSummary({ b }: { b: Balance }) {
    const alert = balanceAlert(b);
    const covered = reloadCovers(b);
    const allowance = b.monthly_allowance_usd;
    const frac = allowance > 0 ? Math.max(0, Math.min(1, b.monthly_usd / allowance)) : 0;
    return (
        <div className="flex flex-col sm:flex-row sm:items-end gap-4 sm:gap-8">
            <div className="min-w-0">
                <div className="flex items-center gap-2">
                    <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Available</span>
                    {alert && (
                        <span
                            className={cn(
                                "inline-flex items-center gap-1 text-[10.5px] font-medium",
                                alert === "out" ? "text-red-600" : "text-amber-600",
                            )}
                        >
                            <span className={cn("size-1.5 rounded-full", alert === "out" ? "bg-red-500" : "bg-amber-500")} />
                            {alert === "out" ? "Used up, AI is paused" : covered ? "Low, reloads automatically" : "Running low"}
                        </span>
                    )}
                </div>
                <AnimatedNumber
                    value={b.balance_usd}
                    format={(n) => fmtUsd(n)}
                    className={cn(
                        "mt-1 block text-[28px] leading-none font-semibold tracking-tight tabular-nums",
                        alert === "out" ? "text-red-600" : "text-slate-900",
                    )}
                />
            </div>
            <div className="min-w-0 flex-1 grid grid-cols-1 sm:grid-cols-2 gap-x-6 gap-y-2.5 text-[11.5px]">
                <div>
                    <div className="flex items-baseline justify-between gap-2">
                        <span className="text-slate-500">Included this cycle</span>
                        <span className="font-mono tabular-nums text-slate-700">
                            {fmtUsd(b.monthly_usd)}
                            <span className="text-slate-400"> of {fmtUsd(allowance)}</span>
                        </span>
                    </div>
                    <DitherMeter frac={frac} tone={frac <= 0.15 ? "amber" : "sky"} height={4} className="mt-1.5" />
                    <div className="mt-1 text-[10.5px] text-slate-400">
                        {fmtUsd(b.monthly_used_usd)} used{b.next_reset_at ? `, resets ${fmtDay(b.next_reset_at)}` : ""}
                    </div>
                </div>
                <div>
                    <div className="flex items-baseline justify-between gap-2">
                        <span className="text-slate-500">Added money</span>
                        <span className="font-mono tabular-nums text-slate-700">{fmtUsd(b.purchased_usd)}</span>
                    </div>
                    <div className="mt-1 text-[10.5px] text-slate-400">Never expires. Spent after the included usage.</div>
                </div>
            </div>
        </div>
    );
}

function PaymentMethodSection({ b }: { b: Balance }) {
    const addCard = useAddPaymentMethod();
    const pm = b.payment_method ?? null;
    const exp = pm ? paymentMethodExpiry(pm) : null;
    const soon = pm ? expiresSoon(pm) : false;
    return (
        <Section eyebrow="Card" description="Used for one-click payments and automatic reload. Stripe keeps the card; Warmbly never sees the number.">
            <Row
                label={
                    pm ? (
                        <span className="inline-flex items-center gap-2">
                            <CreditCardIcon className="w-3.5 h-3.5 text-slate-400" />
                            <span className="font-mono">{paymentMethodLabel(pm)}</span>
                        </span>
                    ) : (
                        "No card saved"
                    )
                }
                description={
                    pm ? (
                        soon ? (
                            <span className="inline-flex items-center gap-1 text-amber-700">
                                <AlertTriangleIcon className="w-3 h-3" />
                                Expires {exp}. Replace it before a reload fails.
                            </span>
                        ) : exp ? (
                            `Expires ${exp}`
                        ) : undefined
                    ) : (
                        "Add one to pay in one click and to turn on automatic reload."
                    )
                }
            >
                <button
                    type="button"
                    onClick={() => addCard.mutate()}
                    disabled={addCard.isPending}
                    className={cn(
                        "h-7 px-2.5 rounded-md text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60",
                        pm
                            ? "border border-slate-200 bg-white hover:border-slate-300 text-slate-700 hover:text-slate-900"
                            : "bg-sky-600 hover:bg-sky-700 text-white",
                    )}
                >
                    {addCard.isPending ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <CreditCardIcon className="w-3 h-3" />}
                    {pm ? "Replace card" : "Add a card"}
                </button>
            </Row>
        </Section>
    );
}

/** Handles ?top_up= and ?payment_method=, then waits for the balance to rise. */
function useCheckoutReturn(b: Balance | undefined): boolean {
    const qc = useQueryClient();
    const [params, setParams] = useSearchParams();
    const topUp = params.get("top_up");
    const pmParam = params.get("payment_method");
    const [confirming, setConfirming] = React.useState(false);
    const base = React.useRef<number | null>(null);
    const handled = React.useRef(false);

    React.useEffect(() => {
        if (handled.current || (!topUp && !pmParam)) return;
        handled.current = true;
        if (topUp === "success") {
            base.current = qc.getQueryData<Balance>(balanceQuery.queryKey)?.purchased_usd ?? null;
            setConfirming(true);
        } else if (topUp === "canceled") {
            toast("Checkout canceled. Nothing was charged.");
        }
        if (pmParam) resumeAutoReload(pmParam === "success", qc);
        const next = new URLSearchParams(params);
        next.delete("top_up");
        next.delete("payment_method");
        setParams(next, { replace: true });
    }, [topUp, pmParam, params, setParams, qc]);

    // The webhook may land after the person is back; realtime refreshes the balance when it does.
    React.useEffect(() => {
        if (!confirming || !b) return;
        if (base.current === null) {
            base.current = b.purchased_usd;
            return;
        }
        if (b.purchased_usd > base.current) {
            toast.success(`Added ${fmtUsd(b.purchased_usd - base.current)} to the balance`);
            void qc.invalidateQueries({ queryKey: balanceInvoicesQuery.queryKey });
            base.current = null;
            setConfirming(false);
        }
    }, [b, confirming, qc]);

    // A safety net for a missed realtime event, then give up quietly.
    React.useEffect(() => {
        if (!confirming) return;
        const refetch = [4_000, 12_000].map((ms) =>
            setTimeout(() => void qc.invalidateQueries({ queryKey: balanceQuery.queryKey }), ms),
        );
        const stop = setTimeout(() => setConfirming(false), 30_000);
        return () => {
            refetch.forEach(clearTimeout);
            clearTimeout(stop);
        };
    }, [confirming, qc]);

    return confirming;
}

/** Back from saving a card: turn on automatic reload with the settings chosen before leaving. */
function resumeAutoReload(saved: boolean, qc: QueryClient) {
    let patch: AutoReloadPatch | null = null;
    try {
        patch = JSON.parse(sessionStorage.getItem(RESUME_AUTO_RELOAD_KEY) ?? "null") as AutoReloadPatch | null;
        sessionStorage.removeItem(RESUME_AUTO_RELOAD_KEY);
    } catch {
        patch = null;
    }
    if (!saved) {
        toast("No card was added.");
        return;
    }
    void qc.invalidateQueries({ queryKey: balanceQuery.queryKey });
    if (!patch) {
        toast.success("Card saved");
        return;
    }
    const body = { ...patch, enabled: true };
    // Stripe's webhook attaches the card; give it a moment when the first try beats it.
    const attempt = (left: number) =>
        updateAutoReload(body)
            .then((next) => {
                qc.setQueryData(balanceQuery.queryKey, next);
                toast.success("Automatic reload is on");
            })
            .catch((e: AppError) => {
                if (e?.code === "payment_method_required" && left > 0) {
                    setTimeout(() => void attempt(left - 1), 2_500);
                    return;
                }
                toast.error(buildError(e));
            });
    void attempt(3);
}
