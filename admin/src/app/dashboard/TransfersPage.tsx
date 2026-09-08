// Transfers: every workspace export and import on the instance. Exports can
// be downloaded and deleted here; imports are started from the workspace's
// own page (Organizations > workspace > Transfer) because they need a
// destination. Polls at 15s only while a job is queued or running.

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import { Download, ExternalLink, PackageOpen, Terminal, Trash2 } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { Button } from "@/components/ui/button";
import { DataTable, type Column } from "@/components/data/DataTable";
import { useConfirm } from "@/components/ConfirmDialog";
import { docsUrl } from "@/lib/docs";
import {
    deleteOrgExport,
    downloadOrgExport,
    formatBytes,
    isTransferActive,
    listTransfers,
    saveBlob,
    type AdminTransferJob,
} from "@/lib/api/client/admin/transfers";
import { ExportDialog } from "./transfers/ExportDialog";
import { KindPill, StatusPill } from "./transfers/TransferPills";
import { fmtDateTime, shortId } from "./fleet/format";

const POLL = 15_000;

export default function TransfersPage() {
    const qc = useQueryClient();
    const confirm = useConfirm();
    const [exportOpen, setExportOpen] = useState(false);
    const [downloading, setDownloading] = useState<string | null>(null);

    const { data, isLoading, error, refetch } = useQuery({
        queryKey: ["admin", "transfers", "all"],
        queryFn: () => listTransfers(200),
        refetchInterval: (q) => ((q.state.data?.data ?? []).some((j) => isTransferActive(j.status)) ? POLL : false),
    });
    const rows = data?.data ?? [];
    const active = rows.filter((j) => isTransferActive(j.status)).length;

    const invalidate = () => qc.invalidateQueries({ queryKey: ["admin", "transfers"] });

    async function onDownload(j: AdminTransferJob) {
        setDownloading(j.id);
        try {
            const { blob, filename } = await downloadOrgExport(j.organization_id, j.id, j.organization_name);
            saveBlob(blob, filename);
        } catch (e) {
            toast.error((e as Error).message || "Download failed");
        } finally {
            setDownloading(null);
        }
    }

    const del = useMutation({
        mutationFn: (j: AdminTransferJob) => deleteOrgExport(j.organization_id, j.id),
        onSuccess: () => {
            toast.success("Archive deleted");
            invalidate();
        },
        onError: (e: Error) => toast.error(e.message || "Delete failed"),
    });

    async function onDelete(j: AdminTransferJob) {
        const ok = await confirm({
            title: `Delete the archive for ${j.organization_name}?`,
            description: "The stored file is removed and cannot be downloaded again. Run another export to rebuild it.",
            confirmLabel: "Delete",
            destructive: true,
        });
        if (ok) del.mutate(j);
    }

    const columns: Column<AdminTransferJob>[] = [
        { id: "kind", header: "Kind", cell: (j) => <KindPill kind={j.kind} />, csv: (j) => j.kind },
        {
            id: "org",
            header: "Workspace",
            cell: (j) => (
                <div>
                    <Link
                        to={`/organizations/${j.organization_id}?tab=transfer`}
                        onClick={(e) => e.stopPropagation()}
                        className="font-medium text-[var(--admin-accent-strong)] hover:underline"
                    >
                        {j.organization_name || shortId(j.organization_id)}
                    </Link>
                    <div className="font-mono text-[10px] text-muted-foreground">{shortId(j.id)}</div>
                </div>
            ),
            csv: (j) => j.organization_name,
        },
        {
            id: "by",
            header: "Requested by",
            cell: (j) => <span className="text-xs">{j.requested_by_email || <span className="text-muted-foreground">system</span>}</span>,
            csv: (j) => j.requested_by_email,
        },
        {
            id: "status",
            header: "Status",
            cell: (j) => (
                <div>
                    <StatusPill status={j.status} progress={j.progress_percent} stage={j.progress_stage} />
                    {j.error_message && <div className="mt-0.5 max-w-xs text-[11px] text-red-600">{j.error_message}</div>}
                </div>
            ),
            csv: (j) => j.status,
        },
        { id: "size", header: "Size", align: "right", cell: (j) => <span className="text-xs tabular-nums">{formatBytes(j.archive_bytes)}</span>, csv: (j) => j.archive_bytes ?? "" },
        { id: "groups", header: "Groups", align: "right", cell: (j) => <span className="text-xs tabular-nums">{j.groups?.length ?? 0}</span>, csv: (j) => j.groups?.length ?? 0 },
        { id: "secrets", header: "Secrets", cell: (j) => <span className="text-xs">{j.include_secrets ? "yes" : "no"}</span>, csv: (j) => (j.include_secrets ? "yes" : "no") },
        { id: "started", header: "Started", cell: (j) => <span className="text-xs text-muted-foreground">{fmtDateTime(j.started_at ?? j.created_at)}</span>, csv: (j) => j.started_at ?? j.created_at },
        { id: "completed", header: "Completed", cell: (j) => <span className="text-xs text-muted-foreground">{fmtDateTime(j.completed_at)}</span>, csv: (j) => j.completed_at ?? "" },
        { id: "expires", header: "Expires", cell: (j) => <span className="text-xs text-muted-foreground">{fmtDateTime(j.expires_at)}</span>, csv: (j) => j.expires_at ?? "", defaultHidden: true },
        {
            id: "actions",
            header: "",
            align: "right",
            cell: (j) =>
                j.kind === "export" ? (
                    <div className="flex justify-end gap-1">
                        <Button
                            size="xs"
                            variant="outline"
                            disabled={j.status !== "completed" || downloading === j.id}
                            onClick={(e) => {
                                e.stopPropagation();
                                void onDownload(j);
                            }}
                        >
                            <Download className="size-3" />
                            {downloading === j.id ? "Fetching…" : "Download"}
                        </Button>
                        <Button
                            size="xs"
                            variant="outline"
                            className="text-red-700 hover:bg-red-50"
                            disabled={isTransferActive(j.status) || del.isPending}
                            onClick={(e) => {
                                e.stopPropagation();
                                void onDelete(j);
                            }}
                            aria-label="Delete archive"
                        >
                            <Trash2 className="size-3" />
                        </Button>
                    </div>
                ) : null,
        },
    ];

    return (
        <div>
            <PageHeader
                title="Transfers"
                description="Workspace archives across the instance: the same exports and imports owners build from Settings > Data, started by them or by an operator."
            >
                <Button size="sm" onClick={() => setExportOpen(true)}>
                    <PackageOpen className="size-4" />
                    Export a workspace
                </Button>
            </PageHeader>

            <div className="mb-4 flex items-start gap-2 rounded-md border border-border bg-card px-3 py-2.5 text-[12.5px] text-muted-foreground">
                <Terminal className="mt-0.5 size-4 shrink-0" />
                <span>
                    These move one workspace at a time. A whole-instance backup, including every workspace, the
                    encryption keys and blob storage, is <code>warmblyctl backup</code> and <code>warmblyctl restore</code>{" "}
                    from a shell on the host.{" "}
                    <a
                        href={docsUrl("/development/warmblyctl/")}
                        target="_blank"
                        rel="noreferrer"
                        className="inline-flex items-center gap-0.5 text-[var(--admin-accent-strong)] hover:underline"
                    >
                        warmblyctl docs <ExternalLink className="size-3" />
                    </a>
                    {active > 0 && <span className="ml-1.5 tabular-nums">· {active} running, refreshing every 15s</span>}
                </span>
            </div>

            <DataTable
                columns={columns}
                rows={rows}
                getRowId={(j) => `${j.kind}:${j.id}`}
                loading={isLoading}
                error={error}
                onRetry={() => refetch()}
                errorTitle="Failed to load transfers"
                storageKey="admin.transfers"
                csvName="warmbly-transfers"
                noun="jobs"
                emptyTitle="No transfers"
                emptyHint="No workspace has been exported or imported on this instance yet. Start an export above, or import on a workspace's Transfer tab."
            />

            <ExportDialog open={exportOpen} onOpenChange={setExportOpen} onStarted={invalidate} />
        </div>
    );
}
