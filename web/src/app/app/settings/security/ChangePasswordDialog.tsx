// Change-password modal for the security page. Verifies the current password
// server-side, enforces the password policy, and shows clear errors.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { Loader2Icon, XIcon } from "lucide-react";
import toast from "react-hot-toast";
import { Label, TextInput } from "@/components/ui/field";
import changePassword from "@/lib/api/client/auth/changePassword";
import { useQueryClient } from "@tanstack/react-query";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

export default function ChangePasswordDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
    const [current, setCurrent] = React.useState("");
    const [next, setNext] = React.useState("");
    const [confirm, setConfirm] = React.useState("");
    const [pending, setPending] = React.useState(false);
    const queryClient = useQueryClient();

    React.useEffect(() => {
        if (open) {
            setCurrent("");
            setNext("");
            setConfirm("");
        }
    }, [open]);

    const valid =
        current.length > 0 &&
        next.length >= 12 &&
        /[a-z]/.test(next) &&
        /[A-Z]/.test(next) &&
        /[0-9]/.test(next) &&
        next === confirm;

    async function submit() {
        if (!valid || pending) return;
        setPending(true);
        try {
            await changePassword({ current_password: current, new_password: next });
            // Other sessions are revoked server-side; refresh the list so they
            // drop out of the Sessions panel live.
            void queryClient.invalidateQueries({ queryKey: ["sessions"] });
            toast.success("Password changed. Other devices were signed out.");
            onClose();
        } catch (e) {
            toast.error(buildError(e as AppError));
        } finally {
            setPending(false);
        }
    }

    return (
        <AnimatePresence>
            {open && (
                <motion.div
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    className="fixed inset-0 z-50 bg-slate-900/30 flex items-center justify-center p-4"
                    onMouseDown={(e) => {
                        if (e.target === e.currentTarget && !pending) onClose();
                    }}
                >
                    <motion.div
                        initial={{ opacity: 0, y: 8, scale: 0.98 }}
                        animate={{ opacity: 1, y: 0, scale: 1 }}
                        exit={{ opacity: 0, y: 8, scale: 0.98 }}
                        className="w-full max-w-sm rounded-lg bg-white border border-slate-200 shadow-xl"
                    >
                        <header className="px-4 h-12 flex items-center gap-2 border-b border-slate-200">
                            <h3 className="text-[13px] font-semibold text-slate-900">Change password</h3>
                            <button
                                type="button"
                                onClick={onClose}
                                disabled={pending}
                                aria-label="Close"
                                className="ml-auto size-7 rounded-md text-slate-400 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center disabled:opacity-60"
                            >
                                <XIcon className="w-4 h-4" />
                            </button>
                        </header>

                        <div className="p-4 space-y-3">
                            <div>
                                <Label>Current password</Label>
                                <TextInput type="password" value={current} onChange={setCurrent} placeholder="Current password" autoFocus />
                            </div>
                            <div>
                                <Label>New password</Label>
                                <TextInput type="password" value={next} onChange={setNext} placeholder="At least 12 characters" />
                                <p className="text-[11px] text-slate-400 mt-1">12+ characters with upper and lower case and a number.</p>
                            </div>
                            <div>
                                <Label>Confirm new password</Label>
                                <TextInput type="password" value={confirm} onChange={setConfirm} placeholder="Repeat new password" onKeyDown={(e) => { if (e.key === "Enter") submit(); }} />
                                {confirm.length > 0 && next !== confirm && (
                                    <p className="text-[11px] text-rose-500 mt-1">Passwords do not match.</p>
                                )}
                            </div>
                            <p className="text-[11px] text-slate-400 leading-relaxed pt-1">
                                Changing your password signs out every other device. This one stays signed in.
                            </p>
                        </div>

                        <footer className="px-4 h-12 flex items-center justify-end gap-2 border-t border-slate-200">
                            <button type="button" onClick={onClose} disabled={pending} className="h-7 px-2.5 rounded-md text-[12px] font-medium text-slate-600 hover:bg-slate-100 transition-colors disabled:opacity-60">
                                Cancel
                            </button>
                            <button type="button" onClick={submit} disabled={!valid || pending} className="h-7 px-3 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60">
                                {pending && <Loader2Icon className="w-3 h-3 animate-spin" />}
                                Change password
                            </button>
                        </footer>
                    </motion.div>
                </motion.div>
            )}
        </AnimatePresence>
    );
}
