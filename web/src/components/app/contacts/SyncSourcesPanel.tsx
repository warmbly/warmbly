// SyncSourcesPanel — the management surface for on-demand Google-Sheet → leads
// "sync sources". Lists saved sources with their last-sync result + status, and
// offers per-row Sync now / Edit / Delete plus a "New sync" entry that opens the
// SheetSyncWizard. Works in two placements:
//   - global Contacts page (no campaignId): lists every source.
//   - per-campaign leads view (campaignId set): lists that campaign's sources
//     and pre-targets the wizard to it.
//
// Rendered as a centered modal mirroring ImportWizard's dialog shell + theme.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import {
    AlertTriangleIcon,
    CheckCircle2Icon,
    Loader2Icon,
    PencilIcon,
    PlusIcon,
    RefreshCwIcon,
    SheetIcon,
    Trash2Icon,
    XIcon,
} from "lucide-react";
import toast from "react-hot-toast";

import { announceResult, describeError } from "./importShared";
import SheetSyncWizard from "./SheetSyncWizard";
import SyncSourceEditDrawer from "./SyncSourceEditDrawer";
import { useConfirm } from "@/hooks/context/confirm";
import useLeadSyncSources from "@/lib/api/hooks/app/leadsync/useLeadSyncSources";
import useDeleteLeadSyncSource from "@/lib/api/hooks/app/leadsync/useDeleteLeadSyncSource";
import useSyncLeadSyncSource from "@/lib/api/hooks/app/leadsync/useSyncLeadSyncSource";
import type { LeadSyncSource } from "@/lib/api/models/app/leadsync/LeadSync";

export default function SyncSourcesPanel({
    open,
    onClose,
    campaign,
}: {
    open: boolean;
    onClose: () => void;
    // When set, scopes the list + pre-targets new sources to this campaign.
    campaign?: { id: string; name: string };
}) {
    const sources = useLeadSyncSources(campaign?.id);
    const deleteSource = useDeleteLeadSyncSource();
    const syncSource = useSyncLeadSyncSource();
    const confirm = useConfirm();

    const [wizardOpen, setWizardOpen] = React.useState(false);
    const [editing, setEditing] = React.useState<LeadSyncSource | null>(null);
    const [syncingId, setSyncingId] = React.useState<string | null>(null);

    const list = sources.data?.data ?? [];

    async function runSync(src: LeadSyncSource) {
        setSyncingId(src.id);
        try {
            const res = await syncSource.mutateAsync(src.id);
            announceResult(res.result);
        } catch (err) {
            toast.error(describeError(err, "Sync failed."));
        } finally {
            setSyncingId(null);
        }
    }

    function confirmDelete(src: LeadSyncSource) {
        confirm.show(
            `Delete sync source "${src.label || src.sheet_title || "this sheet"}"? Contacts already imported stay.`,
            async () => {
                try {
                    await deleteSource.mutateAsync(src.id);
                    toast.success("Sync source deleted");
                } catch (err) {
                    toast.error(describeError(err, "Delete failed."));
                }
            },
        );
    }

    return (
        <>
            <AnimatePresence>
                {open && (
                    <motion.div
                        key="overlay"
                        initial={{ opacity: 0 }}
                        animate={{ opacity: 1 }}
                        exit={{ opacity: 0 }}
                        transition={{ duration: 0.15 }}
                        onClick={onClose}
                        className="fixed inset-0 z-[110] flex items-center justify-center bg-slate-900/30 backdrop-blur-[2px] px-4"
                    >
                        <motion.div
                            key="card"
                            initial={{ y: 8, opacity: 0 }}
                            animate={{ y: 0, opacity: 1 }}
                            exit={{ y: 8, opacity: 0 }}
                            transition={{ duration: 0.18 }}
                            onClick={(e) => e.stopPropagation()}
                            className="w-full max-w-[720px] rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18),0_8px_16px_-8px_rgba(15,23,42,0.1)] overflow-hidden flex flex-col max-h-[85dvh]"
                        >
                            <header className="h-12 px-4 border-b border-slate-200 flex items-center gap-2.5 shrink-0">
                                <div className="size-5 rounded bg-emerald-50 text-emerald-600 flex items-center justify-center">
                                    <SheetIcon className="w-3 h-3" />
                                </div>
                                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                                    Sync sources
                                </span>
                                {campaign && (
                                    <>
                                        <div className="h-4 w-px bg-slate-200" />
                                        <span className="text-[12.5px] text-slate-900 font-medium truncate min-w-0 max-w-[110px] md:max-w-[200px]">
                                            {campaign.name}
                                        </span>
                                    </>
                                )}
                                <button
                                    type="button"
                                    onClick={() => setWizardOpen(true)}
                                    aria-label={campaign ? "Connect a Google Sheet" : "New sync"}
                                    className="ml-auto h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors"
                                >
                                    <PlusIcon className="w-3 h-3" />
                                    <span className="hidden md:inline">
                                        {campaign ? "Connect a Google Sheet" : "New sync"}
                                    </span>
                                    <span className="md:hidden">New</span>
                                </button>
                                <button
                                    type="button"
                                    onClick={onClose}
                                    aria-label="Close"
                                    className="size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                                >
                                    <XIcon className="w-3.5 h-3.5" />
                                </button>
                            </header>

                            <div className="flex-1 min-h-0 overflow-y-auto">
                                {sources.isPending ? (
                                    <div className="divide-y divide-slate-200/60">
                                        {Array.from({ length: 3 }).map((_, i) => (
                                            <div key={i} className="h-16 px-5 flex items-center gap-3">
                                                <div className="w-8 h-8 rounded-md bg-slate-100" />
                                                <div className="flex-1 space-y-2">
                                                    <div className="h-3 w-40 bg-slate-100 rounded animate-pulse" />
                                                    <div className="h-2.5 w-56 bg-slate-100 rounded animate-pulse" />
                                                </div>
                                            </div>
                                        ))}
                                    </div>
                                ) : sources.isError ? (
                                    <div className="px-5 py-12 text-center">
                                        <div className="mx-auto mb-3 size-8 rounded-md bg-red-50 text-red-600 flex items-center justify-center">
                                            <AlertTriangleIcon className="w-4 h-4" />
                                        </div>
                                        <p className="text-[12.5px] text-slate-900 font-medium">
                                            Couldn&apos;t load sync sources
                                        </p>
                                        <button
                                            type="button"
                                            onClick={() => sources.refetch()}
                                            className="mt-3 h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors"
                                        >
                                            <RefreshCwIcon className="w-3 h-3" />
                                            Try again
                                        </button>
                                    </div>
                                ) : list.length === 0 ? (
                                    <div className="px-5 py-14 text-center">
                                        <div className="mx-auto size-10 rounded-md bg-slate-100 text-slate-400 flex items-center justify-center">
                                            <SheetIcon className="w-5 h-5" />
                                        </div>
                                        <p className="text-[13px] text-slate-900 font-medium mt-3">
                                            No sync sources yet
                                        </p>
                                        <p className="text-[11.5px] text-slate-500 mt-1 max-w-[42ch] mx-auto leading-relaxed">
                                            Connect a Google Sheet and re-run it on demand to pull new and
                                            updated leads into Warmbly{campaign ? ` and into ${campaign.name}` : ""}.
                                        </p>
                                        <button
                                            type="button"
                                            onClick={() => setWizardOpen(true)}
                                            className="mt-4 h-8 px-4 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors"
                                        >
                                            <PlusIcon className="w-3.5 h-3.5" />
                                            {campaign ? "Connect a Google Sheet" : "New sync"}
                                        </button>
                                    </div>
                                ) : (
                                    <div className="divide-y divide-slate-200/60">
                                        {list.map((src) => (
                                            <SourceRow
                                                key={src.id}
                                                source={src}
                                                syncing={syncingId === src.id}
                                                onSync={() => runSync(src)}
                                                onEdit={() => setEditing(src)}
                                                onDelete={() => confirmDelete(src)}
                                            />
                                        ))}
                                    </div>
                                )}
                            </div>
                        </motion.div>
                    </motion.div>
                )}
            </AnimatePresence>

            <SheetSyncWizard
                open={wizardOpen}
                onClose={() => setWizardOpen(false)}
                lockedCampaign={campaign}
                onSaved={() => sources.refetch()}
            />
            {editing && (
                <SyncSourceEditDrawer source={editing} onClose={() => setEditing(null)} />
            )}
        </>
    );
}

function SourceRow({
    source,
    syncing,
    onSync,
    onEdit,
    onDelete,
}: {
    source: LeadSyncSource;
    syncing: boolean;
    onSync: () => void;
    onEdit: () => void;
    onDelete: () => void;
}) {
    const r = source.last_result;
    return (
        <div className="group px-5 py-3 flex items-center gap-3 hover:bg-slate-50/80 transition-colors">
            <div className="size-8 rounded-md bg-emerald-50 text-emerald-600 flex items-center justify-center shrink-0">
                <SheetIcon className="w-4 h-4" />
            </div>
            <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2 min-w-0">
                    <span className="text-[12.5px] text-slate-900 font-medium truncate">
                        {source.label || source.sheet_title || "Untitled sheet"}
                    </span>
                    <StatusBadge status={source.status} hasError={!!source.last_error} />
                </div>
                <div className="text-[11px] text-slate-500 truncate flex items-center gap-1.5 mt-0.5">
                    {source.sheet_title && (
                        <span className="truncate">{source.sheet_title}</span>
                    )}
                    {source.tab_title && (
                        <>
                            <span className="text-slate-300">·</span>
                            <span className="truncate">{source.tab_title}</span>
                        </>
                    )}
                    <span className="text-slate-300">·</span>
                    <span className="shrink-0">
                        {source.last_synced_at
                            ? `synced ${new Date(source.last_synced_at).toLocaleString()}`
                            : "never synced"}
                    </span>
                </div>
                {r && (
                    <div className="mt-1 flex items-center gap-1.5 text-[10.5px] tabular-nums">
                        <Count label="imported" value={r.imported} tone="emerald" />
                        <Count label="updated" value={r.updated} tone="sky" />
                        <Count label="skipped" value={r.skipped} tone="slate" />
                        {r.failed > 0 && <Count label="failed" value={r.failed} tone="red" />}
                    </div>
                )}
                {source.last_error && (
                    <p className="mt-1 text-[10.5px] text-rose-600 truncate">{source.last_error}</p>
                )}
            </div>

            <div className="flex items-center gap-0.5 shrink-0">
                <button
                    type="button"
                    onClick={onSync}
                    disabled={syncing}
                    aria-label="Sync now"
                    className="h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                >
                    {syncing ? (
                        <Loader2Icon className="w-3 h-3 animate-spin" />
                    ) : (
                        <RefreshCwIcon className="w-3 h-3" />
                    )}
                    <span className="hidden md:inline">Sync now</span>
                </button>
                <button
                    type="button"
                    onClick={onEdit}
                    aria-label="Edit sync source"
                    className="size-7 rounded-md text-slate-400 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                >
                    <PencilIcon className="w-3.5 h-3.5" />
                </button>
                <button
                    type="button"
                    onClick={onDelete}
                    aria-label="Delete sync source"
                    className="size-7 rounded-md text-slate-400 hover:text-rose-600 hover:bg-rose-50 inline-flex items-center justify-center transition-colors"
                >
                    <Trash2Icon className="w-3.5 h-3.5" />
                </button>
            </div>
        </div>
    );
}

function StatusBadge({ status, hasError }: { status: string; hasError: boolean }) {
    if (status === "syncing") {
        return (
            <span className="inline-flex items-center gap-1 text-[10px] font-medium text-sky-700 uppercase tracking-[0.08em]">
                <Loader2Icon className="w-2.5 h-2.5 animate-spin" />
                syncing
            </span>
        );
    }
    if (status === "error" || hasError) {
        return (
            <span className="inline-flex items-center gap-1 text-[10px] font-medium text-rose-700 uppercase tracking-[0.08em]">
                <AlertTriangleIcon className="w-2.5 h-2.5" />
                error
            </span>
        );
    }
    return (
        <span className="inline-flex items-center gap-1 text-[10px] font-medium text-emerald-700 uppercase tracking-[0.08em]">
            <CheckCircle2Icon className="w-2.5 h-2.5" />
            idle
        </span>
    );
}

function Count({
    label,
    value,
    tone,
}: {
    label: string;
    value: number;
    tone: "emerald" | "sky" | "slate" | "red";
}) {
    const cls = {
        emerald: "bg-emerald-50 text-emerald-700",
        sky: "bg-sky-50 text-sky-700",
        slate: "bg-slate-100 text-slate-600",
        red: "bg-red-50 text-red-700",
    }[tone];
    return (
        <span className={`inline-flex items-center gap-1 h-4 px-1.5 rounded ${cls}`}>
            <span className="font-semibold">{value.toLocaleString()}</span>
            <span className="opacity-70">{label}</span>
        </span>
    );
}
