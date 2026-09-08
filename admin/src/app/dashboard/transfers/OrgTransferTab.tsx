// One workspace's exports and imports, with the export dialog and the
// import flow. Polls at 15s only while a job is queued or running.

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Download, PackageOpen, Trash2, Upload } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { ErrorState } from "@/components/ErrorState";
import { useConfirm } from "@/components/ConfirmDialog";
import {
    deleteOrgExport,
    downloadOrgExport,
    formatBytes,
    isTransferActive,
    listOrgExports,
    listOrgImports,
    saveBlob,
    totalRows,
    type OrgExportJob,
    type OrgImportJob,
} from "@/lib/api/client/admin/transfers";
import { ExportDialog } from "./ExportDialog";
import { ImportArchiveDialog } from "./ImportArchiveDialog";
import { StatusPill } from "./TransferPills";
import { fmtDateTime } from "../fleet/format";

const POLL = 15_000;

export function OrgTransferTab({ orgId, orgName }: { orgId: string; orgName: string }) {
    const qc = useQueryClient();
    const confirm = useConfirm();
    const [exportOpen, setExportOpen] = useState(false);
    const [importOpen, setImportOpen] = useState(false);

    const exportsQ = useQuery({
        queryKey: ["admin", "transfers", "org", orgId, "exports"],
        queryFn: () => listOrgExports(orgId),
        refetchInterval: (q) => ((q.state.data?.data ?? []).some((j) => isTransferActive(j.status)) ? POLL : false),
    });
    const importsQ = useQuery({
        queryKey: ["admin", "transfers", "org", orgId, "imports"],
        queryFn: () => listOrgImports(orgId),
        refetchInterval: (q) => ((q.state.data?.data ?? []).some((j) => isTransferActive(j.status)) ? POLL : false),
    });

    const invalidate = () => qc.invalidateQueries({ queryKey: ["admin", "transfers"] });

    const [downloading, setDownloading] = useState<string | null>(null);
    async function onDownload(job: OrgExportJob) {
        setDownloading(job.id);
        try {
            const { blob, filename } = await downloadOrgExport(orgId, job.id, orgName);
            saveBlob(blob, filename);
        } catch (e) {
            toast.error((e as Error).message || "Download failed");
        } finally {
            setDownloading(null);
        }
    }

    const del = useMutation({
        mutationFn: (id: string) => deleteOrgExport(orgId, id),
        onSuccess: () => {
            toast.success("Archive deleted");
            invalidate();
        },
        onError: (e: Error) => toast.error(e.message || "Delete failed"),
    });

    async function onDelete(job: OrgExportJob) {
        const ok = await confirm({
            title: "Delete this archive?",
            description: "The stored file is removed. The job stays in the history as deleted; run another export to rebuild it.",
            confirmLabel: "Delete",
            destructive: true,
        });
        if (ok) del.mutate(job.id);
    }

    const exports = exportsQ.data?.data ?? [];
    const imports = importsQ.data?.data ?? [];

    return (
        <div className="space-y-6">
            <p className="max-w-2xl text-[12.5px] text-muted-foreground">
                The same archives the owner builds from Settings &gt; Data, started here on their behalf. Finished
                exports stay downloadable for 7 days.
            </p>

            <section>
                <div className="mb-2 flex items-center justify-between">
                    <h2 className="text-sm font-semibold">
                        Exports{exports.length > 0 && <span className="ml-1.5 font-normal text-muted-foreground">({exports.length})</span>}
                    </h2>
                    <Button size="sm" onClick={() => setExportOpen(true)}>
                        <PackageOpen className="size-4" />
                        Export
                    </Button>
                </div>
                {exportsQ.isLoading ? (
                    <Skeleton className="h-24 w-full" />
                ) : exportsQ.error ? (
                    <ErrorState error={exportsQ.error} title="Failed to load exports" onRetry={() => exportsQ.refetch()} />
                ) : exports.length === 0 ? (
                    <Empty>No exports yet. Start one above to build a portable archive of this workspace.</Empty>
                ) : (
                    <div className="overflow-hidden rounded-lg border border-border bg-card">
                        <div className="overflow-x-auto">
                            <table className="w-full text-sm">
                                <thead className="bg-muted/40 text-[10.5px] font-semibold uppercase tracking-wider text-muted-foreground">
                                    <tr>
                                        <th className="px-3 py-2 text-left">Status</th>
                                        <th className="px-3 py-2 text-left">Groups</th>
                                        <th className="px-3 py-2 text-left">Secrets</th>
                                        <th className="px-3 py-2 text-right">Rows</th>
                                        <th className="px-3 py-2 text-right">Size</th>
                                        <th className="px-3 py-2 text-left">Started</th>
                                        <th className="px-3 py-2 text-left">Expires</th>
                                        <th className="px-3 py-2 text-right" />
                                    </tr>
                                </thead>
                                <tbody>
                                    {exports.map((j) => (
                                        <tr key={j.id} className="border-t border-border">
                                            <td className="px-3 py-2">
                                                <StatusPill status={j.status} progress={j.progress_percent} stage={j.progress_stage} />
                                                {j.error_message && <div className="mt-0.5 max-w-xs text-[11px] text-red-600">{j.error_message}</div>}
                                            </td>
                                            <td className="px-3 py-2 text-xs">{j.groups?.length ?? 0}</td>
                                            <td className="px-3 py-2 text-xs">{j.include_secrets ? "yes" : "no"}</td>
                                            <td className="px-3 py-2 text-right text-xs tabular-nums">{totalRows(j.row_counts).toLocaleString()}</td>
                                            <td className="px-3 py-2 text-right text-xs tabular-nums">{formatBytes(j.archive_bytes)}</td>
                                            <td className="px-3 py-2 text-xs text-muted-foreground">{fmtDateTime(j.started_at ?? j.created_at)}</td>
                                            <td className="px-3 py-2 text-xs text-muted-foreground">{fmtDateTime(j.expires_at)}</td>
                                            <td className="px-3 py-2 text-right">
                                                <div className="flex justify-end gap-1">
                                                    <Button
                                                        size="xs"
                                                        variant="outline"
                                                        disabled={j.status !== "completed" || downloading === j.id}
                                                        onClick={() => void onDownload(j)}
                                                    >
                                                        <Download className="size-3" />
                                                        {downloading === j.id ? "Fetching…" : "Download"}
                                                    </Button>
                                                    <Button
                                                        size="xs"
                                                        variant="outline"
                                                        className="text-red-700 hover:bg-red-50"
                                                        disabled={isTransferActive(j.status) || del.isPending}
                                                        onClick={() => void onDelete(j)}
                                                    >
                                                        <Trash2 className="size-3" />
                                                    </Button>
                                                </div>
                                            </td>
                                        </tr>
                                    ))}
                                </tbody>
                            </table>
                        </div>
                    </div>
                )}
            </section>

            <section>
                <div className="mb-2 flex items-center justify-between">
                    <h2 className="text-sm font-semibold">
                        Imports{imports.length > 0 && <span className="ml-1.5 font-normal text-muted-foreground">({imports.length})</span>}
                    </h2>
                    <Button size="sm" variant="outline" onClick={() => setImportOpen(true)}>
                        <Upload className="size-4" />
                        Import archive
                    </Button>
                </div>
                {importsQ.isLoading ? (
                    <Skeleton className="h-24 w-full" />
                ) : importsQ.error ? (
                    <ErrorState error={importsQ.error} title="Failed to load imports" onRetry={() => importsQ.refetch()} />
                ) : imports.length === 0 ? (
                    <Empty>No imports yet. Apply an archive from another instance (or an older export) above.</Empty>
                ) : (
                    <div className="overflow-hidden rounded-lg border border-border bg-card">
                        <div className="overflow-x-auto">
                            <table className="w-full text-sm">
                                <thead className="bg-muted/40 text-[10.5px] font-semibold uppercase tracking-wider text-muted-foreground">
                                    <tr>
                                        <th className="px-3 py-2 text-left">Status</th>
                                        <th className="px-3 py-2 text-left">Source</th>
                                        <th className="px-3 py-2 text-left">Groups</th>
                                        <th className="px-3 py-2 text-left">Conflicts</th>
                                        <th className="px-3 py-2 text-right">Rows</th>
                                        <th className="px-3 py-2 text-right">Size</th>
                                        <th className="px-3 py-2 text-left">Started</th>
                                        <th className="px-3 py-2 text-left">Completed</th>
                                    </tr>
                                </thead>
                                <tbody>
                                    {imports.map((j: OrgImportJob) => (
                                        <tr key={j.id} className="border-t border-border">
                                            <td className="px-3 py-2">
                                                <StatusPill status={j.status} progress={j.progress_percent} stage={j.progress_stage} />
                                                {j.error_message && <div className="mt-0.5 max-w-xs text-[11px] text-red-600">{j.error_message}</div>}
                                                {(j.warnings?.length ?? 0) > 0 && (
                                                    <div className="mt-0.5 max-w-xs text-[11px] text-amber-700">{j.warnings!.join(" · ")}</div>
                                                )}
                                            </td>
                                            <td className="px-3 py-2 text-xs">
                                                {j.source_manifest ? (
                                                    <>
                                                        <div>{j.source_manifest.organization_name}</div>
                                                        <div className="text-[10px] text-muted-foreground">{j.source_manifest.source_instance || "unknown instance"}</div>
                                                    </>
                                                ) : (
                                                    "—"
                                                )}
                                            </td>
                                            <td className="px-3 py-2 text-xs">{j.groups?.length ?? 0}</td>
                                            <td className="px-3 py-2 text-xs">{j.conflict_strategy}</td>
                                            <td className="px-3 py-2 text-right text-xs tabular-nums">{totalRows(j.row_counts).toLocaleString()}</td>
                                            <td className="px-3 py-2 text-right text-xs tabular-nums">{formatBytes(j.archive_bytes)}</td>
                                            <td className="px-3 py-2 text-xs text-muted-foreground">{fmtDateTime(j.started_at ?? j.created_at)}</td>
                                            <td className="px-3 py-2 text-xs text-muted-foreground">{fmtDateTime(j.completed_at)}</td>
                                        </tr>
                                    ))}
                                </tbody>
                            </table>
                        </div>
                    </div>
                )}
            </section>

            <ExportDialog open={exportOpen} onOpenChange={setExportOpen} org={{ id: orgId, name: orgName }} onStarted={invalidate} />
            <ImportArchiveDialog open={importOpen} onOpenChange={setImportOpen} orgId={orgId} orgName={orgName} onStarted={invalidate} />
        </div>
    );
}

function Empty({ children }: { children: React.ReactNode }) {
    return <div className="rounded-md border border-border bg-card p-4 text-sm text-muted-foreground">{children}</div>;
}
