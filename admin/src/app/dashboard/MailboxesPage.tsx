// Cross-org mailbox browser. Searchable by identity, owner/org/worker
// placement, provider, warmup, risk band, pool, sync state, credentials,
// numeric ranges, and timeline. The ?org=, ?user=, ?worker= query params
// (linked from other explorers) scope the view; cursor-paged + CSV + sortable.

import { useEffect, useState } from "react";
import { useQuery, keepPreviousData } from "@tanstack/react-query";
import { Link, useSearchParams } from "react-router-dom";
import { X } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
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
import { listManagedWorkers } from "@/lib/api/client/admin/workers";
import type { AdminMailboxRow } from "@/lib/api/models/admin";

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

const RISK_TONE: Record<string, string> = {
    clean: "border-emerald-300 bg-emerald-50 text-emerald-700",
    risky: "border-amber-300 bg-amber-50 text-amber-700",
    quarantine: "border-red-300 bg-red-50 text-red-700",
};

const columns: Column<AdminMailboxRow>[] = [
    {
        id: "email",
        header: "Mailbox",
        sortable: true,
        sortKey: "email",
        cell: (m) => (
            <div>
                <div className="font-medium">{m.email}</div>
                <div className="text-[10px] uppercase tracking-wide text-muted-foreground">{m.provider}</div>
            </div>
        ),
        csv: (m) => m.email,
    },
    {
        id: "owner",
        header: "Owner",
        cell: (m) => (
            <Link to={`/users/${m.user_id}`} className="text-xs text-[var(--admin-accent-strong)] hover:underline">
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
                <Link to={`/organizations/${m.organization_id}`} className="text-xs text-[var(--admin-accent-strong)] hover:underline">
                    {m.org_name || m.organization_id}
                </Link>
            ) : (
                <span className="text-xs text-muted-foreground">—</span>
            ),
        csv: (m) => m.org_name || "",
    },
    {
        id: "status",
        header: "Status",
        cell: (m) =>
            m.status === "active" ? (
                <span className="text-xs text-emerald-600">active</span>
            ) : (
                <Badge variant="outline" className="text-[10px] border-zinc-300 text-zinc-600">
                    {m.status}
                </Badge>
            ),
        csv: (m) => m.status,
    },
    {
        id: "risk",
        header: "Risk",
        cell: (m) => (
            <Badge variant="outline" className={`text-[10px] ${RISK_TONE[m.risk_band] ?? "border-zinc-300 text-zinc-600"}`}>
                {m.risk_band}
            </Badge>
        ),
        csv: (m) => m.risk_band,
    },
    {
        id: "warmup",
        header: "Warmup",
        cell: (m) =>
            m.warmup_enabled ? (
                <Badge variant="outline" className="text-[10px] border-amber-300 bg-amber-50 text-amber-700">
                    {m.warmup_pool_type ? `on · ${m.warmup_pool_type}` : "on"}
                </Badge>
            ) : (
                <span className="text-xs text-muted-foreground">off</span>
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
            if (!m.last_synced_at) return <span className="text-xs text-muted-foreground">never</span>;
            const stale = Date.now() - new Date(m.last_synced_at).getTime() > STALE_MS;
            return (
                <span className={stale ? "text-xs text-amber-700" : "text-xs text-muted-foreground"}>
                    {new Date(m.last_synced_at).toLocaleString()}
                    {stale && " ⚠"}
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
        cell: (m) => <span className="text-xs text-muted-foreground">{new Date(m.created_at).toLocaleDateString()}</span>,
        csv: (m) => m.created_at,
        defaultHidden: true,
    },
];

export default function MailboxesPage() {
    const [params, setParams] = useSearchParams();
    const orgId = params.get("org") || undefined;
    const userId = params.get("user") || undefined;
    const workerParam = params.get("worker") || "";

    const [query, setQuery] = useState("");
    const [status, setStatus] = useState<StatusFilter>("active");
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

    const { data: workersData } = useQuery({ queryKey: ["admin", "workers", "managed"], queryFn: listManagedWorkers, staleTime: 60_000 });
    const workerOptions = [
        { value: "any", label: "Any worker" },
        ...(workersData?.data ?? []).map((w) => ({ value: w.id, label: w.name || w.id.slice(0, 8) })),
    ];

    const filterKey = JSON.stringify({
        query, status, provider, warmup, workerId, riskBand, pool, synced, orgId, userId,
        warmupPaused, trackingVerified, hasTrackingDomain, hasOrg, signatureSync, hasOAuth, hasSmtp,
        capMin, capMax, gapMin, gapMax, connected, lastSynced, sort,
    });

    useEffect(() => {
        reset();
    }, [filterKey, reset]);

    const { data, isLoading, error, refetch } = useQuery({
        queryKey: ["admin", "mailboxes", filterKey, pager.cursor],
        queryFn: () =>
            searchMailboxes({
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
        placeholderData: keepPreviousData,
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
        setParams(next, { replace: true });
    }

    return (
        <div>
            <PageHeader title="Mailboxes" description="Every connected mailbox across the platform. Filter by owner, org, worker, provider, warmup, risk, pool, credentials, limits, and timeline; scope to one entity and export." />
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
                                <div className="flex flex-col gap-1.5">
                                    {orgId && (
                                        <div className="flex items-center justify-between rounded-md border border-[var(--admin-accent)]/30 bg-[var(--admin-accent-soft)]/40 px-2 py-1.5 text-[12px]">
                                            <span className="truncate">One organization</span>
                                            <Button variant="ghost" size="icon-xs" onClick={() => clearParam("org")} title="Clear org filter">
                                                <X className="size-3" />
                                            </Button>
                                        </div>
                                    )}
                                    {userId && (
                                        <div className="flex items-center justify-between rounded-md border border-[var(--admin-accent)]/30 bg-[var(--admin-accent-soft)]/40 px-2 py-1.5 text-[12px]">
                                            <span className="truncate">One user</span>
                                            <Button variant="ghost" size="icon-xs" onClick={() => clearParam("user")} title="Clear user filter">
                                                <X className="size-3" />
                                            </Button>
                                        </div>
                                    )}
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
