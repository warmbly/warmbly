import { AnimatePresence, motion } from "framer-motion";
import { XIcon } from "lucide-react";
import { useEffect } from "react";

// The card every lead finder dialog sits in; closes on backdrop and Escape.
export default function LeadDialog({
    open,
    onClose,
    icon,
    eyebrow,
    title,
    children,
    footer,
    width = 520,
}: {
    open: boolean;
    onClose: () => void;
    icon: React.ReactNode;
    eyebrow: string;
    title: string;
    children: React.ReactNode;
    footer: React.ReactNode;
    width?: number;
}) {
    useEffect(() => {
        if (!open) return;
        const key = (e: KeyboardEvent) => {
            if (e.key !== "Escape") return;
            if (document.querySelector("[data-floating]") || document.querySelector('[role="alertdialog"]')) return;
            onClose();
        };
        window.addEventListener("keydown", key);
        return () => window.removeEventListener("keydown", key);
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
                    className="fixed inset-0 z-[120] flex items-center justify-center bg-slate-900/30 backdrop-blur-[2px] px-4"
                >
                    <motion.div
                        key="card"
                        role="dialog"
                        aria-modal="true"
                        aria-label={title}
                        initial={{ y: 8, opacity: 0, scale: 0.985 }}
                        animate={{ y: 0, opacity: 1, scale: 1 }}
                        exit={{ y: 8, opacity: 0, scale: 0.985 }}
                        transition={{ duration: 0.18, ease: [0.22, 1, 0.36, 1] }}
                        onMouseDown={(e) => e.stopPropagation()}
                        style={{ maxWidth: width }}
                        className="w-full rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18),0_8px_16px_-8px_rgba(15,23,42,0.1)] overflow-hidden flex flex-col max-h-[85dvh]"
                    >
                        <header className="h-12 px-4 border-b border-slate-200 flex items-center gap-2.5 shrink-0">
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
                        </header>
                        <div className="flex-1 min-h-0 overflow-y-auto">{children}</div>
                        <footer className="px-4 py-3 border-t border-slate-200 flex items-center justify-end gap-2 shrink-0 bg-slate-50/60">{footer}</footer>
                    </motion.div>
                </motion.div>
            )}
        </AnimatePresence>
    );
}
