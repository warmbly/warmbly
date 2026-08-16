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
//
// Every dead end here has to end in a command, not an apology: without the
// token there is no way into the instance at all.

import { useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import {
    AlertCircleIcon,
    CheckIcon,
    CopyIcon,
    Loader2Icon,
    RefreshCwIcon,
    ShieldCheckIcon,
} from "lucide-react";
import toast from "react-hot-toast";

import claimSetup from "@/lib/api/client/auth/claimSetup";
import getUser from "@/lib/api/client/auth/getUser";
import useAuthConfig from "@/lib/api/hooks/auth/useAuthConfig";
import { usePasswordStrength } from "@/hooks/usePasswordStrength";
import { saveTokens } from "@/lib/auth";
import { Label, TextInput } from "@/components/ui/field";
import type { AppError } from "@/lib/api/client/normalizeError";
import type AuthConfig from "@/lib/api/models/auth/AuthConfig";

// The one invocation that works on the shipped compose stack, plus the bare
// binary for anyone running the backend some other way.
const SETUP_LINK_COMPOSE = "docker compose -p warmbly exec backend warmblyctl setup-link";
const SETUP_LINK_DIRECT = "warmblyctl setup-link";
const RESET_PASSWORD_COMPOSE =
    "docker compose -p warmbly exec backend warmblyctl user reset-password --email you@example.com";

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
    // The claim refusal stays on screen: a four second toast over a filled-in
    // form told the operator nothing about what to run next.
    const [failure, setFailure] = useState<AppError | null>(null);

    function checkAgain() {
        setFailure(null);
        void queryClient.invalidateQueries({ queryKey: ["auth", "config"] });
    }

    // The backend can refuse the claim as already complete while the cached
    // config still says setup_required, and /auth/login bounces that straight
    // back here. Correct the cache first so signing in actually leaves.
    function goToSignIn() {
        queryClient.setQueryData<AuthConfig>(["auth", "config"], (prev) =>
            prev ? { ...prev, setup_required: false } : prev,
        );
        navigate("/auth/login");
    }

    async function onSubmit(e: React.FormEvent) {
        e.preventDefault();
        if (pending) return;

        if (!email.trim()) {
            toast.error("Enter the email address for the owner account.");
            return;
        }
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
        setFailure(null);
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
            setFailure(err as AppError);
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

    // Already claimed, by this instance's own report or by the backend refusing
    // the exchange. Nobody can set up here again, so the way back in is a
    // password reset, not another setup link.
    if (!config.setup_required || failure?.code === "setup_already_complete") {
        return (
            <Shell>
                <PanelHead
                    tone="slate"
                    icon={<AlertCircleIcon className="w-6 h-6 text-slate-400" />}
                    title="Already set up"
                    body="This instance already has an account, so setup is closed. Sign in, or reset the owner's password from the server."
                />
                <button
                    type="button"
                    onClick={goToSignIn}
                    className="mt-5 w-full h-10 rounded-md bg-slate-900 text-white text-[13px] font-medium hover:bg-slate-800 transition-colors"
                >
                    Go to sign in
                </button>
                <Commands
                    caption="Lost the only account? Reset its password on the server:"
                    commands={[RESET_PASSWORD_COMPOSE]}
                />
            </Shell>
        );
    }

    // A rejected token and a missing token need the same thing: a new link.
    if (!token || failure?.code === "setup_token_invalid") {
        const rejected = !!failure;
        return (
            <Shell>
                <PanelHead
                    tone="amber"
                    icon={<AlertCircleIcon className="w-6 h-6 text-amber-500" />}
                    title={rejected ? "That setup link no longer works" : "This link has no setup token"}
                    body={
                        rejected
                            ? "It was already used, or it expired. Print a fresh one on the server, then open it here."
                            : "This instance has no accounts yet and cannot be signed into. Print the setup link on the server, then open it here."
                    }
                />
                <Commands
                    caption="Run one of these where the backend runs:"
                    commands={[SETUP_LINK_COMPOSE, SETUP_LINK_DIRECT]}
                />
                <CheckAgain onClick={checkAgain} />
            </Shell>
        );
    }

    return (
        <Shell>
            <PanelHead
                tone="sky"
                icon={<ShieldCheckIcon className="w-6 h-6 text-sky-500" />}
                title="Claim this instance"
                body="This creates the owner account and makes it a platform admin. It can only be done once."
            />

            <form onSubmit={onSubmit} className="mt-6 space-y-4">
                <div>
                    <Label>Your name</Label>
                    <TextInput
                        value={firstName}
                        onChange={setFirstName}
                        autoComplete="given-name"
                        placeholder="Alex"
                        className={FIELD}
                    />
                </div>

                <div>
                    <Label>Email</Label>
                    <TextInput
                        type="email"
                        value={email}
                        onChange={setEmail}
                        autoFocus
                        autoComplete="email"
                        placeholder="you@example.com"
                        className={FIELD}
                    />
                </div>

                <div>
                    <Label>Password</Label>
                    <TextInput
                        type="password"
                        value={password}
                        onChange={setPassword}
                        autoComplete="new-password"
                        placeholder="At least 8 characters"
                        className={FIELD}
                    />
                </div>

                <div>
                    <Label>Confirm password</Label>
                    <TextInput
                        type="password"
                        value={confirm}
                        onChange={setConfirm}
                        autoComplete="new-password"
                        className={FIELD}
                    />
                </div>

                {failure && (
                    <div className="rounded-md border border-rose-200 bg-rose-50 px-3 py-2 text-[12.5px] leading-relaxed text-rose-800">
                        {failure.message || "The claim failed. Try again."}
                        {failure.request_id && (
                            <span className="block mt-1 font-mono text-[11px] text-rose-500">
                                {failure.request_id}
                            </span>
                        )}
                    </div>
                )}

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

// Auth-screen sizing on top of the shared field primitive.
const FIELD = "w-full h-10 md:text-[13px]";

function Shell({ children }: { children: React.ReactNode }) {
    return (
        <div className="min-h-screen grid place-items-center bg-slate-50 px-4 py-10">
            <div className="w-full max-w-sm bg-white rounded-xl border border-slate-200 p-6 shadow-sm">
                {children}
            </div>
        </div>
    );
}

function PanelHead({
    tone,
    icon,
    title,
    body,
}: {
    tone: "sky" | "slate" | "amber";
    icon: React.ReactNode;
    title: string;
    body: string;
}) {
    const bg = tone === "sky" ? "bg-sky-50" : tone === "amber" ? "bg-amber-50" : "bg-slate-100";
    return (
        <>
            <div className={`mx-auto w-12 h-12 rounded-xl ${bg} grid place-items-center mb-4`}>{icon}</div>
            <h1 className="text-[22px] font-bold text-slate-900 tracking-tight text-center">{title}</h1>
            <p className="text-sm text-slate-500 mt-2 text-center leading-relaxed">{body}</p>
        </>
    );
}

function Commands({ caption, commands }: { caption: string; commands: string[] }) {
    return (
        <div className="mt-5 rounded-md border border-slate-200 bg-slate-50/60 p-3">
            <p className="text-[12px] text-slate-600 leading-relaxed mb-2">{caption}</p>
            <div className="space-y-1.5">
                {commands.map((c) => (
                    <CommandRow key={c} command={c} />
                ))}
            </div>
        </div>
    );
}

function CommandRow({ command }: { command: string }) {
    const [copied, setCopied] = useState(false);

    async function copy() {
        try {
            await navigator.clipboard.writeText(command);
            setCopied(true);
            setTimeout(() => setCopied(false), 2000);
        } catch {
            toast.error("Could not copy. Select the command and copy it manually.");
        }
    }

    return (
        <div className="flex items-stretch gap-1.5">
            <code className="flex-1 min-w-0 rounded border border-slate-200 bg-white px-2 py-1.5 font-mono text-[11px] text-slate-700 break-all">
                {command}
            </code>
            <button
                type="button"
                onClick={copy}
                aria-label={`Copy ${command}`}
                className="shrink-0 w-8 rounded border border-slate-200 bg-white text-slate-400 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
            >
                {copied ? <CheckIcon className="w-3.5 h-3.5 text-emerald-600" /> : <CopyIcon className="w-3.5 h-3.5" />}
            </button>
        </div>
    );
}

function CheckAgain({ onClick }: { onClick: () => void }) {
    return (
        <button
            type="button"
            onClick={onClick}
            className="mt-3 w-full h-10 rounded-md border border-slate-200 bg-white text-slate-700 text-[13px] font-medium hover:bg-slate-50 transition-colors inline-flex items-center justify-center gap-1.5"
        >
            <RefreshCwIcon className="w-3.5 h-3.5" />
            Check again
        </button>
    );
}
