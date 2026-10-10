// Cross-org mailbox browser. Searchable by identity, owner/org/worker
// placement, provider, warmup, risk band, pool, sync state, credentials,
// numeric ranges, and timeline. The ?org=, ?user=, ?worker= query params
// (linked from other explorers) scope the view; cursor-paged + CSV + sortable.

import { useEffect, useState } from "react";
import { useQuery, keepPreviousData } from "@tanstack/react-query";
import { Link, useSearchParams } from "react-router-dom";
import { AlertTriangle, Building2, User, X } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { Button } from "@/components/ui/button";
import { StatusBadge, StatusDot } from "@/components/ui/kit";
import {
    Explorer,
    FilterGroup,
    SearchFilter,
    SegmentedFilter,
    SelectFilter,
    ToggleFilter,
    DateRangeFilter,
    NumberRangeFilter,
} from "@/components/data/Explorer";
import { DataTable, type Column } from "@/components/data/DataTable";
import { useCursorPager } from "@/lib/useCursorPager";
import { emptyRange, rangeActive, rangeWithin, rangeAfter, rangeBefore, type DateRange } from "@/lib/dateRange";
import { searchMailboxes } from "@/lib/api/client/admin/mailboxes";
import { listFleetNodes, type FleetNode } from "@/lib/api/client/admin/fleetNodes";
import type { AdminMailboxRow } from "@/lib/api/models/admin";
import { TONE_TEXT, type Tone } from "@/lib/tones";
import { cn } from "@/lib/utils";

type StatusFilter = "active" | "inactive" | "all";
type WarmupFilter = "all" | "on" | "off";
type SyncedFilter = "" | "never" | "stale" | "recent";
const STALE_MS = 24 * 60 * 60 * 1000;

const PROVIDER_OPTIONS = [
    { value: "any", label: "Any provider" },
    { value: "gmail", label: "Gmail" },
    { value: "outlook", label: "Outlook" },
    { value: "smtp_imap", label: "SMTP / IMAP" },
];

const RISK_OPTIONS = [
    { value: "any", label: "Any risk band" },
    { value: "clean", label: "Clean" },
    { value: "risky", label: "Risky" },
    { value: "quarantine", label: "Quarantine" },
];

const POOL_OPTIONS = [
    { value: "any", label: "Any pool" },
    { value: "free", label: "Free pool" },
    { value: "premium", label: "Premium pool" },
];

const PROVIDER_LABEL: Record<string, string> = {
    gmail: "Gmail",
    outlook: "Outlook",
    smtp_imap: "SMTP / IMAP",
};

const RISK_TONE: Record<string, Tone> = {
    clean: "success",
    risky: "warning",
    quarantine: "danger",
};

const cap = (s: string) => (s ? s.charAt(0).toUpperCase() + s.slice(1) : s);

const linkCls = "truncate text-foreground decoration-border-strong underline-offset-2 hover:text-[var(--admin-accent-strong)] hover:underline";

const columns: Column<AdminMailboxRow>[] = [
    {
        id: "email",
        header: "Mailbox",
        sortable: true,
        sortKey: "email",
        cell: (m) => (
            <div className="min-w-0 py-1">
                <div className="truncate font-medium text-foreground">{m.email}</div>
                <div className="text-xs text-muted-foreground">{PROVIDER_LABEL[m.provider] ?? m.provider}</div>
            </div>
        ),
        csv: (m) => m.email,
    },
    {
        id: "owner",
        header: "Owner",
        cell: (m) => (
            <Link to={`/users/${m.user_id}`} className={linkCls} onClick={(e) => e.stopPropagation()}>
                {m.owner_email}
            </Link>
        ),
        csv: (m) => m.owner_email,
    },
    {
        id: "org",
        header: "Organization",
        cell: (m) =>
            m.organization_id ? (
                <Link to={`/organizations/${m.organization_id}`} className={linkCls} onClick={(e) => e.stopPropagation()}>
                    {m.org_name || m.organization_id}
                </Link>
            ) : (
                <span className="text-subtle-foreground">No org</span>
            ),
        csv: (m) => m.org_name || "",
    },
    {
        id: "status",
        header: "Status",
        cell: (m) =>
            m.status === "active" ? (
                <StatusDot tone="success" className="text-muted-foreground">Active</StatusDot>
            ) : (
                <StatusBadge tone="neutral">{cap(m.status)}</StatusBadge>
            ),
        csv: (m) => m.status,
    },
    {
        id: "risk",
        header: "Risk",
        cell: (m) => (
            <StatusBadge tone={RISK_TONE[m.risk_band] ?? "neutral"} dot>
                {cap(m.risk_band)}
            </StatusBadge>
        ),
        csv: (m) => m.risk_band,
    },
    {
        id: "warmup",
        header: "Warmup",
        cell: (m) =>
            m.warmup_enabled ? (
                <StatusBadge tone="accent">{m.warmup_pool_type ? `On · ${m.warmup_pool_type}` : "On"}</StatusBadge>
            ) : (
                <span className="text-subtle-foreground">Off</span>
            ),
        csv: (m) => (m.warmup_enabled ? "on" : "off"),
    },
    { id: "limit", header: "Daily cap", align: "right", sortable: true, sortKey: "campaign_limit", cell: (m) => <span className="tabular-nums">{m.campaign_limit}</span>, csv: (m) => m.campaign_limit },
    {
        id: "synced",
        header: "Last synced",
        sortable: true,
        sortKey: "last_synced_at",
        cell: (m) => {
            if (!m.last_synced_at) return <span className="text-subtle-foreground">Never</span>;
            const stale = Date.now() - new Date(m.last_synced_at).getTime() > STALE_MS;
            return (
                <span
                    className={cn("inline-flex items-center gap-1 whitespace-nowrap tabular-nums", stale ? TONE_TEXT.warning : "text-muted-foreground")}
                    title={stale ? "Not synced in the last 24 hours" : undefined}
                >
                    {stale && <AlertTriangle className="size-3.5" />}
                    {new Date(m.last_synced_at).toLocaleString()}
                </span>
            );
        },
        csv: (m) => m.last_synced_at || "",
    },
    {
        id: "created",
        header: "Connected",
        sortable: true,
        sortKey: "created_at",
        cell: (m) => <span className="whitespace-nowrap tabular-nums text-muted-foreground">{new Date(m.created_at).toLocaleDateString()}</span>,
        csv: (m) => m.created_at,
        defaultHidden: true,
    },
];

export default function MailboxesPage() {
    const [params, setParams] = useSearchParams();
    const orgId = params.get("org") || undefined;
    const userId = params.get("user") || undefined;
    const workerParam = params.get("worker") || "";
    const mailboxId = params.get("mailbox_id") || undefined;

    // `?q=` seeds the search box so the command palette can land here on a
    // mailbox; a status of "all" keeps a disabled mailbox findable that way.
    const [query, setQuery] = useState(params.get("q") ?? "");
    const [status, setStatus] = useState<StatusFilter>(params.get("q") ? "all" : "active");
    const [provider, setProvider] = useState("");
    const [warmup, setWarmup] = useState<WarmupFilter>("all");
    const [workerId, setWorkerId] = useState(workerParam);
    const [riskBand, setRiskBand] = useState("");
    const [pool, setPool] = useState("");
    const [synced, setSynced] = useState<SyncedFilter>("");
    const [warmupPaused, setWarmupPaused] = useState(false);
    const [trackingVerified, setTrackingVerified] = useState(false);
    const [hasTrackingDomain, setHasTrackingDomain] = useState(false);
    const [hasOrg, setHasOrg] = useState(false);
    const [signatureSync, setSignatureSync] = useState(false);
    const [hasOAuth, setHasOAuth] = useState(false);
    const [hasSmtp, setHasSmtp] = useState(false);
    const [capMin, setCapMin] = useState<number | undefined>();
    const [capMax, setCapMax] = useState<number | undefined>();
    const [gapMin, setGapMin] = useState<number | undefined>();
    const [gapMax, setGapMax] = useState<number | undefined>();
    const [connected, setConnected] = useState<DateRange>(emptyRange);
    const [lastSynced, setLastSynced] = useState<DateRange>(emptyRange);
    const [sort, setSort] = useState<{ by: string; desc: boolean }>({ by: "", desc: true });
    const pager = useCursorPager();
    const { reset } = pager;

    // Keep the worker select in sync if arrived via ?worker=.
    useEffect(() => {
        setWorkerId(workerParam);
    }, [workerParam]);

    // The page stays mounted when the palette lands here again with another
    // ?q=, so the search box follows the URL rather than only its first value.
    const qParam = params.get("q") ?? "";
    useEffect(() => {
        setQuery(qParam);
        if (qParam) setStatus("all");
    }, [qParam]);

    const { data: workersData } = useQuery({ queryKey: ["admin", "workers", "managed"], queryFn: () => listFleetNodes("worker"), staleTime: 60_000 });
    const workerOptions = [
        { value: "any", label: "Any worker" },
        ...(workersData?.data ?? []).map((w) => ({ value: w.id, label: w.name || w.id.slice(0, 8) })),
    ];

    const filterKey = JSON.stringify({
        query, status, provider, warmup, workerId, riskBand, pool, synced, orgId, userId, mailboxId,
        warmupPaused, trackingVerified, hasTrackingDomain, hasOrg, signatureSync, hasOAuth, hasSmtp,
        capMin, capMax, gapMin, gapMax, connected, lastSynced, sort,
    });

    useEffect(() => {
        reset();
    }, [filterKey, reset]);

    const { data, isLoading, error, refetch } = useQuery({
        queryKey: ["admin", "mailboxes", filterKey, pager.cursor],
        queryFn: () =>
            searchMailboxes(mailboxId ? { mailbox_id: mailboxId, status: "all", limit: 50 } : {
                q: query.trim() || undefined,
                status,
                provider: provider || undefined,
                warmup: warmup === "all" ? undefined : warmup,
                worker_id: workerId || undefined,
                org_id: orgId,
                user_id: userId,
                risk_band: riskBand || undefined,
                warmup_pool_type: pool || undefined,
                synced_status: synced || undefined,
                warmup_paused: warmupPaused || undefined,
                tracking_domain_verified: trackingVerified || undefined,
                has_tracking_domain: hasTrackingDomain || undefined,
                has_organization: hasOrg || undefined,
                signature_sync: signatureSync || undefined,
                has_oauth: hasOAuth || undefined,
                has_smtp_imap: hasSmtp || undefined,
                campaign_limit_min: capMin,
                campaign_limit_max: capMax,
                min_wait_time_min: gapMin,
                min_wait_time_max: gapMax,
                created_within: rangeWithin(connected),
                created_after: rangeAfter(connected),
                created_before: rangeBefore(connected),
                last_synced_after: rangeAfter(lastSynced),
                last_synced_before: rangeBefore(lastSynced),
                cursor: pager.cursor,
                limit: 50,
                sort_by: sort.by ? (sort.by as "email" | "created_at" | "last_synced_at" | "campaign_limit") : undefined,
                sort_desc: sort.by ? sort.desc : undefined,
            }),
        staleTime: 30_000,
        placeholderData: mailboxId ? undefined : keepPreviousData,
    });

    const rows = data?.data ?? [];

    function clearParam(key: string) {
        const next = new URLSearchParams(params);
        next.delete(key);
        setParams(next, { replace: true });
    }

    const bools = [warmupPaused, trackingVerified, hasTrackingDomain, hasOrg, signatureSync, hasOAuth, hasSmtp];
    const ranges = [[capMin, capMax], [gapMin, gapMax]];
    const activeCount =
        (query ? 1 : 0) +
        (status !== "active" ? 1 : 0) +
        (provider ? 1 : 0) +
        (warmup !== "all" ? 1 : 0) +
        (workerId ? 1 : 0) +
        (riskBand ? 1 : 0) +
        (pool ? 1 : 0) +
        (synced ? 1 : 0) +
        (orgId ? 1 : 0) +
        (userId ? 1 : 0) +
        bools.filter(Boolean).length +
        ranges.filter(([a, b]) => a !== undefined || b !== undefined).length +
        [connected, lastSynced].filter(rangeActive).length +
        (sort.by ? 1 : 0);

    function resetAll() {
        setQuery("");
        setStatus("active");
        setProvider("");
        setWarmup("all");
        setWorkerId("");
        setRiskBand("");
        setPool("");
        setSynced("");
        setWarmupPaused(false);
        setTrackingVerified(false);
        setHasTrackingDomain(false);
        setHasOrg(false);
        setSignatureSync(false);
        setHasOAuth(false);
        setHasSmtp(false);
        setCapMin(undefined);
        setCapMax(undefined);
        setGapMin(undefined);
        setGapMax(undefined);
        setConnected(emptyRange);
        setLastSynced(emptyRange);
        setSort({ by: "", desc: true });
        const next = new URLSearchParams(params);
        next.delete("org");
        next.delete("user");
        next.delete("worker");
        next.delete("q");
        setParams(next, { replace: true });
    }

    if (mailboxId) return <div>
        <PageHeader title="Mailbox" description="Exact identity lookup across current assignments, including inactive mailboxes. Existing browse filters do not apply." />
        <p className="mb-4 break-all font-mono text-xs">{mailboxId}</p>
        <Button variant="outline" className="mb-4" onClick={() => clearParam("mailbox_id")}>Return to mailbox browser</Button>
        <DataTable columns={columns} rows={rows} getRowId={(m) => m.id} loading={isLoading} error={error}
            onRetry={() => refetch()} errorTitle="Mailbox lookup failed" noun="mailboxes"
            emptyTitle="Mailbox not found" emptyHint="This mailbox is no longer registered on this instance." />
    </div>;

    return (
        <div>
            <PageHeader
                title="Mailboxes"
                meta={
                    data?.pagination.total != null ? (
                        <span className="text-[13px] tabular-nums text-subtle-foreground">{data.pagination.total.toLocaleString()}</span>
                    ) : undefined
                }
                description="Every connected mailbox across the platform. Filter by owner, org, worker, provider, warmup, risk, pool, credentials, limits, and timeline; scope to one entity and export."
            />
            <Explorer
                activeCount={activeCount}
                onReset={resetAll}
                filters={
                    <>
                        <FilterGroup label="Search">
                            <SearchFilter value={query} onChange={setQuery} placeholder="Email, owner, or org…" />
                        </FilterGroup>
                        {(orgId || userId) && (
                            <FilterGroup label="Scope">
                                <div className="flex flex-col gap-1">
                                    {orgId && <ScopeChip icon={Building2} label="One organization" title="Clear org filter" onClear={() => clearParam("org")} />}
                                    {userId && <ScopeChip icon={User} label="One user" title="Clear user filter" onClear={() => clearParam("user")} />}
                                </div>
                            </FilterGroup>
                        )}
                        <FilterGroup label="Status">
                            <SegmentedFilter
                                value={status}
                                onChange={setStatus}
                                options={[
                                    { value: "active", label: "Active" },
                                    { value: "inactive", label: "Inactive" },
                                    { value: "all", label: "All" },
                                ]}
                            />
                        </FilterGroup>
                        <FilterGroup label="Provider">
                            <SelectFilter value={provider || "any"} onChange={(v) => setProvider(v === "any" ? "" : v)} options={PROVIDER_OPTIONS} placeholder="Any provider" />
                        </FilterGroup>
                        <FilterGroup label="Worker">
                            <SelectFilter value={workerId || "any"} onChange={(v) => setWorkerId(v === "any" ? "" : v)} options={workerOptions} placeholder="Any worker" />
                        </FilterGroup>
                        <FilterGroup label="Warmup">
                            <SegmentedFilter
                                value={warmup}
                                onChange={setWarmup}
                                options={[
                                    { value: "all", label: "All" },
                                    { value: "on", label: "On" },
                                    { value: "off", label: "Off" },
                                ]}
                            />
                            <div className="mt-2 flex flex-col gap-2">
                                <ToggleFilter checked={warmupPaused} onChange={setWarmupPaused} label="Warmup paused" />
                            </div>
                        </FilterGroup>
                        <FilterGroup label="Risk band">
                            <SelectFilter value={riskBand || "any"} onChange={(v) => setRiskBand(v === "any" ? "" : v)} options={RISK_OPTIONS} placeholder="Any risk band" />
                        </FilterGroup>
                        <FilterGroup label="Warmup pool">
                            <SelectFilter value={pool || "any"} onChange={(v) => setPool(v === "any" ? "" : v)} options={POOL_OPTIONS} placeholder="Any pool" />
                        </FilterGroup>
                        <FilterGroup label="Sync state">
                            <SegmentedFilter
                                value={synced || "any"}
                                onChange={(v) => setSynced(v === "any" ? "" : (v as SyncedFilter))}
                                options={[
                                    { value: "any", label: "Any" },
                                    { value: "recent", label: "Recent" },
                                    { value: "stale", label: "Stale" },
                                ]}
                            />
                            <div className="mt-1.5">
                                <ToggleFilter checked={synced === "never"} onChange={(c) => setSynced(c ? "never" : "")} label="Never synced" />
                            </div>
                        </FilterGroup>
                        <FilterGroup label="Connected">
                            <DateRangeFilter value={connected} onChange={setConnected} />
                        </FilterGroup>
                        <FilterGroup label="Credentials & flags">
                            <div className="flex flex-col gap-2">
                                <ToggleFilter checked={hasOAuth} onChange={setHasOAuth} label="OAuth connected" />
                                <ToggleFilter checked={hasSmtp} onChange={setHasSmtp} label="SMTP / IMAP connected" />
                                <ToggleFilter checked={hasOrg} onChange={setHasOrg} label="Assigned to an org" />
                                <ToggleFilter checked={hasTrackingDomain} onChange={setHasTrackingDomain} label="Has tracking domain" />
                                <ToggleFilter checked={trackingVerified} onChange={setTrackingVerified} label="Tracking domain verified" />
                                <ToggleFilter checked={signatureSync} onChange={setSignatureSync} label="Signature sync on" />
                            </div>
                        </FilterGroup>
                        <FilterGroup label="Daily cap">
                            <NumberRangeFilter min={capMin} max={capMax} onMinChange={setCapMin} onMaxChange={setCapMax} />
                        </FilterGroup>
                        <FilterGroup label="Min gap (seconds)">
                            <NumberRangeFilter min={gapMin} max={gapMax} onMinChange={setGapMin} onMaxChange={setGapMax} />
                        </FilterGroup>
                        <FilterGroup label="Last synced">
                            <DateRangeFilter value={lastSynced} onChange={setLastSynced} mode="custom" />
                        </FilterGroup>
                    </>
                }
            >
                <DataTable
                    columns={columns}
                    rows={rows}
                    getRowId={(m) => m.id}
                    loading={isLoading}
                    error={error}
                    onRetry={() => refetch()}
                    errorTitle="Failed to load mailboxes"
                    sort={sort.by ? sort : undefined}
                    onSortChange={setSort}
                    storageKey="admin.mailboxes"
                    csvName="warmbly-mailboxes"
                    noun="mailboxes"
                    emptyTitle="No mailboxes"
                    emptyHint="No mailboxes match these filters."
                    pager={{
                        canPrev: pager.canPrev,
                        canNext: !!data?.pagination.has_more,
                        onPrev: pager.prev,
                        onNext: () => pager.next(data?.pagination.next_cursor),
                        page: pager.page,
                        shown: rows.length,
                        total: data?.pagination.total ?? null,
                    }}
                />
            </Explorer>
        </div>
    );
}

function ScopeChip({ icon: Icon, label, title, onClear }: { icon: LucideIcon; label: string; title: string; onClear: () => void }) {
    return (
        <div className="flex h-7 items-center gap-1.5 rounded-md border border-[color-mix(in_oklab,var(--admin-accent)_30%,transparent)] bg-[var(--admin-accent-weak)] pr-0.5 pl-2 text-[12.5px] text-[var(--admin-accent-strong)]">
            <Icon className="size-3.5 shrink-0" />
            <span className="min-w-0 flex-1 truncate font-medium">{label}</span>
            <Button variant="ghost" size="icon-xs" onClick={onClear} title={title} className="text-[var(--admin-accent-strong)] hover:bg-[var(--admin-accent-soft)]">
                <X className="size-3" />
            </Button>
        </div>
    );
}
