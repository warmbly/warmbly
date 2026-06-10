// Multi-step connect drawer. The connect path depends on the provider's
// auth method:
//   - oauth (HubSpot, Slack, Pipedrive, Salesforce): one-click
//     "Connect with X" → provider popup → encrypted tokens stored server-side.
//     No credentials are ever pasted. Providers without server credentials
//     render as "coming soon".
//   - api_key (Close, Zapier, Make, n8n): paste a provider token (Close) or a
//     scoped Warmbly API key (Zapier/Make/n8n).
//   - webhook (Discord): paste a channel webhook URL.
//   - inbound webhook (Calendly, Cal.com): we mint an inbound URL on connect.
//
// Step 1 is always an honest overview — what you get, which scopes are
// requested. Step 2 is the connect action. Step 3 confirms + points to the
// detail drawer where event automations are configured.

"use client";

import React from "react";
import { motion } from "framer-motion";
import {
    ArrowRightIcon,
    CheckCircle2Icon,
    ExternalLinkIcon,
    KeyRoundIcon,
    Loader2Icon,
    LockIcon,
    ShieldCheckIcon,
    XIcon,
    ZapIcon,
} from "lucide-react";
import toast from "react-hot-toast";

import { Label, TextInput } from "@/components/ui/field";
import useConnectIntegration from "@/lib/api/hooks/app/integrations/useConnectIntegration";
import {
    useFinishIntegrationOAuth,
    useStartIntegrationOAuth,
} from "@/lib/api/hooks/app/integrations/useIntegrationOAuth";
import { openOAuthPopup } from "@/lib/integrations/oauthPopup";
import type {
    IntegrationCatalogEntry,
    IntegrationConnection,
} from "@/lib/api/models/app/integrations/Integration";
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
    // Zapier / Make / n8n need no credential to connect — see the note in the
    // overview step. We fan events to a per-automation webhook URL, and the
    // reverse direction authenticates with a Warmbly API key created in the
    // API-keys page (pasted into the tool, not here).
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

export default function ConnectDrawer({
    entry,
    onClose,
    onConnected,
}: {
    entry: IntegrationCatalogEntry;
    onClose: () => void;
    onConnected: (c: IntegrationConnection) => void;
}) {
    const [label, setLabel] = React.useState("");
    const [config, setConfig] = React.useState<Record<string, string>>({});
    const [step, setStep] = React.useState<"overview" | "credentials">("overview");
    const [busy, setBusy] = React.useState(false);

    const connect = useConnectIntegration();
    const startOAuth = useStartIntegrationOAuth();
    const finishOAuth = useFinishIntegrationOAuth();

    const isOAuth = entry.auth_method === "oauth";
    const isInbound = entry.provider === "calendly" || entry.provider === "cal_com";
    const isAutomation =
        entry.provider === "zapier" || entry.provider === "make" || entry.provider === "n8n";
    const fields = FIELDS_BY_PROVIDER[entry.provider] ?? [];
    // Only providers with real credential fields take the extra credentials step.
    const needsCredentials = !isOAuth && !isInbound && fields.length > 0;

    function update(key: string, value: string) {
        setConfig((c) => ({ ...c, [key]: value }));
    }

    async function runOAuth() {
        setBusy(true);
        try {
            const { url } = await startOAuth.mutateAsync({ provider: entry.provider, label: label.trim() });
            const { code, state } = await openOAuthPopup(url);
            const conn = await finishOAuth.mutateAsync({ code, state });
            toast.success(`Connected to ${entry.name}`);
            onConnected(conn);
            onClose();
        } catch (err: unknown) {
            toast.error(errMessage(err) ?? "Connection failed");
        } finally {
            setBusy(false);
        }
    }

    async function submitCredentials(e: React.FormEvent) {
        e.preventDefault();
        for (const f of fields) {
            if (f.required && !config[f.key]?.trim()) {
                toast.error(`${f.label} is required`);
                return;
            }
        }
        setBusy(true);
        try {
            const conn = await connect.mutateAsync({
                provider: entry.provider,
                label: label.trim() || entry.name,
                config,
            });
            toast.success(`Connected to ${entry.name}`);
            onConnected(conn);
            onClose();
        } catch (err: unknown) {
            toast.error(errMessage(err) ?? "Connect failed");
        } finally {
            setBusy(false);
        }
    }

    const notConfigured = isOAuth && !entry.configured;

    return (
        <Drawer title="Connect" name={entry.name} provider={entry.provider} onClose={onClose}>
            {step === "overview" && (
                <>
                    <div className="flex-1 overflow-y-auto px-5 py-5 space-y-5">
                        <p className="text-[12.5px] text-slate-600 leading-relaxed">{entry.tagline}</p>

                        {entry.highlights && entry.highlights.length > 0 && (
                            <div className="space-y-2">
                                <SectionLabel>What you get</SectionLabel>
                                <ul className="space-y-1.5">
                                    {entry.highlights.map((h) => (
                                        <li key={h} className="flex items-start gap-2 text-[12.5px] text-slate-700">
                                            <CheckCircle2Icon className="w-3.5 h-3.5 text-emerald-500 mt-0.5 shrink-0" />
                                            <span>{h}</span>
                                        </li>
                                    ))}
                                </ul>
                            </div>
                        )}

                        {isOAuth && entry.scopes && entry.scopes.length > 0 && (
                            <div className="space-y-2">
                                <SectionLabel>Permissions requested</SectionLabel>
                                <div className="rounded-md border border-slate-200 bg-slate-50/70 divide-y divide-slate-200">
                                    {entry.scopes.map((s) => (
                                        <div key={s} className="flex items-center gap-2 px-2.5 py-1.5">
                                            <ShieldCheckIcon className="w-3 h-3 text-slate-400 shrink-0" />
                                            <code className="text-[10.5px] text-slate-600 font-mono truncate">{s}</code>
                                        </div>
                                    ))}
                                </div>
                                <p className="text-[10.5px] text-slate-400 leading-relaxed flex items-center gap-1">
                                    <LockIcon className="w-3 h-3" />
                                    Tokens are encrypted at rest with your organization key. We never store your password.
                                </p>
                            </div>
                        )}

                        {isAutomation && (
                            <div className="rounded-md border border-sky-200 bg-sky-50/50 px-3 py-2.5 space-y-1.5">
                                <p className="text-[12px] text-slate-700 leading-relaxed">
                                    No key needed to connect. After connecting, add an automation that
                                    sends Warmbly events to your {entry.name} webhook URL.
                                </p>
                                <p className="text-[11px] text-slate-500 leading-relaxed">
                                    Want {entry.name} to call Warmbly back (e.g. create a contact)? Create a
                                    scoped key under Settings → API keys and paste it into {entry.name}.
                                </p>
                            </div>
                        )}

                        <div>
                            <Label>Connection label (optional)</Label>
                            <TextInput value={label} onChange={setLabel} placeholder={entry.name} />
                            <p className="text-[10.5px] text-slate-400 mt-1">
                                Useful if you connect more than one {entry.name} account.
                            </p>
                        </div>

                        {notConfigured && (
                            <div className="rounded-md border border-amber-200 bg-amber-50 px-3 py-2.5">
                                <p className="text-[12px] text-amber-800 leading-relaxed">
                                    {entry.name} OAuth isn’t enabled on this workspace yet. An admin needs to add the{" "}
                                    {entry.name} app credentials. Reach out and we’ll switch it on.
                                </p>
                            </div>
                        )}
                    </div>
                    <DrawerFooter onClose={onClose}>
                        {isOAuth ? (
                            <button
                                type="button"
                                disabled={busy || notConfigured}
                                onClick={runOAuth}
                                className={cn(primaryBtn, (busy || notConfigured) && "opacity-60 cursor-not-allowed")}
                            >
                                {busy ? <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> : <ZapIcon className="w-3.5 h-3.5" />}
                                {busy ? "Connecting…" : `Connect with ${entry.name}`}
                            </button>
                        ) : isInbound ? (
                            <button
                                type="button"
                                disabled={busy}
                                onClick={() => void submitCredentials(new Event("submit") as unknown as React.FormEvent)}
                                className={cn(primaryBtn, busy && "opacity-60")}
                            >
                                {busy ? <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> : <ArrowRightIcon className="w-3.5 h-3.5" />}
                                {busy ? "Creating…" : "Create inbound URL"}
                            </button>
                        ) : needsCredentials ? (
                            <button type="button" onClick={() => setStep("credentials")} className={primaryBtn}>
                                <KeyRoundIcon className="w-3.5 h-3.5" />
                                Continue
                            </button>
                        ) : (
                            <button
                                type="button"
                                disabled={busy}
                                onClick={() => void submitCredentials(new Event("submit") as unknown as React.FormEvent)}
                                className={cn(primaryBtn, busy && "opacity-60")}
                            >
                                {busy ? <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> : <ArrowRightIcon className="w-3.5 h-3.5" />}
                                {busy ? "Connecting…" : `Connect ${entry.name}`}
                            </button>
                        )}
                    </DrawerFooter>
                </>
            )}

            {step === "credentials" && (
                <form onSubmit={submitCredentials} className="flex-1 min-h-0 flex flex-col">
                    <div className="flex-1 overflow-y-auto px-5 py-5 space-y-4">
                        {fields.map((f) => (
                            <div key={f.key}>
                                <Label>
                                    {f.label}
                                    {f.required && <span className="text-rose-500 ml-0.5">*</span>}
                                </Label>
                                <TextInput
                                    type={f.type ?? "text"}
                                    value={config[f.key] ?? ""}
                                    onChange={(v) => update(f.key, v)}
                                    placeholder={f.placeholder}
                                    className="font-mono"
                                />
                                {f.helper && (
                                    <p className="text-[10.5px] text-slate-400 mt-1 leading-relaxed">{f.helper}</p>
                                )}
                            </div>
                        ))}
                        {entry.docs_url && (
                            <a
                                href={entry.docs_url}
                                target="_blank"
                                rel="noopener noreferrer"
                                className="inline-flex items-center gap-1 text-[11px] text-sky-700 hover:underline"
                            >
                                <ExternalLinkIcon className="w-3 h-3" />
                                {entry.name} docs
                            </a>
                        )}
                    </div>
                    <DrawerFooter onClose={() => setStep("overview")} cancelLabel="Back">
                        <button type="submit" disabled={busy} className={cn(primaryBtn, busy && "opacity-60")}>
                            {busy ? <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> : <CheckCircle2Icon className="w-3.5 h-3.5" />}
                            {busy ? "Connecting…" : "Connect"}
                        </button>
                    </DrawerFooter>
                </form>
            )}
        </Drawer>
    );
}

// --- shared drawer chrome (also used by ConnectionDetail) -------------------

export function Drawer({
    title,
    name,
    provider,
    onClose,
    children,
}: {
    title: string;
    name: string;
    provider: string;
    onClose: () => void;
    children: React.ReactNode;
}) {
    return (
        <div className="fixed inset-0 z-40 flex">
            <motion.button
                type="button"
                aria-label="Close"
                onClick={onClose}
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                transition={{ duration: 0.18 }}
                className="absolute inset-0 bg-slate-900/30 backdrop-blur-[2px]"
            />
            <motion.div
                initial={{ x: 28, opacity: 0 }}
                animate={{ x: 0, opacity: 1 }}
                transition={{ duration: 0.24, ease: [0.16, 1, 0.3, 1] }}
                className="ml-auto h-full w-full sm:w-[480px] sm:max-w-[92vw] bg-white shadow-xl flex flex-col z-10 relative"
            >
                <div className="h-12 px-5 border-b border-slate-200 flex items-center gap-3 shrink-0">
                    <ProviderGlyph provider={provider} name={name} size={7} />
                    <div className="min-w-0 flex-1">
                        <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">{title}</div>
                        <div className="text-[12.5px] text-slate-900 font-medium truncate">{name}</div>
                    </div>
                    <button
                        type="button"
                        onClick={onClose}
                        aria-label="Close"
                        className="h-7 w-7 rounded border border-slate-200 hover:border-slate-300 text-slate-500 hover:text-slate-900 inline-flex items-center justify-center transition-colors"
                    >
                        <XIcon className="w-3.5 h-3.5" />
                    </button>
                </div>
                {children}
            </motion.div>
        </div>
    );
}

export function DrawerFooter({
    onClose,
    cancelLabel = "Cancel",
    children,
}: {
    onClose: () => void;
    cancelLabel?: string;
    children: React.ReactNode;
}) {
    return (
        <div className="mt-auto border-t border-slate-200 px-5 py-3 flex items-center justify-end gap-2 shrink-0">
            <button
                type="button"
                onClick={onClose}
                className="h-7 px-3 rounded-md border border-slate-200 text-[12px] text-slate-700 hover:border-slate-300 hover:text-slate-900 transition-colors"
            >
                {cancelLabel}
            </button>
            {children}
        </div>
    );
}

export function SectionLabel({ children }: { children: React.ReactNode }) {
    return (
        <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">{children}</div>
    );
}

export const primaryBtn =
    "h-7 px-3 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors";

function errMessage(err: unknown): string | undefined {
    const e = err as { response?: { data?: { message?: string; error?: string } }; message?: string };
    return e.response?.data?.message ?? e.response?.data?.error ?? e.message;
}
