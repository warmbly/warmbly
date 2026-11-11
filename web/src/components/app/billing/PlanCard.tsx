// The plan card, shared by the plan chooser dialog and the Billing > Plans tab
// so the two never drift.
//
// One accent for every plan: the recommended card carries the ring and the
// filled button, the rest stay neutral, so the eye lands on one choice.
//
// `feature` is only passed by the dialog when a locked feature opened it, where
// the card has to say whether that plan unlocks the thing the user just hit.

import React from "react";
import { AnimatePresence, motion, useReducedMotion } from "framer-motion";
import { ArrowRightIcon, CheckIcon, Loader2Icon, MinusIcon } from "lucide-react";
import RollingNumber from "@/components/ui/RollingNumber";
import type DiscountPreview from "@/lib/api/models/app/subscription/DiscountPreview";
import { getPlan, isAtLeast, planOrder, type PlanID } from "@/lib/plans";
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
    /** Position in the row; drives the entrance stagger. */
    index: number;
    current: PlanID;
    /** The one card the eye should land on: accent ring, chip and filled button. */
    recommended: boolean;
    interval: BillingInterval;
    discount?: DiscountPreview | null;
    /** This card's action is in flight. */
    pending?: boolean;
    /** Any card's action is in flight. */
    busy?: boolean;
    /** False for members who cannot change the plan. */
    canAct?: boolean;
    /** Plan chooser only: the locked feature this card is judged against. */
    feature?: string;
    /** Plan chooser only: minimum plan that unlocks `feature`. */
    minPlan?: PlanID;
    ctaVerb?: string;
    /** Extra content above the CTA, e.g. a proration preview. */
    footer?: React.ReactNode;
    onChoose: () => void;
}) {
    const reduced = useReducedMotion();
    const plan = getPlan(id);
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

    const sends =
        plan.sendsPerDay === Number.POSITIVE_INFINITY
            ? "Custom sending volume"
            : `${plan.sendsPerDay.toLocaleString()} emails a day`;

    // A gated card that does not unlock the requested feature must not offer to
    // buy it: checkout would succeed and leave the user still locked out of the
    // thing they clicked.
    const showCta = canAct && !below && !isCurrent && unlocks;
    const chip = isCurrent ? "Current plan" : recommended ? "Recommended" : plan.featured ? "Most popular" : null;

    return (
        <motion.div
            initial={reduced ? false : { opacity: 0, y: 12 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.3, ease: EASE, delay: 0.05 + index * 0.05 }}
            className={cn(
                "relative rounded-xl border bg-white p-5 flex flex-col transition-colors",
                recommended
                    ? "border-sky-500 ring-1 ring-sky-500 shadow-[0_8px_24px_-12px_rgba(2,132,199,0.35)]"
                    : "border-slate-200 hover:border-slate-300",
                !unlocks && "opacity-60",
            )}
        >
            <div className="flex items-center gap-2 min-h-[20px]">
                <span className="text-[14px] font-semibold text-slate-900">{plan.label}</span>
                {chip && (
                    <span
                        className={cn(
                            "ml-auto h-5 px-1.5 rounded-md text-[10.5px] font-medium inline-flex items-center",
                            recommended && !isCurrent ? "bg-sky-50 text-sky-700" : "bg-slate-100 text-slate-600",
                        )}
                    >
                        {chip}
                    </span>
                )}
            </div>
            <p className="mt-1 text-[12.5px] text-slate-500 leading-snug min-h-[36px]">{plan.description}</p>

            {/* Price */}
            <div className="mt-4 flex items-baseline gap-1.5">
                {custom ? (
                    <span className="text-[32px] font-semibold tracking-[-0.03em] leading-none text-slate-900">Custom</span>
                ) : (
                    <>
                        <span className="text-[32px] font-semibold tracking-[-0.03em] tabular-nums leading-none text-slate-900 inline-flex items-end">
                            $
                            <RollingNumber
                                value={shown as number}
                                format={(n) => fmtMoney(Math.round(n * 100) / 100)}
                            />
                        </span>
                        <span className="text-[12.5px] text-slate-500">/ month</span>
                        {disc != null && (
                            <span className="text-[12.5px] text-slate-400 line-through tabular-nums">
                                ${fmtMoney(base as number)}
                            </span>
                        )}
                    </>
                )}
            </div>
            <div className="relative mt-1.5 h-[18px] overflow-hidden">
                <AnimatePresence initial={false} mode="popLayout">
                    <motion.div
                        key={interval}
                        initial={reduced ? { opacity: 0 } : { opacity: 0, y: 10 }}
                        animate={{ opacity: 1, y: 0 }}
                        exit={reduced ? { opacity: 0 } : { opacity: 0, y: -10 }}
                        transition={{ duration: 0.22, ease: EASE }}
                        className="absolute inset-0 text-[12px] text-slate-500 tabular-nums"
                    >
                        {custom ? (
                            "Sized to your volume"
                        ) : annual ? (
                            <>
                                Billed yearly
                                {yearlySaving > 0 && <span className="text-emerald-700 font-medium"> · save ${yearlySaving}</span>}
                            </>
                        ) : (
                            "Billed monthly"
                        )}
                    </motion.div>
                </AnimatePresence>
            </div>

            <div className="mt-4 pt-4 border-t border-slate-100 flex-1">
                <div className="text-[12.5px] font-medium text-slate-900">{sends}</div>
                <ul className="mt-2.5 space-y-1.5">
                    {plan.bullets.map((b) => (
                        <li key={b} className="flex items-start gap-2 text-[12.5px] text-slate-600 leading-snug">
                            <CheckIcon className="w-3.5 h-3.5 text-sky-600 mt-px shrink-0" />
                            <span>{b}</span>
                        </li>
                    ))}
                    {gated && (
                        <li
                            className={cn(
                                "flex items-start gap-2 text-[12.5px] leading-snug",
                                unlocks ? "text-slate-900 font-medium" : "text-slate-400",
                            )}
                        >
                            {unlocks ? (
                                <CheckIcon className="w-3.5 h-3.5 text-sky-600 mt-px shrink-0" />
                            ) : (
                                <MinusIcon className="w-3.5 h-3.5 text-slate-300 mt-px shrink-0" />
                            )}
                            <span>{unlocks ? `Includes ${feature}` : `Does not include ${feature}`}</span>
                        </li>
                    )}
                </ul>
            </div>

            {footer && <div className="mt-3">{footer}</div>}

            {/* CTA */}
            <div className="mt-5">
                {isCurrent ? (
                    <div className="h-9 rounded-md border border-slate-200 bg-slate-50 text-slate-500 text-[12.5px] font-medium inline-flex items-center justify-center w-full cursor-default">
                        Your current plan
                    </div>
                ) : gated && !unlocks ? (
                    <div className="h-9 text-[12px] text-slate-400 inline-flex items-center justify-center w-full text-center px-2">
                        Does not unlock {feature}
                    </div>
                ) : below && !canAct ? (
                    <div className="h-9 text-[12px] text-slate-400 inline-flex items-center justify-center w-full">
                        The owner can change the plan
                    </div>
                ) : below ? (
                    <button
                        type="button"
                        onClick={onChoose}
                        disabled={busy}
                        className="h-9 w-full rounded-md border border-slate-200 hover:border-slate-300 bg-white text-[12.5px] font-medium text-slate-700 hover:text-slate-900 transition-colors disabled:opacity-60"
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
                            recommended && id !== "enterprise"
                                ? "bg-sky-600 hover:bg-sky-700 text-white"
                                : "border border-slate-200 hover:border-slate-300 bg-white text-slate-800 hover:text-slate-900",
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
                    <div className="h-9 text-[12px] text-slate-400 inline-flex items-center justify-center w-full">
                        The owner can upgrade
                    </div>
                )}
            </div>
        </motion.div>
    );
}
