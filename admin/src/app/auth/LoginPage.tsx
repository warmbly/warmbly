// Admin sign-in — two-step, matching the backend's email-code login.
//
// Step 1: email + password (+ Turnstile) -> /auth/login, which emails a
//   one-time code and returns a short-lived session.
// Step 2: the emailed 6-digit code -> /auth/login/confirm, which returns the
//   real access/refresh token pair.
//
// The two steps cross-fade/slide (motion) inside a fixed-height frame, so the
// transition is smooth. In dev the captcha is bypassed and the code lands in
// mailpit (:18025). Clean and in-theme (light shadcn surface, red admin
// accent), deliberately distinct from the consumer dashboard's marketing login.

import { useEffect, useRef, useState, type FormEvent } from "react";
import { useNavigate, useLocation } from "react-router-dom";
import { AnimatePresence, motion, type Transition } from "motion/react";
import { toast } from "sonner";
import { ShieldCheck, Eye, EyeOff, Loader2, ArrowLeft, MailCheck } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { InputOTP, InputOTPGroup, InputOTPSlot } from "@/components/ui/input-otp";
import { Logo } from "@/components/Logo";
import { TurnstileModal } from "@/components/captcha/TurnstileModal";
import { login, loginConfirm, verifyTwoFA } from "@/lib/api/client/auth";
import type { LoginResponse } from "@/lib/api/models/auth";
import { setToken } from "@/lib/auth/storage";
import { APIError } from "@/lib/api/client";
import { DASHBOARD_URL, ENV_LABEL } from "@/lib/env";
import { cn } from "@/lib/utils";

// Admin accent — red-600, the theme's --admin-danger token ("restricted access").
const ACCENT = "var(--admin-danger)";
const FOCUS_RED = "focus-visible:border-red-400 focus-visible:ring-red-100";
const RESEND_SECONDS = 45;
const SWAP_TRANSITION: Transition = { duration: 0.26, ease: [0.16, 1, 0.3, 1] };

function errorMessage(err: unknown): string {
    return err instanceof APIError
        ? err.message
        : "Sign-in failed. Check your credentials or contact an admin.";
}

export default function LoginPage() {
    const nav = useNavigate();
    const loc = useLocation();
    const [phase, setPhase] = useState<"credentials" | "code" | "twofa">("credentials");
    const [pendingToken, setPendingToken] = useState("");
    const [email, setEmail] = useState("");
    const [password, setPassword] = useState("");
    const [showPassword, setShowPassword] = useState(false);
    const [session, setSession] = useState("");
    const [code, setCode] = useState("");
    const [captcha, setCaptcha] = useState(false);
    const [submitting, setSubmitting] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [resendIn, setResendIn] = useState(0);
    const loginStartInFlight = useRef(false);
    const confirmInFlight = useRef(false);

    const busy = submitting || captcha;

    // Resend cooldown tick.
    useEffect(() => {
        if (resendIn <= 0) return;
        const t = setTimeout(() => setResendIn((s) => s - 1), 1000);
        return () => clearTimeout(t);
    }, [resendIn]);

    // ── Step 1: password -> captcha -> /auth/login (emails the code) ──
    function startLogin() {
        setError(null);
        if (!busy && !loginStartInFlight.current) setCaptcha(true);
    }

    function onCredentials(e: FormEvent) {
        e.preventDefault();
        startLogin();
    }

    async function onToken(token: string) {
        if (loginStartInFlight.current) return;
        loginStartInFlight.current = true;
        setCaptcha(false);
        setSubmitting(true);
        const fresh = phase === "credentials";
        try {
            const res = await login({ email, password, turnstile: token });
            // Nothing was emailed: this deployment has the login code off, or
            // already knows this device. The login is already resolved.
            if (!res.code_required) {
                if (res.two_fa_required && res.pending_token) {
                    setPendingToken(res.pending_token);
                    setCode("");
                    setPhase("twofa");
                    return;
                }
                if (res.token) {
                    setToken(res.token);
                    const dest = (loc.state as { from?: string } | null)?.from ?? "/";
                    nav(dest, { replace: true });
                    return;
                }
                throw new Error("The server returned an unexpected sign-in response.");
            }
            setSession(res.session ?? "");
            setCode("");
            setResendIn(RESEND_SECONDS);
            if (fresh) setPhase("code");
            else toast.success("A new code is on its way.");
        } catch (err) {
            const msg = errorMessage(err);
            setError(msg);
            toast.error(msg);
        } finally {
            loginStartInFlight.current = false;
            setSubmitting(false);
        }
    }

    function onCaptchaError(message = "Verification failed. Please try again.") {
        loginStartInFlight.current = false;
        setCaptcha(false);
        setSubmitting(false);
        setError(message);
        toast.error(message);
    }

    // ── Step 2: emailed code -> /auth/login/confirm (returns the token) ──
    async function submitCode(value: string) {
        if (confirmInFlight.current || value.length < 6) return;
        confirmInFlight.current = true;
        setError(null);
        setSubmitting(true);
        try {
            const res = await loginConfirm({ session, code: value });
            // 2FA gate. Without this branch an operator with TOTP enrolled got
            // a response carrying no access token and was silently stuck.
            if (res.two_fa_required) {
                if (!res.pending_token) throw new Error("The server returned an unexpected sign-in response.");
                setPendingToken(res.pending_token);
                setCode("");
                setPhase("twofa");
                return;
            }
            if (!res.access_token) throw new Error("The server returned an unexpected sign-in response.");
            setToken(res as LoginResponse);
            const dest = (loc.state as { from?: string } | null)?.from ?? "/";
            nav(dest, { replace: true });
        } catch (err) {
            const msg = errorMessage(err);
            setError(msg);
            toast.error(msg);
            setCode("");
        } finally {
            confirmInFlight.current = false;
            setSubmitting(false);
        }
    }

    // ── Step 3: TOTP or recovery code -> /auth/2fa/verify ──
    async function submitTwoFA(value: string) {
        if (confirmInFlight.current || value.length < 6) return;
        confirmInFlight.current = true;
        setError(null);
        setSubmitting(true);
        try {
            const tok = await verifyTwoFA({ pending_token: pendingToken, code: value });
            setToken(tok);
            const dest = (loc.state as { from?: string } | null)?.from ?? "/";
            nav(dest, { replace: true });
        } catch (err) {
            const msg = errorMessage(err);
            setError(msg);
            toast.error(msg);
            setCode("");
        } finally {
            confirmInFlight.current = false;
            setSubmitting(false);
        }
    }

    function onResend() {
        if (resendIn <= 0) startLogin();
    }

    function backToCredentials() {
        setPhase("credentials");
        setSession("");
        setCode("");
        setError(null);
    }

    const errorLine = error && (
        <p className="flex items-start gap-1.5 text-[12.5px] text-red-600">
            <span className="mt-1.5 size-1 shrink-0 rounded-full bg-red-500" />
            {error}
        </p>
    );

    return (
        <div className="flex min-h-dvh flex-col items-center justify-center bg-muted/40 px-4 py-10">
            <div className="w-full max-w-[400px]">
                {/* Brand */}
                <div className="mb-6 flex items-center justify-center gap-2.5">
                    <Logo className="h-7 w-7 text-foreground" />
                    <span className="text-[18px] font-bold tracking-tight">Warmbly</span>
                </div>

                {/* Card — a thin red top edge is the only "admin" flourish. */}
                <div className="overflow-hidden rounded-xl border border-border bg-card shadow-sm">
                    <div className="h-1" style={{ backgroundColor: ACCENT }} />

                    <div className="px-7 pt-6 pb-7">
                        {/* Static header — stays put while the steps cross-fade. */}
                        <div className="flex items-center justify-between">
                            <span
                                className="inline-flex items-center gap-1.5 text-[12px] font-semibold"
                                style={{ color: ACCENT }}
                            >
                                <ShieldCheck className="size-3.5" />
                                Admin access
                            </span>
                            {/* Env is shown only where it matters — dev stays clean. */}
                            {ENV_LABEL !== "development" && (
                                <span
                                    className={cn(
                                        "text-[11px] font-semibold uppercase tracking-wide",
                                        ENV_LABEL === "production" ? "text-red-600" : "text-amber-600",
                                    )}
                                >
                                    {ENV_LABEL === "production" ? "Production" : "Staging"}
                                </span>
                            )}
                        </div>

                        {/* Fixed-height swap frame so steps slide without a height jump. */}
                        <div className="relative mt-4 min-h-[286px]">
                            <AnimatePresence mode="wait" initial={false}>
                                {phase === "credentials" ? (
                                    <motion.div
                                        key="credentials"
                                        initial={{ opacity: 0, x: 14 }}
                                        animate={{ opacity: 1, x: 0 }}
                                        exit={{ opacity: 0, x: -14 }}
                                        transition={SWAP_TRANSITION}
                                    >
                                        <h1 className="text-[20px] font-semibold tracking-tight">Sign in</h1>
                                        <p className="mt-1 text-[13px] text-muted-foreground">
                                            Restricted to Warmbly staff with admin permissions.
                                        </p>

                                        <form className="mt-6 space-y-4" onSubmit={onCredentials}>
                                            <div className="space-y-1.5">
                                                <Label htmlFor="email">Email</Label>
                                                <Input
                                                    id="email"
                                                    type="email"
                                                    autoComplete="email"
                                                    autoFocus
                                                    value={email}
                                                    onChange={(e) => setEmail(e.target.value)}
                                                    onInput={() => error && setError(null)}
                                                    required
                                                    placeholder="you@warmbly.com"
                                                    aria-invalid={Boolean(error)}
                                                    className={cn("h-10", FOCUS_RED)}
                                                />
                                            </div>

                                            <div className="space-y-1.5">
                                                <Label htmlFor="password">Password</Label>
                                                <div className="relative">
                                                    <Input
                                                        id="password"
                                                        type={showPassword ? "text" : "password"}
                                                        autoComplete="current-password"
                                                        value={password}
                                                        onChange={(e) => setPassword(e.target.value)}
                                                        onInput={() => error && setError(null)}
                                                        required
                                                        placeholder="••••••••••••"
                                                        aria-invalid={Boolean(error)}
                                                        className={cn("h-10 pr-10", FOCUS_RED)}
                                                    />
                                                    <button
                                                        type="button"
                                                        onClick={() => setShowPassword((v) => !v)}
                                                        aria-label={showPassword ? "Hide password" : "Show password"}
                                                        className="absolute right-0.5 top-1/2 grid size-9 -translate-y-1/2 place-items-center rounded-md text-muted-foreground transition-colors hover:text-foreground"
                                                    >
                                                        {showPassword ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
                                                    </button>
                                                </div>
                                            </div>

                                            {errorLine}

                                            <Button
                                                type="submit"
                                                disabled={busy}
                                                className="h-10 w-full text-white hover:opacity-90"
                                                style={{ backgroundColor: ACCENT }}
                                            >
                                                {busy ? (
                                                    <>
                                                        <Loader2 className="size-4 animate-spin" />
                                                        Signing in…
                                                    </>
                                                ) : (
                                                    "Sign in"
                                                )}
                                            </Button>

                                            <TurnstileModal visible={captcha} onToken={onToken} onError={onCaptchaError} />
                                        </form>
                                    </motion.div>
                                ) : (
                                    <motion.div
                                        key="code"
                                        initial={{ opacity: 0, x: 14 }}
                                        animate={{ opacity: 1, x: 0 }}
                                        exit={{ opacity: 0, x: -14 }}
                                        transition={SWAP_TRANSITION}
                                        className="text-center"
                                    >
                                        <div className="mx-auto mt-1 grid size-12 place-items-center rounded-xl bg-red-50 text-red-600">
                                            {phase === "twofa" ? <ShieldCheck className="size-6" /> : <MailCheck className="size-6" />}
                                        </div>
                                        <h1 className="mt-4 text-[20px] font-semibold tracking-tight">
                                            {phase === "twofa" ? "Two-factor authentication" : "Check your email"}
                                        </h1>
                                        <p className="mt-1 text-[13px] text-muted-foreground">
                                            {phase === "twofa" ? (
                                                <>Enter the 6-digit code from your authenticator app, or a recovery code.</>
                                            ) : (
                                                <>
                                                    We sent a 6-digit code to{" "}
                                                    <span className="font-medium text-foreground">{email}</span>.
                                                </>
                                            )}
                                        </p>

                                        <form className="mt-6 flex flex-col items-center gap-4" onSubmit={(e) => { e.preventDefault(); phase === "twofa" ? submitTwoFA(code) : submitCode(code); }}>
                                            <InputOTP
                                                maxLength={6}
                                                value={code}
                                                onChange={(v) => {
                                                    setCode(v);
                                                    if (error) setError(null);
                                                    if (v.length === 6) { if (phase === "twofa") submitTwoFA(v); else submitCode(v); }
                                                }}
                                                disabled={submitting}
                                                containerClassName="gap-2"
                                                aria-invalid={Boolean(error)}
                                            >
                                                <InputOTPGroup className="gap-2">
                                                    {[0, 1, 2, 3, 4, 5].map((i) => (
                                                        <InputOTPSlot
                                                            key={i}
                                                            index={i}
                                                            className={cn(
                                                                "!h-12 !w-11 !rounded-md !border !border-border !text-[16px] !font-semibold !shadow-none",
                                                                "data-[active=true]:!border-red-400 data-[active=true]:!ring-2 data-[active=true]:!ring-red-100",
                                                                error && "!border-red-300",
                                                            )}
                                                        />
                                                    ))}
                                                </InputOTPGroup>
                                            </InputOTP>

                                            {errorLine}

                                            <div className={cn("text-[12px] text-muted-foreground", phase === "twofa" && "hidden")}>
                                                {resendIn > 0 ? (
                                                    <span>
                                                        Resend code in <span className="font-medium text-foreground">{resendIn}s</span>
                                                    </span>
                                                ) : (
                                                    <button
                                                        type="button"
                                                        onClick={onResend}
                                                        disabled={busy}
                                                        className="font-medium underline-offset-4 hover:underline disabled:opacity-50"
                                                        style={{ color: ACCENT }}
                                                    >
                                                        Resend code
                                                    </button>
                                                )}
                                            </div>

                                            {import.meta.env.DEV && phase === "code" && (
                                                <p className="text-[11px] text-muted-foreground">
                                                    Dev: the code is in mailpit →{" "}
                                                    <a
                                                        href="http://localhost:18025"
                                                        target="_blank"
                                                        rel="noreferrer"
                                                        className="underline underline-offset-2 hover:text-foreground"
                                                    >
                                                        localhost:18025
                                                    </a>
                                                </p>
                                            )}

                                            <Button
                                                type="submit"
                                                disabled={busy || code.length < 6}
                                                className="h-10 w-full text-white hover:opacity-90"
                                                style={{ backgroundColor: ACCENT }}
                                            >
                                                {submitting ? (
                                                    <>
                                                        <Loader2 className="size-4 animate-spin" />
                                                        Verifying…
                                                    </>
                                                ) : (
                                                    "Verify & sign in"
                                                )}
                                            </Button>

                                            <button
                                                type="button"
                                                onClick={backToCredentials}
                                                className="inline-flex items-center gap-1 text-[12px] text-muted-foreground underline-offset-4 transition-colors hover:text-foreground hover:underline"
                                            >
                                                <ArrowLeft className="size-3" />
                                                Use a different account
                                            </button>
                                        </form>
                                    </motion.div>
                                )}
                            </AnimatePresence>
                        </div>
                    </div>
                </div>

                {/* Footer */}
                <div className="mt-5 space-y-2 text-center">
                    <p className="text-[11px] text-muted-foreground">
                        Every admin action is logged to{" "}
                        <code className="font-mono">admin_audit_logs</code>.
                    </p>
                    <a
                        href={DASHBOARD_URL}
                        className="inline-block text-[11px] text-muted-foreground underline-offset-4 transition-colors hover:text-foreground hover:underline"
                    >
                        ← Back to the Warmbly dashboard
                    </a>
                </div>
            </div>
        </div>
    );
}
