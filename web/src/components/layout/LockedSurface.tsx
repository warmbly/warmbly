// LockedSurface — render a feature page through a frosted-glass
// overlay when the org doesn't have the subscription tier.
//
// The page contents are still rendered behind the lock at reduced
// opacity so the user gets a preview of what they'd unlock. The
// overlay sits absolute with a backdrop-blur and a centered upgrade
// card that shows: the feature name, the minimum plan that unlocks
// it, that plan's price, and the bullets from `lib/plans` so the
// user sees what they'd be paying for at the same time.
//
// Use it like:
//
//   <LockedSurface
//     locked={!access.hasInbox}
//     feature="Unified inbox"
//     blurb="See every reply across every connected mailbox in one place."
//     minPlan="starter"
//     bullets={["Search by sender, subject, account, date range, and tag"]}
//   >
//     <RealInbox />
//   </LockedSurface>

import React from "react";
import { Link } from "react-router-dom";
import { CheckIcon, LockIcon, SparklesIcon } from "lucide-react";
import { useAppStore } from "@/stores";
import { PLAN_ACCENT_CLASSES, getPlan, type PlanID } from "@/lib/plans";

interface Props {
    locked: boolean;
    feature: string;
    blurb: string;
    /** Minimum plan that unlocks this feature. Drives the badge,
     *  price, color and the bullets shown in the card. */
    minPlan?: PlanID;
    children: React.ReactNode;
    /** Where the upgrade button routes — defaults to /app/settings/billing. */
    upgradeTo?: string;
    /** Optional override bullets — used when the feature has specifics
     *  beyond the plan's standard inclusion list. Falls back to the
     *  plan's own bullets when omitted. */
    bullets?: string[];
}

export function LockedSurface({
    locked,
    feature,
    blurb,
    minPlan = "starter",
    children,
    upgradeTo = "/app/settings/billing",
    bullets,
}: Props) {
    const isOwner = useAppStore((s) => s.currentOrganization?.role === "owner");
    const plan = getPlan(minPlan);
    const accent = PLAN_ACCENT_CLASSES[plan.accent];

    if (!locked) return <>{children}</>;

    const featureBullets = bullets ?? plan.bullets;
    const priceLabel = plan.priceMonthly == null
        ? "Custom pricing"
        : `from $${plan.priceMonthly}/mo`;

    return (
        <div className="relative h-full">
            {/* Preview layer — the real page rendered as a teaser. */}
            <div
                aria-hidden
                className="absolute inset-0 overflow-hidden pointer-events-none select-none opacity-40"
            >
                {children}
            </div>

            {/* Frosted overlay + centered upgrade card. */}
            <div className="absolute inset-0 bg-gradient-to-b from-white/50 via-white/70 to-white/90 backdrop-blur-[6px] flex items-center justify-center px-4">
                <div className="w-full max-w-[460px] rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.12),0_8px_16px_-8px_rgba(15,23,42,0.06)] overflow-hidden">
                    {/* Header row */}
                    <div className="h-12 px-4 border-b border-slate-200 flex items-center gap-2.5">
                        <div className="size-5 rounded bg-amber-50 text-amber-600 flex items-center justify-center">
                            <LockIcon className="w-3 h-3" />
                        </div>
                        <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                            Locked
                        </span>
                        <div className="h-4 w-px bg-slate-200" />
                        <span className="text-[12.5px] text-slate-900 font-medium truncate">
                            {feature}
                        </span>
                        <span
                            className={`ml-auto inline-flex items-center gap-1 text-[10px] uppercase tracking-[0.08em] font-semibold border rounded px-1.5 py-0.5 ${accent.pill}`}
                        >
                            <span className={`size-1 rounded-full ${accent.dot}`} />
                            {plan.label}
                        </span>
                    </div>

                    {/* Body */}
                    <div className="px-4 py-4">
                        <p className="text-[13px] text-slate-700 leading-relaxed mb-3">
                            {blurb}
                        </p>
                        {featureBullets && featureBullets.length > 0 && (
                            <ul className="space-y-1.5 mb-3">
                                {featureBullets.map((b) => (
                                    <li
                                        key={b}
                                        className="flex items-start gap-2 text-[12px] text-slate-700 leading-snug"
                                    >
                                        <CheckIcon className="w-3 h-3 text-emerald-600 mt-0.5 shrink-0" />
                                        <span>{b}</span>
                                    </li>
                                ))}
                            </ul>
                        )}
                        <div className="text-[11px] text-slate-500 font-mono tabular-nums border-t border-slate-200/60 pt-2 mt-2">
                            {plan.label} · {priceLabel}
                        </div>
                    </div>

                    {/* Footer CTA */}
                    <div className="px-3 h-12 border-t border-slate-200 flex items-center gap-1.5">
                        {isOwner ? (
                            <>
                                <span className="text-[11px] text-slate-400">
                                    Upgrade unlocks it instantly
                                </span>
                                <Link
                                    to={upgradeTo}
                                    className="ml-auto h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors"
                                >
                                    <SparklesIcon className="w-3 h-3" />
                                    Upgrade to {plan.label}
                                </Link>
                            </>
                        ) : (
                            <span className="text-[11.5px] text-slate-500">
                                Ask your workspace owner to upgrade to{" "}
                                <span className="font-medium text-slate-900">{plan.label}</span>.
                            </span>
                        )}
                    </div>
                </div>
            </div>
        </div>
    );
}
