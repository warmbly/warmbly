// Connect popup for a built-in integration. OAuth providers authorize in the
// provider's own window while this one waits; credential providers take their
// fields on a second step; inbound providers mint a URL. Every path ends on a
// connected screen that offers the next step instead of opening a drawer.

"use client";

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import {
    AlertCircleIcon,
    ArrowLeftIcon,
    CheckIcon,
    ChevronDownIcon,
    ExternalLinkIcon,
    Loader2Icon,
    LockIcon,
    ShieldCheckIcon,
    XIcon,
} from "lucide-react";

import { Logo } from "@/components/svg";
import { FieldError, Label, TextInput } from "@/components/ui/field";
import { OptionSelect } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import { useConfirm } from "@/hooks/context/confirm";
import useConnectIntegration from "@/lib/api/hooks/app/integrations/useConnectIntegration";
import { useFinishIntegrationOAuth, useStartIntegrationOAuth } from "@/lib/api/hooks/app/integrations/useIntegrationOAuth";
import { authorizeInPopup } from "@/lib/integrations/oauthPopup";
import type { IntegrationCatalogEntry, IntegrationConnection } from "@/lib/api/models/app/integrations/Integration";
import { cn } from "@/lib/utils";

import ProviderGlyph from "./ProviderGlyph";

interface FieldDef {
    key: string;
    label: string;
    placeholder?: string;
    helper?: string;
    type?: "text" | "password";
    required?: boolean;
}

// Credential fields for non-OAuth providers only. OAuth providers never paste.
const FIELDS_BY_PROVIDER: Record<string, FieldDef[]> = {
    companyenrich: [{ key: "api_key", label: "CompanyEnrich API key", type: "password", required: true, helper: "Copy your API key from app.companyenrich.com. Connecting checks your key for free; research and enrichment requests can use credits." }],
    apollo: [
        {
            key: "api_key",
            label: "Apollo API key",
            type: "password",
            required: true,
            helper: "Settings → Integrations → API in Apollo. Use a master key, or one with access to lists, contact search and people enrichment. It is checked before it is saved.",
        },
    ],
    cleanmylist: [
        {
            key: "api_key",
            label: "CleanMyList API key",
            type: "password",
            required: true,
            helper: "App → API keys in CleanMyList. Verify your account email first. Connecting is free; checks use your plan allowance, then credits.",
        },
    ],
    millionverifier: [
        {
            key: "api_key",
            label: "MillionVerifier API key",
            type: "password",
            required: true,
            helper: "API → API key in your MillionVerifier account. The key is checked before it is saved; one credit is spent per address verified.",
        },
    ],
    close: [
        { key: "workspace", label: "Organization", placeholder: "Acme" },
        {
            key: "api_token",
            label: "Close API key",
            type: "password",
            required: true,
            helper: "Settings → Developer → API Keys in Close.",
        },
    ],
    discord: [
        { key: "server", label: "Server name", placeholder: "Acme" },
        {
            key: "webhook_url",
            label: "Channel webhook URL",
            type: "password",
            required: true,
            helper: "Edit Channel → Integrations → Webhooks → New Webhook → Copy URL.",
        },
    ],
};

// What to do right after connecting, said in one line.
const NEXT_STEP: Record<string, string> = {
    slack: "Mention @Warmbly in any channel or message it to ask about your outreach. Pick where notifications and inbox replies go in setup.",
    hubspot: "Finish the CRM setup: what syncs, and in which direction.",
    pipedrive: "Finish the CRM setup: what syncs, and in which direction.",
    salesforce: "Finish the setup: sync rules, field mapping and the first import.",
    apollo: "Import your first Apollo list, or enrich contacts you already have.",
    companyenrich: "Research companies and people, or fill empty fields on an existing contact.",
    discord: "Choose which Warmbly events post to the channel.",
};

type SalesforceEnv = "production" | "sandbox" | "custom";
type Phase = "overview" | "credentials" | "waiting" | "done";

// Accepts "acme.my.salesforce.com" or a pasted URL; returns the bare host or "".
function salesforceHost(raw: string): string {
    const host = raw.trim().toLowerCase().replace(/^https?:\/\//, "").replace(/\/.*$/, "");
    return /^[a-z0-9-]+(\.[a-z0-9-]+)+$/.test(host) ? host : "";
}

// Credential connections are keyed by label, so another account needs its own.
function freeLabel(name: string, existing: IntegrationConnection[]): string {
    const taken = new Set(existing.map((c) => c.label));
    if (!taken.has(name)) return name;
    for (let n = 2; ; n++) if (!taken.has(`${name} ${n}`)) return `${name} ${n}`;
}

const ORDER: Record<Phase, number> = { overview: 0, credentials: 1, waiting: 1, done: 2 };

const paneVariants = {
    enter: (dir: 1 | -1) => ({ x: dir * 24, opacity: 0 }),
    center: { x: 0, opacity: 1 },
    exit: (dir: 1 | -1) => ({ x: dir * -24, opacity: 0 }),
};

const primaryBtn =
    "h-8 px-3.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12.5px] font-medium inline-flex items-center justify-center gap-1.5 transition-colors disabled:bg-slate-200 disabled:text-slate-500 disabled:cursor-not-allowed";
const secondaryBtn =
    "h-8 px-3 rounded-md border border-slate-200 text-[12.5px] text-slate-700 hover:border-slate-300 hover:text-slate-900 inline-flex items-center gap-1.5 transition-colors";

export default function ConnectDialog({
    entry,
    existing,
    onClose,
    onConnected,
    onSetup,
}: {
    entry: IntegrationCatalogEntry;
    // This provider's connections in the workspace.
    existing: IntegrationConnection[];
    onClose: () => void;
    onConnected: (c: IntegrationConnection) => void;
    // The connected screen's main button, or an inbound URL that must be shown now.
    onSetup: (c: IntegrationConnection) => void;
}) {
    const confirm = useConfirm();
    const connect = useConnectIntegration();
    const startOAuth = useStartIntegrationOAuth();
    const finishOAuth = useFinishIntegrationOAuth();

    const [phase, setPhase] = React.useState<Phase>("overview");
    const [direction, setDirection] = React.useState<1 | -1>(1);
    const [config, setConfig] = React.useState<Record<string, string>>({});
    const [busy, setBusy] = React.useState(false);
    const [error, setError] = React.useState<string | null>(null);
    const [showScopes, setShowScopes] = React.useState(false);
    const [sfEnv, setSfEnv] = React.useState<SalesforceEnv>("production");
    const [sfDomain, setSfDomain] = React.useState("");
    const [connected, setConnected] = React.useState<IntegrationConnection | null>(null);
    // A closed dialog ignores an authorization that finishes afterwards.
    const alive = React.useRef(true);
    React.useEffect(() => {
        alive.current = true;
        return () => {
            alive.current = false;
        };
    }, []);

    const isOAuth = entry.auth_method === "oauth";
    const isInbound = entry.provider === "calendly" || entry.provider === "cal_com" || entry.provider === "clay";
    const isAutomation = entry.provider === "zapier" || entry.provider === "make" || entry.provider === "n8n";
    const isSalesforce = entry.provider === "salesforce";
    const fields = FIELDS_BY_PROVIDER[entry.provider] ?? [];
    const needsCredentials = !isOAuth && !isInbound && fields.length > 0;
    const notConfigured = isOAuth && !entry.configured;
    const sfHost = salesforceHost(sfDomain);
    const sfDomainError =
        isSalesforce && sfEnv === "custom" && sfDomain.trim() !== "" && !sfHost ? "Enter a host like acme.my.salesforce.com" : null;
    const dirty = Object.values(config).some((v) => v.trim() !== "");

    function goTo(next: Phase) {
        setDirection(ORDER[next] >= ORDER[phase] ? 1 : -1);
        setError(null);
        setPhase(next);
    }

    function finish(conn: IntegrationConnection) {
        onConnected(conn);
        // An inbound URL is shown once, so it goes straight to its own dialog.
        if (conn.inbound_webhook_url) {
            onSetup(conn);
            onClose();
            return;
        }
        setConnected(conn);
        goTo("done");
    }

    async function runOAuth() {
        if (isSalesforce && sfEnv === "custom" && !sfHost) {
            setError("Enter your Salesforce My Domain, like acme.my.salesforce.com.");
            return;
        }
        setBusy(true);
        setError(null);
        try {
            const sf = isSalesforce
                ? sfEnv === "custom"
                    ? { environment: sfHost.includes(".sandbox.") ? ("sandbox" as const) : ("production" as const), domain: sfHost }
                    : { environment: sfEnv }
                : {};
            const { code, state } = await authorizeInPopup(
                async () => (await startOAuth.mutateAsync({ provider: entry.provider, label: "", ...sf })).url,
                () => {
                    if (alive.current) goTo("waiting");
                },
            );
            if (!alive.current) return;
            const conn = await finishOAuth.mutateAsync({ code, state });
            if (!alive.current) return;
            finish(conn);
        } catch (err: unknown) {
            if (!alive.current) return;
            setDirection(-1);
            setPhase("overview");
            setError(errMessage(err) ?? `Could not connect ${entry.name}.`);
        } finally {
            if (alive.current) setBusy(false);
        }
    }

    async function submitCredentials(e?: React.FormEvent) {
        e?.preventDefault();
        const missing = fields.find((f) => f.required && !config[f.key]?.trim());
        if (missing) {
            setError(`${missing.label} is required.`);
            return;
        }
        setBusy(true);
        setError(null);
        try {
            const conn = await connect.mutateAsync({ provider: entry.provider, label: freeLabel(entry.name, existing), config });
            if (alive.current) finish(conn);
        } catch (err: unknown) {
            if (alive.current) setError(errMessage(err) ?? `Could not connect ${entry.name}.`);
        } finally {
            if (alive.current) setBusy(false);
        }
    }

    const requestClose = React.useCallback(() => {
        if (phase === "credentials" && dirty && !busy) {
            confirm.show(`Discard the ${entry.name} details you entered?`, async () => onClose());
            return;
        }
        onClose();
    }, [phase, dirty, busy, confirm, entry.name, onClose]);

    React.useEffect(() => {
        const onKey = (ev: KeyboardEvent) => {
            if (ev.key !== "Escape") return;
            // An open dropdown or the confirm owns this Escape.
            if (document.querySelector("[data-floating], [role='alertdialog']")) return;
            ev.preventDefault();
            requestClose();
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [requestClose]);

    const account = connected?.external_account_name?.trim();
    const title =
        phase === "done" ? `${entry.name} is connected` : phase === "waiting" ? `Waiting for ${entry.name}` : `Connect ${entry.name}`;
    const subtitle =
        phase === "done"
            ? account
                ? `Connected to ${account}.`
                : "You can change or disconnect it any time."
            : phase === "waiting"
              ? `Finish in the ${entry.name} window that opened. This updates by itself.`
              : entry.tagline;

    return (
        <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.15 }}
            onMouseDown={requestClose}
            className="fixed inset-0 z-[110] flex items-center justify-center bg-slate-900/30 backdrop-blur-[2px] px-3"
        >
            <motion.div
                role="dialog"
                aria-modal="true"
                aria-label={title}
                initial={{ y: 8, opacity: 0, scale: 0.985 }}
                animate={{ y: 0, opacity: 1, scale: 1 }}
                exit={{ y: 8, opacity: 0, scale: 0.985 }}
                transition={{ duration: 0.18, ease: [0.22, 1, 0.36, 1] }}
                onMouseDown={(ev) => ev.stopPropagation()}
                className="relative w-full max-w-[460px] max-h-[min(90dvh,720px)] rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18),0_8px_16px_-8px_rgba(15,23,42,0.1)] overflow-hidden flex flex-col"
            >
                <button
                    type="button"
                    onClick={requestClose}
                    aria-label="Close"
                    className="absolute top-3 right-3 h-7 w-7 rounded-md text-slate-400 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                >
                    <XIcon className="w-3.5 h-3.5" />
                </button>

                <div className="px-6 pt-7 pb-4 flex flex-col items-center text-center shrink-0">
                    <PairMark provider={entry.provider} name={entry.name} phase={phase} />
                    <h2 className="mt-4 text-[15px] font-semibold text-slate-900">{title}</h2>
                    <p className="mt-1 text-[12.5px] text-slate-500 leading-relaxed max-w-[340px]">{subtitle}</p>
                </div>

                <div className="flex-1 min-h-0 overflow-y-auto overflow-x-hidden">
                    <AnimatePresence mode="wait" initial={false} custom={direction}>
                        <motion.div
                            key={phase}
                            custom={direction}
                            variants={paneVariants}
                            initial="enter"
                            animate="center"
                            exit="exit"
                            transition={{ duration: 0.18, ease: [0.22, 1, 0.36, 1] }}
                            className="px-6 pb-5"
                        >
                            {phase === "overview" && (
                                <div className="space-y-4">
                                    {entry.highlights && entry.highlights.length > 0 && (
                                        <ul className="rounded-md border border-slate-200 divide-y divide-slate-200/70">
                                            {entry.highlights.map((h) => (
                                                <li key={h} className="flex items-start gap-2.5 px-3 py-2 text-[12.5px] text-slate-700">
                                                    <CheckIcon className="w-3.5 h-3.5 text-sky-600 mt-0.5 shrink-0" />
                                                    <span>{h}</span>
                                                </li>
                                            ))}
                                        </ul>
                                    )}

                                    {isAutomation && (
                                        <p className="text-[12px] text-slate-500 leading-relaxed">
                                            No key needed. After connecting, add an automation that sends Warmbly events to your{" "}
                                            {entry.name} webhook URL. For {entry.name} to call Warmbly back, create a scoped key under
                                            Settings → API keys.
                                        </p>
                                    )}

                                    {isSalesforce && (
                                        <div className="space-y-2">
                                            <Label>Salesforce environment</Label>
                                            <OptionSelect
                                                value={sfEnv}
                                                onChange={setSfEnv}
                                                aria-label="Salesforce environment"
                                                options={[
                                                    { value: "production", label: "Production", hint: "login.salesforce.com" },
                                                    { value: "sandbox", label: "Sandbox", hint: "test.salesforce.com" },
                                                    { value: "custom", label: "Custom domain", hint: "Your org's My Domain login" },
                                                ]}
                                            />
                                            {sfEnv === "custom" && (
                                                <div>
                                                    <TextInput
                                                        value={sfDomain}
                                                        onChange={setSfDomain}
                                                        placeholder="acme.my.salesforce.com"
                                                        className="w-full font-mono"
                                                        invalid={!!sfDomainError}
                                                        autoFocus
                                                    />
                                                    <FieldError message={sfDomainError} />
                                                </div>
                                            )}
                                            <p className="text-[11px] text-slate-400 leading-relaxed">
                                                The user you sign in as needs API access. Your Salesforce admin may need to approve the
                                                Warmbly connected app first.
                                            </p>
                                        </div>
                                    )}

                                    {isOAuth && entry.scopes && entry.scopes.length > 0 && (
                                        <div>
                                            <button
                                                type="button"
                                                onClick={() => setShowScopes((v) => !v)}
                                                aria-expanded={showScopes}
                                                className="w-full flex items-center gap-2 text-[12px] text-slate-500 hover:text-slate-900 transition-colors"
                                            >
                                                <ShieldCheckIcon className="w-3.5 h-3.5 text-slate-400" />
                                                <span className="flex-1 text-left">
                                                    {entry.scopes.length} {entry.scopes.length === 1 ? "permission" : "permissions"} requested
                                                </span>
                                                <ChevronDownIcon className={cn("w-3.5 h-3.5 transition-transform", showScopes && "rotate-180")} />
                                            </button>
                                            <AnimatePresence initial={false}>
                                                {showScopes && (
                                                    <motion.div
                                                        initial={{ height: 0, opacity: 0 }}
                                                        animate={{ height: "auto", opacity: 1 }}
                                                        exit={{ height: 0, opacity: 0 }}
                                                        transition={{ duration: 0.16 }}
                                                        className="overflow-hidden"
                                                    >
                                                        <div className="mt-2 flex flex-wrap gap-1">
                                                            {entry.scopes.map((s) => (
                                                                <code
                                                                    key={s}
                                                                    className="px-1.5 py-0.5 rounded bg-slate-100 text-[10.5px] text-slate-600 font-mono"
                                                                >
                                                                    {s}
                                                                </code>
                                                            ))}
                                                        </div>
                                                    </motion.div>
                                                )}
                                            </AnimatePresence>
                                            <p className="mt-2 flex items-center gap-1.5 text-[11px] text-slate-400">
                                                <LockIcon className="w-3 h-3 shrink-0" />
                                                Tokens are encrypted with your workspace key. Warmbly never sees your password.
                                            </p>
                                        </div>
                                    )}

                                    {notConfigured && (
                                        <p className="rounded-md bg-amber-50 px-3 py-2 text-[12px] text-amber-800 leading-relaxed">
                                            {entry.name} is not set up on this Warmbly instance yet. An admin needs to add the {entry.name}{" "}
                                            app credentials first.
                                        </p>
                                    )}
                                </div>
                            )}

                            {phase === "credentials" && (
                                <form id="connect-credentials" onSubmit={submitCredentials} className="space-y-4">
                                    {fields.map((f, i) => (
                                        <div key={f.key}>
                                            <Label>
                                                {f.label}
                                                {!f.required && <span className="ml-1 text-slate-400 font-normal">(optional)</span>}
                                            </Label>
                                            <TextInput
                                                type={f.type ?? "text"}
                                                value={config[f.key] ?? ""}
                                                onChange={(v) => setConfig((c) => ({ ...c, [f.key]: v }))}
                                                placeholder={f.placeholder}
                                                autoComplete={f.type === "password" ? "off" : undefined}
                                                autoFocus={i === fields.findIndex((x) => x.required)}
                                                className="w-full font-mono"
                                            />
                                            {f.helper && <p className="mt-1 text-[11px] text-slate-400 leading-relaxed">{f.helper}</p>}
                                        </div>
                                    ))}
                                </form>
                            )}

                            {phase === "waiting" && (
                                <div className="flex items-center justify-center gap-2 py-3 text-[12px] text-slate-500">
                                    <Loader2Icon className="w-3.5 h-3.5 animate-spin text-sky-600" />
                                    Waiting for you to approve in {entry.name}
                                </div>
                            )}

                            {phase === "done" && (
                                <p className="rounded-md border border-slate-200 bg-slate-50/60 px-3 py-2.5 text-[12.5px] text-slate-600 leading-relaxed">
                                    {NEXT_STEP[entry.provider] ?? `Next, choose what ${entry.name} receives from Warmbly.`}
                                </p>
                            )}
                        </motion.div>
                    </AnimatePresence>

                    <AnimatePresence initial={false}>
                        {error && (
                            <motion.div
                                initial={{ height: 0, opacity: 0 }}
                                animate={{ height: "auto", opacity: 1 }}
                                exit={{ height: 0, opacity: 0 }}
                                transition={{ duration: 0.16 }}
                                className="overflow-hidden"
                            >
                                <div role="alert" className="mx-6 mb-4 flex items-start gap-2 rounded-md bg-rose-50 px-3 py-2 text-[12px] text-rose-700">
                                    <AlertCircleIcon className="w-3.5 h-3.5 mt-0.5 shrink-0" />
                                    <span>{error}</span>
                                </div>
                            </motion.div>
                        )}
                    </AnimatePresence>
                </div>

                <div className="border-t border-slate-200 px-6 py-3 flex items-center gap-2 shrink-0">
                    {phase === "credentials" ? (
                        <button type="button" onClick={() => goTo("overview")} disabled={busy} className={secondaryBtn}>
                            <ArrowLeftIcon className="w-3.5 h-3.5" />
                            Back
                        </button>
                    ) : entry.docs_url && phase === "overview" ? (
                        <a
                            href={entry.docs_url}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="inline-flex items-center gap-1 text-[12px] text-slate-500 hover:text-sky-700 transition-colors"
                        >
                            Docs
                            <ExternalLinkIcon className="w-3 h-3" />
                        </a>
                    ) : null}
                    <div className="ml-auto flex items-center gap-2">
                        {phase === "overview" && (
                            <>
                                <button type="button" onClick={requestClose} className={secondaryBtn}>
                                    Cancel
                                </button>
                                {needsCredentials ? (
                                    <button type="button" onClick={() => goTo("credentials")} className={primaryBtn}>
                                        Continue
                                    </button>
                                ) : (
                                    <button
                                        type="button"
                                        autoFocus
                                        disabled={busy || notConfigured}
                                        onClick={() => void (isOAuth ? runOAuth() : submitCredentials())}
                                        className={primaryBtn}
                                    >
                                        {busy && <Loader2Icon className="w-3.5 h-3.5 animate-spin" />}
                                        {isOAuth
                                            ? error
                                                ? "Try again"
                                                : `Continue to ${entry.name}`
                                            : isInbound
                                              ? "Create inbound URL"
                                              : `Connect ${entry.name}`}
                                    </button>
                                )}
                            </>
                        )}
                        {phase === "credentials" && (
                            <button type="submit" form="connect-credentials" disabled={busy} className={primaryBtn}>
                                {busy && <Loader2Icon className="w-3.5 h-3.5 animate-spin" />}
                                {busy ? "Checking…" : `Connect ${entry.name}`}
                            </button>
                        )}
                        {phase === "waiting" && (
                            <button type="button" onClick={requestClose} className={secondaryBtn}>
                                Cancel
                            </button>
                        )}
                        {phase === "done" && connected && (
                            <>
                                <button type="button" onClick={onClose} className={secondaryBtn}>
                                    Done
                                </button>
                                <button
                                    type="button"
                                    autoFocus
                                    onClick={() => {
                                        onSetup(connected);
                                        onClose();
                                    }}
                                    className={primaryBtn}
                                >
                                    Set up {entry.name}
                                </button>
                            </>
                        )}
                    </div>
                </div>
            </motion.div>
        </motion.div>
    );
}

// Warmbly and the provider side by side, joined by a line that runs while
// waiting and turns into a check once connected.
function PairMark({ provider, name, phase }: { provider: string; name: string; phase: Phase }) {
    const done = phase === "done";
    const waiting = phase === "waiting";
    return (
        <div className="flex items-center" aria-hidden>
            <span className="w-12 h-12 rounded-lg border border-slate-200 bg-white inline-flex items-center justify-center">
                <Logo className="w-6 text-slate-900" />
            </span>
            <span className="relative w-14 h-6 flex items-center justify-center">
                <span className={cn("absolute inset-x-1.5 top-1/2 border-t border-dashed transition-colors", done ? "border-emerald-300" : "border-slate-300")} />
                {waiting && (
                    <motion.span
                        className="absolute top-1/2 -mt-[3px] w-1.5 h-1.5 rounded-full bg-sky-500"
                        initial={{ left: "10%" }}
                        animate={{ left: ["10%", "82%"] }}
                        transition={{ duration: 1.1, repeat: Infinity, ease: "easeInOut", repeatType: "reverse" }}
                    />
                )}
                <AnimatePresence>
                    {done && (
                        <motion.span
                            initial={{ scale: 0.4, opacity: 0 }}
                            animate={{ scale: 1, opacity: 1 }}
                            transition={{ type: "spring", stiffness: 420, damping: 22 }}
                            className="relative w-5 h-5 rounded-full bg-emerald-500 text-white inline-flex items-center justify-center"
                        >
                            <CheckIcon className="w-3 h-3" strokeWidth={3} />
                        </motion.span>
                    )}
                </AnimatePresence>
            </span>
            <span className="rounded-lg">
                <ProviderGlyph provider={provider} name={name} size={12} />
            </span>
        </div>
    );
}

function errMessage(err: unknown): string | undefined {
    const e = err as { response?: { data?: { message?: string; error?: string } }; message?: string };
    return e.response?.data?.message ?? e.response?.data?.error ?? e.message;
}
