// Shared chrome for the agency dialogs: closes on Escape and on the backdrop.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { XIcon } from "lucide-react";

export function AgencyDialog({
    open,
    onClose,
    eyebrow,
    title,
    icon,
    children,
    footer,
}: {
    open: boolean;
    onClose: () => void;
    eyebrow: string;
    title: string;
    icon: React.ReactNode;
    children: React.ReactNode;
    footer: React.ReactNode;
}) {
    React.useEffect(() => {
        if (!open) return;
        const onKey = (e: KeyboardEvent) => {
            if (e.key !== "Escape") return;
            if (document.querySelector('[data-floating], [role="alertdialog"]')) return;
            e.stopPropagation();
            onClose();
        };
        window.addEventListener("keydown", onKey);
        return () => window.removeEventListener("keydown", onKey);
    }, [open, onClose]);

    return (
        <AnimatePresence>
            {open && (
                <motion.div
                    key="overlay"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.15 }}
                    onMouseDown={onClose}
                    className="fixed inset-0 z-[110] flex items-center justify-center bg-slate-900/30 backdrop-blur-[2px] px-4"
                >
                    <motion.div
                        key="card"
                        role="dialog"
                        aria-modal="true"
                        aria-label={title}
                        initial={{ y: 8, opacity: 0 }}
                        animate={{ y: 0, opacity: 1 }}
                        exit={{ y: 8, opacity: 0 }}
                        transition={{ duration: 0.16 }}
                        onMouseDown={(e) => e.stopPropagation()}
                        className="w-full max-w-[460px] rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18),0_8px_16px_-8px_rgba(15,23,42,0.1)] overflow-hidden"
                    >
                        <div className="h-12 px-4 border-b border-slate-200 flex items-center gap-2.5">
                            <div className="size-5 rounded bg-slate-100 text-slate-600 flex items-center justify-center">{icon}</div>
                            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">{eyebrow}</span>
                            <div className="h-4 w-px bg-slate-200" />
                            <span className="text-[12.5px] text-slate-900 font-medium truncate">{title}</span>
                            <button
                                type="button"
                                onClick={onClose}
                                aria-label="Close"
                                className="ml-auto size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                            >
                                <XIcon className="w-3.5 h-3.5" />
                            </button>
                        </div>
                        {children}
                        <div className="px-3 h-12 border-t border-slate-200 flex items-center gap-1.5">{footer}</div>
                    </motion.div>
                </motion.div>
            )}
        </AnimatePresence>
    );
}
