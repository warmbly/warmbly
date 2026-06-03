// /warmup-content/jobs — paged table of generation jobs (sync + batch),
// polled live so running jobs update without a manual refresh. Batch jobs
// surface their OpenAI batch status and an inline Cancel action.

import { useMemo } from "react";
import {
    keepPreviousData,
    useMutation,
    useQuery,
    useQueryClient,
} from "@tanstack/react-query";
import { toast } from "sonner";
import { Ban } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { DataTable, type Column } from "@/components/data/DataTable";
import { useCursorPager } from "@/lib/useCursorPager";
import {
    cancelWarmupBatch,
    isJobActive,
    isJobCancellable,
    listWarmupGenerationJobs,
    type WarmupGenerationJob,
} from "@/lib/api/client/admin/warmupContent";
import { PoolBadge } from "./components";
import { batchTone, fmtDate, jobTone } from "./shared";

export default function JobsPage() {
    const qc = useQueryClient();
    const pager = useCursorPager();

    const { data, isLoading, error, refetch } = useQuery({
        queryKey: ["admin", "warmup-content", "jobs", pager.cursor],
        queryFn: () => listWarmupGenerationJobs({ cursor: pager.cursor, limit: 50 }),
        placeholderData: keepPreviousData,
        // Poll only while at least one job is still in flight; idle pages stop
        // hammering the endpoint.
        refetchInterval: (query) => {
            const rows = query.state.data?.data ?? [];
            return rows.some(isJobActive) ? 10_000 : false;
        },
    });

    const cancel = useMutation({
        mutationFn: (id: string) => cancelWarmupBatch(id),
        onSuccess: () => {
            toast.success("Batch cancellation requested");
            qc.invalidateQueries({ queryKey: ["admin", "warmup-content", "jobs"] });
        },
        onError: (err: Error) => toast.error(err.message || "Failed to cancel job"),
    });

    const columns: Column<WarmupGenerationJob>[] = useMemo(
        () => [
            {
                id: "status",
                header: "Status",
                cell: (j) => (
                    <Badge variant="outline" className={`text-[10px] ${jobTone(j.status)}`}>
                        {j.status}
                    </Badge>
                ),
                csv: (j) => j.status,
            },
            {
                id: "mode",
                header: "Mode",
                cell: (j) => (
                    <Badge
                        variant="outline"
                        className={`text-[10px] ${
                            j.mode === "batch"
                                ? "border-sky-300 bg-sky-50 text-sky-700"
                                : "border-zinc-300 text-zinc-600"
                        }`}
                    >
                        {j.mode ?? "sync"}
                    </Badge>
                ),
                csv: (j) => j.mode ?? "sync",
            },
            {
                id: "batch_status",
                header: "Batch",
                cell: (j) =>
                    j.mode === "batch" && j.batch_status ? (
                        <Badge
                            variant="outline"
                            className={`text-[10px] ${batchTone(j.batch_status)}`}
                        >
                            {j.batch_status}
                        </Badge>
                    ) : (
                        <span className="text-xs text-muted-foreground">—</span>
                    ),
                csv: (j) => j.batch_status ?? "",
            },
            {
                id: "pool",
                header: "Pool",
                cell: (j) => <PoolBadge pool={j.pool_type} />,
                csv: (j) => j.pool_type,
            },
            {
                id: "segment",
                header: "Segment",
                cell: (j) => <span className="text-xs">{j.segment || "—"}</span>,
                csv: (j) => j.segment,
            },
            {
                id: "trigger",
                header: "Trigger",
                cell: (j) => (
                    <span className="text-xs text-muted-foreground">
                        {j.trigger || "—"}
                    </span>
                ),
                csv: (j) => j.trigger,
            },
            {
                id: "model",
                header: "Model",
                cell: (j) => <span className="text-xs">{j.model || "—"}</span>,
                csv: (j) => j.model,
            },
            {
                id: "counts",
                header: "Generated / Requested",
                align: "right",
                cell: (j) => (
                    <span className="tabular-nums">
                        {j.generated_count} / {j.requested_count}
                    </span>
                ),
                csv: (j) => `${j.generated_count}/${j.requested_count}`,
            },
            {
                id: "rejected",
                header: "Lint rej.",
                align: "right",
                cell: (j) => (
                    <span
                        className={`tabular-nums ${
                            j.lint_rejected_count > 0
                                ? "text-amber-700"
                                : "text-muted-foreground"
                        }`}
                    >
                        {j.lint_rejected_count}
                    </span>
                ),
                csv: (j) => j.lint_rejected_count,
            },
            {
                id: "failed",
                header: "Failed",
                align: "right",
                cell: (j) => (
                    <span
                        className={`tabular-nums ${
                            j.failed_count > 0 ? "text-red-700" : "text-muted-foreground"
                        }`}
                    >
                        {j.failed_count}
                    </span>
                ),
                csv: (j) => j.failed_count,
            },
            {
                id: "window",
                header: "Window",
                cell: (j) => (
                    <span className="text-xs text-muted-foreground">
                        {j.completion_window || "—"}
                    </span>
                ),
                csv: (j) => j.completion_window ?? "",
                defaultHidden: true,
            },
            {
                id: "batch_id",
                header: "Batch ID",
                cell: (j) => (
                    <span className="font-mono text-[11px] text-muted-foreground">
                        {j.batch_id || "—"}
                    </span>
                ),
                csv: (j) => j.batch_id ?? "",
                defaultHidden: true,
            },
            {
                id: "started",
                header: "Started",
                cell: (j) => (
                    <span className="text-xs text-muted-foreground">
                        {fmtDate(j.started_at)}
                    </span>
                ),
                csv: (j) => j.started_at ?? "",
            },
            {
                id: "finished",
                header: "Finished",
                cell: (j) => (
                    <span className="text-xs text-muted-foreground">
                        {fmtDate(j.finished_at)}
                    </span>
                ),
                csv: (j) => j.finished_at ?? "",
            },
            {
                id: "error",
                header: "Error",
                cell: (j) =>
                    j.error ? (
                        <span className="text-xs text-red-700" title={j.error}>
                            {j.error.length > 60 ? `${j.error.slice(0, 60)}…` : j.error}
                        </span>
                    ) : (
                        <span className="text-xs text-muted-foreground">—</span>
                    ),
                csv: (j) => j.error,
                defaultHidden: true,
            },
            {
                id: "created",
                header: "Created",
                cell: (j) => (
                    <span className="text-xs text-muted-foreground">
                        {new Date(j.created_at).toLocaleString()}
                    </span>
                ),
                csv: (j) => j.created_at,
                defaultHidden: true,
            },
            {
                id: "actions",
                header: "Actions",
                align: "right",
                cell: (j) =>
                    isJobCancellable(j) ? (
                        <div
                            className="flex justify-end"
                            onClick={(e) => e.stopPropagation()}
                        >
                            <Button
                                size="xs"
                                variant="outline"
                                className="text-red-700 hover:bg-red-50"
                                onClick={() => cancel.mutate(j.id)}
                                disabled={cancel.isPending}
                            >
                                <Ban className="size-3" /> Cancel
                            </Button>
                        </div>
                    ) : (
                        <span className="block text-right text-xs text-muted-foreground">
                            —
                        </span>
                    ),
            },
        ],
        [cancel],
    );

    const rows = data?.data ?? [];

    return (
        <DataTable
            columns={columns}
            rows={rows}
            getRowId={(j) => j.id}
            loading={isLoading}
            error={error}
            onRetry={() => refetch()}
            errorTitle="Failed to load jobs"
            storageKey="admin.warmup-content.jobs"
            csvName="warmbly-warmup-content-jobs"
            noun="jobs"
            emptyTitle="No generation jobs"
            emptyHint="Queue a job from the Generate tab to see it here."
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
    );
}
