// Recent task failures: what a task reported when it stopped, with the
// mailbox and workspace it belongs to. Polls at 60s; there is no event.

import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { Badge } from "@/components/ui/badge";
import { DataTable, type Column } from "@/components/data/DataTable";
import { listTaskFailures, type AdminTaskFailureRow } from "@/lib/api/client/admin/sends";
import { ExpandableText } from "@/app/dashboard/jobs/ExpandableText";
import { absolute, relative, shortId } from "@/app/dashboard/jobs/format";

const TASK_TONE: Record<string, string> = {
    pending: "border-amber-300 bg-amber-50 text-amber-700",
    processing: "border-amber-300 bg-amber-50 text-amber-700",
    completed: "border-emerald-300 bg-emerald-50 text-emerald-700",
    failed: "border-red-300 bg-red-50 text-red-700",
};

const columns: Column<AdminTaskFailureRow>[] = [
    {
        id: "title",
        header: "Failure",
        className: "max-w-md",
        cell: (r) => (
            <div>
                <div className="text-xs font-medium">{r.title || "Task failed"}</div>
                <ExpandableText text={r.message} className="text-muted-foreground" />
            </div>
        ),
        csv: (r) => `${r.title}: ${r.message}`,
    },
    {
        id: "task",
        header: "Task",
        cell: (r) => (
            <div className="flex items-center gap-1.5">
                <span className="font-mono text-xs">{r.task_type}</span>
                <Badge variant="outline" className={`text-[10px] ${TASK_TONE[r.task_status] ?? "border-zinc-300 text-zinc-600"}`}>
                    {r.task_status}
                </Badge>
            </div>
        ),
        csv: (r) => `${r.task_type} (${r.task_status})`,
    },
    {
        id: "mailbox",
        header: "Mailbox",
        cell: (r) => (
            <div>
                <div className="text-xs">{r.mailbox_email || "—"}</div>
                <div className="font-mono text-[10px] text-muted-foreground">{shortId(r.email_account_id)}</div>
            </div>
        ),
        csv: (r) => r.mailbox_email,
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
        id: "occurred",
        header: "Occurred",
        align: "right",
        cell: (r) => (
            <span className="text-xs text-muted-foreground" title={absolute(r.occurred_at)}>
                {relative(r.occurred_at)}
            </span>
        ),
        csv: (r) => r.occurred_at,
    },
];

export function FailuresTab() {
    const { data, isLoading, error, refetch } = useQuery({
        queryKey: ["admin", "sends", "failures"],
        queryFn: () => listTaskFailures(100),
        refetchInterval: 60_000,
    });
    const rows = data?.data ?? [];

    return (
        <div>
            <p className="mb-4 max-w-2xl text-sm text-muted-foreground">
                The most recent failures tasks recorded about themselves: auth errors, provider refusals, send exceptions. Newest first.
            </p>
            <DataTable
                columns={columns}
                rows={rows}
                getRowId={(r) => `${r.task_id}:${r.occurred_at}`}
                loading={isLoading}
                error={error}
                onRetry={() => refetch()}
                errorTitle="Failed to load task failures"
                storageKey="admin.sends.failures"
                csvName="warmbly-task-failures"
                noun="failures"
                emptyTitle="No recent failures"
                emptyHint="No task has recorded a failure recently. Rows appear when a send, sync or warmup task stops with an error."
            />
        </div>
    );
}
