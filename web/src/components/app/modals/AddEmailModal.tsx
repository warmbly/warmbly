// Connect-mailbox modal — themed, single column, three providers.
//
// Same chrome as every other dialog in the app: 48px header band with
// eyebrow + subtitle + close, hairline rows, slate-900 primary,
// 28px buttons. Replaces the old indigo-gradient "feature billboard"
// triptych.
//
// Flow:
//   provider picker ──► gmail OAuth popup  ─┐
//                  ──► outlook OAuth popup ─┼─► /emails/onboarding/oauth/finish
//                  ──► smtp/imap form ──────────► /emails/onboarding/smtp-imap
//
// OAuth popup posts {type:"email_oauth_callback", code, state} back here
// via window.postMessage; we then call OAuth-finish with the user's bearer.
//
// Linked to Warmbly Cloud: the consent runs on the cloud's app, the popup
// returns to /cloud-oauth/done, and we redeem its session via /cloud-link/oauth/finish.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import {
    ArrowLeftIcon,
    CheckIcon,
    ChevronRightIcon,
    CloudIcon,
    InboxIcon,
    KeyRoundIcon,
    Loader2Icon,
    MailIcon,
    ExternalLinkIcon,
    SendIcon,
    SettingsIcon,
    ShieldCheckIcon,
    XIcon,
} from "lucide-react";
import toast from "react-hot-toast";
import { useQueryClient } from "@tanstack/react-query";

import { Logo } from "@/components/svg";
import { TextInput } from "@/components/ui/field";
import { useUserProfile } from "@/hooks/context/user";
import { API_URL, APP_URL } from "@/lib/information";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import addEmail from "@/lib/api/client/app/emails/addEmail";
import {
    defaultImapSecurity,
    defaultSmtpSecurity,
    validPort,
    type MailSecurity,
} from "@/lib/api/models/app/emails/Service";
import SecuritySelect from "@/components/app/emails/SecuritySelect";
import onboardOAuthStart from "@/lib/api/client/app/emails/onboardOAuthStart";
import onboardOAuthFinish from "@/lib/api/client/app/emails/onboardOAuthFinish";
import { finishCloudOAuth, startCloudOAuth } from "@/lib/api/client/app/cloudlink/cloudLink";
import { useAdoptCloudMailbox, useCloudWorkspaceMailboxes } from "@/lib/api/hooks/app/cloudlink/useCloudLink";
import useCloudPool from "@/hooks/useCloudPool";
import type { CloudOAuthDoneMessage } from "@/app/cloud-oauth/done/page";
import { Google, Outlook } from "@/components/svg";
import { cn } from "@/lib/utils";

type View = "pick" | "gmail" | "outlook" | "smtp_imap";
type OAuthProvider = "gmail" | "outlook";

interface OAuthCallbackMessage {
    type: "email_oauth_callback";
    provider: OAuthProvider;
    code: string;
    state: string;
    error: string;
}

// originOf normalises a configured base URL to a bare origin. APP_URL and
// API_URL may carry a trailing slash or a path; event.origin never does.
function originOf(value: string | undefined): string | null {
    if (!value) return null;
    try {
        return new URL(value, window.location.href).origin;
    } catch {
        return null;
    }
}

// The origins we accept an OAuth callback message from.
//
// The bridge page is served by the API, not the dashboard, so that the
// redirect_uri registered with Google and Microsoft stays stable across
// front-end changes. On a split-domain deployment (dashboard on one host, API
// on another, which is what the self-hosting guide sets up) event.origin is
// therefore API_URL's origin and never APP_URL's. Accepting only APP_URL
// silently dropped every callback and left the modal waiting forever.
//
// This is a coarse gate: the real replay protection is the single-use state
// match below, which the message still has to satisfy.
function allowedCallbackOrigins(): string[] {
    return [originOf(APP_URL), originOf(API_URL), window.location.origin].filter(
        (o): o is string => Boolean(o),
    );
}

function openCentered(url: string, name: string): Window | null {
    const w = 520;
    const h = 640;
    const sx = window.screenLeft ?? window.screenX;
    const sy = window.screenTop ?? window.screenY;
    const sw = window.innerWidth ?? document.documentElement.clientWidth ?? screen.width;
    const sh = window.innerHeight ?? document.documentElement.clientHeight ?? screen.height;
    const left = sx + (sw - w) / 2;
    const top = sy + (sh - h) / 2;
    const popup = window.open(
        url,
        name,
        `width=${w},height=${h},left=${left},top=${top}`,
    );
    popup?.focus();
    return popup;
}

export default function AddEmailModal() {
    const user = useUserProfile();
    const qc = useQueryClient();

    const [view, setView] = React.useState<View>("pick");
    const [oauthBusy, setOauthBusy] = React.useState<OAuthProvider | null>(null);
    // Set when the deployment has no OAuth client for the provider the user
    // picked. Rendered inline rather than as a toast: it is a setup instruction
    // with a link, not a transient failure.
    const [notConfigured, setNotConfigured] = React.useState<OAuthProvider | null>(null);
    const pendingState = React.useRef<{ provider: OAuthProvider; state: string } | null>(null);
    // A consent running on Warmbly Cloud's app; redeemed by session, not code.
    const pendingCloud = React.useRef<{ provider: OAuthProvider; session: string } | null>(null);
    const pool = useCloudPool();
    const viaCloud = pool.connected;

    // Reset when the modal closes.
    React.useEffect(() => {
        if (!user.addEmail) {
            setView("pick");
            setOauthBusy(null);
            setNotConfigured(null);
            pendingState.current = null;
            pendingCloud.current = null;
        }
    }, [user.addEmail]);

    // The cloud-brokered popup lands on our own origin (/cloud-oauth/done).
    React.useEffect(() => {
        function onMessage(event: MessageEvent) {
            if (event.origin !== window.location.origin) return;
            const data = event.data as CloudOAuthDoneMessage | undefined;
            if (!data || data.type !== "cloud_oauth_callback") return;
            const expected = pendingCloud.current;
            if (!expected || expected.session !== data.session) return;
            pendingCloud.current = null;
            if (data.status !== "ok") {
                setOauthBusy(null);
                if (data.error !== "access_denied") {
                    toast.error(data.message || (data.error ? `Provider error: ${data.error}` : "Connection was cancelled."));
                }
                return;
            }
            void toast.promise(
                finishCloudOAuth(data.session).then((inbox) => {
                    qc.invalidateQueries({ queryKey: ["emails", "list"] });
                    qc.invalidateQueries({ queryKey: ["cloud-link"] });
                    user.setAddEmail(false);
                    return inbox;
                }),
                {
                    loading: "Adding the mailbox…",
                    success: "Mailbox connected. Warmbly Cloud warms it from now on.",
                    error: (e: AppError) => buildError(e),
                },
            ).finally(() => setOauthBusy(null));
        }
        window.addEventListener("message", onMessage);
        return () => window.removeEventListener("message", onMessage);
    }, [qc, user]);

    // Listen for the OAuth popup's postMessage. We only honour messages from an
    // origin we own and whose state matches the one we issued, which is what
    // protects against replay and stray posts.
    React.useEffect(() => {
        function onMessage(event: MessageEvent) {
            if (event.origin && !allowedCallbackOrigins().includes(event.origin)) {
                return;
            }
            const data = event.data as OAuthCallbackMessage | undefined;
            if (!data || data.type !== "email_oauth_callback") return;

            const expected = pendingState.current;
            if (!expected || expected.state !== data.state) return;
            pendingState.current = null;

            if (data.error || !data.code) {
                setOauthBusy(null);
                if (data.error !== "access_denied") {
                    toast.error(data.error ? `Provider error: ${data.error}` : "Connection was cancelled.");
                }
                return;
            }

            void toast.promise(
                onboardOAuthFinish(data.code, data.state).then((inbox) => {
                    qc.invalidateQueries({ queryKey: ["emails", "list"] });
                    user.setAddEmail(false);
                    return inbox;
                }),
                {
                    loading: "Connecting…",
                    success: "Mailbox connected",
                    error: (e: AppError) => buildError(e),
                },
            ).finally(() => setOauthBusy(null));
        }
        window.addEventListener("message", onMessage);
        return () => window.removeEventListener("message", onMessage);
    }, [qc, user]);

    async function startOAuth(provider: OAuthProvider) {
        if (oauthBusy) return;
        setOauthBusy(provider);
        setNotConfigured(null);
        if (viaCloud) {
            try {
                const { url, session } = await startCloudOAuth(provider);
                pendingCloud.current = { provider, session };
                const popup = openCentered(url, `connect-${provider}`);
                if (!popup) {
                    pendingCloud.current = null;
                    setOauthBusy(null);
                    toast.error("Could not open the authorization window. Please allow popups and try again.");
                }
            } catch (err) {
                pendingCloud.current = null;
                setOauthBusy(null);
                toast.error(buildError(err as AppError));
            }
            return;
        }
        try {
            const { url, state } = await onboardOAuthStart(provider);
            pendingState.current = { provider, state };
            const popup = openCentered(url, `connect-${provider}`);
            if (!popup) {
                pendingState.current = null;
                setOauthBusy(null);
                toast.error("Could not open the authorization window. Please allow popups and try again.");
            }
        } catch (err) {
            pendingState.current = null;
            setOauthBusy(null);
            const e = err as AppError;
            if (e.code === "mailbox_provider_not_configured") {
                setNotConfigured(provider);
                return;
            }
            toast.error(buildError(e));
        }
    }

    return (
        <AnimatePresence>
            {user.addEmail && (
                <motion.div
                    key="overlay"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.15 }}
                    onClick={() => user.setAddEmail(false)}
                    className="fixed inset-0 z-[110] flex items-center justify-center bg-slate-900/30 backdrop-blur-[2px] px-4"
                >
                    <motion.div
                        key="card"
                        initial={{ y: 8, opacity: 0 }}
                        animate={{ y: 0, opacity: 1 }}
                        exit={{ y: 8, opacity: 0 }}
                        transition={{ duration: 0.16 }}
                        onClick={(e) => e.stopPropagation()}
                        className="w-full max-w-[560px] rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18),0_8px_16px_-8px_rgba(15,23,42,0.1)] overflow-hidden flex flex-col max-h-[88dvh]"
                    >
                        <Header
                            view={view}
                            onBack={() => {
                                setNotConfigured(null);
                                setView("pick");
                            }}
                            onClose={() => user.setAddEmail(false)}
                        />
                        <div className="flex-1 min-h-0 overflow-y-auto overflow-x-hidden relative">
                            <AnimatePresence mode="wait" initial={false}>
                                <motion.div
                                    key={view}
                                    initial={{ opacity: 0, x: view === "pick" ? -12 : 12 }}
                                    animate={{ opacity: 1, x: 0 }}
                                    exit={{ opacity: 0, x: view === "pick" ? 12 : -12 }}
                                    transition={{ duration: 0.18, ease: [0.32, 0.72, 0, 1] }}
                                >
                                    {view === "pick" && (
                                        <PickProvider
                                            onPick={setView}
                                            viaCloud={viaCloud}
                                            onAdopted={() => {
                                                qc.invalidateQueries({ queryKey: ["emails", "list"] });
                                                user.setAddEmail(false);
                                            }}
                                        />
                                    )}
                                    {view === "gmail" && (
                                        notConfigured === "gmail" ? (
                                            <ProviderNotConfigured provider="gmail" selfHosted={pool.selfHosted} />
                                        ) : (
                                            <OAuthPanel
                                                provider="gmail"
                                                busy={oauthBusy === "gmail"}
                                                viaCloud={viaCloud}
                                                onConnect={() => startOAuth("gmail")}
                                            />
                                        )
                                    )}
                                    {view === "outlook" && (
                                        notConfigured === "outlook" ? (
                                            <ProviderNotConfigured provider="outlook" selfHosted={pool.selfHosted} />
                                        ) : (
                                            <OAuthPanel
                                                provider="outlook"
                                                busy={oauthBusy === "outlook"}
                                                viaCloud={viaCloud}
                                                onConnect={() => startOAuth("outlook")}
                                            />
                                        )
                                    )}
                                    {view === "smtp_imap" && (
                                        <SmtpImapPanel
                                            onDone={() => {
                                                qc.invalidateQueries({ queryKey: ["emails", "list"] });
                                                user.setAddEmail(false);
                                            }}
                                        />
                                    )}
                                </motion.div>
                            </AnimatePresence>
                        </div>
                    </motion.div>
                </motion.div>
            )}
        </AnimatePresence>
    );
}

function Header({
    view,
    onBack,
    onClose,
}: {
    view: View;
    onBack: () => void;
    onClose: () => void;
}) {
    const sub: Record<View, string> = {
        pick: "Connect a sending account",
        gmail: "Gmail or Google Workspace",
        outlook: "Outlook or Microsoft 365",
        smtp_imap: "Any provider via SMTP / IMAP",
    };
    return (
        <div className="h-12 px-3 border-b border-slate-200 flex items-center gap-2.5 shrink-0">
            {view !== "pick" && (
                <button
                    type="button"
                    onClick={onBack}
                    aria-label="Back"
                    className="size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                >
                    <ArrowLeftIcon className="w-3.5 h-3.5" />
                </button>
            )}
            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                Mailbox
            </span>
            <div className="h-4 w-px bg-slate-200" />
            <span className="text-[12px] text-slate-600 truncate">{sub[view]}</span>
            <button
                type="button"
                onClick={onClose}
                aria-label="Close"
                className="ml-auto size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
            >
                <XIcon className="w-3.5 h-3.5" />
            </button>
        </div>
    );
}

// Shown when the API reports this deployment has no OAuth client for the chosen
// provider. Self-host only, and the person seeing it can usually fix it, so it
// names the exact variables and links the setup guide instead of just failing.
const PROVIDER_SETUP: Record<OAuthProvider, { label: string; vars: string[] }> = {
    gmail: {
        label: "Gmail and Google Workspace",
        vars: ["BOX_GOOGLE_CLIENT_ID", "BOX_GOOGLE_CLIENT_SECRET"],
    },
    outlook: {
        label: "Outlook and Microsoft 365",
        vars: ["BOX_OUTLOOK_CLIENT_ID", "BOX_OUTLOOK_CLIENT_SECRET"],
    },
};

function ProviderNotConfigured({ provider, selfHosted }: { provider: OAuthProvider; selfHosted: boolean }) {
    const { label, vars } = PROVIDER_SETUP[provider];
    return (
        <div className="p-4">
            {selfHosted && (
                <div className="mb-3 rounded-md border border-sky-200 bg-sky-50 p-3 flex items-start gap-2.5">
                    <CloudIcon className="w-4 h-4 text-sky-600 mt-0.5 shrink-0" />
                    <div className="min-w-0">
                        <p className="text-[12.5px] font-medium text-sky-900">Skip the OAuth setup: connect Warmbly Cloud</p>
                        <p className="text-[12.5px] text-sky-800 mt-1">
                            Linked instances sign mailboxes in through Warmbly's own Google and Microsoft apps, and the cloud warms them. Free for 10 mailboxes.
                        </p>
                        <a href="/app/settings/warmbly-cloud" className="mt-2 inline-flex h-7 px-2.5 items-center gap-1.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium transition-colors">
                            Connect Warmbly Cloud
                        </a>
                    </div>
                </div>
            )}
            <div className="rounded-md border border-amber-200 bg-amber-50 p-3">
                <div className="flex items-start gap-2.5">
                    <SettingsIcon className="w-4 h-4 text-amber-600 mt-0.5 shrink-0" />
                    <div className="min-w-0">
                        <p className="text-[12.5px] font-medium text-amber-900">
                            {label} is not configured on this deployment
                        </p>
                        <p className="text-[12.5px] text-amber-800 mt-1">
                            Connecting these mailboxes needs an OAuth client. Add both values to
                            the <code className="bg-white/70 px-1 rounded">.env</code> at the root of
                            your Warmbly install, then restart with{" "}
                            <code className="bg-white/70 px-1 rounded">make up</code>.
                        </p>
                        <ul className="mt-2 space-y-1">
                            {vars.map((v) => (
                                <li
                                    key={v}
                                    className="text-[12px] font-mono text-amber-900 bg-white/70 rounded px-1.5 py-1"
                                >
                                    {v}=
                                </li>
                            ))}
                        </ul>
                    </div>
                </div>
            </div>

            <a
                href="https://docs.warmbly.com/development/deployment-guide/#connect-mailboxes"
                target="_blank"
                rel="noreferrer"
                className="mt-3 h-7 px-2.5 inline-flex items-center gap-1.5 rounded-md border border-slate-200 text-[12.5px] text-slate-700 hover:bg-slate-50 transition-colors"
            >
                <ExternalLinkIcon className="w-3.5 h-3.5" />
                Full environment setup guide
            </a>

            <p className="mt-3 text-[12px] text-slate-500">
                No setup needed for any other provider: connect it over SMTP and IMAP instead.
            </p>
        </div>
    );
}

function PickProvider({ onPick, viaCloud, onAdopted }: { onPick: (v: View) => void; viaCloud: boolean; onAdopted: () => void }) {
    const rows: Array<{
        key: View;
        icon: React.ReactNode;
        title: string;
        sub: string;
        tone: "primary" | "neutral";
    }> = [
        {
            key: "gmail",
            icon: <Google className="w-5 h-5" />,
            title: "Gmail / Google Workspace",
            sub: viaCloud ? "Sign in through Warmbly Cloud. Warmup included, no OAuth app needed." : "OAuth via Google. Best deliverability for Gmail.",
            tone: "primary",
        },
        {
            key: "outlook",
            icon: <Outlook className="w-5 h-5" />,
            title: "Outlook / Microsoft 365",
            sub: viaCloud ? "Sign in through Warmbly Cloud. Warmup included, no OAuth app needed." : "OAuth via Microsoft. Native sync for Outlook accounts.",
            tone: "primary",
        },
        {
            key: "smtp_imap",
            icon: <Logo className="w-4 h-5 text-slate-700" />,
            title: "Other (SMTP / IMAP)",
            sub: "Any provider with manual host, port, and app password.",
            tone: "neutral",
        },
    ];
    return (
        <div className="divide-y divide-slate-200/60">
            {rows.map((r, i) => (
                <motion.button
                    key={r.key}
                    type="button"
                    onClick={() => onPick(r.key)}
                    initial={{ opacity: 0, y: 4 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ delay: 0.04 + i * 0.04, duration: 0.18, ease: "easeOut" }}
                    className="w-full px-4 py-3.5 flex items-center gap-3 text-left group hover:bg-slate-50 transition-colors"
                >
                    <div className="size-9 rounded-md border border-slate-200 bg-white flex items-center justify-center shrink-0 transition-colors group-hover:border-slate-300">
                        {r.icon}
                    </div>
                    <div className="min-w-0 flex-1">
                        <div className="text-[13px] font-medium text-slate-900 truncate">{r.title}</div>
                        <div className="text-[11.5px] text-slate-500 truncate">{r.sub}</div>
                    </div>
                    <ChevronRightIcon className="w-4 h-4 text-slate-300 shrink-0 group-hover:text-slate-500 group-hover:translate-x-0.5 transition-all" />
                </motion.button>
            ))}
            {viaCloud && <WorkspaceMailboxes onAdopted={onAdopted} />}
        </div>
    );
}

// Mailboxes connected directly on the linked Warmbly Cloud workspace: one
// click brings each one here, sending with tokens the cloud brokers.
function WorkspaceMailboxes({ onAdopted }: { onAdopted: () => void }) {
    const list = useCloudWorkspaceMailboxes();
    const adopt = useAdoptCloudMailbox();
    const [busy, setBusy] = React.useState<string | null>(null);
    const items = list.data ?? [];
    if (list.isLoading || items.length === 0) return null;

    const run = async (id: string, email: string) => {
        setBusy(id);
        try {
            await adopt.mutateAsync(id);
            toast.success(`${email} connected`);
            onAdopted();
        } catch (e) {
            toast.error(buildError(e as AppError));
        } finally {
            setBusy(null);
        }
    };

    return (
        <div className="px-4 py-3 bg-sky-50/40">
            <div className="flex items-center gap-1.5 mb-2">
                <CloudIcon className="w-3.5 h-3.5 text-sky-600" />
                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">In your Warmbly Cloud workspace</span>
            </div>
            <div className="space-y-1.5">
                {items.map((m) => (
                    <div key={m.id} className="flex items-center gap-2.5 rounded-md border border-slate-200 bg-white px-2.5 py-2">
                        <div className="size-7 rounded-md border border-slate-200 bg-white flex items-center justify-center shrink-0">
                            {m.provider === "gmail" ? <Google className="w-4 h-4" /> : <Outlook className="w-4 h-4" />}
                        </div>
                        <div className="min-w-0 flex-1">
                            <div className="text-[12.5px] text-slate-900 truncate">{m.email}</div>
                            <div className="text-[11px] text-slate-500 truncate">Connected on the cloud. Add it here to send campaigns from it.</div>
                        </div>
                        <button
                            type="button"
                            disabled={busy === m.id}
                            onClick={() => void run(m.id, m.email)}
                            className="shrink-0 h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                        >
                            {busy === m.id ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <CheckIcon className="w-3 h-3" />}
                            Connect
                        </button>
                    </div>
                ))}
            </div>
        </div>
    );
}

function OAuthPanel({
    provider,
    busy,
    viaCloud,
    onConnect,
}: {
    provider: OAuthProvider;
    busy: boolean;
    viaCloud: boolean;
    onConnect: () => void;
}) {
    const label = provider === "gmail" ? "Google" : "Microsoft";
    const Icon = provider === "gmail" ? Google : Outlook;
    return (
        <div className="px-5 py-6 space-y-5">
            <div className="flex items-center gap-3">
                <div className="size-11 rounded-md border border-slate-200 bg-white flex items-center justify-center shrink-0">
                    <Icon className="w-6 h-6" />
                </div>
                <div>
                    <div className="text-[13.5px] font-medium text-slate-900">
                        Connect with {label}
                    </div>
                    <div className="text-[11.5px] text-slate-500">
                        {viaCloud
                            ? `Warmbly Cloud opens the ${label} window on its own app. Approve and you're done.`
                            : `We'll open a ${label} window. Approve the scopes and you're done.`}
                    </div>
                </div>
            </div>

            {viaCloud ? (
                <ul className="text-[11.5px] text-slate-600 space-y-1.5 px-1">
                    <Scope>Sends campaigns and syncs replies from this server, as usual</Scope>
                    <Scope>Warmbly Cloud keeps the sign-in and warms the mailbox in its pool</Scope>
                    <Scope>The mailbox also appears in your cloud workspace; remove it from either side</Scope>
                </ul>
            ) : (
                <ul className="text-[11.5px] text-slate-600 space-y-1.5 px-1">
                    <Scope>Send and read mail on your behalf</Scope>
                    <Scope>Track replies and deliveries</Scope>
                    <Scope>Refresh tokens are stored encrypted; revoke any time</Scope>
                </ul>
            )}

            <motion.button
                type="button"
                onClick={onConnect}
                disabled={busy}
                whileTap={busy ? undefined : { scale: 0.985 }}
                className="w-full h-9 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12.5px] font-medium inline-flex items-center justify-center gap-2 transition-colors disabled:opacity-60"
            >
                {busy ? (
                    <Loader2Icon className="w-3.5 h-3.5 animate-spin" />
                ) : (
                    <ShieldCheckIcon className="w-3.5 h-3.5" />
                )}
                {busy ? "Waiting for authorization…" : `Continue with ${label}`}
            </motion.button>
        </div>
    );
}

function Scope({ children }: { children: React.ReactNode }) {
    return (
        <li className="flex items-start gap-2">
            <CheckIcon className="w-3 h-3 text-slate-400 mt-1 shrink-0" />
            <span>{children}</span>
        </li>
    );
}

function SmtpImapPanel({ onDone }: { onDone: () => void }) {
    const [name, setName] = React.useState("");
    const [email, setEmail] = React.useState("");

    const [imapHost, setImapHost] = React.useState("");
    const [imapPort, setImapPort] = React.useState("993");
    const [imapUser, setImapUser] = React.useState("");
    const [imapPass, setImapPass] = React.useState("");
    const [imapSecurity, setImapSecurity] = React.useState<MailSecurity>("tls");

    const [smtpHost, setSmtpHost] = React.useState("");
    const [smtpPort, setSmtpPort] = React.useState("587");
    const [smtpUser, setSmtpUser] = React.useState("");
    const [smtpPass, setSmtpPass] = React.useState("");
    const [smtpSecurity, setSmtpSecurity] = React.useState<MailSecurity>("starttls");

    // The port implies the security mode for every conventional setup, so
    // typing a port moves the selector with it. Once the user picks a mode by
    // hand we stop guessing: that is exactly the non-standard case they came
    // here for (a submission relay on 2525, IMAP on a custom port).
    const imapSecurityTouched = React.useRef(false);
    const smtpSecurityTouched = React.useRef(false);
    React.useEffect(() => {
        if (!imapSecurityTouched.current) {
            setImapSecurity(defaultImapSecurity(Number(imapPort)));
        }
    }, [imapPort]);
    React.useEffect(() => {
        if (!smtpSecurityTouched.current) {
            setSmtpSecurity(defaultSmtpSecurity(Number(smtpPort)));
        }
    }, [smtpPort]);

    // Single-credentials toggle — covers the 90% case where IMAP and SMTP
    // share the same login. The user can flip it off and supply distinct
    // SMTP creds for legacy setups.
    const [sameCreds, setSameCreds] = React.useState(true);
    const [submitting, setSubmitting] = React.useState(false);

    // Auto-fill the username fields from the email address so the user
    // doesn't have to re-type it. Cleared if they touched the user field.
    const imapUserTouched = React.useRef(false);
    const smtpUserTouched = React.useRef(false);
    React.useEffect(() => {
        if (!imapUserTouched.current) setImapUser(email);
        if (!smtpUserTouched.current && !sameCreds) setSmtpUser(email);
    }, [email, sameCreds]);

    function effectiveSmtp() {
        return sameCreds
            ? { user: imapUser, pass: imapPass, host: smtpHost, port: Number(smtpPort) }
            : { user: smtpUser, pass: smtpPass, host: smtpHost, port: Number(smtpPort) };
    }

    function valid() {
        if (!name.trim() || !email.trim()) return false;
        if (!imapHost.trim() || !imapPort.trim() || !imapUser.trim() || !imapPass) return false;
        if (!smtpHost.trim() || !smtpPort.trim()) return false;
        if (!sameCreds && (!smtpUser.trim() || !smtpPass)) return false;
        // Any routable port is allowed; the security mode carries how to
        // connect, so 2525 and other non-standard ports work.
        if (!validPort(Number(smtpPort)) || !validPort(Number(imapPort))) return false;
        return true;
    }

    async function submit() {
        if (submitting || !valid()) return;
        setSubmitting(true);
        const eff = effectiveSmtp();
        try {
            await toast.promise(
                addEmail({
                    name: name.trim(),
                    email: email.trim(),
                    imap: {
                        username: imapUser.trim(),
                        password: imapPass,
                        host: imapHost.trim(),
                        port: Number(imapPort),
                        security: imapSecurity,
                    },
                    smtp: {
                        username: eff.user.trim(),
                        password: eff.pass,
                        host: eff.host.trim(),
                        port: eff.port,
                        security: smtpSecurity,
                    },
                }),
                {
                    loading: "Verifying credentials…",
                    success: "Mailbox connected",
                    error: (e: AppError) => buildError(e),
                },
            );
            onDone();
        } catch {
            /* surfaced by toast */
        } finally {
            setSubmitting(false);
        }
    }

    return (
        <div>
            <Section title="Account" sub="Name and address you send from" icon={<MailIcon className="w-3.5 h-3.5" />}>
                <Field label="Name">
                    <TextInput value={name} onChange={setName} placeholder="Alex Rivera" />
                </Field>
                <Field label="Email">
                    <TextInput value={email} onChange={setEmail} placeholder="alex@company.com" />
                </Field>
            </Section>

            <Section title="IMAP" sub="Incoming, usually 993" icon={<InboxIcon className="w-3.5 h-3.5" />}>
                <Field label="Server">
                    <HostPortInput
                        host={imapHost}
                        onHost={setImapHost}
                        hostPlaceholder="imap.example.com"
                        port={imapPort}
                        onPort={setImapPort}
                        portPlaceholder="993"
                    />
                </Field>
                <Field label="Security">
                    <SecuritySelect
                        value={imapSecurity}
                        onChange={(v) => {
                            imapSecurityTouched.current = true;
                            setImapSecurity(v);
                        }}
                    />
                </Field>
                <Field label="Username">
                    <TextInput
                        value={imapUser}
                        onChange={(v) => {
                            imapUserTouched.current = true;
                            setImapUser(v);
                        }}
                        placeholder={email || "alex@company.com"}
                    />
                </Field>
                <Field label="Password">
                    <TextInput value={imapPass} onChange={setImapPass} placeholder="App password" type="password" />
                </Field>
            </Section>

            <Section title="SMTP" sub="Outgoing, usually 587 or 465" icon={<SendIcon className="w-3.5 h-3.5" />}>
                <Field label="Server">
                    <HostPortInput
                        host={smtpHost}
                        onHost={setSmtpHost}
                        hostPlaceholder="smtp.example.com"
                        port={smtpPort}
                        onPort={setSmtpPort}
                        portPlaceholder="587"
                    />
                </Field>
                <Field label="Security">
                    <SecuritySelect
                        value={smtpSecurity}
                        onChange={(v) => {
                            smtpSecurityTouched.current = true;
                            setSmtpSecurity(v);
                        }}
                    />
                </Field>
                <label className="flex items-center gap-2 pl-[76px] pt-0.5 cursor-pointer">
                    <input
                        type="checkbox"
                        checked={sameCreds}
                        onChange={(e) => setSameCreds(e.target.checked)}
                        className="size-3.5 rounded border-slate-300 accent-slate-900"
                    />
                    <span className="text-[11.5px] text-slate-600">
                        Use the same login as IMAP
                    </span>
                </label>
                <AnimatePresence initial={false}>
                    {!sameCreds && (
                        <motion.div
                            key="smtp-creds"
                            initial={{ height: 0, opacity: 0 }}
                            animate={{ height: "auto", opacity: 1 }}
                            exit={{ height: 0, opacity: 0 }}
                            transition={{ duration: 0.2, ease: [0.32, 0.72, 0, 1] }}
                            className="overflow-hidden"
                        >
                            <div className="space-y-2 pt-2">
                                <Field label="Username">
                                    <TextInput
                                        value={smtpUser}
                                        onChange={(v) => {
                                            smtpUserTouched.current = true;
                                            setSmtpUser(v);
                                        }}
                                        placeholder={email || "alex@company.com"}
                                    />
                                </Field>
                                <Field label="Password">
                                    <TextInput value={smtpPass} onChange={setSmtpPass} placeholder="App password" type="password" />
                                </Field>
                            </div>
                        </motion.div>
                    )}
                </AnimatePresence>
            </Section>

            <div className="px-4 py-2.5 border-t border-slate-200 bg-slate-50/60 flex items-center gap-2 min-w-0 sticky bottom-0">
                <div className="flex items-center gap-1.5 text-[11px] text-slate-500 min-w-0 flex-1">
                    <KeyRoundIcon className="w-3 h-3 shrink-0" />
                    <span className="truncate">Verified against your server before saving.</span>
                </div>
                <motion.button
                    type="button"
                    onClick={submit}
                    disabled={!valid() || submitting}
                    whileTap={valid() && !submitting ? { scale: 0.97 } : undefined}
                    className={cn(
                        "shrink-0 h-7 px-3 rounded-md text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors",
                        "bg-slate-900 hover:bg-slate-800 text-white disabled:opacity-50 disabled:cursor-not-allowed",
                    )}
                >
                    {submitting ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <CheckIcon className="w-3 h-3" />}
                    Connect
                </motion.button>
            </div>
        </div>
    );
}

function Section({
    title,
    sub,
    icon,
    children,
}: {
    title: string;
    sub: string;
    icon: React.ReactNode;
    children: React.ReactNode;
}) {
    return (
        <div className="px-4 py-3 border-b border-slate-200/60 last:border-b-0 min-w-0">
            <div className="flex items-center gap-1.5 mb-2 min-w-0">
                <span className="text-slate-500 shrink-0">{icon}</span>
                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium shrink-0">
                    {title}
                </span>
                <div className="h-3 w-px bg-slate-200 shrink-0" />
                <span className="text-[11.5px] text-slate-500 truncate min-w-0">{sub}</span>
            </div>
            <div className="space-y-2 min-w-0">{children}</div>
        </div>
    );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
    return (
        <div className="flex items-center gap-3 min-w-0">
            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium w-16 shrink-0">
                {label}
            </span>
            <div className="flex-1 min-w-0">{children}</div>
        </div>
    );
}

// HostPortInput — one bordered field that holds host (flex) and port
// (fixed 56px) with a hairline divider between them. Treating it as a
// single visual input avoids the old "Port label + tiny input" pinch
// that was overflowing on narrow modal widths.
function HostPortInput({
    host,
    onHost,
    hostPlaceholder,
    port,
    onPort,
    portPlaceholder,
}: {
    host: string;
    onHost: (v: string) => void;
    hostPlaceholder: string;
    port: string;
    onPort: (v: string) => void;
    portPlaceholder: string;
}) {
    return (
        <div className="flex items-stretch h-7 rounded-md border border-slate-200 bg-white focus-within:border-sky-400 focus-within:ring-2 focus-within:ring-sky-100 transition-colors min-w-0 overflow-hidden">
            <input
                value={host}
                onChange={(e) => onHost(e.target.value)}
                placeholder={hostPlaceholder}
                className="flex-1 min-w-0 px-2.5 bg-transparent outline-none text-[12.5px] text-slate-900 placeholder:text-slate-400"
            />
            <div className="w-px bg-slate-200 shrink-0" />
            <input
                value={port}
                onChange={(e) => onPort(e.target.value)}
                placeholder={portPlaceholder}
                inputMode="numeric"
                className="w-14 shrink-0 px-2 bg-slate-50/60 outline-none text-[12.5px] text-slate-900 placeholder:text-slate-400 tabular-nums text-center"
            />
        </div>
    );
}
