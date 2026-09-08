// Task dead letters: tasks that exhausted their retries, with a replay per
// pending row. Nothing publishes an event when one is written, so it polls
// at 60s.

import { useEffect, useState } from "react";
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import { RotateCcw } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { DataTable, type Column } from "@/components/data/DataTable";
import { useConfirm } from "@/components/ConfirmDialog";
import { useCursorPager } from "@/lib/useCursorPager";
import {
    listDeadLetters,
    replayDeadLetter,
    type AdminDeadLetterRow,
    type AdminDeadLetterStatus,
} from "@/lib/api/client/admin/sends";
import { StatusSegments } from "@/app/dashboard/sends/StatusSegments";
import { ExpandableText } from "@/app/dashboard/jobs/ExpandableText";
import { absolute, relative, shortId } from "@/app/dashboard/jobs/format";

type StatusFilter = AdminDeadLetterStatus | "all";

const STATUS_TONE: Record<string, string> = {
    pending: "border-amber-300 bg-amber-50 text-amber-700",
    replayed: "border-emerald-300 bg-emerald-50 text-emerald-700",
    failed: "border-red-300 bg-red-50 text-red-700",
};

export function DeadLettersTab() {
    const qc = useQueryClient();
    const confirm = useConfirm();
    const [status, setStatus] = useState<StatusFilter>("all");
    const pager = useCursorPager();
    const { reset } = pager;

    useEffect(() => {
        reset();
    }, [status, reset]);

    const { data, isLoading, error, refetch } = useQuery({
        queryKey: ["admin", "sends", "dead-letters", status, pager.cursor],
        queryFn: () => listDeadLetters({ status, cursor: pager.cursor, limit: 50 }),
        refetchInterval: 60_000,
        placeholderData: keepPreviousData,
    });

    const replay = useMutation({
        mutationFn: (row: AdminDeadLetterRow) => replayDeadLetter(row.id),
        onSuccess: () => {
            toast.success("Dead letter replayed; the task is back on the queue");
            qc.invalidateQueries({ queryKey: ["admin", "sends", "dead-letters"] });
        },
        onError: (err: Error) => toast.error(err.message || "Failed to replay"),
    });

    async function onReplay(row: AdminDeadLetterRow) {
        const ok = await confirm({
            title: "Replay this dead letter?",
            description: `Re-dispatches the ${row.task_type} task${row.organization_name ? ` for ${row.organization_name}` : ""}. If it is a send, this puts mail on the wire again, so make sure the failure it died on is fixed first. Last error: ${row.last_error || "none recorded"}`,
            confirmLabel: "Replay",
            destructive: true,
        });
        if (!ok) return;
        replay.mutate(row);
    }

    const rows = data?.data ?? [];
    const counts = {
        pending: data?.pending ?? 0,
        replayed: data?.replayed ?? 0,
        failed: data?.failed ?? 0,
    };

    const columns: Column<AdminDeadLetterRow>[] = [
        {
            id: "type",
            header: "Task",
            cell: (r) => (
                <div>
                    <div className="font-mono text-xs">{r.task_type}</div>
                    <div className="font-mono text-[10px] text-muted-foreground">{shortId(r.task_id)}</div>
                </div>
            ),
            csv: (r) => r.task_type,
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
        {
            id: "error",
            header: "Last error",
            className: "max-w-md",
            cell: (r) => <ExpandableText text={r.last_error} mono />,
            csv: (r) => r.last_error,
        },
        {
            id: "attempts",
            header: "Attempts",
            align: "right",
            cell: (r) => (
                <span className="text-xs tabular-nums">
                    {r.attempts} / {r.max_attempts}
                </span>
            ),
            csv: (r) => `${r.attempts}/${r.max_attempts}`,
        },
        {
            id: "status",
            header: "Status",
            cell: (r) => (
                <Badge variant="outline" className={`text-[10px] ${STATUS_TONE[r.status] ?? "border-zinc-300 text-zinc-600"}`}>
                    {r.status}
                </Badge>
            ),
            csv: (r) => r.status,
        },
        {
            id: "next_retry",
            header: "Next retry",
            cell: (r) => (
                <span className="text-xs text-muted-foreground" title={absolute(r.next_retry_at)}>
                    {relative(r.next_retry_at, "—")}
                </span>
            ),
            csv: (r) => r.next_retry_at || "",
        },
        {
            id: "created",
            header: "Created",
            cell: (r) => (
                <span className="text-xs text-muted-foreground" title={absolute(r.created_at)}>
                    {relative(r.created_at)}
                </span>
            ),
            csv: (r) => r.created_at,
        },
        {
            id: "replayed_at",
            header: "Replayed",
            defaultHidden: true,
            cell: (r) => <span className="text-xs text-muted-foreground">{relative(r.replayed_at, "—")}</span>,
            csv: (r) => r.replayed_at || "",
        },
        {
            id: "actions",
            header: "",
            align: "right",
            cell: (r) =>
                r.status === "pending" ? (
                    <Button
                        size="xs"
                        variant="outline"
                        disabled={replay.isPending && replay.variables?.id === r.id}
                        onClick={(e) => {
                            e.stopPropagation();
                            void onReplay(r);
                        }}
                        title="Re-dispatch this task"
                    >
                        <RotateCcw className="size-3" /> Replay
                    </Button>
                ) : null,
        },
    ];

    return (
        <div>
            <div className="mb-4 flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
                <p className="max-w-2xl text-sm text-muted-foreground">
                    Tasks that exhausted their retries. A pending row can be replayed once the cause is fixed; replayed and failed rows are kept for the record.
                </p>
                <StatusSegments
                    value={status}
                    onChange={setStatus}
                    options={[
                        { value: "all", label: "All", count: counts.pending + counts.replayed + counts.failed },
                        { value: "pending", label: "Pending", count: counts.pending },
                        { value: "replayed", label: "Replayed", count: counts.replayed },
                        { value: "failed", label: "Failed", count: counts.failed },
                    ]}
                />
            </div>

            <DataTable
                columns={columns}
                rows={rows}
                getRowId={(r) => r.id}
                loading={isLoading}
                error={error}
                onRetry={() => refetch()}
                errorTitle="Failed to load dead letters"
                storageKey="admin.sends.dead-letters"
                csvName="warmbly-dead-letters"
                noun="dead letters"
                emptyTitle="No dead letters"
                emptyHint={
                    status === "all"
                        ? "No task has exhausted its retries on this instance. Rows appear when the task runner gives up on one."
                        : `No ${status} dead letters.`
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
        </div>
    );
}
