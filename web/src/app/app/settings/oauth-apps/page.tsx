// OAuth apps — a Settings section. Developers register third-party OAuth clients
// here (client id + one-time secret, redirect URIs, requested scopes) and review
// the apps the workspace's members have authorized. Every app is issued a client
// secret; PKCE is an optional extra layer the developer can add. The flow itself
// (consent + token exchange) lives on the standalone /oauth/authorize page + API.

import React from "react";
import useBrowseState from "@/hooks/useBrowseState";
import { browseSearchSchema, oauthAppsTabSchema, webhookStatusSchema } from "@/lib/browse-other-lists";
import { createPortal } from "react-dom";
import toast from "react-hot-toast/headless";
import { AnimatePresence, motion } from "framer-motion";
import {
    CheckIcon,
    ChevronRightIcon,
    Loader2Icon,
    PencilIcon,
    PlusIcon,
    RefreshCwIcon,
    Trash2Icon,
    WebhookIcon,
    XIcon,
} from "lucide-react";

import { NoAccess } from "@/components/layout/NoAccess";
import { usePermission } from "@/hooks/usePermission";
import { EmptyBlock } from "@/components/layout/Page";
import { Label, TextInput } from "@/components/ui/field";
import { cn } from "@/lib/utils";
import { appNameError } from "@/lib/displayName";
import { useConfirm } from "@/hooks/context/confirm";
import useAPIPermissions from "@/lib/api/hooks/app/api-keys/useAPIPermissions";
import type APIPermission from "@/lib/api/models/app/apikeys/APIPermission";
import {
    useOAuthApps,
    useUpdateOAuthApp,
    useDeleteOAuthApp,
    useRotateOAuthAppSecret,
    useSetOAuthAppLogo,
    useDeleteOAuthAppLogo,
    useOAuthAppWebhookSecret,
    useRotateOAuthAppWebhookSecret,
    useOAuthAppWebhookEndpoints,
    useOAuthAppWebhookDeliveries,
} from "@/lib/api/hooks/app/oauth/useOAuthApps";
import { useWebhookEventCatalog } from "@/lib/api/hooks/app/webhooks/useWebhooks";
import listOAuthAppWebhookDeliveries from "@/lib/api/client/app/oauth/listOAuthAppWebhookDeliveries";
import { useRevokeWorkspaceAuthorization, useWorkspaceAuthorizations } from "@/lib/api/hooks/app/oauth/useAuthorizedApps";
import type { OAuthApplication } from "@/lib/api/models/app/oauth/OAuthApp";
import type {
    WebhookDelivery,
    WebhookDeliveryStatus,
    WebhookEndpoint,
    WebhookEventDescriptor,
} from "@/lib/api/models/app/webhooks/Webhook";
import { SectionShell } from "../_components/SectionShell";
import { AvatarUploader } from "@/components/app/avatar/AvatarUploader";
import AppListingPanel from "./AppListingPanel";
import { AppLogo, CopyButton, EventPicker } from "./parts";
import RegisterAppDialog from "./RegisterAppDialog";

function formatRelative(date: Date | string | undefined): string {
    if (!date) return "never";
    const d = typeof date === "string" ? new Date(date) : date;
    if (Number.isNaN(d.getTime())) return "never";
    const diff = Date.now() - d.getTime();
    if (diff < 0) return d.toLocaleString();
    const mins = Math.floor(diff / 60_000);
    if (mins < 1) return "just now";
    if (mins < 60) return `${mins}m ago`;
    const hours = Math.floor(mins / 60);
    if (hours < 24) return `${hours}h ago`;
    const days = Math.floor(hours / 24);
    if (days < 7) return `${days}d ago`;
    return d.toLocaleDateString("en-US", { month: "short", day: "numeric" });
}

function hostOf(url: string): string {
    try {
        return new URL(url).host;
    } catch {
        return url;
    }
}

// ScopePicker is a checkbox grid over the API permissions, toggling bits in the
// scope bitmask. Reuses the same permission catalogue as API keys.
function ScopePicker({ value, onChange }: { value: number; onChange: (v: number) => void }) {
    const perms = useAPIPermissions();
    const grouped = React.useMemo(() => {
        const g: Record<string, APIPermission[]> = {};
        const appScopes = perms.data?.app_scopes ?? 0;
        for (const p of perms.data?.permissions ?? []) {
            if ((appScopes & p.value) !== p.value) continue;
            (g[p.category] ??= []).push(p);
        }
        return g;
    }, [perms.data]);
    const toggle = (bit: number) => onChange(value & bit ? value & ~bit : value | bit);
    return (
        <div className="space-y-3">
            {Object.entries(grouped).map(([cat, list]) => (
                <div key={cat}>
                    <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 mb-1">{cat}</div>
                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-1">
                        {list.map((p) => {
                            const on = (value & p.value) === p.value;
                            return (
                                <button
                                    key={p.name}
                                    type="button"
                                    onClick={() => toggle(p.value)}
                                    className={`flex items-start gap-2 rounded-md border px-2 py-1.5 text-left ${on ? "border-sky-400 bg-sky-50" : "border-slate-200 hover:bg-slate-50"}`}
                                >
                                    <span
                                        className={`mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded ${on ? "bg-sky-600 text-white" : "border border-slate-300"}`}
                                    >
                                        {on && <CheckIcon className="w-3 h-3" />}
                                    </span>
                                    <span className="min-w-0">
                                        <span className="block text-[12px] font-medium text-slate-700">
                                            {p.name.toLowerCase()}
                                        </span>
                                        <span className="block text-[11px] text-slate-400 leading-tight">{p.description}</span>
                                    </span>
                                </button>
                            );
                        })}
                    </div>
                </div>
            ))}
        </div>
    );
}

const DELIVERY_TONE: Record<WebhookDeliveryStatus, string> = {
    delivered: "bg-emerald-50 text-emerald-700 border-emerald-100",
    pending: "bg-slate-100 text-slate-600 border-slate-200",
    in_flight: "bg-sky-50 text-sky-700 border-sky-100",
    failed: "bg-amber-50 text-amber-700 border-amber-100",
    abandoned: "bg-rose-50 text-rose-700 border-rose-100",
};

function DeliveryStatusBadge({ status }: { status: WebhookDeliveryStatus }) {
    return (
        <span
            className={cn(
                "inline-flex items-center rounded-sm border px-1.5 py-0.5 text-[10px] uppercase tracking-[0.06em] font-semibold",
                DELIVERY_TONE[status] ?? DELIVERY_TONE.pending,
            )}
        >
            {status.replace("_", " ")}
        </span>
    );
}

// AppWebhookChip — an at-a-glance chip when the app has a webhook URL set,
// showing the live install count (per-org endpoints it materialized).
function AppWebhookChip({ app }: { app: OAuthApplication }) {
    const endpoints = useOAuthAppWebhookEndpoints(app.id, true);
    const installs = endpoints.data?.endpoints.length ?? 0;
    return (
        <span
            title={app.webhook_url}
            className="inline-flex items-center gap-1 rounded border border-sky-200 bg-sky-50 px-1.5 py-0.5 text-[10.5px] font-medium text-sky-700"
        >
            <WebhookIcon className="w-3 h-3" />
            {endpoints.isPending
                ? hostOf(app.webhook_url)
                : `${installs} ${installs === 1 ? "install" : "installs"}`}
        </span>
    );
}

function AppRow({ app, blocked }: { app: OAuthApplication; blocked: boolean }) {
    const del = useDeleteOAuthApp();
    const rotate = useRotateOAuthAppSecret();
    const confirm = useConfirm();
    const [secret, setSecret] = React.useState<string | null>(null);
    const [editOpen, setEditOpen] = React.useState(false);

    const onRotate = () =>
        confirm.show("Rotate this app's client secret? The current secret stops working immediately.", async () => {
            const res = await rotate.mutateAsync(app.id);
            setSecret(res.client_secret);
        });
    const onDelete = () =>
        confirm.show(`Delete "${app.name}"? Every token issued to it is revoked.`, async () => {
            await del.mutateAsync(app.id);
        });

    const webhookDomains = app.allowed_webhook_domains ?? [];

    return (
        <div className="rounded-lg border border-slate-200 p-3">
            <div className="flex items-start gap-3">
                <AppLogo name={app.name} url={app.logo_url} size="md" />
                <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-1.5 min-w-0">
                        <span className="text-[13px] font-semibold text-slate-800 truncate">{app.name}</span>
                        {app.suspended_at ? (
                            <span className="h-[18px] px-1.5 rounded bg-rose-50 text-rose-700 text-[10.5px] font-medium inline-flex items-center shrink-0">Suspended</span>
                        ) : app.status === "disabled" ? (
                            <span className="h-[18px] px-1.5 rounded bg-slate-100 text-slate-500 text-[10.5px] font-medium inline-flex items-center shrink-0">Disabled</span>
                        ) : null}
                    </div>
                    {app.description && <div className="text-[11.5px] text-slate-400 truncate">{app.description}</div>}
                    <div className="mt-1.5 flex items-center gap-1.5 flex-wrap">
                        <code className="truncate rounded border border-slate-200 bg-slate-50 px-1.5 py-0.5 text-[11px] font-mono text-slate-600">
                            {app.client_id}
                        </code>
                        <CopyButton value={app.client_id} label="ID" />
                        {app.webhook_url && <AppWebhookChip app={app} />}
                    </div>
                </div>
                <div className="flex shrink-0 items-center gap-1">
                    <button
                        onClick={() => setEditOpen(true)}
                        title="Edit app"
                        className="h-7 w-7 inline-flex items-center justify-center rounded-md text-slate-400 hover:bg-slate-100 hover:text-slate-700"
                    >
                        <PencilIcon className="w-3.5 h-3.5" />
                    </button>
                    <button
                        onClick={onRotate}
                        title="Rotate secret"
                        className="h-7 w-7 inline-flex items-center justify-center rounded-md text-slate-400 hover:bg-slate-100 hover:text-slate-700"
                    >
                        <RefreshCwIcon className="w-3.5 h-3.5" />
                    </button>
                    <button
                        onClick={onDelete}
                        title="Delete app"
                        className="h-7 w-7 inline-flex items-center justify-center rounded-md text-slate-400 hover:bg-rose-50 hover:text-rose-600"
                    >
                        <Trash2Icon className="w-3.5 h-3.5" />
                    </button>
                </div>
            </div>
            <div className="mt-2">
                <div className="text-[10px] uppercase tracking-[0.12em] text-slate-400 mb-1">Redirect URIs</div>
                <div className="flex flex-wrap gap-1">
                    {app.redirect_uris.map((u) => (
                        <span key={u} className="rounded bg-slate-100 px-1.5 py-0.5 text-[10.5px] font-mono text-slate-500">
                            {u}
                        </span>
                    ))}
                </div>
            </div>
            <div className="mt-2">
                <div className="text-[10px] uppercase tracking-[0.12em] text-slate-400 mb-1">Webhook domains</div>
                {webhookDomains.length === 0 ? (
                    <span className="text-[10.5px] text-slate-400">No webhook domains. This app can't register webhooks.</span>
                ) : (
                    <div className="flex flex-wrap gap-1">
                        {webhookDomains.map((d) => (
                            <span key={d} className="rounded bg-slate-100 px-1.5 py-0.5 text-[10.5px] font-mono text-slate-500">
                                {d}
                            </span>
                        ))}
                    </div>
                )}
            </div>
            {app.suspended_at && (
                <div className="mt-2 rounded-md bg-rose-50 px-3 py-2 text-[12px] text-rose-800 leading-relaxed">
                    Suspended by the instance’s administrators, so it can’t sign anyone in and its tokens don’t work
                    {app.suspended_reason ? `: ${app.suspended_reason}` : "."}
                </div>
            )}
            <AppListingPanel app={app} blocked={blocked} />
            {secret && (
                <div className="mt-2 rounded-md border border-amber-200 bg-amber-50 p-2">
                    <div className="text-[10.5px] uppercase tracking-[0.12em] text-amber-700 mb-1">New client secret (shown once)</div>
                    <div className="flex items-center gap-1.5">
                        <code className="flex-1 truncate text-[11.5px] font-mono text-amber-800" data-ph-mask="">
                            {secret}
                        </code>
                        <CopyButton value={secret} />
                    </div>
                </div>
            )}
            <AnimatePresence>
                {editOpen && <EditModal key="edit-oauth-app" app={app} onClose={() => setEditOpen(false)} />}
            </AnimatePresence>
        </div>
    );
}

// AppWebhookSecretRow — reveal + rotate the app-level webhook signing secret.
function AppWebhookSecretRow({ app }: { app: OAuthApplication }) {
    const [reveal, setReveal] = React.useState(false);
    const secret = useOAuthAppWebhookSecret(app.id, reveal);
    const rotate = useRotateOAuthAppWebhookSecret();
    const confirm = useConfirm();
    const [rotated, setRotated] = React.useState<string | null>(null);

    const onRotate = () =>
        confirm.show("Rotate this app's webhook signing secret? The current secret stops working immediately.", async () => {
            const res = await rotate.mutateAsync(app.id);
            setRotated(res.webhook_secret);
            setReveal(true);
        });

    return (
        <div className="space-y-2.5">
            <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Signing secret</div>
            <p className="text-[11.5px] text-slate-500 leading-relaxed">
                Verify the HMAC signature on every delivery with this secret. Shared across all installs of this app.
            </p>
            <div className="flex items-center gap-1.5">
                {reveal && secret.data ? (
                    <code
                        className="flex-1 truncate rounded-md border border-slate-200 bg-slate-50 px-2 h-7 inline-flex items-center text-[11.5px] font-mono text-slate-700"
                        data-ph-mask=""
                    >
                        {secret.data.webhook_secret}
                    </code>
                ) : (
                    <code className="flex-1 truncate rounded-md border border-slate-200 bg-slate-50 px-2 h-7 inline-flex items-center text-[11.5px] font-mono text-slate-400">
                        {reveal && secret.isPending ? "Loading…" : "••••••••••••••••••••••••"}
                    </code>
                )}
                {reveal && secret.data ? (
                    <CopyButton value={secret.data.webhook_secret} />
                ) : (
                    <button
                        type="button"
                        onClick={() => setReveal(true)}
                        className="inline-flex items-center gap-1 rounded-md border border-slate-200 px-2 h-7 text-[11.5px] text-slate-600 hover:bg-slate-50"
                    >
                        Reveal
                    </button>
                )}
                <button
                    type="button"
                    onClick={onRotate}
                    disabled={rotate.isPending}
                    className="inline-flex items-center gap-1 rounded-md border border-slate-200 px-2 h-7 text-[11.5px] text-slate-600 hover:bg-slate-50 disabled:opacity-60"
                >
                    {rotate.isPending ? <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> : <RefreshCwIcon className="w-3.5 h-3.5" />}
                    Rotate
                </button>
            </div>
            {rotated && (
                <div className="rounded-md border border-amber-200 bg-amber-50 p-2">
                    <div className="text-[10.5px] uppercase tracking-[0.12em] text-amber-700 mb-1">New signing secret (shown once)</div>
                    <div className="flex items-center gap-1.5">
                        <code className="flex-1 truncate text-[11.5px] font-mono text-amber-800">{rotated}</code>
                        <CopyButton value={rotated} />
                    </div>
                </div>
            )}
        </div>
    );
}

// AppWebhookInstalls — the per-org endpoints the app materialized, with health.
function AppWebhookInstalls({ app }: { app: OAuthApplication }) {
    const endpoints = useOAuthAppWebhookEndpoints(app.id, true);
    const list = endpoints.data?.endpoints ?? [];

    return (
        <div className="space-y-2">
            <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                Installations{list.length > 0 ? ` (${list.length})` : ""}
            </div>
            {endpoints.isPending ? (
                <div className="py-4 text-center text-[11.5px] text-slate-400">Loading installs…</div>
            ) : list.length === 0 ? (
                <p className="text-[11.5px] text-slate-400">No orgs have installed this app's webhook yet.</p>
            ) : (
                <div className="rounded-md border border-slate-200 divide-y divide-slate-100">
                    {list.map((e) => (
                        <InstallRow key={e.id} endpoint={e} />
                    ))}
                </div>
            )}
        </div>
    );
}

function InstallRow({ endpoint }: { endpoint: WebhookEndpoint }) {
    const failing = endpoint.consecutive_failures > 0;
    return (
        <div className="flex items-center gap-2 px-2.5 py-2">
            <div className="min-w-0 flex-1">
                <div className="flex items-center gap-1.5 flex-wrap">
                    <code className="text-[11.5px] font-mono text-slate-700 truncate">{endpoint.organization_id}</code>
                    {!endpoint.enabled ? (
                        <span className="inline-flex items-center rounded-sm bg-slate-100 border border-slate-200 px-1.5 py-0.5 text-[10px] uppercase tracking-[0.06em] font-semibold text-slate-500">
                            Disabled
                        </span>
                    ) : endpoint.verified_at ? (
                        <span className="inline-flex items-center rounded-sm bg-emerald-50 border border-emerald-100 px-1.5 py-0.5 text-[10px] uppercase tracking-[0.06em] font-semibold text-emerald-700">
                            Verified
                        </span>
                    ) : (
                        <span className="inline-flex items-center rounded-sm bg-amber-50 border border-amber-100 px-1.5 py-0.5 text-[10px] uppercase tracking-[0.06em] font-semibold text-amber-700">
                            Pending
                        </span>
                    )}
                    {failing && (
                        <span className="inline-flex items-center rounded-sm bg-rose-50 border border-rose-100 px-1.5 py-0.5 text-[10px] uppercase tracking-[0.06em] font-semibold text-rose-700">
                            {endpoint.consecutive_failures} failing
                        </span>
                    )}
                </div>
                <div className="mt-0.5 text-[10.5px] text-slate-400">
                    Last success {formatRelative(endpoint.last_success_at)}
                </div>
            </div>
        </div>
    );
}

const APP_DELIVERY_STATUSES: WebhookDeliveryStatus[] = ["pending", "in_flight", "delivered", "failed", "abandoned"];

// AppWebhookDeliveries — the cross-org delivery log (read-only, no redeliver).
function AppWebhookDeliveries({ app, catalog }: { app: OAuthApplication; catalog: WebhookEventDescriptor[] }) {
    const [status, setStatus] = useBrowseState<WebhookDeliveryStatus | "">(`settings.oauth-apps.${app.id}.deliveries.status`, "", webhookStatusSchema);
    const [eventType, setEventType] = useBrowseState(`settings.oauth-apps.${app.id}.deliveries.event-type`, "", browseSearchSchema);

    const first = useOAuthAppWebhookDeliveries(app.id, { status, eventType, limit: 25 });
    const [extra, setExtra] = React.useState<WebhookDelivery[]>([]);
    const [cursor, setCursor] = React.useState<string | null>(null);
    const [hasMore, setHasMore] = React.useState(false);
    const [loadingMore, setLoadingMore] = React.useState(false);

    React.useEffect(() => {
        setExtra([]);
        setCursor(first.data?.pagination.next_cursor ?? null);
        setHasMore(first.data?.pagination.has_more ?? false);
    }, [first.data, status, eventType]);

    const loadMore = async () => {
        if (!cursor) return;
        setLoadingMore(true);
        try {
            const res = await listOAuthAppWebhookDeliveries(app.id, { status, eventType, cursor, limit: 25 });
            setExtra((prev) => [...prev, ...res.data]);
            setCursor(res.pagination.next_cursor);
            setHasMore(res.pagination.has_more);
        } catch (e) {
            toast.error((e as { message?: string })?.message ?? "Could not load more deliveries");
        } finally {
            setLoadingMore(false);
        }
    };

    const rows = [...(first.data?.data ?? []), ...extra];

    return (
        <div className="space-y-2.5">
            <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Recent deliveries</div>
            <div className="flex items-center gap-2 flex-wrap">
                <select
                    value={status}
                    onChange={(e) => setStatus(e.target.value as WebhookDeliveryStatus | "")}
                    className="h-7 rounded-md border border-slate-200 bg-white px-2 text-[12px] text-slate-700 outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
                >
                    <option value="">All statuses</option>
                    {APP_DELIVERY_STATUSES.map((s) => (
                        <option key={s} value={s}>{s.replace("_", " ")}</option>
                    ))}
                </select>
                <select
                    value={eventType}
                    onChange={(e) => setEventType(e.target.value)}
                    className="h-7 rounded-md border border-slate-200 bg-white px-2 text-[12px] text-slate-700 outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100 max-w-[180px]"
                >
                    <option value="">All events</option>
                    {eventType && !catalog.some((d) => d.type === eventType) && <option value={eventType}>{eventType} (unavailable)</option>}
                    {catalog.map((d) => (
                        <option key={d.type} value={d.type}>{d.type}</option>
                    ))}
                </select>
            </div>

            {first.isPending ? (
                <div className="py-8 text-center text-[12px] text-slate-400">Loading deliveries…</div>
            ) : rows.length === 0 ? (
                <p className="text-[11.5px] text-slate-400 py-2">No deliveries yet. Once events fire, every attempt shows here.</p>
            ) : (
                <div className="rounded-md border border-slate-200 divide-y divide-slate-100">
                    {rows.map((d) => (
                        <AppDeliveryRow key={d.id} delivery={d} />
                    ))}
                </div>
            )}

            {hasMore && (
                <div className="flex justify-center pt-1">
                    <button
                        type="button"
                        onClick={loadMore}
                        disabled={loadingMore}
                        className="h-8 px-3 rounded-md border border-slate-200 text-[12px] text-slate-600 hover:bg-slate-50 disabled:opacity-60 inline-flex items-center gap-1.5"
                    >
                        {loadingMore && <Loader2Icon className="w-3.5 h-3.5 animate-spin" />}
                        Load more
                    </button>
                </div>
            )}
        </div>
    );
}

// AppDeliveryRow — one delivery attempt, expandable to payload + error (read-only).
function AppDeliveryRow({ delivery }: { delivery: WebhookDelivery }) {
    const [open, setOpen] = React.useState(false);

    const prettyPayload = React.useMemo(() => {
        try {
            return JSON.stringify(delivery.payload, null, 2);
        } catch {
            return String(delivery.payload);
        }
    }, [delivery.payload]);

    return (
        <div>
            <button
                type="button"
                onClick={() => setOpen((o) => !o)}
                className="w-full flex items-center gap-2 px-2.5 py-2 text-left hover:bg-slate-50/80 transition-colors"
            >
                <ChevronRightIcon className={cn("w-3.5 h-3.5 text-slate-300 shrink-0 transition-transform", open && "rotate-90")} />
                <code className="text-[11.5px] font-mono text-slate-700 truncate flex-1 min-w-0">{delivery.event_type}</code>
                <DeliveryStatusBadge status={delivery.status} />
                {typeof delivery.response_status === "number" && (
                    <span
                        className={cn(
                            "text-[11px] font-mono tabular-nums",
                            delivery.response_status >= 200 && delivery.response_status < 300 ? "text-emerald-600" : "text-rose-600",
                        )}
                    >
                        {delivery.response_status}
                    </span>
                )}
                <span className="text-[10.5px] text-slate-400 tabular-nums shrink-0">
                    {delivery.attempt_count}/{delivery.max_attempts}
                </span>
                <span className="text-[10.5px] text-slate-400 shrink-0 hidden sm:inline">{formatRelative(delivery.created_at)}</span>
            </button>
            <AnimatePresence initial={false}>
                {open && (
                    <motion.div
                        initial={{ height: 0, opacity: 0 }}
                        animate={{ height: "auto", opacity: 1 }}
                        exit={{ height: 0, opacity: 0 }}
                        transition={{ duration: 0.18 }}
                        className="overflow-hidden bg-slate-50/60"
                    >
                        <div className="px-3 py-2.5 space-y-2.5">
                            {delivery.error_reason && (
                                <div className="rounded-md border border-rose-200 bg-rose-50 px-2.5 py-1.5 text-[11px] text-rose-700 leading-relaxed">
                                    <span className="font-medium">Error:</span> {delivery.error_reason}
                                </div>
                            )}
                            {delivery.response_body_excerpt && (
                                <div>
                                    <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium mb-1">Response body</div>
                                    <pre className="rounded-md border border-slate-200 bg-white p-2 text-[10.5px] font-mono text-slate-600 whitespace-pre-wrap break-words max-h-32 overflow-y-auto">
                                        {delivery.response_body_excerpt}
                                    </pre>
                                </div>
                            )}
                            <div>
                                <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium mb-1">Payload</div>
                                <pre className="rounded-md border border-slate-200 bg-white p-2 text-[10.5px] font-mono text-slate-600 whitespace-pre-wrap break-words max-h-64 overflow-y-auto">
                                    {prettyPayload}
                                </pre>
                            </div>
                            <span className="block text-[10.5px] text-slate-400">
                                Last attempt {formatRelative(delivery.last_attempt_at ?? delivery.created_at)}
                            </span>
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}

// EditModal — a simple form modal for an existing app, styled to match the
// create wizard. Edits name/description/website/redirects/webhook domains/
// webhook URL + events/scopes via useUpdateOAuthApp, and surfaces the app's
// webhook signing secret, installs, and delivery log when a webhook URL is set.
function EditModal({ app, onClose }: { app: OAuthApplication; onClose: () => void }) {
    const update = useUpdateOAuthApp();
    const setLogo = useSetOAuthAppLogo();
    const deleteLogo = useDeleteOAuthAppLogo();
    const catalog = useWebhookEventCatalog();

    const [name, setName] = React.useState(app.name);
    const [description, setDescription] = React.useState(app.description);
    const [website, setWebsite] = React.useState(app.website_url);
    const [redirects, setRedirects] = React.useState(app.redirect_uris.join("\n"));
    const [webhookDomains, setWebhookDomains] = React.useState((app.allowed_webhook_domains ?? []).join("\n"));
    const [webhookUrl, setWebhookUrl] = React.useState(app.webhook_url ?? "");
    const [webhookEvents, setWebhookEvents] = React.useState<string[]>(app.webhook_events ?? []);
    const [scopes, setScopes] = React.useState(app.scopes);

    const redirectList = redirects.split("\n").map((s) => s.trim()).filter(Boolean);
    const webhookDomainList = webhookDomains.split("\n").map((s) => s.trim()).filter(Boolean);

    const webhookUrlValid = webhookUrl.trim() === "" || /^https:\/\/.+/i.test(webhookUrl.trim());

    const save = async () => {
        const nameProblem = appNameError("Name", name, app.name);
        if (nameProblem) {
            toast.error(nameProblem);
            return;
        }
        if (redirectList.length === 0) {
            toast.error("Add at least one redirect URI");
            return;
        }
        if (!webhookUrlValid) {
            toast.error("The webhook URL must start with https://");
            return;
        }
        if (scopes === 0) {
            toast.error("Select at least one scope");
            return;
        }
        try {
            await update.mutateAsync({
                id: app.id,
                data: {
                    name: name.trim(),
                    description: description.trim(),
                    website_url: website.trim(),
                    redirect_uris: redirectList,
                    allowed_webhook_domains: webhookDomainList,
                    webhook_url: webhookUrl.trim() || undefined,
                    webhook_events: webhookUrl.trim() ? webhookEvents : undefined,
                    scopes,
                },
            });
            toast.success("App updated");
            onClose();
        } catch (e) {
            toast.error((e as { message?: string })?.message ?? "Could not update the app");
        }
    };

    return createPortal(
        <div className="fixed inset-0 z-[60] flex items-center justify-center p-4">
            <motion.div
                className="absolute inset-0 bg-slate-900/40"
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                exit={{ opacity: 0 }}
                transition={{ duration: 0.12, ease: "easeOut" }}
                onClick={onClose}
            />
            <motion.div
                className="relative w-full max-w-lg max-h-[90vh] flex flex-col overflow-hidden rounded-xl bg-white shadow-xl ring-1 ring-slate-200"
                initial={{ opacity: 0, y: 20 }}
                animate={{ opacity: 1, y: 0 }}
                exit={{ opacity: 0, y: 20 }}
                transition={{ duration: 0.15, ease: "easeOut" }}
            >
                <div className="flex items-center border-b border-slate-200 px-4 h-11 shrink-0">
                    <span className="text-[12.5px] font-medium text-slate-900">Edit {app.name}</span>
                    <button onClick={onClose} className="ml-auto h-7 w-7 inline-flex items-center justify-center rounded-md text-slate-400 hover:bg-slate-100">
                        <XIcon className="w-4 h-4" />
                    </button>
                </div>
                <div className="flex-1 overflow-y-auto p-4 space-y-3">
                    <AvatarUploader
                        shape="square"
                        current={app.logo_url || null}
                        fallbackInitials={(app.name.trim()[0] ?? "?").toUpperCase()}
                        onUpload={async (blob) => {
                            await setLogo.mutateAsync({ id: app.id, blob });
                        }}
                        onRemove={async () => {
                            await deleteLogo.mutateAsync(app.id);
                        }}
                    />
                    <div>
                        <Label>Name</Label>
                        <TextInput value={name} onChange={setName} placeholder="Acme Integration" className="w-full" />
                    </div>
                    <div>
                        <Label>Description</Label>
                        <TextInput value={description} onChange={setDescription} placeholder="What the app does" className="w-full" />
                    </div>
                    <div>
                        <Label>Website</Label>
                        <TextInput value={website} onChange={setWebsite} placeholder="https://acme.com" className="w-full" />
                    </div>
                    <div>
                        <Label>Redirect URIs</Label>
                        <textarea
                            value={redirects}
                            onChange={(e) => setRedirects(e.target.value)}
                            placeholder={"https://acme.com/oauth/callback"}
                            rows={3}
                            className="w-full rounded-md border border-slate-200 bg-white px-2 py-1.5 text-[12px] font-mono text-slate-900 placeholder:text-slate-400 outline-none resize-y focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
                        />
                        <p className="mt-1 text-[11px] text-slate-400">One per line. Must be HTTPS (or a loopback URL), matched exactly.</p>
                    </div>
                    <div>
                        <Label>Webhook domains</Label>
                        <textarea
                            value={webhookDomains}
                            onChange={(e) => setWebhookDomains(e.target.value)}
                            placeholder={".acme.com\nhooks.partner.com"}
                            rows={3}
                            className="w-full rounded-md border border-slate-200 bg-white px-2 py-1.5 text-[12px] font-mono text-slate-900 placeholder:text-slate-400 outline-none resize-y focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
                        />
                        <p className="mt-1 text-[11px] text-slate-400 leading-relaxed">
                            Webhooks this app registers must point at these domains. Use a leading dot for subdomains (.acme.com
                            matches hooks.acme.com); a bare domain (acme.com) is an exact match. Leave empty to forbid this app from
                            registering webhooks.
                        </p>
                    </div>
                    <div>
                        <Label>Webhook URL</Label>
                        <TextInput value={webhookUrl} onChange={setWebhookUrl} placeholder="https://hooks.acme.com/warmbly" className="w-full" />
                        <p className="mt-1 text-[11px] text-slate-400 leading-relaxed">
                            Must be https and its host must fall within the allowed webhook domains above. Leave empty to disable webhooks.
                        </p>
                        {!webhookUrlValid && (
                            <p className="mt-1 text-[11px] text-rose-600">The webhook URL must start with https://</p>
                        )}
                    </div>
                    {webhookUrl.trim() !== "" && (
                        <div>
                            <Label>Events</Label>
                            {catalog.isPending ? (
                                <div className="py-6 text-center text-[11.5px] text-slate-400">Loading events…</div>
                            ) : (
                                <EventPicker
                                    catalog={catalog.data?.event_types ?? []}
                                    value={webhookEvents}
                                    onChange={setWebhookEvents}
                                />
                            )}
                        </div>
                    )}
                    <div>
                        <Label>Scopes</Label>
                        <ScopePicker value={scopes} onChange={setScopes} />
                    </div>

                    {app.webhook_url && (
                        <div className="border-t border-slate-200 pt-4 space-y-5">
                            <AppWebhookSecretRow app={app} />
                            <AppWebhookInstalls app={app} />
                            <AppWebhookDeliveries app={app} catalog={catalog.data?.event_types ?? []} />
                        </div>
                    )}
                </div>
                <div className="px-4 py-2.5 border-t border-slate-200 flex items-center gap-2 shrink-0">
                    <button onClick={onClose} className="h-8 px-3 rounded-md border border-slate-200 text-[12.5px] text-slate-600 hover:bg-slate-50">
                        Cancel
                    </button>
                    <button
                        onClick={save}
                        disabled={update.isPending}
                        className="ml-auto h-8 px-3 rounded-md bg-sky-600 text-white text-[12.5px] font-medium hover:bg-sky-700 disabled:opacity-60 inline-flex items-center gap-1.5"
                    >
                        {update.isPending ? <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> : <CheckIcon className="w-3.5 h-3.5" />}
                        Save changes
                    </button>
                </div>
            </motion.div>
        </div>,
        document.body,
    );
}

function AuthorizedTab() {
    const authorized = useWorkspaceAuthorizations();
    const revoke = useRevokeWorkspaceAuthorization();
    const confirm = useConfirm();
    const apps = authorized.data?.authorizations ?? [];

    if (apps.length === 0) {
        return <EmptyBlock title="No authorized apps" body="Apps anyone in this workspace connects via OAuth will appear here." />;
    }
    return (
        <div className="space-y-2">
            {apps.map((a) => (
                <div key={`${a.application_id}:${a.user_id}`} className="flex items-center gap-3 rounded-lg border border-slate-200 p-3">
                    <AppLogo name={a.name} url={a.logo_url} size="md" />
                    <div className="min-w-0 flex-1">
                        <div className="text-[13px] font-semibold text-slate-800 truncate">{a.name}</div>
                        <div className="text-[11.5px] text-slate-400 truncate">
                            Authorized by {a.user_name || a.user_email || "a former member"} on {new Date(a.authorized_at).toLocaleDateString()}
                        </div>
                    </div>
                    <button
                        onClick={() =>
                            confirm.show(`Revoke "${a.name}" for ${a.user_name || a.user_email || "this member"}? Its tokens stop working immediately.`, async () => {
                                await revoke.mutateAsync({ applicationId: a.application_id, userId: a.user_id });
                            })
                        }
                        className="h-7 px-2.5 rounded-md border border-slate-200 text-[12px] text-slate-600 hover:bg-rose-50 hover:text-rose-600 hover:border-rose-200"
                    >
                        Revoke
                    </button>
                </div>
            ))}
        </div>
    );
}

export default function OAuthAppsPage() {
    const canManage = usePermission("MANAGE_API_KEYS");
    const apps = useOAuthApps();
    const [tab, setTab] = useBrowseState("settings.oauth-apps.tab", "apps", oauthAppsTabSchema);
    const [createOpen, setCreateOpen] = React.useState(false);

    if (!canManage) return <NoAccess feature="OAuth apps" permissionLabel="Manage API keys" />;

    const list = apps.data?.applications ?? [];
    const access = apps.data?.developer_access;

    return (
        <SectionShell
            title="OAuth apps"
            description="Apps that connect to Warmbly on a user's behalf via OAuth2."
            actions={
                tab === "apps" ? (
                    <button
                        onClick={() => setCreateOpen(true)}
                        disabled={access?.blocked}
                        title={access?.blocked ? "Registering apps is blocked for this workspace" : undefined}
                        className="h-7 px-3 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 disabled:bg-slate-200 disabled:text-slate-500"
                    >
                        <PlusIcon className="w-3.5 h-3.5" /> Register app
                    </button>
                ) : null
            }
        >
            <div className="px-4 py-5 md:px-8 md:py-6">
                {access?.blocked && (
                    <div className="mb-4 rounded-md bg-amber-50 px-3 py-2.5 text-[12.5px] text-amber-900 leading-relaxed">
                        The instance’s administrators have blocked this workspace from registering and publishing apps
                        {access.reason ? `: ${access.reason}` : "."} Existing apps keep working unless they are suspended.
                    </div>
                )}
                <div className="mb-3 flex items-center gap-1 border-b border-slate-200">
                    {(
                        [
                            ["apps", "Your apps"],
                            ["authorized", "Authorized apps"],
                        ] as const
                    ).map(([k, label]) => (
                        <button
                            key={k}
                            onClick={() => setTab(k)}
                            className={`relative h-9 px-2.5 text-[12.5px] transition-colors ${tab === k ? "text-slate-900 font-medium" : "text-slate-500 hover:text-slate-700"}`}
                        >
                            {label}
                            {tab === k && (
                                <motion.span
                                    layoutId="oauth-apps-tab"
                                    className="absolute inset-x-1 -bottom-px h-0.5 rounded bg-sky-600"
                                    transition={{ type: "spring", stiffness: 520, damping: 40 }}
                                />
                            )}
                        </button>
                    ))}
                </div>

                <AnimatePresence mode="wait" initial={false}>
                    <motion.div
                        key={tab}
                        initial={{ opacity: 0, x: 8 }}
                        animate={{ opacity: 1, x: 0 }}
                        exit={{ opacity: 0, x: -8 }}
                        transition={{ duration: 0.15, ease: "easeOut" }}
                    >
                        {tab === "apps" ? (
                            list.length === 0 ? (
                                <EmptyBlock
                                    title="No OAuth apps yet"
                                    body="Register an app to let it request scoped access to Warmbly accounts via OAuth2."
                                />
                            ) : (
                                <div className="space-y-2">
                                    {list.map((app) => (
                                        <AppRow key={app.id} app={app} blocked={!!access?.blocked} />
                                    ))}
                                </div>
                            )
                        ) : (
                            <AuthorizedTab />
                        )}
                    </motion.div>
                </AnimatePresence>
            </div>

            <AnimatePresence>
                {createOpen && <RegisterAppDialog key="register-oauth-app" onClose={() => setCreateOpen(false)} />}
            </AnimatePresence>
        </SectionShell>
    );
}
