// Reserved campaign sends no worker result has resolved. A row lives between
// ReserveSend and the EMAIL_SENT / EMAIL_FAILED answer; the consumer's stuck
// send reclaimer resolves anything older than the reclaim window. No realtime
// event covers reservations, so this polls at 30s.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import { AlertTriangle, Clock, Hourglass, Play, Send, Timer } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { DataTable, type Column } from "@/components/data/DataTable";
import { useConfirm } from "@/components/ConfirmDialog";
import { listInFlightSends, type AdminInFlightSend } from "@/lib/api/client/admin/sends";
import { runJob, STUCK_SEND_RECLAIMER_JOB } from "@/lib/api/client/admin/jobs";
import { StatCard } from "@/app/dashboard/sends/StatCard";
import { absolute, humanSeconds, relative, shortId } from "@/app/dashboard/jobs/format";

const TASK_TONE: Record<string, string> = {
    pending: "border-amber-300 bg-amber-50 text-amber-700",
    processing: "border-amber-300 bg-amber-50 text-amber-700",
    completed: "border-emerald-300 bg-emerald-50 text-emerald-700",
    failed: "border-red-300 bg-red-50 text-red-700",
};

export function InFlightTab() {
    const qc = useQueryClient();
    const confirm = useConfirm();

    const { data, isLoading, error, refetch } = useQuery({
        queryKey: ["admin", "sends", "in-flight"],
        queryFn: () => listInFlightSends(200),
        refetchInterval: 30_000,
    });

    const run = useMutation({
        mutationFn: () => runJob(STUCK_SEND_RECLAIMER_JOB),
        onSuccess: () => {
            toast.success("Reclaimer requested; the consumer's reclaim loop runs it at its next poll, within about 15 seconds");
            qc.invalidateQueries({ queryKey: ["admin", "jobs"] });
        },
        onError: (err: Error) => toast.error(err.message || "Failed to request the reclaimer"),
    });

    const summary = data?.summary;
    const reclaimAfter = summary?.reclaim_after_minutes;
    const windowSeconds = (reclaimAfter ?? 0) * 60;

    async function onRun() {
        const ok = await confirm({
            title: "Run the stuck-send reclaimer now?",
            description: `This asks the consumer's reclaim loop to run at its next poll, within about 15 seconds. Every reservation older than ${reclaimAfter ?? "the reclaim window in"} minutes is resolved: a task that already carries a Message-ID is stamped as sent, anything else is walked back as a failed attempt and retried on the next routing tick.`,
            confirmLabel: "Run reclaimer",
        });
        if (!ok) return;
        run.mutate();
    }

    const rows = data?.data ?? [];

    const columns: Column<AdminInFlightSend>[] = [
        {
            id: "campaign",
            header: "Campaign",
            cell: (r) => (
                <div>
                    <Link to={`/campaigns/${r.campaign_id}`} className="font-medium text-[var(--admin-accent-strong)] hover:underline">
                        {r.campaign_name || shortId(r.campaign_id)}
                    </Link>
                    <div className="font-mono text-[10px] text-muted-foreground">{shortId(r.campaign_id)}</div>
                </div>
            ),
            csv: (r) => r.campaign_name,
        },
        {
            id: "workspace",
            header: "Workspace",
            cell: (r) =>
                r.organization_id ? (
                    <Link to={`/organizations/${r.organization_id}`} className="text-xs text-[var(--admin-accent-strong)] hover:underline">
                        {r.organization_name || r.organization_id}
                    </Link>
                ) : (
                    <span className="text-xs text-muted-foreground">—</span>
                ),
            csv: (r) => r.organization_name || "",
        },
        { id: "contact", header: "Contact", cell: (r) => <span className="text-xs">{r.contact_email}</span>, csv: (r) => r.contact_email },
        {
            id: "mailbox",
            header: "Mailbox",
            cell: (r) => (
                <div>
                    <div className="text-xs">{r.mailbox_email || "—"}</div>
                    {r.worker_id && (
                        <Link to={`/workers/${r.worker_id}`} className="font-mono text-[10px] text-muted-foreground hover:underline">
                            worker {shortId(r.worker_id)}
                        </Link>
                    )}
                </div>
            ),
            csv: (r) => r.mailbox_email,
        },
        {
            id: "task",
            header: "Task",
            cell: (r) => (
                <div className="flex items-center gap-1.5">
                    <Badge variant="outline" className={`text-[10px] ${TASK_TONE[r.task_status] ?? "border-zinc-300 text-zinc-600"}`}>
                        {r.task_status || "unknown"}
                    </Badge>
                    {r.task_id && <span className="font-mono text-[10px] text-muted-foreground">{shortId(r.task_id)}</span>}
                </div>
            ),
            csv: (r) => r.task_status,
        },
        {
            id: "message_id",
            header: "Message ID",
            cell: (r) =>
                r.has_message_id ? (
                    <Badge
                        variant="outline"
                        className="text-[10px] border-emerald-300 bg-emerald-50 text-emerald-700"
                        title="The worker put the mail on the wire and only the stamp was lost; the reclaimer stamps it rather than retry"
                    >
                        on the wire
                    </Badge>
                ) : (
                    <span className="text-xs text-muted-foreground">none</span>
                ),
            csv: (r) => (r.has_message_id ? "yes" : "no"),
        },
        {
            id: "dispatched",
            header: "Dispatched",
            align: "right",
            cell: (r) => {
                const late = windowSeconds > 0 && r.age_seconds >= windowSeconds;
                return (
                    <span className={`text-xs tabular-nums ${late ? "text-red-700" : "text-muted-foreground"}`} title={absolute(r.dispatched_at)}>
                        {humanSeconds(r.age_seconds)} ago
                    </span>
                );
            },
            csv: (r) => r.dispatched_at,
        },
    ];

    return (
        <div>
            <div className="mb-4 flex flex-col gap-3 md:flex-row md:items-start md:justify-between">
                <p className="max-w-2xl text-sm text-muted-foreground">
                    A reservation is written before SEND_EMAIL goes on the bus and resolved by exactly one worker result.
                    {reclaimAfter !== undefined && (
                        <> The reclaimer sweeps every 5 minutes and resolves anything older than {reclaimAfter} minutes.</>
                    )}
                </p>
                <Button size="sm" variant="outline" className="shrink-0 text-xs" onClick={onRun} disabled={run.isPending}>
                    <Play className="size-3" />
                    {run.isPending ? "Requesting…" : "Run reclaimer now"}
                </Button>
            </div>

            <div className="mb-6 grid gap-3 sm:grid-cols-2 lg:grid-cols-5">
                <StatCard icon={Send} label="In flight" value={fmt(summary?.total)} loading={isLoading} />
                <StatCard icon={Timer} label="Under 5 min" value={fmt(summary?.under_5m)} loading={isLoading} />
                <StatCard icon={Clock} label="Under 30 min" value={fmt(summary?.under_30m)} loading={isLoading} />
                <StatCard
                    icon={AlertTriangle}
                    label="Past reclaim window"
                    value={fmt(summary?.past_reclaim_window)}
                    loading={isLoading}
                    tone={summary?.past_reclaim_window ? "danger" : "neutral"}
                    sub={reclaimAfter !== undefined ? `older than ${reclaimAfter} min` : undefined}
                />
                <StatCard
                    icon={Hourglass}
                    label="Oldest"
                    value={summary?.oldest_dispatched_at ? relative(summary.oldest_dispatched_at) : "—"}
                    loading={isLoading}
                    sub={summary?.oldest_dispatched_at ? absolute(summary.oldest_dispatched_at) : undefined}
                />
            </div>

            <DataTable
                columns={columns}
                rows={rows}
                getRowId={(r) => `${r.campaign_id}:${r.contact_id}:${r.sequence_id}`}
                loading={isLoading}
                error={error}
                onRetry={() => refetch()}
                errorTitle="Failed to load in-flight sends"
                storageKey="admin.sends.in-flight"
                csvName="warmbly-in-flight-sends"
                noun="sends"
                emptyTitle="Nothing in flight"
                emptyHint="No reserved send is waiting on a worker result. Rows appear between a SEND_EMAIL dispatch and its EMAIL_SENT or EMAIL_FAILED answer."
            />
        </div>
    );
}

function fmt(n: number | undefined): string {
    return n === undefined ? "—" : n.toLocaleString();
}
