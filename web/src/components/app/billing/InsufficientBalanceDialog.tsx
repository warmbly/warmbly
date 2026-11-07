// The app-wide answer to a 402 for a balance that ran out: what is left, and
// a way to add money right here (one click on the saved card, otherwise Stripe
// Checkout). A member who cannot manage billing is told who can.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { Link } from "@tanstack/react-router";
import { CreditCardIcon, Loader2Icon, WalletIcon, XIcon } from "lucide-react";
import { usePermission } from "@/hooks/usePermission";
import useFeatureAccess from "@/hooks/useFeatureAccess";
import { useUpgradeDialog } from "@/hooks/context/upgrade";
import { useBalance, useBalanceCheckout, useTopUp } from "@/lib/api/hooks/app/subscription/useBalance";
import { onInsufficientBalance } from "@/lib/insufficientBalance";
import { DEFAULT_TOP_UP_USD, fmtUsd, fmtUsdTrim } from "@/lib/money";
import { fmtDay, paymentMethodLabel } from "./balanceModel";

export default function InsufficientBalanceDialog() {
    const [open, setOpen] = React.useState(false);
    React.useEffect(() => onInsufficientBalance(() => setOpen(true)), []);
    return <AnimatePresence>{open && <DialogBody onClose={() => setOpen(false)} />}</AnimatePresence>;
}

function DialogBody({ onClose }: { onClose: () => void }) {
    const canManage = usePermission("MANAGE_BILLING");
    const access = useFeatureAccess();
    const upgrade = useUpgradeDialog();
    const balance = useBalance(canManage && access.billing);
    const pay = useTopUp();
    const checkout = useBalanceCheckout();
    const b = balance.data;
    const canBuy = canManage && access.billing && (b?.can_add_money ?? access.paid);
    const amount = b?.top_up?.default_usd ?? DEFAULT_TOP_UP_USD;
    const pm = b?.payment_method ?? null;
    const busy = pay.isPending || checkout.isPending;

    React.useEffect(() => {
        const onKey = (e: KeyboardEvent) => {
            if (e.key !== "Escape") return;
            // The confirm dialog or the reauth prompt on top takes Escape first.
            if (document.querySelector('[role="alertdialog"]')) return;
            onClose();
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [onClose]);

    const addMoney = () => {
        if (pm) pay.mutate(amount, { onSuccess: (r) => r && onClose() });
        else checkout.mutate(amount);
    };

    const body = !b
        ? "There isn't enough left on the workspace balance for this."
        : `${fmtUsd(b.balance_usd)} is left, not enough for this.${
              b.next_reset_at && b.monthly_allowance_usd > 0 ? ` The included usage resets ${fmtDay(b.next_reset_at)}.` : ""
          }`;

    return (
        <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.15 }}
            className="fixed inset-0 z-[70] flex items-center justify-center bg-slate-900/30 backdrop-blur-[2px] px-4"
            onMouseDown={(e) => {
                if (e.target === e.currentTarget && !busy) onClose();
            }}
        >
            <motion.div
                initial={{ y: 8, opacity: 0 }}
                animate={{ y: 0, opacity: 1 }}
                exit={{ y: 8, opacity: 0 }}
                transition={{ duration: 0.16 }}
                role="dialog"
                aria-modal="true"
                aria-labelledby="balance-dialog-title"
                className="w-full max-w-[420px] rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18),0_8px_16px_-8px_rgba(15,23,42,0.1)] overflow-hidden"
            >
                <div className="h-12 px-4 border-b border-slate-200 flex items-center gap-2.5">
                    <div className="size-5 rounded bg-amber-50 text-amber-600 flex items-center justify-center">
                        <WalletIcon className="w-3 h-3" />
                    </div>
                    <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Balance</span>
                    <div className="h-4 w-px bg-slate-200" />
                    <span id="balance-dialog-title" className="text-[12.5px] text-slate-900 font-medium truncate">
                        The balance is too low
                    </span>
                    <button
                        type="button"
                        onClick={onClose}
                        disabled={busy}
                        aria-label="Close"
                        className="ml-auto size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors disabled:opacity-50"
                    >
                        <XIcon className="w-3.5 h-3.5" />
                    </button>
                </div>

                <div className="px-4 py-4 space-y-2">
                    <p className="text-[12.5px] text-slate-700 leading-relaxed">{body}</p>
                    {canBuy ? (
                        <p className="text-[12px] text-slate-500 leading-relaxed">
                            Add money to keep going now. It never expires.
                            {!b?.auto_reload?.enabled && " Automatic reload can keep this from happening again."}
                        </p>
                    ) : canManage && access.billing ? (
                        <p className="text-[12px] text-slate-500 leading-relaxed">Adding money needs an active paid plan.</p>
                    ) : (
                        <p className="text-[12px] text-slate-500 leading-relaxed">
                            Ask the workspace owner or a billing admin to add money.
                        </p>
                    )}
                </div>

                <div className="px-3 min-h-12 py-2 border-t border-slate-200 flex flex-wrap items-center justify-end gap-1.5">
                    {canBuy ? (
                        <>
                            <Link
                                to="/app/settings/billing/{-$tab}"
                                params={{ tab: "balance" }}
                                onClick={onClose}
                                className="h-7 px-2.5 rounded-md text-[12px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center transition-colors"
                            >
                                Other amounts
                            </Link>
                            <button
                                type="button"
                                onClick={addMoney}
                                disabled={busy || balance.isPending}
                                className="h-7 px-3 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                            >
                                {busy ? <Loader2Icon className="w-3 h-3 animate-spin" /> : pm ? <CreditCardIcon className="w-3 h-3" /> : null}
                                {pm ? `Pay ${fmtUsdTrim(amount)} with ${paymentMethodLabel(pm)}` : `Add ${fmtUsdTrim(amount)}`}
                            </button>
                        </>
                    ) : canManage && access.billing ? (
                        <>
                            <button
                                type="button"
                                onClick={onClose}
                                className="h-7 px-2.5 rounded-md text-[12px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                            >
                                Not now
                            </button>
                            <button
                                type="button"
                                onClick={() => {
                                    onClose();
                                    upgrade.open({ feature: "Adding money", minPlan: "starter" });
                                }}
                                className="h-7 px-3 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium transition-colors"
                            >
                                See plans
                            </button>
                        </>
                    ) : (
                        <button
                            type="button"
                            onClick={onClose}
                            className="h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium transition-colors"
                        >
                            Got it
                        </button>
                    )}
                </div>
            </motion.div>
        </motion.div>
    );
}
