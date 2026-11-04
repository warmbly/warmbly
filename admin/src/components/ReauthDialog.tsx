// Global "confirm it is you" prompt, the admin's mirror of the dashboard's
// ReauthModal. The API client dispatches `reauth-required` with resolve and
// reject when a route answers reauth_required, and retries once on resolve.

import { useCallback, useEffect, useState, type FormEvent } from "react";
import * as DialogPrimitive from "@radix-ui/react-dialog";
import { ShieldCheck } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { reauth, reauthMethods } from "@/lib/api/client/auth";
import { APIError } from "@/lib/api/client";

interface ReauthDetail {
    resolve: () => void;
    reject: () => void;
    ack?: () => void;
}

export function ReauthDialog() {
    const [pending, setPending] = useState<ReauthDetail | null>(null);
    const [password, setPassword] = useState("");
    const [code, setCode] = useState("");
    const [error, setError] = useState("");
    const [busy, setBusy] = useState(false);
    // Null while loading or when the lookup failed, which offers both fields.
    const [methods, setMethods] = useState<{ password: boolean; totp: boolean } | null>(null);

    useEffect(() => {
        const handler = (e: Event) => {
            setPassword("");
            setCode("");
            setError("");
            setBusy(false);
            const detail = (e as CustomEvent<ReauthDetail>).detail;
            detail.ack?.();
            setMethods(null);
            setPending(detail);
            reauthMethods().then(setMethods, () => setMethods(null));
        };
        window.addEventListener("reauth-required", handler);
        return () => window.removeEventListener("reauth-required", handler);
    }, []);

    const cancel = useCallback(() => {
        pending?.reject();
        setPending(null);
    }, [pending]);

    async function submit(e: FormEvent) {
        e.preventDefault();
        if (busy || !pending) return;
        setBusy(true);
        setError("");
        try {
            await reauth({ password: password || undefined, code: code || undefined });
            pending.resolve();
            setPending(null);
        } catch (err) {
            const errCode = err instanceof APIError ? err.code : undefined;
            if (err instanceof APIError && (errCode === "reauth_no_factor" || errCode === "reauth_limited" || errCode === "attempt_check_unavailable")) {
                setError(err.message);
            } else if (errCode === "two_fa_code_used") {
                setError("Wait for a new authenticator code, then try again.");
            } else if (!errCode || errCode === "internal_error" || errCode === "service_unavailable") {
                setError("We couldn't verify your confirmation right now. Try again shortly.");
            } else {
                setError("That did not match. Try again.");
            }
            setBusy(false);
        }
    }

    const showPassword = methods === null || methods.password;
    const showCode = methods === null || methods.totp;
    const description =
        showPassword && showCode
            ? "This change needs a fresh check. Enter your password, or a code from your authenticator."
            : showPassword
              ? "This change needs a fresh check. Enter your password."
              : showCode
                ? "This change needs a fresh check. Enter a code from your authenticator app."
                : "This account has no password or authenticator code to confirm with here. Sign in to the admin panel again and repeat this within five minutes.";

    return (
        <DialogPrimitive.Root open={pending !== null} onOpenChange={(open) => !open && cancel()}>
            <DialogPrimitive.Portal>
                <DialogPrimitive.Overlay className="data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 fixed inset-0 z-[70] bg-black/40 backdrop-blur-[2px] dark:bg-black/60" />
                <DialogPrimitive.Content
                    role="alertdialog"
                    data-floating=""
                    onMouseDown={(e) => e.stopPropagation()}
                    className="bg-background dark:bg-[oklch(0.19_0.004_286)] data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-[0.98] data-[state=open]:zoom-in-[0.96] data-[state=open]:slide-in-from-top-[1%] ease-[cubic-bezier(0.16,1,0.3,1)] fixed top-[50%] left-[50%] z-[70] w-full max-w-[calc(100%-2rem)] translate-x-[-50%] translate-y-[-50%] rounded-xl border-0 p-5 shadow-popover duration-150 outline-none sm:max-w-sm"
                >
                    <form onSubmit={submit} className="grid gap-4">
                        <div className="flex items-start gap-3">
                            <span className="mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-full bg-muted text-foreground">
                                <ShieldCheck className="size-4" />
                            </span>
                            <div className="min-w-0 flex flex-col gap-1.5">
                                <DialogPrimitive.Title className="text-[15px] leading-snug font-semibold text-foreground">
                                    Confirm it is you
                                </DialogPrimitive.Title>
                                <DialogPrimitive.Description className="text-[13px] leading-relaxed text-muted-foreground">
                                    {description}
                                </DialogPrimitive.Description>
                            </div>
                        </div>
                        {showPassword || showCode ? (
                            <div className="grid gap-2.5">
                                {showPassword && (
                                    <Input
                                        type="password"
                                        value={password}
                                        onChange={(e) => setPassword(e.target.value)}
                                        placeholder="Password"
                                        aria-label="Password"
                                        autoComplete="current-password"
                                        autoFocus
                                    />
                                )}
                                {showCode && (
                                    <Input
                                        type="text"
                                        value={code}
                                        onChange={(e) => setCode(e.target.value)}
                                        placeholder="Two-factor or recovery code"
                                        aria-label="Two-factor or recovery code"
                                        autoComplete="one-time-code"
                                        autoFocus={!showPassword}
                                        data-ph-mask=""
                                    />
                                )}
                            </div>
                        ) : null}
                        {error && <p role="alert" className="text-sm text-[var(--admin-danger)]">{error}</p>}
                        <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
                            <Button type="button" variant="outline" onClick={cancel}>
                                Cancel
                            </Button>
                            {(showPassword || showCode) && (
                                <Button type="submit" disabled={busy || (!password && !code)}>
                                    {busy ? "Checking…" : "Confirm"}
                                </Button>
                            )}
                        </div>
                    </form>
                </DialogPrimitive.Content>
            </DialogPrimitive.Portal>
        </DialogPrimitive.Root>
    );
}
