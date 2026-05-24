// Confirm provider — brae-density modal.
//
// Sits at z-[200] so it stacks above every other dialog in the app
// (folder/tag list modal is z-[110], page-level overlays are z-[100]).
// Without this, deleting an item from inside the folders modal would
// pop the confirm BEHIND the modal — invisible, and the click would
// land on the folders backdrop and dismiss it.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { AlertTriangleIcon, Loader2Icon, XIcon } from "lucide-react";
import { ConfirmContext } from "./context/confirm";

export default function ConfirmProvider({ children }: { children: React.ReactNode }) {
    const [visible, setVisible] = React.useState<boolean>(false);
    const [loading, setLoading] = React.useState<boolean>(false);
    const submitRef = React.useRef<() => void | Promise<void>>(null);
    const [text, setText] = React.useState<string>("");

    const show = React.useCallback(
        (text: string, onSubmit: () => void | Promise<void>) => {
            setText(text);
            submitRef.current = onSubmit;
            setVisible(true);
        },
        [],
    );

    const onConfirm = async () => {
        if (loading || !submitRef.current) return;
        try {
            setLoading(true);
            await submitRef.current();
        } finally {
            setLoading(false);
        }
    };

    React.useEffect(() => {
        if (!visible) return;
        const onKey = (e: KeyboardEvent) => {
            if (e.key === "Escape" && !loading) setVisible(false);
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [visible, loading]);

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
                        onClick={() => !loading && setVisible(false)}
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
                                <div className="size-5 rounded bg-red-50 text-red-600 flex items-center justify-center">
                                    <AlertTriangleIcon className="w-3 h-3" />
                                </div>
                                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                                    Confirm
                                </span>
                                <div className="h-4 w-px bg-slate-200" />
                                <span className="text-[12.5px] text-slate-900 font-medium truncate">
                                    Are you sure?
                                </span>
                                <button
                                    type="button"
                                    onClick={() => !loading && setVisible(false)}
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
                                    onClick={() => !loading && setVisible(false)}
                                    disabled={loading}
                                    className="ml-auto h-7 px-2.5 rounded-md text-[12px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 transition-colors disabled:opacity-50"
                                >
                                    Cancel
                                </button>
                                <button
                                    type="button"
                                    onClick={onConfirm}
                                    disabled={loading}
                                    className="h-7 px-2.5 rounded-md bg-red-600 hover:bg-red-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                                >
                                    {loading && <Loader2Icon className="w-3 h-3 animate-spin" />}
                                    Confirm
                                </button>
                            </div>
                        </motion.div>
                    </motion.div>
                )}
            </AnimatePresence>
        </ConfirmContext.Provider>
    );
}
