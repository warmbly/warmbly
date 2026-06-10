// Connection-management drawer — this is HOW a user actually uses an
// integration after connecting. It surfaces lifecycle + health, the connected
// account, granted scopes, recent activity (sync runs), and the automation
// rules wired to this connection:
//
//   "When a prospect replies (positive, ≥60% confidence) → notify #sales
//    with: 🔥 {{contact_email}} is interested — {{subject}}"
//
// Each rule is fully customizable: trigger event, filters (reply intent + min
// confidence), destination (Slack channel / Sheet ID / webhook), and a custom
// message template with {{placeholder}} substitution. Reauthorize / disconnect
// live here too.

"use client";

import React from "react";
import {
    AlertTriangleIcon,
    CheckCircle2Icon,
    CopyIcon,
    EyeIcon,
    Loader2Icon,
    RefreshCwIcon,
    SendIcon,
    UnplugIcon,
} from "lucide-react";
import toast from "react-hot-toast";

import { TextInput } from "@/components/ui/field";
import { useConfirm } from "@/hooks/context/confirm";
import useConnectionDetail from "@/lib/api/hooks/app/integrations/useConnectionDetail";
import useDisconnectIntegration from "@/lib/api/hooks/app/integrations/useDisconnectIntegration";
import {
    useFinishIntegrationOAuth,
    useReauthIntegration,
} from "@/lib/api/hooks/app/integrations/useIntegrationOAuth";
import { openOAuthPopup } from "@/lib/integrations/oauthPopup";
import {
    type CapabilityObject,
    type IntegrationCatalogEntry,
    type IntegrationConnection,
} from "@/lib/api/models/app/integrations/Integration";
import { useFieldMappings, useUpdateConnectionConfig } from "@/lib/api/hooks/app/integrations/useFieldMappings";
import { useRevealWebhookSecret, useTestConnection } from "@/lib/api/hooks/app/integrations/useConnectionWebhookTools";
import { cn } from "@/lib/utils";

import { Drawer, SectionLabel } from "./ConnectDrawer";
import FieldMapEditor from "./FieldMapEditor";
import StatusPill, { HealthDot } from "./StatusPill";

// Providers whose deliveries we can test (notify + generic webhook). Automation
// tools additionally expose an HMAC signing secret for verification.
const WEBHOOK_TOOL_PROVIDERS = ["slack", "discord", "zapier", "make", "n8n"];
const SIGNING_PROVIDERS = ["zapier", "make", "n8n"];

export default function ConnectionDetail({
    connection,
    entry,
    onClose,
}: {
    connection: IntegrationConnection;
    entry?: IntegrationCatalogEntry;
    onClose: () => void;
}) {
    const detail = useConnectionDetail(connection.id);
    const disconnect = useDisconnectIntegration();
    const reauth = useReauthIntegration();
    const finishOAuth = useFinishIntegrationOAuth();

    const [busy, setBusy] = React.useState(false);
    const confirm = useConfirm();

    const conn = detail.data?.connection ?? connection;
    const events = detail.data?.events ?? [];
    const runs = detail.data?.runs ?? [];

    const capability = entry?.capability;
    const crmObject = capability?.objects?.[0];
    const isOAuth = conn.auth_method === "oauth";
    const needsReauth = conn.status === "reauth_required";

    async function handleReauth() {
        setBusy(true);
        try {
            const { url } = await reauth.mutateAsync(conn.id);
            const { code, state } = await openOAuthPopup(url);
            await finishOAuth.mutateAsync({ code, state });
            toast.success("Reconnected");
            detail.refetch();
        } catch (err: unknown) {
            toast.error(msg(err) ?? "Reconnect failed");
        } finally {
            setBusy(false);
        }
    }

    function handleDisconnect() {
        confirm.show(`Disconnect ${conn.label}? Automations using it will stop.`, async () => {
            try {
                await disconnect.mutateAsync(conn.id);
                toast.success("Disconnected");
                onClose();
            } catch {
                toast.error("Disconnect failed");
            }
        });
    }


    return (
        <Drawer title="Manage" name={conn.label} provider={conn.provider} onClose={onClose}>
            <div className="flex-1 overflow-auto">
                {/* Status header */}
                <div className="px-5 py-4 border-b border-slate-200 space-y-3">
                    <div className="flex items-center justify-between gap-2">
                        <StatusPill status={conn.status} />
                        <div className="flex items-center gap-1.5 text-[11px] text-slate-500">
                            <HealthDot health={conn.health} />
                            {conn.health}
                        </div>
                    </div>
                    {conn.external_account_name && <Row label="Account" value={conn.external_account_name} />}
                    <Row label="Auth" value={conn.auth_method.replace("_", " ")} mono />
                    <Row
                        label="Last sync"
                        value={conn.last_synced_at ? new Date(conn.last_synced_at).toLocaleString() : "never"}
                    />
                    {conn.last_error && (
                        <div className="rounded-md border border-rose-200 bg-rose-50 px-2.5 py-2 flex items-start gap-2">
                            <AlertTriangleIcon className="w-3.5 h-3.5 text-rose-500 mt-0.5 shrink-0" />
                            <p className="text-[11px] text-rose-700 leading-relaxed break-words">{conn.last_error}</p>
                        </div>
                    )}
                    {needsReauth && (
                        <button
                            type="button"
                            onClick={handleReauth}
                            disabled={busy}
                            className="w-full h-8 rounded-md bg-amber-500 hover:bg-amber-600 text-white text-[12px] font-medium inline-flex items-center justify-center gap-1.5 transition-colors"
                        >
                            {busy ? <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> : <RefreshCwIcon className="w-3.5 h-3.5" />}
                            Reconnect to fix
                        </button>
                    )}
                </div>

                {/* Granted access */}
                {conn.granted_scopes && conn.granted_scopes.length > 0 && (
                    <div className="px-5 py-4 border-b border-slate-200 space-y-2">
                        <SectionLabel>Granted access</SectionLabel>
                        <div className="flex flex-wrap gap-1">
                            {conn.granted_scopes.map((s) => (
                                <span
                                    key={s}
                                    className="px-1.5 h-5 inline-flex items-center rounded bg-slate-100 text-[10px] font-mono text-slate-600"
                                >
                                    {s}
                                </span>
                            ))}
                        </div>
                    </div>
                )}

                {/* Field mapping — control exactly what each CRM record gets */}
                {crmObject && (
                    <div className="px-5 py-4 border-b border-slate-200 space-y-2.5">
                        <SectionLabel>Field mapping</SectionLabel>
                        <FieldMappingsBlock connectionId={conn.id} object={crmObject} />
                    </div>
                )}

                {/* Booking link — for scheduling providers (Calendly / Cal.com) */}
                {capability?.supports_booking_link && (
                    <div className="px-5 py-4 border-b border-slate-200 space-y-2">
                        <SectionLabel>Booking link</SectionLabel>
                        <BookingLinkBlock connection={conn} onSaved={() => detail.refetch()} />
                    </div>
                )}

                {/* Webhook delivery — test wiring + (automation tools) signature */}
                {WEBHOOK_TOOL_PROVIDERS.includes(conn.provider) && (
                    <div className="px-5 py-4 border-b border-slate-200 space-y-3">
                        <SectionLabel>Webhook delivery</SectionLabel>
                        <WebhookToolsBlock
                            connectionId={conn.id}
                            provider={conn.provider}
                            hasAutomations={events.length > 0}
                        />
                    </div>
                )}

                {/* Activity */}
                <div className="px-5 py-4 space-y-2">
                    <SectionLabel>Recent activity</SectionLabel>
                    {runs.length === 0 ? (
                        <p className="text-[11.5px] text-slate-400">Nothing yet.</p>
                    ) : (
                        <div className="space-y-1">
                            {runs.map((r) => (
                                <div key={r.id} className="flex items-center gap-2 text-[11px]">
                                    {r.status === "success" ? (
                                        <CheckCircle2Icon className="w-3 h-3 text-emerald-500 shrink-0" />
                                    ) : r.status === "error" ? (
                                        <AlertTriangleIcon className="w-3 h-3 text-rose-500 shrink-0" />
                                    ) : (
                                        <Loader2Icon className="w-3 h-3 text-slate-400 animate-spin shrink-0" />
                                    )}
                                    <span className="text-slate-600 truncate flex-1">
                                        {r.kind}
                                        {r.detail ? ` · ${r.detail}` : ""}
                                    </span>
                                    <span className="text-slate-400 tabular-nums shrink-0">
                                        {new Date(r.started_at).toLocaleTimeString()}
                                    </span>
                                </div>
                            ))}
                        </div>
                    )}
                </div>
            </div>

            <div className="mt-auto border-t border-slate-200 px-5 py-3 flex items-center justify-between shrink-0">
                <button
                    type="button"
                    onClick={handleDisconnect}
                    className="h-7 px-3 rounded-md text-[12px] text-rose-600 hover:bg-rose-50 inline-flex items-center gap-1.5 transition-colors"
                >
                    <UnplugIcon className="w-3.5 h-3.5" />
                    Disconnect
                </button>
                {isOAuth && !needsReauth && (
                    <button
                        type="button"
                        onClick={handleReauth}
                        disabled={busy}
                        className="h-7 px-3 rounded-md border border-slate-200 text-[12px] text-slate-700 hover:border-slate-300 inline-flex items-center gap-1.5 transition-colors"
                    >
                        {busy ? <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> : <RefreshCwIcon className="w-3.5 h-3.5" />}
                        Reauthorize
                    </button>
                )}
            </div>
        </Drawer>
    );
}

// FieldMappingsBlock loads the connection's field maps and renders the editor.
function FieldMappingsBlock({ connectionId, object }: { connectionId: string; object: CapabilityObject }) {
    const mappings = useFieldMappings(connectionId);
    if (mappings.isPending) {
        return <p className="text-[11.5px] text-slate-400 inline-flex items-center gap-1.5"><Loader2Icon className="w-3 h-3 animate-spin" /> Loading…</p>;
    }
    return (
        <FieldMapEditor
            connectionId={connectionId}
            object={object}
            mappings={mappings.data?.mappings ?? []}
        />
    );
}

// BookingLinkBlock lets the user set the public scheduling URL surfaced by the
// contextual "Book a call" buttons across the dashboard.
function BookingLinkBlock({ connection, onSaved }: { connection: IntegrationConnection; onSaved: () => void }) {
    const update = useUpdateConnectionConfig();
    const stored =
        (connection.config_capabilities?.scheduling_url as string) ||
        ((connection.display_fields?.scheduling_url as string) ?? "");
    const [url, setUrl] = React.useState(stored);
    React.useEffect(() => setUrl(stored), [stored]);
    const dirty = url.trim() !== stored.trim();

    async function save() {
        const v = url.trim();
        if (v && !/^https?:\/\//i.test(v)) {
            toast.error("Enter a full https:// booking link");
            return;
        }
        await toast.promise(
            update.mutateAsync({
                connectionId: connection.id,
                config_capabilities: { ...(connection.config_capabilities ?? {}), scheduling_url: v },
            }),
            { loading: "Saving…", success: "Booking link saved", error: "Could not save" },
        );
        onSaved();
    }

    return (
        <div className="space-y-1.5">
            <p className="text-[11.5px] text-slate-500 leading-relaxed">
                Paste your public scheduling link. A “Book a call” button appears on contacts and inbox
                threads, prefilled with the contact’s email.
            </p>
            <TextInput value={url} onChange={setUrl} placeholder="https://calendly.com/you/intro" className="font-mono" />
            {dirty && (
                <div className="flex justify-end">
                    <button
                        type="button"
                        onClick={save}
                        disabled={update.isPending}
                        className={cn(
                            "h-6 px-2.5 rounded text-[11.5px] font-medium text-white bg-sky-600 hover:bg-sky-700 inline-flex items-center gap-1.5 transition-colors",
                            update.isPending && "opacity-60",
                        )}
                    >
                        {update.isPending && <Loader2Icon className="w-3 h-3 animate-spin" />}
                        Save link
                    </button>
                </div>
            )}
        </div>
    );
}

function Row({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
    return (
        <div className="flex items-center justify-between gap-3">
            <span className="text-[10.5px] uppercase tracking-[0.1em] text-slate-400 shrink-0">{label}</span>
            <span className={cn("text-[12px] text-slate-700 truncate min-w-0", mono && "font-mono")}>{value}</span>
        </div>
    );
}

function msg(err: unknown): string | undefined {
    const e = err as { response?: { data?: { message?: string; error?: string } }; message?: string };
    return e.response?.data?.message ?? e.response?.data?.error ?? e.message;
}

// WebhookToolsBlock — "send a test event" for any notify/webhook provider, plus
// (for Zapier/Make/n8n) the HMAC signing secret used to verify our deliveries.
function WebhookToolsBlock({
    connectionId,
    provider,
    hasAutomations,
}: {
    connectionId: string;
    provider: string;
    hasAutomations: boolean;
}) {
    const test = useTestConnection();
    const reveal = useRevealWebhookSecret();
    const [secret, setSecret] = React.useState<string | null>(null);

    const runTest = () =>
        test.mutate(connectionId, {
            onSuccess: (r) => toast.success(`Sent ${r.sent} test event${r.sent === 1 ? "" : "s"}`),
            onError: (e) => toast.error(msg(e) ?? "Test failed"),
        });

    const showSecret = () =>
        reveal.mutate(connectionId, {
            onSuccess: (r) => setSecret(r.signing_secret),
            onError: (e) => toast.error(msg(e) ?? "Could not load secret"),
        });

    const copy = () => {
        if (secret) void navigator.clipboard.writeText(secret).then(() => toast.success("Copied"));
    };

    return (
        <div className="space-y-2.5">
            <p className="text-[11.5px] text-slate-400 leading-relaxed">
                {hasAutomations
                    ? "Send a sample event to confirm your automation is wired correctly."
                    : "Add an automation above first, then send a test event to confirm it's wired."}
            </p>
            <button
                type="button"
                onClick={runTest}
                disabled={!hasAutomations || test.isPending}
                className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-slate-700 hover:text-slate-900 text-[12px] inline-flex items-center gap-1.5 transition-colors disabled:opacity-60 disabled:cursor-not-allowed"
            >
                {test.isPending ? (
                    <Loader2Icon className="w-3.5 h-3.5 animate-spin" />
                ) : (
                    <SendIcon className="w-3.5 h-3.5" />
                )}
                Send test event
            </button>

            {SIGNING_PROVIDERS.includes(provider) && (
                <div className="pt-1.5 space-y-1.5">
                    <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                        Signing secret
                    </div>
                    <p className="text-[11px] text-slate-400 leading-relaxed">
                        Every delivery is signed with{" "}
                        <span className="font-mono">X-Warmbly-Signature: t=&lt;unix&gt;,v1=&lt;hmac&gt;</span> (HMAC-SHA256
                        of <span className="font-mono">{"{t}.{body}"}</span>). Use this secret to verify it.
                    </p>
                    {secret ? (
                        <div className="flex items-center gap-1.5">
                            <code className="flex-1 min-w-0 truncate rounded-md border border-slate-200 bg-slate-50 px-2 h-7 inline-flex items-center text-[11px] font-mono text-slate-700">
                                {secret}
                            </code>
                            <button
                                type="button"
                                onClick={copy}
                                title="Copy"
                                className="h-7 w-7 rounded-md border border-slate-200 hover:border-slate-300 text-slate-500 hover:text-slate-900 inline-flex items-center justify-center"
                            >
                                <CopyIcon className="w-3.5 h-3.5" />
                            </button>
                        </div>
                    ) : (
                        <button
                            type="button"
                            onClick={showSecret}
                            disabled={reveal.isPending}
                            className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-slate-700 hover:text-slate-900 text-[12px] inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                        >
                            {reveal.isPending ? (
                                <Loader2Icon className="w-3.5 h-3.5 animate-spin" />
                            ) : (
                                <EyeIcon className="w-3.5 h-3.5" />
                            )}
                            Reveal signing secret
                        </button>
                    )}
                </div>
            )}
        </div>
    );
}
