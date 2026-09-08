// Mailbox sync governor. The platform copy of every mailbox's sync state:
// backfill progress and the fair-use throttle the worker last reported via
// SYNC_STATE. No polling here: the realtime spine invalidates
// ["admin","sync"] on SYNC_STATE and account events.

import { useEffect } from "react";
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useSearchParams } from "react-router-dom";
import { toast } from "sonner";
import {
    AlertTriangle,
    CheckCircle2,
    Clock,
    Database,
    Gauge,
    Hourglass,
    MoreHorizontal,
    PauseCircle,
    RotateCcw,
} from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Explorer, FilterGroup, SearchFilter, SegmentedFilter } from "@/components/data/Explorer";
import { DataTable, type Column } from "@/components/data/DataTable";
import { useConfirm } from "@/components/ConfirmDialog";
import { useCursorPager } from "@/lib/useCursorPager";
import {
    ADMIN_SYNC_STATES,
    clearSyncThrottle,
    restartSyncBackfill,
    searchSync,
    type AdminSyncActionResult,
    type AdminSyncRow,
    type AdminSyncStateFilter,
} from "@/lib/api/client/admin/sync";
import { StatCard } from "@/app/dashboard/sends/StatCard";
import { absolute, relative } from "@/app/dashboard/jobs/format";

const BACKFILL_TONE: Record<string, string> = {
    pending: "border-zinc-300 bg-zinc-50 text-zinc-600",
    running: "border-amber-300 bg-amber-50 text-amber-700",
    complete: "border-emerald-300 bg-emerald-50 text-emerald-700",
};

function isThrottled(row: AdminSyncRow): boolean {
    return !!row.throttled_until && new Date(row.throttled_until).getTime() > Date.now();
}

function parseState(raw: string | null): AdminSyncStateFilter {
    return ADMIN_SYNC_STATES.includes(raw as AdminSyncStateFilter) ? (raw as AdminSyncStateFilter) : "all";
}

export default function SyncPage() {
    const [params, setParams] = useSearchParams();
    const state = parseState(params.get("state"));
    const query = params.get("q") ?? "";
    const pager = useCursorPager();
    const { reset } = pager;
    const qc = useQueryClient();
    const confirm = useConfirm();

    function setParam(key: "state" | "q", value: string) {
        const next = new URLSearchParams(params);
        if (value && value !== "all") next.set(key, value);
        else next.delete(key);
        setParams(next, { replace: true });
    }

    const filters = { state, q: query.trim() };
    const filterKey = JSON.stringify(filters);

    useEffect(() => {
        reset();
    }, [filterKey, reset]);

    const { data, isLoading, error, refetch } = useQuery({
        queryKey: ["admin", "sync", filters, pager.cursor],
        queryFn: () =>
            searchSync({
                state: state === "all" ? undefined : state,
                q: filters.q || undefined,
                cursor: pager.cursor,
                limit: 50,
            }),
        staleTime: 30_000,
        placeholderData: keepPreviousData,
    });

    // The worker is re-shipped the mailbox after the write; when that fails
    // the platform copy is right and the worker's live copy is not, which is
    // worth a warning rather than a success.
    function report(verb: string, res: AdminSyncActionResult) {
        if (res.reloaded) {
            toast.success(`${verb}; the worker reloaded the mailbox`);
        } else {
            toast.warning(`${verb}, but the worker was not reloaded: ${res.reload_error || "unknown error"}`);
        }
        qc.invalidateQueries({ queryKey: ["admin", "sync"] });
    }

    const clear = useMutation({
        mutationFn: (row: AdminSyncRow) => clearSyncThrottle(row.email_id),
        onSuccess: (res) => report("Throttle cleared", res),
        onError: (err: Error) => toast.error(err.message || "Failed to clear throttle"),
    });

    const restart = useMutation({
        mutationFn: (row: AdminSyncRow) => restartSyncBackfill(row.email_id),
        onSuccess: (res) => report("Backfill restarted", res),
        onError: (err: Error) => toast.error(err.message || "Failed to restart backfill"),
    });

    async function onRestart(row: AdminSyncRow) {
        const ok = await confirm({
            title: "Restart backfill?",
            description: `${row.email} will re-import its history from scratch: the backfill cursor is dropped and the worker starts again from the newest message, within the configured window. Mail already imported is kept; the pass counts against the mailbox's daily budget.`,
            confirmLabel: "Restart backfill",
            destructive: true,
        });
        if (!ok) return;
        restart.mutate(row);
    }

    const rows = data?.data ?? [];
    const summary = data?.summary;
    const activeCount = (state !== "all" ? 1 : 0) + (query ? 1 : 0);

    const columns: Column<AdminSyncRow>[] = [
        {
            id: "mailbox",
            header: "Mailbox",
            cell: (m) => (
                <div>
                    <div className="flex items-center gap-1.5">
                        <span className="font-medium">{m.email}</span>
                        {m.account_status && m.account_status !== "active" && (
                            <Badge variant="outline" className="text-[10px] border-zinc-300 text-zinc-600">
                                {m.account_status}
                            </Badge>
                        )}
                    </div>
                    <div className="text-[10px] uppercase tracking-wide text-muted-foreground">{m.provider}</div>
                </div>
            ),
            csv: (m) => m.email,
        },
        {
            id: "workspace",
            header: "Workspace",
            cell: (m) =>
                m.organization_id ? (
                    <Link to={`/organizations/${m.organization_id}`} className="text-xs text-[var(--admin-accent-strong)] hover:underline">
                        {m.organization_name || m.organization_id}
                    </Link>
                ) : (
                    <span className="text-xs text-muted-foreground">—</span>
                ),
            csv: (m) => m.organization_name || "",
        },
        {
            id: "backfill",
            header: "Backfill",
            cell: (m) => (
                <div>
                    <div className="flex items-center gap-1.5">
                        <Badge variant="outline" className={`text-[10px] ${BACKFILL_TONE[m.backfill_status] ?? "border-zinc-300 text-zinc-600"}`}>
                            {m.backfill_status || "unknown"}
                        </Badge>
                        {m.stalled && (
                            <Badge variant="outline" className="text-[10px] border-red-300 bg-red-50 text-red-700" title="Running, but its state has not moved for an hour">
                                stalled
                            </Badge>
                        )}
                    </div>
                    <div className="mt-0.5 text-[11px] text-muted-foreground tabular-nums">
                        {m.backfill_synced.toLocaleString()} synced
                        {m.backfill_completed_at
                            ? ` · completed ${relative(m.backfill_completed_at)}`
                            : m.backfill_started_at
                              ? ` · started ${relative(m.backfill_started_at)}`
                              : ""}
                    </div>
                </div>
            ),
            csv: (m) => `${m.backfill_status}${m.stalled ? " (stalled)" : ""}: ${m.backfill_synced}`,
        },
        {
            id: "throttle",
            header: "Throttle",
            cell: (m) => {
                const throttled = isThrottled(m);
                return (
                    <div>
                        {throttled ? (
                            <div className="flex items-center gap-1.5">
                                <Badge variant="outline" className="text-[10px] border-orange-300 bg-orange-50 text-orange-700">
                                    throttled
                                </Badge>
                                <span className="text-xs">{m.throttle_reason || "over budget"}</span>
                            </div>
                        ) : (
                            <span className="text-xs text-muted-foreground">—</span>
                        )}
                        <div className="mt-0.5 text-[11px] text-muted-foreground tabular-nums">
                            {throttled && <span title={absolute(m.throttled_until)}>until {relative(m.throttled_until)}</span>}
                            {throttled && m.deferred > 0 && " · "}
                            {m.deferred > 0 && `${m.deferred.toLocaleString()} deferred`}
                        </div>
                    </div>
                );
            },
            csv: (m) => (isThrottled(m) ? `${m.throttle_reason} until ${m.throttled_until}` : ""),
        },
        {
            id: "synced",
            header: "Last synced",
            cell: (m) => (
                <span className="text-xs text-muted-foreground" title={absolute(m.last_synced_at)}>
                    {relative(m.last_synced_at)}
                </span>
            ),
            csv: (m) => m.last_synced_at || "",
        },
        {
            id: "updated",
            header: "Updated",
            cell: (m) => (
                <span className="text-xs text-muted-foreground" title={absolute(m.updated_at)}>
                    {relative(m.updated_at)}
                </span>
            ),
            csv: (m) => m.updated_at,
        },
        {
            id: "worker",
            header: "Worker",
            defaultHidden: true,
            cell: (m) =>
                m.worker_id ? (
                    <Link to={`/workers/${m.worker_id}`} className="font-mono text-[11px] text-[var(--admin-accent-strong)] hover:underline">
                        {m.worker_id.slice(0, 8)}
                    </Link>
                ) : (
                    <span className="text-xs text-muted-foreground">—</span>
                ),
            csv: (m) => m.worker_id || "",
        },
        {
            id: "actions",
            header: "",
            align: "right",
            cell: (m) => (
                <RowActions
                    row={m}
                    busy={(clear.isPending && clear.variables?.email_id === m.email_id) || (restart.isPending && restart.variables?.email_id === m.email_id)}
                    onClear={() => clear.mutate(m)}
                    onRestart={() => onRestart(m)}
                />
            ),
        },
    ];

    return (
        <div>
            <PageHeader
                title="Sync"
                description="The platform copy of every mailbox's sync state: how far its history backfill has come and whether the fair-use governor is holding it back."
            />

            <div className="mb-6 grid gap-3 sm:grid-cols-2 lg:grid-cols-4 xl:grid-cols-7">
                <StatCard icon={Database} label="Mailboxes" value={fmt(summary?.total)} loading={isLoading} />
                <StatCard icon={PauseCircle} label="Throttled" value={fmt(summary?.throttled)} loading={isLoading} tone={summary?.throttled ? "warn" : "neutral"} />
                <StatCard icon={Hourglass} label="Backfilling" value={fmt(summary?.backfilling)} loading={isLoading} />
                <StatCard icon={AlertTriangle} label="Stalled" value={fmt(summary?.stalled)} loading={isLoading} tone={summary?.stalled ? "danger" : "neutral"} />
                <StatCard icon={Clock} label="Pending" value={fmt(summary?.pending)} loading={isLoading} />
                <StatCard icon={CheckCircle2} label="Complete" value={fmt(summary?.complete)} loading={isLoading} />
                <StatCard icon={Gauge} label="Deferred" value={fmt(summary?.deferred)} loading={isLoading} sub="messages held for a later pass" />
            </div>

            <Explorer
                activeCount={activeCount}
                onReset={() => {
                    const next = new URLSearchParams(params);
                    next.delete("state");
                    next.delete("q");
                    setParams(next, { replace: true });
                }}
                filters={
                    <>
                        <FilterGroup label="Search">
                            <SearchFilter value={query} onChange={(v) => setParam("q", v)} placeholder="Mailbox or workspace…" />
                        </FilterGroup>
                        <FilterGroup label="State">
                            <SegmentedFilter
                                value={state}
                                onChange={(v) => setParam("state", v)}
                                options={[
                                    { value: "all", label: "All" },
                                    { value: "throttled", label: "Throttled" },
                                    { value: "backfilling", label: "Backfilling" },
                                    { value: "stalled", label: "Stalled" },
                                    { value: "pending", label: "Pending" },
                                    { value: "complete", label: "Complete" },
                                ]}
                            />
                        </FilterGroup>
                    </>
                }
            >
                <DataTable
                    columns={columns}
                    rows={rows}
                    getRowId={(m) => m.email_id}
                    loading={isLoading}
                    error={error}
                    onRetry={() => refetch()}
                    errorTitle="Failed to load sync state"
                    storageKey="admin.sync"
                    csvName="warmbly-sync"
                    noun="mailboxes"
                    emptyTitle="No sync state"
                    emptyHint={
                        activeCount > 0
                            ? "No mailboxes match these filters."
                            : "Rows appear once a worker has reported SYNC_STATE for a mailbox. Nothing has synced on this instance yet."
                    }
                    pager={{
                        canPrev: pager.canPrev,
                        canNext: !!data?.pagination?.has_more,
                        onPrev: pager.prev,
                        onNext: () => pager.next(data?.pagination?.next_cursor),
                        page: pager.page,
                        shown: rows.length,
                        total: data?.pagination?.total ?? null,
                    }}
                />
            </Explorer>
        </div>
    );
}

function fmt(n: number | undefined): string {
    return n === undefined ? "—" : n.toLocaleString();
}

// Radix DropdownMenu closes on Escape and click-away on its own; the trigger
// is always visible so the actions are reachable on touch.
function RowActions({
    row,
    busy,
    onClear,
    onRestart,
}: {
    row: AdminSyncRow;
    busy: boolean;
    onClear: () => void;
    onRestart: () => void;
}) {
    const throttled = isThrottled(row);
    return (
        <DropdownMenu>
            <DropdownMenuTrigger asChild>
                <Button variant="outline" size="icon-xs" title="More" disabled={busy} onClick={(e) => e.stopPropagation()}>
                    <MoreHorizontal className="size-3" />
                </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="min-w-44">
                {throttled && (
                    <DropdownMenuItem onSelect={onClear} className="text-[12.5px]">
                        <Gauge /> Clear throttle
                    </DropdownMenuItem>
                )}
                <DropdownMenuItem onSelect={onRestart} className="text-[12.5px]">
                    <RotateCcw /> Restart backfill
                </DropdownMenuItem>
            </DropdownMenuContent>
        </DropdownMenu>
    );
}
