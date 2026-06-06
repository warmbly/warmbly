// Per-step attachments editor (lives under the Step composer). Drag-and-drop or
// click to upload files for this step; lists each file with its name + size and
// a delete control, and shows the total size used across the step's files.
//
// Attachments are scoped to the step via sequence_id on upload; the list is the
// campaign-wide set filtered down to this step. Campaign-level attachments (no
// sequence_id) are not shown here.

import React from "react";
import { PaperclipIcon, UploadCloudIcon, Loader2Icon, Trash2Icon, FileIcon } from "lucide-react";
import toast from "react-hot-toast";
import {
    useCampaignAttachments,
    useUploadAttachment,
    useDeleteAttachment,
} from "@/lib/api/hooks/app/campaigns/useCampaignAttachments";
import type Attachment from "@/lib/api/models/app/campaigns/Attachment";
import { useConfirm } from "@/hooks/context/confirm";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import formatBytes from "@/lib/helper/formatBytes";

export default function StepAttachments({
    campaignId,
    sequenceId,
}: {
    campaignId: string;
    sequenceId: string;
}) {
    const { data: all, isLoading } = useCampaignAttachments(campaignId);
    const upload = useUploadAttachment(campaignId);
    const del = useDeleteAttachment(campaignId);
    const confirm = useConfirm();

    const inputRef = React.useRef<HTMLInputElement>(null);
    const [dragging, setDragging] = React.useState(false);

    const attachments = (all ?? []).filter((a) => a.sequence_id === sequenceId);
    const totalSize = attachments.reduce((sum, a) => sum + (a.size || 0), 0);

    const uploadFiles = React.useCallback(
        (files: FileList | File[]) => {
            const list = Array.from(files);
            if (list.length === 0) return;
            for (const file of list) {
                upload.mutate(
                    { file, opts: { sequenceId } },
                    {
                        onSuccess: () => toast.success(`Attached "${file.name}"`),
                        onError: (e) => toast.error(buildError(e as unknown as AppError)),
                    },
                );
            }
        },
        [upload, sequenceId],
    );

    const onDrop = (e: React.DragEvent) => {
        e.preventDefault();
        setDragging(false);
        if (e.dataTransfer?.files?.length) uploadFiles(e.dataTransfer.files);
    };

    const remove = (a: Attachment) => {
        confirm.show(`Remove "${a.filename}" from this step?`, async () => {
            await del.mutateAsync(a.id);
            toast.success("Attachment removed.");
        });
    };

    return (
        <div className="rounded-md border border-slate-200 bg-white">
            <div className="flex items-center justify-between gap-2 border-b border-slate-200/70 px-3 py-2.5">
                <div className="flex items-center gap-2 min-w-0">
                    <PaperclipIcon className="w-3.5 h-3.5 text-slate-400 shrink-0" />
                    <div className="min-w-0">
                        <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                            Attachments
                        </div>
                        <p className="truncate text-[11px] text-slate-400">
                            {attachments.length === 0
                                ? "Attach files to send with this step."
                                : `${attachments.length} file${attachments.length === 1 ? "" : "s"} · ${formatBytes(totalSize)} used`}
                        </p>
                    </div>
                </div>
            </div>

            <div className="space-y-3 p-3">
                <input
                    ref={inputRef}
                    type="file"
                    multiple
                    className="hidden"
                    onChange={(e) => {
                        if (e.target.files?.length) uploadFiles(e.target.files);
                        e.target.value = "";
                    }}
                />
                <button
                    type="button"
                    onClick={() => inputRef.current?.click()}
                    onDragOver={(e) => {
                        e.preventDefault();
                        setDragging(true);
                    }}
                    onDragLeave={() => setDragging(false)}
                    onDrop={onDrop}
                    disabled={upload.isPending}
                    className={`flex w-full flex-col items-center justify-center gap-1.5 rounded-md border border-dashed px-3 py-6 text-center transition-colors ${
                        dragging
                            ? "border-sky-400 bg-sky-50"
                            : "border-slate-300 bg-slate-50/60 hover:border-slate-400 hover:bg-slate-50"
                    } disabled:opacity-60`}
                >
                    {upload.isPending ? (
                        <Loader2Icon className="w-5 h-5 text-slate-400 animate-spin" />
                    ) : (
                        <UploadCloudIcon className={`w-5 h-5 ${dragging ? "text-sky-500" : "text-slate-400"}`} />
                    )}
                    <span className="text-[12.5px] font-medium text-slate-700">
                        {upload.isPending ? "Uploading…" : "Drag files here or click to upload"}
                    </span>
                    <span className="text-[10.5px] text-slate-400">
                        Files are sent with every email from this step.
                    </span>
                </button>

                {isLoading ? (
                    <div className="text-[11.5px] text-slate-400">Loading attachments…</div>
                ) : attachments.length === 0 ? (
                    <p className="text-[11.5px] text-slate-400">No attachments yet.</p>
                ) : (
                    <div className="divide-y divide-slate-200/60 rounded-md border border-slate-200">
                        {attachments.map((a) => (
                            <div key={a.id} className="flex items-center gap-2.5 px-3 py-2">
                                <FileIcon className="w-3.5 h-3.5 shrink-0 text-slate-400" />
                                <div className="min-w-0 flex-1">
                                    <a
                                        href={a.url}
                                        target="_blank"
                                        rel="noreferrer"
                                        className="block truncate text-[12.5px] font-medium text-slate-800 hover:text-sky-700"
                                        title={a.filename}
                                    >
                                        {a.filename}
                                    </a>
                                    <div className="truncate text-[10.5px] text-slate-400">
                                        {formatBytes(a.size)}
                                        {a.mime_type ? ` · ${a.mime_type}` : ""}
                                    </div>
                                </div>
                                <button
                                    type="button"
                                    onClick={() => remove(a)}
                                    title="Remove attachment"
                                    className="size-7 shrink-0 inline-flex items-center justify-center rounded-md text-slate-400 hover:text-rose-600 hover:bg-rose-50 transition-colors"
                                >
                                    <Trash2Icon className="w-3.5 h-3.5" />
                                </button>
                            </div>
                        ))}
                    </div>
                )}
            </div>
        </div>
    );
}
