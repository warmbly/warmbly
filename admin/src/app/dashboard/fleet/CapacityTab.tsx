// Fleet capacity: every worker against its capacity-view row. There is no
// realtime event for the rolling 1h counters, so this view polls at 30s.

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { StatusBadge, StatusDot } from "@/components/ui/kit";
import { DataTable, type Column } from "@/components/data/DataTable";
import { StateLegend } from "@/components/StateLegend";
import { WORKER_HEALTH_LEGEND } from "@/lib/legends";
import { getFleetCapacity, type AdminFleetWorkerRow } from "@/lib/api/client/admin/fleet";
import { TONE_TEXT } from "@/lib/tones";
import { cn } from "@/lib/utils";
import { HealthPill, LiveDot } from "./tones";
import { fmtAgo, resourceCSV } from "./format";
import { ResourceUsage } from "./ResourceUsage";
import { FleetScalePanel } from "./FleetScalePanel";

function MailboxTarget({ row }: { row: AdminFleetWorkerRow }) {
    const u = row.utilization ?? 0;
    const pct = Math.max(0, Math.round(u * 100));
    return (
        <div className="min-w-[160px]">
            <div className="flex items-center justify-between text-xs tabular-nums">
                <span className="text-foreground">
                    {row.account_count.toLocaleString()}
                    <span className="text-subtle-foreground"> / {row.effective_capacity.toFixed(0)}</span>
                </span>
                <span
                    className="text-muted-foreground"
                    title="Assigned mailboxes relative to the planning target, not CPU or RAM saturation"
                >
                    {pct}% of plan
                </span>
            </div>
        </div>
    );
}

function Pair({ a, b, tone, title }: { a: number; b: number; tone?: string; title?: string }) {
    return (
        <span className="tabular-nums" title={title}>
            <span className={tone}>{a.toLocaleString()}</span>
            <span className="text-subtle-foreground"> / {b.toLocaleString()}</span>
        </span>
    );
}

function Count({ n, warnAbove = 0 }: { n: number; warnAbove?: number }) {
    return (
        <span className={cn("tabular-nums", n > warnAbove ? cn("font-medium", TONE_TEXT.danger) : "text-subtle-foreground")}>
            {n.toLocaleString()}
        </span>
    );
}

type FleetAnalyticsRow = AdminFleetWorkerRow & { mailboxShare: number | null; sendShare: number | null };

const columns: Column<FleetAnalyticsRow>[] = [
    {
        id: "name",
        header: "Worker",
        sortable: true,
        cell: (w) => (
            <div className="min-w-0">
                <Link
                    to={`/workers/${w.worker_id}`}
                    onClick={(e) => e.stopPropagation()}
                    className="font-medium text-foreground underline-offset-2 hover:underline"
                >
                    {w.name || w.worker_id.slice(0, 8)}
                </Link>
            </div>
        ),
        csv: (w) => w.name || w.worker_id,
    },
    {
        id: "ip",
        header: "Public IPv4",
        cell: (w) => <span className="font-mono text-xs">{w.ip_addr || "Unavailable"}</span>,
        csv: (w) => w.ip_addr,
    },
    {
        id: "region",
        header: "Region",
        cell: (w) =>
            w.region ? (
                <span className="font-mono text-xs text-muted-foreground">{w.region}</span>
            ) : (
                <span className="text-subtle-foreground">—</span>
            ),
        csv: (w) => w.region,
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
        header: "Mailboxes",
        align: "right",
        sortable: true,
        cell: (w) => <span className="tabular-nums">{w.account_count}</span>,
        csv: (w) => w.account_count,
    },
    {
        id: "utilization",
        header: "Mailbox target",
        sortable: true,
        cell: (w) => <MailboxTarget row={w} />,
        csv: (w) => `${w.account_count}/${w.effective_capacity.toFixed(0)} (${Math.round(w.utilization * 100)}% of plan)`,
    },
    { id: "cpu", header: "CPU", align: "right", cell: (w) => <ResourceUsage usage={w.usage} kind="cpu" live={w.live} />, csv: (w) => resourceCSV(w.usage, "cpu", w.live) },
    { id: "memory", header: "RAM", align: "right", cell: (w) => <ResourceUsage usage={w.usage} kind="memory" live={w.live} />, csv: (w) => resourceCSV(w.usage, "memory", w.live) },
    { id: "resident", header: "Process RAM", align: "right", cell: (w) => <ResourceUsage usage={w.usage} kind="resident" live={w.live} />, csv: (w) => w.live ? w.usage?.resident_mb ?? "" : "stale" },
    { id: "mailboxShare", header: "Mailbox share", align: "right", cell: (w) => <span className="tabular-nums" title="Share of all mailboxes assigned across the fleet">{w.mailboxShare == null ? "N/A" : `${w.mailboxShare.toFixed(1)}%`}</span>, csv: (w) => w.mailboxShare?.toFixed(1) ?? "" },
    { id: "sendShare", header: "Send share 60m", align: "right", cell: (w) => <span className="tabular-nums" title="Share of all provider send attempts in the rolling last 60 minutes">{w.sendShare == null ? "No attempts" : `${w.sendShare.toFixed(1)}%`}</span>, csv: (w) => w.sendShare?.toFixed(1) ?? "", defaultHidden: true },
    { id: "successRate", header: "Send success 60m", align: "right", cell: (w) => <span className="tabular-nums" title="Successful provider handoffs / attempts, not inbox placement or delivery rate">{w.sends_attempted_1h === 0 ? "No attempts" : `${(w.sends_succeeded_1h / w.sends_attempted_1h * 100).toFixed(1)}%`}</span>, csv: (w) => w.sends_attempted_1h === 0 ? "" : (w.sends_succeeded_1h / w.sends_attempted_1h * 100).toFixed(1), defaultHidden: true },
    {
        id: "sends",
        header: "Send activity 60m",
        align: "right",
        sortable: true,
        cell: (w) =>
            w.sends_attempted_1h === 0 ? (
                <span
                    className="text-subtle-foreground"
                    title="The worker made no provider send attempts during the rolling last 60 minutes"
                >
                    No attempts
                </span>
            ) : (
                <Pair
                    a={w.sends_succeeded_1h}
                    b={w.sends_attempted_1h}
                    tone="text-foreground"
                    title="Succeeded / attempted during the rolling last 60 minutes"
                />
            ),
        csv: (w) => `${w.sends_succeeded_1h}/${w.sends_attempted_1h}`,
    },
    {
        id: "bounces",
        header: "Bounces 1h",
        align: "right",
        cell: (w) => <Pair a={w.bounces_hard_1h} b={w.bounces_soft_1h} tone={w.bounces_hard_1h > 0 ? cn("font-medium", TONE_TEXT.danger) : "text-foreground"} />,
        csv: (w) => `${w.bounces_hard_1h} hard / ${w.bounces_soft_1h} soft`,
        defaultHidden: true,
    },
    { id: "complaints", header: "Complaints 1h", align: "right", cell: (w) => <Count n={w.complaints_1h} />, csv: (w) => w.complaints_1h, defaultHidden: true },
    { id: "auth", header: "Auth errors 1h", align: "right", cell: (w) => <Count n={w.auth_errors_1h} />, csv: (w) => w.auth_errors_1h, defaultHidden: true },
    { id: "seen", header: "Last heartbeat", cell: (w) => w.last_seen_at ? <span title={new Date(w.last_seen_at).toLocaleString()}>{fmtAgo(w.last_seen_at)}</span> : "Never", csv: (w) => w.last_seen_at ?? "", defaultHidden: true },
    {
        id: "tags",
        header: "Tags",
        cell: (w) =>
            w.tags && w.tags.length ? (
                <div className="flex flex-wrap gap-1">
                    {w.tags.map((t) => (
                        <StatusBadge key={t}>{t}</StatusBadge>
                    ))}
                </div>
            ) : (
                <span className="text-subtle-foreground">—</span>
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
        const mailboxes = all.reduce((sum, row) => sum + row.account_count, 0);
        const attempts = all.reduce((sum, row) => sum + row.sends_attempted_1h, 0);
        const enriched = all.map((row) => ({ ...row, mailboxShare: mailboxes ? row.account_count / mailboxes * 100 : null, sendShare: attempts ? row.sends_attempted_1h / attempts * 100 : null }));
        return sort.by ? enriched.sort((a, b) => compare(a, b, sort.by) * (sort.desc ? -1 : 1)) : enriched;
    }, [data, sort]);

    const live = rows.filter((r) => r.live).length;
    const mailboxes = rows.reduce((sum, row) => sum + row.account_count, 0);

    return (
        <div>
            <FleetScalePanel />
            <div className="mb-4 flex flex-wrap items-start justify-between gap-x-8 gap-y-3">
                <p className="max-w-3xl text-[12.5px] leading-relaxed text-muted-foreground">
                    Mailbox targets guide placement, not machine saturation or provider send limits.
                    CPU and RAM are measured independently, labeled container or host. Process RAM is
                    the worker's resident memory. Unavailable means not measured; stale means no fresh heartbeat.
                    Shares show each worker's portion of fleet mailboxes and send attempts. Send counters
                    cover the rolling last 60 minutes, not inbox placement.
                </p>
                <div className="flex shrink-0 flex-wrap items-center gap-x-4 gap-y-2">
                    {rows.length > 0 && (
                        <span className="flex items-center gap-3 text-xs text-muted-foreground tabular-nums">
                            <StatusDot tone="neutral" className="text-xs">
                                {live} / {rows.length} live
                            </StatusDot>
                            <span>{mailboxes.toLocaleString()} mailboxes</span>
                        </span>
                    )}
                    <StateLegend label="Health states" entries={WORKER_HEALTH_LEGEND} />
                </div>
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
