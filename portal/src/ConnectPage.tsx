// The /connect/$token route: a client connects their own mailboxes into the
// workspace that sent the link, by Google or Microsoft sign-in or by server
// details. Works on its own tab and inside an iframe on the agency's website.

import type React from "react";
import { useCallback, useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";

import { StalePageError } from "./api";
import { BrandFooter, BrandMark } from "./Brand";
import {
    ConnectError,
    type ConnectLink,
    type ConnectProvider,
    connectSMTPIMAP,
    fetchConnectLink,
    fetchSigninOutcome,
    startConnectOAuth,
} from "./connectApi";
import {
    closeIfSigninWindow,
    navigateForSignin,
    openSigninWindow,
    postConnected,
    readSigninResult,
    type SigninResult,
    startConnectResizeReporting,
} from "./connectEmbed";
import { CUSTOM_PRESET, MAIL_PRESETS } from "./connectPresets";
import { VendorForm } from "./ConnectVendor";
import { isEmbedded } from "./embed";
import { AlertIcon, BackIcon, CheckIcon, ChevronIcon, MailIcon, StackIcon } from "./icons";
import { useBrandLook } from "./look";
import { StalePage } from "./NotFound";
import type { Brand } from "./theme";
import { Turnstile } from "./Turnstile";
import { resetTurnstile } from "./turnstileScript";
import "./connect.css";

interface Connected {
    email: string;
    provider: string;
}

type View = "choose" | "imap" | "vendor" | "waiting";

const POLL_MS = 2000;
const POLL_MAX_MS = 10 * 60 * 1000;

const PROVIDER_LABEL: Record<string, string> = {
    gmail: "Google",
    outlook: "Microsoft",
    smtp_imap: "Server details",
};

// Terminal codes: the link cannot connect anything, whatever the client does.
const TERMINAL: Record<string, { title: string; body: string }> = {
    client_link_not_found: { title: "This link does not exist", body: "Check the address you were sent, or ask for a new link." },
    client_link_expired: { title: "This link has expired", body: "Ask whoever sent it for a new one." },
    client_link_revoked: { title: "This link was turned off", body: "Ask whoever sent it for a new one." },
    client_link_exhausted: { title: "This link is complete", body: "Every mailbox it allows is connected. Ask for a new link to connect more." },
};

function storageKey(token: string) {
    return `warmbly-connect:${token.slice(0, 12)}`;
}

function loadConnected(token: string): Connected[] {
    try {
        const raw = sessionStorage.getItem(storageKey(token));
        return raw ? (JSON.parse(raw) as Connected[]) : [];
    } catch {
        return [];
    }
}

function saveConnected(token: string, list: Connected[]) {
    try {
        sessionStorage.setItem(storageKey(token), JSON.stringify(list));
    } catch {
        // private mode: the list lives only as long as the page
    }
}

// errorCopy is what the client reads for a refusal; the workspace's own allowance is put in the client's terms.
function errorCopy(err: unknown, workspace: string): string {
    if (err instanceof ConnectError) {
        if (err.code === "mailbox_allowance_reached") {
            return `${workspace} cannot take more mailboxes right now. Let them know, then try again.`;
        }
        if (err.code === "rate_limit_exceeded") return "Too many attempts from this address. Wait a few minutes and try again.";
        return err.message;
    }
    return "Something went wrong. Try again.";
}

export function ConnectPage() {
    const { token } = useParams({ from: "/connect/$token" });
    const qc = useQueryClient();
    const embedded = isEmbedded();
    const query = useQuery({
        queryKey: ["connect", token],
        queryFn: () => fetchConnectLink(token),
        staleTime: 30_000,
        retry: (count, err) => !(err instanceof ConnectError && err.status < 500) && !(err instanceof StalePageError) && count < 2,
    });

    const [connected, setConnected] = useState<Connected[]>(() => loadConnected(token));
    const [view, setView] = useState<View>("choose");
    const [error, setError] = useState<string | null>(null);
    const [returned, setReturned] = useState<SigninResult | null>(null);

    // A sign-in that came back to this tab (no window could open, or this is that window).
    useEffect(() => {
        const r = readSigninResult();
        if (!r) return;
        setReturned(r);
        if (r.connected) closeIfSigninWindow();
        else if (r.error) setError(signinErrorMessage(r.error));
    }, []);

    const addConnected = useCallback(
        (m: Connected, count?: number) => {
            setConnected((prev) => {
                if (prev.some((p) => p.email.toLowerCase() === m.email.toLowerCase())) return prev;
                const next = [...prev, m];
                saveConnected(token, next);
                return next;
            });
            if (typeof count === "number") postConnected(count);
            void qc.invalidateQueries({ queryKey: ["connect", token] });
        },
        [qc, token],
    );

    useEffect(() => {
        if (returned?.connected) addConnected({ email: returned.connected, provider: returned.provider ?? "" });
    }, [returned, addConnected]);

    useEffect(() => {
        if (embedded) document.body.classList.add("embed");
        return startConnectResizeReporting();
    }, [embedded]);

    const link = query.data;
    const workspace = link?.workspace ?? "Your agency";
    useEffect(() => {
        document.title = link ? `Connect your mailbox to ${link.workspace}` : "Connect your mailbox";
    }, [link]);
    useBrandLook(link?.brand);

    if (query.isPending) return <Loading />;
    if (query.isError) {
        if (query.error instanceof StalePageError) return <StalePage />;
        const code = query.error instanceof ConnectError ? query.error.code : "";
        const t = TERMINAL[code];
        if (t && connected.length > 0 && code === "client_link_exhausted") {
            return (
                <Shell brand={link?.brand}>
                    <Done connected={connected} workspace="the team that sent this link" />
                </Shell>
            );
        }
        return (
            <Shell brand={link?.brand}>
                <div className="cx-state">
                    <span className="cx-state-icon">
                        <AlertIcon />
                    </span>
                    <h1>{t?.title ?? "This page is temporarily unavailable"}</h1>
                    <p>{t?.body ?? "Try again in a few minutes."}</p>
                    {!t && (
                        <button type="button" className="cx-secondary" disabled={query.isFetching} onClick={() => void query.refetch()}>
                            {query.isFetching ? "Trying…" : "Try again"}
                        </button>
                    )}
                </div>
            </Shell>
        );
    }

    const live = query.data;
    const remaining = Math.max(0, live.max_mailboxes - live.connected);

    return (
        <Shell brand={live.brand}>
            {view === "imap" ? (
                <ImapForm
                    token={token}
                    link={live}
                    onBack={() => setView("choose")}
                    onConnected={(m) => {
                        addConnected({ email: m.email, provider: m.provider }, m.connected);
                        setView("choose");
                    }}
                />
            ) : view === "vendor" ? (
                <VendorForm
                    token={token}
                    link={live}
                    errorCopy={(e) => errorCopy(e, workspace)}
                    onBack={() => setView("choose")}
                    onStarted={() => void qc.invalidateQueries({ queryKey: ["connect", token] })}
                />
            ) : view === "waiting" ? (
                <div className="cx-state">
                    <span className="cx-spinner" aria-hidden />
                    <h1>Finish signing in</h1>
                    <p>Complete the sign-in in the window that opened. This page updates by itself.</p>
                    <button type="button" className="cx-link" onClick={() => setView("choose")}>
                        Cancel
                    </button>
                </div>
            ) : (
                <>
                    <h1 className="cx-title">Connect your mailbox to {workspace}</h1>
                    <p className="cx-sub">
                        {workspace} will send and warm up email from the mailboxes you connect. You sign in with your own provider, and your
                        password is never shared with {workspace}.
                    </p>
                    {connected.length > 0 && <ConnectedList connected={connected} />}
                    {live.workspace_full ? (
                        <p className="cx-error" role="alert">
                            {workspace} cannot take more mailboxes right now. Let them know, then open this link again.
                        </p>
                    ) : remaining === 0 ? (
                        <Done connected={connected} workspace={workspace} />
                    ) : (
                        <>
                            <div className="cx-progress">
                                <p className="cx-count">
                                    {live.connected > 0
                                        ? `${live.connected} of ${live.max_mailboxes} mailboxes connected through this link.`
                                        : live.max_mailboxes === 1
                                          ? "This link connects one mailbox."
                                          : `This link connects up to ${live.max_mailboxes} mailboxes.`}
                                </p>
                                {live.connected > 0 && (
                                    <div className="cx-meter" aria-hidden>
                                        <span style={{ width: `${Math.min(100, (live.connected / live.max_mailboxes) * 100)}%` }} />
                                    </div>
                                )}
                            </div>
                            {error && (
                                <p className="cx-error" role="alert">
                                    {error}
                                </p>
                            )}
                            <ProviderButtons
                                token={token}
                                link={live}
                                onError={(e) => setError(errorCopy(e, workspace))}
                                onWaiting={() => {
                                    setError(null);
                                    setView("waiting");
                                }}
                                onDone={(outcome) => {
                                    setView("choose");
                                    if (outcome.status === "connected" && outcome.email) {
                                        setError(null);
                                        addConnected({ email: outcome.email, provider: outcome.provider ?? "" }, outcome.connected);
                                    } else if (outcome.status === "failed") {
                                        setError(
                                            errorCopy(new ConnectError(outcome.code ?? "", outcome.message ?? "Nothing was connected.", 400), workspace),
                                        );
                                    }
                                }}
                                onImap={() => {
                                    setError(null);
                                    setView("imap");
                                }}
                                onVendor={() => {
                                    setError(null);
                                    setView("vendor");
                                }}
                            />
                        </>
                    )}
                </>
            )}
        </Shell>
    );
}

function signinErrorMessage(code: string): string {
    switch (code) {
        case "client_link_signin_declined":
            return "The sign-in was cancelled, so nothing was connected.";
        case "client_link_signin_expired":
            return "This sign-in expired or was already used. Start again.";
        case "mailbox_allowance_reached":
            return "The team that sent this link cannot take more mailboxes right now. Let them know, then try again.";
        default:
            return TERMINAL[code]?.body ?? "The mailbox was not connected. Try again, or connect it with its server details.";
    }
}

// Shell draws in the look already on the document; a link refused on first load has no brand, so no mark and no footer.
function Shell({ brand, children }: { brand: Brand | undefined; children: React.ReactNode }) {
    const hasMark = brand !== undefined && Boolean(brand.logoURL || brand.name);
    return (
        <div className="cx">
            {brand && hasMark && (
                <header className="cx-head">
                    <BrandMark brand={brand} />
                </header>
            )}
            <main className="cx-card">{children}</main>
            {brand && <BrandFooter brand={brand} className="cx-foot" />}
        </div>
    );
}

// Loading keeps the shape of the page it becomes, so nothing jumps when the link arrives.
function Loading() {
    return (
        <div className="cx" aria-busy="true">
            <div className="cx-head">
                <span className="pt-sk cx-sk-mark" />
            </div>
            <main className="cx-card" role="status">
                <span className="pt-sr">Loading</span>
                <span className="pt-sk cx-sk-title" />
                <span className="pt-sk cx-sk-line" />
                <span className="pt-sk cx-sk-line cx-sk-short" />
                <span className="pt-sk cx-sk-button" />
                <span className="pt-sk cx-sk-button" />
                <span className="pt-sk cx-sk-button" />
            </main>
        </div>
    );
}

function ConnectedList({ connected }: { connected: Connected[] }) {
    return (
        <ul className="cx-connected" aria-label="Connected mailboxes">
            {connected.map((m) => (
                <li key={m.email}>
                    <span className="cx-check">
                        <CheckIcon />
                    </span>
                    <span className="cx-addr">{m.email}</span>
                    {m.provider && <span className="cx-via">{PROVIDER_LABEL[m.provider] ?? ""}</span>}
                </li>
            ))}
        </ul>
    );
}

function Done({ connected, workspace }: { connected: Connected[]; workspace: string }) {
    return (
        <div className="cx-state">
            <span className="cx-state-icon is-ok">
                <CheckIcon />
            </span>
            <h1>You're all set</h1>
            <p>
                {connected.length > 0
                    ? `${connected.length === 1 ? "Your mailbox is" : "Your mailboxes are"} connected to ${workspace}. You can close this page.`
                    : "Every mailbox this link allows is connected. You can close this page."}
            </p>
            {connected.length > 0 && <ConnectedList connected={connected} />}
        </div>
    );
}

function ProviderButtons({
    token,
    link,
    onError,
    onWaiting,
    onDone,
    onImap,
    onVendor,
}: {
    token: string;
    link: ConnectLink;
    onError: (e: unknown) => void;
    onWaiting: () => void;
    onDone: (o: { status: string; email?: string; provider?: string; code?: string; message?: string; connected?: number }) => void;
    onImap: () => void;
    onVendor: () => void;
}) {
    const [busy, setBusy] = useState<ConnectProvider | null>(null);
    const poll = useRef<number | null>(null);
    useEffect(() => () => {
        if (poll.current) window.clearTimeout(poll.current);
    }, []);

    const signIn = async (provider: ConnectProvider) => {
        // Opened inside the click, before the URL exists, so no popup blocker treats it as unsolicited.
        const win = openSigninWindow();
        setBusy(provider);
        try {
            const started = await startConnectOAuth(token, provider);
            if (!win) {
                navigateForSignin(started.url);
                return;
            }
            win.location.href = started.url;
            onWaiting();
            const deadline = Date.now() + POLL_MAX_MS;
            const tick = async () => {
                try {
                    const o = await fetchSigninOutcome(token, started.result_key);
                    if (o.status !== "pending") {
                        onDone(o);
                        return;
                    }
                } catch {
                    // a dropped poll is retried on the next tick
                }
                if (Date.now() > deadline) {
                    onDone({ status: "failed", code: "client_link_signin_expired", message: "The sign-in took too long. Start again." });
                    return;
                }
                poll.current = window.setTimeout(tick, POLL_MS);
            };
            poll.current = window.setTimeout(tick, POLL_MS);
        } catch (e) {
            win?.close();
            onError(e);
        } finally {
            setBusy(null);
        }
    };

    const has = (p: ConnectProvider) => link.providers.includes(p);
    return (
        <div className="cx-buttons">
            {has("gmail") && (
                <button type="button" className="cx-provider" disabled={busy !== null} onClick={() => void signIn("gmail")}>
                    <GoogleMark />
                    {busy === "gmail" ? "Opening Google…" : "Continue with Google"}
                </button>
            )}
            {has("outlook") && (
                <button type="button" className="cx-provider" disabled={busy !== null} onClick={() => void signIn("outlook")}>
                    <MicrosoftMark />
                    {busy === "outlook" ? "Opening Microsoft…" : "Continue with Microsoft"}
                </button>
            )}
            {has("smtp_imap") && (
                <button type="button" className="cx-provider" disabled={busy !== null} onClick={onImap}>
                    <MailIcon className="cx-mark cx-mark-mail" />
                    Use IMAP and SMTP details
                </button>
            )}
            {has("vendor") && (link.vendors?.length ?? 0) > 0 && (
                <button type="button" className="cx-provider" disabled={busy !== null} onClick={onVendor}>
                    <StackIcon className="cx-mark cx-mark-mail" />
                    Connect many from my inbox vendor
                </button>
            )}
            {link.providers.length === 0 && <p className="cx-error">This link has no way to connect a mailbox on this server. Ask for a new link.</p>}
        </div>
    );
}

type Security = "tls" | "starttls";

function SecuritySelect({ value, onChange }: { value: Security; onChange: (v: Security) => void }) {
    return (
        <span className="cx-select">
            <select value={value} onChange={(e) => onChange(e.target.value as Security)}>
                <option value="tls">SSL/TLS</option>
                <option value="starttls">STARTTLS</option>
            </select>
            <ChevronIcon />
        </span>
    );
}

function ImapForm({
    token,
    link,
    onBack,
    onConnected,
}: {
    token: string;
    link: ConnectLink;
    onBack: () => void;
    onConnected: (m: { email: string; provider: string; connected: number }) => void;
}) {
    const [preset, setPreset] = useState(CUSTOM_PRESET);
    const [email, setEmail] = useState("");
    const [name, setName] = useState("");
    const [password, setPassword] = useState("");
    const [imapHost, setImapHost] = useState("");
    const [imapPort, setImapPort] = useState("993");
    const [imapSec, setImapSec] = useState<Security>("tls");
    const [smtpHost, setSmtpHost] = useState("");
    const [smtpPort, setSmtpPort] = useState("465");
    const [smtpSec, setSmtpSec] = useState<Security>("tls");
    const [separate, setSeparate] = useState(false);
    const [smtpUser, setSmtpUser] = useState("");
    const [smtpPass, setSmtpPass] = useState("");
    const [captcha, setCaptcha] = useState("");
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState<string | null>(null);

    const chosen = MAIL_PRESETS.find((p) => p.id === preset);
    const choose = (id: string) => {
        setPreset(id);
        const p = MAIL_PRESETS.find((x) => x.id === id);
        if (!p) return;
        setImapHost(p.imap.host);
        setImapPort(String(p.imap.port));
        setImapSec(p.imap.security);
        setSmtpHost(p.smtp.host);
        setSmtpPort(String(p.smtp.port));
        setSmtpSec(p.smtp.security);
    };

    const submit = async (e: React.FormEvent) => {
        e.preventDefault();
        if (link.captcha_site_key && !captcha) {
            setError("Complete the security check first.");
            return;
        }
        setBusy(true);
        setError(null);
        try {
            const user = email.trim();
            const out = await connectSMTPIMAP(token, {
                email: user,
                name: name.trim() || undefined,
                imap: { host: imapHost.trim(), port: Number(imapPort), username: user, password, security: imapSec },
                smtp: {
                    host: smtpHost.trim(),
                    port: Number(smtpPort),
                    username: separate && smtpUser.trim() ? smtpUser.trim() : user,
                    password: separate && smtpPass ? smtpPass : password,
                    security: smtpSec,
                },
                captcha_token: captcha || undefined,
            });
            setPassword("");
            setSmtpPass("");
            onConnected(out);
        } catch (err) {
            setError(errorCopy(err, link.workspace));
            setCaptcha("");
            resetTurnstile();
        } finally {
            setBusy(false);
        }
    };

    return (
        <form className="cx-form" onSubmit={(e) => void submit(e)}>
            <button type="button" className="cx-link cx-back" onClick={onBack} disabled={busy}>
                <BackIcon /> Other ways to connect
            </button>
            <h1 className="cx-title">Connect with server details</h1>
            <p className="cx-sub">Your password is checked against your mail server and stored encrypted. {link.workspace} never sees it.</p>

            <label className="cx-field">
                <span>Mail provider</span>
                <span className="cx-select">
                    <select value={preset} onChange={(e) => choose(e.target.value)} disabled={busy}>
                        <option value={CUSTOM_PRESET}>Other (enter the servers)</option>
                        {MAIL_PRESETS.map((p) => (
                            <option key={p.id} value={p.id}>
                                {p.label}
                            </option>
                        ))}
                    </select>
                    <ChevronIcon />
                </span>
            </label>
            <div className="cx-row">
                <label className="cx-field">
                    <span>Email address</span>
                    <input type="email" required autoComplete="email" value={email} onChange={(e) => setEmail(e.target.value)} disabled={busy} />
                </label>
                <label className="cx-field">
                    <span>Sender name (optional)</span>
                    <input type="text" autoComplete="name" maxLength={50} value={name} onChange={(e) => setName(e.target.value)} disabled={busy} />
                </label>
            </div>
            <label className="cx-field">
                <span>Password</span>
                <input type="password" required autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} disabled={busy} />
                {chosen?.hint && <small>{chosen.hint}</small>}
            </label>

            <fieldset className="cx-servers" disabled={busy}>
                <legend>Incoming (IMAP)</legend>
                <div className="cx-row cx-row-server">
                    <label className="cx-field">
                        <span>Host</span>
                        <input required value={imapHost} onChange={(e) => setImapHost(e.target.value)} placeholder="imap.example.com" />
                    </label>
                    <label className="cx-field cx-port">
                        <span>Port</span>
                        <input required inputMode="numeric" pattern="[0-9]{1,5}" value={imapPort} onChange={(e) => setImapPort(e.target.value)} />
                    </label>
                    <label className="cx-field cx-sec">
                        <span>Security</span>
                        <SecuritySelect value={imapSec} onChange={setImapSec} />
                    </label>
                </div>
            </fieldset>
            <fieldset className="cx-servers" disabled={busy}>
                <legend>Outgoing (SMTP)</legend>
                <div className="cx-row cx-row-server">
                    <label className="cx-field">
                        <span>Host</span>
                        <input required value={smtpHost} onChange={(e) => setSmtpHost(e.target.value)} placeholder="smtp.example.com" />
                    </label>
                    <label className="cx-field cx-port">
                        <span>Port</span>
                        <input required inputMode="numeric" pattern="[0-9]{1,5}" value={smtpPort} onChange={(e) => setSmtpPort(e.target.value)} />
                    </label>
                    <label className="cx-field cx-sec">
                        <span>Security</span>
                        <SecuritySelect value={smtpSec} onChange={setSmtpSec} />
                    </label>
                </div>
                <label className="cx-check-row">
                    <input type="checkbox" checked={separate} onChange={(e) => setSeparate(e.target.checked)} />
                    <span>Outgoing mail uses a different login</span>
                </label>
                {separate && (
                    <div className="cx-row">
                        <label className="cx-field">
                            <span>SMTP username</span>
                            <input value={smtpUser} onChange={(e) => setSmtpUser(e.target.value)} />
                        </label>
                        <label className="cx-field">
                            <span>SMTP password</span>
                            <input type="password" autoComplete="off" value={smtpPass} onChange={(e) => setSmtpPass(e.target.value)} />
                        </label>
                    </div>
                )}
            </fieldset>

            {link.captcha_site_key && <Turnstile siteKey={link.captcha_site_key} onToken={setCaptcha} theme={link.brand.theme.appearance} />}
            {error && (
                <p className="cx-error" role="alert">
                    {error}
                </p>
            )}
            <button type="submit" className="cx-primary" disabled={busy}>
                {busy ? (
                    <>
                        <span className="cx-spinner cx-spinner-sm" aria-hidden /> Checking your mailbox…
                    </>
                ) : (
                    "Connect mailbox"
                )}
            </button>
            {busy && <p className="cx-note">Signing in to your mail server takes a few seconds.</p>}
        </form>
    );
}

function GoogleMark() {
    return (
        <svg className="cx-mark" viewBox="0 0 18 18" aria-hidden>
            <path fill="#4285F4" d="M17.64 9.2c0-.64-.06-1.25-.16-1.84H9v3.48h4.84a4.14 4.14 0 0 1-1.8 2.72v2.26h2.92c1.7-1.57 2.68-3.88 2.68-6.62z" />
            <path fill="#34A853" d="M9 18c2.43 0 4.47-.8 5.96-2.18l-2.92-2.26c-.8.54-1.84.86-3.04.86-2.34 0-4.32-1.58-5.03-3.7H.96v2.33A9 9 0 0 0 9 18z" />
            <path fill="#FBBC05" d="M3.97 10.72A5.4 5.4 0 0 1 3.68 9c0-.6.1-1.18.29-1.72V4.95H.96A9 9 0 0 0 0 9c0 1.45.35 2.83.96 4.05l3.01-2.33z" />
            <path fill="#EA4335" d="M9 3.58c1.32 0 2.5.45 3.44 1.35l2.58-2.58A9 9 0 0 0 .96 4.95l3.01 2.33C4.68 5.16 6.66 3.58 9 3.58z" />
        </svg>
    );
}

function MicrosoftMark() {
    return (
        <svg className="cx-mark" viewBox="0 0 18 18" aria-hidden>
            <path fill="#F25022" d="M1 1h7.5v7.5H1z" />
            <path fill="#7FBA00" d="M9.5 1H17v7.5H9.5z" />
            <path fill="#00A4EF" d="M1 9.5h7.5V17H1z" />
            <path fill="#FFB900" d="M9.5 9.5H17V17H9.5z" />
        </svg>
    );
}
