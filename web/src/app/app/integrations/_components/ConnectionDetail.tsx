// Connection-management popup: how a user works with an integration after
// connecting. It shows status and health, field mapping, the booking link,
// webhook delivery testing and recent activity, with reconnect and disconnect
// in the footer. Automations themselves are built in the flow builder.

"use client";

import React from "react";
import useBrowseState from "@/hooks/useBrowseState";
import { slackTabSchema } from "@/lib/browse-other-lists";
import { Link, useLocation } from "@tanstack/react-router";
import {
    AlertTriangleIcon,
    ArrowRightIcon,
    CheckCircle2Icon,
    ChevronRightIcon,
    CopyIcon,
    EyeIcon,
    Loader2Icon,
    RefreshCwIcon,
    SendIcon,
    Settings2Icon,
    UnplugIcon,
} from "lucide-react";
import toast from "react-hot-toast/headless";

import { TextInput } from "@/components/ui/field";
import { CRM_INFO, CrmMark, type ExternalCrm } from "@/components/app/crm/crmProviders";
import { useConfirm } from "@/hooks/context/confirm";
import { usePresenceResource } from "@/hooks/PresenceProvider";
import ResourceViewers from "@/components/app/presence/ResourceViewers";
import useConnectionDetail from "@/lib/api/hooks/app/integrations/useConnectionDetail";
import useDisconnectIntegration from "@/lib/api/hooks/app/integrations/useDisconnectIntegration";
import {
    useFinishIntegrationOAuth,
    useReauthIntegration,
} from "@/lib/api/hooks/app/integrations/useIntegrationOAuth";
import { authorizeInPopup } from "@/lib/integrations/oauthPopup";
import {
    type CapabilityObject,
    type IntegrationCatalogEntry,
    type IntegrationConnection,
} from "@/lib/api/models/app/integrations/Integration";
import { useFieldMappings, useUpdateConnectionConfig } from "@/lib/api/hooks/app/integrations/useFieldMappings";
import {
    useRevealWebhookSecret,
    useRotateInboundUrl,
    useSetConnectionSigningKey,
    useTestConnection,
} from "@/lib/api/hooks/app/integrations/useConnectionWebhookTools";
import { useAutomations } from "@/lib/api/hooks/app/automations/useAutomations";
import type { IntegrationAction } from "@/lib/api/models/app/integrations/Integration";
import { cn } from "@/lib/utils";

import { IntegrationDialog, SectionTitle } from "./IntegrationDialog";
import FieldMapEditor from "./FieldMapEditor";
import InboundUrlDialog from "./InboundUrlDialog";
import { SlackStatusBanner, SlackTabBar, SlackTabContent, type SlackTab } from "./SlackPanel";
import StatusPill, { HealthDot } from "./StatusPill";

// Providers whose deliveries we can test (notify + generic webhook). Automation
// tools additionally expose an HMAC signing secret for verification.
const WEBHOOK_TOOL_PROVIDERS = ["slack", "discord", "zapier", "make", "n8n"];
const SIGNING_PROVIDERS = ["zapier", "make", "n8n"];

// Action nodes that "Send test event" can actually fire (notify + generic
// webhook). Native + CRM-upsert actions are deliberately excluded.
const NOTIFY_WEBHOOK_ACTIONS: IntegrationAction[] = ["slack.notify", "discord.notify", "webhook.ping"];

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
    const [slackTab, setSlackTab] = useBrowseState<SlackTab>(`integrations.${connection.id}.slack.tab`, "overview", slackTabSchema);

    const conn = detail.data?.connection ?? connection;
    const runs = detail.data?.runs ?? [];

    usePresenceResource(conn.id ? `integration_connection:${conn.id}` : null, "editing");

    // Only webhook-tool connections expose the "Send test event" control, so only
    // those need the automations list (avoid an extra request per drawer open).
    const isWebhookTool = WEBHOOK_TOOL_PROVIDERS.includes(conn.provider);
    const automations = useAutomations({ enabled: isWebhookTool });
    const hasAutomations = (automations.data?.automations ?? []).some(
        (a) =>
            a.enabled &&
            a.graph?.nodes?.some(
                (n) =>
                    n.type === "action" &&
                    n.connection_id === conn.id &&
                    !!n.action &&
                    NOTIFY_WEBHOOK_ACTIONS.includes(n.action),
            ),
    );

    const capability = entry?.capability;
    const crmObject = capability?.objects?.[0];
    const isOAuth = conn.auth_method === "oauth";
    const needsReauth = conn.status === "reauth_required";
    const isSlack = conn.provider === "slack";
    // HubSpot and Pipedrive have their own home: CRM mode, mapping, rules.
    const crmHome = conn.provider === "hubspot" || conn.provider === "pipedrive" ? CRM_INFO[conn.provider as ExternalCrm] : null;
    const onCrmHome = useLocation({ select: (l) => !!crmHome && l.pathname.startsWith(crmHome.settingsPath) });

    async function handleReauth() {
        setBusy(true);
        try {
            const { code, state } = await authorizeInPopup(async () => (await reauth.mutateAsync(conn.id)).url);
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


    const account = conn.external_account_name?.trim();
    const synced = conn.last_synced_at ? `Last synced ${timeAgo(conn.last_synced_at)}` : "Not synced yet";

    return (
        <IntegrationDialog
            name={entry?.name ?? conn.label}
            provider={conn.provider}
            subtitle={
                <span className="inline-flex items-center gap-1.5 min-w-0">
                    <HealthDot health={conn.health} />
                    <span className="truncate">{[account || conn.label, synced].filter(Boolean).join(" · ")}</span>
                </span>
            }
            badge={<StatusPill status={conn.status} />}
            onClose={onClose}
            headerExtra={
                <ResourceViewers
                    resource={conn.id ? `integration_connection:${conn.id}` : null}
                    className="shrink-0 mt-1"
                />
            }
            tabs={isSlack ? <SlackTabBar tab={slackTab} onTab={setSlackTab} /> : undefined}
            footer={
                <>
                    <button
                        type="button"
                        onClick={handleDisconnect}
                        className="h-7 px-2.5 rounded-md text-[12px] text-rose-600 hover:bg-rose-50 inline-flex items-center gap-1.5 transition-colors"
                    >
                        <UnplugIcon className="w-3.5 h-3.5" />
                        Disconnect
                    </button>
                    <div className="ml-auto flex items-center gap-1.5">
                        {isOAuth && !needsReauth && (
                            <button
                                type="button"
                                onClick={handleReauth}
                                disabled={busy}
                                className="h-7 px-2.5 rounded-md text-[12px] text-slate-600 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                            >
                                {busy ? <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> : <RefreshCwIcon className="w-3.5 h-3.5" />}
                                Reconnect
                            </button>
                        )}
                        <button
                            type="button"
                            onClick={onClose}
                            className="h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium transition-colors"
                        >
                            Done
                        </button>
                    </div>
                </>
            }
        >
            {isSlack && <SlackStatusBanner onReconnect={handleReauth} reconnecting={busy} />}
            {isSlack && slackTab !== "overview" ? (
                <SlackTabContent tab={slackTab} />
            ) : (
                <div className="divide-y divide-slate-100">
                    {(conn.last_error || needsReauth) && (
                        <div className="px-5 py-4 space-y-3">
                            {conn.last_error && (
                                <div className="rounded-md border border-rose-200 bg-rose-50 px-3 py-2.5 flex items-start gap-2">
                                    <AlertTriangleIcon className="w-3.5 h-3.5 text-rose-500 mt-0.5 shrink-0" />
                                    <p className="text-[12px] text-rose-700 leading-relaxed break-words">{conn.last_error}</p>
                                </div>
                            )}
                            {needsReauth && (
                                <button
                                    type="button"
                                    onClick={handleReauth}
                                    disabled={busy}
                                    className="h-8 px-3 rounded-md bg-amber-500 hover:bg-amber-600 text-white text-[12px] font-medium inline-flex items-center justify-center gap-1.5 transition-colors"
                                >
                                    {busy ? <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> : <RefreshCwIcon className="w-3.5 h-3.5" />}
                                    Reconnect {entry?.name ?? ""} to fix it
                                </button>
                            )}
                        </div>
                    )}

                    {/* Salesforce has its own page: sync rules, field map, imports, activity log. */}
                    {conn.provider === "salesforce" && (
                        <div className="px-5 py-4">
                            <HomeLink
                                to={`/app/integrations/salesforce/${conn.id}`}
                                onNavigate={onClose}
                                icon={<Settings2Icon className="w-4 h-4 text-sky-600" />}
                                title="Salesforce settings"
                                body="Sync rules, field mapping, imports from list views and campaigns, and the activity log."
                            />
                        </div>
                    )}

                    {/* HubSpot and Pipedrive: CRM mode, field mapping and rules live on their own page */}
                    {crmHome && !onCrmHome && (
                        <div className="px-5 py-4">
                            <HomeLink
                                to={crmHome.settingsPath}
                                onNavigate={onClose}
                                icon={<CrmMark provider={crmHome.id} className="w-4 h-4" />}
                                title={`${crmHome.name} settings`}
                                body={`Use ${crmHome.name} as your CRM, field mapping, activity logging, ${crmHome.words.owners}, rules and sync health.`}
                            />
                        </div>
                    )}

                    {crmObject && !crmHome && conn.provider !== "salesforce" && (
                        <div className="px-5 py-4 space-y-3">
                            <SectionTitle description={`What each ${crmObject.label?.toLowerCase() || "record"} gets when Warmbly sends it.`}>
                                Field mapping
                            </SectionTitle>
                            <FieldMappingsBlock connectionId={conn.id} object={crmObject} />
                        </div>
                    )}

                    {capability?.supports_booking_link && (
                        <div className="px-5 py-4 space-y-3">
                            <SectionTitle description="A Book a call button appears on contacts and inbox threads, prefilled with the contact's email.">
                                Booking link
                            </SectionTitle>
                            <BookingLinkBlock connection={conn} onSaved={() => detail.refetch()} />
                        </div>
                    )}

                    {(conn.provider === "calendly" || conn.provider === "cal_com") && (
                        <div className="px-5 py-4 space-y-3">
                            <SectionTitle description="Where the provider sends bookings, and how Warmbly knows they are real.">
                                Booking notifications
                            </SectionTitle>
                            <InboundRotateBlock connection={conn} />
                            <InboundSigningBlock connection={conn} />
                        </div>
                    )}

                    {isWebhookTool && (
                        <div className="px-5 py-4 space-y-3">
                            <SectionTitle
                                description={
                                    hasAutomations
                                        ? "Send a sample event to confirm your automation is wired correctly."
                                        : "Build an automation that uses this integration first, then send a test event to confirm it is wired."
                                }
                            >
                                Test delivery
                            </SectionTitle>
                            <WebhookToolsBlock
                                connectionId={conn.id}
                                provider={conn.provider}
                                hasAutomations={hasAutomations}
                            />
                        </div>
                    )}

                    <div className="px-5 py-4 space-y-2.5">
                        <SectionTitle>Recent activity</SectionTitle>
                        {runs.length === 0 ? (
                            <p className="text-[12px] text-slate-400">Nothing yet.</p>
                        ) : (
                            <ul className="rounded-md border border-slate-200 divide-y divide-slate-100">
                                {runs.map((r) => (
                                    <li key={r.id} className="px-3 py-2 flex items-center gap-2 text-[12px]">
                                        {r.status === "success" ? (
                                            <CheckCircle2Icon className="w-3.5 h-3.5 text-emerald-500 shrink-0" />
                                        ) : r.status === "error" ? (
                                            <AlertTriangleIcon className="w-3.5 h-3.5 text-rose-500 shrink-0" />
                                        ) : (
                                            <Loader2Icon className="w-3.5 h-3.5 text-slate-400 animate-spin shrink-0" />
                                        )}
                                        <span className="text-slate-700 truncate flex-1">{r.detail || humanizeKind(r.kind)}</span>
                                        <span className="text-[11px] text-slate-400 tabular-nums shrink-0">{timeAgo(r.started_at)}</span>
                                    </li>
                                ))}
                            </ul>
                        )}
                    </div>

                    {conn.granted_scopes && conn.granted_scopes.length > 0 && (
                        <details className="px-5 py-3 group">
                            <summary className="cursor-pointer list-none text-[12px] text-slate-500 hover:text-slate-900 inline-flex items-center gap-1.5 select-none">
                                <ChevronRightIcon className="w-3 h-3 transition-transform group-open:rotate-90" />
                                Permissions granted ({conn.granted_scopes.length})
                            </summary>
                            <div className="mt-2.5 flex flex-wrap gap-1">
                                {conn.granted_scopes.map((s) => (
                                    <span
                                        key={s}
                                        className="px-1.5 h-5 inline-flex items-center rounded bg-slate-100 text-[10.5px] font-mono text-slate-600"
                                    >
                                        {s}
                                    </span>
                                ))}
                            </div>
                        </details>
                    )}
                </div>
            )}
        </IntegrationDialog>
    );
}

// A link to a provider's own home page, for settings too rich for this popup.
function HomeLink({
    to,
    onNavigate,
    icon,
    title,
    body,
}: {
    to: string;
    onNavigate: () => void;
    icon: React.ReactNode;
    title: string;
    body: string;
}) {
    return (
        <Link
            to={to}
            onClick={onNavigate}
            className="flex items-center gap-3 rounded-md border border-slate-200 hover:border-slate-300 hover:bg-slate-50/60 px-3 py-2.5 transition-colors group"
        >
            <span className="shrink-0">{icon}</span>
            <span className="min-w-0 flex-1">
                <span className="block text-[12.5px] font-medium text-slate-900">{title}</span>
                <span className="block text-[11.5px] text-slate-500 leading-relaxed">{body}</span>
            </span>
            <ArrowRightIcon className="w-3.5 h-3.5 text-slate-400 group-hover:text-slate-700 shrink-0" />
        </Link>
    );
}

// "manual_push" reads as "Manual push" when a run has no detail of its own.
function humanizeKind(kind: string): string {
    const s = kind.replace(/[_-]+/g, " ").trim();
    return s ? s.charAt(0).toUpperCase() + s.slice(1) : "Sync";
}

function timeAgo(at: Date | string): string {
    const d = typeof at === "string" ? new Date(at) : at;
    const sec = Math.max(0, Math.round((Date.now() - d.getTime()) / 1000));
    if (sec < 45) return "just now";
    const m = Math.round(sec / 60);
    if (m < 60) return `${m}m ago`;
    const h = Math.round(m / 60);
    if (h < 24) return `${h}h ago`;
    return d.toLocaleDateString(undefined, { month: "short", day: "numeric" });
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

// InboundRotateBlock replaces a leaked inbound URL. The new one is shown once.
function InboundRotateBlock({ connection }: { connection: IntegrationConnection }) {
    const confirm = useConfirm();
    const rotate = useRotateInboundUrl();
    const [fresh, setFresh] = React.useState<string | null>(null);

    function run() {
        confirm.show(
            "Rotate the inbound URL? The current URL stops working immediately, so bookings are missed until you paste the new one into the provider.",
            async () => {
                try {
                    const r = await rotate.mutateAsync(connection.id);
                    setFresh(r.inbound_webhook_url);
                } catch (err: unknown) {
                    toast.error(msg(err) ?? "Could not rotate the URL");
                }
            },
        );
    }

    return (
        <div className="space-y-1.5">
            <p className="text-[11.5px] text-slate-500 leading-relaxed">
                The URL contains a secret. If it was exposed, rotate it and paste the new one into the provider.
            </p>
            <button
                type="button"
                onClick={run}
                disabled={rotate.isPending}
                className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-slate-700 hover:text-slate-900 text-[12px] inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
            >
                {rotate.isPending ? (
                    <Loader2Icon className="w-3.5 h-3.5 animate-spin" />
                ) : (
                    <RefreshCwIcon className="w-3.5 h-3.5" />
                )}
                Rotate URL
            </button>
            {fresh && (
                <InboundUrlDialog provider={connection.provider} url={fresh} onClose={() => setFresh(null)} />
            )}
        </div>
    );
}

const SIGNING_HINTS: Record<string, string> = {
    calendly:
        "Paste the signing key of your Calendly webhook subscription (the signing_key you set, or the one Calendly returned when it was created).",
    cal_com: "Paste the secret you set on the Cal.com webhook (Settings, Developer, Webhooks, Secret).",
};

// InboundSigningBlock sets the key Calendly / Cal.com deliveries must be signed
// with. Once set, a delivery without a valid signature is refused.
function InboundSigningBlock({ connection }: { connection: IntegrationConnection }) {
    const confirm = useConfirm();
    const setKey = useSetConnectionSigningKey();
    const [key, setKeyValue] = React.useState("");
    const signed = connection.display_fields?.inbound_signing === true;

    async function save() {
        const v = key.trim();
        if (v.length < 8) {
            toast.error("A signing key is at least 8 characters");
            return;
        }
        try {
            await setKey.mutateAsync({ connectionId: connection.id, signing_key: v });
            setKeyValue("");
            toast.success("Signing key saved. Unsigned deliveries are now refused.");
        } catch (err: unknown) {
            toast.error(msg(err) ?? "Could not save the signing key");
        }
    }

    function remove() {
        confirm.show(
            "Remove the signing key? Deliveries will be accepted on the inbound URL alone.",
            async () => {
                try {
                    await setKey.mutateAsync({ connectionId: connection.id, signing_key: "" });
                    toast.success("Signing key removed");
                } catch (err: unknown) {
                    toast.error(msg(err) ?? "Could not remove the signing key");
                }
            },
        );
    }

    return (
        <div className="space-y-1.5">
            <p className="text-[11.5px] text-slate-500 leading-relaxed">
                {signed
                    ? "Deliveries must carry a valid signature. Paste a new key to replace it."
                    : SIGNING_HINTS[connection.provider]}
            </p>
            <TextInput
                type="password"
                value={key}
                onChange={setKeyValue}
                placeholder={signed ? "Replace signing key" : "Signing key"}
                className="font-mono"
            />
            <div className="flex items-center justify-end gap-1.5">
                {signed && (
                    <button
                        type="button"
                        onClick={remove}
                        disabled={setKey.isPending}
                        className="h-6 px-2.5 rounded border border-slate-200 hover:border-slate-300 text-[11.5px] text-slate-600 hover:text-slate-900 transition-colors disabled:opacity-60"
                    >
                        Remove
                    </button>
                )}
                {key.trim() !== "" && (
                    <button
                        type="button"
                        onClick={save}
                        disabled={setKey.isPending}
                        className={cn(
                            "h-6 px-2.5 rounded text-[11.5px] font-medium text-white bg-sky-600 hover:bg-sky-700 inline-flex items-center gap-1.5 transition-colors",
                            setKey.isPending && "opacity-60",
                        )}
                    >
                        {setKey.isPending && <Loader2Icon className="w-3 h-3 animate-spin" />}
                        Save key
                    </button>
                )}
            </div>
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
                <div className="pt-3 mt-1 border-t border-slate-100 space-y-2">
                    <h4 className="text-[12.5px] font-semibold text-slate-900">Signing secret</h4>
                    <p className="text-[11.5px] text-slate-500 leading-relaxed">
                        Every delivery is signed with{" "}
                        <span className="font-mono">X-Warmbly-Signature: t=&lt;unix&gt;,v1=&lt;hmac&gt;</span> (HMAC-SHA256
                        of <span className="font-mono">{"{t}.{body}"}</span>). Use this secret to verify it.
                    </p>
                    {secret ? (
                        <div className="flex items-center gap-1.5">
                            <code
                                className="flex-1 min-w-0 truncate rounded-md border border-slate-200 bg-slate-50 px-2 h-7 inline-flex items-center text-[11px] font-mono text-slate-700"
                                data-ph-mask=""
                            >
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
