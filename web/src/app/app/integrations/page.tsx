// Integrations.
//
// Every view has its own URL under /app/integrations: Discover, /all,
// /connected, /community, /build, /category/:category, a built-in integration's
// page at /:provider and a community app's page at /apps/:slug. The route keeps
// the page mounted across them, so drawers and scroll state survive.

"use client";

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { Link, Navigate, useNavigate, useParams } from "@tanstack/react-router";
import {
    AlertTriangleIcon,
    ArrowRightIcon,
    BlocksIcon,
    BotIcon,
    ChevronRightIcon,
    CompassIcon,
    InboxIcon,
    KeyRoundIcon,
    LayoutGridIcon,
    PlugIcon,
    UsersIcon,
    WebhookIcon,
    WrenchIcon,
} from "lucide-react";

import { Page, PageTopbar, TopbarAction } from "@/components/layout/Page";
import { useSearchParams } from "@/hooks/useSearchParams";
import ScrollStrip from "@/components/ui/scroll-strip";
import { useCommunityApps } from "@/lib/api/hooks/app/integrations/useCommunityApps";
import useIntegrationCatalog from "@/lib/api/hooks/app/integrations/useIntegrationCatalog";
import useIntegrationConnections from "@/lib/api/hooks/app/integrations/useIntegrationConnections";
import { POPULAR_INSTALLS, type CommunityApp } from "@/lib/api/models/app/integrations/Community";
import type {
    IntegrationCatalogEntry,
    IntegrationCategory,
    IntegrationConnection,
    IntegrationProvider,
} from "@/lib/api/models/app/integrations/Integration";
import { hrefTarget } from "@/lib/routerSearch";
import { cn } from "@/lib/utils";

import ConnectDialog from "./_components/ConnectDialog";
import ConnectionDetail from "./_components/ConnectionDetail";
import InboundUrlDialog from "./_components/InboundUrlDialog";
import AppCard from "./_components/store/AppCard";
import { BuiltinDetail, CommunityDetail } from "./_components/store/AppDetail";
import Browse from "./_components/store/Browse";
import SearchBox from "./_components/store/SearchBox";
import StoreSidebar, { type NavEntry } from "./_components/store/StoreSidebar";
import {
    builtinItem,
    CATEGORY_META,
    categoryLabel,
    communityItem,
    isUsable,
    itemPath,
    STORE_BASE,
    STORE_CATEGORIES,
    type StoreCategory,
    type StoreItem,
    withQuery,
} from "./_components/store/model";

// Providers with their own home page instead of the generic drawers.
const CRM_HOMES = new Set<string>(["hubspot", "pipedrive"]);

type Route =
    | { kind: "home" }
    | { kind: "all" }
    | { kind: "connected" }
    | { kind: "community" }
    | { kind: "build" }
    | { kind: "category"; category: StoreCategory }
    | { kind: "app"; slug: string }
    | { kind: "builtin"; provider: string };

function parseRoute(splat: string): Route | null {
    const parts = splat.split("/").filter(Boolean);
    if (parts.length === 0) return { kind: "home" };
    if (parts.length === 1 && (parts[0] === "all" || parts[0] === "connected" || parts[0] === "community" || parts[0] === "build")) {
        return { kind: parts[0] };
    }
    if (parts.length === 2 && parts[0] === "category" && (STORE_CATEGORIES as string[]).includes(parts[1])) {
        return { kind: "category", category: parts[1] as StoreCategory };
    }
    if (parts.length === 2 && parts[0] === "apps") return { kind: "app", slug: parts[1] };
    if (parts.length === 1) return { kind: "builtin", provider: parts[0] };
    return null;
}

// The gaps a recommendation fills, in the order they are offered.
const GAPS: { category: IntegrationCategory; prefer?: IntegrationProvider }[] = [
    { category: "crm" },
    { category: "notifications", prefer: "slack" },
    { category: "meetings" },
    { category: "verification" },
];

export default function IntegrationsPage() {
    // Mounted for both /app/integrations and its splat, so read the splat loosely.
    const splat = useParams({ strict: false })._splat ?? "";
    const route = parseRoute(splat);
    if (!route) return <Navigate to={STORE_BASE} replace />;
    return <IntegrationsStore route={route} routeKey={splat} />;
}

function IntegrationsStore({ route, routeKey }: { route: Route; routeKey: string }) {
    const navigate = useNavigate();
    const [searchParams, setSearchParams] = useSearchParams();
    const topRef = React.useRef<HTMLDivElement | null>(null);

    const catalogQuery = useIntegrationCatalog();
    const connectionsQuery = useIntegrationConnections();
    const communityQuery = useCommunityApps();

    const [connectTarget, setConnectTarget] = React.useState<IntegrationCatalogEntry | null>(null);
    const [manageTarget, setManageTarget] = React.useState<IntegrationConnection | null>(null);
    const [inboundUrl, setInboundUrl] = React.useState<{ provider: IntegrationProvider; url: string } | null>(null);

    React.useEffect(() => {
        topRef.current?.scrollIntoView({ block: "start" });
    }, [routeKey]);

    const catalog = React.useMemo(
        () => [...(catalogQuery.data?.catalog ?? [])].sort((a, b) => (a.rank || 99) - (b.rank || 99)),
        [catalogQuery.data?.catalog],
    );
    const connections = React.useMemo(() => connectionsQuery.data?.connections ?? [], [connectionsQuery.data?.connections]);
    const community = React.useMemo(() => communityQuery.data?.data ?? [], [communityQuery.data?.data]);

    const entryByProvider = React.useMemo(() => {
        const m: Partial<Record<string, IntegrationCatalogEntry>> = {};
        for (const e of catalog) m[e.provider] = e;
        return m;
    }, [catalog]);

    // A disconnected row is history, not a connection.
    const liveConnections = React.useMemo(() => connections.filter((c) => c.status !== "disconnected"), [connections]);
    const connByProvider = React.useMemo(() => {
        const m: Partial<Record<string, IntegrationConnection>> = {};
        for (const c of liveConnections) if (!m[c.provider]) m[c.provider] = c;
        return m;
    }, [liveConnections]);

    const builtins = React.useMemo(() => catalog.map(builtinItem), [catalog]);
    const communityItems = React.useMemo(() => community.map(communityItem), [community]);
    const allItems = React.useMemo(() => [...builtins, ...communityItems], [builtins, communityItems]);

    const counts = React.useMemo(() => {
        const m: Partial<Record<string, number>> = {};
        for (const it of allItems) m[it.category] = (m[it.category] ?? 0) + 1;
        return m;
    }, [allItems]);
    const visibleCategories = STORE_CATEGORIES.filter((c) => (counts[c] ?? 0) > 0);

    const recommendations = React.useMemo(() => {
        const have = new Set(liveConnections.map((c) => entryByProvider[c.provider]?.category));
        const out: StoreItem[] = [];
        for (const gap of GAPS) {
            if (have.has(gap.category)) continue;
            const options = catalog.filter((e) => e.category === gap.category && isUsable(e));
            const pick = options.find((e) => e.provider === gap.prefer) ?? options[0];
            if (pick) out.push(builtinItem(pick));
        }
        return out.slice(0, 4);
    }, [liveConnections, entryByProvider, catalog]);

    // ?connect=1 (a link from the assistant) opens the connect popup once.
    const wantsConnect = route.kind === "builtin" && searchParams.get("connect") === "1";
    React.useEffect(() => {
        if (!wantsConnect || route.kind !== "builtin" || catalogQuery.isPending || connectionsQuery.isPending) return;
        const entry = entryByProvider[route.provider];
        if (entry && isUsable(entry) && !connByProvider[route.provider]) setConnectTarget(entry);
        const next = new URLSearchParams(searchParams);
        next.delete("connect");
        setSearchParams(next, { replace: true });
    }, [wantsConnect, route, catalogQuery.isPending, connectionsQuery.isPending, entryByProvider, connByProvider, searchParams, setSearchParams]);

    const isConnected = React.useCallback(
        (item: StoreItem) => (item.kind === "builtin" ? !!connByProvider[item.entry.provider] : item.app.installed),
        [connByProvider],
    );
    const connectedCount = React.useMemo(() => allItems.filter(isConnected).length, [allItems, isConnected]);

    function go(path: string) {
        navigate(hrefTarget(path));
    }

    // HubSpot and Pipedrive have their own home (connect, CRM setup and
    // settings), so every way into them leads there, not to the generic drawers.
    function manage(c: IntegrationConnection) {
        if (CRM_HOMES.has(c.provider)) go(`${STORE_BASE}/${c.provider}`);
        else setManageTarget(c);
    }

    function openItem(item: StoreItem) {
        go(itemPath(item));
    }

    // The card's own button: connect or manage a built-in in place, open a community app's page.
    function actOnItem(item: StoreItem) {
        if (item.kind === "community") {
            openItem(item);
            return;
        }
        if (CRM_HOMES.has(item.entry.provider)) {
            go(`${STORE_BASE}/${item.entry.provider}`);
            return;
        }
        const existing = connByProvider[item.entry.provider];
        if (existing) manage(existing);
        else setConnectTarget(item.entry);
    }

    const cardProps = (item: StoreItem) => ({
        item,
        connection: item.kind === "builtin" ? connByProvider[item.entry.provider] : undefined,
        onOpen: () => openItem(item),
        onAction: () => actOnItem(item),
    });

    const relatedTo = (category: string, exclude: string) => allItems.filter((it) => it.category === category && it.key !== exclude);

    const attention = connections.filter((c) => c.status === "degraded" || c.status === "reauth_required");

    let content: React.ReactNode;
    if (catalogQuery.isPending) {
        content = <GridSkeleton />;
    } else if (route.kind === "builtin") {
        const entry = entryByProvider[route.provider];
        content = !entry ? (
            <Navigate to={STORE_BASE} replace />
        ) : (
            <BuiltinDetail
                entry={entry}
                connections={connections.filter((c) => c.provider === entry.provider)}
                allConnections={connByProvider}
                related={relatedTo(entry.category, `b:${entry.provider}`)}
                onConnect={() => setConnectTarget(entry)}
                onManage={manage}
                onOpenItem={openItem}
                onActItem={actOnItem}
            />
        );
    } else if (route.kind === "app") {
        content = (
            <CommunityDetail
                slug={route.slug}
                preview={community.find((a) => a.slug === route.slug)}
                allConnections={connByProvider}
                related={(app: CommunityApp) => relatedTo(app.category, `c:${app.slug}`)}
                onOpenItem={openItem}
                onActItem={actOnItem}
            />
        );
    } else if (route.kind === "all") {
        content = (
            <Browse
                items={allItems}
                isConnected={isConnected}
                cardProps={cardProps}
                header={<ViewHeader title="All apps" description="Every integration and listed community app." />}
            />
        );
    } else if (route.kind === "connected") {
        content = (
            <Browse
                items={allItems}
                locked={{ status: "connected" }}
                isConnected={isConnected}
                cardProps={cardProps}
                header={<ViewHeader title="Connected" description="Everything this workspace uses. Open one to manage it." />}
                empty={
                    <EmptyNote
                        title="Nothing connected yet"
                        body="Start with a CRM or a Slack alert. Most connect in one click."
                        action={{ label: "Discover integrations", onClick: () => go(STORE_BASE) }}
                    />
                }
            />
        );
    } else if (route.kind === "community") {
        content = (
            <Browse
                items={allItems}
                locked={{ type: "community" }}
                isConnected={isConnected}
                cardProps={cardProps}
                header={
                    <ViewHeader
                        title="Community apps"
                        description={`Built on the Warmbly API by other teams. An app is listed when we feature it or ${POPULAR_INSTALLS} workspaces use it; others are shared by link.`}
                    />
                }
                empty={
                    <EmptyNote
                        title="No community apps yet"
                        body="Built something on the Warmbly API? Publish it and share the link with the people who use it."
                        action={{ label: "Publish an app", to: "/app/settings/oauth-apps" }}
                    />
                }
            />
        );
    } else if (route.kind === "build") {
        content = <BuildView />;
    } else if (route.kind === "category") {
        content = (
            <Browse
                items={allItems}
                locked={{ categories: [route.category] }}
                isConnected={isConnected}
                cardProps={cardProps}
                header={<ViewHeader title={categoryLabel(route.category)} description={`${CATEGORY_META[route.category].blurb}.`} />}
            />
        );
    } else {
        const featured = communityItems.filter((it) => it.kind === "community" && it.app.status === "featured");
        content = (
            <>
                {attention.length > 0 && (
                    <div className="flex items-center gap-2.5 rounded-md bg-amber-50 px-3 py-2 text-[12.5px] text-amber-900 flex-wrap">
                        <AlertTriangleIcon className="w-3.5 h-3.5 text-amber-600 shrink-0" />
                        <span className="mr-auto">
                            {attention.length === 1 ? "A connection needs attention." : `${attention.length} connections need attention.`}
                        </span>
                        {attention.map((c) => (
                            <button key={c.id} type="button" onClick={() => manage(c)} className="font-medium underline-offset-2 hover:underline">
                                Fix {c.label || entryByProvider[c.provider]?.name}
                            </button>
                        ))}
                    </div>
                )}

                {recommendations.length > 0 && (
                    <Section title="Recommended for you" description="For the gaps in what this workspace has connected.">
                        <CardGrid>
                            {recommendations.map((it) => (
                                <AppCard key={it.key} {...cardProps(it)} />
                            ))}
                        </CardGrid>
                    </Section>
                )}

                <Section title="Popular" description="Most used by workspaces on this instance." onSeeAll={() => go(`${STORE_BASE}/all`)}>
                    <CardGrid>
                        {builtins.slice(0, 8).map((it) => (
                            <AppCard key={it.key} {...cardProps(it)} />
                        ))}
                    </CardGrid>
                </Section>

                {featured.length > 0 && (
                    <Section
                        title="Featured community apps"
                        description="Built on the Warmbly API by other teams, picked by us."
                        onSeeAll={() => go(`${STORE_BASE}/community?featured=1`)}
                    >
                        <CardGrid>
                            {featured.slice(0, 8).map((it) => (
                                <AppCard key={it.key} {...cardProps(it)} />
                            ))}
                        </CardGrid>
                    </Section>
                )}

                <p className="text-[12.5px] text-slate-500">
                    Don’t see your tool? Connect it through Zapier, Make or n8n, or{" "}
                    <Link to="/app/integrations/$" params={{ _splat: "build" }} className="text-sky-700 hover:text-sky-800">
                        build your own
                    </Link>
                    .
                </p>
            </>
        );
    }

    const active = route.kind === "category" ? `category/${route.category}` : route.kind;
    const navGroups: { label?: string; items: NavEntry[] }[] = [
        {
            items: [
                { key: "home", label: "Discover", to: STORE_BASE, icon: CompassIcon },
                { key: "all", label: "All apps", to: `${STORE_BASE}/all`, icon: LayoutGridIcon, count: allItems.length },
                { key: "connected", label: "Connected", to: `${STORE_BASE}/connected`, icon: PlugIcon, count: connectedCount },
            ],
        },
        {
            label: "Categories",
            items: visibleCategories.map((c) => ({
                key: `category/${c}`,
                label: categoryLabel(c),
                to: `${STORE_BASE}/category/${c}`,
                icon: CATEGORY_META[c].icon,
                count: counts[c],
            })),
        },
        {
            label: "Developers",
            items: [
                { key: "community", label: "Community apps", to: `${STORE_BASE}/community`, icon: UsersIcon, count: community.length },
                { key: "build", label: "Build your own", to: `${STORE_BASE}/build`, icon: WrenchIcon },
            ],
        },
    ];

    // On a list the search filters what is shown; elsewhere it suggests.
    const browsing = route.kind === "all" || route.kind === "connected" || route.kind === "community" || route.kind === "category";
    const search = (
        <SearchBox
            browseKey={routeKey || "home"}
            items={allItems}
            onOpenItem={openItem}
            onOpenCategory={(c) => go(`${STORE_BASE}/category/${c}`)}
            onSearchAll={(term) => go(`${STORE_BASE}/all?q=${encodeURIComponent(term)}`)}
            placeholder={route.kind === "category" ? `Search ${categoryLabel(route.category)}` : "Search apps"}
            filter={
                browsing
                    ? { value: searchParams.get("q") ?? "", onChange: (v) => setSearchParams(withQuery(searchParams, v), { replace: true }) }
                    : undefined
            }
        />
    );

    return (
        <Page>
            <div ref={topRef} />
            <PageTopbar eyebrow="Integrations" subtitle="Connect the tools your team already uses">
                <TopbarAction href="/app/settings/oauth-apps" variant="ghost" icon={<BlocksIcon className="w-3.5 h-3.5" />}>
                    Publish an app
                </TopbarAction>
            </PageTopbar>

            <div className="flex-1 flex items-start">
                <StoreSidebar search={search} groups={navGroups} active={active} />

                <div className="flex-1 min-w-0">
                    <div className="md:hidden border-b border-slate-200/70 px-2 py-2 flex flex-col gap-2">
                        <div className="px-1">{search}</div>
                        <ScrollStrip activeKey={active} innerClassName="gap-0.5">
                            {navGroups
                                .flatMap((g) => g.items)
                                .map((it) => (
                                    <Link
                                        key={it.key}
                                        to={it.to}
                                        activeOptions={{ exact: true, includeSearch: false }}
                                        data-active={active === it.key ? "true" : undefined}
                                        className={cn(
                                            "h-8 px-2.5 rounded-md text-[12.5px] whitespace-nowrap inline-flex items-center transition-colors",
                                            active === it.key ? "bg-slate-200/70 text-slate-900 font-medium" : "text-slate-600",
                                        )}
                                    >
                                        {it.label}
                                    </Link>
                                ))}
                        </ScrollStrip>
                    </div>
                    <AnimatePresence mode="wait" initial={false}>
                        <motion.div
                            key={routeKey}
                            initial={{ opacity: 0, y: 6 }}
                            animate={{ opacity: 1, y: 0 }}
                            exit={{ opacity: 0, y: -4 }}
                            transition={{ duration: 0.16, ease: "easeOut" }}
                            className="w-full max-w-[1400px] px-4 md:px-8 py-6 flex flex-col gap-8"
                        >
                            {content}
                        </motion.div>
                    </AnimatePresence>
                </div>
            </div>

            <AnimatePresence>
                {connectTarget && (
                    <ConnectDialog
                        key={connectTarget.provider}
                        entry={connectTarget}
                        existing={connections.filter((c) => c.provider === connectTarget.provider)}
                        onClose={() => setConnectTarget(null)}
                        onConnected={() => void connectionsQuery.refetch()}
                        onSetup={(conn) => {
                            if (conn.inbound_webhook_url) {
                                setInboundUrl({ provider: conn.provider, url: conn.inbound_webhook_url });
                            } else if (CRM_HOMES.has(conn.provider)) {
                                go(`${STORE_BASE}/${conn.provider}`);
                            } else if (conn.provider === "salesforce") {
                                // Setup continues on the Salesforce page: rules, mapping, imports.
                                go(`${STORE_BASE}/salesforce/${conn.id}`);
                            } else {
                                setManageTarget(conn);
                            }
                        }}
                    />
                )}
            </AnimatePresence>
            <AnimatePresence>
                {manageTarget && (
                    <ConnectionDetail
                        key={manageTarget.id}
                        connection={manageTarget}
                        entry={entryByProvider[manageTarget.provider]}
                        onClose={() => {
                            setManageTarget(null);
                            void connectionsQuery.refetch();
                        }}
                    />
                )}
            </AnimatePresence>
            {inboundUrl && (
                <InboundUrlDialog provider={inboundUrl.provider} url={inboundUrl.url} onClose={() => setInboundUrl(null)} />
            )}
        </Page>
    );
}

// --- pieces -------------------------------------------------------------------------

function ViewHeader({ title, description }: { title: string; description?: string }) {
    return (
        <div className="min-w-0">
            <h1 className="text-[15px] font-semibold text-slate-900">{title}</h1>
            {description && <p className="mt-0.5 text-[12.5px] text-slate-500 leading-relaxed max-w-2xl">{description}</p>}
        </div>
    );
}

function Section({
    title,
    description,
    onSeeAll,
    children,
}: {
    title: string;
    description?: string;
    onSeeAll?: () => void;
    children: React.ReactNode;
}) {
    return (
        <section className="flex flex-col gap-3">
            <div className="flex items-end gap-3">
                <div className="min-w-0 flex-1">
                    <h2 className="text-[13px] font-semibold text-slate-900">{title}</h2>
                    {description && <p className="text-[12px] text-slate-500">{description}</p>}
                </div>
                {onSeeAll && (
                    <button
                        type="button"
                        onClick={onSeeAll}
                        className="shrink-0 inline-flex items-center gap-0.5 text-[12px] text-slate-500 hover:text-slate-900 transition-colors"
                    >
                        See all
                        <ChevronRightIcon className="w-3.5 h-3.5" />
                    </button>
                )}
            </div>
            {children}
        </section>
    );
}

function CardGrid({ children }: { children: React.ReactNode }) {
    return <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">{children}</div>;
}

function EmptyNote({
    title,
    body,
    action,
}: {
    title: string;
    body: string;
    action?: { label: string; onClick?: () => void; to?: string };
}) {
    const cls =
        "h-7 px-3 rounded-md border border-slate-200 text-[12px] font-medium text-slate-700 hover:border-slate-300 hover:text-slate-900 inline-flex items-center transition-colors";
    return (
        <div className="rounded-lg border border-dashed border-slate-200 px-6 py-14 text-center">
            <p className="text-[13px] font-medium text-slate-800">{title}</p>
            <p className="mt-1 text-[12.5px] text-slate-500 max-w-sm mx-auto leading-relaxed">{body}</p>
            {action && (
                <div className="mt-4">
                    {action.to ? (
                        <Link to={action.to} className={cls}>
                            {action.label}
                        </Link>
                    ) : (
                        <button type="button" onClick={action.onClick} className={cls}>
                            {action.label}
                        </button>
                    )}
                </div>
            )}
        </div>
    );
}

function GridSkeleton() {
    return (
        <CardGrid>
            {Array.from({ length: 8 }).map((_, i) => (
                <div key={i} className="rounded-lg border border-slate-200 p-4 animate-pulse">
                    <div className="flex items-center gap-3">
                        <div className="w-10 h-10 rounded-lg bg-slate-100" />
                        <div className="flex-1 space-y-1.5">
                            <div className="h-3 w-24 rounded bg-slate-100" />
                            <div className="h-2.5 w-16 rounded bg-slate-100" />
                        </div>
                    </div>
                    <div className="mt-4 h-2.5 w-full rounded bg-slate-100" />
                    <div className="mt-2 h-2.5 w-2/3 rounded bg-slate-100" />
                </div>
            ))}
        </CardGrid>
    );
}

const BUILD_TOOLS: { title: string; body: string; to: string; icon: React.ElementType }[] = [
    { title: "Publish an app", body: "Register an OAuth app any workspace can install, and share its link.", to: "/app/settings/oauth-apps", icon: BlocksIcon },
    { title: "Webhooks", body: "Send signed events, like replies, bounces and meetings, to an HTTPS endpoint you run.", to: "/app/settings/webhooks", icon: WebhookIcon },
    { title: "API keys", body: "Call the Warmbly API from your own code with a key scoped to what it needs.", to: "/app/api-keys", icon: KeyRoundIcon },
    { title: "Inbound lead webhook", body: "Give any form or tool a URL that creates contacts and starts an automation.", to: "/app/automations", icon: InboxIcon },
    { title: "MCP tools", body: "Connect an MCP server so the AI assistant can use your own tools, with your approval.", to: "/app/settings/connections", icon: BotIcon },
];

function BuildView() {
    return (
        <>
            <ViewHeader title="Build your own" description="Everything you need to connect Warmbly to a tool that isn’t listed." />
            <div className="rounded-lg border border-slate-200 divide-y divide-slate-200/70 overflow-hidden max-w-3xl">
                {BUILD_TOOLS.map((t) => (
                    <Link key={t.title} to={t.to} className="group flex items-center gap-3.5 px-4 py-3 hover:bg-slate-50 transition-colors">
                        <span className="w-8 h-8 rounded-md bg-slate-100 text-slate-500 inline-flex items-center justify-center shrink-0">
                            <t.icon className="w-4 h-4" />
                        </span>
                        <span className="min-w-0 flex-1">
                            <span className="block text-[13px] font-medium text-slate-900">{t.title}</span>
                            <span className="block text-[12px] text-slate-500 truncate">{t.body}</span>
                        </span>
                        <ChevronRightIcon className="w-4 h-4 text-slate-300 group-hover:text-slate-500 shrink-0" />
                    </Link>
                ))}
            </div>
            <a
                href="https://docs.warmbly.com/api/oauth/"
                target="_blank"
                rel="noopener noreferrer"
                className="self-start inline-flex items-center gap-1 text-[12px] text-slate-500 hover:text-sky-700"
            >
                Developer docs
                <ArrowRightIcon className="w-3 h-3" />
            </a>
        </>
    );
}
