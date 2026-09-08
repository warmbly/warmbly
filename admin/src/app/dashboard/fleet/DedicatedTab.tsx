// Dedicated bindings: which worker is reserved for which workspace. Keyed
// under ["admin","workers"] so the realtime workers spine refreshes it; no poll.

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import { Plus, Unlink } from "lucide-react";
import { Button } from "@/components/ui/button";
import { DataTable, type Column } from "@/components/data/DataTable";
import { useConfirm } from "@/components/ConfirmDialog";
import {
    listDedicatedAssignments,
    releaseDedicatedWorker,
    type AdminDedicatedAssignment,
} from "@/lib/api/client/admin/fleet";
import { LiveDot } from "./tones";
import { fmtDate, shortId } from "./format";
import { ConvertDedicatedDialog } from "./ConvertDedicatedDialog";

export function DedicatedTab() {
    const qc = useQueryClient();
    const confirm = useConfirm();
    const [convertOpen, setConvertOpen] = useState(false);

    const { data, isLoading, error, refetch } = useQuery({
        queryKey: ["admin", "workers", "dedicated"],
        queryFn: listDedicatedAssignments,
    });

    const release = useMutation({
        mutationFn: (orgId: string) => releaseDedicatedWorker(orgId),
        onSuccess: (res) => {
            toast.success(
                `${res.accounts_moved} mailbox${res.accounts_moved === 1 ? "" : "es"} moved to shared workers` +
                    (res.returned_to_shared ? "; worker returned to the shared pool" : "; worker still bound to another workspace"),
            );
            qc.invalidateQueries({ queryKey: ["admin", "workers"] });
            qc.invalidateQueries({ queryKey: ["admin", "fleet"] });
        },
        onError: (e: Error) => toast.error(e.message || "Release failed"),
    });

    async function onRelease(a: AdminDedicatedAssignment) {
        const ok = await confirm({
            title: `Release ${a.worker_name || shortId(a.worker_id)} from ${a.organization_name}?`,
            description: `The workspace's ${a.account_count} mailbox${a.account_count === 1 ? "" : "es"} move back onto shared premium workers and the worker returns to the shared pool once no other workspace binds it. A mailbox with no live shared target stays where it is.`,
            confirmLabel: "Release",
            destructive: true,
        });
        if (ok) release.mutate(a.organization_id);
    }

    const columns: Column<AdminDedicatedAssignment>[] = [
        {
            id: "worker",
            header: "Worker",
            cell: (a) => (
                <div>
                    <Link to={`/workers/${a.worker_id}`} className="font-medium text-[var(--admin-accent-strong)] hover:underline">
                        {a.worker_name || shortId(a.worker_id)}
                    </Link>
                    <div className="font-mono text-[10px] text-muted-foreground">{a.worker_id}</div>
                </div>
            ),
            csv: (a) => a.worker_name || a.worker_id,
        },
        { id: "live", header: "Live", cell: (a) => <LiveDot live={a.worker_live} />, csv: (a) => (a.worker_live ? "live" : "offline") },
        {
            id: "org",
            header: "Workspace",
            cell: (a) => (
                <Link to={`/organizations/${a.organization_id}`} className="font-medium text-[var(--admin-accent-strong)] hover:underline">
                    {a.organization_name || shortId(a.organization_id)}
                </Link>
            ),
            csv: (a) => a.organization_name,
        },
        {
            id: "sub",
            header: "Subscription",
            cell: (a) => (
                <span className="font-mono text-[11px] text-muted-foreground" title={a.subscription_id}>
                    {shortId(a.subscription_id)}
                </span>
            ),
            csv: (a) => a.subscription_id,
        },
        { id: "assigned", header: "Assigned", cell: (a) => <span className="text-xs text-muted-foreground">{fmtDate(a.assigned_at)}</span>, csv: (a) => a.assigned_at },
        { id: "accounts", header: "Accounts", align: "right", cell: (a) => <span className="tabular-nums">{a.account_count}</span>, csv: (a) => a.account_count },
        {
            id: "actions",
            header: "",
            align: "right",
            cell: (a) => (
                <Button
                    size="xs"
                    variant="outline"
                    onClick={(e) => {
                        e.stopPropagation();
                        void onRelease(a);
                    }}
                    disabled={release.isPending}
                >
                    <Unlink className="size-3" />
                    Release
                </Button>
            ),
        },
    ];

    return (
        <div>
            <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
                <p className="text-[12.5px] text-muted-foreground max-w-2xl">
                    A dedicated worker carries one workspace's mailboxes and nothing else, so its IP reputation is that
                    workspace's alone. Placement still respects the mailbox tier and the warmup pool policy.
                </p>
                <Button size="sm" onClick={() => setConvertOpen(true)}>
                    <Plus className="size-4" />
                    Convert a worker to dedicated
                </Button>
            </div>
            <DataTable
                columns={columns}
                rows={data?.data ?? []}
                getRowId={(a) => a.id}
                loading={isLoading}
                error={error}
                onRetry={() => refetch()}
                errorTitle="Failed to load dedicated assignments"
                storageKey="admin.fleet.dedicated"
                csvName="warmbly-dedicated-workers"
                noun="assignments"
                emptyTitle="No dedicated workers"
                emptyHint="Every worker is shared. Convert one above to reserve it for a single workspace."
            />
            <ConvertDedicatedDialog open={convertOpen} onOpenChange={setConvertOpen} />
        </div>
    );
}
