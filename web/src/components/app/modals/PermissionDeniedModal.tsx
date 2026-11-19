// Global permission-denied popup. The API client dispatches a `permission-denied`
// window event whenever a WRITE action (POST/PATCH/PUT/DELETE) returns 403, so
// any denied edit/save/delete anywhere in the dashboard gets one clear, consistent
// explanation instead of a silent failure or a generic error toast.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { LockIcon, ShieldCheckIcon, SparklesIcon, XIcon } from "lucide-react";
import { Link } from "@tanstack/react-router";

interface DeniedDetail {
    message?: string;
    code?: string;
}

const STANDING_TITLES: Record<string, string> = {
    account_suspended: "Account suspended",
    account_sending_suspended: "Sending is turned off for your account",
    account_workspace_create_suspended: "You can't create workspaces",
    workspace_sending_suspended: "Sending is paused for this workspace",
    organization_suspended: "This workspace is paused",
    organization_cancelled: "This workspace is paused",
};

export default function PermissionDeniedModal() {
    const [open, setOpen] = React.useState(false);
    const [message, setMessage] = React.useState("");
    const [code, setCode] = React.useState<string | undefined>(undefined);

    React.useEffect(() => {
        const handler = (e: Event) => {
            const detail = (e as CustomEvent<DeniedDetail>).detail;
            setMessage(detail?.message?.trim() || "You don't have permission to do that.");
            setCode(detail?.code);
            setOpen(true);
        };
        window.addEventListener("permission-denied", handler);
        return () => window.removeEventListener("permission-denied", handler);
    }, []);

    // Plan/billing gates come back as 403 too, but they aren't a role problem —
    // label them as an upgrade prompt so the message and the call to action match.
    // Admin routes refuse a session that never presented a second factor. That
    // is fixed under Settings > Security by the person themselves, so the
    // "ask an admin" advice would be wrong; link them there instead.
    const isMFA = code === "admin_mfa_required";
    // A restriction on the account or workspace is lifted by support, not by
    // a role, so the server's own message is the whole explanation.
    const standing = code ? STANDING_TITLES[code] : undefined;
    const isPlan = !isMFA && !standing && /\b(plan|upgrade|trial|subscription|paid)\b/i.test(message);
    const close = () => setOpen(false);

    return (
        <AnimatePresence>
            {open && (
                <motion.div
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    className="fixed inset-0 z-[70] bg-slate-900/30 flex items-center justify-center p-4"
                    onMouseDown={(e) => {
                        if (e.target === e.currentTarget) close();
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
                                onClick={close}
                                aria-label="Close"
                                className="size-7 rounded-md text-slate-400 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center"
                            >
                                <XIcon className="w-4 h-4" />
                            </button>
                        </div>
                        <div
                            className={`mx-auto mb-3 size-11 rounded-xl border flex items-center justify-center ${
                                isPlan
                                    ? "bg-violet-50 border-violet-200 text-violet-600"
                                    : isMFA
                                      ? "bg-sky-50 border-sky-200 text-sky-600"
                                      : "bg-amber-50 border-amber-200 text-amber-600"
                            }`}
                        >
                            {isPlan ? (
                                <SparklesIcon className="w-5 h-5" />
                            ) : isMFA ? (
                                <ShieldCheckIcon className="w-5 h-5" />
                            ) : (
                                <LockIcon className="w-5 h-5" />
                            )}
                        </div>
                        <h3 className="text-[14px] font-semibold text-slate-900">
                            {isPlan
                                ? "Upgrade required"
                                : isMFA
                                  ? "Two-factor authentication required"
                                  : (standing ?? "You don't have permission")}
                        </h3>
                        <p className="text-[12.5px] text-slate-500 leading-relaxed mt-1.5">
                            {message}
                            {!isPlan && !isMFA && !standing && (
                                <>
                                    {" "}
                                    Ask a workspace admin or the owner to grant you access from{" "}
                                    <span className="font-medium text-slate-700">
                                        Settings &rarr; Roles &amp; access
                                    </span>
                                    .
                                </>
                            )}
                        </p>
                        <div className="mt-4 flex items-center justify-center gap-2">
                            {isPlan && (
                                <Link
                                    to="/app/settings/billing/{-$tab}"
                                    params={{ tab: undefined }}
                                    onClick={close}
                                    className="inline-flex items-center h-8 px-3 rounded-md bg-violet-600 hover:bg-violet-700 text-white text-[12.5px] font-medium transition-colors"
                                >
                                    View plans
                                </Link>
                            )}
                            {isMFA && (
                                <Link
                                    to="/app/settings/security"
                                    onClick={close}
                                    className="inline-flex items-center h-8 px-3 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12.5px] font-medium transition-colors"
                                >
                                    Open security settings
                                </Link>
                            )}
                            <button
                                type="button"
                                onClick={close}
                                className={`inline-flex items-center h-8 px-4 rounded-md text-[12.5px] font-medium transition-colors ${
                                    isPlan || isMFA
                                        ? "border border-slate-200 bg-white hover:bg-slate-50 text-slate-700"
                                        : "bg-slate-900 hover:bg-slate-800 text-white"
                                }`}
                            >
                                Got it
                            </button>
                        </div>
                    </motion.div>
                </motion.div>
            )}
        </AnimatePresence>
    );
}
