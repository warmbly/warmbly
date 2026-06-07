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
    Loader2Icon,
    PlusIcon,
    RefreshCwIcon,
    Trash2Icon,
    UnplugIcon,
    ZapIcon,
} from "lucide-react";
import toast from "react-hot-toast";

import { Label, TextInput } from "@/components/ui/field";
import { SelectMenu, type SelectOption } from "@/components/ui/select-menu";
import { useConfirm } from "@/hooks/context/confirm";
import useConnectionDetail from "@/lib/api/hooks/app/integrations/useConnectionDetail";
import useDisconnectIntegration from "@/lib/api/hooks/app/integrations/useDisconnectIntegration";
import {
    useFinishIntegrationOAuth,
    useReauthIntegration,
} from "@/lib/api/hooks/app/integrations/useIntegrationOAuth";
import {
    useCreateConnectionEvent,
    useDeleteConnectionEvent,
} from "@/lib/api/hooks/app/integrations/useConnectionEvents";
import { openOAuthPopup } from "@/lib/integrations/oauthPopup";
import {
    EVENT_LABELS,
    REPLY_INTENT_OPTIONS,
    type IntegrationCatalogEntry,
    type IntegrationConnection,
    type IntegrationEventSubscription,
} from "@/lib/api/models/app/integrations/Integration";
import { cn } from "@/lib/utils";

import { Drawer, SectionLabel } from "./ConnectDrawer";
import StatusPill, { HealthDot } from "./StatusPill";

const REPLY_EVENT = "campaign.reply_received";

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
    const createEvent = useCreateConnectionEvent();
    const deleteEvent = useDeleteConnectionEvent();

    const [adding, setAdding] = React.useState(false);
    const [busy, setBusy] = React.useState(false);
    const confirm = useConfirm();

    const conn = detail.data?.connection ?? connection;
    const events = detail.data?.events ?? [];
    const runs = detail.data?.runs ?? [];

    const availableEvents = entry?.events ?? Object.keys(EVENT_LABELS);
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

    async function addAutomation(eventType: string, config: Record<string, unknown>) {
        try {
            await createEvent.mutateAsync({
                connectionId: conn.id,
                event_type: eventType,
                action: actionForProvider(conn.provider),
                config,
                enabled: true,
            });
            toast.success("Automation added");
            setAdding(false);
            detail.refetch();
        } catch (err: unknown) {
            toast.error(msg(err) ?? "Could not add automation");
        }
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

                {/* Automations — the core "how you use it" surface */}
                <div className="px-5 py-4 border-b border-slate-200 space-y-3">
                    <div className="flex items-center justify-between">
                        <SectionLabel>Automations</SectionLabel>
                        {!adding && (
                            <button
                                type="button"
                                onClick={() => setAdding(true)}
                                className="h-6 px-2 rounded text-[11px] text-sky-700 hover:bg-sky-50 inline-flex items-center gap-1 transition-colors"
                            >
                                <PlusIcon className="w-3 h-3" />
                                New rule
                            </button>
                        )}
                    </div>

                    {events.length === 0 && !adding && (
                        <p className="text-[11.5px] text-slate-400 leading-relaxed">
                            No automations yet. Add a rule to push Warmbly events into {entry?.name ?? conn.label} —
                            e.g. ping a channel when a prospect replies.
                        </p>
                    )}

                    {events.map((ev) => (
                        <AutomationRow
                            key={ev.id}
                            sub={ev}
                            onDelete={() =>
                                deleteEvent
                                    .mutateAsync({ connectionId: conn.id, eventId: ev.id })
                                    .then(() => detail.refetch())
                            }
                        />
                    ))}

                    {adding && (
                        <AddAutomation
                            provider={conn.provider}
                            availableEvents={availableEvents}
                            onCancel={() => setAdding(false)}
                            onAdd={addAutomation}
                            busy={createEvent.isPending}
                        />
                    )}
                </div>

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

// AutomationRow renders one configured rule with a human summary of its
// trigger, filters, destination, and custom message.
function AutomationRow({ sub, onDelete }: { sub: IntegrationEventSubscription; onDelete: () => void }) {
    const cfg = (sub.config ?? {}) as Record<string, unknown>;
    const intents = Array.isArray(cfg.intents) ? (cfg.intents as string[]) : [];
    const minConf = typeof cfg.min_confidence === "number" ? cfg.min_confidence : undefined;
    const dest =
        (cfg.channel as string) || (cfg.url as string) || (cfg.webhook_url as string) || "";
    const tmpl = (cfg.message_template as string) || "";

    const filters: string[] = [];
    if (intents.length) filters.push(intents.join(", "));
    if (minConf) filters.push(`≥${Math.round(minConf * 100)}%`);

    return (
        <div className="rounded-md border border-slate-200 px-2.5 py-2">
            <div className="flex items-center gap-2">
                <ZapIcon className="w-3.5 h-3.5 text-sky-500 shrink-0" />
                <div className="min-w-0 flex-1">
                    <div className="text-[12px] text-slate-800 truncate">
                        {EVENT_LABELS[sub.event_type] ?? sub.event_type}
                    </div>
                    <div className="text-[10px] text-slate-400 font-mono truncate">
                        {dest || sub.action}
                        {filters.length > 0 && ` · ${filters.join(" · ")}`}
                    </div>
                </div>
                <button
                    type="button"
                    onClick={onDelete}
                    aria-label="Remove automation"
                    className="h-6 w-6 rounded text-slate-400 hover:text-rose-600 hover:bg-rose-50 inline-flex items-center justify-center transition-colors"
                >
                    <Trash2Icon className="w-3.5 h-3.5" />
                </button>
            </div>
            {tmpl && (
                <p className="mt-1.5 pl-5 text-[10.5px] text-slate-500 italic truncate">“{tmpl}”</p>
            )}
        </div>
    );
}

// AddAutomation is the rule builder. Everything a user needs to customize a
// behaviour lives here: trigger, reply-intent + confidence filters, the
// destination, and a custom message template.
function AddAutomation({
    provider,
    availableEvents,
    onAdd,
    onCancel,
    busy,
}: {
    provider: string;
    availableEvents: string[];
    onAdd: (eventType: string, config: Record<string, unknown>) => void;
    onCancel: () => void;
    busy: boolean;
}) {
    const [eventType, setEventType] = React.useState(
        availableEvents.includes(REPLY_EVENT) ? REPLY_EVENT : availableEvents[0] ?? REPLY_EVENT,
    );
    const eventOptions = React.useMemo<SelectOption[]>(
        () => availableEvents.map((ev) => ({ value: ev, label: EVENT_LABELS[ev] ?? ev })),
        [availableEvents],
    );
    const [dest, setDest] = React.useState("");
    const [intents, setIntents] = React.useState<string[]>([]);
    const [minConf, setMinConf] = React.useState(0);
    const [template, setTemplate] = React.useState("");

    const needsChannel = provider === "slack";
    const needsURL = provider !== "slack" && provider !== "discord" &&
        provider !== "hubspot" && provider !== "pipedrive";
    const isReplyTrigger = eventType === REPLY_EVENT;
    const destRequired = needsChannel || needsURL;

    function toggleIntent(v: string) {
        setIntents((cur) => (cur.includes(v) ? cur.filter((x) => x !== v) : [...cur, v]));
    }

    function buildConfig(): Record<string, unknown> {
        const cfg: Record<string, unknown> = {};
        if (needsChannel && dest.trim()) cfg.channel = dest.trim();
        if (needsURL && dest.trim()) cfg.url = dest.trim();
        if (isReplyTrigger && intents.length) cfg.intents = intents;
        if (isReplyTrigger && minConf > 0) cfg.min_confidence = minConf;
        if (template.trim()) cfg.message_template = template.trim();
        return cfg;
    }

    const destLabel = needsChannel ? "Channel" : "Destination URL";
    const destPlaceholder = needsChannel ? "#sales" : "https://…";
    const canSubmit = !busy && !(destRequired && !dest.trim());

    return (
        <div className="rounded-md border border-sky-200 bg-sky-50/40 p-3 space-y-3">
            <div>
                <Label>When this happens</Label>
                <SelectMenu
                    value={eventType}
                    onChange={setEventType}
                    options={eventOptions}
                    className="w-full"
                    aria-label="When this happens"
                />
            </div>

            {/* Reply-only filters */}
            {isReplyTrigger && (
                <div className="space-y-2">
                    <Label>Only for these reply types (optional)</Label>
                    <div className="flex flex-wrap gap-1">
                        {REPLY_INTENT_OPTIONS.map((opt) => {
                            const on = intents.includes(opt.value);
                            return (
                                <button
                                    key={opt.value}
                                    type="button"
                                    onClick={() => toggleIntent(opt.value)}
                                    className={cn(
                                        "h-6 px-2 rounded-full text-[10.5px] border transition-colors",
                                        on
                                            ? "bg-sky-600 border-sky-600 text-white"
                                            : "bg-white border-slate-200 text-slate-600 hover:border-slate-300",
                                    )}
                                >
                                    {opt.label}
                                </button>
                            );
                        })}
                    </div>
                    <div>
                        <Label>Minimum confidence: {Math.round(minConf * 100)}%</Label>
                        <input
                            type="range"
                            min={0}
                            max={100}
                            step={5}
                            value={Math.round(minConf * 100)}
                            onChange={(e) => setMinConf(Number(e.target.value) / 100)}
                            className="w-full accent-sky-600"
                        />
                    </div>
                </div>
            )}

            {(needsChannel || needsURL) && (
                <div>
                    <Label>{destLabel}</Label>
                    <TextInput
                        value={dest}
                        onChange={setDest}
                        placeholder={destPlaceholder}
                        className={needsURL ? "font-mono" : undefined}
                    />
                </div>
            )}

            {/* Custom message (for notification-style actions) */}
            {(needsChannel || provider === "discord" || needsURL) && (
                <div>
                    <Label>Custom message (optional)</Label>
                    <textarea
                        value={template}
                        onChange={(e) => setTemplate(e.target.value)}
                        rows={2}
                        placeholder="🔥 {{contact_email}} replied — {{subject}}"
                        className="w-full px-2 py-1.5 rounded-md border border-slate-200 bg-white text-[12px] text-slate-900 outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100 resize-none"
                    />
                    <p className="text-[10px] text-slate-400 mt-1">
                        Use {"{{contact_email}}"}, {"{{subject}}"}, {"{{intent}}"}, {"{{campaign_id}}"}, {"{{reason}}"}.
                    </p>
                </div>
            )}

            <div className="flex items-center justify-end gap-2 pt-0.5">
                <button
                    type="button"
                    onClick={onCancel}
                    className="h-6 px-2.5 rounded text-[11.5px] text-slate-600 hover:text-slate-900"
                >
                    Cancel
                </button>
                <button
                    type="button"
                    disabled={!canSubmit}
                    onClick={() => onAdd(eventType, buildConfig())}
                    className={cn(
                        "h-6 px-2.5 rounded text-[11.5px] font-medium text-white bg-sky-600 hover:bg-sky-700 transition-colors",
                        !canSubmit && "opacity-60",
                    )}
                >
                    {busy ? "Adding…" : "Add automation"}
                </button>
            </div>
        </div>
    );
}

function Row({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
    return (
        <div className="flex items-center justify-between gap-3">
            <span className="text-[10.5px] uppercase tracking-[0.1em] text-slate-400">{label}</span>
            <span className={cn("text-[12px] text-slate-700 truncate", mono && "font-mono")}>{value}</span>
        </div>
    );
}

// actionForProvider maps a connection's provider to the action its automations
// perform. Mirrors defaultActionForProvider in the model module.
function actionForProvider(provider: string): string {
    switch (provider) {
        case "slack":
            return "slack.notify";
        case "discord":
            return "discord.notify";
        case "hubspot":
            return "hubspot.upsert_contact";
        case "pipedrive":
            return "pipedrive.upsert_person";
        default:
            return "webhook.ping";
    }
}

function msg(err: unknown): string | undefined {
    const e = err as { response?: { data?: { message?: string; error?: string } }; message?: string };
    return e.response?.data?.message ?? e.response?.data?.error ?? e.message;
}
