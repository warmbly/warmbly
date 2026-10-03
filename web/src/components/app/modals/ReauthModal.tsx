// Global "confirm it is you" prompt.
//
// Some changes need a fresh proof of identity, not just a live session:
// minting an API key, registering or removing a passkey, handing a workspace
// to someone else, scheduling a deletion. The backend answers those with
// `reauth_required` until the session has re-authenticated in the last few
// minutes.
//
// The API client dispatches a `reauth-required` event carrying resolve and
// reject, waits for one of them, and retries the original request on success.
// That way every gated action gets the prompt without each call site knowing
// about it.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { ShieldCheckIcon, XIcon } from "lucide-react";

import reauth from "@/lib/api/client/auth/reauth";

interface ReauthDetail {
    resolve: () => void;
    reject: () => void;
}

export default function ReauthModal() {
    const [pending, setPending] = React.useState<ReauthDetail | null>(null);
    const [password, setPassword] = React.useState("");
    const [code, setCode] = React.useState("");
    const [error, setError] = React.useState("");
    const [busy, setBusy] = React.useState(false);

    React.useEffect(() => {
        const handler = (e: Event) => {
            setPassword("");
            setCode("");
            setError("");
            setBusy(false);
            setPending((e as CustomEvent<ReauthDetail>).detail);
        };
        window.addEventListener("reauth-required", handler);
        return () => window.removeEventListener("reauth-required", handler);
    }, []);

    const cancel = React.useCallback(() => {
        pending?.reject();
        setPending(null);
    }, [pending]);

    async function submit(e: React.FormEvent) {
        e.preventDefault();
        if (busy || !pending) return;
        setBusy(true);
        setError("");
        try {
            await reauth({ password: password || undefined, code: code || undefined });
            pending.resolve();
            setPending(null);
        } catch (err) {
            const code = (err as { code?: string } | undefined)?.code;
            if (code === "reauth_no_factor") {
                // An account created through Google, Apple or SSO has nothing
                // to confirm with until it adds one. Say what to do; the
                // generic message below would be a dead end.
                setError(
                    (err as { message?: string }).message ??
                        "This account has nothing to confirm with yet. Turn on two-factor authentication under Settings > Security.",
                );
            } else {
                // Otherwise one message: which factor matched is not something
                // to spell out to whoever is sitting at the keyboard.
                setError("That did not match. Try again.");
            }
            setBusy(false);
        }
    }

    return (
        <AnimatePresence>
            {pending && (
                <motion.div
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    // Above all dialogs and confirm modals (z-[200]).
                    className="fixed inset-0 z-[250] bg-slate-900/30 flex items-center justify-center p-4"
                    onMouseDown={(e) => {
                        if (e.target === e.currentTarget) cancel();
                    }}
                >
                    <motion.form
                        // alertdialog so a dialog underneath leaves Escape to this prompt.
                        role="alertdialog"
                        aria-modal="true"
                        initial={{ opacity: 0, scale: 0.97, y: 8 }}
                        animate={{ opacity: 1, scale: 1, y: 0 }}
                        exit={{ opacity: 0, scale: 0.97, y: 8 }}
                        onSubmit={submit}
                        onMouseDown={(e) => e.stopPropagation()}
                        className="w-full max-w-sm rounded-lg border border-slate-200 bg-white p-5 shadow-xl"
                    >
                        <div className="flex items-start gap-3">
                            <div className="mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-sky-50">
                                <ShieldCheckIcon className="h-4 w-4 text-sky-700" />
                            </div>
                            <div className="flex-1">
                                <h2 className="text-[13.5px] font-medium text-slate-900">Confirm it is you</h2>
                                <p className="mt-1 text-[12.5px] text-slate-600">
                                    This change needs a fresh check. Enter your password, or a code from
                                    your authenticator.
                                </p>
                            </div>
                            <button
                                type="button"
                                onClick={cancel}
                                aria-label="Cancel"
                                className="text-slate-400 hover:text-slate-600"
                            >
                                <XIcon className="h-4 w-4" />
                            </button>
                        </div>

                        <div className="mt-4 space-y-2.5">
                            <input
                                type="password"
                                value={password}
                                onChange={(e) => setPassword(e.target.value)}
                                placeholder="Password"
                                autoComplete="current-password"
                                autoFocus
                                className="h-9 w-full rounded-md border border-slate-200 px-2.5 text-[12.5px] outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
                            />
                            <input
                                type="text"
                                value={code}
                                onChange={(e) => setCode(e.target.value)}
                                placeholder="Two-factor or recovery code"
                                inputMode="text"
                                autoComplete="one-time-code"
                                data-ph-mask=""
                                className="h-9 w-full rounded-md border border-slate-200 px-2.5 text-[12.5px] outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
                            />
                        </div>

                        {error && <p className="mt-2 text-[12px] text-rose-600">{error}</p>}

                        <div className="mt-4 flex justify-end gap-2">
                            <button
                                type="button"
                                onClick={cancel}
                                className="h-8 rounded-md px-3 text-[12.5px] text-slate-600 hover:bg-slate-50"
                            >
                                Cancel
                            </button>
                            <button
                                type="submit"
                                disabled={busy || (!password && !code)}
                                className="h-8 rounded-md bg-sky-600 px-3 text-[12.5px] font-medium text-white disabled:opacity-50"
                            >
                                {busy ? "Checking…" : "Confirm"}
                            </button>
                        </div>
                    </motion.form>
                </motion.div>
            )}
        </AnimatePresence>
    );
}
