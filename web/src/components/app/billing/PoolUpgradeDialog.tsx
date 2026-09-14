// The self-hosted pool upgrade. A customer arrives here from the "Unlimited
// for $15/mo" button on their own instance's Settings > Warmbly Cloud page, so
// they have already chosen: this confirms what they are buying, which
// workspace it applies to, and how often they are billed.
//
// The plan is not in the public plan list, so it has no card in the plans grid
// and its price comes from /pool-link/offer rather than the static catalog.

import React from "react";
import { createPortal } from "react-dom";
import { AnimatePresence, motion } from "framer-motion";
import toast from "react-hot-toast";
import { ArrowRightIcon, CheckIcon, Loader2Icon, ServerIcon, XIcon } from "lucide-react";
import { useAppStore } from "@/stores";
import { usePoolLinkOffer, useStartPoolLinkCheckout } from "@/lib/api/hooks/app/cloudlink/useCloudLink";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

type Interval = "month" | "year";

export default function PoolUpgradeDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
    const orgName = useAppStore((s) => s.currentOrganization?.name);
    const offer = usePoolLinkOffer(open);
    const checkout = useStartPoolLinkCheckout();
    const [interval, setInterval] = React.useState<Interval>("year");

    // Yearly is the better default, but only where it exists. Applied once per
    // opening: a background refetch must not move the choice under someone who
    // has already picked the other period.
    const defaulted = React.useRef(false);
    React.useEffect(() => {
        if (!open) {
            defaulted.current = false;
            return;
        }
        if (defaulted.current || !offer.data) return;
        defaulted.current = true;
        setInterval(offer.data.yearly_available ? "year" : "month");
    }, [open, offer.data]);

    const cardRef = React.useRef<HTMLDivElement>(null);
    React.useEffect(() => {
        if (!open) return;
        const previous = document.activeElement as HTMLElement | null;
        cardRef.current?.focus();
        const onKey = (e: KeyboardEvent) => {
            if (e.key === "Escape" && !checkout.isPending) {
                e.stopPropagation();
                onClose();
            }
        };
        document.addEventListener("keydown", onKey, true);
        return () => {
            document.removeEventListener("keydown", onKey, true);
            previous?.focus?.();
        };
    }, [open, checkout.isPending, onClose]);

    const data = offer.data;
    const available = data?.available === true;
    const monthly = data?.monthly_usd ?? 0;
    const yearly = data?.yearly_usd ?? 0;
    // Only worth showing when it is actually a saving.
    const yearlySavingPct =
        monthly > 0 && yearly > 0 ? Math.round((1 - yearly / (monthly * 12)) * 100) : 0;

    async function start() {
        if (checkout.isPending || !available) return;
        try {
            const res = await checkout.mutateAsync(interval);
            if (!res.checkout_url) throw new Error("no checkout url");
            window.location.href = res.checkout_url;
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    }

    return createPortal(
        <AnimatePresence>
            {open && (
                <motion.div
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.16 }}
                    onMouseDown={(e) => {
                        if (e.target === e.currentTarget && !checkout.isPending) onClose();
                    }}
                    className="fixed inset-0 z-[160] flex items-center justify-center bg-slate-900/40 backdrop-blur-[2px] px-4"
                >
                    <motion.div
                        role="dialog"
                        aria-modal="true"
                        aria-labelledby="pool-upgrade-title"
                        data-floating
                        ref={cardRef}
                        tabIndex={-1}
                        initial={{ opacity: 0, y: 8 }}
                        animate={{ opacity: 1, y: 0 }}
                        exit={{ opacity: 0, y: 8 }}
                        transition={{ duration: 0.18, ease: [0.22, 1, 0.36, 1] }}
                        onMouseDown={(e) => e.stopPropagation()}
                        className="w-full max-w-[440px] max-h-[calc(100dvh-3rem)] overflow-y-auto rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18)]"
                    >
                        <div className="h-12 px-4 border-b border-slate-200 flex items-center gap-2.5 sticky top-0 bg-white">
                            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                                Self-hosted
                            </span>
                            <div className="h-4 w-px bg-slate-200" />
                            <span id="pool-upgrade-title" className="text-[12.5px] text-slate-900 font-medium">
                                Unlimited pool mailboxes
                            </span>
                            <button
                                type="button"
                                onClick={onClose}
                                disabled={checkout.isPending}
                                aria-label="Close"
                                className="ml-auto size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors disabled:opacity-50"
                            >
                                <XIcon className="w-3.5 h-3.5" />
                            </button>
                        </div>

                        <div className="px-4 py-4 space-y-3.5">
                            <p className="text-[12.5px] text-slate-500 leading-relaxed">
                                Warm as many mailboxes as you like from your own instance, instead of the
                                free allowance. Sending, data and workers stay on your infrastructure;
                                this only covers the shared warmup pool.
                            </p>

                            <div className="flex items-center gap-2 rounded-md border border-slate-200 bg-slate-50 px-2.5 py-2">
                                <ServerIcon className="size-3.5 text-slate-400 shrink-0" aria-hidden="true" />
                                <span className="text-[12px] text-slate-600 truncate">
                                    Applies to <span className="text-slate-900 font-medium">{orgName || "this workspace"}</span>
                                </span>
                            </div>

                            {offer.isLoading && (
                                <div className="h-[92px] rounded-md border border-slate-200 flex items-center justify-center">
                                    <Loader2Icon className="size-4 text-slate-400 animate-spin" />
                                </div>
                            )}

                            {!offer.isLoading && !available && (
                                <div className="rounded-md border border-amber-200 bg-amber-50 px-3 py-2.5">
                                    <p className="text-[12px] text-amber-900">
                                        {offer.isError
                                            ? "The price could not be loaded. Try again in a moment."
                                            : "This instance has no price set for the pool plan yet, so it cannot be bought here."}
                                    </p>
                                </div>
                            )}

                            {!offer.isLoading && available && (
                                <div className="space-y-2">
                                    {data?.yearly_available && (
                                        <IntervalRow
                                            selected={interval === "year"}
                                            onSelect={() => setInterval("year")}
                                            label="Yearly"
                                            price={`$${yearly}`}
                                            per="per year"
                                            badge={yearlySavingPct > 0 ? `Save ${yearlySavingPct}%` : undefined}
                                        />
                                    )}
                                    {data?.monthly_available && (
                                        <IntervalRow
                                            selected={interval === "month"}
                                            onSelect={() => setInterval("month")}
                                            label="Monthly"
                                            price={`$${monthly}`}
                                            per="per month"
                                        />
                                    )}
                                </div>
                            )}
                        </div>

                        <div className="px-4 py-3 border-t border-slate-200 flex items-center justify-end gap-2">
                            <button
                                type="button"
                                onClick={onClose}
                                disabled={checkout.isPending}
                                className="h-8 px-3 rounded-md border border-slate-200 text-slate-700 text-[12.5px] font-medium hover:bg-slate-50 transition-colors disabled:opacity-50"
                            >
                                Not now
                            </button>
                            <button
                                type="button"
                                onClick={start}
                                disabled={!available || checkout.isPending}
                                className="h-8 px-3 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12.5px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-50"
                            >
                                {checkout.isPending ? (
                                    <Loader2Icon className="w-3.5 h-3.5 animate-spin" />
                                ) : (
                                    <ArrowRightIcon className="w-3.5 h-3.5" />
                                )}
                                Continue to payment
                            </button>
                        </div>
                    </motion.div>
                </motion.div>
            )}
        </AnimatePresence>,
        document.body,
    );
}

function IntervalRow({
    selected,
    onSelect,
    label,
    price,
    per,
    badge,
}: {
    selected: boolean;
    onSelect: () => void;
    label: string;
    price: string;
    per: string;
    badge?: string;
}) {
    return (
        <button
            type="button"
            onClick={onSelect}
            aria-pressed={selected}
            className={`w-full h-[52px] px-3 rounded-md border text-left flex items-center gap-2.5 transition-colors ${
                selected ? "border-sky-400 bg-sky-50" : "border-slate-200 hover:bg-slate-50"
            }`}
        >
            <span
                className={`size-4 rounded-full border inline-flex items-center justify-center shrink-0 ${
                    selected ? "border-sky-600 bg-sky-600 text-white" : "border-slate-300"
                }`}
            >
                {selected && <CheckIcon className="size-2.5" />}
            </span>
            <span className="text-[12.5px] text-slate-900 font-medium">{label}</span>
            {badge && (
                <span className="inline-flex items-center h-5 px-1.5 rounded bg-emerald-50 text-emerald-700 text-[11px] font-medium">
                    {badge}
                </span>
            )}
            <span className="ml-auto text-right">
                <span className="block text-[13px] text-slate-900 font-semibold">{price}</span>
                <span className="block text-[11px] text-slate-500">{per}</span>
            </span>
        </button>
    );
}
