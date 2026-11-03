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
//
// The prompt asks the backend how this account can confirm and offers only
// those: a passkey, the identity it signs in with (Google, Apple, SSO), a
// password, an authenticator code. An account with none of them is told how
// to get one instead of being shown a field it cannot fill.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { useNavigate } from "@tanstack/react-router";
import { RiAppleFill } from "@remixicon/react";
import { KeyRoundIcon, Loader2Icon, LogInIcon, ShieldCheckIcon, XIcon } from "lucide-react";

import reauth from "@/lib/api/client/auth/reauth";
import getReauthMethods from "@/lib/api/client/auth/reauth/methods";
import { beginPasskeyReauth, type ReauthPasskeyChallenge } from "@/lib/api/client/auth/reauth/passkey";
import { beginSSOReauth, finishSSOReauth } from "@/lib/api/client/auth/reauth/sso";
import type ReauthMethods from "@/lib/api/models/auth/ReauthMethods";
import useLogout from "@/lib/api/hooks/auth/useLogout";
import { Google } from "@/components/svg";
import { PasskeyCancelled, confirmWithPasskey, passkeySupported } from "@/lib/passkey";
import { acceptReauthReturn } from "@/lib/oauthReturn";
import { POPUP_CLOSED, closePopup, navigatePopup, notifyPopupBlocked, reservePopup, waitForPopupMessage } from "@/lib/popup";

interface ReauthDetail {
    resolve: () => void;
    reject: () => void;
    ack?: () => void;
}

const POPUP_NAME = "warmbly-reauth";

// null while loading; "unknown" when the lookup failed and every field is offered.
type MethodsState = ReauthMethods | "unknown" | null;

function errorOf(err: unknown): { code?: string; message?: string } {
    if (typeof err !== "object" || err === null) return {};
    return {
        code: "code" in err && typeof err.code === "string" ? err.code : undefined,
        message: "message" in err && typeof err.message === "string" ? err.message : undefined,
    };
}

export default function ReauthModal() {
    const navigate = useNavigate();
    const logout = useLogout();

    // Every gated request waiting on this prompt; one confirmation answers them all.
    const [pending, setPending] = React.useState<ReauthDetail[] | null>(null);
    const [methods, setMethods] = React.useState<MethodsState>(null);
    const [password, setPassword] = React.useState("");
    const [code, setCode] = React.useState("");
    const [error, setError] = React.useState("");
    // Which method is in flight: "form", "passkey" or a provider id.
    const [busy, setBusy] = React.useState<string | null>(null);

    const pendingRef = React.useRef<ReauthDetail[] | null>(null);
    // Bumped on every open and close, so a late answer from an abandoned attempt does nothing.
    const attempt = React.useRef(0);
    const popupRef = React.useRef<Window | null>(null);
    const passkeyChallenge = React.useRef<Promise<ReauthPasskeyChallenge> | null>(null);

    // Fetched ahead of the click: Safari drops the gesture WebAuthn needs across a round trip.
    const preparePasskey = React.useCallback(() => {
        const p = beginPasskeyReauth();
        p.catch(() => undefined);
        passkeyChallenge.current = p;
    }, []);

    React.useEffect(() => {
        const handler = (e: Event) => {
            const detail = (e as CustomEvent<ReauthDetail>).detail;
            detail.ack?.();
            // Already open: this request waits on the same confirmation.
            if (pendingRef.current) {
                const joined = [...pendingRef.current, detail];
                pendingRef.current = joined;
                setPending(joined);
                return;
            }
            attempt.current += 1;
            const mine = attempt.current;
            setPassword("");
            setCode("");
            setError("");
            setBusy(null);
            setMethods(null);
            passkeyChallenge.current = null;
            pendingRef.current = [detail];
            setPending([detail]);

            getReauthMethods()
                .then((m) => {
                    if (attempt.current !== mine) return;
                    setMethods(m);
                    if (m.passkey && passkeySupported()) preparePasskey();
                })
                .catch(() => {
                    if (attempt.current === mine) setMethods("unknown");
                });
        };
        window.addEventListener("reauth-required", handler);
        return () => window.removeEventListener("reauth-required", handler);
    }, [preparePasskey]);

    const close = React.useCallback((outcome: "resolve" | "reject") => {
        const waiting = pendingRef.current;
        if (!waiting) return;
        attempt.current += 1;
        closePopup(popupRef.current);
        popupRef.current = null;
        pendingRef.current = null;
        setPending(null);
        for (const detail of waiting) {
            if (outcome === "resolve") detail.resolve();
            else detail.reject();
        }
    }, []);

    const cancel = React.useCallback(() => close("reject"), [close]);

    // Topmost layer, so Escape is ours; dialogs underneath bail while an alertdialog is up.
    React.useEffect(() => {
        if (!pending) return;
        const onKey = (e: KeyboardEvent) => {
            if (e.key !== "Escape") return;
            e.stopPropagation();
            cancel();
        };
        window.addEventListener("keydown", onKey, true);
        return () => window.removeEventListener("keydown", onKey, true);
    }, [pending, cancel]);

    async function submit(e: React.FormEvent) {
        e.preventDefault();
        if (busy || !pending) return;
        const mine = attempt.current;
        setBusy("form");
        setError("");
        try {
            await reauth({ password: password || undefined, code: code || undefined });
            if (attempt.current === mine) close("resolve");
        } catch (err) {
            if (attempt.current !== mine) return;
            const { code: errCode, message } = errorOf(err);
            // These say nothing about which factor matched, so their own words are safe to show.
            if (errCode === "reauth_no_factor" || errCode === "reauth_limited" || errCode === "attempt_check_unavailable") {
                setError(message ?? "That did not work. Try again.");
            } else if (errCode === "two_fa_code_used") {
                setError("Wait for a new authenticator code, then try again.");
            } else if (!errCode || errCode === "internal_error" || errCode === "service_unavailable") {
                setError("We couldn't verify your confirmation right now. Try again shortly.");
            } else {
                setError("That did not match. Try again.");
            }
            setBusy(null);
        }
    }

    async function confirmPasskey() {
        if (busy) return;
        const mine = attempt.current;
        setBusy("passkey");
        setError("");
        try {
            const challenge = await (passkeyChallenge.current ?? beginPasskeyReauth());
            passkeyChallenge.current = null;
            await confirmWithPasskey(challenge);
            if (attempt.current === mine) close("resolve");
        } catch (err) {
            if (attempt.current !== mine) return;
            if (err instanceof PasskeyCancelled) {
                if (err.reason === "not-allowed") setError("No passkey was used. Try again, or pick another way.");
                if (err.reason === "timeout") setError("The passkey prompt timed out. Try again.");
            } else {
                setError(errorOf(err).message ?? "That passkey could not be verified. Try again.");
            }
            setBusy(null);
            // A challenge is single use, so have the next one ready.
            preparePasskey();
        }
    }

    async function confirmProvider(provider: string) {
        if (busy) return;
        const mine = attempt.current;
        // Opened inside the click, or Safari blocks it.
        const reserved = reservePopup(POPUP_NAME);
        popupRef.current = reserved;
        setBusy(provider);
        setError("");
        try {
            const { url, binding } = await beginSSOReauth(provider);
            if (attempt.current !== mine) {
                closePopup(reserved);
                return;
            }
            const opened = navigatePopup(reserved, url, POPUP_NAME);
            if (opened.status === "closed") {
                setBusy(null);
                return;
            }
            const popup = opened.status === "open" ? opened.window : null;
            popupRef.current = popup;
            if (!popup) notifyPopupBlocked();

            const result = await waitForPopupMessage(popup, acceptReauthReturn);
            if (attempt.current !== mine) return;
            if (result.cancelled) {
                setBusy(null);
                return;
            }
            if (result.error || !result.code) {
                setError(result.error || "That sign-in did not finish. Try again.");
                setBusy(null);
                return;
            }
            await finishSSOReauth(result.code, binding);
            if (attempt.current === mine) close("resolve");
        } catch (err) {
            closePopup(popupRef.current);
            if (attempt.current !== mine) return;
            const message = err instanceof Error ? err.message : "";
            // Closing the window is changing your mind, not a failure.
            if (message !== POPUP_CLOSED) setError(errorOf(err).message ?? "That did not work. Try again.");
            setBusy(null);
        }
    }

    async function signInAgain() {
        const next = window.location.pathname + window.location.search;
        close("reject");
        await logout.mutateAsync();
        navigate({ to: "/auth/login", search: { next }, replace: true });
    }

    const known = methods !== null && methods !== "unknown" ? methods : null;
    const showPassword = methods === "unknown" || Boolean(known?.password);
    const showCode = methods === "unknown" || Boolean(known?.totp);
    const showPasskey = Boolean(known?.passkey) && passkeySupported();
    const providers = known?.providers ?? [];
    const hasButtons = showPasskey || providers.length > 0;
    const hasFields = showPassword || showCode;
    const nothing = known !== null && !hasButtons && !hasFields;

    let description = "This change needs a fresh check that it is you.";
    if (methods !== null && !nothing) {
        if (hasButtons) description += " Pick one of the ways below.";
        else if (showPassword && showCode) description += " Enter your password, or a code from your authenticator.";
        else if (showPassword) description += " Enter your password.";
        else description += " Enter a code from your authenticator app.";
    }

    const optionClass =
        "h-9 w-full rounded-md border border-slate-200 bg-white px-3 text-[12.5px] font-medium text-slate-800 inline-flex items-center justify-center gap-2 hover:bg-slate-50 disabled:opacity-60 disabled:hover:bg-white";

    return (
        <AnimatePresence>
            {pending && (
                <motion.div
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    // Confirmation dialogs can be waiting for this identity check to finish.
                    className="fixed inset-0 z-[210] bg-slate-900/30 flex items-center justify-center p-4"
                    onMouseDown={(e) => {
                        if (e.target === e.currentTarget) cancel();
                    }}
                >
                    <motion.form
                        // alertdialog so a dialog underneath leaves Escape to this prompt.
                        role="alertdialog"
                        aria-modal="true"
                        aria-labelledby="reauth-title"
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
                                <h2 id="reauth-title" className="text-[13.5px] font-medium text-slate-900">Confirm it is you</h2>
                                <p className="mt-1 text-[12.5px] text-slate-600">{description}</p>
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

                        {methods === null ? (
                            <div className="mt-5 mb-1 flex justify-center" aria-label="Loading">
                                <Loader2Icon className="h-4 w-4 animate-spin text-slate-400" />
                            </div>
                        ) : nothing ? (
                            <div className="mt-4 rounded-md border border-slate-200 bg-slate-50 px-3 py-2.5 text-[12.5px] leading-relaxed text-slate-600">
                                This account has no password, passkey or authenticator yet. Sign in again and repeat
                                this within five minutes. To skip this next time, add a passkey or two-factor
                                authentication under Settings &gt; Security.
                            </div>
                        ) : (
                            <>
                                {hasButtons && (
                                    <div className="mt-4 space-y-2">
                                        {showPasskey && (
                                            <button
                                                type="button"
                                                onClick={confirmPasskey}
                                                disabled={busy !== null}
                                                className={optionClass}
                                            >
                                                {busy === "passkey" ? (
                                                    <Loader2Icon className="h-4 w-4 animate-spin text-sky-500" />
                                                ) : (
                                                    <KeyRoundIcon className="h-4 w-4 text-sky-500" />
                                                )}
                                                {busy === "passkey" ? "Waiting for your passkey…" : "Use a passkey"}
                                            </button>
                                        )}
                                        {providers.map((p) => {
                                            const label = p.label || (p.id === "google" ? "Google" : p.id === "apple" ? "Apple" : "single sign-on");
                                            return (
                                                <button
                                                    key={p.id}
                                                    type="button"
                                                    onClick={() => confirmProvider(p.id)}
                                                    disabled={busy !== null}
                                                    className={optionClass}
                                                >
                                                    {busy === p.id ? (
                                                        <Loader2Icon className="h-4 w-4 animate-spin text-sky-500" />
                                                    ) : p.id === "google" ? (
                                                        <Google className="w-4 shrink-0" />
                                                    ) : p.id === "apple" ? (
                                                        <RiAppleFill className="h-4 w-4 shrink-0" />
                                                    ) : (
                                                        <LogInIcon className="h-4 w-4 text-slate-500" />
                                                    )}
                                                    {busy === p.id ? `Waiting for ${label}…` : `Continue with ${label}`}
                                                </button>
                                            );
                                        })}
                                        {busy !== null && busy !== "form" && busy !== "passkey" && (
                                            <p className="text-center text-[12px] text-slate-500">
                                                Finish in the window that opened.
                                            </p>
                                        )}
                                    </div>
                                )}

                                {hasButtons && hasFields && (
                                    <div className="mt-4 flex items-center gap-3">
                                        <div className="h-px flex-1 bg-slate-200" />
                                        <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400">or</span>
                                        <div className="h-px flex-1 bg-slate-200" />
                                    </div>
                                )}

                                {hasFields && (
                                    <div className="mt-4 space-y-2.5">
                                        {showPassword && (
                                            <input
                                                type="password"
                                                value={password}
                                                onChange={(e) => setPassword(e.target.value)}
                                                placeholder="Password"
                                                aria-label="Password"
                                                autoComplete="current-password"
                                                autoFocus={!hasButtons}
                                                className="h-9 w-full rounded-md border border-slate-200 px-2.5 text-[12.5px] outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
                                            />
                                        )}
                                        {showCode && (
                                            <input
                                                type="text"
                                                value={code}
                                                onChange={(e) => setCode(e.target.value)}
                                                placeholder="Two-factor or recovery code"
                                                aria-label="Two-factor or recovery code"
                                                inputMode="text"
                                                autoComplete="one-time-code"
                                                autoFocus={!hasButtons && !showPassword}
                                                data-ph-mask=""
                                                className="h-9 w-full rounded-md border border-slate-200 px-2.5 text-[12.5px] outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
                                            />
                                        )}
                                    </div>
                                )}
                            </>
                        )}

                        {error && <p role="alert" className="mt-2 text-[12px] text-rose-600">{error}</p>}

                        <div className="mt-4 flex justify-end gap-2">
                            <button
                                type="button"
                                onClick={cancel}
                                className="h-8 rounded-md px-3 text-[12.5px] text-slate-600 hover:bg-slate-50"
                            >
                                Cancel
                            </button>
                            {nothing ? (
                                <button
                                    type="button"
                                    onClick={signInAgain}
                                    disabled={logout.isPending}
                                    className="h-8 rounded-md bg-sky-600 px-3 text-[12.5px] font-medium text-white disabled:opacity-50"
                                >
                                    {logout.isPending ? "Signing out…" : "Sign in again"}
                                </button>
                            ) : (
                                hasFields && (
                                    <button
                                        type="submit"
                                        disabled={busy !== null || (!password && !code)}
                                        className="h-8 rounded-md bg-sky-600 px-3 text-[12.5px] font-medium text-white disabled:opacity-50"
                                    >
                                        {busy === "form" ? "Checking…" : "Confirm"}
                                    </button>
                                )
                            )}
                        </div>
                    </motion.form>
                </motion.div>
            )}
        </AnimatePresence>
    );
}
