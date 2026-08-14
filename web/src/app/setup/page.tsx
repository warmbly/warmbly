// First-run setup (/setup?token=...).
//
// The backend prints this link once when it boots against an empty database.
// Whoever opens it becomes the owner and platform admin of the instance, so it
// replaces the old routine of registering through the public form, digging a
// code out of a mail catcher, and then running a psql UPDATE from the host.
//
// The token is the capability. It is single use, expires, and the backend
// refuses the exchange outright once any account exists, so a stale link out of
// an old log cannot mint a second owner.

import { useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2Icon, ShieldCheckIcon, AlertCircleIcon } from "lucide-react";
import toast from "react-hot-toast";

import claimSetup from "@/lib/api/client/auth/claimSetup";
import getUser from "@/lib/api/client/auth/getUser";
import useAuthConfig from "@/lib/api/hooks/auth/useAuthConfig";
import { usePasswordStrength } from "@/hooks/usePasswordStrength";
import { saveTokens } from "@/lib/auth";
import buildError from "@/lib/helper/buildError";
import type { AppError } from "@/lib/api/client/normalizeError";

export default function SetupPage() {
    const [params] = useSearchParams();
    const navigate = useNavigate();
    const queryClient = useQueryClient();
    const { config, ready } = useAuthConfig();
    const { evaluate } = usePasswordStrength();

    const token = params.get("token") ?? "";
    const [email, setEmail] = useState("");
    const [firstName, setFirstName] = useState("");
    const [password, setPassword] = useState("");
    const [confirm, setConfirm] = useState("");
    const [pending, setPending] = useState(false);

    async function onSubmit(e: React.FormEvent) {
        e.preventDefault();
        if (pending) return;

        if (password.length < 8) {
            toast.error("Password must be at least 8 characters long.");
            return;
        }
        if (password !== confirm) {
            toast.error("Passwords don't match.");
            return;
        }
        const strength = await evaluate(password);
        if (strength.score < 2) {
            toast.error(strength.warning || "Please choose a stronger password.");
            return;
        }

        setPending(true);
        try {
            const session = await claimSetup({
                token,
                email,
                password,
                first_name: firstName || undefined,
            });
            saveTokens(session as unknown as Record<string, unknown>);
            // Prime the profile with the new token before entering the gated
            // shell, so it never mounts without an identity.
            queryClient.clear();
            try {
                await queryClient.fetchQuery({ queryKey: ["auth", "me"], queryFn: getUser });
            } catch {
                // UserProvider retries and redirects on a genuine failure.
            }
            toast.success("This instance is yours.");
            navigate("/app/emails");
        } catch (err) {
            toast.error(buildError(err as AppError));
        } finally {
            setPending(false);
        }
    }

    if (!ready) {
        return (
            <div className="min-h-screen grid place-items-center">
                <Loader2Icon className="w-5 h-5 animate-spin text-slate-400" />
            </div>
        );
    }

    // Already claimed, or someone found the URL without a link.
    if (!config.setup_required || !token) {
        return (
            <Shell>
                <div className="mx-auto w-12 h-12 rounded-xl bg-slate-100 grid place-items-center mb-4">
                    <AlertCircleIcon className="w-6 h-6 text-slate-400" />
                </div>
                <h1 className="text-[22px] font-bold text-slate-900 tracking-tight text-center">
                    {config.setup_required ? "This link is incomplete" : "Already set up"}
                </h1>
                <p className="text-sm text-slate-500 mt-2 text-center">
                    {config.setup_required
                        ? "Open the full link from the backend logs, or run make claim to print it again."
                        : "This instance already has an account. Sign in instead."}
                </p>
                <button
                    onClick={() => navigate("/auth/login")}
                    className="mt-6 w-full h-10 rounded-md bg-slate-900 text-white text-[13px] font-medium hover:bg-slate-800 transition-colors"
                >
                    Go to sign in
                </button>
            </Shell>
        );
    }

    return (
        <Shell>
            <div className="mx-auto w-12 h-12 rounded-xl bg-sky-50 grid place-items-center mb-4">
                <ShieldCheckIcon className="w-6 h-6 text-sky-500" />
            </div>
            <h1 className="text-[22px] font-bold text-slate-900 tracking-tight text-center">
                Claim this instance
            </h1>
            <p className="text-sm text-slate-500 mt-2 text-center">
                This creates the owner account and makes it a platform admin. It can only be done once.
            </p>

            <form onSubmit={onSubmit} className="mt-6 space-y-4">
                <Field label="Your name">
                    <input
                        type="text"
                        value={firstName}
                        onChange={(e) => setFirstName(e.target.value)}
                        autoComplete="given-name"
                        placeholder="Alex"
                        className={inputClass}
                    />
                </Field>

                <Field label="Email">
                    <input
                        type="email"
                        required
                        autoFocus
                        value={email}
                        onChange={(e) => setEmail(e.target.value)}
                        autoComplete="email"
                        placeholder="you@example.com"
                        className={inputClass}
                    />
                </Field>

                <Field label="Password">
                    <input
                        type="password"
                        required
                        value={password}
                        onChange={(e) => setPassword(e.target.value)}
                        autoComplete="new-password"
                        placeholder="At least 8 characters"
                        className={inputClass}
                    />
                </Field>

                <Field label="Confirm password">
                    <input
                        type="password"
                        required
                        value={confirm}
                        onChange={(e) => setConfirm(e.target.value)}
                        autoComplete="new-password"
                        className={inputClass}
                    />
                </Field>

                <button
                    type="submit"
                    disabled={pending}
                    className="w-full h-10 rounded-md bg-slate-900 text-white text-[13px] font-medium hover:bg-slate-800 transition-colors disabled:opacity-60 inline-flex items-center justify-center gap-2"
                >
                    {pending && <Loader2Icon className="w-4 h-4 animate-spin" />}
                    {pending ? "Setting up…" : "Create owner account"}
                </button>
            </form>
        </Shell>
    );
}

const inputClass =
    "w-full h-10 px-3 rounded-md border border-slate-200 text-[13px] text-slate-900 placeholder:text-slate-400 focus:border-sky-400 focus:ring-2 focus:ring-sky-100 outline-none transition-colors";

function Shell({ children }: { children: React.ReactNode }) {
    return (
        <div className="min-h-screen grid place-items-center bg-slate-50 px-4">
            <div className="w-full max-w-sm bg-white rounded-xl border border-slate-200 p-6 shadow-sm">
                {children}
            </div>
        </div>
    );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
    return (
        <div>
            <label className="block text-sm font-medium text-slate-600 mb-1 pl-0.5">{label}</label>
            {children}
        </div>
    );
}
