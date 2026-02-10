import React, { useState, useRef, useEffect, useCallback } from "react";
import { Link, useNavigate, useLocation } from "react-router-dom";
import { motion, AnimatePresence } from "motion/react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import toast from "react-hot-toast";
import { ArrowLeft } from "lucide-react";

import { TurnstileModal } from "@/components/captcha/TurnstileModal";
import AuthButton from "@/components/auth/button";
import ExternalLogin from "@/components/auth/external";
import { InputOTP, InputOTPGroup, InputOTPSlot } from "@/components/ui/input-otp";

import useLogin from "@/lib/api/hooks/auth/useLogin";
import useLoginConfirm from "@/lib/api/hooks/auth/useLoginConfirm";
import useRegister from "@/lib/api/hooks/auth/useRegister";
import useRegisterConfirm from "@/lib/api/hooks/auth/useRegisterConfirm";
import { saveTokens } from "@/lib/auth";
import { WEBSITE_URL } from "@/lib/information";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

/* ── Schemas ─────────────────────── */

const emailSchema = z.object({
    email: z.string().email("Please enter a valid email address"),
});

const signInSchema = z.object({
    password: z.string().min(1, "Password is required"),
});

const signUpSchema = z.object({
    password: z.string()
        .min(8, "At least 8 characters")
        .regex(/[A-Z]/, "One uppercase letter required")
        .regex(/[a-z]/, "One lowercase letter required")
        .regex(/[0-9]/, "One number required"),
    confirmPassword: z.string(),
    acceptTerms: z.boolean(),
}).refine((d) => d.password === d.confirmPassword, {
    message: "Passwords don't match",
    path: ["confirmPassword"],
}).refine((d) => d.acceptTerms === true, {
    message: "You must accept the terms",
    path: ["acceptTerms"],
});

/* ── Animation ─────────────────────── */

const slideVariants = {
    enter: (dir: number) => ({ x: dir > 0 ? 260 : -260, opacity: 0 }),
    center: { x: 0, opacity: 1 },
    exit: (dir: number) => ({ x: dir > 0 ? -260 : 260, opacity: 0 }),
};

const slideTrans = {
    x: { type: "spring" as const, stiffness: 320, damping: 32 },
    opacity: { duration: 0.18 },
};

/* ── Shared input class ─────────────────────── */

const INPUT = "w-full h-11 rounded-lg border border-sky-200 bg-white px-4 text-[15px] text-slate-800 placeholder:text-slate-300 outline-none transition-all duration-200 focus:border-sky-400 focus:ring-4 focus:ring-sky-400/15";

/* ── Password strength ─────────────────────── */

function PasswordStrength({ password }: { password: string }) {
    if (!password) return null;

    let score = 0;
    if (password.length >= 8) score++;
    if (password.length >= 12) score++;
    if (/[A-Z]/.test(password)) score++;
    if (/[a-z]/.test(password)) score++;
    if (/[0-9]/.test(password)) score++;

    const level = score <= 2 ? "weak" : score <= 3 ? "fair" : score <= 4 ? "good" : "strong";
    const colors = { weak: "bg-red-400", fair: "bg-amber-400", good: "bg-sky-400", strong: "bg-emerald-400" };
    const widths = { weak: "25%", fair: "50%", good: "75%", strong: "100%" };
    const labels = { weak: "Weak", fair: "Fair", good: "Good", strong: "Strong" };

    return (
        <div className="space-y-1">
            <div className="h-1 w-full bg-slate-100 rounded-full overflow-hidden">
                <motion.div
                    className={`h-full rounded-full ${colors[level]}`}
                    initial={{ width: 0 }}
                    animate={{ width: widths[level] }}
                    transition={{ duration: 0.35, ease: "easeOut" }}
                />
            </div>
            <p className="text-xs text-slate-400">{labels[level]} password</p>
        </div>
    );
}

/* ── Error fade ─────────────────────── */

function FieldError({ message }: { message?: string }) {
    return (
        <AnimatePresence>
            {message && (
                <motion.p
                    initial={{ opacity: 0, y: -4 }}
                    animate={{ opacity: 1, y: 0 }}
                    exit={{ opacity: 0, y: -4 }}
                    transition={{ duration: 0.2 }}
                    className="text-xs text-rose-500 mt-1 pl-0.5"
                >
                    {message}
                </motion.p>
            )}
        </AnimatePresence>
    );
}

/* ── Countdown hook ─────────────────────── */

function useCountdown(seconds: number) {
    const [count, setCount] = useState(seconds);
    const [active, setActive] = useState(true);
    useEffect(() => {
        if (!active || count <= 0) return;
        const id = setInterval(() => setCount((c) => c - 1), 1000);
        return () => clearInterval(id);
    }, [active, count]);
    const reset = useCallback(() => { setCount(seconds); setActive(true); }, [seconds]);
    return { count, expired: count <= 0, reset };
}

/* ═══════════════════════════════════════════
   Main multi-step auth page
   ═══════════════════════════════════════════ */

type Step = "email" | "signin" | "signup" | "verify";

export default function LoginPage() {
    const navigate = useNavigate();
    const location = useLocation();

    /* State */
    const [step, setStep] = useState<Step>("email");
    const [mode, setMode] = useState<"signin" | "signup">(() =>
        location.pathname.includes("/register") ? "signup" : "signin"
    );

    /* Mode change — update URL without remounting */
    const handleModeChange = (m: "signin" | "signup") => {
        setMode(m);
        window.history.replaceState(null, "", m === "signin" ? "/auth/login" : "/auth/register");
    };
    const [email, setEmail] = useState("");
    const [password, setPassword] = useState("");
    const [session, setSession] = useState("");
    const [direction, setDirection] = useState(0);
    const [captcha, setCaptcha] = useState(false);
    const pendingRef = useRef<((token: string) => Promise<void>) | null>(null);

    /* Mutations */
    const loginMutation = useLogin();
    const registerMutation = useRegister();
    const loginConfirmMutation = useLoginConfirm();
    const registerConfirmMutation = useRegisterConfirm();

    const pending =
        loginMutation.isPending || registerMutation.isPending ||
        loginConfirmMutation.isPending || registerConfirmMutation.isPending;

    /* Step nav */
    const goTo = (s: Step, dir: 1 | -1 = 1) => { setDirection(dir); setStep(s); };

    /* Captcha helper */
    const withCaptcha = (fn: (token: string) => Promise<void>) => {
        pendingRef.current = fn;
        setCaptcha(true);
    };
    const onToken = async (token: string) => {
        setCaptcha(false);
        if (pendingRef.current) {
            await pendingRef.current(token);
            pendingRef.current = null;
        }
    };

    /* ── Step 1: Email ─────────────────────── */
    const handleEmailContinue = (data: z.infer<typeof emailSchema>) => {
        setEmail(data.email);
        goTo(mode === "signin" ? "signin" : "signup");
    };

    /* ── Step 2a: Sign in ─────────────────────── */
    const handleSignIn = (data: z.infer<typeof signInSchema>) => {
        setPassword(data.password);
        withCaptcha(async (token) => {
            try {
                const res = await loginMutation.mutateAsync({ email, password: data.password, turnstile: token });
                setSession(res.session);
                goTo("verify");
            } catch (e) { toast.error(buildError(e as AppError)); }
        });
    };

    /* ── Step 2b: Sign up ─────────────────────── */
    const handleSignUp = (data: z.infer<typeof signUpSchema>) => {
        setPassword(data.password);
        withCaptcha(async (token) => {
            try {
                const res = await registerMutation.mutateAsync({ email, password: data.password, turnstile: token });
                setSession(res.session);
                goTo("verify");
            } catch (e) { toast.error(buildError(e as AppError)); }
        });
    };

    /* ── Step 3: OTP ─────────────────────── */
    const handleVerify = (code: string) => {
        if (code.length !== 6) return;
        withCaptcha(async (token) => {
            try {
                if (mode === "signin") {
                    const res = await loginConfirmMutation.mutateAsync({ session, code, turnstile: token });
                    saveTokens(res as unknown as Record<string, string>);
                    navigate("/app/emails");
                } else {
                    await registerConfirmMutation.mutateAsync({ session, code, turnstile: token });
                    toast.success("Account created! Please sign in.");
                    handleModeChange("signin");
                    goTo("email", -1);
                }
            } catch (e) { toast.error(buildError(e as AppError)); }
        });
    };

    /* ── Resend OTP ─────────────────────── */
    const handleResend = useCallback(() => {
        withCaptcha(async (token) => {
            try {
                const mutation = mode === "signin" ? loginMutation : registerMutation;
                const res = await mutation.mutateAsync({ email, password, turnstile: token });
                setSession(res.session);
                toast.success("Code resent!");
            } catch (e) { toast.error(buildError(e as AppError)); }
        });
    }, [mode, email, password, loginMutation, registerMutation]);

    return (
        <div className="relative overflow-hidden p-4 -m-4">
            <AnimatePresence mode="wait" custom={direction}>
                {step === "email" && (
                    <MotionWrap key="email" direction={direction}>
                        <EmailStep
                            mode={mode}
                            onModeChange={handleModeChange}
                            defaultEmail={email}
                            onContinue={handleEmailContinue}
                        />
                    </MotionWrap>
                )}
                {step === "signin" && (
                    <MotionWrap key="signin" direction={direction}>
                        <SignInStep
                            email={email}
                            pending={pending}
                            onBack={() => goTo("email", -1)}
                            onSubmit={handleSignIn}
                        />
                    </MotionWrap>
                )}
                {step === "signup" && (
                    <MotionWrap key="signup" direction={direction}>
                        <SignUpStep
                            email={email}
                            pending={pending}
                            onBack={() => goTo("email", -1)}
                            onSubmit={handleSignUp}
                        />
                    </MotionWrap>
                )}
                {step === "verify" && (
                    <MotionWrap key="verify" direction={direction}>
                        <VerifyStep
                            email={email}
                            mode={mode}
                            pending={pending}
                            onBack={() => goTo(mode === "signin" ? "signin" : "signup", -1)}
                            onSubmit={handleVerify}
                            onResend={handleResend}
                        />
                    </MotionWrap>
                )}
            </AnimatePresence>

            <TurnstileModal visible={captcha} onToken={onToken} />
        </div>
    );
}

/* ── Motion wrapper ─────────────────────── */

function MotionWrap({ children, direction }: { children: React.ReactNode; direction: number }) {
    return (
        <motion.div
            custom={direction}
            variants={slideVariants}
            initial="enter"
            animate="center"
            exit="exit"
            transition={slideTrans}
        >
            {children}
        </motion.div>
    );
}

/* ── Back button ─────────────────────── */

function BackButton({ onClick }: { onClick: () => void }) {
    return (
        <button
            type="button"
            onClick={onClick}
            className="inline-flex items-center gap-1 text-sm text-slate-400 hover:text-slate-600 transition-colors mb-5 cursor-pointer"
        >
            <ArrowLeft className="w-4 h-4" />
            Back
        </button>
    );
}

/* ═══════════════════════════════════════════
   Step components
   ═══════════════════════════════════════════ */

/* ── Email step ─────────────────────── */

function EmailStep({
    mode,
    onModeChange,
    defaultEmail,
    onContinue,
}: {
    mode: "signin" | "signup";
    onModeChange: (m: "signin" | "signup") => void;
    defaultEmail: string;
    onContinue: (data: z.infer<typeof emailSchema>) => void;
}) {
    const { register, handleSubmit, formState: { errors } } = useForm<z.infer<typeof emailSchema>>({
        resolver: zodResolver(emailSchema),
        defaultValues: { email: defaultEmail },
    });

    return (
        <div className="space-y-6">
            <div className="text-center overflow-hidden">
                <AnimatePresence mode="wait">
                    <motion.div key={mode}>
                        <h1 className="font-serif text-[32px] text-slate-800 tracking-tight leading-tight">
                            {(mode === "signin" ? ["Welcome", "back"] : ["Get", "started"]).map((word, i) => (
                                <motion.span
                                    key={word + i}
                                    className="inline-block"
                                    initial={{ opacity: 0, y: 20, filter: "blur(4px)" }}
                                    animate={{ opacity: 1, y: 0, filter: "blur(0px)" }}
                                    exit={{ opacity: 0, y: -10 }}
                                    transition={{ delay: i * 0.1, duration: 0.3 }}
                                >
                                    {word}{i === 0 ? "\u00A0" : ""}
                                </motion.span>
                            ))}
                        </h1>
                        <motion.p
                            className="text-sm text-slate-400 mt-1.5"
                            initial={{ opacity: 0 }}
                            animate={{ opacity: 1 }}
                            exit={{ opacity: 0 }}
                            transition={{ delay: 0.15, duration: 0.25 }}
                        >
                            {mode === "signin" ? "Sign in to your account" : "Create your free account"}
                        </motion.p>
                    </motion.div>
                </AnimatePresence>
            </div>

            {/* Mode toggle */}
            <div className="relative flex rounded-lg bg-sky-50 p-1">
                {(["signin", "signup"] as const).map((m) => (
                    <button
                        key={m}
                        type="button"
                        onClick={() => onModeChange(m)}
                        className={`flex-1 relative py-2 text-sm font-medium rounded-md cursor-pointer transition-colors duration-200 ${
                            mode === m ? "text-slate-800" : "text-slate-400 hover:text-slate-600"
                        }`}
                    >
                        {mode === m && (
                            <motion.span
                                layoutId="auth-mode-pill"
                                className="absolute inset-0 rounded-md bg-white shadow-sm"
                                transition={{ type: "spring", stiffness: 500, damping: 35 }}
                            />
                        )}
                        <span className="relative z-10">{m === "signin" ? "Sign in" : "Create account"}</span>
                    </button>
                ))}
            </div>

            <form onSubmit={handleSubmit(onContinue)} className="space-y-4">
                <div>
                    <label className="text-sm font-medium text-slate-600 pl-0.5">Email address</label>
                    <input
                        type="email"
                        placeholder="name@company.com"
                        className={INPUT}
                        autoFocus
                        {...register("email")}
                    />
                    <FieldError message={errors.email?.message} />
                </div>

                <AuthButton loading={false}>Continue</AuthButton>
            </form>

            {/* Divider */}
            <div className="flex items-center gap-3">
                <div className="flex-1 h-px bg-sky-100" />
                <span className="text-xs text-slate-300 font-medium">or continue with</span>
                <div className="flex-1 h-px bg-sky-100" />
            </div>

            <ExternalLogin />
        </div>
    );
}

/* ── Sign-in step ─────────────────────── */

function SignInStep({
    email,
    pending,
    onBack,
    onSubmit,
}: {
    email: string;
    pending: boolean;
    onBack: () => void;
    onSubmit: (data: z.infer<typeof signInSchema>) => void;
}) {
    const { register, handleSubmit, formState: { errors } } = useForm<z.infer<typeof signInSchema>>({
        resolver: zodResolver(signInSchema),
    });

    return (
        <div>
            <BackButton onClick={onBack} />
            <div className="text-center mb-6">
                <h1 className="font-serif text-[32px] text-slate-800 tracking-tight leading-tight">Sign in</h1>
                <p className="text-sm text-slate-400 mt-1.5">
                    Welcome back, <span className="text-slate-600 font-medium">{email}</span>
                </p>
            </div>

            <form onSubmit={handleSubmit(onSubmit)} className="space-y-4">
                <div>
                    <div className="flex items-center justify-between mb-1">
                        <label className="text-sm font-medium text-slate-600 pl-0.5">Password</label>
                        <Link to="/auth/reset-password" className="text-xs text-sky-500 hover:text-sky-600 font-medium transition-colors">
                            Forgot password?
                        </Link>
                    </div>
                    <input
                        type="password"
                        placeholder="Enter your password"
                        className={INPUT}
                        autoFocus
                        {...register("password")}
                    />
                    <FieldError message={errors.password?.message} />
                </div>

                <div className="pt-1">
                    <AuthButton loading={pending}>Sign in</AuthButton>
                </div>
            </form>
        </div>
    );
}

/* ── Sign-up step ─────────────────────── */

function SignUpStep({
    email,
    pending,
    onBack,
    onSubmit,
}: {
    email: string;
    pending: boolean;
    onBack: () => void;
    onSubmit: (data: z.infer<typeof signUpSchema>) => void;
}) {
    const { register, handleSubmit, watch, formState: { errors } } = useForm<z.infer<typeof signUpSchema>>({
        resolver: zodResolver(signUpSchema),
        defaultValues: { password: "", confirmPassword: "", acceptTerms: false },
    });
    const pw = watch("password");
    const termsChecked = watch("acceptTerms");

    return (
        <div>
            <BackButton onClick={onBack} />
            <div className="text-center mb-6">
                <h1 className="font-serif text-[32px] text-slate-800 tracking-tight leading-tight">Create account</h1>
                <p className="text-sm text-slate-400 mt-1.5">
                    Setting up <span className="text-slate-600 font-medium">{email}</span>
                </p>
            </div>

            <form onSubmit={handleSubmit(onSubmit)} className="space-y-4">
                <div>
                    <label className="text-sm font-medium text-slate-600 pl-0.5">Password</label>
                    <input type="password" placeholder="Create a password" className={INPUT} autoFocus {...register("password")} />
                    <FieldError message={errors.password?.message} />
                    <div className="mt-2">
                        <PasswordStrength password={pw} />
                    </div>
                </div>

                <div>
                    <label className="text-sm font-medium text-slate-600 pl-0.5">Confirm password</label>
                    <input type="password" placeholder="Confirm your password" className={INPUT} {...register("confirmPassword")} />
                    <FieldError message={errors.confirmPassword?.message} />
                </div>

                {/* Terms */}
                <label className="flex items-start gap-3 pt-0.5 cursor-pointer">
                    <div className="relative mt-0.5 shrink-0">
                        <input type="checkbox" className="peer sr-only" {...register("acceptTerms")} />
                        <div className={`size-[18px] rounded-md border-2 transition-all duration-200 flex items-center justify-center ${termsChecked ? "bg-sky-500 border-sky-500" : "border-sky-200 bg-white"}`}>
                            {termsChecked && (
                                <svg className="size-3 text-white" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={3}>
                                    <path strokeLinecap="round" strokeLinejoin="round" d="M5 13l4 4L19 7" />
                                </svg>
                            )}
                        </div>
                    </div>
                    <span className="text-[13px] text-slate-400 leading-relaxed">
                        I agree to the{" "}
                        <a href={`${WEBSITE_URL}/terms`} target="_blank" rel="noopener noreferrer" className="text-sky-500 hover:text-sky-600 font-medium transition-colors">
                            Terms of Service
                        </a>
                        {" "}and{" "}
                        <a href={`${WEBSITE_URL}/privacy`} target="_blank" rel="noopener noreferrer" className="text-sky-500 hover:text-sky-600 font-medium transition-colors">
                            Privacy Policy
                        </a>
                    </span>
                </label>
                <FieldError message={errors.acceptTerms?.message} />

                <div className="pt-1">
                    <AuthButton loading={pending}>Create account</AuthButton>
                </div>
            </form>
        </div>
    );
}

/* ── Verify step ─────────────────────── */

function VerifyStep({
    email,
    mode,
    pending,
    onBack,
    onSubmit,
    onResend,
}: {
    email: string;
    mode: "signin" | "signup";
    pending: boolean;
    onBack: () => void;
    onSubmit: (code: string) => void;
    onResend: () => void;
}) {
    const [otp, setOtp] = useState("");
    const { count, expired, reset } = useCountdown(60);

    const handleResend = () => {
        onResend();
        reset();
    };

    return (
        <div>
            <BackButton onClick={onBack} />
            <div className="text-center mb-6">
                <div className="mx-auto w-14 h-14 rounded-2xl bg-sky-50 flex items-center justify-center mb-4">
                    <svg className="w-7 h-7 text-sky-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
                        <path strokeLinecap="round" strokeLinejoin="round" d="M21.75 6.75v10.5a2.25 2.25 0 01-2.25 2.25h-15a2.25 2.25 0 01-2.25-2.25V6.75m19.5 0A2.25 2.25 0 0019.5 4.5h-15a2.25 2.25 0 00-2.25 2.25m19.5 0v.243a2.25 2.25 0 01-1.07 1.916l-7.5 4.615a2.25 2.25 0 01-2.36 0L3.32 8.91a2.25 2.25 0 01-1.07-1.916V6.75" />
                    </svg>
                </div>
                <h1 className="font-serif text-[32px] text-slate-800 tracking-tight leading-tight">Check your email</h1>
                <p className="text-sm text-slate-400 mt-1.5">
                    We sent a 6-digit code to <span className="text-slate-600 font-medium">{email}</span>
                </p>
            </div>

            <div className="space-y-5">
                {/* OTP Input */}
                <div className="flex justify-center">
                    <InputOTP
                        maxLength={6}
                        value={otp}
                        onChange={(v) => setOtp(v)}
                        containerClassName="gap-2.5"
                    >
                        <InputOTPGroup className="gap-2.5">
                            {[0, 1, 2, 3, 4, 5].map((i) => (
                                <InputOTPSlot
                                    key={i}
                                    index={i}
                                    className="!w-12 !h-14 !rounded-lg !border-sky-200 text-lg font-semibold data-[active=true]:!border-sky-400 data-[active=true]:!ring-sky-400/15 !shadow-none first:!rounded-lg last:!rounded-lg !border"
                                />
                            ))}
                        </InputOTPGroup>
                    </InputOTP>
                </div>

                {/* Timer & resend */}
                <div className="text-center">
                    {expired ? (
                        <button
                            type="button"
                            onClick={handleResend}
                            className="text-sm text-sky-500 hover:text-sky-600 font-medium transition-colors cursor-pointer"
                        >
                            Resend code
                        </button>
                    ) : (
                        <p className="text-sm text-slate-400">
                            Resend code in <span className="font-medium text-slate-500">{count}s</span>
                        </p>
                    )}
                </div>

                <div onClick={() => !pending && onSubmit(otp)}>
                    <AuthButton loading={pending}>Verify</AuthButton>
                </div>
            </div>
        </div>
    );
}
