// The big plan card, shared by the full-screen upgrade dialog and the
// Billing > Plans tab so the two never drift.
//
// Prices roll on a per-digit odometer, and flipping the billing interval
// pulses the price and sweeps a sheen across the card, staggered by `index`
// so a row of them reads left to right.
//
// `feature` is only passed by the upgrade dialog, where the card has to say
// whether that plan unlocks the thing the user just hit.

import React from "react";
import { AnimatePresence, motion, useAnimationControls, useReducedMotion } from "framer-motion";
import { ArrowRightIcon, CheckIcon, Loader2Icon, MinusIcon, SparklesIcon } from "lucide-react";
import RollingNumber from "@/components/ui/RollingNumber";
import type DiscountPreview from "@/lib/api/models/app/subscription/DiscountPreview";
import {
    PLAN_ACCENT_CLASSES,
    getPlan,
    isAtLeast,
    planOrder,
    type PlanID,
} from "@/lib/plans";
import { discountedPrice, fmtMoney, type BillingInterval } from "@/lib/pricing";
import { cn } from "@/lib/utils";

const EASE = [0.22, 1, 0.36, 1] as const;

export default function PlanCard({
    id,
    index,
    current,
    recommended,
    interval,
    discount,
    pending,
    busy,
    canAct = true,
    feature,
    minPlan,
    ctaVerb = "Upgrade to",
    footer,
    onChoose,
}: {
    id: PlanID;
    /** Position in the row; drives the entrance and sweep stagger. */
    index: number;
    current: PlanID;
    /** Spotlight this card with its accent ring and a ribbon. */
    recommended: boolean;
    interval: BillingInterval;
    discount?: DiscountPreview | null;
    /** This card's action is in flight. */
    pending?: boolean;
    /** Any card's action is in flight. */
    busy?: boolean;
    /** False for members who cannot change the plan. */
    canAct?: boolean;
    /** Upgrade dialog only: the locked feature this card is judged against. */
    feature?: string;
    /** Upgrade dialog only: minimum plan that unlocks `feature`. */
    minPlan?: PlanID;
    ctaVerb?: string;
    /** Extra content above the CTA, e.g. a proration preview. */
    footer?: React.ReactNode;
    onChoose: () => void;
}) {
    const reduced = useReducedMotion();
    const plan = getPlan(id);
    const accent = PLAN_ACCENT_CLASSES[plan.accent];
    const gated = !!feature && !!minPlan;
    const unlocks = gated ? isAtLeast(id, minPlan) : true;
    const isCurrent = id === current;
    const below = planOrder(id) < planOrder(current);
    const annual = interval === "annual";
    const base = annual ? plan.priceAnnual : plan.priceMonthly;
    const disc = discountedPrice(base ?? null, discount, interval);
    const shown = disc ?? base;
    const custom = base == null;
    const yearlySaving =
        plan.priceMonthly != null && plan.priceAnnual != null
            ? Math.round((plan.priceMonthly - plan.priceAnnual) * 12)
            : 0;

    // Flipping the interval pulses the price. Mount is covered by the entrance
    // animation below, so skip the first run.
    const priceControls = useAnimationControls();
    const mounted = React.useRef(false);
    React.useEffect(() => {
        if (!mounted.current) {
            mounted.current = true;
            return;
        }
        if (reduced) return;
        priceControls.start({
            scale: [1, 1.05, 1],
            transition: { duration: 0.5, ease: EASE, delay: index * 0.05 },
        });
    }, [interval, reduced, index, priceControls]);

    const sends =
        plan.sendsPerDay === Number.POSITIVE_INFINITY
            ? "Custom volume"
            : `${plan.sendsPerDay.toLocaleString()} emails / day`;

    // A gated card that does not unlock the requested feature must not offer to
    // buy it: checkout would succeed and leave the user still locked out of the
    // thing they clicked.
    const showCta = canAct && !below && !isCurrent && unlocks;

    return (
        <motion.div
            initial={reduced ? false : { opacity: 0, y: 18 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.38, ease: EASE, delay: 0.12 + index * 0.07 }}
            className={cn(
                // `isolate` so the sheen's negative z-index stays above the card
                // background but below the card's own content.
                "relative isolate rounded-xl border bg-white p-5 flex flex-col transition-colors",
                recommended
                    ? cn("border-transparent ring-2 bg-gradient-to-b to-white", accent.ring, accent.soft)
                    : isCurrent
                      ? "border-slate-900"
                      : "border-slate-200 hover:border-slate-300",
                !unlocks && "opacity-70",
            )}
        >
            {/* Sheen that sweeps when the interval flips. Its own clipping
                layer so the ribbon above can still overhang the top edge. */}
            <span className="pointer-events-none absolute inset-0 -z-10 rounded-xl overflow-hidden">
                <AnimatePresence>
                    {!reduced && (
                        <motion.span
                            key={interval}
                            initial={{ x: "-140%" }}
                            animate={{ x: "140%" }}
                            exit={{ opacity: 0 }}
                            transition={{ duration: 0.85, ease: "easeOut", delay: index * 0.06 }}
                            className="absolute inset-y-0 w-2/3 bg-gradient-to-r from-transparent via-slate-900/[0.07] to-transparent"
                        />
                    )}
                </AnimatePresence>
            </span>

            {recommended && (
                <motion.span
                    initial={reduced ? false : { opacity: 0, y: 4 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ duration: 0.3, ease: EASE, delay: 0.4 + index * 0.07 }}
                    className={cn(
                        "absolute -top-2.5 left-4 inline-flex items-center gap-1 h-5 px-2 rounded-full text-[10px] font-semibold uppercase tracking-[0.08em] text-white",
                        accent.button,
                    )}
                >
                    <SparklesIcon className="w-2.5 h-2.5" />
                    {feature ? `Unlocks ${feature}` : "Best value"}
                </motion.span>
            )}

            <div className="flex items-center gap-2">
                <span className={cn("size-2 rounded-full", accent.dot)} />
                <span className="text-[12px] uppercase tracking-[0.1em] font-semibold text-slate-800">
                    {plan.label}
                </span>
                {isCurrent ? (
                    <span className="ml-auto text-[9px] uppercase tracking-[0.08em] font-semibold text-slate-700 bg-slate-100 border border-slate-200 rounded px-1">
                        Current
                    </span>
                ) : plan.featured && !recommended ? (
                    // The ribbon already says it; two badges on one card reads as noise.
                    <span className={cn("ml-auto text-[9px] uppercase tracking-[0.08em] font-semibold border rounded px-1", accent.pill)}>
                        Popular
                    </span>
                ) : null}
            </div>
            <p className="mt-1.5 text-[12px] text-slate-500 leading-snug min-h-[32px]">{plan.description}</p>

            {/* Price */}
            <motion.div
                animate={priceControls}
                style={{ transformOrigin: "left bottom" }}
                className="mt-4 flex items-baseline gap-1.5"
            >
                {custom ? (
                    <span className="text-[30px] font-semibold tracking-[-0.03em] text-slate-900">Custom</span>
                ) : (
                    <>
                        <span
                            className={cn(
                                "text-[32px] font-semibold tracking-[-0.03em] tabular-nums leading-none inline-flex items-end",
                                disc != null ? "text-emerald-700" : "text-slate-900",
                            )}
                        >
                            $
                            <RollingNumber
                                value={shown as number}
                                format={(n) => fmtMoney(Math.round(n * 100) / 100)}
                            />
                        </span>
                        <span className="text-[12px] text-slate-500">/ mo</span>
                        {disc != null && (
                            <span className="text-[12px] text-slate-400 line-through tabular-nums">
                                ${fmtMoney(base as number)}
                            </span>
                        )}
                    </>
                )}
            </motion.div>
            <div className="relative mt-1 h-4 overflow-hidden">
                <AnimatePresence initial={false} mode="popLayout">
                    <motion.div
                        key={interval}
                        initial={reduced ? { opacity: 0 } : { opacity: 0, y: 12 }}
                        animate={{ opacity: 1, y: 0 }}
                        exit={reduced ? { opacity: 0 } : { opacity: 0, y: -12 }}
                        transition={{ duration: 0.28, ease: EASE, delay: index * 0.04 }}
                        className="absolute inset-0 flex items-center gap-1.5 text-[11px] text-slate-400 tabular-nums"
                    >
                        {custom ? (
                            "tailored to your volume"
                        ) : annual ? (
                            <>
                                <span>billed annually</span>
                                {yearlySaving > 0 && (
                                    <span className="inline-flex items-center h-4 px-1.5 rounded-full bg-emerald-50 text-emerald-700 border border-emerald-100 text-[10px] font-semibold">
                                        save ${yearlySaving} / yr
                                    </span>
                                )}
                            </>
                        ) : (
                            "billed monthly"
                        )}
                    </motion.div>
                </AnimatePresence>
            </div>

            {/* Headline limit */}
            <div className="mt-4 rounded-md bg-slate-50 border border-slate-200/70 px-2.5 py-2 text-[12px] font-medium text-slate-800">
                {sends}
            </div>

            <ul className="mt-3 space-y-1.5 flex-1">
                {plan.bullets.map((b) => (
                    <li key={b} className="flex items-start gap-1.5 text-[12px] text-slate-700 leading-snug">
                        <CheckIcon className="w-3 h-3 text-emerald-600 mt-0.5 shrink-0" />
                        <span>{b}</span>
                    </li>
                ))}
                {gated && (
                    <li
                        className={cn(
                            "flex items-start gap-1.5 text-[12px] leading-snug pt-1.5 mt-1.5 border-t border-slate-200/70",
                            unlocks ? "text-slate-900 font-medium" : "text-slate-400",
                        )}
                    >
                        {unlocks ? (
                            <CheckIcon className="w-3 h-3 text-emerald-600 mt-0.5 shrink-0" />
                        ) : (
                            <MinusIcon className="w-3 h-3 text-slate-300 mt-0.5 shrink-0" />
                        )}
                        <span>{unlocks ? `Includes ${feature}` : `Does not include ${feature}`}</span>
                    </li>
                )}
            </ul>

            {footer && <div className="mt-3">{footer}</div>}

            {/* CTA */}
            <div className="mt-4">
                {isCurrent ? (
                    <div className="h-9 rounded-md bg-slate-100 text-slate-400 text-[12.5px] font-medium inline-flex items-center justify-center w-full cursor-default">
                        Current plan
                    </div>
                ) : gated && !unlocks ? (
                    <div className="h-9 text-[11.5px] text-slate-400 inline-flex items-center justify-center w-full text-center px-2">
                        Does not unlock {feature}
                    </div>
                ) : below && !canAct ? (
                    <div className="h-9 text-[11.5px] text-slate-400 inline-flex items-center justify-center w-full">
                        Owner can change the plan
                    </div>
                ) : below ? (
                    <button
                        type="button"
                        onClick={onChoose}
                        disabled={busy}
                        className="h-9 w-full rounded-md border border-slate-200 hover:border-slate-300 text-[12.5px] font-medium text-slate-600 hover:text-slate-900 transition-colors disabled:opacity-60"
                    >
                        {pending ? "Working…" : `Downgrade to ${plan.label}`}
                    </button>
                ) : showCta ? (
                    <button
                        type="button"
                        onClick={onChoose}
                        disabled={busy}
                        className={cn(
                            "h-9 w-full rounded-md text-[12.5px] font-medium inline-flex items-center justify-center gap-1.5 transition-colors disabled:opacity-60",
                            id === "enterprise"
                                ? "border border-slate-200 hover:border-slate-300 bg-white text-slate-800"
                                : recommended
                                  ? accent.button
                                  : "bg-slate-900 hover:bg-slate-800 text-white",
                        )}
                    >
                        {pending ? (
                            <>
                                <Loader2Icon className="w-3.5 h-3.5 animate-spin" />
                                {id === "enterprise" ? "Opening…" : "Redirecting…"}
                            </>
                        ) : id === "enterprise" ? (
                            <>Talk to sales</>
                        ) : (
                            <>
                                {ctaVerb} {plan.label}
                                <ArrowRightIcon className="w-3.5 h-3.5" />
                            </>
                        )}
                    </button>
                ) : (
                    <div className="h-9 text-[11.5px] text-slate-400 inline-flex items-center justify-center w-full">
                        Owner can upgrade
                    </div>
                )}
            </div>
        </motion.div>
    );
}
