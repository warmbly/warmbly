// Billing > Balance: the balance and how long it lasts, adding funds (one-time or automatic) in one dialog,
// the store's rates, spend controls, activity and invoices. Returning from Stripe Checkout lands here
// (?top_up=, ?payment_method=) and is confirmed when the balance moves.

import React from "react";
import { useQueryClient, type QueryClient } from "@tanstack/react-query";
import { useLocation, useRouter } from "@tanstack/react-router";
import { Loader2Icon } from "lucide-react";
import toast from "react-hot-toast/headless";
import { useSearchParams } from "@/hooks/useSearchParams";
import AddFundsDialog, { type FundsMode, type FundsTab } from "@/components/app/billing/AddFundsDialog";
import BalanceOverview from "@/components/app/billing/BalanceOverview";
import { fundsPolicy } from "@/components/app/billing/funds";
import type { AppError } from "@/lib/api/client/normalizeError";
import { adoptCardSetup, updateAutoReload } from "@/lib/api/client/app/subscription/balance";
import { useStoreEnabled } from "@/lib/api/hooks/app/store/useStore";
import { RESUME_AUTO_RELOAD_KEY, balanceInvoicesQuery, balanceQuery, useBalance, useBalanceBurn } from "@/lib/api/hooks/app/subscription/useBalance";
import type { AutoReloadPatch, Balance } from "@/lib/api/models/app/subscription/Credits";
import buildError from "@/lib/helper/buildError";
import { fmtUsd } from "@/lib/money";
import { Section } from "../_components/SectionShell";
import AIUsageCard from "./AIUsageCard";
import BalanceActivity from "./BalanceActivity";
import InvoicesSection from "./InvoicesSection";

export default function BalanceTab({ isPaid }: { isPaid: boolean }) {
    const balance = useBalance();
    const confirming = useCheckoutReturn(balance.data);
    const ready = !!balance.data && !balance.data.unlimited;
    const burn = useBalanceBurn(ready);
    // The daily draw is its own read, so the balance every page shows stays cheap.
    const b = React.useMemo(() => (balance.data && burn.data ? { ...balance.data, burn: burn.data } : balance.data), [balance.data, burn.data]);
    const store = useStoreEnabled(ready);
    const [dialog, setDialog] = React.useState<{ mode: FundsMode; tab: FundsTab } | null>(null);
    const openFunds = (tab: FundsTab) => setDialog({ mode: "funds", tab });

    // The banner and notices link to #add-funds, #auto-reload and #ai-spend: open the dialog, or scroll to the section.
    // The hash is dropped once the dialog opens, so the same link works again and a reload does not reopen it.
    const router = useRouter();
    const hash = useLocation({ select: (l) => l.hash });
    React.useEffect(() => {
        if (!ready) return;
        const id = hash.replace(/^#/, "");
        if (!id) return;
        if (id === "add-funds" || id === "auto-reload") {
            setDialog({ mode: "funds", tab: id === "auto-reload" ? "auto" : "once" });
            void router.navigate({ to: ".", search: true, replace: true, resetScroll: false });
            return;
        }
        const t = setTimeout(() => document.getElementById(id)?.scrollIntoView({ behavior: "smooth", block: "start" }), 200);
        return () => clearTimeout(t);
    }, [ready, hash, router]);

    // A failed-payment notice links here with ?update_card=1: the card opens at once. Saving it charges what is unpaid.
    const [params, setParams] = useSearchParams();
    const wantsCard = params.get("update_card") === "1";
    React.useEffect(() => {
        if (!wantsCard || !ready) return;
        const next = new URLSearchParams(params);
        next.delete("update_card");
        setParams(next, { replace: true });
        setDialog({ mode: "card", tab: "once" });
    }, [wantsCard, ready, params, setParams]);

    if (balance.isPending) {
        return (
            <Section eyebrow="Balance">
                <div className="h-32 rounded-2xl skeleton-shimmer" />
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

    const canBuy = b.can_add_money ?? isPaid;
    return (
        <>
            <div id="balance" className="scroll-mt-14 px-4 py-5 md:px-8 md:py-6 space-y-3 border-b border-slate-200/70">
                {confirming && (
                    <div role="status" className="flex items-center gap-2 rounded-lg border border-sky-200 bg-sky-50 px-3 py-2 text-[12px] text-sky-800">
                        <Loader2Icon className="w-3.5 h-3.5 animate-spin shrink-0" />
                        Payment received. Adding it to the balance, this usually takes a few seconds.
                    </div>
                )}
                <BalanceOverview b={b} canBuy={canBuy} onAddFunds={openFunds} onPaymentMethod={() => setDialog({ mode: "card", tab: "once" })} />
            </div>
            <AddFundsDialog
                open={!!dialog}
                mode={dialog?.mode ?? "funds"}
                tab={dialog?.tab ?? "once"}
                onTab={(tab) => setDialog((d) => (d ? { ...d, tab } : d))}
                onCard={() => setDialog({ mode: "card", tab: "once" })}
                onClose={() => setDialog(null)}
                balance={b}
                policy={fundsPolicy(b.top_up, { enabled: store.enabled, minCardChargeMicros: store.status?.min_card_charge_micros })}
                canBuy={canBuy}
            />
            <div id="ai-spend" className="scroll-mt-14">
                <AIUsageCard />
            </div>
            <BalanceActivity />
            <InvoicesSection />
        </>
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
        if (pmParam) {
            const saved = pmParam === "success" && (params.get("redirect_status") ?? "succeeded") === "succeeded";
            // A bank that redirected skipped the in-page default; the card it saved becomes the default first.
            const setupIntent = params.get("setup_intent");
            const adopted = saved && setupIntent ? adoptCardSetup(setupIntent).catch(() => undefined) : Promise.resolve();
            void adopted.then(() => resumeAutoReload(saved, qc));
        }
        const next = new URLSearchParams(params);
        next.delete("top_up");
        next.delete("payment_method");
        next.delete("setup_intent");
        next.delete("setup_intent_client_secret");
        next.delete("redirect_status");
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
