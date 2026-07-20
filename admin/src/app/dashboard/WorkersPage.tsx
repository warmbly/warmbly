// Workers explorer — the managed-worker control plane as a faceted browser.
// Data is fetched all-at-once from /admin/workers/managed (small N), so search,
// faceting, and sort run client-side; the Explorer rail mirrors the Users /
// Organizations / Mailboxes browsers. SSH lifecycle (install / restart /
// uninstall) lives in each worker's detail page.

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "react-router-dom";
import { PageHeader } from "@/components/layout/PageHeader";
import { Badge } from "@/components/ui/badge";
import {
    Explorer,
    FilterGroup,
    SearchFilter,
    SelectFilter,
    ToggleFilter,
    DateRangeFilter,
    NumberRangeFilter,
} from "@/components/data/Explorer";
import { DataTable, type Column } from "@/components/data/DataTable";
import { emptyRange, rangeActive, type DateRange } from "@/lib/dateRange";
import { listManagedWorkers } from "@/lib/api/client/admin/workers";
import type { ManagedWorker } from "@/lib/api/models/admin";

const OFFLINE_MS = 5 * 60_000;
const DAY_MS = 24 * 60 * 60 * 1000;

// Client-side date-range test for the workers explorer (data is in memory).
function inDateRange(iso: string | undefined, r: DateRange): boolean {
    if (!rangeActive(r)) return true;
    if (!iso) return false;
    const t = new Date(iso).getTime();
    if (r.preset && r.preset !== "custom") {
        return t >= Date.now() - Number(r.preset) * DAY_MS;
    }
    if (r.after && t < new Date(r.after).getTime()) return false;
    if (r.before && t >= new Date(r.before).getTime() + DAY_MS) return false;
    return true;
}

type LiveKey = "online" | "stale" | "offline" | "none";

function liveKey(w: ManagedWorker): LiveKey {
    if (!w.last_seen_at) return "none";
    const age = Date.now() - new Date(w.last_seen_at).getTime();
    if (age < 90_000) return "online";
    if (age < OFFLINE_MS * 2) return "stale";
    return "offline";
}

const LIVE_LABEL: Record<LiveKey, { label: string; cls: string }> = {
    online: { label: "online", cls: "text-emerald-600" },
    stale: { label: "stale", cls: "text-amber-600" },
    offline: { label: "offline", cls: "text-red-600" },
    none: { label: "no heartbeat", cls: "text-zinc-400" },
};

const columns: Column<ManagedWorker>[] = [
    {
        id: "name",
        header: "Worker",
        sortable: true,
        sortKey: "name",
        cell: (w) => (
            <div>
                <Link to={`/workers/${w.id}`} onClick={(e) => e.stopPropagation()} className="font-medium text-[var(--admin-accent-strong)] hover:underline">
                    {w.name || w.id.slice(0, 8)}
                </Link>
                <div className="font-mono text-[10px] text-muted-foreground">{w.id}</div>
            </div>
        ),
        csv: (w) => w.name || w.id,
    },
    {
        id: "host",
        header: "Host",
        cell: (w) => (
            <span className="font-mono text-[11px]">
                {w.ssh_user ? `${w.ssh_user}@` : ""}
                {w.ssh_host || w.ip_addr}
                {w.ssh_port ? `:${w.ssh_port}` : ""}
            </span>
        ),
        csv: (w) => w.ssh_host || w.ip_addr,
    },
    {
        id: "live",
        header: "Live",
        cell: (w) => {
            const l = LIVE_LABEL[liveKey(w)];
            return <span className={`text-xs font-medium ${l.cls}`}>{l.label}</span>;
        },
        csv: (w) => LIVE_LABEL[liveKey(w)].label,
    },
    { id: "mailboxes", header: "Mailboxes", align: "right", sortable: true, sortKey: "mailboxes", cell: (w) => <span className="tabular-nums">{w.account_count}</span>, csv: (w) => w.account_count },
    {
        id: "image",
        header: "Image",
        cell: (w) => (w.image_version ? <span className="font-mono text-xs">{w.image_version}</span> : <span className="text-xs text-muted-foreground">—</span>),
        csv: (w) => w.image_version || "",
        defaultHidden: true,
    },
    {
        id: "tags",
        header: "Tags",
        cell: (w) =>
            w.tags && w.tags.length ? (
                <div className="flex flex-wrap gap-1">
                    {w.tags.map((t) => (
                        <Badge key={t} variant="outline" className="text-[10px]">
                            {t}
                        </Badge>
                    ))}
                </div>
            ) : (
                <span className="text-xs text-muted-foreground">—</span>
            ),
        csv: (w) => (w.tags || []).join(" "),
        defaultHidden: true,
    },
    {
        id: "seen",
        header: "Last seen",
        sortable: true,
        sortKey: "seen",
        cell: (w) => <span className="text-xs text-muted-foreground">{w.last_seen_at ? new Date(w.last_seen_at).toLocaleString() : "—"}</span>,
        csv: (w) => w.last_seen_at || "",
    },
    {
        id: "created",
        header: "Created",
        sortable: true,
        sortKey: "created",
        cell: (w) => <span className="text-xs text-muted-foreground">{new Date(w.created_at).toLocaleDateString()}</span>,
        csv: (w) => w.created_at,
        defaultHidden: true,
    },
];

function compare(a: ManagedWorker, b: ManagedWorker, by: string): number {
    switch (by) {
        case "name":
            return (a.name || a.id).localeCompare(b.name || b.id);
        case "mailboxes":
            return a.account_count - b.account_count;
        case "seen":
            return new Date(a.last_seen_at || 0).getTime() - new Date(b.last_seen_at || 0).getTime();
        case "created":
            return new Date(a.created_at).getTime() - new Date(b.created_at).getTime();
        default:
            return 0;
    }
}

export default function WorkersPage() {
    const nav = useNavigate();
    const { data, isLoading, error, refetch } = useQuery({
        queryKey: ["admin", "workers", "managed"],
        queryFn: listManagedWorkers,
        refetchInterval: 15_000,
    });

    const [query, setQuery] = useState("");
    const [live, setLive] = useState("");
    const [activeOnly, setActiveOnly] = useState(false);
    const [hasMailboxes, setHasMailboxes] = useState(false);
    const [hasError, setHasError] = useState(false);
    const [hasTags, setHasTags] = useState(false);
    const [mbMin, setMbMin] = useState<number | undefined>();
    const [mbMax, setMbMax] = useState<number | undefined>();
    const [created, setCreated] = useState<DateRange>(emptyRange);
    const [lastSeen, setLastSeen] = useState<DateRange>(emptyRange);
    const [sort, setSort] = useState<{ by: string; desc: boolean }>({ by: "", desc: true });

    const rows = useMemo(() => {
        let all = data?.data ?? [];
        const q = query.trim().toLowerCase();
        if (q) {
            all = all.filter((w) =>
                `${w.name} ${w.id} ${w.ssh_host ?? ""} ${w.ip_addr} ${w.image_version ?? ""} ${(w.tags || []).join(" ")}`
                    .toLowerCase()
                    .includes(q),
            );
        }
        if (live) all = all.filter((w) => liveKey(w) === live);
        if (activeOnly) all = all.filter((w) => w.active);
        if (hasMailboxes) all = all.filter((w) => w.account_count > 0);
        if (hasError) all = all.filter((w) => !!w.last_error && w.last_error !== "");
        if (hasTags) all = all.filter((w) => (w.tags?.length ?? 0) > 0);
        if (mbMin !== undefined) all = all.filter((w) => w.account_count >= mbMin);
        if (mbMax !== undefined) all = all.filter((w) => w.account_count <= mbMax);
        if (rangeActive(created)) all = all.filter((w) => inDateRange(w.created_at, created));
        if (rangeActive(lastSeen)) all = all.filter((w) => inDateRange(w.last_seen_at, lastSeen));
        if (sort.by) {
            all = [...all].sort((a, b) => compare(a, b, sort.by) * (sort.desc ? -1 : 1));
        }
        return all;
    }, [data, query, live, activeOnly, hasMailboxes, hasError, hasTags, mbMin, mbMax, created, lastSeen, sort]);

    const activeCount =
        (query ? 1 : 0) +
        (live ? 1 : 0) +
        (activeOnly ? 1 : 0) +
        (hasMailboxes ? 1 : 0) +
        (hasError ? 1 : 0) +
        (hasTags ? 1 : 0) +
        (mbMin !== undefined || mbMax !== undefined ? 1 : 0) +
        [created, lastSeen].filter(rangeActive).length +
        (sort.by ? 1 : 0);

    function resetAll() {
        setQuery("");
        setLive("");
        setActiveOnly(false);
        setHasMailboxes(false);
        setHasError(false);
        setHasTags(false);
        setMbMin(undefined);
        setMbMax(undefined);
        setCreated(emptyRange);
        setLastSeen(emptyRange);
        setSort({ by: "", desc: true });
    }

    return (
        <div>
            <PageHeader
                title="Workers"
                description="Physical worker processes managed over SSH. One worker = one machine running the Warmbly worker binary."
            />

            <Explorer
                activeCount={activeCount}
                onReset={resetAll}
                filters={
                    <>
                        <FilterGroup label="Search">
                            <SearchFilter value={query} onChange={setQuery} placeholder="Name, host, IP, version, tag…" />
                        </FilterGroup>
                        <FilterGroup label="Liveness">
                            <SelectFilter
                                value={live || "any"}
                                onChange={(v) => setLive(v === "any" ? "" : v)}
                                options={[
                                    { value: "any", label: "Any" },
                                    { value: "online", label: "Online" },
                                    { value: "stale", label: "Stale" },
                                    { value: "offline", label: "Offline" },
                                    { value: "none", label: "No heartbeat" },
                                ]}
                            />
                        </FilterGroup>
                        <FilterGroup label="Flags">
                            <div className="flex flex-col gap-2">
                                <ToggleFilter checked={activeOnly} onChange={setActiveOnly} label="Active only" />
                                <ToggleFilter checked={hasMailboxes} onChange={setHasMailboxes} label="Has mailboxes" />
                                <ToggleFilter checked={hasError} onChange={setHasError} label="Has SSH error" />
                                <ToggleFilter checked={hasTags} onChange={setHasTags} label="Has tags" />
                            </div>
                        </FilterGroup>
                        <FilterGroup label="Mailbox count">
                            <NumberRangeFilter min={mbMin} max={mbMax} onMinChange={setMbMin} onMaxChange={setMbMax} />
                        </FilterGroup>
                        <FilterGroup label="Connected">
                            <DateRangeFilter value={created} onChange={setCreated} />
                        </FilterGroup>
                        <FilterGroup label="Last seen">
                            <DateRangeFilter value={lastSeen} onChange={setLastSeen} mode="custom" />
                        </FilterGroup>
                    </>
                }
            >
                <DataTable
                    columns={columns}
                    rows={rows}
                    getRowId={(w) => w.id}
                    loading={isLoading}
                    error={error}
                    onRetry={() => refetch()}
                    errorTitle="Failed to load workers"
                    onRowClick={(w) => nav(`/workers/${w.id}`)}
                    sort={sort.by ? sort : undefined}
                    onSortChange={setSort}
                    storageKey="admin.workers"
                    csvName="warmbly-workers"
                    noun="workers"
                    emptyTitle="No workers"
                    emptyHint="No workers match these filters."
                />
            </Explorer>
        </div>
    );
}
