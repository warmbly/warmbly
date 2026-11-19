// A mailbox's sign-in, shown once. It lives only in this dialog's props: nothing caches it, and closing drops it.
import React from "react";
import { createPortal } from "react-dom";
import { motion, useReducedMotion } from "framer-motion";
import { ArrowUpRightIcon, EyeIcon, EyeOffIcon, KeyRoundIcon, XIcon } from "lucide-react";
import type { MailboxSignIn } from "@/lib/api/models/app/store/Store";
import CopyButton from "./CopyButton";

export default function SignInDialog({ signIn, admin, onClose }: { signIn: MailboxSignIn; admin: boolean; onClose: () => void }) {
    const reduced = useReducedMotion();
    const [reveal, setReveal] = React.useState(false);

    React.useEffect(() => {
        const onKey = (e: KeyboardEvent) => {
            if (e.key !== "Escape") return;
            if (document.querySelector("[data-floating], [role='alertdialog']")) return;
            e.stopPropagation();
            onClose();
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [onClose]);

    // Portalled: a transformed drawer would otherwise become the fixed backdrop's containing block.
    return createPortal(
        <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.15 }}
            onMouseDown={onClose}
            className="fixed inset-0 z-[120] flex items-center justify-center bg-slate-900/30 backdrop-blur-[2px] px-4"
        >
            <motion.div
                role="dialog"
                aria-modal="true"
                aria-labelledby="signin-title"
                initial={reduced ? { opacity: 0 } : { opacity: 0, y: 8, scale: 0.98 }}
                animate={{ opacity: 1, y: 0, scale: 1 }}
                exit={reduced ? { opacity: 0 } : { opacity: 0, y: 8, scale: 0.98 }}
                transition={{ duration: 0.18, ease: [0.22, 1, 0.36, 1] }}
                onMouseDown={(e) => e.stopPropagation()}
                className="w-full max-w-[440px] rounded-xl border border-slate-200 bg-white shadow-[0_24px_48px_-16px_rgba(15,23,42,0.3)] overflow-hidden"
            >
                <div className="px-4 h-12 flex items-center gap-2 border-b border-slate-200">
                    <KeyRoundIcon className="w-3.5 h-3.5 text-slate-400" />
                    <h2 id="signin-title" className="text-[13px] font-semibold text-slate-900 flex-1 truncate">
                        {admin ? "Admin sign-in" : "Sign-in"}
                    </h2>
                    <button
                        type="button"
                        onClick={onClose}
                        aria-label="Close"
                        className="size-7 rounded-md flex items-center justify-center text-slate-400 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                    >
                        <XIcon className="w-4 h-4" />
                    </button>
                </div>
                <div className="p-4 space-y-3">
                    <p className="text-[12px] text-slate-500 leading-relaxed">
                        {admin
                            ? "This signs in to the account that manages the domain's mailboxes. It is shown once, so copy what you need now."
                            : "Shown once, so copy what you need now."}
                    </p>
                    <dl className="rounded-md border border-slate-200 divide-y divide-slate-100">
                        <Row label="Email" value={signIn.email} />
                        {signIn.username && signIn.username !== signIn.email && <Row label="Username" value={signIn.username} />}
                        <div className="pl-3 pr-1.5 py-1.5 flex items-center gap-2">
                            <dt className="w-20 shrink-0 text-[11px] text-slate-500">Password</dt>
                            <dd className="flex-1 min-w-0 font-mono text-[12px] text-slate-900 truncate select-all">
                                {reveal ? signIn.password : "•".repeat(Math.min(16, signIn.password.length || 12))}
                            </dd>
                            <button
                                type="button"
                                onClick={() => setReveal((r) => !r)}
                                aria-label={reveal ? "Hide password" : "Show password"}
                                className="size-6 shrink-0 inline-flex items-center justify-center rounded-md text-slate-400 hover:text-slate-800 hover:bg-slate-100 transition-colors"
                            >
                                {reveal ? <EyeOffIcon className="w-3 h-3" /> : <EyeIcon className="w-3 h-3" />}
                            </button>
                            <CopyButton value={signIn.password} label="password" />
                        </div>
                    </dl>
                    <div className="flex flex-wrap gap-2">
                        {signIn.admin_console_url && (
                            <a
                                href={signIn.admin_console_url}
                                target="_blank"
                                rel="noopener noreferrer"
                                className="h-8 px-3 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-semibold inline-flex items-center gap-1.5 transition-colors"
                            >
                                Open admin console <ArrowUpRightIcon className="w-3.5 h-3.5" />
                            </a>
                        )}
                        {signIn.login_url && (
                            <a
                                href={signIn.login_url}
                                target="_blank"
                                rel="noopener noreferrer"
                                className="h-8 px-3 rounded-md border border-slate-200 hover:border-slate-300 text-slate-800 text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors"
                            >
                                Open sign-in page <ArrowUpRightIcon className="w-3.5 h-3.5" />
                            </a>
                        )}
                    </div>
                </div>
            </motion.div>
        </motion.div>,
        document.body,
    );
}

function Row({ label, value }: { label: string; value: string }) {
    return (
        <div className="pl-3 pr-1.5 py-1.5 flex items-center gap-2">
            <dt className="w-20 shrink-0 text-[11px] text-slate-500">{label}</dt>
            <dd className="flex-1 min-w-0 font-mono text-[12px] text-slate-900 truncate select-all">{value}</dd>
            <CopyButton value={value} label={label.toLowerCase()} />
        </div>
    );
}
