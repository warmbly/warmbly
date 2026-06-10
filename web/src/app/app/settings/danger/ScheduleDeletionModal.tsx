// Phrase-confirmation modal for danger-zone deletions.
//
// Same idea as GitHub's "Type the repo name to delete it" dialog: a
// destructive button alone is too easy to misclick, so we force the
// user to type a recognisable phrase (org name / their email) and an
// optional reason. The button stays disabled until the phrase matches.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { AlertTriangleIcon, Loader2Icon, XIcon } from "lucide-react";
import toast from "react-hot-toast";
import buildError from "@/lib/helper/buildError";
import type { AppError } from "@/lib/api/client/normalizeError";
import { TextInput, Label } from "@/components/ui/field";

interface Props {
    open: boolean;
    onClose: () => void;

    // Visible title — e.g. "Delete Acme Inc" or "Delete your account".
    title: string;
    // Body shown above the warning + input. Plain text or JSX.
    body: React.ReactNode;
    // Phrase the user must type to enable the destructive button.
    confirmationHint: string;
    // How many days will pass before the actual delete happens.
    graceDays: number;

    submitLabel?: string;
    onSubmit: (data: { confirmation: string; reason: string }) => Promise<unknown>;
}

export default function ScheduleDeletionModal({
    open,
    onClose,
    title,
    body,
    confirmationHint,
    graceDays,
    submitLabel = "Schedule deletion",
    onSubmit,
}: Props) {
    const [confirmation, setConfirmation] = React.useState("");
    const [reason, setReason] = React.useState("");
    const [loading, setLoading] = React.useState(false);

    // Reset the inputs whenever the modal re-opens so old text doesn't
    // linger from a previous attempt.
    React.useEffect(() => {
        if (open) {
            setConfirmation("");
            setReason("");
        }
    }, [open]);

    React.useEffect(() => {
        if (!open) return;
        const onKey = (e: KeyboardEvent) => {
            if (e.key === "Escape" && !loading) onClose();
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [open, loading, onClose]);

    const matches =
        confirmation.trim().toLowerCase() === confirmationHint.trim().toLowerCase();

    const handleSubmit = async () => {
        if (loading || !matches) return;
        try {
            setLoading(true);
            await onSubmit({ confirmation: confirmation.trim(), reason: reason.trim() });
            onClose();
        } catch (err) {
            toast.error(buildError(err as AppError));
        } finally {
            setLoading(false);
        }
    };

    return (
        <AnimatePresence>
            {open && (
                <motion.div
                    key="overlay"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.15 }}
                    onClick={() => !loading && onClose()}
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
                        className="w-full max-w-[460px] max-h-[calc(100dvh-2rem)] flex flex-col rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18),0_8px_16px_-8px_rgba(15,23,42,0.1)] overflow-hidden"
                    >
                        <div className="shrink-0 h-12 px-4 border-b border-slate-200 flex items-center gap-2.5">
                            <div className="size-5 rounded bg-red-50 text-red-600 flex items-center justify-center">
                                <AlertTriangleIcon className="w-3 h-3" />
                            </div>
                            <span className="text-[10px] uppercase tracking-[0.14em] text-red-600 font-medium">
                                Danger zone
                            </span>
                            <div className="h-4 w-px bg-slate-200" />
                            <span className="text-[12.5px] text-slate-900 font-medium truncate">
                                {title}
                            </span>
                            <button
                                type="button"
                                onClick={() => !loading && onClose()}
                                disabled={loading}
                                aria-label="Close"
                                className="ml-auto size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors disabled:opacity-50"
                            >
                                <XIcon className="w-3.5 h-3.5" />
                            </button>
                        </div>

                        <div className="flex-1 min-h-0 overflow-y-auto px-4 py-4 space-y-4">
                            <div className="text-[12.5px] text-slate-700 leading-relaxed">
                                {body}
                            </div>

                            <div className="rounded-md border border-amber-200 bg-amber-50/70 px-3 py-2 text-[11.5px] text-amber-800 leading-relaxed">
                                We'll wait <strong>{graceDays} days</strong> before
                                running the actual delete. You can cancel any time
                                before then from this same page. After that, the data
                                is gone and we can't get it back.
                            </div>

                            <div className="space-y-1.5">
                                <Label>
                                    Type{" "}
                                    <code className="font-mono text-[12px] text-slate-900 bg-slate-100 px-1 rounded break-all">
                                        {confirmationHint}
                                    </code>{" "}
                                    to confirm
                                </Label>
                                <TextInput
                                    value={confirmation}
                                    onChange={setConfirmation}
                                    placeholder={confirmationHint}
                                    // Only autofocus with a pointer device; on touch it
                                    // would pop the keyboard and shrink the viewport
                                    // before the user has read the warning.
                                    autoFocus={window.matchMedia("(pointer: fine)").matches}
                                />
                            </div>

                            <div className="space-y-1.5">
                                <Label>Reason (optional)</Label>
                                <TextInput
                                    value={reason}
                                    onChange={setReason}
                                    placeholder="So we can improve. Stays on the audit log."
                                />
                            </div>
                        </div>

                        <div className="shrink-0 px-3 h-12 border-t border-slate-200 flex items-center gap-1.5">
                            <button
                                type="button"
                                onClick={() => !loading && onClose()}
                                disabled={loading}
                                className="ml-auto h-7 px-2.5 rounded-md text-[12px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 transition-colors disabled:opacity-50"
                            >
                                Cancel
                            </button>
                            <button
                                type="button"
                                onClick={handleSubmit}
                                disabled={loading || !matches}
                                className="h-7 px-2.5 rounded-md bg-red-600 hover:bg-red-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                            >
                                {loading && <Loader2Icon className="w-3 h-3 animate-spin" />}
                                {submitLabel}
                            </button>
                        </div>
                    </motion.div>
                </motion.div>
            )}
        </AnimatePresence>
    );
}
