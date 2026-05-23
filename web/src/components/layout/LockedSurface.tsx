// LockedSurface — render a feature page through a frosted-glass
// overlay when the org doesn't have the subscription tier.
//
// The page contents are still rendered behind the lock at reduced
// opacity so the user gets a preview of what they'd unlock. The
// overlay sits absolute with a backdrop-blur, centered upgrade card
// with feature name, blurb, and a slate-900 CTA.
//
// Use it like:
//
//   const access = useFeatureAccess();
//   <LockedSurface
//     locked={!access.hasInbox}
//     feature="Unified inbox"
//     blurb="See every reply across every connected mailbox in one place."
//     plan="Pro"
//   >
//     <RealInbox />
//   </LockedSurface>

import React from "react";
import { Link } from "react-router-dom";
import { LockIcon, SparklesIcon } from "lucide-react";
import { useAppStore } from "@/stores";

interface Props {
    locked: boolean;
    feature: string;
    blurb: string;
    plan?: string;
    children: React.ReactNode;
    /** Where the upgrade button routes — defaults to /app/billing. */
    upgradeTo?: string;
    /** Bullets shown under the blurb. Each short, one line max. */
    bullets?: string[];
}

export function LockedSurface({
    locked,
    feature,
    blurb,
    plan = "Pro",
    children,
    upgradeTo = "/app/billing",
    bullets,
}: Props) {
    const isOwner = useAppStore((s) => s.currentOrganization?.role === "owner");

    if (!locked) return <>{children}</>;

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
                <div className="w-full max-w-[440px] rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.12),0_8px_16px_-8px_rgba(15,23,42,0.06)] overflow-hidden">
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
                        <span className="ml-auto text-[10px] uppercase tracking-[0.1em] font-semibold text-sky-700 bg-sky-50 border border-sky-100 rounded px-1.5 py-0.5">
                            {plan}
                        </span>
                    </div>

                    <div className="px-4 py-4">
                        <p className="text-[13px] text-slate-700 leading-relaxed mb-3">
                            {blurb}
                        </p>
                        {bullets && bullets.length > 0 && (
                            <ul className="space-y-1.5 mb-4">
                                {bullets.map((b) => (
                                    <li
                                        key={b}
                                        className="flex items-start gap-2 text-[12px] text-slate-700 leading-snug"
                                    >
                                        <span className="size-1 rounded-full bg-slate-400 mt-1.5 shrink-0" />
                                        <span>{b}</span>
                                    </li>
                                ))}
                            </ul>
                        )}
                    </div>

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
                                    Upgrade to {plan}
                                </Link>
                            </>
                        ) : (
                            <span className="text-[11.5px] text-slate-500">
                                Ask your workspace owner to upgrade to <span className="font-medium text-slate-900">{plan}</span>.
                            </span>
                        )}
                    </div>
                </div>
            </div>
        </div>
    );
}
