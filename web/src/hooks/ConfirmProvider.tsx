// Above page dialogs, below the identity check an awaited action may need.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { AlertTriangleIcon, CheckIcon, Loader2Icon, XIcon } from "lucide-react";
import { cn } from "@/lib/utils";
import { ConfirmContext, type ConfirmOptions } from "./context/confirm";

export default function ConfirmProvider({ children }: { children: React.ReactNode }) {
    const [visible, setVisible] = React.useState<boolean>(false);
    const [loading, setLoading] = React.useState<boolean>(false);
    const submitRef = React.useRef<() => void | Promise<void>>(null);
    const cancelRef = React.useRef<(() => void) | null>(null);
    const [text, setText] = React.useState<string>("");
    const [options, setOptions] = React.useState<ConfirmOptions>({});
    const primary = options.tone === "primary";

    const show = React.useCallback(
        (text: string, onSubmit: () => void | Promise<void>, onCancel?: () => void, opts?: ConfirmOptions) => {
            setText(text);
            setOptions(opts ?? {});
            submitRef.current = onSubmit;
            cancelRef.current = onCancel ?? null;
            setVisible(true);
        },
        [],
    );

    // Every way of closing without confirming: Escape, the backdrop, the X and Cancel.
    const dismiss = React.useCallback(() => {
        if (loading) return;
        setVisible(false);
        const cancel = cancelRef.current;
        cancelRef.current = null;
        cancel?.();
    }, [loading]);

    const onConfirm = async () => {
        if (loading || !submitRef.current) return;
        try {
            setLoading(true);
            await submitRef.current();
            // Close on success. (Previously the dialog stayed open after a
            // successful confirm unless the action happened to navigate away,
            // so on many pages the popup never disappeared.)
            setVisible(false);
            submitRef.current = null;
            cancelRef.current = null;
        } catch {
            // Keep the dialog open on failure so the user can retry or cancel;
            // the caller surfaces the error (toast). Loading is cleared below.
        } finally {
            setLoading(false);
        }
    };

    React.useEffect(() => {
        if (!visible) return;
        const onKey = (e: KeyboardEvent) => {
            if (e.key === "Escape") dismiss();
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [visible, dismiss]);

    return (
        <ConfirmContext.Provider value={{ show, setShow: setVisible, setLoading }}>
            {children}
            <AnimatePresence>
                {visible && (
                    <motion.div
                        key="overlay"
                        initial={{ opacity: 0 }}
                        animate={{ opacity: 1 }}
                        exit={{ opacity: 0 }}
                        transition={{ duration: 0.15 }}
                        onClick={dismiss}
                        className="fixed inset-0 z-[200] flex items-center justify-center bg-slate-900/30 backdrop-blur-[2px] px-4"
                    >
                        <motion.div
                            key="card"
                            initial={{ y: 8, opacity: 0 }}
                            animate={{ y: 0, opacity: 1 }}
                            exit={{ y: 8, opacity: 0 }}
                            transition={{ duration: 0.16 }}
                            onClick={(e) => e.stopPropagation()}
                            role="alertdialog"
                            aria-modal="true"
                            className="w-full max-w-[400px] rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18),0_8px_16px_-8px_rgba(15,23,42,0.1)] overflow-hidden"
                        >
                            <div className="h-12 px-4 border-b border-slate-200 flex items-center gap-2.5">
                                <div className={cn("size-5 rounded flex items-center justify-center", primary ? "bg-sky-50 text-sky-600" : "bg-red-50 text-red-600")}>
                                    {primary ? <CheckIcon className="w-3 h-3" /> : <AlertTriangleIcon className="w-3 h-3" />}
                                </div>
                                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                                    Confirm
                                </span>
                                <div className="h-4 w-px bg-slate-200" />
                                <span className="text-[12.5px] text-slate-900 font-medium truncate">
                                    {options.title ?? "Are you sure?"}
                                </span>
                                <button
                                    type="button"
                                    onClick={dismiss}
                                    disabled={loading}
                                    aria-label="Close"
                                    className="ml-auto size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors disabled:opacity-50"
                                >
                                    <XIcon className="w-3.5 h-3.5" />
                                </button>
                            </div>

                            <div className="px-4 py-4">
                                <p className="text-[12.5px] text-slate-700 leading-relaxed">{text}</p>
                            </div>

                            <div className="px-3 h-12 border-t border-slate-200 flex items-center gap-1.5">
                                <button
                                    type="button"
                                    onClick={dismiss}
                                    disabled={loading}
                                    className="ml-auto h-7 px-2.5 rounded-md text-[12px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 transition-colors disabled:opacity-50"
                                >
                                    Cancel
                                </button>
                                <button
                                    type="button"
                                    onClick={onConfirm}
                                    disabled={loading}
                                    className={cn(
                                        "h-7 px-2.5 rounded-md text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60",
                                        primary ? "bg-sky-600 hover:bg-sky-700" : "bg-red-600 hover:bg-red-700",
                                    )}
                                >
                                    {loading && <Loader2Icon className="w-3 h-3 animate-spin" />}
                                    {options.confirmLabel ?? "Confirm"}
                                </button>
                            </div>
                        </motion.div>
                    </motion.div>
                )}
            </AnimatePresence>
        </ConfirmContext.Provider>
    );
}
