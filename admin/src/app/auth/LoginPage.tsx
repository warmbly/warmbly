// Admin sign-in.
//
// Clean and in-theme (the same light shadcn surface as the rest of the admin
// app), but distinct from the consumer dashboard's two-pane marketing login:
// a single, focused card with a red accent that marks this as restricted
// admin access. Same /auth/login endpoint as the dashboard. Stack matches the
// dashboard — Tailwind + the shadcn ui primitives + the shared theme tokens.

import { useState, type FormEvent } from "react";
import { useNavigate, useLocation } from "react-router-dom";
import { toast } from "sonner";
import { ShieldCheck, Eye, EyeOff, Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Logo } from "@/components/Logo";
import { EnvPill } from "@/components/layout/EnvPill";
import { login } from "@/lib/api/client/auth";
import { setToken } from "@/lib/auth/storage";
import { APIError } from "@/lib/api/client";
import { DASHBOARD_URL } from "@/lib/env";
import { cn } from "@/lib/utils";

// Admin accent. red-600 — the same value as the theme's --admin-danger token,
// signalling "restricted / elevated access" without any extra chrome.
const ACCENT = "var(--admin-danger)";

export default function LoginPage() {
    const nav = useNavigate();
    const loc = useLocation();
    const [email, setEmail] = useState("");
    const [password, setPassword] = useState("");
    const [showPassword, setShowPassword] = useState(false);
    const [submitting, setSubmitting] = useState(false);
    const [error, setError] = useState<string | null>(null);

    async function onSubmit(e: FormEvent) {
        e.preventDefault();
        setSubmitting(true);
        setError(null);
        try {
            const tok = await login({ email, password });
            setToken(tok);
            const dest = (loc.state as { from?: string } | null)?.from ?? "/";
            nav(dest, { replace: true });
        } catch (err) {
            const msg =
                err instanceof APIError
                    ? err.message
                    : "Sign-in failed. Check your credentials or contact an admin.";
            setError(msg);
            toast.error(msg);
        } finally {
            setSubmitting(false);
        }
    }

    const focusRed =
        "focus-visible:border-red-400 focus-visible:ring-red-100";

    return (
        <div className="flex min-h-dvh flex-col items-center justify-center bg-muted/40 px-4 py-10">
            <div className="w-full max-w-[400px] animate-in fade-in slide-in-from-bottom-2 duration-500">
                {/* Brand */}
                <div className="mb-6 flex items-center justify-center gap-2.5">
                    <Logo className="h-7 w-7 text-foreground" />
                    <span className="text-[18px] font-bold tracking-tight">Warmbly</span>
                </div>

                {/* Card — a thin red top edge is the only "admin" flourish. */}
                <div className="overflow-hidden rounded-xl border border-border bg-card shadow-sm">
                    <div className="h-1" style={{ backgroundColor: ACCENT }} />

                    <div className="px-7 pt-6 pb-7">
                        <div className="flex items-center justify-between">
                            <span
                                className="inline-flex items-center gap-1.5 text-[12px] font-semibold"
                                style={{ color: ACCENT }}
                            >
                                <ShieldCheck className="size-3.5" />
                                Admin access
                            </span>
                            <EnvPill />
                        </div>

                        <h1 className="mt-4 text-[20px] font-semibold tracking-tight">Sign in</h1>
                        <p className="mt-1 text-[13px] text-muted-foreground">
                            Restricted to Warmbly staff with admin permissions.
                        </p>

                        <form className="mt-6 space-y-4" onSubmit={onSubmit}>
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
                                    className={cn("h-10", focusRed)}
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
                                        className={cn("h-10 pr-10", focusRed)}
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

                            {error && (
                                <p className="flex items-start gap-1.5 text-[12.5px] text-red-600">
                                    <span className="mt-1.5 size-1 shrink-0 rounded-full bg-red-500" />
                                    {error}
                                </p>
                            )}

                            <Button
                                type="submit"
                                disabled={submitting}
                                className="h-10 w-full text-white hover:opacity-90"
                                style={{ backgroundColor: ACCENT }}
                            >
                                {submitting ? (
                                    <>
                                        <Loader2 className="size-4 animate-spin" />
                                        Signing in…
                                    </>
                                ) : (
                                    "Sign in"
                                )}
                            </Button>
                        </form>
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
