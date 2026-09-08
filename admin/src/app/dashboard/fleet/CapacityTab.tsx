// Fleet capacity: every worker against its capacity-view row. There is no
// realtime event for the rolling 1h counters, so this view polls at 30s.

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { Badge } from "@/components/ui/badge";
import { DataTable, type Column } from "@/components/data/DataTable";
import { StateLegend } from "@/components/StateLegend";
import { WORKER_HEALTH_LEGEND, WORKER_RISK_POOL_LEGEND } from "@/lib/legends";
import { getFleetCapacity, type AdminFleetWorkerRow } from "@/lib/api/client/admin/fleet";
import { cn } from "@/lib/utils";
import { HealthPill, LiveDot, RiskPoolPill, TierPill, TypePill } from "./tones";
import { fmtAgo } from "./format";

function UtilizationBar({ row }: { row: AdminFleetWorkerRow }) {
    const u = row.utilization ?? 0;
    const pct = Math.max(0, Math.min(100, Math.round(u * 100)));
    const hot = u > 0.8;
    const cold = u < 0.5;
    return (
        <div className="min-w-[160px]">
            <div className="flex items-center justify-between text-[11px] tabular-nums">
                <span>
                    {row.load_score.toFixed(2)}
                    <span className="text-muted-foreground"> / {row.effective_capacity.toFixed(2)}</span>
                </span>
                <span
                    className={cn(
                        "font-medium",
                        hot ? "text-red-600" : cold ? "text-muted-foreground" : "text-emerald-700",
                    )}
                >
                    {pct}%
                </span>
            </div>
            <div className="mt-1 h-1.5 overflow-hidden rounded bg-muted">
                <div
                    className={cn(
                        "h-full",
                        hot ? "bg-gradient-to-r from-amber-500 to-red-500" : cold ? "bg-zinc-300" : "bg-emerald-500",
                    )}
                    style={{ width: `${pct}%` }}
                />
            </div>
        </div>
    );
}

function Pair({ a, b, tone }: { a: number; b: number; tone?: string }) {
    return (
        <span className="tabular-nums text-xs">
            <span className={tone}>{a.toLocaleString()}</span>
            <span className="text-muted-foreground"> / {b.toLocaleString()}</span>
        </span>
    );
}

function Count({ n, warnAbove = 0 }: { n: number; warnAbove?: number }) {
    return (
        <span className={cn("tabular-nums text-xs", n > warnAbove ? "font-medium text-red-600" : "text-muted-foreground")}>
            {n.toLocaleString()}
        </span>
    );
}

const columns: Column<AdminFleetWorkerRow>[] = [
    {
        id: "name",
        header: "Worker",
        sortable: true,
        cell: (w) => (
            <div>
                <Link
                    to={`/workers/${w.worker_id}`}
                    onClick={(e) => e.stopPropagation()}
                    className="font-medium text-[var(--admin-accent-strong)] hover:underline"
                >
                    {w.name || w.worker_id.slice(0, 8)}
                </Link>
                <div className="font-mono text-[10px] text-muted-foreground">{w.ip_addr}</div>
            </div>
        ),
        csv: (w) => w.name || w.worker_id,
    },
    { id: "tier", header: "Tier", cell: (w) => <TierPill freeTier={w.free_tier} />, csv: (w) => (w.free_tier ? "free" : "premium") },
    { id: "type", header: "Type", cell: (w) => <TypePill type={w.worker_type} />, csv: (w) => w.worker_type },
    { id: "pool", header: "Risk pool", cell: (w) => <RiskPoolPill pool={w.risk_pool} />, csv: (w) => w.risk_pool },
    {
        id: "egress",
        header: "Egress",
        cell: (w) => <span className="font-mono text-[11px]">{w.egress_kind}</span>,
        csv: (w) => w.egress_kind,
        defaultHidden: true,
    },
    { id: "health", header: "Health", cell: (w) => <HealthPill state={w.health_state} />, csv: (w) => w.health_state },
    {
        id: "live",
        header: "Live",
        cell: (w) => <LiveDot live={w.live} title={w.last_seen_at ? `last seen ${fmtAgo(w.last_seen_at)}` : "no heartbeat"} />,
        csv: (w) => (w.live ? "live" : "offline"),
    },
    {
        id: "accounts",
        header: "Accounts",
        align: "right",
        sortable: true,
        cell: (w) => <span className="tabular-nums">{w.account_count}</span>,
        csv: (w) => w.account_count,
    },
    {
        id: "utilization",
        header: "Load / capacity",
        sortable: true,
        cell: (w) => <UtilizationBar row={w} />,
        csv: (w) => `${w.load_score.toFixed(2)}/${w.effective_capacity.toFixed(2)} (${Math.round(w.utilization * 100)}%)`,
    },
    {
        id: "sends",
        header: "Sends 1h",
        align: "right",
        sortable: true,
        cell: (w) => <Pair a={w.sends_succeeded_1h} b={w.sends_attempted_1h} tone="text-foreground" />,
        csv: (w) => `${w.sends_succeeded_1h}/${w.sends_attempted_1h}`,
    },
    {
        id: "bounces",
        header: "Bounces 1h",
        align: "right",
        cell: (w) => <Pair a={w.bounces_hard_1h} b={w.bounces_soft_1h} tone={w.bounces_hard_1h > 0 ? "text-red-600 font-medium" : "text-foreground"} />,
        csv: (w) => `${w.bounces_hard_1h} hard / ${w.bounces_soft_1h} soft`,
    },
    { id: "complaints", header: "Complaints 1h", align: "right", cell: (w) => <Count n={w.complaints_1h} />, csv: (w) => w.complaints_1h },
    { id: "auth", header: "Auth errors 1h", align: "right", cell: (w) => <Count n={w.auth_errors_1h} />, csv: (w) => w.auth_errors_1h },
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
];

function compare(a: AdminFleetWorkerRow, b: AdminFleetWorkerRow, by: string): number {
    switch (by) {
        case "name":
            return (a.name || a.worker_id).localeCompare(b.name || b.worker_id);
        case "accounts":
            return a.account_count - b.account_count;
        case "utilization":
            return a.utilization - b.utilization;
        case "sends":
            return a.sends_attempted_1h - b.sends_attempted_1h;
        default:
            return 0;
    }
}

export function CapacityTab() {
    const { data, isLoading, error, refetch } = useQuery({
        queryKey: ["admin", "fleet", "capacity"],
        queryFn: getFleetCapacity,
        refetchInterval: 30_000,
    });
    const [sort, setSort] = useState<{ by: string; desc: boolean }>({ by: "utilization", desc: true });

    const rows = useMemo(() => {
        const all = data?.data ?? [];
        return sort.by ? [...all].sort((a, b) => compare(a, b, sort.by) * (sort.desc ? -1 : 1)) : all;
    }, [data, sort]);

    const hot = rows.filter((r) => r.utilization > 0.8).length;
    const cold = rows.filter((r) => r.utilization < 0.5).length;

    return (
        <div>
            <div className="mb-3 flex flex-wrap items-center justify-between gap-2 rounded-md border border-border bg-card px-3 py-2 text-[12.5px] text-muted-foreground">
                <span>
                    The rebalancer drains a worker above <span className="font-medium text-foreground">80%</span> utilization
                    onto peers below <span className="font-medium text-foreground">50%</span> in the same tier, and never
                    across tiers. Counters are the last hour; this view refreshes every 30s.
                    {rows.length > 0 && (
                        <span className="ml-1.5 tabular-nums">
                            ({hot} hot, {cold} cold of {rows.length})
                        </span>
                    )}
                </span>
                <span className="flex flex-wrap gap-3">
                    <StateLegend label="Risk pools" entries={WORKER_RISK_POOL_LEGEND} />
                    <StateLegend label="Health states" entries={WORKER_HEALTH_LEGEND} />
                </span>
            </div>
            <DataTable
                columns={columns}
                rows={rows}
                getRowId={(w) => w.worker_id}
                loading={isLoading}
                error={error}
                onRetry={() => refetch()}
                errorTitle="Failed to load fleet capacity"
                sort={sort.by ? sort : undefined}
                onSortChange={setSort}
                storageKey="admin.fleet.capacity"
                csvName="warmbly-fleet-capacity"
                noun="workers"
                emptyTitle="No workers"
                emptyHint="Capacity rows appear once a worker has registered and heartbeated. Add one under Workers."
            />
        </div>
    );
}
