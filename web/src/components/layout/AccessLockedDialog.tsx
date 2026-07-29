// Popup shown when a member clicks a sidebar item they don't have access to.
// Two variants, same lock affordance:
//   - "permission": the member's role lacks the required permission.
//   - "plan": the feature needs a higher plan; offers an upgrade path.
// Either way it explains the lock instead of silently doing nothing or routing
// to a misleadingly-empty page.

import { createPortal } from "react-dom";
import { AnimatePresence, motion } from "framer-motion";
import { LockIcon, XIcon, SparklesIcon } from "lucide-react";
import { Link } from "react-router-dom";

export default function AccessLockedDialog({
    open,
    onClose,
    feature,
    variant = "permission",
    permissionLabel = "the required",
    planLabel,
    upgradeTo,
    canUpgrade = true,
}: {
    open: boolean;
    onClose: () => void;
    feature: string;
    /** "permission" = role lacks a permission; "plan" = needs a higher plan. */
    variant?: "permission" | "plan";
    /** Permission variant: the friendly permission name they're missing. */
    permissionLabel?: string;
    /** Plan variant: the plan that unlocks the feature (e.g. "Starter"). */
    planLabel?: string;
    /** Plan variant: where the upgrade button routes (billing / roles). */
    upgradeTo?: string;
    /** Plan variant: whether this member can actually upgrade (owner/billing). */
    canUpgrade?: boolean;
}) {
    const isPlan = variant === "plan";
    // Portal to <body>: this dialog renders inside the sidebar <aside>, which has
    // a transform (slide-in) + overflow, so a `fixed` overlay would clip to the
    // sidebar instead of the viewport. The portal escapes that containing block.
    return createPortal(
        <AnimatePresence>
            {open && (
                <motion.div
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    className="fixed inset-0 z-[60] bg-slate-900/30 flex items-center justify-center p-4"
                    onMouseDown={(e) => {
                        if (e.target === e.currentTarget) onClose();
                    }}
                >
                    <motion.div
                        initial={{ opacity: 0, y: 8, scale: 0.98 }}
                        animate={{ opacity: 1, y: 0, scale: 1 }}
                        exit={{ opacity: 0, y: 8, scale: 0.98 }}
                        className="w-full max-w-sm rounded-lg bg-white border border-slate-200 shadow-xl p-5 text-center"
                    >
                        <div className="flex justify-end -mt-1 -mr-1">
                            <button
                                type="button"
                                onClick={onClose}
                                aria-label="Close"
                                className="size-7 rounded-md text-slate-400 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center"
                            >
                                <XIcon className="w-4 h-4" />
                            </button>
                        </div>
                        <div
                            className={
                                isPlan
                                    ? "mx-auto mb-3 size-11 rounded-xl bg-indigo-50 border border-indigo-200 text-indigo-600 flex items-center justify-center"
                                    : "mx-auto mb-3 size-11 rounded-xl bg-amber-50 border border-amber-200 text-amber-600 flex items-center justify-center"
                            }
                        >
                            {isPlan ? <SparklesIcon className="w-5 h-5" /> : <LockIcon className="w-5 h-5" />}
                        </div>
                        <h3 className="text-[14px] font-semibold text-slate-900">
                            You don't have access to {feature}
                        </h3>

                        {isPlan ? (
                            <>
                                <p className="text-[12.5px] text-slate-500 leading-relaxed mt-1.5">
                                    {feature} is on the{" "}
                                    <span className="font-medium text-slate-700">{planLabel ?? "a higher"}</span> plan.
                                    {canUpgrade
                                        ? " Upgrade to turn it on for your whole workspace."
                                        : " Ask the workspace owner to upgrade to turn it on."}
                                </p>
                                <div className="mt-4 flex items-center justify-center gap-2">
                                    {canUpgrade && upgradeTo ? (
                                        <Link
                                            to={upgradeTo}
                                            onClick={onClose}
                                            className="inline-flex items-center gap-1.5 h-8 px-4 rounded-md bg-indigo-600 hover:bg-indigo-700 text-white text-[12.5px] font-medium transition-colors"
                                        >
                                            <SparklesIcon className="w-3.5 h-3.5" /> Upgrade
                                        </Link>
                                    ) : null}
                                    <button
                                        type="button"
                                        onClick={onClose}
                                        className="inline-flex items-center h-8 px-4 rounded-md border border-slate-200 hover:bg-slate-100 text-slate-700 text-[12.5px] font-medium transition-colors"
                                    >
                                        Not now
                                    </button>
                                </div>
                            </>
                        ) : (
                            <>
                                <p className="text-[12.5px] text-slate-500 leading-relaxed mt-1.5">
                                    Your role in this workspace doesn't include the{" "}
                                    <span className="font-medium text-slate-700">{permissionLabel}</span> permission. Ask a
                                    workspace admin or the owner to grant it from Settings → Roles &amp; access.
                                </p>
                                <button
                                    type="button"
                                    onClick={onClose}
                                    className="mt-4 inline-flex items-center h-8 px-4 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12.5px] font-medium transition-colors"
                                >
                                    Got it
                                </button>
                            </>
                        )}
                    </motion.div>
                </motion.div>
            )}
        </AnimatePresence>,
        document.body,
    );
}
