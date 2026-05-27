// Admin login. Same endpoint as the dashboard (/auth/login). The page
// is intentionally lean — no marketing chrome, no sky animations. The
// admin app exists to do work, not to convert anyone.

import { useState, type FormEvent } from "react";
import { useNavigate, useLocation } from "react-router-dom";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { AdminBadge } from "@/components/layout/AdminBadge";
import { EnvPill } from "@/components/layout/EnvPill";
import { login } from "@/lib/api/client/auth";
import { setToken } from "@/lib/auth/storage";
import { APIError } from "@/lib/api/client";

export default function LoginPage() {
    const nav = useNavigate();
    const loc = useLocation();
    const [email, setEmail] = useState("");
    const [password, setPassword] = useState("");
    const [submitting, setSubmitting] = useState(false);

    async function onSubmit(e: FormEvent) {
        e.preventDefault();
        setSubmitting(true);
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
            toast.error(msg);
        } finally {
            setSubmitting(false);
        }
    }

    return (
        <div className="min-h-screen flex items-center justify-center bg-background p-4">
            {/* Mirror the app-shell stripe so the auth screen is also
                visually anchored to the admin surface. */}
            <div className="fixed top-0 left-0 right-0 h-[3px] admin-stripe" />

            <div className="w-full max-w-sm">
                <div className="flex items-center justify-between mb-6">
                    <div className="flex items-center gap-2">
                        <div className="size-8 rounded-md bg-zinc-900 text-white flex items-center justify-center font-bold text-sm">
                            W
                        </div>
                        <div className="text-base font-semibold">Warmbly</div>
                    </div>
                    <EnvPill />
                </div>

                <div className="rounded-lg border border-border bg-card p-6 shadow-sm">
                    <div className="flex items-center gap-2 mb-1">
                        <AdminBadge compact />
                        <span className="text-xs text-muted-foreground">sign in to control plane</span>
                    </div>
                    <h1 className="text-xl font-semibold mt-2">Admin sign in</h1>
                    <p className="text-xs text-muted-foreground mt-1">
                        Restricted to accounts with admin permissions on the Warmbly platform.
                    </p>

                    <form className="mt-5 space-y-4" onSubmit={onSubmit}>
                        <div className="space-y-1.5">
                            <Label htmlFor="email">Email</Label>
                            <Input
                                id="email"
                                type="email"
                                autoComplete="email"
                                autoFocus
                                value={email}
                                onChange={(e) => setEmail(e.target.value)}
                                required
                            />
                        </div>
                        <div className="space-y-1.5">
                            <Label htmlFor="password">Password</Label>
                            <Input
                                id="password"
                                type="password"
                                autoComplete="current-password"
                                value={password}
                                onChange={(e) => setPassword(e.target.value)}
                                required
                            />
                        </div>
                        <Button
                            type="submit"
                            disabled={submitting}
                            className="w-full bg-[var(--admin-accent)] hover:bg-[var(--admin-accent-strong)] text-[var(--admin-accent-foreground)]"
                        >
                            {submitting ? "Signing in…" : "Sign in"}
                        </Button>
                    </form>
                </div>

                <p className="text-[11px] text-muted-foreground text-center mt-4">
                    All admin actions are logged to <code className="font-mono">admin_audit_logs</code>.
                </p>
            </div>
        </div>
    );
}
