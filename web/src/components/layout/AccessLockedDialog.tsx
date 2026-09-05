// Popup shown when a member clicks a sidebar item their role cannot access.
// It explains the missing permission instead of silently doing nothing or
// routing to a misleadingly-empty page. Plan locks do not come here: they
// open the full-screen UpgradeDialog through useUpgradeDialog().

import { createPortal } from "react-dom";
import { AnimatePresence, motion } from "framer-motion";
import { LockIcon, XIcon } from "lucide-react";

export default function AccessLockedDialog({
    open,
    onClose,
    feature,
    permissionLabel = "the required",
}: {
    open: boolean;
    onClose: () => void;
    feature: string;
    /** The friendly permission name they're missing. */
    permissionLabel?: string;
}) {
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
                        <div className="mx-auto mb-3 size-11 rounded-xl bg-amber-50 border border-amber-200 text-amber-600 flex items-center justify-center">
                            <LockIcon className="w-5 h-5" />
                        </div>
                        <h3 className="text-[14px] font-semibold text-slate-900">
                            You don't have access to {feature}
                        </h3>
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
                    </motion.div>
                </motion.div>
            )}
        </AnimatePresence>,
        document.body,
    );
}
