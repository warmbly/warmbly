// Customer webhook delivery health across the instance, and the endpoints
// that are currently failing. Polls at 30s; no realtime event covers the
// delivery queue.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import { AlertTriangle, CheckCircle2, Clock, RotateCcw, Trash2, XCircle, Ban } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { DataTable, type Column } from "@/components/data/DataTable";
import { useConfirm } from "@/components/ConfirmDialog";
import { getWebhookHealth, reclaimWebhookDeliveries, type AdminWebhookEndpointRow } from "@/lib/api/client/admin/sends";
import { StatCard } from "@/app/dashboard/sends/StatCard";
import { ExpandableText } from "@/app/dashboard/jobs/ExpandableText";
import { absolute, relative } from "@/app/dashboard/jobs/format";

const columns: Column<AdminWebhookEndpointRow>[] = [
    {
        id: "workspace",
        header: "Workspace",
        cell: (r) => (
            <Link to={`/organizations/${r.organization_id}`} className="text-xs text-[var(--admin-accent-strong)] hover:underline">
                {r.organization_name || r.organization_id}
            </Link>
        ),
        csv: (r) => r.organization_name,
    },
    {
        id: "url",
        header: "Endpoint",
        className: "max-w-sm",
        cell: (r) => (
            <div>
                <div className="truncate font-mono text-[11px]" title={r.url}>
                    {r.url}
                </div>
                {r.description && <div className="truncate text-[11px] text-muted-foreground">{r.description}</div>}
            </div>
        ),
        csv: (r) => r.url,
    },
    {
        id: "consecutive",
        header: "Consecutive failures",
        align: "right",
        cell: (r) => (
            <span className={`text-xs tabular-nums ${r.consecutive_failures > 0 ? "font-medium text-red-700" : "text-muted-foreground"}`}>
                {r.consecutive_failures.toLocaleString()}
            </span>
        ),
        csv: (r) => r.consecutive_failures,
    },
    {
        id: "last_failure",
        header: "Last failure",
        className: "max-w-md",
        cell: (r) => (
            <div>
                <ExpandableText text={r.last_failure_reason} mono />
                {r.last_failure_at && (
                    <div className="text-[11px] text-muted-foreground" title={absolute(r.last_failure_at)}>
                        {relative(r.last_failure_at)}
                        {r.last_success_at && ` · last success ${relative(r.last_success_at)}`}
                    </div>
                )}
            </div>
        ),
        csv: (r) => r.last_failure_reason,
    },
    {
        id: "week",
        header: "Last 7 days",
        align: "right",
        cell: (r) => (
            <span className="text-xs tabular-nums text-muted-foreground" title="delivered / failed / dropped">
                <span className="text-emerald-700">{r.deliveries_last_7d.toLocaleString()}</span>
                {" / "}
                <span className={r.failed_last_7d > 0 ? "text-red-700" : undefined}>{r.failed_last_7d.toLocaleString()}</span>
                {" / "}
                <span className={r.drops_last_7d > 0 ? "text-amber-700" : undefined}>{r.drops_last_7d.toLocaleString()}</span>
            </span>
        ),
        csv: (r) => `${r.deliveries_last_7d}/${r.failed_last_7d}/${r.drops_last_7d}`,
    },
    {
        id: "enabled",
        header: "Enabled",
        cell: (r) =>
            r.enabled ? (
                <span className="text-xs text-emerald-600">enabled</span>
            ) : (
                <Badge variant="outline" className="text-[10px] border-zinc-300 text-zinc-600">
                    disabled
                </Badge>
            ),
        csv: (r) => (r.enabled ? "yes" : "no"),
    },
];

export function WebhooksTab() {
    const qc = useQueryClient();
    const confirm = useConfirm();

    const { data, isLoading, error, refetch } = useQuery({
        queryKey: ["admin", "sends", "webhooks"],
        queryFn: getWebhookHealth,
        refetchInterval: 30_000,
    });

    const reclaim = useMutation({
        mutationFn: reclaimWebhookDeliveries,
        onSuccess: (res) => {
            toast.success(`Reclaimed ${res.reclaimed.toLocaleString()} stuck ${res.reclaimed === 1 ? "delivery" : "deliveries"}`);
            qc.invalidateQueries({ queryKey: ["admin", "sends", "webhooks"] });
        },
        onError: (err: Error) => toast.error(err.message || "Failed to reclaim deliveries"),
    });

    async function onReclaim() {
        const ok = await confirm({
            title: "Reclaim stuck deliveries?",
            description: `Deliveries claimed longer ago than the ${data?.lease_minutes ?? ""} minute lease are handed back to the queue so a delivery worker can pick them up again. An endpoint may receive a duplicate if the original attempt did complete after its lease expired.`,
            confirmLabel: "Reclaim",
        });
        if (!ok) return;
        reclaim.mutate();
    }

    const rows = data?.failing_endpoints ?? [];

    return (
        <div>
            <div className="mb-4 flex flex-col gap-3 md:flex-row md:items-start md:justify-between">
                <p className="max-w-2xl text-sm text-muted-foreground">
                    Instance-wide delivery of customer webhooks.
                    {data && <> A delivery is stale once it has been claimed for more than {data.lease_minutes} minutes without a result.</>}
                </p>
                <Button size="sm" variant="outline" className="shrink-0 text-xs" onClick={onReclaim} disabled={reclaim.isPending}>
                    <RotateCcw className="size-3" />
                    {reclaim.isPending ? "Reclaiming…" : "Reclaim stuck deliveries"}
                </Button>
            </div>

            <div className="mb-6 grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-6">
                <StatCard icon={AlertTriangle} label="Stale in flight" value={fmt(data?.in_flight_stale)} loading={isLoading} tone={data?.in_flight_stale ? "warn" : "neutral"} sub={data ? `lease ${data.lease_minutes} min` : undefined} />
                <StatCard icon={Clock} label="Pending due" value={fmt(data?.pending_due)} loading={isLoading} />
                <StatCard icon={CheckCircle2} label="Delivered 24h" value={fmt(data?.delivered_last_24h)} loading={isLoading} />
                <StatCard icon={XCircle} label="Failed 24h" value={fmt(data?.failed_last_24h)} loading={isLoading} tone={data?.failed_last_24h ? "warn" : "neutral"} />
                <StatCard icon={Ban} label="Abandoned 24h" value={fmt(data?.abandoned_last_24h)} loading={isLoading} tone={data?.abandoned_last_24h ? "danger" : "neutral"} />
                <StatCard icon={Trash2} label="Drops 7d" value={fmt(data?.drops_last_7d)} loading={isLoading} sub="queue full, never attempted" />
            </div>

            <div className="mb-2 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">Failing endpoints</div>
            <DataTable
                columns={columns}
                rows={rows}
                getRowId={(r) => r.id}
                loading={isLoading}
                error={error}
                onRetry={() => refetch()}
                errorTitle="Failed to load webhook health"
                storageKey="admin.sends.webhooks"
                csvName="warmbly-failing-webhooks"
                noun="endpoints"
                emptyTitle="No failing endpoints"
                emptyHint="Every customer endpoint accepted its last delivery. Rows appear when an endpoint fails consecutively, worst first."
            />
        </div>
    );
}

function fmt(n: number | undefined): string {
    return n === undefined ? "—" : n.toLocaleString();
}
