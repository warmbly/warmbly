// An app's page: what it does, what it can touch, how it connects, and your
// connections to it. Built-in integrations connect through the existing drawers;
// community apps install on their developer's site and come back through the
// consent screen.

import React from "react";
import { Link } from "@tanstack/react-router";
import toast from "react-hot-toast/headless";
import { useQueryClient } from "@tanstack/react-query";
import { ArrowLeftIcon, CheckIcon, ExternalLinkIcon, LinkIcon, Loader2Icon, ShieldAlertIcon } from "lucide-react";

import { useConfirm } from "@/hooks/context/confirm";
import { httpUrl } from "@/lib/safeUrl";
import HelpLink from "@/components/shared/HelpLink";
import { helpLink } from "@/lib/helpLink";
import { useCommunityApp } from "@/lib/api/hooks/app/integrations/useCommunityApps";
import { useAuthorizedApps, useRevokeAuthorizedApp } from "@/lib/api/hooks/app/oauth/useAuthorizedApps";
import { communityAppPath, type CommunityApp } from "@/lib/api/models/app/integrations/Community";
import { EVENT_LABELS, type IntegrationCatalogEntry, type IntegrationConnection } from "@/lib/api/models/app/integrations/Integration";

import StatusPill from "../StatusPill";
import AppCard, { ItemLogo, ItemTag, MakerLine } from "./AppCard";
import { builtinItem, categoryLabel, communityItem, installsLabel, isUsable, STORE_BASE, type StoreItem } from "./model";

function hostOf(url: string): string {
    try {
        return new URL(url).host;
    } catch {
        return url;
    }
}

const CONNECT_METHOD: Record<string, string> = {
    oauth: "One-click sign in",
    api_key: "API key",
    webhook: "Webhook URL",
};

const primaryBtn =
    "h-8 px-3.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12.5px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:bg-slate-200 disabled:text-slate-500";
const secondaryBtn =
    "h-8 px-3 rounded-md border border-slate-200 text-[12.5px] font-medium text-slate-700 hover:border-slate-300 hover:text-slate-900 inline-flex items-center gap-1.5 transition-colors";

function DetailLayout({
    item,
    actions,
    notice,
    main,
    details,
    related,
    onOpenItem,
    onActItem,
    connections,
}: {
    item: StoreItem;
    actions: React.ReactNode;
    notice?: React.ReactNode;
    main: React.ReactNode;
    details: React.ReactNode;
    related: StoreItem[];
    onOpenItem: (item: StoreItem) => void;
    onActItem: (item: StoreItem) => void;
    connections: Partial<Record<string, IntegrationConnection>>;
}) {
    return (
        <div className="flex flex-col gap-8 max-w-5xl">
            <div className="flex flex-col gap-5">
                <Link
                    to={STORE_BASE}
                    className="self-start inline-flex items-center gap-1 text-[12px] text-slate-500 hover:text-slate-900 transition-colors"
                >
                    <ArrowLeftIcon className="w-3.5 h-3.5" />
                    Integrations
                </Link>
                <div className="flex flex-col sm:flex-row sm:items-center gap-4">
                    <ItemLogo item={item} size={12} />
                    <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-2">
                            <h1 className="text-[18px] font-semibold text-slate-900 truncate">{item.name}</h1>
                            <ItemTag item={item} />
                        </div>
                        <div className="mt-0.5 flex items-center gap-1.5 text-[12.5px] text-slate-400">
                            <MakerLine item={item} className="text-[12.5px]" />
                            <span>·</span>
                            <span>{categoryLabel(item.category)}</span>
                        </div>
                    </div>
                    <div className="flex items-center gap-2">{actions}</div>
                </div>
                <p className="text-[13.5px] text-slate-600 leading-relaxed max-w-2xl">{item.tagline}</p>
                {notice}
            </div>

            <div className="grid lg:grid-cols-[minmax(0,1fr)_280px] gap-8 items-start">
                <div className="flex flex-col gap-7 min-w-0">{main}</div>
                <div className="rounded-lg border border-slate-200 divide-y divide-slate-200/70 text-[12.5px]">{details}</div>
            </div>

            {related.length > 0 && (
                <Section title={`More in ${categoryLabel(item.category)}`}>
                    <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
                        {related.slice(0, 3).map((r) => (
                            <AppCard
                                key={r.key}
                                item={r}
                                connection={r.kind === "builtin" ? connections[r.entry.provider] : undefined}
                                onOpen={() => onOpenItem(r)}
                                onAction={() => onActItem(r)}
                            />
                        ))}
                    </div>
                </Section>
            )}
        </div>
    );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
    return (
        <section className="flex flex-col gap-2.5">
            <h2 className="text-[13px] font-semibold text-slate-900">{title}</h2>
            {children}
        </section>
    );
}

function DetailRow({ label, children }: { label: string; children: React.ReactNode }) {
    return (
        <div className="flex items-center justify-between gap-3 px-3.5 py-2.5">
            <span className="text-slate-500 shrink-0">{label}</span>
            <span className="text-slate-900 text-right min-w-0 truncate">{children}</span>
        </div>
    );
}

function DetailLink({ href, children }: { href: string; children: React.ReactNode }) {
    return (
        <a
            href={href}
            target="_blank"
            rel="noopener noreferrer"
            className="flex items-center justify-between gap-2 px-3.5 py-2.5 text-slate-700 hover:text-sky-700 transition-colors"
        >
            <span className="truncate">{children}</span>
            <ExternalLinkIcon className="w-3.5 h-3.5 shrink-0 text-slate-400" />
        </a>
    );
}

function Checklist({ items }: { items: string[] }) {
    return (
        <ul className="flex flex-col gap-2">
            {items.map((h) => (
                <li key={h} className="flex items-start gap-2 text-[13px] text-slate-700 leading-relaxed">
                    <CheckIcon className="w-3.5 h-3.5 text-emerald-600 mt-[3px] shrink-0" />
                    <span>{h}</span>
                </li>
            ))}
        </ul>
    );
}

// --- built-in integrations ---------------------------------------------------------

export function BuiltinDetail({
    entry,
    connections,
    allConnections,
    related,
    onConnect,
    onManage,
    onOpenItem,
    onActItem,
}: {
    entry: IntegrationCatalogEntry;
    connections: IntegrationConnection[];
    allConnections: Partial<Record<string, IntegrationConnection>>;
    related: StoreItem[];
    onConnect: () => void;
    onManage: (c: IntegrationConnection) => void;
    onOpenItem: (item: StoreItem) => void;
    onActItem: (item: StoreItem) => void;
}) {
    const item = builtinItem(entry);
    const usable = isUsable(entry);
    const events = (entry.events ?? []).map((e) => EVENT_LABELS[e] ?? e);
    const actions = entry.capability?.actions ?? [];

    return (
        <DetailLayout
            item={item}
            connections={allConnections}
            actions={
                <>
                    {entry.docs_url && (
                        <HelpLink
                            href={entry.docs_url}
                            className={secondaryBtn}
                            portalLabel={<>Get help <ExternalLinkIcon className="w-3.5 h-3.5 text-slate-400" /></>}
                        >
                            Docs
                            <ExternalLinkIcon className="w-3.5 h-3.5 text-slate-400" />
                        </HelpLink>
                    )}
                    {connections.length > 0 ? (
                        <button type="button" onClick={() => onManage(connections[0])} className={secondaryBtn}>
                            Manage
                        </button>
                    ) : (
                        <button type="button" onClick={onConnect} disabled={!usable} className={primaryBtn}>
                            {usable ? `Connect ${entry.name}` : "Coming soon"}
                        </button>
                    )}
                </>
            }
            notice={
                !usable && connections.length === 0 ? (
                    <p className="text-[12.5px] text-slate-500">
                        {entry.name} sign-in isn’t switched on for this instance yet. An administrator adds the app credentials.
                    </p>
                ) : null
            }
            main={
                <>
                    {entry.highlights && entry.highlights.length > 0 && (
                        <Section title="What you can do">
                            <Checklist items={entry.highlights} />
                        </Section>
                    )}
                    {actions.length > 0 && (
                        <Section title="Actions">
                            <div className="flex flex-col gap-2">
                                {actions.map((a) => (
                                    <div key={a.id}>
                                        <div className="text-[13px] font-medium text-slate-800">{a.label}</div>
                                        {a.description && <p className="text-[12.5px] text-slate-500 leading-relaxed">{a.description}</p>}
                                    </div>
                                ))}
                            </div>
                        </Section>
                    )}
                    {events.length > 0 && (
                        <Section title="Triggers">
                            <p className="text-[12.5px] text-slate-500 -mt-1">An automation can run it when any of these happen.</p>
                            <div className="flex flex-wrap gap-1.5">
                                {events.map((e) => (
                                    <span key={e} className="h-6 px-2 rounded-md bg-slate-100 text-[12px] text-slate-600 inline-flex items-center">
                                        {e}
                                    </span>
                                ))}
                            </div>
                        </Section>
                    )}
                    {entry.auth_method === "oauth" && entry.scopes && entry.scopes.length > 0 && (
                        <Section title="Access it asks for">
                            <div className="flex flex-wrap gap-1.5">
                                {entry.scopes.map((s) => (
                                    <code key={s} className="h-6 px-2 rounded-md bg-slate-100 text-[11.5px] text-slate-600 font-mono inline-flex items-center">
                                        {s}
                                    </code>
                                ))}
                            </div>
                            <p className="text-[12px] text-slate-500">Tokens are encrypted with your workspace key.</p>
                        </Section>
                    )}
                </>
            }
            details={
                <>
                    {connections.map((c) => (
                        <button
                            key={c.id}
                            type="button"
                            onClick={() => onManage(c)}
                            className="w-full flex items-center justify-between gap-3 px-3.5 py-2.5 text-left hover:bg-slate-50 transition-colors"
                        >
                            <span className="min-w-0 truncate text-slate-900">{c.external_account_name || c.label || entry.name}</span>
                            <StatusPill status={c.status} />
                        </button>
                    ))}
                    {connections.length > 0 && usable && (
                        <button
                            type="button"
                            onClick={onConnect}
                            className="w-full px-3.5 py-2.5 text-left text-sky-700 hover:bg-slate-50 transition-colors"
                        >
                            Connect another account
                        </button>
                    )}
                    <DetailRow label="Made by">Warmbly</DetailRow>
                    <DetailRow label="Category">{categoryLabel(entry.category)}</DetailRow>
                    <DetailRow label="Connects with">{CONNECT_METHOD[entry.auth_method] ?? entry.auth_method}</DetailRow>
                    {entry.docs_url && helpLink(entry.docs_url) === entry.docs_url && <DetailLink href={entry.docs_url}>Developer docs</DetailLink>}
                </>
            }
            related={related}
            onOpenItem={onOpenItem}
            onActItem={onActItem}
        />
    );
}

// --- community apps ---------------------------------------------------------------

export function CommunityDetail({
    slug,
    preview,
    allConnections,
    related,
    onOpenItem,
    onActItem,
}: {
    slug: string;
    preview?: CommunityApp;
    allConnections: Partial<Record<string, IntegrationConnection>>;
    related: (app: CommunityApp) => StoreItem[];
    onOpenItem: (item: StoreItem) => void;
    onActItem: (item: StoreItem) => void;
}) {
    const query = useCommunityApp(slug);
    const app = query.data ?? preview;
    const authorized = useAuthorizedApps();
    const revoke = useRevokeAuthorizedApp();
    const confirm = useConfirm();
    const qc = useQueryClient();

    if (!app) {
        return query.isError ? (
            <div className="py-20 text-center">
                <p className="text-[13px] font-medium text-slate-800">This app isn’t available</p>
                <p className="mt-1 text-[12.5px] text-slate-500">Its developer may have unpublished it, or the link is wrong.</p>
                <Link to={STORE_BASE} className="mt-3 inline-block text-[12.5px] text-sky-700 hover:text-sky-800">
                    Back to Integrations
                </Link>
            </div>
        ) : (
            <div className="flex justify-center py-20">
                <Loader2Icon className="w-4 h-4 animate-spin text-slate-400" />
            </div>
        );
    }

    const item = communityItem(app);
    const mine = (authorized.data?.authorized_apps ?? []).some((a) => a.application_id === app.application_id);
    const permissions = [...app.permissions.filter((p) => p.category !== "read"), ...app.permissions.filter((p) => p.category === "read")];

    function install() {
        // A listing is a third party's input: only an http(s) address is opened.
        const url = httpUrl(app!.install_url);
        if (url) window.open(url, "_blank", "noopener,noreferrer");
    }

    function copyLink() {
        navigator.clipboard
            .writeText(`${window.location.origin}${communityAppPath(slug)}`)
            .then(() => toast.success("Link copied"))
            .catch(() => toast.error("Could not copy the link"));
    }

    const notice =
        app.status === "featured" ? (
            <p className="text-[12.5px] text-slate-500">Featured by us. Built and run by {app.developer || "its developer"}, not by Warmbly.</p>
        ) : app.listed ? (
            <p className="text-[12.5px] text-slate-500">
                Listed because {installsLabel(app.installs)} use it. We haven’t reviewed it, so check what it asks for.
            </p>
        ) : (
            <div className="flex items-start gap-2 rounded-md bg-amber-50 px-3 py-2 text-[12.5px] text-amber-900 max-w-2xl">
                <ShieldAlertIcon className="w-3.5 h-3.5 text-amber-600 mt-0.5 shrink-0" />
                <span>
                    Shared by link. This app isn’t listed and we haven’t reviewed it. Install it only if you trust{" "}
                    {app.developer || "its developer"}.
                </span>
            </div>
        );

    return (
        <DetailLayout
            item={item}
            connections={allConnections}
            actions={
                <>
                    <button type="button" onClick={copyLink} aria-label="Copy link" title="Copy link" className={secondaryBtn}>
                        <LinkIcon className="w-3.5 h-3.5" />
                    </button>
                    {mine ? (
                        <button
                            type="button"
                            onClick={() =>
                                confirm.show(`Remove ${app.name}’s access? Its tokens stop working right away.`, async () => {
                                    await revoke.mutateAsync(app.application_id);
                                    void qc.invalidateQueries({ queryKey: ["integrations", "community"] });
                                    toast.success(`${app.name} disconnected`);
                                })
                            }
                            className={secondaryBtn}
                        >
                            Disconnect
                        </button>
                    ) : null}
                    <button type="button" onClick={install} className={app.installed ? secondaryBtn : primaryBtn}>
                        {app.installed ? "Open app" : "Install"}
                        <ExternalLinkIcon className="w-3.5 h-3.5 opacity-70" />
                    </button>
                </>
            }
            notice={notice}
            main={
                <>
                    {app.description && (
                        <Section title="About">
                            <p className="text-[13px] text-slate-700 leading-relaxed whitespace-pre-line break-words max-w-2xl">{app.description}</p>
                        </Section>
                    )}
                    <Section title="What it can ask for">
                        {permissions.length === 0 ? (
                            <p className="text-[12.5px] text-slate-500">No permissions.</p>
                        ) : (
                            <ul className="flex flex-col gap-1.5">
                                {permissions.map((p) => (
                                    <li key={p.name} className="flex items-baseline gap-2 text-[13px] text-slate-700">
                                        <span className={p.category === "read" ? "text-slate-400" : "text-amber-600"}>•</span>
                                        <span>{p.description}</span>
                                        <code className="text-[11px] text-slate-400 font-mono">{p.name.toLowerCase()}</code>
                                    </li>
                                ))}
                            </ul>
                        )}
                        <p className="text-[12px] text-slate-500">
                            Install opens {hostOf(app.install_url)}, then brings you back here to approve exactly this. You can remove its access at any time.
                        </p>
                    </Section>
                </>
            }
            details={
                <>
                    {app.installed && (
                        <div className="flex items-center gap-1.5 px-3.5 py-2.5 text-emerald-700">
                            <CheckIcon className="w-3.5 h-3.5" />
                            Installed in this workspace
                        </div>
                    )}
                    <DetailRow label="Made by">{app.developer || "Unknown"}</DetailRow>
                    <DetailRow label="Category">{categoryLabel(app.category)}</DetailRow>
                    <DetailRow label="Used by">{installsLabel(app.installs)}</DetailRow>
                    {app.website_url && <DetailLink href={app.website_url}>{hostOf(app.website_url)}</DetailLink>}
                    {app.support_url && <DetailLink href={app.support_url}>Support</DetailLink>}
                    {app.privacy_url && <DetailLink href={app.privacy_url}>Privacy policy</DetailLink>}
                    {app.installed && !mine && (
                        <Link to="/app/settings/oauth-apps" className="block px-3.5 py-2.5 text-sky-700 hover:bg-slate-50 transition-colors">
                            Manage access
                        </Link>
                    )}
                </>
            }
            related={related(app)}
            onOpenItem={onOpenItem}
            onActItem={onActItem}
        />
    );
}
