// Export and import history, with live progress on anything still running.

import React from "react";
import toast from "react-hot-toast";
import {
    AlertTriangleIcon,
    DownloadIcon,
    Loader2Icon,
    Trash2Icon,
    UploadIcon,
} from "lucide-react";
import { useConfirm } from "@/hooks/context/confirm";
import { downloadOrgExport } from "@/lib/api/client/app/orgtransfer/orgTransfer";
import { useDeleteOrgExport } from "@/lib/api/hooks/app/orgtransfer/useOrgTransfer";
import type { OrgExportJob, OrgImportJob } from "@/lib/api/models/app/orgtransfer/OrgTransfer";
import { totalRows } from "@/lib/api/models/app/orgtransfer/OrgTransfer";
import { formatBytes, formatExpiry } from "./format";

export default function TransferHistory({
    exports,
    imports,
    loading,
}: {
    exports: OrgExportJob[];
    imports: OrgImportJob[];
    loading?: boolean;
}) {
    if (loading) {
        return <div className="text-[12px] text-slate-400">Loading…</div>;
    }
    if (exports.length === 0 && imports.length === 0) {
        return (
            <div className="text-[12.5px] text-slate-500">
                No archives yet. Start an export above, or import one from another instance.
            </div>
        );
    }

    return (
        <div className="rounded-md border border-slate-200 divide-y divide-slate-200/70 overflow-hidden">
            {exports.map((job) => (
                <ExportRow key={job.id} job={job} />
            ))}
            {imports.map((job) => (
                <ImportRow key={job.id} job={job} />
            ))}
        </div>
    );
}

function ExportRow({ job }: { job: OrgExportJob }) {
    const confirm = useConfirm();
    const remove = useDeleteOrgExport();
    const [downloading, setDownloading] = React.useState(false);

    const running = job.status === "queued" || job.status === "running";

    async function download() {
        setDownloading(true);
        try {
            const { blob, filename } = await downloadOrgExport(job.id);
            // Object URL rather than a direct link, because the endpoint needs
            // the bearer token an anchor cannot carry.
            const url = URL.createObjectURL(blob);
            const a = document.createElement("a");
            a.href = url;
            a.download = filename;
            document.body.appendChild(a);
            a.click();
            a.remove();
            URL.revokeObjectURL(url);
        } catch {
            toast.error("That archive could not be downloaded.");
        } finally {
            setDownloading(false);
        }
    }

    return (
        <Row
            icon={<DownloadIcon className="w-3.5 h-3.5 text-slate-400" />}
            title="Export"
            job={job}
            detail={
                job.status === "completed"
                    ? [
                          `${totalRows(job.row_counts).toLocaleString()} rows`,
                          formatBytes(job.archive_bytes),
                          job.include_secrets ? "with credentials" : "no credentials",
                          formatExpiry(job.expires_at),
                      ]
                          .filter(Boolean)
                          .join(" · ")
                    : undefined
            }
            actions={
                <>
                    {job.status === "completed" && (
                        <button
                            type="button"
                            onClick={download}
                            disabled={downloading}
                            className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 inline-flex items-center gap-1.5 transition-colors disabled:opacity-50"
                        >
                            {downloading && <Loader2Icon className="w-3 h-3 animate-spin" />}
                            Download
                        </button>
                    )}
                    {!running && (
                        <button
                            type="button"
                            aria-label="Delete archive"
                            onClick={() =>
                                confirm.show(
                                    "Delete this archive? The file is removed from storage and cannot be downloaded again.",
                                    () => remove.mutateAsync(job.id),
                                )
                            }
                            className="h-7 w-7 rounded-md border border-slate-200 hover:border-red-300 text-slate-400 hover:text-red-600 inline-flex items-center justify-center transition-colors"
                        >
                            <Trash2Icon className="w-3.5 h-3.5" />
                        </button>
                    )}
                </>
            }
        />
    );
}

function ImportRow({ job }: { job: OrgImportJob }) {
    return (
        <Row
            icon={<UploadIcon className="w-3.5 h-3.5 text-slate-400" />}
            title={
                job.source_manifest
                    ? `Import from ${job.source_manifest.organization_name}`
                    : "Import"
            }
            job={job}
            detail={
                job.status === "completed"
                    ? `${totalRows(job.row_counts).toLocaleString()} rows applied`
                    : undefined
            }
            warnings={job.warnings}
        />
    );
}

function Row({
    icon,
    title,
    job,
    detail,
    actions,
    warnings,
}: {
    icon: React.ReactNode;
    title: string;
    job: OrgExportJob | OrgImportJob;
    detail?: string;
    actions?: React.ReactNode;
    warnings?: string[];
}) {
    const running = job.status === "queued" || job.status === "running";

    return (
        <div className="px-3 py-2.5">
            <div className="flex items-start gap-2.5">
                <span className="mt-0.5 shrink-0">{icon}</span>
                <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-1.5 flex-wrap">
                        <span className="text-[12.5px] font-medium text-slate-900">{title}</span>
                        <StatusPill status={job.status} />
                        <span className="text-[11px] text-slate-400">
                            {new Date(job.created_at).toLocaleString()}
                        </span>
                    </div>

                    {detail && (
                        <div className="text-[11.5px] text-slate-500 leading-tight mt-0.5">
                            {detail}
                        </div>
                    )}

                    {running && (
                        <div className="mt-1.5">
                            <div className="h-1 rounded-full bg-slate-100 overflow-hidden">
                                <div
                                    className="h-full bg-sky-500 transition-[width] duration-500"
                                    style={{ width: `${Math.max(2, job.progress_percent)}%` }}
                                />
                            </div>
                            <div className="text-[11px] text-slate-500 mt-1">
                                {job.progress_percent}% · {job.progress_stage || "starting"}
                            </div>
                        </div>
                    )}

                    {job.status === "failed" && job.error_message && (
                        <div className="flex items-start gap-1.5 text-[11.5px] text-red-700 mt-1">
                            <AlertTriangleIcon className="w-3 h-3 mt-0.5 shrink-0" />
                            <span className="leading-relaxed">{job.error_message}</span>
                        </div>
                    )}

                    {warnings?.map((w) => (
                        <div key={w} className="flex items-start gap-1.5 text-[11.5px] text-amber-700 mt-1">
                            <AlertTriangleIcon className="w-3 h-3 mt-0.5 shrink-0" />
                            <span className="leading-relaxed">{w}</span>
                        </div>
                    ))}
                </div>

                {actions && <div className="flex items-center gap-1.5 shrink-0">{actions}</div>}
            </div>
        </div>
    );
}

function StatusPill({ status }: { status: string }) {
    const tone =
        status === "completed"
            ? "bg-emerald-50 text-emerald-700"
            : status === "failed"
                ? "bg-red-50 text-red-700"
                : status === "expired"
                    ? "bg-slate-100 text-slate-500"
                    : "bg-sky-50 text-sky-700";
    return (
        <span
            className={`text-[10px] uppercase tracking-[0.08em] font-medium rounded-sm px-1 ${tone}`}
        >
            {status}
        </span>
    );
}
