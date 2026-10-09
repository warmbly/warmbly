// Admin sign-in: two steps, matching the backend's email-code login.
//
// Step 1: email + password (+ Turnstile) -> /auth/login, which emails a
//   one-time code and returns a short-lived session.
// Step 2: the emailed 6-digit code -> /auth/login/confirm, which returns the
//   real access/refresh token pair.
//
// Step 3, when the account has a second factor: a TOTP or recovery code ->
//   /auth/2fa/verify.
//
// A centred column with no card; the steps slide inside a fixed-height frame.
// In dev the captcha is bypassed and the code lands in mailpit (:18025).

import { useEffect, useRef, useState, type FormEvent } from "react";
import { useNavigate, useLocation } from "react-router-dom";
import { AnimatePresence, motion, type Transition } from "motion/react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Eye, EyeOff, Loader2, ArrowLeft, ArrowRight } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { InputOTP, InputOTPGroup, InputOTPSlot } from "@/components/ui/input-otp";
import { Logo } from "@/components/Logo";
import { StatusBadge } from "@/components/ui/kit";
import { TurnstileModal } from "@/components/captcha/TurnstileModal";
import { getAuthConfig, login, loginConfirm, verifyTwoFA } from "@/lib/api/client/auth";
import type { LoginResponse } from "@/lib/api/models/auth";
import { setToken } from "@/lib/auth/storage";
import { APIError } from "@/lib/api/client";
import { DASHBOARD_URL, ENV_LABEL } from "@/lib/env";
import { cn } from "@/lib/utils";

const RESEND_SECONDS = 45;
const SWAP_TRANSITION: Transition = { duration: 0.26, ease: [0.16, 1, 0.3, 1] };

function errorMessage(err: unknown): string {
    return err instanceof APIError
        ? err.message
        : "Sign-in failed. Check your credentials or contact an admin.";
}

export default function LoginPage() {
    const nav = useNavigate();
    const queryClient = useQueryClient();
    const loc = useLocation();
    const [phase, setPhase] = useState<"credentials" | "code" | "twofa">("credentials");
    const [pendingToken, setPendingToken] = useState("");
    const [email, setEmail] = useState("");
    const [password, setPassword] = useState("");
    const [showPassword, setShowPassword] = useState(false);
    const [session, setSession] = useState("");
    const [code, setCode] = useState("");
    const [useRecovery, setUseRecovery] = useState(false);
    const [captcha, setCaptcha] = useState(false);
    // Whether this deployment verifies a captcha token at all. null until the
    // answer arrives: nothing is mounted before then, so an instance with no
    // route to Cloudflare does not raise a widget error on a screen nobody has
    // submitted. A failed fetch resolves to true rather than leaving it
    // pending, so the check is never skipped on an instance that enforces it.
    const [captchaRequired, setCaptchaRequired] = useState<boolean | null>(null);
    const [submitting, setSubmitting] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [resendIn, setResendIn] = useState(0);
    const loginStartInFlight = useRef(false);
    const confirmInFlight = useRef(false);

    const busy = submitting || captcha;

    // What this deployment can do, read before anything mounts a widget. A
    // self-host with CAPTCHA_PROVIDER=none cannot reach Cloudflare, so an
    // invisible Turnstile there can only time out and lock the operator out.
    useEffect(() => {
        let cancelled = false;
        getAuthConfig()
            .then((cfg) => {
                if (!cancelled) setCaptchaRequired(cfg.captcha);
            })
            .catch(() => {
                // Fail safe: assume the check is enforced.
                if (!cancelled) setCaptchaRequired(true);
            });
        return () => {
            cancelled = true;
        };
    }, []);

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
                    queryClient.clear();
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
            queryClient.clear();
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
            queryClient.clear();
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
        setUseRecovery(false);
        setSession("");
        setCode("");
        setError(null);
    }

    const step = phase === "credentials" ? 0 : phase === "code" ? 1 : 2;

    const errorLine = error && (
        <p role="alert" className="text-center text-[13px] text-red-600 dark:text-red-400">
            {error}
        </p>
    );

    const field =
        "h-11 rounded-lg px-3.5 text-[14px] placeholder:text-subtle-foreground dark:bg-white/[0.03]";

    const codeSlots = (
        <InputOTP
            maxLength={6}
            value={code}
            autoFocus
            onChange={(v) => {
                setCode(v);
                if (error) setError(null);
                if (v.length === 6) {
                    if (phase === "twofa") submitTwoFA(v);
                    else submitCode(v);
                }
            }}
            disabled={submitting}
            containerClassName="justify-center"
            aria-invalid={Boolean(error)}
        >
            <InputOTPGroup className="gap-2">
                {[0, 1, 2, 3, 4, 5].map((i) => (
                    <InputOTPSlot
                        key={i}
                        index={i}
                        className={cn(
                            "!h-12 !w-11 !rounded-lg !border !border-input !bg-transparent !text-[20px] !font-medium !text-foreground !shadow-none tabular-nums dark:!bg-white/[0.03]",
                            "data-[active=true]:!border-foreground/50 data-[active=true]:!ring-[3px] data-[active=true]:!ring-foreground/[0.08]",
                            error && "!border-red-500/60",
                        )}
                    />
                ))}
            </InputOTPGroup>
        </InputOTP>
    );

    const slide = {
        initial: { opacity: 0, x: 16 },
        animate: { opacity: 1, x: 0 },
        exit: { opacity: 0, x: -16 },
        transition: SWAP_TRANSITION,
    };

    return (
        <div className="flex min-h-dvh flex-col bg-sidebar text-foreground">
            <header className="flex h-14 shrink-0 items-center justify-between px-5 sm:px-8">
                <a
                    href={DASHBOARD_URL}
                    className="inline-flex items-center gap-1.5 rounded-md px-1.5 py-1 text-[13px] text-muted-foreground transition-colors hover:text-foreground"
                >
                    <ArrowLeft className="size-3.5" />
                    Dashboard
                </a>
                {ENV_LABEL !== "development" && (
                    <StatusBadge tone={ENV_LABEL === "production" ? "danger" : "warning"} dot>
                        {ENV_LABEL === "production" ? "Production" : "Staging"}
                    </StatusBadge>
                )}
            </header>

            <main className="flex flex-1 items-center justify-center px-5 pb-20">
                <div className="w-full max-w-[340px]">
                    <div className="flex flex-col items-center">
                        <Logo className="size-11 text-foreground" />
                        {/* Where the operator is in the sign-in. */}
                        <div className="mt-7 flex items-center gap-1.5" aria-hidden>
                            {[0, 1, 2].map((i) => (
                                <span
                                    key={i}
                                    className={cn(
                                        "h-[3px] rounded-full transition-all duration-300",
                                        i === step ? "w-6 bg-foreground" : i < step ? "w-3 bg-foreground/40" : "w-3 bg-foreground/12",
                                    )}
                                />
                            ))}
                        </div>
                    </div>

                    <div className="relative mt-6 min-h-[330px]">
                        <AnimatePresence mode="wait" initial={false}>
                            {phase === "credentials" ? (
                                <motion.div key="credentials" {...slide}>
                                    <h1 className="text-center text-[24px] leading-tight font-semibold tracking-[-0.025em]">
                                        Sign in to Warmbly
                                    </h1>
                                    <p className="mt-2 text-center text-[14px] text-muted-foreground">
                                        Admin access for staff only.
                                    </p>

                                    <form className="mt-8 space-y-3" onSubmit={onCredentials}>
                                        <label htmlFor="email" className="sr-only">
                                            Email
                                        </label>
                                        <Input
                                            id="email"
                                            type="email"
                                            autoComplete="email"
                                            autoFocus
                                            value={email}
                                            onChange={(e) => setEmail(e.target.value)}
                                            onInput={() => error && setError(null)}
                                            required
                                            placeholder="Email address"
                                            aria-invalid={Boolean(error)}
                                            className={field}
                                        />
                                        <label htmlFor="password" className="sr-only">
                                            Password
                                        </label>
                                        <div className="relative">
                                            <Input
                                                id="password"
                                                type={showPassword ? "text" : "password"}
                                                data-ph-mask=""
                                                autoComplete="current-password"
                                                value={password}
                                                onChange={(e) => setPassword(e.target.value)}
                                                onInput={() => error && setError(null)}
                                                required
                                                placeholder="Password"
                                                aria-invalid={Boolean(error)}
                                                className={cn(field, "pr-11")}
                                            />
                                            <button
                                                type="button"
                                                onClick={() => setShowPassword((v) => !v)}
                                                aria-label={showPassword ? "Hide password" : "Show password"}
                                                className="absolute right-1.5 top-1/2 grid size-8 -translate-y-1/2 place-items-center rounded-md text-subtle-foreground transition-colors outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/40"
                                            >
                                                {showPassword ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
                                            </button>
                                        </div>

                                        {errorLine}

                                        <Button type="submit" disabled={busy} className="h-11 w-full rounded-lg text-[14px]">
                                            {busy ? (
                                                <>
                                                    <Loader2 className="size-4 animate-spin" />
                                                    Signing in…
                                                </>
                                            ) : (
                                                <>
                                                    Continue
                                                    <ArrowRight className="size-4" />
                                                </>
                                            )}
                                        </Button>

                                    </form>
                                </motion.div>
                            ) : (
                                <motion.div key={phase} {...slide}>
                                    <h1 className="text-center text-[24px] leading-tight font-semibold tracking-[-0.025em]">
                                        {phase === "twofa" ? "Two-factor authentication" : "Check your email"}
                                    </h1>
                                    <p className="mt-2 text-center text-[14px] leading-relaxed text-muted-foreground">
                                        {phase === "twofa" ? (
                                            useRecovery ? (
                                                "Enter one of the recovery codes you saved when you turned on 2FA."
                                            ) : (
                                                "Enter the 6-digit code from your authenticator app."
                                            )
                                        ) : (
                                            <>
                                                We sent a 6-digit code to
                                                <br />
                                                <span className="font-medium text-foreground">{email}</span>
                                            </>
                                        )}
                                    </p>

                                    <form
                                        className="mt-8 space-y-4"
                                        onSubmit={(e) => {
                                            e.preventDefault();
                                            if (phase === "twofa") submitTwoFA(code.trim());
                                            else submitCode(code);
                                        }}
                                    >
                                        {phase === "twofa" && useRecovery ? (
                                            <Input
                                                autoFocus
                                                value={code}
                                                onChange={(e) => {
                                                    setCode(e.target.value);
                                                    if (error) setError(null);
                                                }}
                                                autoComplete="one-time-code"
                                                spellCheck={false}
                                                placeholder="xxxxx-xxxxx"
                                                aria-label="Recovery code"
                                                aria-invalid={Boolean(error)}
                                                className={cn(field, "text-center font-mono tracking-[0.12em]")}
                                            />
                                        ) : (
                                            codeSlots
                                        )}

                                        {errorLine}

                                        <Button
                                            type="submit"
                                            disabled={busy || code.trim().length < 6}
                                            className="h-11 w-full rounded-lg text-[14px]"
                                        >
                                            {submitting ? (
                                                <>
                                                    <Loader2 className="size-4 animate-spin" />
                                                    Verifying…
                                                </>
                                            ) : (
                                                "Verify and sign in"
                                            )}
                                        </Button>

                                        <div className="flex flex-col items-center gap-2 pt-1 text-[13px]">
                                            {phase === "code" &&
                                                (resendIn > 0 ? (
                                                    <span className="text-muted-foreground">
                                                        Resend code in{" "}
                                                        <span className="tabular-nums text-foreground">{resendIn}s</span>
                                                    </span>
                                                ) : (
                                                    <button
                                                        type="button"
                                                        onClick={onResend}
                                                        disabled={busy}
                                                        className="text-muted-foreground transition-colors hover:text-foreground disabled:opacity-50"
                                                    >
                                                        Didn't get it? <span className="text-foreground">Resend code</span>
                                                    </button>
                                                ))}
                                            {phase === "twofa" && (
                                                <button
                                                    type="button"
                                                    onClick={() => {
                                                        setUseRecovery((v) => !v);
                                                        setCode("");
                                                        setError(null);
                                                    }}
                                                    className="text-muted-foreground transition-colors hover:text-foreground"
                                                >
                                                    {useRecovery ? "Use your authenticator app" : "Use a recovery code"}
                                                </button>
                                            )}
                                            <button
                                                type="button"
                                                onClick={backToCredentials}
                                                className="text-muted-foreground transition-colors hover:text-foreground"
                                            >
                                                Use a different account
                                            </button>
                                        </div>

                                        {import.meta.env.DEV && phase === "code" && (
                                            <p className="text-center text-[12px] text-subtle-foreground">
                                                Dev: the code is in{" "}
                                                <a
                                                    href="http://localhost:18025"
                                                    target="_blank"
                                                    rel="noreferrer"
                                                    className="underline underline-offset-2 hover:text-foreground"
                                                >
                                                    Mailpit
                                                </a>
                                            </p>
                                        )}
                                    </form>
                                </motion.div>
                            )}
                        </AnimatePresence>
                    </div>
                    {/* Outside the steps: Resend on the code step needs a token too. */}
                    <div className="flex justify-center">
                        <TurnstileModal
                            visible={captcha}
                            required={captchaRequired}
                            onToken={onToken}
                            onError={onCaptchaError}
                        />
                    </div>
                </div>
            </main>

            <footer className="shrink-0 pb-6 text-center text-[12px] text-subtle-foreground">
                Every admin action is recorded in the audit log.
            </footer>
        </div>
    );
}
