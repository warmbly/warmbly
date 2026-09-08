// Every background loop on the instance, grouped by the process that owns
// it. "Run now" only sets a flag the owning loop checks on its next poll,
// so the marker stays until that process clears it. Polls at 15s: the job
// table has no realtime event.

import { useMemo } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Loader2, Play } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ErrorState } from "@/components/ErrorState";
import { DataTable, type Column } from "@/components/data/DataTable";
import { listJobs, runJob, type ScheduledJobRun } from "@/lib/api/client/admin/jobs";
import { ExpandableText } from "@/app/dashboard/jobs/ExpandableText";
import { absolute, humanDuration, humanInterval, relative } from "@/app/dashboard/jobs/format";

const STATUS_TONE: Record<string, string> = {
    idle: "border-zinc-300 bg-zinc-50 text-zinc-600",
    running: "border-amber-300 bg-amber-50 text-amber-700",
    ok: "border-emerald-300 bg-emerald-50 text-emerald-700",
    error: "border-red-300 bg-red-50 text-red-700",
};

// backend and consumer first; anything new sorts after them by name.
const SERVICE_ORDER = ["backend", "consumer"];

function serviceRank(s: string): number {
    const i = SERVICE_ORDER.indexOf(s);
    return i === -1 ? SERVICE_ORDER.length : i;
}

export default function JobsPage() {
    const qc = useQueryClient();

    const { data, isLoading, error, refetch } = useQuery({
        queryKey: ["admin", "jobs"],
        queryFn: listJobs,
        refetchInterval: 15_000,
    });

    const run = useMutation({
        mutationFn: (name: string) => runJob(name),
        onSuccess: () => {
            toast.success("Requested; the loop picks it up within 15 seconds");
            qc.invalidateQueries({ queryKey: ["admin", "jobs"] });
        },
        onError: (err: Error) => toast.error(err.message || "Failed to request a run"),
    });

    const groups = useMemo(() => {
        const by = new Map<string, ScheduledJobRun[]>();
        for (const job of data?.data ?? []) {
            const list = by.get(job.service) ?? [];
            list.push(job);
            by.set(job.service, list);
        }
        return [...by.entries()]
            .sort(([a], [b]) => serviceRank(a) - serviceRank(b) || a.localeCompare(b))
            .map(([service, jobs]) => [service, jobs.sort((a, b) => a.name.localeCompare(b.name))] as const);
    }, [data]);

    const columns: Column<ScheduledJobRun>[] = [
        {
            id: "name",
            header: "Job",
            cell: (j) => <span className="font-mono text-xs">{j.name}</span>,
            csv: (j) => j.name,
        },
        {
            id: "interval",
            header: "Interval",
            cell: (j) => <span className="text-xs text-muted-foreground">{humanInterval(j.interval_seconds)}</span>,
            csv: (j) => j.interval_seconds,
        },
        {
            id: "last_run",
            header: "Last run",
            cell: (j) =>
                j.last_started_at ? (
                    <div>
                        <div className="text-xs" title={absolute(j.last_started_at)}>
                            {relative(j.last_started_at)}
                        </div>
                        <div className="text-[11px] text-muted-foreground tabular-nums">
                            {j.last_status === "running" ? "still running" : `took ${humanDuration(j.last_duration_ms)}`}
                        </div>
                    </div>
                ) : (
                    <span className="text-xs text-muted-foreground">never</span>
                ),
            csv: (j) => j.last_started_at || "",
        },
        {
            id: "status",
            header: "Status",
            cell: (j) => (
                <Badge variant="outline" className={`text-[10px] ${STATUS_TONE[j.last_status] ?? "border-zinc-300 text-zinc-600"}`}>
                    {j.last_status === "running" && <Loader2 className="size-3 animate-spin" />}
                    {j.last_status || "idle"}
                </Badge>
            ),
            csv: (j) => j.last_status,
        },
        {
            id: "next_run",
            header: "Next run",
            cell: (j) => (
                <div className="flex items-center gap-1.5">
                    <span className="text-xs text-muted-foreground" title={absolute(j.next_run_at)}>
                        {relative(j.next_run_at, "—")}
                    </span>
                    {j.run_requested_at && (
                        <Badge
                            variant="outline"
                            className="text-[10px] border-[var(--admin-accent)]/40 bg-[var(--admin-accent-soft)] text-[var(--admin-accent-strong)]"
                            title={`Run requested ${relative(j.run_requested_at)}; cleared when the ${j.service} picks it up`}
                        >
                            requested
                        </Badge>
                    )}
                </div>
            ),
            csv: (j) => j.next_run_at || "",
        },
        {
            id: "counts",
            header: "Runs / errors",
            align: "right",
            cell: (j) => (
                <span className="text-xs tabular-nums">
                    {j.run_count.toLocaleString()}
                    <span className="text-muted-foreground"> / </span>
                    <span className={j.error_count > 0 ? "text-red-700" : "text-muted-foreground"}>{j.error_count.toLocaleString()}</span>
                </span>
            ),
            csv: (j) => `${j.run_count}/${j.error_count}`,
        },
        {
            id: "last_error",
            header: "Last error",
            className: "max-w-md",
            cell: (j) => <ExpandableText text={j.last_error} mono />,
            csv: (j) => j.last_error,
        },
        {
            id: "actions",
            header: "",
            align: "right",
            cell: (j) => {
                const pending = run.isPending && run.variables === j.name;
                return (
                    <Button
                        size="xs"
                        variant="outline"
                        disabled={pending || !!j.run_requested_at}
                        onClick={(e) => {
                            e.stopPropagation();
                            run.mutate(j.name);
                        }}
                        title={j.run_requested_at ? "A run is already requested" : `Ask the ${j.service} to run this loop now`}
                    >
                        <Play className="size-3" /> {pending ? "Requesting…" : "Run now"}
                    </Button>
                );
            },
        },
    ];

    const empty = !isLoading && !error && groups.length === 0;

    return (
        <div>
            <PageHeader
                title="Jobs"
                description="Every background loop on this instance. A job runs in the process named in its service column, and Run now is picked up by that process at its next poll."
            />

            {error ? (
                <ErrorState error={error} title="Failed to load jobs" onRetry={() => refetch()} />
            ) : empty ? (
                <div className="rounded-lg border border-border bg-card p-6 text-sm text-muted-foreground">
                    <div className="font-medium text-foreground">No jobs have reported yet</div>
                    <div className="mt-0.5 text-xs">
                        Rows appear as soon as a service (the backend or the consumer) has booted on this build and registered its loops.
                    </div>
                </div>
            ) : isLoading ? (
                <DataTable columns={columns} rows={[]} getRowId={(j) => j.name} loading storageKey="admin.jobs" noun="jobs" />
            ) : (
                groups.map(([service, jobs]) => (
                    <section key={service} className="mt-6 first:mt-0">
                        <div className="mb-2 flex items-baseline gap-2">
                            <h2 className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">{service}</h2>
                            <span className="text-[11px] text-muted-foreground">
                                {jobs.length} {jobs.length === 1 ? "loop" : "loops"}
                            </span>
                        </div>
                        <DataTable
                            columns={columns}
                            rows={jobs}
                            getRowId={(j) => j.name}
                            storageKey="admin.jobs"
                            csvName={`warmbly-jobs-${service}`}
                            noun="jobs"
                        />
                    </section>
                ))
            )}
        </div>
    );
}
