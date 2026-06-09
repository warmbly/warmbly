// Integrations marketplace.
//
// The connect experience is the point of this page: a searchable app directory
// grouped by category, a "connected" rail across the top, a multi-step connect
// drawer (one-click OAuth where the provider supports it), and a management
// drawer for health, reauth, and event automations. Realtime keeps connection
// state live without a manual refresh.

"use client";

import React from "react";
import { motion } from "framer-motion";
import { CalendarCheckIcon, ExternalLinkIcon, RefreshCwIcon, SettingsIcon } from "lucide-react";

import {
    EmptyBlock,
    Page,
    PageBody,
    PageTopbar,
    SectionBar,
    Stat,
    StatStrip,
} from "@/components/layout/Page";
import { SearchInput } from "@/components/ui/field";
import useIntegrationCatalog from "@/lib/api/hooks/app/integrations/useIntegrationCatalog";
import useIntegrationConnections from "@/lib/api/hooks/app/integrations/useIntegrationConnections";
import useMeetingBookings from "@/lib/api/hooks/app/integrations/useMeetingBookings";
import {
    CATEGORY_LABELS,
    CATEGORY_ORDER,
    type IntegrationCatalogEntry,
    type IntegrationCategory,
    type IntegrationConnection,
    type IntegrationProvider,
} from "@/lib/api/models/app/integrations/Integration";
import { cn } from "@/lib/utils";

import ConnectDrawer from "./_components/ConnectDrawer";
import ConnectionDetail from "./_components/ConnectionDetail";
import InboundUrlDialog from "./_components/InboundUrlDialog";
import ProviderGlyph from "./_components/ProviderGlyph";
import StatusPill from "./_components/StatusPill";

export default function IntegrationsPage() {
    const catalogQuery = useIntegrationCatalog();
    const connectionsQuery = useIntegrationConnections();
    const bookingsQuery = useMeetingBookings();

    const [connectTarget, setConnectTarget] = React.useState<IntegrationCatalogEntry | null>(null);
    const [manageTarget, setManageTarget] = React.useState<IntegrationConnection | null>(null);
    const [inboundUrl, setInboundUrl] = React.useState<{ provider: IntegrationProvider; url: string } | null>(null);
    const [query, setQuery] = React.useState("");

    const catalog = React.useMemo(() => catalogQuery.data?.catalog ?? [], [catalogQuery.data?.catalog]);
    const connections = React.useMemo(
        () => connectionsQuery.data?.connections ?? [],
        [connectionsQuery.data?.connections],
    );
    const bookings = bookingsQuery.data?.bookings ?? [];

    const entryByProvider = React.useMemo(() => {
        const m: Record<string, IntegrationCatalogEntry> = {};
        for (const e of catalog) m[e.provider] = e;
        return m;
    }, [catalog]);

    const firstConnByProvider = React.useMemo(() => {
        const m: Record<string, IntegrationConnection> = {};
        for (const c of connections) if (!m[c.provider]) m[c.provider] = c;
        return m;
    }, [connections]);

    const q = query.trim().toLowerCase();
    const filtered = React.useMemo(() => {
        if (!q) return catalog;
        return catalog.filter(
            (e) =>
                e.name.toLowerCase().includes(q) ||
                e.tagline.toLowerCase().includes(q) ||
                e.category.toLowerCase().includes(q),
        );
    }, [catalog, q]);

    const grouped = React.useMemo(() => {
        const map: Partial<Record<IntegrationCategory, IntegrationCatalogEntry[]>> = {};
        for (const entry of filtered) (map[entry.category] ??= []).push(entry);
        return map;
    }, [filtered]);

    const connectedCount = connections.filter((c) => c.status === "connected").length;
    const attentionCount = connections.filter(
        (c) => c.status === "degraded" || c.status === "reauth_required",
    ).length;

    function refreshAll() {
        catalogQuery.refetch();
        connectionsQuery.refetch();
        bookingsQuery.refetch();
    }

    function onCardClick(entry: IntegrationCatalogEntry) {
        const existing = firstConnByProvider[entry.provider];
        if (existing) setManageTarget(existing);
        else setConnectTarget(entry);
    }

    return (
        <Page>
            <PageTopbar eyebrow="Integrations" subtitle="Connect your stack — CRMs, alerts, automation, meetings, and data">
                <div className="flex items-center gap-2">
                    <div className="w-48 hidden sm:block">
                        <SearchInput value={query} onChange={setQuery} placeholder="Search integrations" />
                    </div>
                    <button
                        type="button"
                        onClick={refreshAll}
                        aria-label="Refresh"
                        className="h-7 w-7 rounded-md border border-slate-200 hover:border-slate-300 text-slate-500 hover:text-slate-900 inline-flex items-center justify-center transition-colors"
                    >
                        <RefreshCwIcon className={cn("w-3 h-3", connectionsQuery.isFetching && "animate-spin")} />
                    </button>
                </div>
            </PageTopbar>

            <StatStrip cols={4}>
                <Stat label="Available" value={catalog.length} sub="providers" />
                <Stat label="Connected" value={connectedCount} sub={`${connections.length} total`} accent={connectedCount > 0} />
                <Stat label="Needs attention" value={attentionCount} sub={attentionCount > 0 ? "reconnect / errors" : "all healthy"} />
                <Stat label="Meetings" value={bookings.length} sub="booked via integrations" last />
            </StatStrip>

            <PageBody>
                {/* Connected rail */}
                {connections.length > 0 && (
                    <section>
                        <SectionBar label="Your connections" count={connections.length} />
                        <div className="grid sm:grid-cols-2 lg:grid-cols-3 gap-px bg-slate-200/60 border-b border-slate-200/60">
                            {connections.map((c, i) => (
                                <ConnectionCard
                                    key={c.id}
                                    index={i}
                                    connection={c}
                                    entry={entryByProvider[c.provider]}
                                    onManage={() => setManageTarget(c)}
                                />
                            ))}
                        </div>
                    </section>
                )}

                {/* Catalog by category */}
                {CATEGORY_ORDER.map((category) => {
                    const entries = grouped[category] ?? [];
                    if (entries.length === 0) return null;
                    return (
                        <section key={category}>
                            <SectionBar label={CATEGORY_LABELS[category]} count={entries.length} />
                            <div className="grid sm:grid-cols-2 lg:grid-cols-3 gap-px bg-slate-200/60 border-b border-slate-200/60">
                                {entries.map((entry, i) => (
                                    <CatalogCard
                                        key={entry.provider}
                                        index={i}
                                        entry={entry}
                                        connection={firstConnByProvider[entry.provider]}
                                        onClick={() => onCardClick(entry)}
                                    />
                                ))}
                            </div>
                        </section>
                    );
                })}

                {q && filtered.length === 0 && (
                    <EmptyBlock title="No matches" body={`Nothing in the catalog matches “${query}”.`} />
                )}

                {/* Meetings */}
                <SectionBar label="Meeting bookings" count={bookings.length}>
                    <CalendarCheckIcon className="w-3 h-3 text-slate-400" />
                </SectionBar>
                {bookings.length === 0 ? (
                    <EmptyBlock
                        title="No meetings booked yet"
                        body="Connect Calendly or Cal.com to credit booked meetings to the campaign that surfaced the lead."
                    />
                ) : (
                    <div className="divide-y divide-slate-200/60 border-b border-slate-200/60">
                        {bookings.slice(0, 12).map((b) => (
                            <div key={b.id} className="px-5 h-12 flex items-center gap-3 text-[12.5px]">
                                <span
                                    className={cn(
                                        "size-1.5 rounded-full shrink-0",
                                        b.source === "calendly" ? "bg-rose-400" : "bg-indigo-400",
                                    )}
                                />
                                <span className="font-medium text-slate-900 truncate w-32 sm:w-60">{b.invitee_email}</span>
                                <span className="text-slate-500 truncate flex-1">{b.event_name}</span>
                                <span className="font-mono text-[10.5px] text-slate-400 tabular-nums">
                                    {b.scheduled_for ? new Date(b.scheduled_for).toLocaleString() : "tbd"}
                                </span>
                            </div>
                        ))}
                    </div>
                )}
            </PageBody>

            {connectTarget && (
                <ConnectDrawer
                    entry={connectTarget}
                    onClose={() => setConnectTarget(null)}
                    onConnected={(conn) => {
                        connectionsQuery.refetch();
                        if (conn.inbound_webhook_url) {
                            setInboundUrl({ provider: conn.provider, url: conn.inbound_webhook_url });
                        } else {
                            // Drop straight into management so the user can wire automations.
                            setManageTarget(conn);
                        }
                    }}
                />
            )}
            {manageTarget && (
                <ConnectionDetail
                    connection={manageTarget}
                    entry={entryByProvider[manageTarget.provider]}
                    onClose={() => {
                        setManageTarget(null);
                        connectionsQuery.refetch();
                    }}
                />
            )}
            {inboundUrl && (
                <InboundUrlDialog
                    provider={inboundUrl.provider}
                    url={inboundUrl.url}
                    onClose={() => setInboundUrl(null)}
                />
            )}
        </Page>
    );
}

function CatalogCard({
    entry,
    connection,
    onClick,
    index = 0,
}: {
    entry: IntegrationCatalogEntry;
    connection?: IntegrationConnection;
    onClick: () => void;
    index?: number;
}) {
    const connected = !!connection;
    const comingSoon = entry.auth_method === "oauth" && !entry.configured && !connected;
    return (
        <motion.button
            type="button"
            onClick={onClick}
            initial={{ opacity: 0, y: 8 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.25, ease: [0.16, 1, 0.3, 1], delay: Math.min(index, 12) * 0.03 }}
            className="text-left bg-white p-5 flex flex-col min-h-[150px] hover:bg-slate-50/60 transition-colors group"
        >
            <div className="flex items-start justify-between gap-3">
                <div className="flex items-center gap-2.5 min-w-0">
                    <ProviderGlyph provider={entry.provider} name={entry.name} />
                    <div className="min-w-0">
                        <div className="text-[13px] font-semibold text-slate-900 truncate">{entry.name}</div>
                        <div className="text-[10px] uppercase tracking-[0.08em] text-slate-400 font-mono">
                            {entry.auth_method === "oauth" ? "one-click" : entry.auth_method}
                            {entry.beta && <span className="ml-1.5 text-amber-600">· beta</span>}
                        </div>
                    </div>
                </div>
                {connected ? <StatusPill status={connection.status} /> : comingSoon ? <ComingSoon /> : null}
            </div>

            <p className="mt-3 text-[12px] text-slate-600 leading-relaxed line-clamp-2">{entry.tagline}</p>

            <div className="mt-auto pt-3 flex items-center justify-between gap-2">
                {entry.docs_url ? (
                    <a
                        href={entry.docs_url}
                        target="_blank"
                        rel="noopener noreferrer"
                        onClick={(e) => e.stopPropagation()}
                        className="text-[11px] text-slate-400 hover:text-sky-700 inline-flex items-center gap-1 underline decoration-dotted underline-offset-2"
                    >
                        <ExternalLinkIcon className="w-3 h-3" />
                        Docs
                    </a>
                ) : (
                    <span />
                )}
                <span
                    className={cn(
                        "h-7 px-2.5 rounded-md text-[11.5px] font-medium inline-flex items-center gap-1 transition-colors",
                        connected
                            ? "text-slate-600 group-hover:text-slate-900 group-hover:bg-slate-100"
                            : comingSoon
                              ? "text-slate-300"
                              : "bg-sky-600 text-white group-hover:bg-sky-700",
                    )}
                >
                    {connected ? (
                        <>
                            <SettingsIcon className="w-3 h-3" />
                            Manage
                        </>
                    ) : comingSoon ? (
                        "Coming soon"
                    ) : (
                        "Connect"
                    )}
                </span>
            </div>
        </motion.button>
    );
}

function ConnectionCard({
    connection,
    entry,
    onManage,
    index = 0,
}: {
    connection: IntegrationConnection;
    entry?: IntegrationCatalogEntry;
    onManage: () => void;
    index?: number;
}) {
    const account =
        connection.external_account_name ||
        (connection.display_fields as Record<string, string>)?.account ||
        (connection.display_fields as Record<string, string>)?.workspace ||
        (connection.display_fields as Record<string, string>)?.channel ||
        "";
    return (
        <motion.button
            type="button"
            onClick={onManage}
            initial={{ opacity: 0, y: 8 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.25, ease: [0.16, 1, 0.3, 1], delay: Math.min(index, 12) * 0.03 }}
            className="text-left bg-white p-4 flex items-center gap-3 hover:bg-slate-50/60 transition-colors"
        >
            <ProviderGlyph provider={connection.provider} name={entry?.name ?? connection.label} />
            <div className="min-w-0 flex-1">
                <div className="text-[12.5px] font-semibold text-slate-900 truncate">{connection.label}</div>
                <div className="text-[11px] text-slate-400 truncate">{account || (entry?.name ?? connection.provider)}</div>
            </div>
            <StatusPill status={connection.status} />
        </motion.button>
    );
}

function ComingSoon() {
    return (
        <span className="inline-flex items-center h-5 px-1.5 rounded text-[9.5px] uppercase tracking-[0.08em] font-medium bg-slate-100 text-slate-400">
            soon
        </span>
    );
}
